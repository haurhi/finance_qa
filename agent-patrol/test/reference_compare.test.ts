import test from "node:test";
import assert from "node:assert/strict";
import { deriveReferenceRules } from "../src/reference_compare.ts";

const referenceAnswer = "2025-10~2026-04 老板口径先看项目汇总：项目应收 10943576.36 元。补充项目结算 45769448.67 元、已到账 35610224.56 元；其中已开票未回款 212890.38 元。 来源：《fin-revenue-0422.xlsx》的【25年Q4收入明细】和【26年Q1收入明细】；《fin-revcost-0601.xlsx》的【26年Q2收入明细】 来源更新时间：2026-06-25 12:35:09";

test("deriveReferenceRules extracts labeled headline amounts, periods, sources, and perspectives", () => {
  const rules = deriveReferenceRules(referenceAnswer, {
    amounts: {
      labels: ["项目应收", "应收未收"]
    },
    periods: true,
    sources: true,
    perspectives: true
  });

  assert.deepEqual(rules.amounts, [{ label: "项目应收", value: 10943576.36 }]);
  assert.deepEqual(rules.periods, ["2025-10", "2026-04"]);
  assert.deepEqual(rules.sources, ["fin-revenue-0422.xlsx", "fin-revcost-0601.xlsx"]);
  assert.deepEqual(rules.perspectives, ["老板口径", "项目汇总"]);
});

test("deriveReferenceRules converts ten-thousand yuan amounts", () => {
  const rules = deriveReferenceRules("项目应收 218.52 万元，来源：《fin-revcost-0601.xlsx》", {
    amounts: {
      labels: ["项目应收"]
    }
  });

  assert.deepEqual(rules.amounts, [{ label: "项目应收", value: 2185200 }]);
});

test("deriveReferenceRules extracts multiple labels and negative amounts", () => {
  const rules = deriveReferenceRules("2026-06 现金流入 100.00 元，现金流出 300.00 元，净现金流 -200.00 元。", {
    amounts: {
      labels: ["现金流入", "现金流出", "净现金流"]
    }
  });

  assert.deepEqual(rules.amounts, [
    { label: "现金流入", value: 100 },
    { label: "现金流出", value: 300 },
    { label: "净现金流", value: -200 }
  ]);
});
