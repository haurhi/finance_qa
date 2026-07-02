import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import zlib from "node:zlib";
import { spawn, spawnSync, type SpawnOptionsWithoutStdio } from "node:child_process";
import { main } from "../src/index.ts";
import { loadConfig } from "../src/config.ts";
import { generateCases } from "../src/cases.ts";

test("generic command-agent stub example runs through the agent-patrol CLI", async () => {
  const outDir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-command-stub-"));

  const code = await main([
    "run",
    "--config",
    "examples/stub.command-agent.yaml",
    "--suite",
    "smoke",
    "--seed",
    "fixed",
    "--out",
    outDir
  ]);

  assert.equal(code, 0);
  assert.match(fs.readFileSync(path.join(outDir, "summary.md"), "utf8"), /Accuracy: 100\.00%/);
});

test("live OpenClaw SSH runner fails closed unless explicitly enabled", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-live-runner-"));
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "请只回答：AGENT_PATROL_OK", "utf8");

  const result = spawnSync("node", [
    "examples/runners/openclaw_ssh_runner.mjs",
    "--host",
    "clawdbot",
    "--question-file",
    questionFile,
    "--session-id",
    "patrol-test"
  ], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, AGENT_PATROL_LIVE: "" }
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /AGENT_PATROL_LIVE=1/);
});

test("live OpenClaw local runner fails closed unless explicitly enabled", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-live-local-runner-"));
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "请只回答：AGENT_PATROL_OK", "utf8");

  const result = spawnSync("node", [
    "examples/runners/openclaw_local_runner.mjs",
    "--question-file",
    questionFile,
    "--session-id",
    "patrol-test"
  ], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, AGENT_PATROL_LIVE: "" }
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /AGENT_PATROL_LIVE=1/);
});

test("financeqa dry-run wrapper blocks mirror prepare in production mode", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-production-guard-"));
  const envFile = path.join(dir, "financeqa.env");
  fs.writeFileSync(envFile, [
    "AGENT_PATROL_LIVE=1",
    "AGENT_PATROL_ENV=production",
    "AGENT_PATROL_CONFIG=presets/financeqa.yaml",
    "OPENCLAW_AGENT_CMD='echo openclaw'",
    "FINANCEQA_MCP_URL=http://127.0.0.1:3009/mcp",
    "FINANCEQA_MCP_READ_TOKEN=test-token",
    "AGENT_PATROL_PREPARE_CMD=examples/schedules/prepare-financeqa-snapshot-mirror.sh"
  ].join("\n"), "utf8");

  const result = spawnSync("bash", [
    "examples/schedules/run-financeqa-dry-run.sh"
  ], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, AGENT_PATROL_ENV_FILE: envFile }
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr + readDryRunLog(dir), /refusing production dry-run.*prepare-financeqa-snapshot-mirror/);
});

test("financeqa dry-run wrapper prunes old report directories after a successful run", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-retention-"));
  const binDir = path.join(dir, "bin");
  const outDir = path.join(dir, "reports");
  const envFile = path.join(dir, "financeqa.env");
  const oldRun = path.join(outDir, "20260101T000000");
  const freshRun = path.join(outDir, "20260629T120000");
  fs.mkdirSync(binDir, { recursive: true });
  fs.mkdirSync(oldRun, { recursive: true });
  fs.mkdirSync(freshRun, { recursive: true });
  fs.writeFileSync(path.join(oldRun, "summary.json"), "{}", "utf8");
  fs.writeFileSync(path.join(freshRun, "summary.json"), "{}", "utf8");
  const oldDate = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000);
  fs.utimesSync(oldRun, oldDate, oldDate);
  fs.writeFileSync(path.join(binDir, "npm"), `#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$AGENT_PATROL_OUTPUT_DIR/$AGENT_PATROL_RUN_ID"
cat > "$AGENT_PATROL_OUTPUT_DIR/$AGENT_PATROL_RUN_ID/summary.json" <<'JSON'
{"aggregate":{"thresholdPassed":true}}
JSON
exit 0
`, { mode: 0o755 });
  fs.writeFileSync(envFile, [
    "AGENT_PATROL_LIVE=1",
    "AGENT_PATROL_CONFIG=presets/financeqa.yaml",
    "AGENT_PATROL_OUTPUT_DIR=" + outDir,
    "AGENT_PATROL_RUN_ID=20260629T130000",
    "AGENT_PATROL_REPORT_RETENTION_DAYS=1",
    "OPENCLAW_AGENT_CMD='echo openclaw'",
    "FINANCEQA_MCP_URL=http://127.0.0.1:3009/mcp",
    "FINANCEQA_MCP_READ_TOKEN=test-token"
  ].join("\n"), "utf8");

  const result = spawnSync("bash", [
    "examples/schedules/run-financeqa-dry-run.sh"
  ], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: {
      ...process.env,
      AGENT_PATROL_ENV_FILE: envFile,
      PATH: `${binDir}:${process.env.PATH ?? ""}`
    }
  });

  assert.equal(result.status, 0, result.stderr + readDryRunLog(outDir));
  assert.equal(fs.existsSync(oldRun), false);
  assert.equal(fs.existsSync(freshRun), true);
  assert.equal(fs.existsSync(path.join(outDir, "20260629T130000", "summary.json")), true);
});

test("financeqa preset generates varied daily sample pool", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });

  const cases = generateCases(withFinanceqaEntityVariables(config), { suite: "daily", seed: "2026-06-25" });
  const questions = cases.map((item) => item.question);

  assert.equal(cases.length, 14);
  assert.equal(new Set(questions).size, 14);
  assert.equal(questions.some((item) => item.includes("最新月份") || item.includes("最新可见月份")), true);
  assert.equal(questions.some((item) => item.includes("上一个完整自然月月底")), true);
  assert.equal(questions.some((item) => item.includes("应收未收")), true);
  assert.equal(questions.some((item) => item.includes("应付未付") || item.includes("未付款")), true);
  assert.equal(questions.some((item) => item.includes("已开票未回款") || item.includes("已开票未收款")), true);
  assert.equal(questions.some((item) => item.includes("已收票未付款")), true);
  assert.equal(questions.some((item) => item.includes("余额表") || item.includes("账上")), true);
  assert.equal(questions.some((item) => item.includes("银行流水") || item.includes("银行卡上")), true);
  assert.equal(questions.some((item) => item.includes("序时账") || (item.includes("账上") && item.includes("净利润"))), true);
  assert.equal(questions.some((item) => item.includes("差异") || item.includes("银行净流入")), true);
  assert.equal(questions.some((item) => item.includes("客户甲")), true);
  assert.equal(questions.some((item) => item.includes("供应商乙")), true);
  assert.equal(questions.some((item) => item.includes("项目Alpha") || item.includes("合同Beta")), true);
});

test("financeqa preset defines reference-check labels for finance answer comparison", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });

  for (const [name, template] of Object.entries(config.templates ?? {})) {
    const referenceChecks = template.scoring?.referenceChecks as Record<string, unknown> | undefined;
    assert.ok(referenceChecks, `${name} missing referenceChecks`);
    assert.equal(referenceChecks.periods, true, `${name} should compare reference periods`);
    assert.equal(referenceChecks.sources, true, `${name} should compare reference sources`);
    assert.notEqual(referenceChecks.perspectives, true, `${name} should not hard-fail reference boilerplate wording`);
  }
});

test("financeqa preset keeps supplemental payable totals out of hard amount checks", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });

  for (const name of ["finance_project_payable_unpaid", "finance_unpaid_projects"]) {
    const labels = ((config.templates?.[name]?.scoring?.referenceChecks as any)?.amounts?.labels ?? []) as string[];
    assert.deepEqual(labels.includes("项目成本"), false, `${name} should not require supplemental project cost as a primary amount`);
  }
});

test("financeqa preset does not hard-fail named entity style wording", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });

  for (const name of [
    "finance_customer_receivable_unpaid",
    "finance_supplier_payable_unpaid",
    "finance_contract_receivable_unpaid",
    "finance_single_project_payable_unpaid"
  ]) {
    const groups = (config.templates?.[name]?.scoring?.mustContainAny ?? []) as string[][];
    assert.equal(groups.length, 1, `${name} should only hard-check the business metric wording`);
  }
});

