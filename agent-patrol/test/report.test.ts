import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { writeReport, redactSensitive } from "../src/report.ts";

test("redactSensitive removes token-like values", () => {
  const redacted = redactSensitive("Authorization: Bearer abcdefghijklmnopqrstuvwxyz1234567890 token=secret-value");
  assert.doesNotMatch(redacted, /abcdefghijklmnopqrstuvwxyz/);
  assert.doesNotMatch(redacted, /secret-value/);
});

test("writeReport writes summary and raw result files", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-"));
  writeReport(dir, {
    manifest: { suite: "smoke" },
    cases: [{ id: "case-1" }],
    results: [{ caseId: "case-1", answer: "ok" }],
    scores: [{ caseId: "case-1", pass: true }],
    aggregate: { total: 1, passed: 1, accuracy: 1 }
  });

  assert.equal(fs.existsSync(path.join(dir, "manifest.json")), true);
  assert.equal(fs.existsSync(path.join(dir, "cases.json")), true);
  assert.equal(fs.existsSync(path.join(dir, "summary.json")), true);
  assert.equal(fs.existsSync(path.join(dir, "raw_results.jsonl")), true);
  assert.equal(fs.existsSync(path.join(dir, "scores.json")), true);
  assert.match(fs.readFileSync(path.join(dir, "summary.md"), "utf8"), /Accuracy: 100\.00%/);
  const summary = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.deepEqual(summary.aggregate, { total: 1, passed: 1, accuracy: 1 });
  assert.deepEqual(summary.failedCases, []);
});

