import fs from "node:fs";
import path from "node:path";
import yaml from "js-yaml";
import type { PatrolConfig, TargetConfig } from "./types.ts";

type Env = Record<string, string | undefined>;

export function loadConfig(filePath: string, env: Env = process.env): PatrolConfig {
  const raw = fs.readFileSync(filePath, "utf8");
  const loaded = yaml.load(raw) as unknown;
  const expanded = expandEnv(loaded, env) as Partial<PatrolConfig>;
  const caseVariablesFile = resolvedOptionalString(expanded.caseVariablesFile);
  const config: PatrolConfig = {
    version: expanded.version,
    timezone: expanded.timezone,
    writeToolPatterns: expanded.writeToolPatterns,
    caseVariablesFile,
    caseVariables: mergeCaseVariables(
      normalizeCaseVariables(expanded.caseVariables),
      caseVariablesFile ? readCaseVariablesFile(filePath, caseVariablesFile) : undefined
    ),
    report: {
      minAccuracy: expanded.report?.minAccuracy ?? 0.9,
      outputDir: expanded.report?.outputDir
    },
    templates: expanded.templates,
    targets: expanded.targets ?? {}
  };
  validateConfig(config);
  return config;
}

function resolvedOptionalString(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  if (!trimmed || trimmed.includes("${")) return undefined;
  return trimmed;
}

function readCaseVariablesFile(configPath: string, filePath: string): Record<string, Record<string, string[]>> {
  const absolutePath = path.isAbsolute(filePath) ? filePath : path.resolve(path.dirname(configPath), filePath);
  const parsed = JSON.parse(fs.readFileSync(absolutePath, "utf8")) as unknown;
  const record = parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {};
  return normalizeCaseVariables(record.templates ?? record) ?? {};
}

function normalizeCaseVariables(value: unknown): Record<string, Record<string, string[]>> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const out: Record<string, Record<string, string[]>> = {};
  for (const [templateName, variables] of Object.entries(value)) {
    if (!variables || typeof variables !== "object" || Array.isArray(variables)) continue;
    const normalizedVariables: Record<string, string[]> = {};
    for (const [variableName, rawValues] of Object.entries(variables)) {
      const values = Array.isArray(rawValues)
        ? rawValues.map((item) => typeof item === "string" ? item.trim() : "").filter(Boolean)
        : [];
      if (values.length > 0) normalizedVariables[variableName] = values;
    }
    if (Object.keys(normalizedVariables).length > 0) out[templateName] = normalizedVariables;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function mergeCaseVariables(
  first: Record<string, Record<string, string[]>> | undefined,
  second: Record<string, Record<string, string[]>> | undefined
): Record<string, Record<string, string[]>> | undefined {
  const merged: Record<string, Record<string, string[]>> = {};
  for (const source of [first, second]) {
    for (const [templateName, variables] of Object.entries(source ?? {})) {
      const existing = merged[templateName] ?? {};
      for (const [variableName, values] of Object.entries(variables)) {
        existing[variableName] = unique([...(existing[variableName] ?? []), ...values]);
      }
      merged[templateName] = existing;
    }
  }
  return Object.keys(merged).length > 0 ? merged : undefined;
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

function expandEnv(value: unknown, env: Env): unknown {
  if (typeof value === "string") {
    return value.replace(/\$\{([A-Z0-9_]+)\}/g, (match, key: string) => env[key] ?? match);
  }
  if (Array.isArray(value)) {
    return value.map((item) => expandEnv(item, env));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, expandEnv(item, env)])
    );
  }
  return value;
}

function validateConfig(config: PatrolConfig): void {
  if (!config.targets || Object.keys(config.targets).length === 0) {
    throw new Error("config must define at least one target");
  }
  for (const [name, target] of Object.entries(config.targets)) {
    validateTarget(name, target);
  }
}

function validateTarget(name: string, target: TargetConfig): void {
  if (!target.runner || typeof target.runner.type !== "string" || !target.runner.type) {
    throw new Error(`target ${name} missing runner`);
  }
  if (!target.oracle) {
    throw new Error(`target ${name} missing oracle`);
  }
  if (!target.oracle.mcpUrl) {
    throw new Error(`target ${name} missing oracle mcpUrl`);
  }
  if (!Array.isArray(target.oracle.allowedTools) || target.oracle.allowedTools.length === 0) {
    throw new Error(`target ${name} missing oracle allowedTools`);
  }
}