test("financeqa preset keeps business anchors in configuration", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });

  for (const [name, template] of Object.entries(config.templates ?? {})) {
    assert.ok(Array.isArray(template.questionAnchors), `${name} missing questionAnchors`);
    assert.ok(template.questionAnchors.length > 0, `${name} questionAnchors should not be empty`);
    assert.ok(Array.isArray(template.scoring?.amountLabelGroups), `${name} missing amountLabelGroups`);
  }
});

test("financeqa preset template questions satisfy their configured anchors", () => {
  const config = loadConfig("presets/financeqa.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_ssh_runner.mjs --host clawdbot --question-file {questionFile} --session-id {sessionId}",
    FINANCEQA_MCP_URL: "http://127.0.0.1/stub"
  });
  const templates = Object.keys(config.templates ?? {});
  const configWithAllCases = {
    ...withFinanceqaEntityVariables(config),
    targets: {
      finance_qa: {
        ...config.targets.finance_qa,
        suites: {
          anchorcheck: { templates }
        }
      }
    }
  };

  const cases = generateCases(configWithAllCases, { suite: "anchorcheck", seed: "anchors" });
  for (const item of cases) {
    for (const group of item.questionAnchors ?? []) {
      assert.equal(
        group.some((anchor) => normalizeAnchorText(item.question).includes(normalizeAnchorText(anchor))),
        true,
        `${item.id} question should contain one anchor from ${group.join("|")}: ${item.question}`
      );
    }
  }
});

test("LLM command question generator rewrites cases through a configurable CLI", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-llm-question-generator-"));
  const inputPath = path.join(dir, "input.json");
  const llmPath = path.join(dir, "fake-llm.mjs");
  fs.writeFileSync(inputPath, JSON.stringify({
    version: 1,
    target: "finance_qa",
    suite: "smoke",
    seed: "fixed",
    cases: [{
      caseId: "finance_qa_finance_project_receivable_unpaid_001",
      template: "finance_project_receivable_unpaid",
      originalQuestion: "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？",
      questionAnchors: [["项目口径", "项目应收"], ["应收未收", "未回款"]],
      scoring: { referenceChecks: { amounts: { labels: ["项目应收", "应收未收"] } } }
    }]
  }), "utf8");
  fs.writeFileSync(llmPath, `
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => { input += chunk; });
process.stdin.on("end", () => {
  if (!input.includes("questionAnchors") || !input.includes("锚点")) {
    console.error("missing question anchor instruction");
    process.exit(3);
  }
  const data = JSON.parse(input.match(/<cases_json>([\\s\\S]+)<\\/cases_json>/)[1]);
  console.log("LLM note before JSON");
  console.log("\`\`\`json");
  console.log(JSON.stringify({
    questions: [{
      caseId: data.cases[0].caseId,
      template: data.cases[0].template,
      question: "老板，从去年10月到上个完整月，项目上还有多少款没收回来？"
    }]
  }));
  console.log("\`\`\`");
});
`, "utf8");

  const result = await spawnNode([
    "examples/question-generators/llm_command_rewriter.mjs",
    "--input", inputPath
  ], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      AGENT_PATROL_LLM_CMD: `node ${llmPath}`
    }
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.equal(payload.source, "llm_command_rewriter");
  assert.deepEqual(payload.questions, [{
    caseId: "finance_qa_finance_project_receivable_unpaid_001",
    template: "finance_project_receivable_unpaid",
    question: "老板，从去年10月到上个完整月，项目上还有多少款没收回来？"
  }]);
});

test("LLM command question generator extracts JSON from agent envelopes", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-llm-question-generator-envelope-"));
  const inputPath = path.join(dir, "input.json");
  const llmPath = path.join(dir, "fake-agent-llm.mjs");
  fs.writeFileSync(inputPath, JSON.stringify({
    version: 1,
    target: "finance_qa",
    suite: "smoke",
    seed: "fixed",
    cases: [{
      caseId: "finance_qa_finance_latest_month_revenue_001",
      template: "finance_latest_month_revenue",
      originalQuestion: "收入表中最新月份的营收是多少？",
      scoring: { referenceChecks: { amounts: { labels: ["项目结算"] } } }
    }]
  }), "utf8");
  fs.writeFileSync(llmPath, `
console.log(JSON.stringify({
  result: {
    final_answer: "\`\`\`json\\n" + JSON.stringify({
      questions: [{
        caseId: "finance_qa_finance_latest_month_revenue_001",
        template: "finance_latest_month_revenue",
        question: "老板，最新可见月份项目收入大概是多少？"
      }]
    }) + "\\n\`\`\`"
  }
}));
`, "utf8");

  const result = await spawnNode([
    "examples/question-generators/llm_command_rewriter.mjs",
    "--input", inputPath
  ], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      AGENT_PATROL_LLM_CMD: `node ${llmPath}`
    }
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.deepEqual(payload.questions, [{
    caseId: "finance_qa_finance_latest_month_revenue_001",
    template: "finance_latest_month_revenue",
    question: "老板，最新可见月份项目收入大概是多少？"
  }]);
});

test("LLM command question generator fails closed on recursive JSON slices", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-llm-question-generator-recursive-json-"));
  const inputPath = path.join(dir, "input.json");
  const llmPath = path.join(dir, "fake-bad-llm.mjs");
  fs.writeFileSync(inputPath, JSON.stringify({
    version: 1,
    target: "finance_qa",
    suite: "smoke",
    seed: "fixed",
    cases: [{
      caseId: "finance_qa_finance_latest_month_revenue_001",
      template: "finance_latest_month_revenue",
      originalQuestion: "收入表中最新月份的营收是多少？"
    }]
  }), "utf8");
  fs.writeFileSync(llmPath, `
process.stdout.write("{not valid json}");
`, "utf8");

  const result = await spawnNode([
    "examples/question-generators/llm_command_rewriter.mjs",
    "--input", inputPath
  ], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      AGENT_PATROL_LLM_CMD: `node ${llmPath}`
    }
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /stdout did not contain valid JSON/);
  assert.doesNotMatch(result.stderr, /Maximum call stack size exceeded/);
});

test("OpenAI-compatible question generator CLI reads stdin and returns model content", async () => {
  const seenRequests: Array<{ authorization?: string; body: Record<string, unknown> }> = [];
  const server = http.createServer((req, res) => {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      body += chunk;
    });
    req.on("end", () => {
      seenRequests.push({
        authorization: req.headers.authorization,
        body: JSON.parse(body)
      });
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({
        choices: [{
          message: {
            content: JSON.stringify({
              questions: [{
                caseId: "case-1",
                template: "finance_latest_month_revenue",
                question: "老板，最新月份收入是多少？"
              }]
            })
          }
        }]
      }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address === "object");

  try {
    const child = spawn("node", ["examples/question-generators/openai_compatible_chat.mjs"], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        AGENT_PATROL_LLM_BASE_URL: `http://127.0.0.1:${address.port}/v1`,
        AGENT_PATROL_LLM_API_KEY: "test-key",
        AGENT_PATROL_LLM_MODEL: "test-model",
        AGENT_PATROL_LLM_RESPONSE_FORMAT: "json_object"
      },
      stdio: ["pipe", "pipe", "pipe"]
    });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.stdin.end("rewrite this question");
    const status = await new Promise<number | null>((resolve) => child.on("close", resolve));

    assert.equal(status, 0, stderr);
    assert.match(stdout, /最新月份收入/);
    assert.equal(seenRequests[0]?.authorization, "Bearer test-key");
    assert.equal(seenRequests[0]?.body.model, "test-model");
    assert.deepEqual(seenRequests[0]?.body.response_format, { type: "json_object" });
    const messages = seenRequests[0]?.body.messages as Array<{ role: string; content: string }>;
    assert.equal(messages[0]?.content, "rewrite this question");
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test("financeqa snapshot reference provider computes project receivable without FinanceQA MCP", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "项目口径看，从2025年10月起到上一个完整自然月月底还有多少应收未收？", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T18:10:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_fund_income: [
        { year_month: "2025-10", settlement_amount: 1000, received_amount: 200, invoice_amount: 500 },
        { year_month: "2026-05", settlement_amount: 300, received_amount: 100, invoice_amount: 150 },
        { year_month: "2026-06", settlement_amount: 999, received_amount: 0, invoice_amount: 0 }
      ],
      fin_file_mappings: [
        {
          table_type: "fund-income",
          period: "2025-Q4",
          file_name: "收入Q4.xlsx",
          source_version_id: "收入Q4.xlsx:hash-q4",
          updated_at: "2026-06-26T18:00:00+08:00"
        },
        {
          table_type: "fund-income",
          period: "2026-Q2",
          file_name: "收入Q2.xlsx",
          source_version_id: "收入Q2.xlsx:hash-q2",
          updated_at: "2026-06-26T18:00:00+08:00"
        }
      ]
    }
  }), "utf8");

  const result = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_receivable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      FINANCEQA_MCP_URL: "http://127.0.0.1:1/must-not-be-called"
    }
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.equal(payload.result.source, "financeqa_snapshot_reference");
  assert.equal(payload.result.structured.metric, "项目应收");
  assert.equal(payload.result.structured.amount, 1000);
  assert.deepEqual(payload.result.structured.period, { from: "2025-10", to: "2026-05" });
  assert.deepEqual(payload.result.structured.source.files, ["收入Q4.xlsx", "收入Q2.xlsx"]);
  assert.equal(payload.result.structured.row_count, 2);
  assert.match(payload.result.final_answer, /2025-10~2026-05/);
  assert.match(payload.result.final_answer, /项目应收 1000\.00 元/);
  assert.equal(payload.result.structured.totals.settlement, 1300);
});

