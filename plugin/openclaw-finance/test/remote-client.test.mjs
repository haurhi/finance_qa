import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import os from "node:os";

import { RemoteMCPClient, normalizePluginConfig } from "../dist/index.esm.js";

test("normalizePluginConfig reads remote bearer token from mcp_token_file", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "finance-token-file-"));
  const tokenFile = path.join(dir, "mcp_read_token");
  try {
    await writeFile(tokenFile, " file-token-value \n", { mode: 0o600 });

    const config = normalizePluginConfig({
      transport: "remote",
      mcp_url: "http://127.0.0.1:3009/mcp",
      mcp_token_file: tokenFile
    });

    assert.equal(config.transport, "remote");
    assert.equal(config.mcp_url, "http://127.0.0.1:3009/mcp");
    assert.equal(config.mcp_token, "file-token-value");
    assert.equal(config.mcp_token_file, tokenFile);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("RemoteMCPClient sends bearer auth, accept header, and reuses MCP session id", async () => {
  const seen = [];
  await withServer(async (req, res, body) => {
    seen.push({ headers: req.headers, body: JSON.parse(body || "{}") });
    assert.equal(req.headers.authorization, "Bearer test-token");
    assert.match(req.headers.accept || "", /application\/json/);
    assert.match(req.headers.accept || "", /text\/event-stream/);

    if (seen.length === 1) {
      assert.equal(seen[0].body.method, "initialize");
      res.setHeader("Mcp-Session-Id", "session-1");
      writeJSON(res, {
        jsonrpc: "2.0",
        id: seen[0].body.id,
        result: { serverInfo: { name: "financeqa-mcp" }, capabilities: {} }
      });
      return;
    }

    assert.equal(req.headers["mcp-session-id"], "session-1");
    assert.equal(seen[1].body.method, "tools/call");
    assert.equal(seen[1].body.params.name, "finance-query");
    writeJSON(res, {
      jsonrpc: "2.0",
      id: seen[1].body.id,
      result: { content: [{ type: "text", text: "{\"ok\":true}" }] }
    });
  }, async (url) => {
    const client = new RemoteMCPClient({ url, token: "test-token", timeoutMs: 5000 });
    const result = await client.callTool("finance-query", { query: "2026年3月营收" });
    assert.equal(result.content[0].text, "{\"ok\":true}");
  });
});

test("RemoteMCPClient parses SSE JSON-RPC responses", async () => {
  await withServer(async (req, res, body) => {
    const message = JSON.parse(body || "{}");
    res.setHeader("Content-Type", "text/event-stream");
    if (message.method === "initialize") {
      res.end(`event: message\ndata: ${JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: { serverInfo: { name: "financeqa-mcp" }, capabilities: {} }
      })}\n\n`);
      return;
    }
    res.end(`event: message\ndata: ${JSON.stringify({
      jsonrpc: "2.0",
      id: message.id,
      result: { content: [{ type: "text", text: "{\"sse\":true}" }] }
    })}\n\n`);
  }, async (url) => {
    const client = new RemoteMCPClient({ url, token: "test-token", timeoutMs: 5000 });
    const result = await client.callTool("finance-query", { query: "test" });
    assert.equal(result.content[0].text, "{\"sse\":true}");
  });
});

test("RemoteMCPClient reports auth failures without leaking token", async () => {
  await withServer(async (_req, res) => {
    res.statusCode = 401;
    res.end("unauthorized");
  }, async (url) => {
    const client = new RemoteMCPClient({ url, token: "super-secret-token", timeoutMs: 5000 });
    await assert.rejects(
      () => client.callTool("finance-query", { query: "test" }),
      (error) => {
        assert.match(error.message, /auth|401|unauthorized/i);
        assert.doesNotMatch(error.message, /super-secret-token/);
        return true;
      }
    );
  });
});

test("finance prompt hook strips relevant memories before prefetching facts", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const wrappedQuestion = `<relevant-memories>
The following are stored memories for user "mem0-tqt". Use them to personalize your response:
- As of 2026-06-25, 项目口径从2025年10月到2026年5月的应收未收总额为146,688.40 元。
</relevant-memories>

[Sat 2026-06-27 07:01 UTC] 从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？`;

    await hooks.get("before_prompt_build")({
      prompt: wrappedQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: wrappedQuestion }] }]
    });

    assert.equal(toolCalls[0].arguments.query, "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？");
  });
});

test("finance-query execute keeps clean tool query after polluted prompt hook", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const lzhWrappedPrompt = `Conversation info (untrusted metadata):
\`\`\`json
{
  "message_id": "openclaw-weixin:test",
  "timestamp": "Fri 2026-06-26 13:51 GMT+8"
}
\`\`\`

帮我做一个 润泽科技公司深度分析。包含公司概况 核心业务 财务数据 竞争格局 能力优势等`;

    await hooks.get("before_prompt_build")({
      prompt: lzhWrappedPrompt,
      messages: [{ role: "user", content: [{ type: "text", text: lzhWrappedPrompt }] }]
    });

    await tools.get("finance-query").execute("call-clean-query", {
      query: "润泽科技 客户 合同 收入 回款"
    });

    assert.equal(toolCalls.at(-1).arguments.query, "润泽科技 客户 合同 收入 回款");
  });
});

test("finance-query execute preserves raw user finance question for protected terms", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "从账上看，上一个完整月份净利润是多少？";
    const rewrittenQuery = "2026-06 净利润";
    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, { sessionKey: "finance-protected-raw-query" });

    await tools.get("finance-query").execute("call-rewritten-query", {
      query: rewrittenQuery
    });

    assert.equal(toolCalls.at(-1).arguments.query, rewrittenQuery);
    assert.equal(toolCalls.at(-1).arguments.raw_user_query, rawUserQuestion);
  });
});

test("finance-query transports model query unchanged and raw question separately", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "收入表中最新月份的营收数据是多少？";
    const rewrittenQuery = "收入表最新月份营收数据";
    const context = { sessionKey: "finance-query-transport-fields" };

    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, context);

    await tools.get("finance-query").execute("call-transport-fields", {
      query: rewrittenQuery
    }, context);

    assert.equal(toolCalls.at(-1).arguments.query, rewrittenQuery);
    assert.equal(toolCalls.at(-1).arguments.raw_user_query, rawUserQuestion);
  });
});

test("finance-query keeps factory run scope when runtime context is partial", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "老板，从账上看，最近完整月份的净利润是多少？";
    const rewrittenQuery = "2026年6月净利润";
    const factoryContext = { runId: "run-a", sessionKey: "session-a" };
    const financeQuery = tools.get("finance-query").create(factoryContext);

    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, factoryContext);

    await financeQuery.execute("call-run-a", {
      query: rewrittenQuery
    }, { sessionKey: "session-a" });

    assert.equal(toolCalls.at(-1).arguments.query, rewrittenQuery);
    assert.equal(toolCalls.at(-1).arguments.raw_user_query, rawUserQuestion);
  });
});

