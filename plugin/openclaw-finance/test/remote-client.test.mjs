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
        tools.set(options?.name || tool.name, tool);
      },
      on(name, handler) {
        hooks.set(name, handler);
      }
    });
    await run({ hooks, tools });
  });
}