test("financeqa snapshot reference provider reads gzip snapshots and computes latest revenue month", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-gzip-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json.gz");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "收入表中最新月份的营收是多少？", "utf8");
  fs.writeFileSync(snapshotPath, zlib.gzipSync(JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T18:10:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_fund_income: [
        { year_month: "2026-04", settlement_amount: 900, received_amount: 800, invoice_amount: 850 },
        { year_month: "2026-05", settlement_amount: 1200, received_amount: 1000, invoice_amount: 1100 },
        { year_month: "2026-06", settlement_amount: 1300, received_amount: 900, invoice_amount: 950 }
      ],
      fin_file_mappings: [
        {
          table_type: "fund-income",
          period: "2026-Q2",
          file_name: "优集收入、成本计算表 - 上传.xlsx",
          source_version_id: "优集收入、成本计算表 - 上传.xlsx:c34368e51eb0",
          updated_at: "2026-06-26T18:00:00+08:00"
        }
      ]
    }
  })));

  const result = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_latest_month_revenue",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.equal(payload.result.structured.metric, "项目结算");
  assert.equal(payload.result.structured.amount, 1300);
  assert.deepEqual(payload.result.structured.period, { from: "2026-06", to: "2026-06" });
  assert.match(payload.result.final_answer, /2026-06/);
  assert.match(payload.result.final_answer, /项目结算 1300\.00 元/);
  assert.match(payload.result.final_answer, /优集收入、成本计算表 - 上传\.xlsx/);
});

test("financeqa snapshot reference provider nets merged groups against member movements", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-merged-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "项目口径看，从2025年10月起到上一个完整自然月月底还有多少应收未收？", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T18:10:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_contracts: [
        { contract_id: "R1", customer_name: "客户A", contract_content: "收入项目A" },
        { contract_id: "R2", customer_name: "客户A", contract_content: "收入项目B" }
      ],
      fin_fund_income: [
        { contract_id: "R2", year_month: "2026-05", settlement_amount: 0, received_amount: 600, invoice_amount: 0 },
        { contract_id: "R2", year_month: "2026-05", settlement_amount: 0, received_amount: 100, invoice_amount: 0 }
      ],
      fin_fund_income_groups: [
        {
          id: 1,
          customer_name: "客户A",
          year_month: "2026-05",
          source_start_row: 3,
          source_end_row: 4,
          settlement_amount: 1000,
          received_amount: 400,
          invoice_amount: 1000
        }
      ],
      fin_fund_income_group_members: [
        { group_id: 1, contract_id: "R1", source_row_number: 3 },
        { group_id: 1, contract_id: "R2", source_row_number: 4 }
      ]
    }
  }), "utf8");

  const result = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_receivable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.equal(payload.result.structured.amount, 0);
  assert.equal(payload.result.structured.totals.open, 0);
  assert.equal(payload.result.structured.totals.settlement, 1000);
  assert.equal(payload.result.structured.totals.movement, 1100);
});

test("financeqa snapshot reference provider separates project payable from invoiced payable", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-cost-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "项目成本口径看，25年至26年未付款的项目及对应金额有哪些？", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T18:10:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_contracts: [
        { contract_id: "S1", customer_name: "供应商A", contract_content: "成本项目A" },
        { contract_id: "S2", customer_name: "供应商A", contract_content: "成本项目B" }
      ],
      fin_cost_settlements: [
        {
          contract_id: "S1",
          year_month: "2026-05",
          settlement_amount: 1000,
          paid_amount: 300,
          invoice_amount: 900,
          invoice_open_offset_amount: 100
        }
      ],
      fin_cost_settlement_groups: [
        {
          id: 10,
          customer_name: "供应商A",
          year_month: "2026-05",
          settlement_amount: 500,
          paid_amount: 100,
          invoice_amount: 500,
          invoice_open_offset_amount: 50
        }
      ],
      fin_cost_settlement_group_members: [
        { group_id: 10, contract_id: "S2", source_row_number: 7 }
      ]
    }
  }), "utf8");

  const payable = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_payable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });
  assert.equal(payable.status, 0, payable.stderr);
  const payablePayload = JSON.parse(payable.stdout);
  assert.equal(payablePayload.result.structured.metric, "项目应付");
  assert.equal(payablePayload.result.structured.amount, 1100);
  assert.equal(payablePayload.result.structured.totals.invoice_open, 850);

  const invoicedPayable = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_invoiced_payable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });
  assert.equal(invoicedPayable.status, 0, invoicedPayable.stderr);
  const invoicedPayablePayload = JSON.parse(invoicedPayable.stdout);
  assert.equal(invoicedPayablePayload.result.structured.metric, "已收票未付款");
  assert.equal(invoicedPayablePayload.result.structured.amount, 850);

  const unpaidProjects = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_unpaid_projects",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });
  assert.equal(unpaidProjects.status, 0, unpaidProjects.stderr);
  const unpaidPayload = JSON.parse(unpaidProjects.stdout);
  assert.equal(unpaidPayload.result.structured.metric, "项目应付");
  assert.equal(unpaidPayload.result.structured.amount, 1100);
  assert.deepEqual(unpaidPayload.result.structured.items.map((item: { name: string; amount: number }) => [item.name, item.amount]), [
    ["供应商A/成本项目A", 700],
    ["供应商A/成本项目B", 400]
  ]);
  assert.match(unpaidPayload.result.final_answer, /明细前2项/);
  assert.match(unpaidPayload.result.final_answer, /供应商A\/成本项目A 700\.00 元/);
});

test("financeqa snapshot reference cost range follows the business cutoff month", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-cost-cutoff-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "从2025年10月起到上一个完整自然月月底，所有项目的应付未付是多少？", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-07-01T13:09:47+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub"
    },
    tables: {
      fin_fund_income: [
        { year_month: "2026-06", settlement_amount: 1, received_amount: 0, invoice_amount: 0 }
      ],
      fin_cost_settlements: [
        {
          contract_id: "S1",
          year_month: "2026-05",
          settlement_amount: 1000,
          paid_amount: 300,
          invoice_amount: 900,
          invoice_open_offset_amount: 100
        }
      ]
    }
  }), "utf8");

  const result = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_payable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--as-of-date", "2026-07-01"
  ], {
    cwd: process.cwd(),
    env: process.env
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.deepEqual(payload.result.structured.period, { from: "2025-10", to: "2026-06" });
  assert.match(payload.result.final_answer, /2025-10~2026-06/);
  assert.equal(payload.result.structured.amount, 700);
});