test("finance-query overwrites model supplied raw_user_query with the current run question", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "老板，从账上看，最近完整月份的净利润是多少？";
    const context = { runId: "authoritative-raw-run", sessionKey: "authoritative-raw-session" };
    const financeQuery = tools.get("finance-query").create(context);

    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, context);

    await financeQuery.execute("call-authoritative-raw", {
      query: "最近完整月份净利润",
      raw_user_query: "最近完整月份净利润是多少？"
    }, {});

    assert.equal(toolCalls.at(-1).arguments.raw_user_query, rawUserQuestion);
  });
});

test("finance-query concurrent runs do not exchange raw questions", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-concurrent-session";
    const bankContext = { runId: "finance-run-a", sessionKey };
    const revenueContext = { runId: "finance-run-b", sessionKey };
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";
    const bankTool = tools.get("finance-query").create(bankContext);
    const revenueTool = tools.get("finance-query").create(revenueContext);

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, bankContext);
    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, revenueContext);

    const executeCallStart = toolCalls.length;
    await revenueTool.execute("call-run-b", {
      query: "2026年6月项目结算营收"
    }, { sessionKey });
    await bankTool.execute("call-run-a", {
      query: "2026年6月银行卡净现金流"
    }, { sessionKey });

    const [revenueCall, bankCall] = toolCalls.slice(executeCallStart);
    assert.equal(revenueCall.arguments.query, "2026年6月项目结算营收");
    assert.equal(revenueCall.arguments.raw_user_query, revenueQuestion);
    assert.equal(bankCall.arguments.query, "2026年6月银行卡净现金流");
    assert.equal(bankCall.arguments.raw_user_query, bankQuestion);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query fails closed when one session has multiple ambiguous active runs", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-ambiguous-session";
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, { runId: "ambiguous-run-a", sessionKey });
    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, { runId: "ambiguous-run-b", sessionKey });
    await hooks.get("before_prompt_build")({
      prompt: "供应商上个完整自然月付款是多少？",
      messages: [{ role: "user", content: [{ type: "text", text: "供应商上个完整自然月付款是多少？" }] }]
    }, { sessionKey: "unrelated-session-fallback" });

    await tools.get("finance-query").execute("call-ambiguous", {
      query: "2026年6月净现金流",
      raw_user_query: "模型自带的错误原问题"
    }, { sessionKey });

    const args = toolCalls.at(-1).arguments;
    assert.equal(args.query, "2026年6月净现金流");
    assert.equal(Object.hasOwn(args, "raw_user_query"), false);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query does not borrow the remaining raw question after another active run consumes its own", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-partially-consumed-session";
    const bankContext = { runId: "partially-consumed-run-a", sessionKey };
    const revenueContext = { runId: "partially-consumed-run-b", sessionKey };
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, bankContext);
    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, revenueContext);

    await tools.get("finance-query").execute("consume-run-a", {
      query: "2026年6月银行卡净现金流"
    }, bankContext);
    await tools.get("finance-query").execute("ambiguous-after-run-a", {
      query: "2026年6月项目结算营收",
      raw_user_query: "模型自带的错误原问题"
    }, { sessionKey });

    assert.equal(Object.hasOwn(toolCalls.at(-1).arguments, "raw_user_query"), false);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query does not use global fallback after all active run raw questions are consumed", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-fully-consumed-session";
    const bankContext = { runId: "fully-consumed-run-a", sessionKey };
    const revenueContext = { runId: "fully-consumed-run-b", sessionKey };
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, bankContext);
    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, revenueContext);
    await tools.get("finance-query").execute("consume-run-a", {
      query: "2026年6月银行卡净现金流"
    }, bankContext);
    await tools.get("finance-query").execute("consume-run-b", {
      query: "2026年6月项目结算营收"
    }, revenueContext);

    const unrelatedQuestion = "供应商上个完整自然月付款是多少？";
    await hooks.get("before_prompt_build")({
      prompt: unrelatedQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: unrelatedQuestion }] }]
    }, { sessionKey: "unrelated-fully-consumed-fallback" });
    await tools.get("finance-query").execute("ambiguous-after-both", {
      query: "2026年6月项目结算营收",
      raw_user_query: "模型自带的错误原问题"
    }, { sessionKey });

    assert.equal(Object.hasOwn(toolCalls.at(-1).arguments, "raw_user_query"), false);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query does not use session raw while multiple runs remain active", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-active-runs-with-session-raw";
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, { runId: "session-raw-run-a", sessionKey });
    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, { runId: "session-raw-run-b", sessionKey });

    const sessionQuestion = "供应商上个完整自然月付款是多少？";
    await hooks.get("before_prompt_build")({
      prompt: sessionQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: sessionQuestion }] }]
    }, { sessionKey });
    await tools.get("finance-query").execute("ambiguous-session-raw", {
      query: "2026年6月供应商付款",
      raw_user_query: "模型自带的错误原问题"
    }, { sessionKey });

    assert.equal(Object.hasOwn(toolCalls.at(-1).arguments, "raw_user_query"), false);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query keeps observable runs active while their raw questions are pending", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-observable-active-runs";
    const observablePrompt = (question) => [
      "[巡检要求]",
      "这是一条只读巡检请求。回答前必须先调用 `finance-query` 获取最新事实。",
      "",
      "[用户原问题]",
      question
    ].join("\n");
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中最新月份项目结算营收是多少？";

    for (const [runId, question] of [
      ["observable-run-a", bankQuestion],
      ["observable-run-b", revenueQuestion]
    ]) {
      const prompt = observablePrompt(question);
      await hooks.get("before_prompt_build")({
        prompt,
        messages: [{ role: "user", content: [{ type: "text", text: prompt }] }]
      }, { runId, sessionKey });
    }

    const unrelatedQuestion = "供应商上个完整自然月付款是多少？";
    await hooks.get("before_prompt_build")({
      prompt: unrelatedQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: unrelatedQuestion }] }]
    }, { sessionKey: "unrelated-observable-fallback" });
    await tools.get("finance-query").execute("ambiguous-observable-runs", {
      query: "2026年6月项目结算营收",
      raw_user_query: "模型自带的错误原问题"
    }, { sessionKey });

    assert.equal(Object.hasOwn(toolCalls.at(-1).arguments, "raw_user_query"), false);
  }, {
    toolPayload: {
      success: true,
      finance_facts: { required_atoms: ["金额：1 元"] }
    }
  });
});

test("finance-query execute preserves reconciliation difference intent from raw user question", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "上个完整自然月银行净流入和账上净利润差多少？";
    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, { sessionKey: "finance-reconciliation-difference-intent" });

    await tools.get("finance-query").execute("call-rewritten-query-without-difference", {
      query: "上个完整自然月银行净流入和账上净利润"
    }, { sessionKey: "finance-reconciliation-difference-intent" });

    const args = toolCalls.at(-1).arguments;
    assert.equal(args.raw_user_query, rawUserQuestion);
    assert.equal(args.query, "上个完整自然月银行净流入和账上净利润");
  });
});

