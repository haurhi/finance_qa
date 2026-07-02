import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { loadConfig } from "../src/config.ts";

test("loadConfig expands environment variables and validates agent/oracle shape", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-"));
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(configPath, `
version: 1
targets:
  finance:
    kind: openclaw_finance_agent
    runner:
      type: openclaw_agent_cli
      command: "\${OPENCLAW_CMD}"
      isolatedSessionPrefix: patrol-finance
      requireSessionIsolation: true
    oracle:
      type: financeqa_readonly
      mcpUrl: "\${FINANCE_URL}"
      allowedTools: [finance-query]
    goldenReference:
      type: command
      command: "\${FINANCEQA_GOLDEN_CMD}"
      timeoutMs: 45000
`, "utf8");

  const config = loadConfig(configPath, {
    OPENCLAW_CMD: "openclaw agent --json --message {question}",
    FINANCE_URL: "http://127.0.0.1:3009/mcp",
    FINANCEQA_GOLDEN_CMD: "node golden.mjs --question-file {questionFile}"
  });

  assert.equal(config.report.minAccuracy, 0.9);
  assert.equal(config.targets.finance.runner.command, "openclaw agent --json --message {question}");
  assert.equal(config.targets.finance.oracle.mcpUrl, "http://127.0.0.1:3009/mcp");
  assert.equal(config.targets.finance.goldenReference?.type, "command");
  assert.equal(config.targets.finance.goldenReference?.command, "node golden.mjs --question-file {questionFile}");
  assert.equal(config.targets.finance.goldenReference?.timeoutMs, 45000);
});

test("loadConfig rejects targets without actual agent runner", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-"));
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(configPath, `
targets:
  bad:
    oracle:
      mcpUrl: http://127.0.0.1/mcp
      allowedTools: [finance-query]
`, "utf8");

  assert.throws(() => loadConfig(configPath, {}), /bad.*runner/i);
});

test("loadConfig preserves unresolved environment placeholders for offline generation", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-"));
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(configPath, `
targets:
  finance:
    runner:
      type: openclaw_agent_cli
      command: "\${OPENCLAW_CMD}"
    oracle:
      type: financeqa_readonly
      mcpUrl: "\${FINANCE_URL}"
      allowedTools: [finance-query]
`, "utf8");

  const config = loadConfig(configPath, {});

  assert.equal(config.targets.finance?.runner.command, "${OPENCLAW_CMD}");
  assert.equal(config.targets.finance?.oracle.mcpUrl, "${FINANCE_URL}");
});

test("loadConfig reads optional case variables from an external JSON file", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-vars-"));
  const variablesPath = path.join(dir, "case-variables.json");
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(variablesPath, JSON.stringify({
    templates: {
      finance_customer_receivable_unpaid: {
        customer_name: ["客户A", "客户B"]
      }
    }
  }), "utf8");
  fs.writeFileSync(configPath, `
caseVariablesFile: "\${CASE_VARIABLES_FILE}"
targets:
  finance:
    runner:
      type: openclaw_agent_cli
    oracle:
      type: financeqa_readonly
      mcpUrl: http://127.0.0.1/mcp
      allowedTools: [finance-query]
`, "utf8");

  const config = loadConfig(configPath, { CASE_VARIABLES_FILE: variablesPath });

  assert.deepEqual(config.caseVariables?.finance_customer_receivable_unpaid?.customer_name, ["客户A", "客户B"]);
});

test("loadConfig resolves relative case variables file from process cwd before config directory", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-vars-"));
  const configDir = path.join(dir, "presets");
  const variablesDir = path.join(dir, "tmp/reference-snapshots");
  fs.mkdirSync(configDir, { recursive: true });
  fs.mkdirSync(variablesDir, { recursive: true });
  const variablesPath = path.join(variablesDir, "case-variables.json");
  const configPath = path.join(configDir, "patrol.yaml");
  fs.writeFileSync(variablesPath, JSON.stringify({
    templates: {
      finance_supplier_payable_unpaid: {
        supplier_name: ["供应商A"]
      }
    }
  }), "utf8");
  fs.writeFileSync(configPath, `
caseVariablesFile: "${path.relative(process.cwd(), variablesPath)}"
targets:
  finance:
    runner:
      type: openclaw_agent_cli
    oracle:
      type: financeqa_readonly
      mcpUrl: http://127.0.0.1/mcp
      allowedTools: [finance-query]
`, "utf8");

  const config = loadConfig(configPath, {});

  assert.deepEqual(config.caseVariables?.finance_supplier_payable_unpaid?.supplier_name, ["供应商A"]);
});

test("loadConfig ignores unresolved optional case variables file placeholders", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-vars-"));
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(configPath, `
caseVariablesFile: "\${CASE_VARIABLES_FILE}"
targets:
  finance:
    runner:
      type: openclaw_agent_cli
    oracle:
      type: financeqa_readonly
      mcpUrl: http://127.0.0.1/mcp
      allowedTools: [finance-query]
`, "utf8");

  const config = loadConfig(configPath, {});

  assert.equal(config.caseVariables, undefined);
});

test("loadConfig treats a missing optional case variables file as empty", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-config-vars-"));
  const configPath = path.join(dir, "patrol.yaml");
  fs.writeFileSync(configPath, `
caseVariablesFile: "tmp/reference-snapshots/missing-case-variables.json"
targets:
  finance:
    runner:
      type: openclaw_agent_cli
    oracle:
      type: financeqa_readonly
      mcpUrl: http://127.0.0.1/mcp
      allowedTools: [finance-query]
`, "utf8");

  const config = loadConfig(configPath, {});

  assert.equal(config.caseVariables, undefined);
});

test("production FinanceQA preset requires generated questions and golden reference", () => {
  const config = loadConfig("presets/financeqa-production.yaml", {
    OPENCLAW_AGENT_CMD: "node examples/runners/openclaw_local_runner.mjs --question-file {questionFile} --session-id {sessionId}",
    AGENT_PATROL_QUESTION_GEN_CMD: "node examples/question-generators/llm_command_rewriter.mjs --input {inputFile}",
    FINANCEQA_MCP_URL: "http://127.0.0.1:3009/mcp",
    FINANCEQA_GOLDEN_CMD: "node examples/golden/financeqa_snapshot_reference.mjs --template {template} --question-file {questionFile} --snapshot tmp/reference-snapshots/financeqa-production-latest.json.gz"
  });

  const target = config.targets.finance_qa;
  assert.equal(target.questionGenerator?.type, "command");
  assert.match(target.questionGenerator?.command ?? "", /llm_command_rewriter/);
  assert.equal(target.goldenReference?.type, "command");
  assert.match(target.goldenReference?.command ?? "", /financeqa_snapshot_reference/);
  assert.deepEqual(target.suites?.daily?.templates?.slice(-4), [
    "finance_accounting_arap_balance",
    "finance_bank_cashflow",
    "finance_journal_profit",
    "finance_profit_cash_reconciliation"
  ]);
  assert.deepEqual(target.suites?.daily?.templates?.slice(6, 10), [
    "finance_customer_receivable_unpaid",
    "finance_supplier_payable_unpaid",
    "finance_contract_receivable_unpaid",
    "finance_single_project_payable_unpaid"
  ]);
  assert.equal(target.suites?.daily?.caseCount, 14);
});