test("financeqa snapshot reference provider defaults as-of date to Asia Shanghai", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-snapshot-local-date-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "项目口径看，从2025年10月起到上一个完整自然月月底还有多少应收未收？", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-06-01T00:30:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_fund_income: [
        { year_month: "2026-04", settlement_amount: 100, received_amount: 0, invoice_amount: 0 },
        { year_month: "2026-05", settlement_amount: 200, received_amount: 0, invoice_amount: 0 }
      ]
    }
  }), "utf8");

  const result = await spawnNode([
    "examples/golden/financeqa_snapshot_reference.mjs",
    "--template", "finance_project_receivable_unpaid",
    "--question-file", questionFile,
    "--snapshot", snapshotPath,
    "--now-epoch-ms", String(Date.parse("2026-05-31T16:30:00.000Z"))
  ], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      TZ: "UTC"
    }
  }, { timeoutMs: 5_000 });

  assert.equal(result.status, 0, result.stderr);
  const payload = JSON.parse(result.stdout);
  assert.equal(payload.result.audit.as_of_date, "2026-06-01");
  assert.deepEqual(payload.result.structured.period, { from: "2025-10", to: "2026-05" });
  assert.equal(payload.result.structured.amount, 300);
});

test("financeqa snapshot reference provider computes accounting and cashflow coverage templates", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-accounting-snapshot-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "老板问法由模板决定", "utf8");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-07-02T09:10:00+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub"
    },
    tables: {
      fin_bank_statement: [
        { company: "DefaultCompany", transaction_date: "2026-06-03", credit_amount: 3000, debit_amount: 400, counterparty_name: "客户A", summary: "回款" },
        { company: "DefaultCompany", transaction_date: "2026-06-20", credit_amount: 500, debit_amount: 800, counterparty_name: "供应商A", summary: "付款" }
      ],
      fin_balance_detail: [
        { company: "DefaultCompany", year: 2026, period: "2026-06", account_code: "1122", account_name: "应收账款", closing_debit: 1200, closing_credit: 100 },
        { company: "DefaultCompany", year: 2026, period: "2026-06", account_code: "112201", account_name: "应收账款-客户A", closing_debit: 1200, closing_credit: 100 },
        { company: "DefaultCompany", year: 2026, period: "2026-06", account_code: "2202", account_name: "应付账款", closing_debit: 50, closing_credit: 950 },
        { company: "DefaultCompany", year: 2026, period: "2026-06", account_code: "220201", account_name: "应付账款-供应商A", closing_debit: 50, closing_credit: 950 },
        { company: "DefaultCompany", year: 2026, period: "2026-06", account_code: "2241", account_name: "其他应付款", closing_debit: 20, closing_credit: 320 }
      ],
      fin_journal: [
        { company: "DefaultCompany", period: "2026-06", voucher_date: "2026-06-10", account_code: "6001", account_name: "主营业务收入", summary: "确认收入", direction: "贷", amount: 2500, debit_amount: 0, credit_amount: 2500 },
        { company: "DefaultCompany", period: "2026-06", voucher_date: "2026-06-11", account_code: "6401", account_name: "主营业务成本", summary: "结转成本", direction: "借", amount: 700, debit_amount: 700, credit_amount: 0 },
        { company: "DefaultCompany", period: "2026-06", voucher_date: "2026-06-12", account_code: "6602", account_name: "管理费用", summary: "管理费用", direction: "借", amount: 300, debit_amount: 300, credit_amount: 0 },
        { company: "DefaultCompany", period: "2026-06", voucher_date: "2026-06-30", account_code: "4103", account_name: "本年利润", summary: "结转本年利润", direction: "贷", amount: 1200, debit_amount: 0, credit_amount: 1200 }
      ],
      fin_file_mappings: [
        { table_type: "bank-statement", period: "2026-Q2", file_name: "银行流水Q2.xlsx", updated_at: "2026-07-02T08:00:00+08:00" },
        { table_type: "balance-detail", period: "2026-Q2", file_name: "余额表Q2.xlsx", updated_at: "2026-07-02T08:10:00+08:00" },
        { table_type: "journal", period: "2026-Q2", file_name: "序时账Q2.xlsx", updated_at: "2026-07-02T08:20:00+08:00" }
      ]
    }
  }), "utf8");

  const cases = [
    {
      template: "finance_accounting_arap_balance",
      metric: "账上应付端合计",
      amount: 1200,
      required: [/应收账款 1100\.00 元/, /应付账款 900\.00 元/, /其他应付款 300\.00 元/, /余额表Q2\.xlsx/]
    },
    {
      template: "finance_bank_cashflow",
      metric: "净现金流",
      amount: 2300,
      required: [/现金流入 3500\.00 元/, /现金流出 1200\.00 元/, /净现金流 2300\.00 元/, /银行流水Q2\.xlsx/]
    },
    {
      template: "finance_journal_profit",
      metric: "账上净利润",
      amount: 1200,
      required: [/账上确认收入 2500\.00 元/, /账上成本及费用 1000\.00 元/, /账上净利润 1200\.00 元/, /序时账口径/, /默认未剔税/, /序时账Q2\.xlsx/]
    },
    {
      template: "finance_profit_cash_reconciliation",
      metric: "现金账上差异",
      amount: 1100,
      required: [/银行净流入 2300\.00 元/, /账上净利润 1200\.00 元/, /差异金额 1100\.00 元/, /口径不同/]
    }
  ];

  for (const item of cases) {
    const result = await spawnNode([
      "examples/golden/financeqa_snapshot_reference.mjs",
      "--template", item.template,
      "--question-file", questionFile,
      "--snapshot", snapshotPath,
      "--as-of-date", "2026-07-02"
    ], {
      cwd: process.cwd(),
      env: process.env
    }, { timeoutMs: 5_000 });

    assert.equal(result.status, 0, `${item.template}: ${result.stderr}`);
    const payload = JSON.parse(result.stdout);
    assert.equal(payload.result.source, "financeqa_snapshot_reference");
    assert.equal(payload.result.structured.metric, item.metric, item.template);
    assert.equal(payload.result.structured.amount, item.amount, item.template);
    assert.deepEqual(payload.result.structured.period, { from: "2026-06", to: "2026-06" }, item.template);
    for (const pattern of item.required) {
      assert.match(payload.result.final_answer, pattern, item.template);
    }
  }
});

test("financeqa snapshot reference provider filters named customer supplier project and contract templates", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-entity-snapshot-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json");
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(snapshotPath, JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T21:54:44+08:00"
    },
    tables: {
      fin_contracts: [
        { contract_id: "R1", customer_name: "客户甲", contract_content: "项目Alpha" },
        { contract_id: "R2", customer_name: "客户乙", contract_content: "项目Other" },
        { contract_id: "S1", customer_name: "供应商乙", contract_content: "合同Beta" },
        { contract_id: "S2", customer_name: "供应商丙", contract_content: "合同Other" }
      ],
      fin_fund_income: [
        { contract_id: "R1", customer_name: "客户甲", year_month: "2026-05", settlement_amount: 1000, received_amount: 300, invoice_amount: 800 },
        { contract_id: "R2", customer_name: "客户乙", year_month: "2026-05", settlement_amount: 500, received_amount: 300, invoice_amount: 500 }
      ],
      fin_cost_settlements: [
        { contract_id: "S1", customer_name: "供应商乙", year_month: "2026-05", settlement_amount: 900, paid_amount: 100, invoice_amount: 700 },
        { contract_id: "S2", customer_name: "供应商丙", year_month: "2026-05", settlement_amount: 400, paid_amount: 300, invoice_amount: 400 }
      ]
    }
  }), "utf8");

  const cases = [
    {
      template: "finance_customer_receivable_unpaid",
      question: "客户甲 从2025年10月起到上一个完整自然月月底应收未收是多少？",
      metric: "客户项目应收",
      amount: 700,
      entity: "客户甲"
    },
    {
      template: "finance_supplier_payable_unpaid",
      question: "供应商乙 从2025年10月起到上一个完整自然月月底应付未付是多少？",
      metric: "供应商项目应付",
      amount: 800,
      entity: "供应商乙"
    },
    {
      template: "finance_contract_receivable_unpaid",
      question: "合同关键词 项目Alpha 从2025年10月起应收未收是多少？",
      metric: "合同项目应收",
      amount: 700,
      entity: "项目Alpha"
    },
    {
      template: "finance_single_project_payable_unpaid",
      question: "项目 合同Beta 从2025年10月起未付款是多少？",
      metric: "单项目应付",
      amount: 800,
      entity: "合同Beta"
    }
  ];

  for (const item of cases) {
    fs.writeFileSync(questionFile, item.question, "utf8");
    const result = spawnSync("node", [
      "examples/golden/financeqa_snapshot_reference.mjs",
      "--template", item.template,
      "--question-file", questionFile,
      "--snapshot", snapshotPath,
      "--as-of-date", "2026-06-20"
    ], {
      cwd: process.cwd(),
      encoding: "utf8"
    });

    assert.equal(result.status, 0, `${item.template}: ${result.stderr}`);
    const payload = JSON.parse(result.stdout);
    assert.equal(payload.result.structured.metric, item.metric, item.template);
    assert.equal(payload.result.structured.amount, item.amount, item.template);
    assert.equal(payload.result.structured.entity.value, item.entity, item.template);
    assert.match(payload.result.final_answer, new RegExp(item.entity), item.template);
    assert.doesNotMatch(payload.result.final_answer, /客户乙|供应商丙|Other/, item.template);
  }
});