test("writeReport includes failed case details in summary", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-failed-"));
  writeReport(dir, {
    manifest: { suite: "smoke" },
    cases: [{ id: "case-1" }],
    results: [{
      caseId: "case-1",
      question: "请只回答：AGENT_PATROL_OK",
      actual: {
        answer: "wrong answer",
        sessionId: "patrol-session-1"
      }
    }],
    scores: [{
      caseId: "case-1",
      pass: false,
      failures: ["missing_term:AGENT_PATROL_OK"]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Failed Cases/);
  assert.match(summary, /case-1/);
  assert.match(summary, /missing_term:AGENT_PATROL_OK/);
  assert.match(summary, /请只回答：AGENT_PATROL_OK/);
  assert.match(summary, /wrong answer/);
  assert.match(summary, /patrol-session-1/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.deepEqual(summaryJson.failedCases, [{
    caseId: "case-1",
    failures: ["missing_term:AGENT_PATROL_OK"],
    question: "请只回答：AGENT_PATROL_OK",
    answer: "wrong answer",
    sessionId: "patrol-session-1"
  }]);
});

test("writeReport writes failed evidence package with actual and reference answers", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-evidence-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{ id: "finance-case-1" }],
    results: [{
      caseId: "finance-case-1",
      question: "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？",
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：应收未收 2,000,000.00 元",
        sessionId: "patrol-session-1"
      }
    }],
    evidence: [{
      caseId: "finance-case-1",
      target: "finance_qa",
      question: "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？",
      expected: { amounts: [{ label: "应收未收", value: 2185200 }] },
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：应收未收 2,000,000.00 元",
        sessionId: "patrol-session-1",
        toolCalls: [{ name: "finance-query" }]
      },
      reference: {
        source: "financeqa_mcp",
        tool: "finance-query",
        answer: "FinanceQA MCP：项目应收口径，应收未收 2,185,200.00 元"
      },
      score: {
        caseId: "finance-case-1",
        pass: false,
        invalid: false,
        failures: ["missing_amount:应收未收=2185200"],
        failureDetails: [{
          type: "agent_changed_amount",
          message: "actual answer does not contain expected amount but reference does",
          expected: { label: "应收未收", value: 2185200 },
          actual: "OpenClaw 回答：应收未收 2,000,000.00 元"
        }],
        warnings: []
      }
    }],
    scores: [{
      caseId: "finance-case-1",
      pass: false,
      failures: ["missing_amount:应收未收=2185200"],
      failureDetails: [{ type: "agent_changed_amount", message: "actual answer does not contain expected amount but reference does" }]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const evidenceJsonl = fs.readFileSync(path.join(dir, "case_evidence.jsonl"), "utf8");
  assert.match(evidenceJsonl, /FinanceQA MCP/);
  assert.match(evidenceJsonl, /OpenClaw/);

  const failedPackagePath = path.join(dir, "failed_cases", "finance-case-1.json");
  assert.equal(fs.existsSync(failedPackagePath), true);
  const failedPackage = JSON.parse(fs.readFileSync(failedPackagePath, "utf8"));
  assert.equal(failedPackage.question, "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？");
  assert.match(failedPackage.actual.answer, /2,000,000\.00/);
  assert.match(failedPackage.reference.answer, /2,185,200\.00/);
  assert.equal(failedPackage.score.failureDetails[0].type, "agent_changed_amount");

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Failure Types: agent_changed_amount/);
  assert.match(summary, /Agent Tools: finance-query/);
  assert.match(summary, /Reference: FinanceQA MCP/);
  assert.match(summary, /Evidence: failed_cases\/finance-case-1\.json/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.deepEqual(summaryJson.failedCases[0].failureTypes, ["agent_changed_amount"]);
  assert.deepEqual(summaryJson.failedCases[0].agentTools, ["finance-query"]);
  assert.equal(summaryJson.failedCases[0].evidenceFile, "failed_cases/finance-case-1.json");
});

test("writeReport labels golden reference separately from direct tool baseline", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-golden-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{ id: "finance-case-2" }],
    results: [{
      caseId: "finance-case-2",
      question: "项目应付是多少？",
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：项目应付 500.00 元",
        sessionId: "patrol-session-2"
      }
    }],
    evidence: [{
      caseId: "finance-case-2",
      target: "finance_qa",
      question: "项目应付是多少？",
      expected: { referenceChecks: { amounts: { labels: ["项目应付"] } } },
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：项目应付 500.00 元",
        sessionId: "patrol-session-2"
      },
      goldenReference: {
        source: "golden_reference",
        tool: "financeqa-structured-golden",
        answer: "Golden：项目应付 600.00 元"
      },
      directToolBaseline: {
        source: "financeqa_mcp",
        tool: "finance-query",
        answer: "Direct baseline：项目应付 700.00 元"
      },
      reference: {
        source: "golden_reference",
        tool: "financeqa-structured-golden",
        answer: "Golden：项目应付 600.00 元"
      },
      score: {
        caseId: "finance-case-2",
        pass: false,
        invalid: false,
        failures: ["missing_amount:项目应付=600"],
        failureDetails: [{ type: "agent_changed_amount", message: "actual answer does not contain expected amount but golden reference does" }],
        warnings: []
      }
    }],
    scores: [{
      caseId: "finance-case-2",
      pass: false,
      failures: ["missing_amount:项目应付=600"],
      failureDetails: [{ type: "agent_changed_amount", message: "actual answer does not contain expected amount but golden reference does" }]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Golden Reference: Golden/);
  assert.match(summary, /Direct Baseline: Direct baseline/);

  const failedPackage = JSON.parse(fs.readFileSync(path.join(dir, "failed_cases", "finance-case-2.json"), "utf8"));
  assert.match(failedPackage.goldenReference.answer, /600\.00/);
  assert.match(failedPackage.directToolBaseline.answer, /700\.00/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.match(summaryJson.failedCases[0].goldenReferenceAnswer, /600\.00/);
  assert.match(summaryJson.failedCases[0].directToolBaselineAnswer, /700\.00/);
});

test("writeReport surfaces missing golden reference error next to direct baseline", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-missing-golden-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{ id: "finance-case-3" }],
    results: [{
      caseId: "finance-case-3",
      question: "收入表中最新月份的营收是多少？",
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：项目结算 800000.00 元",
        sessionId: "patrol-session-3"
      }
    }],
    evidence: [{
      caseId: "finance-case-3",
      target: "finance_qa",
      question: "收入表中最新月份的营收是多少？",
      expected: { referenceChecks: { amounts: { labels: ["项目结算"] } } },
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：项目结算 800000.00 元",
        sessionId: "patrol-session-3"
      },
      goldenReference: {
        source: "golden_reference",
        tool: "command",
        error: "golden reference is configured for target finance_qa but did not return a result"
      },
      directToolBaseline: {
        source: "financeqa_mcp",
        tool: "finance-query",
        answer: "Direct baseline：项目结算 800000.00 元"
      },
      reference: {
        source: "golden_reference",
        tool: "command",
        error: "golden reference is configured for target finance_qa but did not return a result"
      },
      score: {
        caseId: "finance-case-3",
        pass: false,
        invalid: false,
        failures: ["missing_reference:golden_reference"],
        failureDetails: [{ type: "missing_reference", message: "golden reference is missing, empty, or failed" }],
        warnings: []
      }
    }],
    scores: [{
      caseId: "finance-case-3",
      pass: false,
      failures: ["missing_reference:golden_reference"],
      failureDetails: [{ type: "missing_reference", message: "golden reference is missing, empty, or failed" }]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Golden Reference Error: golden reference is configured/);
  assert.match(summary, /Direct Baseline: Direct baseline/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.match(summaryJson.failedCases[0].goldenReferenceError, /did not return a result/);
  assert.match(summaryJson.failedCases[0].directToolBaselineAnswer, /800000\.00/);
});