test("finance-query execute keeps rewritten entity hints while protecting dynamic period", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "从项目口径看，上个完整自然月百度这个客户还有多少没收回来？";
    await hooks.get("before_prompt_build")({
      prompt: rawUserQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: rawUserQuestion }] }]
    }, { sessionKey: "finance-raw-period-query-entity-merge" });

    await tools.get("finance-query").execute("call-rewritten-query-with-entity", {
      query: "2026年6月 百度在线网络技术(北京)有限公司 项目应收未收"
    });

    const args = toolCalls.at(-1).arguments;
    assert.equal(args.raw_user_query, rawUserQuestion);
    assert.equal(args.query, "2026年6月 百度在线网络技术(北京)有限公司 项目应收未收");
  });
});

test("finance-query protected question is scoped per OpenClaw session", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const bankQuestion = "银行卡上，上个完整自然月净现金流是多少？";
    const revenueQuestion = "收入表中上个完整自然月项目结算营收是多少？";

    await hooks.get("before_prompt_build")({
      prompt: bankQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: bankQuestion }] }]
    }, { sessionKey: "bank-session" });

    await hooks.get("before_prompt_build")({
      prompt: revenueQuestion,
      messages: [{ role: "user", content: [{ type: "text", text: revenueQuestion }] }]
    }, { sessionKey: "revenue-session" });

    await tools.get("finance-query").execute("bank-call", {
      query: "2026年6月 银行卡 净现金流"
    }, { sessionKey: "bank-session" });

    const bankArgs = toolCalls.at(-1).arguments;
    assert.equal(bankArgs.raw_user_query, bankQuestion);
    assert.equal(bankArgs.query, "2026年6月 银行卡 净现金流");

    await tools.get("finance-query").execute("revenue-call", {
      query: "2026年6月 收入表 项目结算营收"
    }, { sessionKey: "revenue-session" });

    const revenueArgs = toolCalls.at(-1).arguments;
    assert.equal(revenueArgs.raw_user_query, revenueQuestion);
    assert.equal(revenueArgs.query, "2026年6月 收入表 项目结算营收");
  });
});

test("finance prompt hook extracts original question from patrol wrapper without prefetching facts", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const rawUserQuestion = "按项目应收口径，2025年10月到上个完整自然月月底未回款合计多少？";
    const patrolPrompt = [
      "[巡检要求]",
      "这是一条只读巡检请求。回答前必须先调用 `finance-query` 获取最新事实；不要使用记忆、历史会话、已有上下文或猜测直接作答。",
      "",
      "[用户原问题]",
      rawUserQuestion
    ].join("\n");
    await hooks.get("before_prompt_build")({
      prompt: patrolPrompt,
      messages: [{ role: "user", content: [{ type: "text", text: patrolPrompt }] }]
    }, { sessionKey: "finance-patrol-original-question" });

    assert.equal(toolCalls.length, 0);

    await tools.get("finance-query").execute("call-rewritten-patrol-query", {
      query: "按项目应收口径，2025年10月到2026年6月底未回款合计多少？"
    });

    const args = toolCalls.at(-1).arguments;
    assert.equal(args.raw_user_query, rawUserQuestion);
    assert.equal(args.query, "按项目应收口径，2025年10月到2026年6月底未回款合计多少？");
  });
});

test("finance prompt hook prefers current prompt over stale session history", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks, tools }) => {
    const sessionKey = "finance-long-session-current-prompt";
    await hooks.get("before_prompt_build")({
      sessionKey,
      prompt: "收入表中最新月份项目结算营收是多少？",
      messages: [
        {
          role: "user",
          content: [{
            type: "text",
            text: "[Sun 2026-07-05 18:20 GMT+8] 按序时账口径，最新完整月份账上净利润是多少？"
          }]
        }
      ]
    }, { sessionKey });

    assert.equal(toolCalls[0].arguments.query, "收入表中最新月份项目结算营收是多少？");

    await tools.get("finance-query").execute("call-current-prompt", {
      query: "2026年3月 项目结算营收"
    });

    assert.equal(toolCalls.at(-1).arguments.query, "2026年3月 项目结算营收");
    assert.equal(toolCalls.at(-1).arguments.raw_user_query, "收入表中最新月份项目结算营收是多少？");
  });
});