test("financeqa canonical golden runner uses template-derived query instead of raw question", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-golden-"));
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "老板随口问：上个月项目还有多少钱没收？", "utf8");
  const seenBodies: string[] = [];
  const server = http.createServer((req, res) => {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      body += chunk;
    });
    req.on("end", () => {
      seenBodies.push(body);
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({
        jsonrpc: "2.0",
        id: "golden",
        result: {
          content: [{
            type: "text",
            text: JSON.stringify({
              final_answer: "2025-10~2026-05 项目应收 800000.00 元。来源：《fin-revenue-0601.xlsx》",
              data: {
                period_from: "2025-10",
                period_to: "2026-05",
                metrics: { "应收": 800000 },
                contract_summary: { receivable_amount: 800000 },
                source_note: "来源：《fin-revenue-0601.xlsx》"
              }
            })
          }]
        }
      }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address === "object");

  try {
    const result = await spawnNode([
      "examples/golden/financeqa_canonical_golden.mjs",
      "--template", "finance_project_receivable_unpaid",
      "--question-file", questionFile,
      "--as-of-date", "2026-06-26"
    ], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        FINANCEQA_MCP_URL: `http://127.0.0.1:${address.port}/mcp`,
        FINANCEQA_MCP_READ_TOKEN: "test-token"
      }
    }, { timeoutMs: 5_000 });

    assert.equal(result.status, 0, result.stderr);
    const payload = JSON.parse(result.stdout);
    assert.equal(payload.result.source, "financeqa_canonical_golden");
    assert.match(payload.result.final_answer, /项目应收 800000\.00 元/);
    assert.equal(payload.result.structured.metric, "项目应收");
    assert.equal(payload.result.structured.amount, 800000);
    assert.equal(payload.result.structured.period.from, "2025-10");
    assert.equal(payload.result.structured.period.to, "2026-05");

    const request = JSON.parse(seenBodies[0]);
    assert.equal(request.params.name, "finance-query");
    assert.equal(request.params.arguments.query, "2025年10月至2026年5月，所有项目的应收未收是多少？");
    assert.doesNotMatch(request.params.arguments.query, /老板随口问/);
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test("financeqa canonical golden runner covers every financeqa preset template", async () => {
  const cases = [
    {
      template: "finance_latest_month_revenue",
      expectedQuery: "收入表中最新月份的营收是多少？",
      metric: "项目结算",
      amount: 100000,
      period: { from: "2026-05", to: "2026-05" }
    },
    {
      template: "finance_project_receivable_unpaid",
      expectedQuery: "2025年10月至2026年5月，所有项目的应收未收是多少？",
      metric: "项目应收",
      amount: 200000,
      period: { from: "2025-10", to: "2026-05" }
    },
    {
      template: "finance_project_invoiced_receivable_unpaid",
      expectedQuery: "2025年10月至2026年5月，所有项目已开票未回款是多少？",
      metric: "已开票未回款",
      amount: 300000,
      period: { from: "2025-10", to: "2026-05" }
    },
    {
      template: "finance_project_payable_unpaid",
      expectedQuery: "2025年10月至2026年5月，所有项目的应付未付是多少？",
      metric: "项目应付",
      amount: 400000,
      period: { from: "2025-10", to: "2026-05" }
    },
    {
      template: "finance_project_invoiced_payable_unpaid",
      expectedQuery: "2025年10月至2026年5月，所有项目已收票未付款是多少？",
      metric: "已收票未付款",
      amount: 500000,
      period: { from: "2025-10", to: "2026-05" }
    },
    {
      template: "finance_unpaid_projects",
      expectedQuery: "2025年10月至2026年5月，按项目列出应付未付金额。",
      metric: "项目应付",
      amount: 600000,
      period: { from: "2025-10", to: "2026-05" }
    }
  ];

  for (const item of cases) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-golden-template-"));
    const questionFile = path.join(dir, "question.txt");
    fs.writeFileSync(questionFile, `老板问法不参与金标语义：${item.template}`, "utf8");
    const seenBodies: string[] = [];
    const server = http.createServer((req, res) => {
      let body = "";
      req.setEncoding("utf8");
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        seenBodies.push(body);
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({
          jsonrpc: "2.0",
          id: "golden",
          result: {
            content: [{
              type: "text",
              text: JSON.stringify({
                final_answer: `${item.period.from}~${item.period.to} ${item.metric} ${item.amount.toFixed(2)} 元。来源：《golden-fixture.xlsx》`,
                data: {
                  period_from: item.period.from,
                  period_to: item.period.to,
                  metrics: { [item.metric]: item.amount },
                  source_note: "来源：《golden-fixture.xlsx》"
                }
              })
            }]
          }
        }));
      });
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    assert.ok(address && typeof address === "object");

    try {
      const result = await spawnNode([
        "examples/golden/financeqa_canonical_golden.mjs",
        "--template", item.template,
        "--question-file", questionFile,
        "--as-of-date", "2026-06-26"
      ], {
        cwd: process.cwd(),
        env: {
          ...process.env,
          FINANCEQA_MCP_URL: `http://127.0.0.1:${address.port}/mcp`,
          FINANCEQA_MCP_READ_TOKEN: "test-token"
        }
      }, { timeoutMs: 5_000 });

      assert.equal(result.status, 0, `${item.template}: ${result.stderr}`);
      const payload = JSON.parse(result.stdout);
      assert.equal(payload.result.structured.metric, item.metric, item.template);
      assert.equal(payload.result.structured.amount, item.amount, item.template);
      assert.deepEqual(payload.result.structured.period, item.period, item.template);

      const request = JSON.parse(seenBodies[0]);
      assert.equal(request.params.name, "finance-query", item.template);
      assert.equal(request.params.arguments.query, item.expectedQuery, item.template);
      assert.doesNotMatch(request.params.arguments.query, /老板问法不参与金标语义/);
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
      fs.rmSync(dir, { recursive: true, force: true });
    }
  }
});

test("financeqa canonical golden runner fails closed for unsupported templates", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-golden-unsupported-"));
  const questionFile = path.join(dir, "question.txt");
  fs.writeFileSync(questionFile, "未知问题", "utf8");

  const result = spawnSync("node", [
    "examples/golden/financeqa_canonical_golden.mjs",
    "--template", "unknown_template",
    "--question-file", questionFile,
    "--as-of-date", "2026-06-26"
  ], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: {
      ...process.env,
      FINANCEQA_MCP_URL: "http://127.0.0.1:1/mcp",
      FINANCEQA_MCP_READ_TOKEN: "test-token"
    }
  });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unsupported FinanceQA golden template/);
});

test("spawnNode helper kills hung children", async () => {
  const result = await spawnNode(["-e", "setInterval(() => {}, 1000)"], {
    cwd: process.cwd()
  }, { timeoutMs: 50 });

  assert.equal(result.timedOut, true);
  assert.match(result.stderr, /timed out after 50ms/);
});

