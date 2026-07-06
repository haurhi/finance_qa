import fs from "node:fs";
import path from "node:path";

interface ReportInput {
  manifest: unknown;
  cases: unknown[];
  results: unknown[];
  scores: unknown[];
  evidence?: unknown[];
  aggregate: {
    total: number;
    passed: number;
    accuracy: number;
    businessPassed?: number;
    businessAccuracy?: number;
    invalid?: number;
    validTotal?: number;
    validPassed?: number;
    validAccuracy?: number;
    validBusinessPassed?: number;
    validBusinessAccuracy?: number;
    durationMs?: number;
    runnerHealthPassed?: boolean;
    validThresholdPassed?: boolean;
    validBusinessThresholdPassed?: boolean;
    thresholdPassed?: boolean;
    businessThresholdPassed?: boolean;
  };
}

export function redactSensitive(text: string): string {
  return text
    .replace(/Bearer\s+[A-Za-z0-9._~+/-]{16,}/gi, "Bearer [REDACTED]")
    .replace(/\b(token|api[_-]?key|authorization)=\S+/gi, "$1=[REDACTED]");
}

export function writeReport(dir: string, input: ReportInput): void {
  fs.mkdirSync(dir, { recursive: true });
  writeJson(path.join(dir, "manifest.json"), input.manifest);
  writeJson(path.join(dir, "cases.json"), input.cases);
  writeJson(path.join(dir, "scores.json"), input.scores);
  if (input.evidence) {
    fs.writeFileSync(
      path.join(dir, "case_evidence.jsonl"),
      input.evidence.map((row) => redactSensitive(JSON.stringify(row))).join("\n") + "\n",
      "utf8"
    );
    writeFailedEvidencePackages(dir, input);
  }
  writeJson(path.join(dir, "summary.json"), renderSummaryJson(input));
  fs.writeFileSync(
    path.join(dir, "raw_results.jsonl"),
    input.results.map((row) => redactSensitive(JSON.stringify(row))).join("\n") + "\n",
    "utf8"
  );
  fs.writeFileSync(path.join(dir, "summary.md"), renderSummary(input), "utf8");
}

function writeJson(filePath: string, value: unknown): void {
  fs.writeFileSync(filePath, redactSensitive(JSON.stringify(value, null, 2)) + "\n", "utf8");
}

function renderSummary(input: ReportInput): string {
  const accuracy = (input.aggregate.accuracy * 100).toFixed(2);
  const businessAccuracy = typeof input.aggregate.businessAccuracy === "number"
    ? (input.aggregate.businessAccuracy * 100).toFixed(2)
    : undefined;
  const failed = input.aggregate.total - input.aggregate.passed;
  const failedCases = failedCaseRows(input);
  const categoryCounts = failureCategoryCounts(failedCases);
  const lines = [
    "# Agent Patrol Summary",
    "",
    `Accuracy: ${accuracy}%`,
    `Passed: ${input.aggregate.passed}/${input.aggregate.total}`,
    `Failed: ${failed}`
  ];
  if (businessAccuracy !== undefined) {
    const businessPassed = input.aggregate.businessPassed ?? 0;
    lines.push(`Business Accuracy: ${businessAccuracy}%`);
    lines.push(`Business Passed: ${businessPassed}/${input.aggregate.total}`);
  }
  if (typeof input.aggregate.invalid === "number") {
    lines.push(`Runner Invalid: ${input.aggregate.invalid}`);
  }
  if (typeof input.aggregate.runnerHealthPassed === "boolean") {
    lines.push(`Runner Health: ${input.aggregate.runnerHealthPassed ? "passed" : "invalid"}`);
  }
  if (typeof input.aggregate.validAccuracy === "number") {
    lines.push(`Valid Accuracy: ${(input.aggregate.validAccuracy * 100).toFixed(2)}%`);
    lines.push(`Valid Passed: ${input.aggregate.validPassed ?? 0}/${input.aggregate.validTotal ?? 0}`);
    if (typeof input.aggregate.validThresholdPassed === "boolean") {
      lines.push(`Valid Threshold: ${input.aggregate.validThresholdPassed ? "passed" : "failed"}`);
    }
  }
  if (typeof input.aggregate.validBusinessAccuracy === "number") {
    lines.push(`Valid Business Accuracy: ${(input.aggregate.validBusinessAccuracy * 100).toFixed(2)}%`);
    lines.push(`Valid Business Passed: ${input.aggregate.validBusinessPassed ?? 0}/${input.aggregate.validTotal ?? 0}`);
    if (typeof input.aggregate.validBusinessThresholdPassed === "boolean") {
      lines.push(`Valid Business Threshold: ${input.aggregate.validBusinessThresholdPassed ? "passed" : "failed"}`);
    }
  }
  lines.push("");
  const categoryNames = Object.keys(categoryCounts);
  if (categoryNames.length > 0) {
    lines.push("## Failure Categories", "");
    for (const name of categoryNames) {
      lines.push(`- ${name}: ${categoryCounts[name]}`);
    }
    lines.push("");
  }
  if (failedCases.length > 0) {
    lines.push("## Failed Cases", "");
    for (const row of failedCases) {
      lines.push(`- ${row.caseId}`);
      if (row.failures.length > 0) lines.push(`  - Failures: ${row.failures.join(", ")}`);
      if ((row.failureTypes ?? []).length > 0) lines.push(`  - Failure Types: ${row.failureTypes!.join(", ")}`);
      if ((row.failureDiagnoses ?? []).length > 0) lines.push(`  - Failure Diagnoses: ${row.failureDiagnoses!.join(", ")}`);
      if ((row.failureCategories ?? []).length > 0) lines.push(`  - Failure Categories: ${row.failureCategories!.join(", ")}`);
      if (row.question) lines.push(`  - Question: ${row.question}`);
      if (row.questionSource) lines.push(`  - Question Source: ${row.questionSource}`);
      if (row.originalQuestion && row.originalQuestion !== row.question) lines.push(`  - Original Question: ${row.originalQuestion}`);
      if (row.questionGeneratorWarning) lines.push(`  - Question Generator Warning: ${row.questionGeneratorWarning}`);
      if (row.answer) lines.push(`  - Answer: ${truncate(row.answer, 240)}`);
      if ((row.agentTools ?? []).length > 0) lines.push(`  - Agent Tools: ${row.agentTools!.join(", ")}`);
      if (row.goldenReferenceAnswer) lines.push(`  - Golden Reference: ${truncate(row.goldenReferenceAnswer, 240)}`);
      if (row.goldenReferenceError) lines.push(`  - Golden Reference Error: ${truncate(row.goldenReferenceError, 240)}`);
      if (row.directToolBaselineAnswer) lines.push(`  - Direct Baseline: ${truncate(row.directToolBaselineAnswer, 240)}`);
      if (row.directToolBaselineError) lines.push(`  - Direct Baseline Error: ${truncate(row.directToolBaselineError, 240)}`);
      if (!row.goldenReferenceAnswer && !row.directToolBaselineAnswer && row.referenceAnswer) {
        lines.push(`  - Reference: ${truncate(row.referenceAnswer, 240)}`);
      }
      if (row.sessionId) lines.push(`  - Session: ${row.sessionId}`);
      if (row.evidenceFile) lines.push(`  - Evidence: ${row.evidenceFile}`);
    }
    lines.push("");
  }
  return redactSensitive(lines.join("\n"));
}