test("before_message_write appends missing FinanceQA fact atoms only", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    assert.equal(typeof beforeWrite, "function");

    const sessionKey = "finance-source-session";
    const toolResult = {
      role: "toolResult",
      toolName: "finance-query",
      content: [{
        type: "text",
        text: JSON.stringify({
          success: true,
          final_answer: [
            "项目成本口径，未付款合计 2638110.61 元。",
            "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
            "来源更新时间：2026-06-29 20:02:31"
          ].join("\n"),
          data: {
            source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
            source_update_note: "来源更新时间：2026-06-29 20:02:31"
          }
        })
      }]
    };
    beforeWrite({ message: toolResult }, { sessionKey });

    const missingSource = {
      role: "assistant",
      content: [{ type: "text", text: "项目成本口径，未付款合计 2638110.61 元。" }],
      stopReason: "stop"
    };
    const patched = beforeWrite({ message: missingSource }, { sessionKey })?.message;
    assert.match(patched.content[0].text, /项目成本口径，未付款合计 2638110\.61 元。/);
    assert.match(patched.content[0].text, /来源：《优集收入、成本计算表 - 上传\.xlsx》的【成本-月度结算】/);
    assert.match(patched.content[0].text, /来源更新时间：2026-06-29 20:02:31/);
    assert.doesNotMatch(patched.content[0].text, /final_answer|finance-query|工具返回/);

    const alreadyHasSource = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "项目成本口径，未付款合计 2638110.61 元。",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
          "来源更新时间：2026-06-29 20:02:31"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    beforeWrite({ message: toolResult }, { sessionKey });
    const unchanged = beforeWrite({ message: alreadyHasSource }, { sessionKey })?.message;
    assert.equal(unchanged.content[0].text, alreadyHasSource.content[0].text);

    const factSessionKey = "finance-fact-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            final_answer: [
              "2025-10~2026-05 老板口径先看项目汇总：项目应付（应付未付/未付款） 1887361.66 元。",
              "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              "来源更新时间：2026-06-29 20:02:31"
            ].join("\n"),
            data: {
              period: "2025-10~2026-05",
              metric_label: "项目应付（应付未付/未付款）",
              total: 1887361.66,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              source_update_note: "来源更新时间：2026-06-29 20:02:31"
            }
          })
        }]
      }
    }, { sessionKey: factSessionKey });
    const convertedAmountAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2025-10~2026-05 项目口径应付约 188.74 万元。",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
          "来源更新时间：2026-06-29 20:02:31"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const factPatched = beforeWrite({ message: convertedAmountAnswer }, { sessionKey: factSessionKey })?.message;
    assert.match(factPatched.content[0].text, /金额：1887361\.66 元/);
    assert.match(factPatched.content[0].text, /口径：项目应付（应付未付\/未付款）/);
    assert.doesNotMatch(factPatched.content[0].text, /final_answer|finance-query|工具返回/);

    const malformedAmountSessionKey = "finance-malformed-amount-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            final_answer: [
              "2025-10~2026-06 老板口径先看项目汇总：项目应付（应付未付/未付款） 3538259.73 元。",
              "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              "来源更新时间：2026-07-01 18:10:42"
            ].join("\n"),
            data: {
              period: "2025-10~2026-06",
              metric_label: "项目应付（应付未付/未付款）",
              total: 3538259.73,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              source_update_note: "来源更新时间：2026-07-01 18:10:42"
            }
          })
        }]
      }
    }, { sessionKey: malformedAmountSessionKey });
    const malformedAmountAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2025-10~2026-06 期间，所有项目应付未付合计 **353,825,97.73 元**。",
          "口径：项目应付（应付未付/未付款）",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
          "更新时间：2026-07-01 18:10:42"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const amountCorrected = beforeWrite({ message: malformedAmountAnswer }, { sessionKey: malformedAmountSessionKey })?.message;
    assert.doesNotMatch(amountCorrected.content[0].text, /353,825,97\.73/);
    assert.match(amountCorrected.content[0].text, /3538259\.73 元/);
    assert.match(amountCorrected.content[0].text, /来源更新时间：2026-07-01 18:10:42/);

    const staleAnswerSessionKey = "finance-stale-answer-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            final_answer: "2025-10~2026-06 老板口径先看项目汇总：项目应付（应付未付/未付款） 3538259.73 元。",
            data: {
              period: "2025-10~2026-06",
              metric_label: "项目应付（应付未付/未付款）",
              total: 3538259.73,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              source_update_note: "来源更新时间：2026-07-01 18:10:42",
              contract_summary: {
                cost_settlement: 14644177.16,
                cost_paid: 11105917.43,
                payable_open_items: [
                  {
                    supplier_name: "南京林悦智能科技有限公司",
                    contract_content: "行业商品数据采购合同",
                    settlement_amount: 3343015.18,
                    paid_amount: 1309631.38,
                    unpaid_amount: 2033383.8
                  },
                  {
                    supplier_name: "重庆智博渊源信息技术咨询服务有限公司",
                    contract_content: "合并行合计",
                    settlement_amount: 137804,
                    paid_amount: 0,
                    unpaid_amount: 137804
                  }
                ]
              }
            }
          })
        }]
      }
    }, { sessionKey: staleAnswerSessionKey });
    const staleAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "口径：项目应付（应付未付/未付款）",
          "金额：1887361.66 元",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
          "来源更新时间：2026-06-29 20:02:31",
          "期间：2025-10~2026-06",
          "",
          "| # | 供应商-合同/项目 | **未付款** |",
          "|---|---|---|",
          "| 1 | 南京林悦智能-行业商品数据采购合同 | 731,806.22 |"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const staleCorrected = beforeWrite({ message: staleAnswer }, { sessionKey: staleAnswerSessionKey })?.message;
    assert.doesNotMatch(staleCorrected.content[0].text, /1887361\.66|731,806\.22|2026-06-29 20:02:31/);
    assert.match(staleCorrected.content[0].text, /金额：3538259\.73 元/);
    assert.match(staleCorrected.content[0].text, /重庆智博渊源信息技术咨询服务有限公司-合并行合计/);
    assert.match(staleCorrected.content[0].text, /未付款 137804\.00 元/);
    assert.match(staleCorrected.content[0].text, /来源更新时间：2026-07-01 18:10:42/);
    assert.doesNotMatch(staleCorrected.content[0].text, /final_answer|finance-query|工具返回/);

    const conflictingHeadlineSessionKey = "finance-conflicting-headline-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            data: {
              period: "2025-10~2026-06",
              metric_label: "项目应收（应收未收）",
              total: 367698.75,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【25年Q4收入明细】和【26年Q2收入明细】",
              source_update_note: "来源更新时间：2026-07-03 18:39:21",
              contract_summary: {
                settlement_amount: 367698.75,
                received_amount: 0,
                receivable_open_items: [
                  {
                    customer_name: "辽宁金程信息科技有限公司",
                    contract_content: "海外平台商品数据监控合同-A05",
                    settlement_amount: 210791.4,
                    received_amount: 0,
                    unreceived_amount: 210791.4
                  },
                  {
                    customer_name: "四川其妙科技有限公司",
                    contract_content: "海外平台商品数据监控合同-A05",
                    settlement_amount: 156907.35,
                    received_amount: 0,
                    unreceived_amount: 156907.35
                  }
                ]
              }
            }
          })
        }]
      }
    }, { sessionKey: conflictingHeadlineSessionKey });
    const conflictingHeadlineAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "合同关键词为“海外平台商品数据监控合同-A05”的合同，应收未收情况如下：",
          "",
          "**项目应收（应收未收）：2717692.45 元**",
          "",
          "明细里两家主体合计未回款 367,698.75 元。",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【25年Q4收入明细】和【26年Q2收入明细】",
          "来源更新时间：2026-07-03 18:39:21"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const headlineCorrected = beforeWrite({ message: conflictingHeadlineAnswer }, { sessionKey: conflictingHeadlineSessionKey })?.message;
    assert.doesNotMatch(headlineCorrected.content[0].text, /2717692\.45/);
    assert.match(headlineCorrected.content[0].text, /项目应收（应收未收）：367698\.75 元/);
    assert.match(headlineCorrected.content[0].text, /金额：367698\.75 元/);
    assert.match(headlineCorrected.content[0].text, /来源更新时间：2026-07-03 18:39:21/);
    assert.doesNotMatch(headlineCorrected.content[0].text, /final_answer|finance-query|工具返回/);

    const nonFinance = {
      role: "assistant",
      content: [{ type: "text", text: "普通回答。" }],
      stopReason: "stop"
    };
    const untouched = beforeWrite({ message: nonFinance }, { sessionKey: "no-finance-tool-result" });
    assert.equal(untouched, undefined);

    beforeWrite({
      message: {
        role: "toolResult",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            data: {
              source_note: "来源：《其他工具.xlsx》",
              source_update_note: "来源更新时间：2026-06-29 20:02:31"
            }
          })
        }]
      }
    }, { sessionKey: "anonymous-tool-result" });
    const anonymousToolAnswer = {
      role: "assistant",
      content: [{ type: "text", text: "其他工具回答。" }],
      stopReason: "stop"
    };
    assert.equal(beforeWrite({ message: anonymousToolAnswer }, { sessionKey: "anonymous-tool-result" }), undefined);
  });
});

