export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

export interface OracleConfig {
  type: string;
  mcpUrl?: string;
  bearerTokenEnv?: string;
  allowedTools: string[];
  timeoutMs?: number;
}

export interface GoldenReferenceConfig {
  type: string;
  command?: string;
  timeoutMs?: number;
}

export interface QuestionGeneratorConfig {
  type: string;
  command?: string;
  timeoutMs?: number;
}

export interface RunnerConfig {
  type: string;
  command?: string;
  agent?: string;
  profile?: string;
  userId?: string;
  isolatedSessionPrefix?: string;
  requireSessionIsolation?: boolean;
  disableDelivery?: boolean;
  timeoutMs?: number;
}

export interface TargetConfig {
  kind?: string;
  runner: RunnerConfig;
  oracle: OracleConfig;
  questionGenerator?: QuestionGeneratorConfig;
  goldenReference?: GoldenReferenceConfig;
  suites?: Record<string, SuiteConfig>;
}

export interface SuiteConfig {
  caseCount?: number;
  templates?: string[];
}

export interface CaseTemplateConfig {
  questions?: string[];
  question?: string;
  fallbackQuestion?: string;
  questionAnchors?: string[][];
  variables?: Record<string, string[]>;
  scoring?: Record<string, unknown>;
}

export interface PatrolConfig {
  version?: number;
  timezone?: string;
  writeToolPatterns?: string[];
  caseVariablesFile?: string;
  caseVariables?: Record<string, Record<string, string[]>>;
  report: {
    minAccuracy: number;
    outputDir?: string;
  };
  templates?: Record<string, CaseTemplateConfig>;
  targets: Record<string, TargetConfig>;
}

export interface PatrolCase {
  id: string;
  target: string;
  template: string;
  question: string;
  questionAnchors?: string[][];
  originalQuestion?: string;
  questionSource?: "template" | "llm_question_generator" | string;
  questionGeneratorWarning?: string;
  actualRunner: string;
  oracle: string;
  scoring: Record<string, unknown>;
}

export interface AgentEnvelope {
  source: "agent" | "direct_mcp" | string;
  answer: string;
  error?: string;
  sessionId?: string;
  sessionKey?: string;
  toolCalls?: Array<{ name?: string; [key: string]: unknown }>;
  sessionEvidence?: AgentSessionEvidence;
  raw?: unknown;
}

export interface AgentSessionEvidence {
  sessionFile?: string;
  userMessages?: Array<{ text?: string; timestamp?: string; truncated?: boolean }>;
  toolCalls?: Array<{ id?: string; name?: string; arguments?: unknown }>;
  toolResults?: Array<{ toolCallId?: string; toolName?: string; text?: string; json?: unknown; truncated?: boolean }>;
  parseErrors?: string[];
}

export interface ReferenceEnvelope {
  source: "golden_reference" | "financeqa_mcp" | "readonly_mcp" | string;
  tool?: string;
  answer?: string;
  error?: string;
  raw?: unknown;
}

export interface CaseEvidence {
  caseId: string;
  target: string;
  runner: string;
  question: string;
  expected: Record<string, unknown>;
  actual: AgentEnvelope;
  goldenReference?: ReferenceEnvelope;
  directToolBaseline?: ReferenceEnvelope;
  reference?: ReferenceEnvelope;
  score: {
    caseId?: string;
    pass?: boolean;
    invalid?: boolean;
    failures?: string[];
    failureDetails?: unknown[];
    warnings?: string[];
  };
}
