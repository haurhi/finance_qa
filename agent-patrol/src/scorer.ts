import type { AgentEnvelope, ReferenceEnvelope } from "./types.ts";
import { isWriteTool } from "./guard.ts";
import { deriveReferenceRules, type ReferenceCheckConfig } from "./reference_compare.ts";

interface ExpectedRules {
  mustContain?: string[];
  mustContainAny?: string[][];
  mustNotContain?: string[];
  amounts?: Array<{ label: string; value: number }>;
  amountLabelGroups?: string[][];
  sources?: string[];
  periods?: string[];
  perspectives?: string[];
  referenceChecks?: ReferenceCheckConfig | true;
}

interface ScoreInput {
  id: string;
  expected: ExpectedRules;
  actual: Partial<AgentEnvelope>;
  goldenReference?: Partial<ReferenceEnvelope>;
  directToolBaseline?: Partial<ReferenceEnvelope>;
  reference?: Partial<ReferenceEnvelope>;
}

export interface CaseFailure {
  type: string;
  message: string;
  expected?: unknown;
  actual?: unknown;
  reference?: unknown;
}

export interface CaseScore {
  caseId: string;
  pass: boolean;
  invalid: boolean;
  failures: string[];
  failureDetails: CaseFailure[];
  warnings: string[];
}

export function scoreCase(input: ScoreInput): CaseScore {
  const failures: string[] = [];
  const failureDetails: CaseFailure[] = [];
  const warnings: string[] = [];
  const answer = input.actual.answer ?? "";
  const scoringReference = input.goldenReference ?? input.reference;
  const referenceAnswer = scoringReference?.answer ?? "";
  const expected = mergeExpectedRules(input.expected, referenceAnswer);

  if (input.actual.source !== "agent") {
    addFailure(failures, failureDetails, "invalid_actual_path", "invalid_actual_path", {
      message: "actual answer did not come from the agent path",
      actual: input.actual.source
    });
  }
  if (input.actual.error) {
    addFailure(failures, failureDetails, "agent_runner_error", "agent_runner_error", {
      message: "agent runner failed before producing a valid answer",
      actual: input.actual.error
    });
  }
  if (scoringReference?.source === "golden_reference" && !referenceAnswer) {
    addFailure(failures, failureDetails, "missing_reference:golden_reference", "missing_reference", {
      message: "golden reference is missing, empty, or failed",
      reference: scoringReference.error ?? scoringReference
    });
  } else if (scoringReference?.source === "financeqa_mcp" && !referenceAnswer) {
    addFailure(failures, failureDetails, "missing_reference:financeqa_mcp", "missing_reference", {
      message: "FinanceQA MCP reference is missing, empty, or failed",
      reference: scoringReference.error ?? scoringReference
    });
  }
  for (const amount of expected.amounts ?? []) {
    if (!amountPresent(answer, amount.value, amount.label, expected.amountLabelGroups)) {
      const referenceHasAmount = Boolean(referenceAnswer)
        && amountPresent(referenceAnswer, amount.value, amount.label, expected.amountLabelGroups);
      addFailure(
        failures,
        failureDetails,
        `missing_amount:${amount.label}=${amount.value}`,
        referenceHasAmount ? "agent_changed_amount" : "amount_mismatch",
        {
          message: referenceHasAmount
            ? "actual answer does not contain expected amount but reference does"
            : "actual answer does not contain expected amount",
          expected: amount,
          actual: answer,
          reference: referenceAnswer || undefined
        }
      );
    }
  }
  for (const source of expected.sources ?? []) {
    if (!normalize(answer).includes(normalize(source))) {
      addFailure(failures, failureDetails, `missing_source:${source}`, "missing_source", {
        message: `actual answer is missing source evidence: ${source}`,
        expected: source,
        actual: answer
      });
    }
  }
  for (const period of expected.periods ?? []) {
    if (!periodPresent(answer, period)) {
      addFailure(failures, failureDetails, `period_mismatch:${period}`, "period_mismatch", {
        message: `actual answer is missing expected period evidence: ${period}`,
        expected: period,
        actual: answer
      });
    }
  }
  for (const perspective of expected.perspectives ?? []) {
    if (!normalize(answer).includes(normalize(perspective))) {
      addFailure(failures, failureDetails, `perspective_mismatch:${perspective}`, "perspective_mismatch", {
        message: `actual answer is missing expected perspective evidence: ${perspective}`,
        expected: perspective,
        actual: answer
      });
    }
  }
  for (const term of expected.mustContain ?? []) {
    if (!normalize(answer).includes(normalize(term))) {
      addFailure(failures, failureDetails, `missing_term:${term}`, "scorer_term_miss", {
        message: `actual answer is missing required term: ${term}`,
        expected: term,
        actual: answer
      });
    }
  }
  for (const group of expected.mustContainAny ?? []) {
    if (group.length > 0 && !group.some((term) => normalize(answer).includes(normalize(term)))) {
      addFailure(failures, failureDetails, `missing_any_term:${group.join("|")}`, "scorer_term_miss", {
        message: `actual answer is missing one term from required group: ${group.join("|")}`,
        expected: group,
        actual: answer
      });
    }
  }
  for (const term of expected.mustNotContain ?? []) {
    if (normalize(answer).includes(normalize(term))) {
      addFailure(failures, failureDetails, `forbidden_term:${term}`, "forbidden_term", {
        message: `actual answer contains forbidden term: ${term}`,
        expected: term,
        actual: answer
      });
    }
  }
  for (const call of input.actual.toolCalls ?? []) {
    if (call.name && isWriteTool(call.name)) {
      addFailure(failures, failureDetails, `write_tool_called:${call.name}`, "write_tool_called", {
        message: `actual agent called write tool: ${call.name}`,
        actual: call.name
      });
    }
  }

  return {
    caseId: input.id,
    pass: failures.length === 0,
    invalid: failures.includes("invalid_actual_path"),
    failures,
    failureDetails,
    warnings
  };
}