test("prefetched finance facts guard repeated answers that skip a fresh tool call", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforePrompt = hooks.get("before_prompt_build");
    const beforeWrite = hooks.get("before_message_write");
    const llmOutput = hooks.get("llm_output");
    const sessionKey = "finance-repeat-session";

    const promptResult = await beforePrompt({
      sessionKey,
      prompt: "25年至26年未付款的项目及对应金额有哪些？",
      messages: [
        {
          role: "assistant",
          content: [{
            type: "text",
            text: "2025-10~2026-05 项目口径应付（应付未付/未付款） 1887361.66 元。"
          }]
        },
        {
          role: "user",
          content: [{
            type: "text",
            text: "[Wed 2026-07-01 11:09 GMT+8] 25年至26年未付款的项目及对应金额有哪些？"
          }]
        }
      ]
    }, { sessionKey });

    assert.equal(toolCalls.length, 1);
    assert.equal(toolCalls[0].arguments.query, "25年至26年未付款的项目及对应金额有哪些？");
    assert.match(promptResult.prependSystemContext, /2025-10~2026-06/);

    const staleRepeatedAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2025-10~2026-05 项目口径应付（应付未付/未付款） 1887361.66 元。",
          "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
          "来源更新时间：2026-06-29 20:02:31"
        ].join("\n")
      }],
      stopReason: "stop"
    };

    const patched = beforeWrite({ message: staleRepeatedAnswer, sessionKey }, { sessionKey })?.message;
    assert.match(patched.content[0].text, /2025-10~2026-06/);
    assert.doesNotMatch(patched.content[0].text, /2025-10~2026-05/);
    assert.match(patched.content[0].text, /口径：项目应付（应付未付\/未付款）/);
    assert.match(patched.content[0].text, /1887361\.66/);
    assert.match(patched.content[0].text, /来源：《优集收入、成本计算表 - 上传\.xlsx》的【成本-月度结算】/);
    assert.match(patched.content[0].text, /来源更新时间：2026-06-29 20:02:31/);
    assert.doesNotMatch(patched.content[0].text, /final_answer|finance-query|工具返回/);

    const assistantTexts = [
      [
        "口径：项目应付（应付未付/未付款）",
        "金额：1887361.66 元",
        "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
        "来源更新时间：2026-06-29 20:02:31",
        "期间：2025-10~2026-05"
      ].join("\n")
    ];
    llmOutput({ assistantTexts }, { sessionKey });
    assert.match(assistantTexts[0], /期间：2025-10~2026-06/);
    assert.doesNotMatch(assistantTexts[0], /2025-10~2026-05/);
    assert.doesNotMatch(assistantTexts[0], /final_answer|finance-query|工具返回/);
  }, {
    toolPayload: {
      success: true,
      final_answer: [
        "2025-10~2026-06 老板口径先看项目汇总：项目应付（应付未付/未付款） 1887361.66 元。",
        "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
        "来源更新时间：2026-06-29 20:02:31"
      ].join("\n"),
      data: {
        period: "2025-10~2026-06",
        metric_label: "项目应付（应付未付/未付款）",
        total: 1887361.66,
        source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
        source_update_note: "来源更新时间：2026-06-29 20:02:31"
      }
    }
  });
});

test("finance facts package drives prompt context and answer guard", async () => {
  const toolCalls = [];
  const toolPayload = {
    success: true,
    final_answer: "旧答案不应作为事实源：2026-06 账上净利润 0 元。",
    finance_facts: {
      schema_version: "finance_facts.v1",
      resolved_period: "2026-03",
      requested_period: "最新完整月份",
      basis: "序时账口径",
      source_tables: ["fin_journal"],
      source_files: ["《测试序时账.xls》"],
      source_note: "来源：《测试序时账.xls》",
      source_update_note: "来源更新时间：2026-07-01 12:00:00",
      metrics: { "账上净利润": 291291.55 },
      headline_metric: "账上净利润",
      headline_amount: 291291.55,
      warnings: ["数据只到 2026-03，2026-06 未入库"],
      explanation_hints: ["按序时账本年利润科目取数"],
      required_atoms: [
        "期间：2026-03",
        "口径：序时账口径",
        "金额：291291.55 元",
        "来源：《测试序时账.xls》",
        "来源更新时间：2026-07-01 12:00:00"
      ]
    }
  };

  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforePrompt = hooks.get("before_prompt_build");
    const beforeWrite = hooks.get("before_message_write");
    const sessionKey = "finance-facts-package-session";

    const promptResult = await beforePrompt({
      sessionKey,
      prompt: "按序时账口径，最新完整月份账上净利润是多少？",
      messages: []
    }, { sessionKey });

    assert.match(promptResult.prependSystemContext, /FinanceQA 决定事实，OpenClaw 决定表达/);
    assert.match(promptResult.prependSystemContext, /结构化事实包/);
    assert.match(promptResult.prependSystemContext, /resolved_period=2026-03/);
    assert.match(promptResult.prependSystemContext, /requested_period=最新完整月份/);
    assert.match(promptResult.prependSystemContext, /basis=序时账口径/);
    assert.match(promptResult.prependSystemContext, /标准金额：291291\.55/);
    assert.match(promptResult.prependSystemContext, /数据只到 2026-03/);
    assert.doesNotMatch(promptResult.prependSystemContext, /旧答案不应作为事实源/);

    const wrongAssistantAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2026-06 账上净利润为 0 元。",
          "来源：未记录"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const patched = beforeWrite({ message: wrongAssistantAnswer }, { sessionKey })?.message;
    assert.match(patched.content[0].text, /期间：2026-03/);
    assert.match(patched.content[0].text, /口径：序时账口径/);
    assert.match(patched.content[0].text, /金额：291291\.55 元/);
    assert.match(patched.content[0].text, /来源：《测试序时账\.xls》/);
    assert.match(patched.content[0].text, /来源更新时间：2026-07-01 12:00:00/);
    assert.doesNotMatch(patched.content[0].text, /2026-06 账上净利润为 0 元/);
    assert.doesNotMatch(patched.content[0].text, /旧答案不应作为事实源|final_answer|finance-query|工具返回/);
  }, { toolPayload });
});

