import { generateCases } from "./cases.ts";
import { runDoctor } from "./doctor.ts";
import { applyQuestionGenerators, type ExecuteQuestionGenerator } from "./question_generator.ts";
import { writeReport } from "./report.ts";
import { executeGoldenReference, executeReadonlyReference, type ExecuteReferenceInput } from "./reference.ts";
import { runCommandAgent } from "./runners/command_runner.ts";
import { scoreCase, type CaseScore } from "./scorer.ts";
import type { AgentEnvelope, CaseEvidence, PatrolCase, PatrolConfig, ReferenceEnvelope, TargetConfig } from "./types.ts";

export interface ExecuteAgentInput {
  patrolCase: PatrolCase;
  target: TargetConfig;
  sessionId: string;
}

export type ExecuteAgent = (input: ExecuteAgentInput) => Promise<AgentEnvelope>;
export type ExecuteReference = (input: ExecuteReferenceInput) => Promise<ReferenceEnvelope | undefined>;
export type ExecuteGoldenReference = (input: ExecuteReferenceInput) => Promise<ReferenceEnvelope | undefined>;

export interface RunSuiteOptions {
  suite: string;
  seed: string;
  outDir: string;
  executeAgent?: ExecuteAgent;
  executeReference?: ExecuteReference;
  executeGoldenReference?: ExecuteGoldenReference;
  executeQuestionGenerator?: ExecuteQuestionGenerator;
}

export interface RunSuiteResult {
  cases: PatrolCase[];
  results: Array<{
    caseId: string;
    target: string;
    runner: string;
    question: string;
    durationMs: number;
    actual: AgentEnvelope;
  }>;
  scores: CaseScore[];
  evidence: CaseEvidence[];
  aggregate: {
    total: number;
    passed: number;
    accuracy: number;
    businessPassed: number;
    businessAccuracy: number;
    durationMs: number;
    thresholdPassed: boolean;
    businessThresholdPassed: boolean;
  };
}

export async function runSuite(config: PatrolConfig, options: RunSuiteOptions): Promise<RunSuiteResult> {
  const doctor = runDoctor(config);
  if (!doctor.ok) {
    throw new Error("patrol doctor failed; fix config before run");
  }
  const cases = await applyQuestionGenerators(
    generateCases(config, { suite: options.suite, seed: options.seed }),
    {
      targets: config.targets,
      suite: options.suite,
      seed: options.seed,
      executeQuestionGenerator: options.executeQuestionGenerator
    }
  );
  const executeAgent = options.executeAgent ?? defaultExecuteAgent;
  const executeReference = options.executeReference ?? executeReadonlyReference;
  const executeGolden = options.executeGoldenReference ?? executeGoldenReference;
  const results: RunSuiteResult["results"] = [];
  const scores: CaseScore[] = [];
  const evidence: CaseEvidence[] = [];
  const startedAt = Date.now();

  for (const patrolCase of cases) {
    const target = config.targets[patrolCase.target];
    if (!target) {
      throw new Error(`missing target for case: ${patrolCase.target}`);
    }
    const sessionId = makeSessionId(target, patrolCase, options.seed);
    const caseStartedAt = Date.now();
    const actual = await safeExecuteAgent({ executeAgent, patrolCase, target, sessionId });
    const durationMs = Date.now() - caseStartedAt;
    results.push({
      caseId: patrolCase.id,
      target: patrolCase.target,
      runner: target.runner.type,
      question: patrolCase.question,
      durationMs,
      actual
    });
    const configuredGoldenReference = Boolean(target.goldenReference);
    const goldenReference = configuredGoldenReference
      ? await safeExecuteGoldenReference({ executeGolden, patrolCase, target })
      : undefined;
    const directToolBaseline = await safeExecuteReference({ executeReference, patrolCase, target });
    const reference = configuredGoldenReference ? goldenReference : directToolBaseline;
    const score = scoreCase({
      id: patrolCase.id,
      expected: patrolCase.scoring,
      actual,
      goldenReference,
      directToolBaseline,
      reference
    });
    scores.push(score);
    evidence.push({
      caseId: patrolCase.id,
      target: patrolCase.target,
      runner: target.runner.type,
      question: patrolCase.question,
      expected: patrolCase.scoring,
      actual,
      goldenReference,
      directToolBaseline,
      reference,
      score
    });
  }

  const aggregate = aggregateScores(scores, config.report.minAccuracy, Date.now() - startedAt);
  writeReport(options.outDir, {
    manifest: {
      suite: options.suite,
      seed: options.seed,
      generatedAt: new Date().toISOString()
    },
    cases,
    results,
    scores,
    evidence,
    aggregate
  });
  return { cases, results, scores, evidence, aggregate };
}

async function defaultExecuteAgent(input: ExecuteAgentInput): Promise<AgentEnvelope> {
  if (!input.target.runner.command) {
    throw new Error(`target ${input.patrolCase.target} missing runner command`);
  }
  return runCommandAgent({
    commandTemplate: input.target.runner.command,
    question: input.patrolCase.question,
    sessionId: input.sessionId,
    requireSessionIsolation: input.target.runner.requireSessionIsolation,
    timeoutMs: input.target.runner.timeoutMs,
    values: {
      agent: input.target.runner.agent ?? "",
      profile: input.target.runner.profile ?? "",
      userId: input.target.runner.userId ?? ""
    }
  });
}

