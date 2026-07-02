export interface ReferenceCheckConfig {
  amounts?: boolean | {
    labels?: string[];
  };
  periods?: boolean;
  sources?: boolean;
  perspectives?: boolean;
}

export interface DerivedReferenceRules {
  amounts?: Array<{ label: string; value: number }>;
  periods?: string[];
  sources?: string[];
  perspectives?: string[];
  mustContainAny?: string[][];
}

const DEFAULT_AMOUNT_LABELS = [
  "项目应收",
  "应收未收",
  "已开票未回款",
  "已开票未收款",
  "项目应付",
  "应付未付",
  "已收票未付款",
  "项目结算",
  "营收",
  "收入",
  "账上应付端合计",
  "应收账款",
  "应付账款",
  "其他应付款",
  "现金流入",
  "现金流出",
  "净现金流",
  "银行净流入",
  "账上确认收入",
  "账上成本及费用",
  "账上净利润",
  "差异金额"
];

const PERSPECTIVE_TERMS = [
  "老板口径",
  "项目口径",
  "项目汇总"
];

export function deriveReferenceRules(referenceAnswer: string, config: ReferenceCheckConfig | true | undefined): DerivedReferenceRules {
  if (!config) return {};
  const resolved = config === true ? {
    amounts: true,
    periods: true,
    sources: true,
    perspectives: true
  } satisfies ReferenceCheckConfig : config;
  const rules: DerivedReferenceRules = {};

  if (resolved.amounts) {
    const labels = typeof resolved.amounts === "object" && resolved.amounts.labels?.length
      ? resolved.amounts.labels
      : DEFAULT_AMOUNT_LABELS;
    const amounts = deriveAmounts(referenceAnswer, labels);
    if (amounts.length > 0) rules.amounts = amounts;
  }
  if (resolved.periods) {
    const periods = derivePeriods(referenceAnswer);
    if (periods.length > 0) rules.periods = periods;
  }
  if (resolved.sources) {
    const sources = deriveSources(referenceAnswer);
    if (sources.length > 0) rules.sources = sources;
  }
  if (resolved.perspectives) {
    const perspectives = derivePerspectives(referenceAnswer);
    if (perspectives.length > 0) {
      rules.perspectives = perspectives;
      const headline = perspectives.filter((term) => term === "老板口径" || term === "项目汇总");
      if (headline.length > 0) {
        rules.mustContainAny = [headline];
      }
    }
  }

  return rules;
}

function deriveAmounts(referenceAnswer: string, labels: string[]): Array<{ label: string; value: number }> {
  const amounts: Array<{ label: string; value: number }> = [];
  for (const label of labels) {
    const escaped = escapeRegex(label);
    const match = referenceAnswer.match(new RegExp(`${escaped}\\s*(?:为|[:：])?\\s*(-?[0-9][0-9,]*(?:\\.\\d+)?)\\s*(万元|万|元)?`));
    if (!match) continue;
    const parsed = parseAmount(match[1]!, match[2]);
    if (parsed === undefined) continue;
    amounts.push({ label, value: parsed });
  }
  return uniqueAmounts(amounts);
}

function parseAmount(raw: string, unit: string | undefined): number | undefined {
  const value = Number(raw.replace(/,/g, ""));
  if (!Number.isFinite(value)) return undefined;
  if (unit === "万元" || unit === "万") {
    return roundMoney(value * 10_000);
  }
  return roundMoney(value);
}

function derivePeriods(referenceAnswer: string): string[] {
  const periods: string[] = [];
  const answerText = businessAnswerText(referenceAnswer);
  for (const match of answerText.matchAll(/\b(20\d{2})[-/年](0?[1-9]|1[0-2])月?\b/g)) {
    periods.push(`${match[1]}-${match[2]!.padStart(2, "0")}`);
  }
  return unique(periods).slice(0, 2);
}

function businessAnswerText(referenceAnswer: string): string {
  return referenceAnswer.split(/(?:\s|^)(?:来源：|来源更新时间：?|快照生成时间：?)/)[0] ?? referenceAnswer;
}

function deriveSources(referenceAnswer: string): string[] {
  const sources: string[] = [];
  for (const match of referenceAnswer.matchAll(/《([^》]+?\.(?:xlsx|xls|csv))》/gi)) {
    sources.push(match[1]!);
  }
  return unique(sources);
}

function derivePerspectives(referenceAnswer: string): string[] {
  return PERSPECTIVE_TERMS.filter((term) => referenceAnswer.includes(term));
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

function uniqueAmounts(values: Array<{ label: string; value: number }>): Array<{ label: string; value: number }> {
  const seen = new Set<string>();
  return values.filter((item) => {
    const key = `${item.label}\t${item.value}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function roundMoney(value: number): number {
  return Math.round(value * 100) / 100;
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