test("before_message_write rejects assistant denial that contradicts successful finance facts", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const sessionKey = "finance-fact-denial-conflict-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2025-10~2026-06",
              basis: "项目成本口径",
              headline_metric: "项目应付（应付未付/未付款）",
              headline_amount: 3624621.66,
              source_files: ["《优集收入、成本计算表 - 上传.xlsx》"],
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              source_update_note: "来源更新时间：2026-07-07 15:02:52",
              metrics: {
                "项目应付": 3624621.66,
                "项目成本": 14872345.68,
                "已付款": 11247724.02
              },
              required_atoms: [
                "期间：2025-10~2026-06",
                "口径：项目应付（应付未付/未付款）",
                "金额：3624621.66 元",
                "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
                "来源更新时间：2026-07-07 15:02:52"
              ]
            },
            data: {
              contract_summary: {
                cost_settlement: 14872345.68,
                cost_paid: 11247724.02
              }
            }
          })
        }]
      }
    }, { sessionKey });

    const denialAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "黄总，2025-10~2026-06 这个期间按项目口径查应付未付，系统目前返回不了具体金额。",
          "原因是没有识别到对应的合同/项目主体，也不能直接回答。"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const patched = beforeWrite({ message: denialAnswer }, { sessionKey })?.message;
    const text = patched.content[0].text;
    assert.match(text, /期间：2025-10~2026-06/);
    assert.match(text, /口径：项目应付（应付未付\/未付款）/);
    assert.match(text, /金额：3624621\.66 元/);
    assert.match(text, /来源：《优集收入、成本计算表 - 上传\.xlsx》的【成本-月度结算】/);
    assert.match(text, /来源更新时间：2026-07-07 15:02:52/);
    assert.doesNotMatch(text, /返回不了具体金额|没有识别到对应的合同\/项目主体|不能直接回答/);
  });
});

test("before_message_write replaces assistant answer that conflicts with reconciliation finance facts", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const sessionKey = "finance-reconciliation-fact-conflict-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-03",
              basis: "账上利润与银行流水双口径对账",
              headline_metric: "账上净利润-银行净流入名义差额",
              headline_amount: 925150.88,
              source_files: [
                "《交易查询，20260101-20260331，共143笔.xlsx》",
                "《南京优集1-3月序时账.xls》"
              ],
              source_note: "来源：《交易查询，20260101-20260331，共143笔.xlsx》；《南京优集1-3月序时账.xls》",
              source_update_note: "来源更新时间：2026-04-27 13:33:40",
              metrics: {
                "银行净流入": -633859.33,
                "账上净利润": 291291.55,
                "差异金额": 925150.88,
                "账上净利润-银行净流入": 925150.88
              },
              required_atoms: [
                "期间：2026-03",
                "口径：账上利润与银行流水双口径对账",
                "银行净流入：-633859.33 元",
                "账上净利润：291291.55 元",
                "差异金额：925150.88 元",
                "金额：925150.88 元",
                "来源：《交易查询，20260101-20260331，共143笔.xlsx》；《南京优集1-3月序时账.xls》",
                "来源更新时间：2026-04-27 13:33:40"
              ]
            }
          })
        }]
      }
    }, { sessionKey });

    const conflictedAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2026年6月银行流水和账上利润都没有数据。",
          "银行净流入 0 元，账上净利润 0 元，相差 0 元。",
          "来源：未记录"
        ].join("\n")
      }],
      stopReason: "stop"
    };

    const patched = beforeWrite({ message: conflictedAnswer }, { sessionKey })?.message;
    const text = patched.content[0].text;
    assert.match(text, /期间：2026-03/);
    assert.match(text, /口径：账上利润与银行流水双口径对账/);
    assert.match(text, /银行净流入：-633859\.33 元/);
    assert.match(text, /账上净利润：291291\.55 元/);
    assert.match(text, /差异金额：925150\.88 元/);
    assert.match(text, /来源：《交易查询，20260101-20260331，共143笔\.xlsx》；《南京优集1-3月序时账\.xls》/);
    assert.match(text, /来源更新时间：2026-04-27 13:33:40/);
    assert.doesNotMatch(text, /2026年6月|银行净流入 0 元|账上净利润 0 元|相差 0 元|来源：未记录/);

    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-03",
              basis: "账上利润与银行流水双口径对账",
              headline_metric: "账上净利润-银行净流入名义差额",
              headline_amount: 925150.88,
              source_note: "来源：《交易查询，20260101-20260331，共143笔.xlsx》；《南京优集1-3月序时账.xls》",
              source_update_note: "来源更新时间：2026-04-27 13:33:40",
              metrics: {
                "银行净流入": -633859.33,
                "账上净利润": 291291.55,
                "差异金额": 925150.88,
                "账上净利润-银行净流入": 925150.88
              },
              required_atoms: [
                "期间：2026-03",
                "口径：账上利润与银行流水双口径对账",
                "银行净流入：-633859.33 元",
                "账上净利润：291291.55 元",
                "差异金额：925150.88 元",
                "金额：925150.88 元",
                "来源：《交易查询，20260101-20260331，共143笔.xlsx》；《南京优集1-3月序时账.xls》",
                "来源更新时间：2026-04-27 13:33:40"
              ]
            }
          })
        }]
      }
    }, { sessionKey });

    const wrongDifferenceOnly = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "2026-03 银行净流入 -633859.33 元，账上净利润 291291.55 元。",
          "两者差异 0 元。",
          "来源：《交易查询，20260101-20260331，共143笔.xlsx》；《南京优集1-3月序时账.xls》",
          "来源更新时间：2026-04-27 13:33:40"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const diffPatched = beforeWrite({ message: wrongDifferenceOnly }, { sessionKey })?.message;
    const diffText = diffPatched.content[0].text;
    assert.match(diffText, /差异金额：925150\.88 元/);
    assert.doesNotMatch(diffText, /差异 0 元/);
  });
});

test("before_message_write appends cash flow amount from structured payload", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const sessionKey = "finance-cash-flow-fact-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            final_answer: "2026-03 银行卡净增加 -633859.33 元。",
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-03",
              requested_period: "2026-07",
              basis: "银行流水口径",
              source_files: ["《交易查询，20260101-20260331，共143笔.xlsx》"],
              source_note: "来源：《交易查询，20260101-20260331，共143笔.xlsx》",
              source_update_note: "来源更新时间：2026-04-27 13:33:40",
              required_atoms: [
                "期间：2026-03",
                "口径：银行流水口径",
                "来源：《交易查询，20260101-20260331，共143笔.xlsx》",
                "来源更新时间：2026-04-27 13:33:40"
              ]
            },
            data: {
              cash_flow: {
                "净现金流": -633859.33,
                "现金流入": 2613554.53,
                "现金流出": 3247413.86
              }
            }
          })
        }]
      }
    }, { sessionKey });

    const missingAmount = {
      role: "assistant",
      content: [{
        type: "text",
        text: [
          "口径：银行流水口径",
          "期间：2026-03",
          "说明：当前未返回该月净现金流具体数值。",
          "来源：《交易查询，20260101-20260331，共143笔.xlsx》",
          "来源更新时间：2026-04-27 13:33:40"
        ].join("\n")
      }],
      stopReason: "stop"
    };
    const patched = beforeWrite({ message: missingAmount }, { sessionKey })?.message;
    assert.match(patched.content[0].text, /金额：-633859\.33 元/);
  });
});