async function safeExecuteAgent(input: {
  executeAgent: ExecuteAgent;
  patrolCase: PatrolCase;
  target: TargetConfig;
  sessionId: string;
}): Promise<AgentEnvelope> {
  try {
    return await input.executeAgent({
      patrolCase: input.patrolCase,
      target: input.target,
      sessionId: input.sessionId
    });
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      source: "agent",
      answer: `Agent runner failed: ${message}`,
      error: message,
      sessionId: input.sessionId,
      raw: {
        error: message
      }
    };
  }
}

function makeSessionId(target: TargetConfig, patrolCase: PatrolCase, seed: string): string {
  const prefix = target.runner.isolatedSessionPrefix ?? "patrol";
  return `${prefix}-${seed}-${patrolCase.id}`;
}

function aggregateScores(scores: CaseScore[], minAccuracy: number, durationMs: number): RunSuiteResult["aggregate"] {
  const total = scores.length;
  const passed = scores.filter((score) => score.pass).length;
  const businessPassed = scores.filter((score) => score.businessPass).length;
  const accuracy = total === 0 ? 0 : passed / total;
  const businessAccuracy = total === 0 ? 0 : businessPassed / total;
  return {
    total,
    passed,
    accuracy,
    businessPassed,
    businessAccuracy,
    durationMs,
    thresholdPassed: accuracy >= minAccuracy,
    businessThresholdPassed: businessAccuracy >= minAccuracy
  };
}

async function safeExecuteReference(input: {
  executeReference: ExecuteReference;
  patrolCase: PatrolCase;
  target: TargetConfig;
}): Promise<ReferenceEnvelope | undefined> {
  const timeoutMs = input.target.oracle.timeoutMs ?? 120_000;
  const operation = input.executeReference({ patrolCase: input.patrolCase, target: input.target })
    .catch((err: unknown) => directBaselineError(input.target, err));
  return withTimeout(operation, timeoutMs, () => directBaselineTimeout(input.target, timeoutMs));
}

async function safeExecuteGoldenReference(input: {
  executeGolden: ExecuteGoldenReference;
  patrolCase: PatrolCase;
  target: TargetConfig;
}): Promise<ReferenceEnvelope> {
  const timeoutMs = input.target.goldenReference?.timeoutMs ?? 120_000;
  const operation = input.executeGolden({ patrolCase: input.patrolCase, target: input.target })
    .catch((err: unknown) => goldenReferenceError(input.patrolCase.target, err));
  const reference = await withTimeout(operation, timeoutMs, () => goldenReferenceTimeout(input.patrolCase.target, timeoutMs));
  return normalizeGoldenReference(reference, input.patrolCase.target);
}

async function withTimeout<T>(operation: Promise<T>, timeoutMs: number, onTimeout: () => T): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<T>((resolve) => {
    timer = setTimeout(() => resolve(onTimeout()), timeoutMs);
  });
  try {
    return await Promise.race([operation, timeout]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

function directBaselineTimeout(target: TargetConfig, timeoutMs: number): ReferenceEnvelope {
  return directBaselineError(target, new Error(`direct tool baseline timed out after ${timeoutMs}ms`));
}

function directBaselineError(target: TargetConfig, err: unknown): ReferenceEnvelope {
  return {
    source: directBaselineSource(target),
    tool: target.oracle.allowedTools?.[0],
    error: err instanceof Error ? err.message : String(err)
  };
}

function directBaselineSource(target: TargetConfig): ReferenceEnvelope["source"] {
  if (target.oracle.type === "financeqa_readonly") return "financeqa_mcp";
  return "readonly_mcp";
}

function goldenReferenceTimeout(targetName: string, timeoutMs: number): ReferenceEnvelope {
  return {
    source: "golden_reference",
    tool: "command",
    error: `golden reference for target ${targetName} timed out after ${timeoutMs}ms`
  };
}

function goldenReferenceError(targetName: string, err: unknown): ReferenceEnvelope {
  return {
    source: "golden_reference",
    tool: "command",
    error: `golden reference for target ${targetName} failed: ${err instanceof Error ? err.message : String(err)}`
  };
}

function normalizeGoldenReference(reference: ReferenceEnvelope | undefined, targetName: string): ReferenceEnvelope {
  if (!reference) return missingGoldenReference(targetName);
  if (reference.source !== "golden_reference") {
    return {
      source: "golden_reference",
      tool: reference.tool,
      error: `golden reference executor returned source ${reference.source}; expected source golden_reference`,
      raw: reference
    };
  }
  if (!reference.answer && !reference.error) {
    return {
      ...reference,
      error: "golden reference returned no extractable answer"
    };
  }
  return reference;
}

function missingGoldenReference(targetName: string): ReferenceEnvelope {
  return {
    source: "golden_reference",
    tool: "command",
    error: `golden reference is configured for target ${targetName} but did not return a result`
  };
}