function renderSummaryJson(input: ReportInput): unknown {
  const failedCases = failedCaseRows(input);
  return {
    manifest: input.manifest,
    aggregate: input.aggregate,
    failureCategoryCounts: failureCategoryCounts(failedCases),
    failedCases
  };
}

function failedCaseRows(input: ReportInput): Array<{
  caseId: string;
  failures: string[];
  failureTypes?: string[];
  failureDiagnoses?: string[];
  failureCategories?: string[];
  question?: string;
  originalQuestion?: string;
  questionSource?: string;
  questionGeneratorWarning?: string;
  answer?: string;
  referenceAnswer?: string;
  goldenReferenceAnswer?: string;
  goldenReferenceError?: string;
  directToolBaselineAnswer?: string;
  directToolBaselineError?: string;
  agentTools?: string[];
  sessionId?: string;
  evidenceFile?: string;
}> {
  const resultsByCase = new Map<string, Record<string, unknown>>();
  for (const result of input.results) {
    const row = asRecord(result);
    const caseId = stringValue(row?.caseId);
    if (caseId && row) resultsByCase.set(caseId, row);
  }
  const evidenceByCase = new Map<string, Record<string, unknown>>();
  for (const evidence of input.evidence ?? []) {
    const row = asRecord(evidence);
    const caseId = stringValue(row?.caseId);
    if (caseId && row) evidenceByCase.set(caseId, row);
  }
  const casesById = new Map<string, Record<string, unknown>>();
  for (const item of input.cases) {
    const row = asRecord(item);
    const caseId = stringValue(row?.id);
    if (caseId && row) casesById.set(caseId, row);
  }
  const rows: Array<{
    caseId: string;
    failures: string[];
    failureTypes?: string[];
    failureDiagnoses?: string[];
    failureCategories?: string[];
    question?: string;
    originalQuestion?: string;
    questionSource?: string;
    questionGeneratorWarning?: string;
    answer?: string;
    referenceAnswer?: string;
    goldenReferenceAnswer?: string;
    goldenReferenceError?: string;
    directToolBaselineAnswer?: string;
    directToolBaselineError?: string;
    agentTools?: string[];
    sessionId?: string;
    evidenceFile?: string;
  }> = [];
  for (const item of input.scores) {
    const score = asRecord(item);
    if (!score || score.pass !== false) continue;
    const caseId = stringValue(score.caseId) ?? "unknown";
    const result = resultsByCase.get(caseId);
    const evidence = evidenceByCase.get(caseId);
    const patrolCase = casesById.get(caseId);
    const actual = asRecord(evidence?.actual) ?? asRecord(result?.actual);
    const reference = asRecord(evidence?.reference);
    const goldenReference = asRecord(evidence?.goldenReference);
    const directToolBaseline = asRecord(evidence?.directToolBaseline);
    const row: {
      caseId: string;
      failures: string[];
      failureTypes?: string[];
      failureDiagnoses?: string[];
      failureCategories?: string[];
      question?: string;
      originalQuestion?: string;
      questionSource?: string;
      questionGeneratorWarning?: string;
      answer?: string;
      referenceAnswer?: string;
      goldenReferenceAnswer?: string;
      goldenReferenceError?: string;
      directToolBaselineAnswer?: string;
      directToolBaselineError?: string;
      agentTools?: string[];
      sessionId?: string;
      evidenceFile?: string;
    } = {
      caseId,
      failures: stringArray(score.failures),
      question: stringValue(evidence?.question) ?? stringValue(result?.question),
      originalQuestion: stringValue(patrolCase?.originalQuestion),
      questionSource: stringValue(patrolCase?.questionSource),
      questionGeneratorWarning: stringValue(patrolCase?.questionGeneratorWarning),
      answer: stringValue(actual?.answer) ?? stringValue(result?.answer),
      referenceAnswer: stringValue(reference?.answer),
      goldenReferenceAnswer: stringValue(goldenReference?.answer),
      goldenReferenceError: stringValue(goldenReference?.error),
      directToolBaselineAnswer: stringValue(directToolBaseline?.answer),
      directToolBaselineError: stringValue(directToolBaseline?.error),
      agentTools: toolNames(actual?.toolCalls),
      sessionId: stringValue(actual?.sessionId) ?? stringValue(actual?.sessionKey),
      evidenceFile: evidence ? failedEvidenceRelativePath(caseId) : undefined
    };
    if (row.agentTools?.length === 0) delete row.agentTools;
    if (row.questionSource === "template") delete row.questionSource;
    const types = failureTypes(score.failureDetails);
    if (types.length > 0) row.failureTypes = types;
    const diagnoses = failureDiagnoses(score.failureDetails);
    if (diagnoses.length > 0) row.failureDiagnoses = diagnoses;
    const categories = failureCategories(score.failureDetails);
    if (categories.length > 0) row.failureCategories = categories;
    rows.push(row);
  }
  return rows;
}