test("writeReport exposes question generation metadata for failed cases", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-question-generator-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{
      id: "finance-case-4",
      question: "老板，上个完整月项目上还有多少应收没收回来？",
      originalQuestion: "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？",
      questionSource: "llm_question_generator"
    }],
    results: [{
      caseId: "finance-case-4",
      question: "老板，上个完整月项目上还有多少应收没收回来？",
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：应收未收 100.00 元",
        sessionId: "patrol-session-4"
      }
    }],
    evidence: [{
      caseId: "finance-case-4",
      target: "finance_qa",
      question: "老板，上个完整月项目上还有多少应收没收回来？",
      expected: { referenceChecks: { amounts: { labels: ["应收未收"] } } },
      actual: {
        source: "agent",
        answer: "OpenClaw 回答：应收未收 100.00 元",
        sessionId: "patrol-session-4"
      },
      reference: {
        source: "golden_reference",
        answer: "Golden：应收未收 200.00 元"
      },
      score: {
        caseId: "finance-case-4",
        pass: false,
        failures: ["missing_amount:应收未收=200"],
        failureDetails: [{ type: "agent_changed_amount" }]
      }
    }],
    scores: [{
      caseId: "finance-case-4",
      pass: false,
      failures: ["missing_amount:应收未收=200"],
      failureDetails: [{ type: "agent_changed_amount" }]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Question Source: llm_question_generator/);
  assert.match(summary, /Original Question: 从2025年10月起到上一个完整自然月月底/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.equal(summaryJson.failedCases[0].questionSource, "llm_question_generator");
  assert.equal(
    summaryJson.failedCases[0].originalQuestion,
    "从2025年10月起到上一个完整自然月月底，所有项目的应收未收是多少？"
  );
});

test("writeReport separates core accuracy failures from source evidence failures", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-categories-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{ id: "finance-case-5" }],
    results: [{
      caseId: "finance-case-5",
      question: "老板，最新月份营收情况？",
      actual: {
        source: "agent",
        answer: "账上营收 0.00 元"
      }
    }],
    scores: [{
      caseId: "finance-case-5",
      pass: false,
      failures: [
        "missing_amount:项目结算=912713.97",
        "missing_source:优集收入、成本计算表 - 上传.xlsx"
      ],
      failureDetails: [
        { type: "agent_changed_amount" },
        { type: "missing_source" }
      ]
    }],
    aggregate: { total: 1, passed: 0, accuracy: 0 }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Failure Categories: core_accuracy, source_evidence/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.deepEqual(summaryJson.failureCategoryCounts, {
    core_accuracy: 1,
    source_evidence: 1
  });
  assert.deepEqual(summaryJson.failedCases[0].failureCategories, ["core_accuracy", "source_evidence"]);
});

test("writeReport reports business accuracy separately from evidence quality", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-patrol-report-business-accuracy-"));
  writeReport(dir, {
    manifest: { suite: "finance" },
    cases: [{ id: "finance-case-6" }],
    results: [{
      caseId: "finance-case-6",
      question: "银行流水净流入是多少？",
      actual: {
        source: "agent",
        answer: "银行卡净增加 -633859.33 元"
      }
    }],
    scores: [{
      caseId: "finance-case-6",
      pass: false,
      businessPass: true,
      failures: ["missing_source:交易查询，20260101-20260331，共143笔.xlsx"],
      failureDetails: [{ type: "missing_source" }]
    }],
    aggregate: {
      total: 1,
      passed: 0,
      accuracy: 0,
      businessPassed: 1,
      businessAccuracy: 1,
      thresholdPassed: false,
      businessThresholdPassed: true
    }
  });

  const summary = fs.readFileSync(path.join(dir, "summary.md"), "utf8");
  assert.match(summary, /Accuracy: 0\.00%/);
  assert.match(summary, /Business Accuracy: 100\.00%/);

  const summaryJson = JSON.parse(fs.readFileSync(path.join(dir, "summary.json"), "utf8"));
  assert.equal(summaryJson.aggregate.businessPassed, 1);
  assert.equal(summaryJson.aggregate.businessAccuracy, 1);
  assert.deepEqual(summaryJson.failedCases[0].failureCategories, ["source_evidence"]);
});