test("financeqa low-frequency dry-run schedule examples only write local reports", () => {
  const defaultFiles = [
    "examples/cleanup/claude-session-cleanup.sh",
    "examples/cleanup/hermes-json-cleanup.sh",
    "examples/cleanup/openclaw-jsonl-cleanup.sh",
    "examples/cleanup/run-agent-cleanups.sh",
    "examples/schedules/README.md",
    "examples/schedules/financeqa-daily.env.example",
    "examples/schedules/financeqa-daily.cron.example",
    "examples/schedules/financeqa-daily.service",
    "examples/schedules/financeqa-daily.timer",
    "examples/schedules/prepare-financeqa-snapshot-mirror.sh",
    "examples/schedules/run-financeqa-dry-run.sh"
  ];
  const productionFiles = [
    "examples/schedules/financeqa-production-hourly.env.example",
    "examples/schedules/financeqa-production-hourly.service",
    "examples/schedules/financeqa-production-hourly.timer",
    "presets/financeqa-production.yaml"
  ];
  const files = [...defaultFiles, ...productionFiles];
  const contents = files.map((file) => fs.readFileSync(file, "utf8")).join("\n");
  const nonProductionScheduleContents = [
    "examples/schedules/financeqa-daily.env.example",
    "examples/schedules/financeqa-daily.cron.example",
    "examples/schedules/financeqa-daily.service",
    "examples/schedules/financeqa-daily.timer"
  ].map((file) => fs.readFileSync(file, "utf8")).join("\n");
  const productionContents = productionFiles.map((file) => fs.readFileSync(file, "utf8")).join("\n");
  const scriptMode = fs.statSync("examples/schedules/run-financeqa-dry-run.sh").mode;

  assert.match(contents, /presets\/financeqa\.yaml/);
  assert.match(contents, /AGENT_PATROL_CONFIG=presets\/financeqa\.yaml/);
  assert.match(contents, /--config "\$CONFIG"/);
  assert.match(contents, /--suite "\$\{AGENT_PATROL_SUITE:-smoke\}"/);
  assert.match(contents, /tmp\/financeqa-dry-run/);
  assert.match(contents, /AGENT_PATROL_LIVE=1/);
  assert.match(contents, /uuidgen|\/proc\/sys\/kernel\/random\/uuid/);
  assert.doesNotMatch(contents, /date \+%F-%H/);
  assert.match(contents, /OPENCLAW_AGENT_CMD="/);
  assert.match(contents, /FINANCEQA_MCP_READ_TOKEN_FILE/);
  assert.match(contents, /FINANCEQA_GOLDEN_CMD/);
  assert.match(contents, /financeqa_canonical_golden\.mjs/);
  assert.match(contents, /FINANCEQA_REFERENCE_SNAPSHOT/);
  assert.match(contents, /FINANCEQA_SQLITE_MIRROR_OUTPUT/);
  assert.match(contents, /financeqa_snapshot_reference\.mjs/);
  assert.match(contents, /financeqa_snapshot_to_sqlite\.mjs/);
  assert.match(contents, /flock/);
  assert.match(contents, /AGENT_PATROL_PREPARE_CMD/);
  assert.match(contents, /prepare-financeqa-snapshot-mirror\.sh/);
  assert.match(contents, /AGENT_PATROL_CLEANUP_CMD/);
  assert.match(contents, /AGENT_PATROL_CLEANUP_KINDS/);
  assert.match(contents, /AGENT_PATROL_SESSION_RETENTION_DAYS/);
  assert.match(contents, /AGENT_PATROL_LLM_RESPONSE_FORMAT=json_object/);
  assert.match(contents, /openclaw-jsonl-cleanup\.sh/);
  assert.match(contents, /hermes-json-cleanup\.sh/);
  assert.match(contents, /claude-session-cleanup\.sh/);
  assert.match(contents, /patrol-\*\.jsonl/);
  assert.match(contents, /patrol-\*\.json/);
  assert.match(contents, /09,17/);
  assert.doesNotMatch(contents, /09,13,18/);
  assert.match(contents, /openclaw-finance/);
  assert.match(contents, /non-production|非生产|dry-run/i);
  assert.doesNotMatch(contents, /--deliver/);
  assert.doesNotMatch(nonProductionScheduleContents, /financeqa-production-hourly/);
  assert.match(productionContents, /AGENT_PATROL_ENV=production/);
  assert.match(productionContents, /financeqa-production\.yaml/);
  assert.match(productionContents, /OnCalendar=\*-\*-\* \*:07:00/);
  assert.match(productionContents, /AGENT_PATROL_REFERENCE_EXPORT_CMD/);
  assert.match(productionContents, /AGENT_PATROL_REPORT_RETENTION_DAYS/);
  assert.doesNotMatch(productionContents, /AGENT_PATROL_PREPARE_CMD=.*prepare-financeqa-snapshot-mirror/);
  for (const serviceFile of [
    "examples/schedules/financeqa-daily.service",
    "examples/schedules/financeqa-production-hourly.service"
  ]) {
    const service = fs.readFileSync(serviceFile, "utf8");
    assert.match(service, /Environment=AGENT_PATROL_ENV_FILE=\/opt\/finance_qa\/agent-patrol\/examples\/schedules\/.*\.env/);
    assert.doesNotMatch(service, /EnvironmentFile=/);
  }
  assert.notEqual(scriptMode & 0o111, 0);
});

test("financeqa dry-run wrapper treats generated threshold-failed reports as schedule success", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-dry-run-wrapper-"));
  const binDir = path.join(dir, "bin");
  const rootDir = path.join(dir, "root");
  const envFile = path.join(dir, "financeqa-daily.env");
  const npmStub = path.join(binDir, "npm");
  fs.mkdirSync(binDir, { recursive: true });
  fs.mkdirSync(rootDir, { recursive: true });
  fs.writeFileSync(npmStub, `#!/usr/bin/env bash
set -euo pipefail
out=""
while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == "--out" ]]; then
    out="$2"
    shift 2
  else
    shift
  fi
done
mkdir -p "$out"
cat > "$out/summary.json" <<'JSON'
{"aggregate":{"total":3,"passed":0,"accuracy":0,"thresholdPassed":false}}
JSON
cat > "$out/summary.md" <<'MD'
# Agent Patrol Summary

Accuracy: 0.00%
MD
exit 1
`, "utf8");
  fs.chmodSync(npmStub, 0o755);
  fs.writeFileSync(envFile, `
AGENT_PATROL_LIVE=1
AGENT_PATROL_ROOT=${rootDir}
AGENT_PATROL_OUTPUT_DIR=tmp/financeqa-dry-run
AGENT_PATROL_RUN_ID=threshold-failed-report
AGENT_PATROL_CONFIG=patrol.yaml
AGENT_PATROL_SUITE=smoke
AGENT_PATROL_CLEANUP_SESSIONS=0
OPENCLAW_AGENT_CMD=stub-openclaw
FINANCEQA_MCP_URL=http://127.0.0.1:1/mcp
FINANCEQA_MCP_READ_TOKEN=test-token
`, "utf8");

  const result = spawnSync("bash", ["examples/schedules/run-financeqa-dry-run.sh"], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${binDir}${path.delimiter}${process.env.PATH ?? ""}`,
      AGENT_PATROL_ENV_FILE: envFile
    }
  });

  assert.equal(result.status, 0, result.stderr);
  assert.equal(fs.existsSync(path.join(rootDir, "tmp/financeqa-dry-run/threshold-failed-report/summary.json")), true);
  const log = fs.readFileSync(path.join(rootDir, "tmp/financeqa-dry-run/dry-run.log"), "utf8");
  assert.match(log, /report_status=generated/);
  assert.match(log, /business_status=threshold_failed/);
});

test("financeqa snapshot export example is read-only and table-whitelisted", () => {
  const contents = fs.readFileSync("examples/golden/export_financeqa_snapshot.sh", "utf8");

  assert.match(contents, /psql/);
  assert.match(contents, /gzip/);
  assert.match(contents, /FINANCEQA_SQLITE_MIRROR_OUTPUT/);
  assert.match(contents, /FINANCEQA_CASE_VARIABLES_OUTPUT/);
  assert.match(contents, /financeqa_snapshot_case_variables\.mjs/);
  assert.match(contents, /financeqa_snapshot_to_sqlite\.mjs/);
  assert.match(contents, /fin_contracts/);
  assert.match(contents, /fin_fund_income/);
  assert.match(contents, /fin_fund_income_groups/);
  assert.match(contents, /fin_fund_income_group_members/);
  assert.match(contents, /fin_cost_settlements/);
  assert.match(contents, /fin_cost_settlement_groups/);
  assert.match(contents, /fin_cost_settlement_group_members/);
  assert.match(contents, /fin_file_mappings/);
  assert.match(contents, /fin_bank_statement/);
  assert.match(contents, /fin_balance_detail/);
  assert.match(contents, /fin_balance_sheet/);
  assert.match(contents, /fin_journal/);
  assert.doesNotMatch(contents, /\bINSERT\b|\bUPDATE\b|\bDELETE\b|\bDROP\b|\bTRUNCATE\b/i);
});

test("financeqa snapshot case-variable export selects nonzero named entity coverage", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-case-vars-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json.gz");
  const outputPath = path.join(dir, "case-variables.json");

  fs.writeFileSync(snapshotPath, zlib.gzipSync(JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T21:54:44+08:00"
    },
    tables: {
      fin_contracts: [
        { contract_id: "R1", customer_name: "客户甲", contract_content: "项目Alpha" },
        { contract_id: "R0", customer_name: "客户零", contract_content: "零金额项目" },
        { contract_id: "S1", customer_name: "供应商乙", contract_content: "合同Beta" },
        { contract_id: "S0", customer_name: "供应商零", contract_content: "零付款合同" }
      ],
      fin_fund_income: [
        { contract_id: "R1", customer_name: "客户甲", year_month: "2026-05", settlement_amount: 1000, received_amount: 300, invoice_amount: 800 },
        { contract_id: "R0", customer_name: "客户零", year_month: "2026-05", settlement_amount: 500, received_amount: 500, invoice_amount: 500 }
      ],
      fin_cost_settlements: [
        { contract_id: "S1", customer_name: "供应商乙", year_month: "2026-05", settlement_amount: 900, paid_amount: 100, invoice_amount: 700 },
        { contract_id: "S0", customer_name: "供应商零", year_month: "2026-05", settlement_amount: 400, paid_amount: 400, invoice_amount: 400 }
      ]
    }
  })));

  const result = spawnSync("node", [
    "examples/golden/financeqa_snapshot_case_variables.mjs",
    "--snapshot", snapshotPath,
    "--output", outputPath
  ], {
    cwd: process.cwd(),
    encoding: "utf8"
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /wrote FinanceQA case variables/);
  const payload = JSON.parse(fs.readFileSync(outputPath, "utf8"));
  assert.deepEqual(payload.templates.finance_customer_receivable_unpaid.customer_name, ["客户甲"]);
  assert.deepEqual(payload.templates.finance_supplier_payable_unpaid.supplier_name, ["供应商乙"]);
  assert.deepEqual(payload.templates.finance_contract_receivable_unpaid.contract_name, ["项目Alpha"]);
  assert.deepEqual(payload.templates.finance_single_project_payable_unpaid.project_name, ["合同Beta"]);
  assert.doesNotMatch(JSON.stringify(payload), /客户零|供应商零|零金额/);
});

test("financeqa snapshot to sqlite mirror builds actual-service data from the same snapshot", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-financeqa-sqlite-mirror-"));
  const snapshotPath = path.join(dir, "financeqa-snapshot.json.gz");
  const sqlitePath = path.join(dir, "financeqa_patrol.sqlite");

  fs.writeFileSync(snapshotPath, zlib.gzipSync(JSON.stringify({
    metadata: {
      generated_at: "2026-06-26T21:54:44+08:00",
      source_database: "bossagent_app",
      source_schema: "tenant_uhub_etl_shadow"
    },
    tables: {
      fin_contracts: [
        {
          contract_id: "C001",
          customer_name: "客户A",
          contract_content: "项目A",
          contract_start_date: "2025-10-01",
          contract_end_date: "2026-12-31",
          settlement_cycle: "月度",
          created_at: "2026-06-26T20:00:00+08:00",
          updated_at: "2026-06-26T20:29:51+08:00"
        }
      ],
      fin_fund_income: [
        {
          id: 7,
          contract_id: "C001",
          year_month: "2026-05",
          source_report_type: "contract_mixed_finance",
          source_sheet_name: "26年Q2收入明细",
          quantity: "1",
          settlement_amount: 1851758.61,
          received_amount: 850926.32,
          is_invoiced: "是",
          invoice_amount: 859782.66,
          remarks: "巡检测试",
          invoice_open_offset_amount: 0,
          invoice_open_offset_reason: "",
          contract_start_date: "2025-10-01",
          contract_end_date: "2026-12-31",
          settlement_cycle: "月度",
          settlement_unit_price: "1851758.61",
          source_cell_notes: "{}",
          created_at: "2026-06-26T20:00:00+08:00",
          updated_at: "2026-06-26T20:29:51+08:00"
        }
      ],
      fin_cost_settlements: [
        {
          id: 9,
          contract_id: "C001",
          year_month: "2026-05",
          source_report_type: "contract_mixed_finance",
          source_sheet_name: "成本-月度结算",
          quantity: "1",
          settlement_amount: 1000,
          is_invoiced: "是",
          invoice_amount: 900,
          paid_amount: 300,
          invoice_open_offset_amount: 100,
          account_code: "6401",
          created_at: "2026-06-26T20:00:00+08:00",
          updated_at: "2026-06-26T20:29:51+08:00"
        }
      ],
      fin_bank_statement: [
        {
          company: "DefaultCompany",
          transaction_date: "2026-05-02",
          credit_amount: 2000,
          debit_amount: 300,
          counterparty_name: "客户A",
          summary: "回款"
        }
      ],
      fin_balance_detail: [
        {
          company: "DefaultCompany",
          year: 2026,
          period: "2026-05",
          account_code: "1122",
          account_name: "应收账款",
          opening_debit: 1000,
          opening_credit: 0,
          current_debit: 200,
          current_credit: 50,
          closing_debit: 1150,
          closing_credit: 0
        }
      ],
      fin_balance_sheet: [
        {
          company: "DefaultCompany",
          period: "2026-05",
          account_code: "1002",
          account_name: "银行存款",
          opening_balance: 1000,
          closing_balance: 2700
        }
      ],
      fin_journal: [
        {
          company: "DefaultCompany",
          period: "2026-05",
          voucher_date: "2026-05-31",
          voucher_no: "记-001",
          account_code: "6001",
          account_name: "主营业务收入",
          summary: "确认收入",
          direction: "贷",
          amount: 1800,
          debit_amount: 0,
          credit_amount: 1800,
          counterparty: "客户A"
        }
      ],
      fin_file_mappings: [
        {
          id: 3,
          table_type: "fund-income",
          period: "2026-Q2",
          company: "DefaultCompany",
          storage_key: "finance/优集收入、成本计算表 - 上传.xlsx",
          file_name: "优集收入、成本计算表 - 上传.xlsx",
          description: "2026-Q2资金收入表",
          file_size: 71247,
          source_file_hash: "hash-latest",
          source_version_id: "优集收入、成本计算表 - 上传.xlsx:hash-latest",
          created_at: "2026-06-26T20:00:00+08:00",
          updated_at: "2026-06-26T20:29:51+08:00"
        }
      ]
    }
  })));

  const result = spawnSync("node", [
    "examples/golden/financeqa_snapshot_to_sqlite.mjs",
    "--snapshot", snapshotPath,
    "--output", sqlitePath
  ], {
    cwd: process.cwd(),
    encoding: "utf8"
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /wrote FinanceQA SQLite mirror/);

  const query = spawnSync("sqlite3", [
    "-json",
    sqlitePath,
    `
    SELECT
      (SELECT COUNT(*) FROM fin_contracts) AS contracts,
      (SELECT ROUND(SUM(settlement_amount), 2) FROM fin_fund_income) AS revenue,
      (SELECT ROUND(SUM(invoice_amount - paid_amount - COALESCE(invoice_open_offset_amount, 0)), 2) FROM fin_cost_settlements) AS invoice_open,
      (SELECT ROUND(SUM(credit_amount - debit_amount), 2) FROM bank_statement) AS bank_net,
      (SELECT ROUND(SUM(closing_debit - closing_credit), 2) FROM balance_detail) AS ar_balance,
      (SELECT ROUND(SUM(credit_amount), 2) FROM journal WHERE account_code LIKE '6001%') AS journal_revenue,
      (SELECT GROUP_CONCAT(file_name, '|') FROM fin_file_mappings) AS files;
    `
  ], { encoding: "utf8" });

  assert.equal(query.status, 0, query.stderr);
  const rows = JSON.parse(query.stdout);
  assert.deepEqual(rows, [{
    contracts: 1,
    revenue: 1851758.61,
    invoice_open: 500,
    bank_net: 1700,
    ar_balance: 1150,
    journal_revenue: 1800,
    files: "优集收入、成本计算表 - 上传.xlsx"
  }]);

  const schemaQuery = spawnSync("sqlite3", [
    "-json",
    sqlitePath,
    `
    SELECT name FROM pragma_table_info('dimensions') WHERE name IN ('code', 'type', 'is_active')
    UNION ALL
    SELECT name FROM pragma_table_info('dimension_members') WHERE name IN ('dimension_id', 'code', 'parent_id')
    UNION ALL
    SELECT name FROM pragma_table_info('mapping_rules') WHERE name IN ('company', 'dimension_code', 'member_code');
    `
  ], { encoding: "utf8" });

  assert.equal(schemaQuery.status, 0, schemaQuery.stderr);
  assert.deepEqual(
    JSON.parse(schemaQuery.stdout).map((row: { name: string }) => row.name).sort(),
    ["code", "code", "company", "dimension_code", "dimension_id", "is_active", "member_code", "parent_id", "type"]
  );

  const commentSchemaQuery = spawnSync("sqlite3", [
    "-json",
    sqlitePath,
    `
    SELECT name FROM pragma_table_info('meta_table_comments') WHERE name IN ('table_name', 'comment', 'updated_at')
    UNION ALL
    SELECT name FROM pragma_table_info('meta_column_comments') WHERE name IN ('table_name', 'column_name', 'comment', 'updated_at');
    `
  ], { encoding: "utf8" });

  assert.equal(commentSchemaQuery.status, 0, commentSchemaQuery.stderr);
  assert.deepEqual(
    JSON.parse(commentSchemaQuery.stdout).map((row: { name: string }) => row.name).sort(),
    ["column_name", "comment", "comment", "table_name", "table_name", "updated_at", "updated_at"]
  );
});

function spawnNode(
  args: string[],
  options: SpawnOptionsWithoutStdio,
  control: { timeoutMs?: number } = {}
): Promise<{ status: number | null; stdout: string; stderr: string; timedOut: boolean }> {
  return new Promise((resolve, reject) => {
    const child = spawn("node", args, options);
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    const timeoutMs = control.timeoutMs ?? 30_000;
    const timer = setTimeout(() => {
      timedOut = true;
      stderr += `spawnNode timed out after ${timeoutMs}ms\n`;
      child.kill("SIGTERM");
    }, timeoutMs);
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk: string) => {
      stderr += chunk;
    });
    child.on("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
    child.on("close", (status) => {
      clearTimeout(timer);
      resolve({ status, stdout, stderr, timedOut });
    });
  });
}

function normalizeAnchorText(value: string): string {
  return value.replace(/[\s,，_`|]/g, "").toLowerCase();
}