function mergeExpectedRules(expected: ExpectedRules, referenceAnswer: string): ExpectedRules {
  if (!expected.referenceChecks || !referenceAnswer) return expected;
  const derived = deriveReferenceRules(referenceAnswer, expected.referenceChecks);
  return {
    ...expected,
    amounts: [...(expected.amounts ?? []), ...(derived.amounts ?? [])],
    amountLabelGroups: expected.amountLabelGroups,
    sources: [...(expected.sources ?? []), ...(derived.sources ?? [])],
    periods: [...(expected.periods ?? []), ...(derived.periods ?? [])],
    perspectives: [...(expected.perspectives ?? []), ...(derived.perspectives ?? []).slice(0, 1)],
    mustContainAny: [...(expected.mustContainAny ?? []), ...(derived.mustContainAny ?? [])]
  };
}

function addFailure(
  failures: string[],
  failureDetails: CaseFailure[],
  code: string,
  type: string,
  detail: Omit<CaseFailure, "type">
): void {
  failures.push(code);
  failureDetails.push({ type, ...detail });
}

function amountPresent(answer: string, value: number, label?: string, labelGroups?: string[][]): boolean {
  const variants = amountVariants(value);
  if (label) {
    for (const candidate of amountLabelCandidates(label, labelGroups)) {
      const presence = labeledAmountPresence(answer, candidate, variants);
      if (presence === "match") return true;
      if (presence === "mismatch") return false;
    }
  }
  const normalizedAnswer = normalize(answer);
  return variants.some((variant) => normalizedAnswer.includes(normalize(variant)));
}

function amountLabelCandidates(label: string, labelGroups: string[] | string[][] | undefined): string[] {
  const candidates = [label];
  const groups = Array.isArray(labelGroups?.[0]) ? labelGroups as string[][] : [];
  for (const group of groups) {
    if (!group.some((item) => normalize(item) === normalize(label))) continue;
    for (const item of group) {
      if (!candidates.some((candidate) => normalize(candidate) === normalize(item))) {
        candidates.push(item);
      }
    }
  }
  return candidates;
}

function labeledAmountPresence(answer: string, label: string, variants: string[]): "match" | "mismatch" | "no_amount" {
  const windows = labeledWindows(answer, label);
  if (windows.length === 0) return "no_amount";
  let sawMoneyLikeWindow = false;
  for (const window of windows) {
    const normalizedWindow = normalize(window);
    if (variants.some((variant) => normalizedWindow.includes(normalize(variant)))) {
      return "match";
    }
    if (containsMoneyLike(window)) sawMoneyLikeWindow = true;
  }
  return sawMoneyLikeWindow ? "mismatch" : "no_amount";
}

function labeledWindows(answer: string, label: string): string[] {
  const windows: string[] = [];
  let offset = answer.indexOf(label);
  while (offset >= 0) {
    const start = Math.max(0, offset - 24);
    const afterLabel = answer.slice(offset + label.length);
    const boundary = afterLabel.search(/[。；;\n\r|]/);
    const end = boundary >= 0
      ? offset + label.length + boundary + 1
      : Math.min(answer.length, offset + label.length + 96);
    let window = answer.slice(start, end);
    const labelWindow = answer.slice(offset, end);
    if (!containsMoneyLike(labelWindow) && boundary >= 0 && /[\n\r]/.test(afterLabel[boundary] ?? "")) {
      const nextLine = afterLabel.slice(boundary + 1).match(/^\s*(?:金额|合计|总额)?\s*[:：]?\s*([*_`]*\s*-?[0-9][0-9,]*(?:\.\d+)?\s*(?:万元|万|元)?\s*[*_`]*)/);
      if (nextLine?.[1]) {
        window += nextLine[1];
      }
    }
    windows.push(window);
    offset = answer.indexOf(label, offset + label.length);
  }
  if (windows.length > 0) return windows;

  const normalizedAnswer = normalize(answer);
  const normalizedLabel = normalize(label);
  let normalizedOffset = normalizedAnswer.indexOf(normalizedLabel);
  while (normalizedOffset >= 0) {
    const window = normalizedAnswer.slice(Math.max(0, normalizedOffset - 24), normalizedOffset + normalizedLabel.length + 48);
    windows.push(window);
    normalizedOffset = normalizedAnswer.indexOf(normalizedLabel, normalizedOffset + normalizedLabel.length);
  }
  return windows;
}

function containsMoneyLike(value: string): boolean {
  return /-?[0-9][0-9,]*(?:\.\d+)?\s*(?:万元|万|元)/.test(value);
}

function amountVariants(value: number): string[] {
  return [
    value.toFixed(2),
    value.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
    String(Math.round(value)),
    (value / 10_000).toFixed(2),
    `${(value / 10_000).toFixed(2)}万`
  ];
}

function periodPresent(answer: string, period: string): boolean {
  const variants = periodVariants(period);
  const normalizedAnswer = normalize(answer);
  return variants.some((variant) => normalizedAnswer.includes(normalize(variant)));
}

function periodVariants(period: string): string[] {
  const match = period.match(/^(20\d{2})-(0?[1-9]|1[0-2])$/);
  if (!match) return [period];
  const year = match[1]!;
  const month = String(Number(match[2]!));
  const padded = match[2]!.padStart(2, "0");
  return [
    `${year}-${padded}`,
    `${year}-${month}`,
    `${year}年${month}月`,
    `${year}年${padded}月`
  ];
}

function normalize(value: string): string {
  return value.replace(/[\s,，_`|]/g, "").toLowerCase();
}