test("llm_output patches runner payload text with missing FinanceQA atoms", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const llmOutput = hooks.get("llm_output");
    const sessionKey = "finance-runner-payload-source-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-03",
              basis: "银行流水口径",
              headline_metric: "净现金流",
              headline_amount: -633859.33,
              source_note: "来源：《交易查询，20260101-20260331，共143笔.xlsx》",
              source_update_note: "来源更新时间：2026-04-27 13:33:40",
              required_atoms: [
                "期间：2026-03",
                "口径：银行流水口径",
                "金额：-633859.33 元",
                "来源：《交易查询，20260101-20260331，共143笔.xlsx》",
                "来源更新时间：2026-04-27 13:33:40"
              ]
            }
          })
        }]
      }
    }, { sessionKey });

    const event = {
      payloads: [{
        text: "2026-03 银行卡净现金流为 **-633,859.33 元**（银行流水口径）。"
      }]
    };
    llmOutput(event, { sessionKey });

    assert.match(event.payloads[0].text, /-633,859\.33 元/);
    assert.doesNotMatch(event.payloads[0].text, /-金额：|金额：-633859\.33 元 元/);
    assert.match(event.payloads[0].text, /来源：《交易查询，20260101-20260331，共143笔\.xlsx》/);
    assert.match(event.payloads[0].text, /来源更新时间：2026-04-27 13:33:40/);

    const nestedSessionKey = "finance-runner-nested-result-payload-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-06",
              basis: "项目口径",
              headline_metric: "项目结算收入（营收）",
              headline_amount: 936308.25,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
              source_update_note: "来源更新时间：2026-07-03 18:39:21",
              required_atoms: [
                "期间：2026-06",
                "口径：项目口径",
                "金额：936308.25 元",
                "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
                "来源更新时间：2026-07-03 18:39:21"
              ]
            }
          })
        }]
      }
    }, { sessionKey: nestedSessionKey });

    const nestedEvent = {
      result: {
        payloads: [{
          text: "2026年6月，项目结算 **93.63万** 元。\n\n来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】，更新时间 2026-07-03 18:39:21"
        }]
      }
    };
    llmOutput(nestedEvent, { sessionKey: nestedSessionKey });

    assert.match(nestedEvent.result.payloads[0].text, /金额：936308\.25 元/);
    assert.match(nestedEvent.result.payloads[0].text, /来源更新时间：2026-07-03 18:39:21/);
    assert.doesNotMatch(nestedEvent.result.payloads[0].text, /final_answer|finance-query|工具返回/);

    const staleDateOnlySessionKey = "finance-runner-date-only-source-update-session";
    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-06",
              basis: "项目口径",
              headline_metric: "项目结算收入（营收）",
              headline_amount: 1858392.90,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
              source_update_note: "来源更新时间：2026-07-07 15:02:52",
              required_atoms: [
                "期间：2026-06",
                "口径：项目口径",
                "金额：1858392.90 元",
                "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
                "来源更新时间：2026-07-07 15:02:52"
              ]
            }
          })
        }]
      }
    }, { sessionKey: staleDateOnlySessionKey });

    const staleDateOnlyEvent = {
      result: {
        payloads: [{
          text: "2026年6月，项目结算 1,858,392.90 元。\n来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】\n更新时间：2026-06-07"
        }]
      }
    };
    llmOutput(staleDateOnlyEvent, { sessionKey: staleDateOnlySessionKey });

    assert.match(staleDateOnlyEvent.result.payloads[0].text, /来源更新时间：2026-07-07 15:02:52/);
    assert.doesNotMatch(staleDateOnlyEvent.result.payloads[0].text, /更新时间：2026-06-07/);
  });
});

test("llm_output guards message-shaped finance answers and preserves facts for before_message_write fallback", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const llmOutput = hooks.get("llm_output");
    const sessionKey = "finance-llm-output-message-shaped-session";

    const toolResult = {
      role: "toolResult",
      toolName: "finance-query",
      content: [{
        type: "text",
        text: JSON.stringify({
          success: true,
          finance_facts: {
            schema_version: "finance_facts.v1",
            resolved_period: "2026-03",
            basis: "序时账口径",
            headline_metric: "账上净利润",
            headline_amount: 291291.55,
            source_files: ["《南京优集1-3月序时账.xls》"],
            source_note: "来源：《南京优集1-3月序时账.xls》",
            source_update_note: "来源更新时间：2026-04-27 13:33:40",
            metrics: { "账上净利润": 291291.55 },
            required_atoms: [
              "期间：2026-03",
              "口径：序时账口径",
              "金额：291291.55 元",
              "来源：《南京优集1-3月序时账.xls》",
              "来源更新时间：2026-04-27 13:33:40"
            ]
          }
        })
      }]
    };

    beforeWrite({ message: toolResult }, { sessionKey });
    const messageEvent = {
      message: {
        role: "assistant",
        content: [{
          type: "text",
          text: [
            "2026-06 项目经营口径利润 -1001256.02 元。",
            "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】"
          ].join("\n")
        }]
      }
    };
    llmOutput(messageEvent, { sessionKey });
    assert.match(messageEvent.message.content[0].text, /期间：2026-03/);
    assert.match(messageEvent.message.content[0].text, /口径：序时账口径/);
    assert.match(messageEvent.message.content[0].text, /金额：291291\.55 元/);
    assert.match(messageEvent.message.content[0].text, /来源：《南京优集1-3月序时账\.xls》/);
    assert.doesNotMatch(messageEvent.message.content[0].text, /2026-06 项目经营口径利润|-1001256\.02/);

    const fallbackSessionKey = "finance-llm-output-unpatchable-fallback-session";
    beforeWrite({ message: toolResult }, { sessionKey: fallbackSessionKey });
    llmOutput({ ignored: { text: "2026-06 项目经营口径利润 -1001256.02 元。" } }, { sessionKey: fallbackSessionKey });
    const persistedWrongAnswer = {
      role: "assistant",
      content: [{
        type: "text",
        text: "2026-06 项目经营口径利润 -1001256.02 元。来源：未记录"
      }]
    };
    const patched = beforeWrite({ message: persistedWrongAnswer }, { sessionKey: fallbackSessionKey })?.message;
    assert.match(patched.content[0].text, /期间：2026-03/);
    assert.match(patched.content[0].text, /金额：291291\.55 元/);
    assert.doesNotMatch(patched.content[0].text, /-1001256\.02|来源：未记录/);
  });
});