function writeFailedEvidencePackages(dir: string, input: ReportInput): void {
  const evidenceByCase = new Map<string, unknown>();
  for (const evidence of input.evidence ?? []) {
    const row = asRecord(evidence);
    const caseId = stringValue(row?.caseId);
    if (caseId) evidenceByCase.set(caseId, evidence);
  }
  const failedCaseIds = new Set<string>();
  for (const item of input.scores) {
    const score = asRecord(item);
    if (score?.pass === false) {
      const caseId = stringValue(score.caseId);
      if (caseId) failedCaseIds.add(caseId);
    }
  }
  if (failedCaseIds.size === 0) return;
  const failedDir = path.join(dir, "failed_cases");
  fs.mkdirSync(failedDir, { recursive: true });
  for (const caseId of failedCaseIds) {
    const evidence = evidenceByCase.get(caseId);
    if (evidence === undefined) continue;
    writeJson(path.join(dir, failedEvidenceRelativePath(caseId)), evidence);
  }
}

function failedEvidenceRelativePath(caseId: string): string {
  return path.join("failed_cases", `${safeFileName(caseId)}.json`);
}

function safeFileName(value: string): string {
  return value.replace(/[^a-zA-Z0-9._-]+/g, "_");
}

function truncate(value: string, maxLength: number): string {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength - 3)}...`;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function toolNames(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const names = value
    .map((item) => stringValue(asRecord(item)?.name))
    .filter((item): item is string => Boolean(item));
  return [...new Set(names)];
}

function failureTypes(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value
    .map((item) => stringValue(asRecord(item)?.type))
    .filter((item): item is string => Boolean(item));
}

function failureDiagnoses(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value
    .map((item) => stringValue(asRecord(item)?.diagnosis))
    .filter((item): item is string => Boolean(item)))];
}

function failureCategories(value: unknown): string[] {
  return [...new Set(failureTypes(value).map(failureCategory))];
}

function failureCategoryCounts(rows: Array<{ failureCategories?: string[] }>): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const row of rows) {
    for (const category of row.failureCategories ?? []) {
      counts[category] = (counts[category] ?? 0) + 1;
    }
  }
  return counts;
}

function failureCategory(type: string): string {
  if (type === "agent_changed_amount" || type === "amount_mismatch" || type === "period_mismatch" || type === "perspective_mismatch") {
    return "core_accuracy";
  }
  if (type === "missing_source") return "source_evidence";
  if (type === "scorer_term_miss" || type === "forbidden_term") return "format_quality";
  if (type === "invalid_actual_path" || type === "agent_runner_error" || type === "required_tool_missing") return "runner_health";
  if (type === "missing_reference") return "reference_health";
  if (type.startsWith("question_generator_")) return "question_validity";
  if (type === "write_tool_called") return "safety";
  return "other";
}