function readDryRunLog(baseDir: string): string {
  const candidates = [
    path.join(baseDir, "dry-run.log"),
    path.join(baseDir, "reports", "dry-run.log")
  ];
  return candidates
    .filter((item) => fs.existsSync(item))
    .map((item) => fs.readFileSync(item, "utf8"))
    .join("\n");
}

test("agent cleanup examples prune only expired patrol session files", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-cleanup-"));
  const oldStamp = "202001010000";

  const cases = [
    {
      script: "examples/cleanup/openclaw-jsonl-cleanup.sh",
      envKey: "AGENT_PATROL_OPENCLAW_SESSION_DIR",
      oldPatrol: "patrol-finance-old.jsonl",
      freshPatrol: "patrol-finance-fresh.jsonl",
      regular: "regular-session.jsonl"
    },
    {
      script: "examples/cleanup/hermes-json-cleanup.sh",
      envKey: "AGENT_PATROL_HERMES_SESSION_DIR",
      oldPatrol: "patrol-hermes-old.json",
      freshPatrol: "patrol-hermes-fresh.json",
      regular: "session_cron_keep.json"
    },
    {
      script: "examples/cleanup/claude-session-cleanup.sh",
      envKey: "AGENT_PATROL_CLAUDE_SESSION_DIR",
      oldPatrol: "patrol-claude-old.jsonl",
      freshPatrol: "patrol-claude-fresh.jsonl",
      regular: "conversation-keep.jsonl"
    }
  ];

  for (const item of cases) {
    const sessionDir = path.join(dir, item.envKey);
    fs.mkdirSync(sessionDir, { recursive: true });
    for (const name of [item.oldPatrol, item.freshPatrol, item.regular]) {
      fs.writeFileSync(path.join(sessionDir, name), "{}", "utf8");
    }
    spawnSync("touch", ["-t", oldStamp, path.join(sessionDir, item.oldPatrol)], { encoding: "utf8" });
    spawnSync("touch", ["-t", oldStamp, path.join(sessionDir, item.regular)], { encoding: "utf8" });

    const result = spawnSync("bash", [item.script], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        [item.envKey]: sessionDir,
        AGENT_PATROL_SESSION_RETENTION_DAYS: "1"
      }
    });

    assert.equal(result.status, 0, `${item.script} failed: ${result.stderr}`);
    assert.equal(fs.existsSync(path.join(sessionDir, item.oldPatrol)), false, `${item.oldPatrol} should be removed`);
    assert.equal(fs.existsSync(path.join(sessionDir, item.freshPatrol)), true, `${item.freshPatrol} should stay`);
    assert.equal(fs.existsSync(path.join(sessionDir, item.regular)), true, `${item.regular} should stay`);
  }
});