test("llm_output still patches stdout after before_message_write patched persisted assistant", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforeWrite = hooks.get("before_message_write");
    const llmOutput = hooks.get("llm_output");
    const sessionKey = "finance-stdout-after-persisted-assistant-session";

    beforeWrite({
      message: {
        role: "toolResult",
        toolName: "finance-query",
        content: [{
          type: "text",
          text: JSON.stringify({
            success: true,
            finance_facts: {
              schema_version: "finance_facts.v1",
              resolved_period: "2026-06",
              basis: "项目口径",
              headline_metric: "项目结算收入（营收）",
              headline_amount: 936308.25,
              source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
              source_update_note: "来源更新时间：2026-07-03 18:39:21",
              required_atoms: [
                "期间：2026-06",
                "口径：项目口径",
                "金额：936308.25 元",
                "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
                "来源更新时间：2026-07-03 18:39:21"
              ]
            }
          })
        }]
      }
    }, { sessionKey });

    const persisted = beforeWrite({
      message: {
        role: "assistant",
        content: [{
          type: "text",
          text: "2026年6月，项目结算收入 **93.63万** 元。"
        }]
      }
    }, { sessionKey })?.message;

    assert.match(persisted.content[0].text, /金额：936308\.25 元/);

    const event = {
      result: {
        payloads: [{
          text: "2026年6月，项目结算收入（营收）：**936308.25万** 元。\n来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】\n更新时间：2026-06-03 18:39:21"
        }]
      }
    };
    llmOutput(event, { sessionKey });

    assert.match(event.result.payloads[0].text, /金额：936308\.25 元/);
    assert.doesNotMatch(event.result.payloads[0].text, /936308\.25万/);
    assert.match(event.result.payloads[0].text, /来源更新时间：2026-07-03 18:39:21/);
    assert.doesNotMatch(event.result.payloads[0].text, /更新时间：2026-06-03 18:39:21/);

    const nextEvent = {
      payloads: [{
        text: "普通非财务回答。"
      }]
    };
    llmOutput(nextEvent, { sessionKey });

    assert.equal(nextEvent.payloads[0].text, "普通非财务回答。");
  });
});

test("finance fact guards are isolated by run when OpenClaw reuses the same session key", async () => {
  const toolCalls = [];
  await withFinancePluginHarness(toolCalls, async ({ hooks }) => {
    const beforePrompt = hooks.get("before_prompt_build");
    const llmOutput = hooks.get("llm_output");
    const sessionKey = "agent:main:main";
    const revenueRun = "run-revenue";
    const payableRun = "run-payable";

    await beforePrompt({
      sessionKey,
      runId: revenueRun,
      prompt: "老板，按最新可见月份，帮我看下当月营收。",
      messages: []
    }, { sessionKey, runId: revenueRun });

    await beforePrompt({
      sessionKey,
      runId: payableRun,
      prompt: "25年至26年未付款的项目及对应金额有哪些？",
      messages: []
    }, { sessionKey, runId: payableRun });

    const event = {
      result: {
        payloads: [{
          text: "2026年6月营收为 93.63 万元。"
        }]
      }
    };
    llmOutput(event, { sessionKey, runId: revenueRun });

    assert.match(event.result.payloads[0].text, /项目结算收入（营收）/);
    assert.match(event.result.payloads[0].text, /金额：936308\.25 元/);
    assert.match(event.result.payloads[0].text, /期间：2026-06/);
    assert.match(event.result.payloads[0].text, /来源：《优集收入、成本计算表 - 上传\.xlsx》的【26年Q2收入明细】/);
    assert.doesNotMatch(event.result.payloads[0].text, /项目应付|3538259\.73|2025-10~2026-06/);
  }, {
    toolPayload(args) {
      const query = String(args.query || "");
      if (query.includes("未付款") || query.includes("应付")) {
        return {
          success: true,
          finance_facts: {
            schema_version: "finance_facts.v1",
            resolved_period: "2025-10~2026-06",
            basis: "项目成本口径",
            headline_metric: "项目应付（应付未付/未付款）",
            headline_amount: 3538259.73,
            source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
            source_update_note: "来源更新时间：2026-07-03 18:39:21",
            required_atoms: [
              "期间：2025-10~2026-06",
              "口径：项目应付（应付未付/未付款）",
              "金额：3538259.73 元",
              "来源：《优集收入、成本计算表 - 上传.xlsx》的【成本-月度结算】",
              "来源更新时间：2026-07-03 18:39:21"
            ]
          }
        };
      }
      return {
        success: true,
        finance_facts: {
          schema_version: "finance_facts.v1",
          resolved_period: "2026-06",
          basis: "项目口径",
          headline_metric: "项目结算收入（营收）",
          headline_amount: 936308.25,
          source_note: "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
          source_update_note: "来源更新时间：2026-07-03 18:39:21",
          required_atoms: [
            "期间：2026-06",
            "口径：项目结算收入（营收）",
            "金额：936308.25 元",
            "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q2收入明细】",
            "来源更新时间：2026-07-03 18:39:21"
          ]
        }
      };
    }
  });
});

async function withServer(handler, run) {
  const server = http.createServer(async (req, res) => {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      body += chunk;
    });
    req.on("end", async () => {
      try {
        await handler(req, res, body);
      } catch (error) {
        res.statusCode = 500;
        res.end(error.stack || String(error));
      }
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  try {
    await run(`http://127.0.0.1:${address.port}/mcp`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

function writeJSON(res, payload) {
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(payload));
}

async function withFinancePluginHarness(toolCalls, run, options = {}) {
  await withServer(async (req, res, body) => {
    const message = JSON.parse(body || "{}");
    assert.equal(req.headers.authorization, "Bearer test-token");
    if (message.method === "initialize") {
      res.setHeader("Mcp-Session-Id", "finance-test-session");
      writeJSON(res, {
        jsonrpc: "2.0",
        id: message.id,
        result: { serverInfo: { name: "financeqa-mcp" }, capabilities: {} }
      });
      return;
    }

    assert.equal(req.headers["mcp-session-id"], "finance-test-session");
    assert.equal(message.method, "tools/call");
    assert.equal(message.params.name, "finance-query");
    toolCalls.push(message.params);
    const toolPayload = typeof options.toolPayload === "function"
      ? options.toolPayload(message.params?.arguments || {})
      : options.toolPayload;
    writeJSON(res, {
      jsonrpc: "2.0",
      id: message.id,
      result: {
        content: [
          {
            type: "text",
            text: JSON.stringify(toolPayload || { success: true, final_answer: "ok" })
          }
        ]
      }
    });
  }, async (url) => {
    const moduleUrl = `../dist/index.esm.js?test=${Date.now()}-${Math.random()}`;
    const { default: plugin } = await import(moduleUrl);
    const tools = new Map();
    const hooks = new Map();
    plugin.register({
      getPluginConfig() {
        return {
          transport: "remote",
          mcp_url: url,
          mcp_token: "test-token",
          timeout_ms: 5000
        };
      },
      registerTool(tool, options) {
        const name = options?.name || tool.name;
        if (typeof tool === "function") {
          const create = (factoryCtx = {}) => {
            const instance = tool(factoryCtx);
            return {
              execute(toolCallId, rawParams, runtimeCtx) {
                return instance.execute(toolCallId, rawParams, runtimeCtx);
              }
            };
          };
          tools.set(name, {
            execute(toolCallId, rawParams, ctx = {}) {
              return create(ctx).execute(toolCallId, rawParams, ctx);
            },
            create
          });
          return;
        }
        tools.set(name, tool);
      },
      on(name, handler) {
        hooks.set(name, handler);
      }
    });
    await run({ hooks, tools });
  });
}
