import type { CaseTemplateConfig, PatrolCase, SuiteConfig } from "./types.ts";

interface GenerateOptions {
  suite: string;
  seed: string;
  anchors?: Record<string, { customers?: Array<{ name?: string }> }>;
}

interface CaseTargetConfig {
  runner: { type: string };
  oracle: { type: string };
  suites?: Record<string, SuiteConfig>;
}

interface CandidateCase {
  templateName: string;
  variantIndex: number;
  question: string;
  questionAnchors?: string[][];
  scoring: Record<string, unknown>;
}

export function generateCases(
  config: {
    targets: Record<string, CaseTargetConfig>;
    templates?: Record<string, CaseTemplateConfig>;
    caseVariables?: Record<string, Record<string, string[]>>;
  },
  options: GenerateOptions
): PatrolCase[] {
  const cases: PatrolCase[] = [];
  for (const [targetName, target] of Object.entries(config.targets)) {
    cases.push(...generateTargetCases(targetName, target, config.templates ?? {}, config.caseVariables ?? {}, options));
  }
  return cases;
}

function generateTargetCases(
  targetName: string,
  target: CaseTargetConfig,
  templateCatalog: Record<string, CaseTemplateConfig>,
  caseVariables: Record<string, Record<string, string[]>>,
  options: GenerateOptions
): PatrolCase[] {
  const suite = target.suites?.[options.suite];
  const templates = suite?.templates ?? [];
  const anchor = options.anchors?.[targetName] ?? {};
  const candidates = templates.flatMap((templateName) => {
    const def = templateCatalog[templateName];
    if (!def) {
      throw new Error(`unknown case template: ${templateName}`);
    }
    return expandTemplate(templateName, def, anchor, caseVariables[templateName]);
  });
  const selected = selectCandidates(candidates, suite?.caseCount, `${options.seed}:${targetName}:${options.suite}`);
  return selected.map((candidate) => {
    return {
      id: stableId(targetName, candidate.templateName, candidate.variantIndex),
      target: targetName,
      template: candidate.templateName,
      question: candidate.question,
      questionAnchors: candidate.questionAnchors,
      actualRunner: target.runner.type,
      oracle: target.oracle.type,
      scoring: candidate.scoring
    };
  });
}

function expandTemplate(
  templateName: string,
  def: CaseTemplateConfig,
  anchor: { customers?: Array<{ name?: string }> },
  dynamicVariables: Record<string, string[]> | undefined
): CandidateCase[] {
  const bases = questionBases(def);
  const variables = variableCombinations(mergeVariables(def.variables, dynamicVariables));
  const candidates: CandidateCase[] = [];
  for (const base of bases) {
    for (const variableSet of variables) {
      const rendered = applyTemplate(base, anchor, variableSet);
      if (rendered) {
        candidates.push({
          templateName,
          variantIndex: candidates.length,
          question: rendered,
          questionAnchors: withVariableAnchors(def.questionAnchors, base, variableSet, dynamicVariables),
          scoring: def.scoring ?? {}
        });
      }
    }
  }
  if (candidates.length > 0) return candidates;
  if (def.fallbackQuestion) {
    return [{
      templateName,
      variantIndex: 0,
      question: def.fallbackQuestion,
      questionAnchors: def.questionAnchors,
      scoring: def.scoring ?? {}
    }];
  }
  throw new Error("template has no renderable question variants");
}

function questionBases(def: CaseTemplateConfig): string[] {
  if (def.question) return [def.question];
  if (def.questions && def.questions.length > 0) {
    return def.questions;
  }
  if (def.fallbackQuestion) {
    return [def.fallbackQuestion];
  }
  throw new Error("template has no question variants");
}

function applyTemplate(
  question: string,
  anchor: { customers?: Array<{ name?: string }> },
  variables: Record<string, string>
): string {
  const customerName = anchor.customers?.[0]?.name;
  if (!customerName && question.includes("{{customer.name}}")) {
    return "";
  }
  const requiredVariables = [...question.matchAll(/\{\{([a-zA-Z0-9_]+)\}\}/g)].map((match) => match[1]!);
  if (requiredVariables.some((key) => !variables[key])) {
    return "";
  }
  return question
    .replace(/\{\{customer\.name\}\}/g, customerName ?? "")
    .replace(/\{\{([a-zA-Z0-9_]+)\}\}/g, (_match, key: string) => variables[key] ?? "")
    .trim();
}

function mergeVariables(
  staticVariables: Record<string, string[]> | undefined,
  dynamicVariables: Record<string, string[]> | undefined
): Record<string, string[]> {
  const merged: Record<string, string[]> = {};
  for (const source of [staticVariables, dynamicVariables]) {
    for (const [key, values] of Object.entries(source ?? {})) {
      merged[key] = unique([...(merged[key] ?? []), ...values]);
    }
  }
  return merged;
}

function withVariableAnchors(
  anchors: string[][] | undefined,
  baseQuestion: string,
  variables: Record<string, string>,
  dynamicVariables: Record<string, string[]> | undefined
): string[][] | undefined {
  const out = [...(anchors ?? [])];
  const dynamicKeys = new Set(Object.keys(dynamicVariables ?? {}));
  for (const [key, value] of Object.entries(variables)) {
    if (value && dynamicKeys.has(key) && baseQuestion.includes(`{{${key}}}`)) {
      out.push([value]);
    }
  }
  return out.length > 0 ? out : undefined;
}

function variableCombinations(variables: Record<string, string[]>): Array<Record<string, string>> {
  const entries = Object.entries(variables);
  if (entries.length === 0) return [{}];
  return entries.reduce<Array<Record<string, string>>>((acc, [key, values]) => {
    if (!Array.isArray(values) || values.length === 0) {
      throw new Error(`template variable ${key} has no values`);
    }
    return acc.flatMap((item) => values.map((value) => ({ ...item, [key]: value })));
  }, [{}]);
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

function selectCandidates(candidates: CandidateCase[], caseCount: number | undefined, seed: string): CandidateCase[] {
  if (!caseCount || caseCount >= candidates.length) {
    return candidates;
  }
  const groups = groupCandidates(candidates);
  const startGroup = hash(seed) % groups.length;
  const selected: CandidateCase[] = [];
  for (let round = 0; selected.length < caseCount; round += 1) {
    for (let offset = 0; offset < groups.length && selected.length < caseCount; offset += 1) {
      const group = groups[(startGroup + offset) % groups.length]!;
      const startItem = hash(`${seed}:${group[0]!.templateName}`) % group.length;
      selected.push(group[(startItem + round) % group.length]!);
    }
  }
  return selected;
}

function groupCandidates(candidates: CandidateCase[]): CandidateCase[][] {
  const groups: CandidateCase[][] = [];
  const indexByTemplate = new Map<string, number>();
  for (const candidate of candidates) {
    let index = indexByTemplate.get(candidate.templateName);
    if (index === undefined) {
      index = groups.length;
      indexByTemplate.set(candidate.templateName, index);
      groups.push([]);
    }
    groups[index]!.push(candidate);
  }
  return groups;
}

function stableId(target: string, template: string, index: number): string {
  return `${target}_${template}_${String(index + 1).padStart(3, "0")}`;
}

function hash(value: string): number {
  let result = 0;
  for (const char of value) {
    result = (result * 31 + char.charCodeAt(0)) >>> 0;
  }
  return result;
}