test("agent cleanup dispatcher requires explicitly configured kinds", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-cleanup-dispatcher-"));
  const oldStamp = "202001010000";
  const files = [
    {
      envKey: "AGENT_PATROL_OPENCLAW_SESSION_DIR",
      name: "patrol-openclaw-old.jsonl"
    },
    {
      envKey: "AGENT_PATROL_HERMES_SESSION_DIR",
      name: "patrol-hermes-old.json"
    },
    {
      envKey: "AGENT_PATROL_CLAUDE_SESSION_DIR",
      name: "patrol-claude-old.jsonl"
    }
  ];
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    AGENT_PATROL_SESSION_RETENTION_DAYS: "1"
  };

  for (const item of files) {
    const sessionDir = path.join(dir, item.envKey);
    fs.mkdirSync(sessionDir, { recursive: true });
    const file = path.join(sessionDir, item.name);
    fs.writeFileSync(file, "{}", "utf8");
    spawnSync("touch", ["-t", oldStamp, file], { encoding: "utf8" });
    env[item.envKey] = sessionDir;
  }
  delete env.AGENT_PATROL_CLEANUP_KINDS;

  const result = spawnSync("bash", ["examples/cleanup/run-agent-cleanups.sh"], {
    cwd: process.cwd(),
    encoding: "utf8",
    env
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /skip agent cleanup: no AGENT_PATROL_CLEANUP_KINDS/);
  for (const item of files) {
    assert.equal(
      fs.existsSync(path.join(dir, item.envKey, item.name)),
      true,
      `${item.name} should stay when cleanup kinds are not configured`
    );
  }
});

function withFinanceqaEntityVariables(config: ReturnType<typeof loadConfig>): ReturnType<typeof loadConfig> {
  return {
    ...config,
    caseVariables: {
      ...(config.caseVariables ?? {}),
      finance_customer_receivable_unpaid: { customer_name: ["客户甲"] },
      finance_supplier_payable_unpaid: { supplier_name: ["供应商乙"] },
      finance_contract_receivable_unpaid: { contract_name: ["项目Alpha"] },
      finance_single_project_payable_unpaid: { project_name: ["合同Beta"] }
    }
  };
}
