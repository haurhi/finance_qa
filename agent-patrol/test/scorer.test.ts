import test from "node:test";
import assert from "node:assert/strict";
import { scoreCase } from "../src/scorer.ts";

test("scoreCase marks missing actual path invalid", () => {
  const score = scoreCase({
    id: "case-1",
    expected: { mustContain: ["项目口径"] },
    actual: { source: "direct_mcp", answer: "项目口径 100 元" }
  });

  assert.equal(score.pass, false);
  assert.equal(score.invalid, true);
  assert.match(score.failures.join(" "), /invalid_actual_path/);
});

test("scoreCase applies required terms, forbidden terms, amount checks, and write tool guard", () => {
  const score = scoreCase({
    id: "case-2",
    expected: {
      mustContain: ["项目口径", "2026-05"],
      mustNotContain: ["合同应付"],
      amounts: [{ label: "应收未收", value: 12345.67 }]
    },
    actual: {
      source: "agent",
      answer: "项目口径看，2026-05 应收未收为 12,345.67 元。",
      toolCalls: [{ name: "finance-query" }]
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase accepts one term from each required synonym group", () => {
  const score = scoreCase({
    id: "case-3",
    expected: {
      mustContain: ["应收未收"],
      mustContainAny: [
        ["项目口径", "所有项目", "项目应收"],
        ["2025年10月", "2025-10"]
      ]
    },
    actual: {
      source: "agent",
      answer: "2025年10月 – 2026年5月，所有项目应收未收 218.52 万元。"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase reports missing synonym groups", () => {
  const score = scoreCase({
    id: "case-4",
    expected: {
      mustContainAny: [
        ["项目口径", "所有项目", "项目应收"]
      ]
    },
    actual: {
      source: "agent",
      answer: "按客户统计，应收未收 218.52 万元。"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failures, ["missing_any_term:项目口径|所有项目|项目应收"]);
});

test("scoreCase classifies term-only misses as scorer_term_miss", () => {
  const score = scoreCase({
    id: "case-5",
    expected: {
      mustContainAny: [["项目口径", "所有项目", "项目应收"]]
    },
    actual: {
      source: "agent",
      answer: "按客户统计，应收未收 218.52 万元。"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), ["scorer_term_miss"]);
});

test("scoreCase detects agent_changed_amount when reference contains expected amount but actual does not", () => {
  const score = scoreCase({
    id: "case-6",
    expected: {
      amounts: [{ label: "应收未收", value: 2185200 }]
    },
    actual: {
      source: "agent",
      answer: "项目口径看，应收未收为 2,000,000.00 元。"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "项目应收口径，应收未收为 2,185,200.00 元。"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failures, ["missing_amount:应收未收=2185200"]);
  assert.equal(score.failureDetails[0]?.type, "agent_changed_amount");
});

test("scoreCase uses golden reference for reference checks and keeps direct baseline diagnostic only", () => {
  const score = scoreCase({
    id: "case-6a",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目应付"] },
        periods: true,
        sources: true
      }
    },
    actual: {
      source: "agent",
      answer: "2025-10~2026-05 项目应付 636000.00 元。"
    },
    goldenReference: {
      source: "golden_reference",
      tool: "financeqa-structured-golden",
      answer: "2025-10~2026-05 项目应付 636000.00 元。来源：《fin-revcost-0601.xlsx》"
    },
    directToolBaseline: {
      source: "financeqa_mcp",
      tool: "finance-query",
      answer: "2025-10~2026-04 项目应付 1029611.43 元。来源：《fin-revcost-0506.xlsx》"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failures, ["missing_source:fin-revcost-0601.xlsx"]);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), ["missing_source"]);
});

test("scoreCase marks missing FinanceQA MCP reference as insufficient evidence", () => {
  const score = scoreCase({
    id: "case-6b",
    expected: {
      amounts: [{ label: "应收未收", value: 2185200 }]
    },
    actual: {
      source: "agent",
      answer: "项目口径看，应收未收为 2,185,200.00 元。"
    },
    reference: {
      source: "financeqa_mcp",
      tool: "finance-query",
      error: "financeqa oracle returned HTTP 503"
    }
  });

  assert.equal(score.pass, false);
  assert.equal(score.failureDetails[0]?.type, "missing_reference");
  assert.match(String(score.failureDetails[0]?.reference), /HTTP 503/);
});

test("scoreCase prioritizes finance source, period, and perspective evidence", () => {
  const score = scoreCase({
    id: "case-7",
    expected: {
      sources: ["项目应收"],
      periods: ["2025年10月", "2026年5月"],
      perspectives: ["项目口径"]
    },
    actual: {
      source: "agent",
      answer: "应收未收为 2,185,200.00 元。"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), [
    "missing_source",
    "period_mismatch",
    "period_mismatch",
    "perspective_mismatch"
  ]);
});

test("scoreCase orders finance evidence failures before auxiliary term failures", () => {
  const score = scoreCase({
    id: "case-8",
    expected: {
      amounts: [{ label: "应收未收", value: 2185200 }],
      sources: ["项目应收"],
      periods: ["2025年10月"],
      perspectives: ["项目口径"],
      mustContainAny: [["项目口径", "所有项目", "项目应收"]]
    },
    actual: {
      source: "agent",
      answer: "按客户统计，应收未收为 2,000,000.00 元。"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "项目应收口径，2025年10月至2026年5月，应收未收为 2,185,200.00 元。"
    }
  });

  assert.deepEqual(score.failureDetails.map((failure) => failure.type), [
    "agent_changed_amount",
    "missing_source",
    "period_mismatch",
    "perspective_mismatch",
    "scorer_term_miss"
  ]);
});

test("scoreCase derives finance checks from FinanceQA reference without fixed expected amounts", () => {
  const score = scoreCase({
    id: "case-9",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目应收", "应收未收"] },
        periods: true,
        sources: true,
        perspectives: true
      }
    },
    actual: {
      source: "agent",
      answer: "从2025年10月起到2026年5月底，项目应收 146,688.40 元。来源：《fin-revcost-0601.xlsx》"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "2025-10~2026-04 老板口径先看项目汇总：项目应收 10943576.36 元。来源：《fin-revenue-0422.xlsx》《fin-revcost-0601.xlsx》"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), [
    "agent_changed_amount",
    "missing_source",
    "period_mismatch",
    "perspective_mismatch",
    "scorer_term_miss"
  ]);
  assert.deepEqual(score.failures, [
    "missing_amount:项目应收=10943576.36",
    "missing_source:fin-revenue-0422.xlsx",
    "period_mismatch:2026-04",
    "perspective_mismatch:老板口径",
    "missing_any_term:老板口径|项目汇总"
  ]);
});

test("scoreCase accepts equivalent period formats and ten-thousand yuan amounts from reference checks", () => {
  const score = scoreCase({
    id: "case-10",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目应收"] },
        periods: true,
        sources: true,
        perspectives: true
      }
    },
    actual: {
      source: "agent",
      answer: "2025年10月至2026年4月，老板口径项目汇总：项目应收 1094.36 万元。来源：《fin-revenue-0422.xlsx》"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "2025-10~2026-04 老板口径先看项目汇总：项目应收 10943576.36 元。来源：《fin-revenue-0422.xlsx》"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase ignores reference metadata timestamps when deriving business periods", () => {
  const score = scoreCase({
    id: "case-10a",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目结算"] },
        periods: true,
        sources: true
      }
    },
    actual: {
      source: "agent",
      answer: "2026-06 最新月份项目结算收入（营收） 912,713.97 元。来源：《优集收入、成本计算表 - 上传.xlsx》"
    },
    reference: {
      source: "golden_reference",
      answer: "2026-06~2026-06 DB金标口径先看项目汇总：项目结算 912713.97 元。补充项目结算 912713.97 元、已到账 387513.97 元、已开票 837613.97 元。 来源：《优集收入、成本计算表 - 上传.xlsx》 来源更新时间：2026-06-29T20:02:31.995894 快照生成时间：2026-07-01T09:07:47+08"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase compares labeled headline amounts instead of any repeated detail amount", () => {
  const score = scoreCase({
    id: "case-11",
    expected: {
      referenceChecks: {
        amounts: { labels: ["已收票未付款"] },
        periods: true,
        sources: true
      }
    },
    actual: {
      source: "agent",
      answer: "2026年5月，汇总：已收票未付款 2,018,430.15 元。明细：行业商品数据采购合同 未付款 636,000.00 元。来源：《fin-revcost-0601.xlsx》"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "2026-05 老板口径先看项目汇总：已收票未付款 636000.00 元。来源：《fin-revcost-0601.xlsx》"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), ["agent_changed_amount"]);
  assert.deepEqual(score.failures, ["missing_amount:已收票未付款=636000"]);
});

test("scoreCase accepts labeled heading amount on the next line", () => {
  const score = scoreCase({
    id: "case-12",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目结算"] },
        periods: true,
        sources: true
      }
    },
    actual: {
      source: "agent",
      answer: "2026年5月，项目结算营收：\n\n**912,725.41 元**\n\n来源：《fin-revcost-0601.xlsx》"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "2026-05 老板口径先看项目汇总：项目结算 912725.41 元。来源：《fin-revcost-0601.xlsx》"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase accepts configured amount label aliases when the exact reference label is not the answer headline", () => {
  const score = scoreCase({
    id: "case-13",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目应收"] },
        periods: true
      },
      amountLabelGroups: [["项目应收", "应收未收", "未回款"]]
    } as any,
    actual: {
      source: "agent",
      answer: "2025年10月 ~ 2026年5月 项目应收未收汇总\n\n**总应收未收：2,065,398.17 元**"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "2025-10~2026-05 DB金标口径先看项目汇总：项目应收 2065398.17 元。"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase accepts primary metric followed by a nearby amount line", () => {
  const score = scoreCase({
    id: "case-13a",
    expected: {
      referenceChecks: {
        amounts: { labels: ["项目应收"] },
        periods: true
      },
      amountLabelGroups: [["项目应收", "应收未收", "未回款"]]
    } as any,
    actual: {
      source: "agent",
      answer: "期间：2025-10~2026-06\n\n口径：项目应收（应收未收）\n\n金额：2590598.17 元\n\n项目结算 2725.88 万元，已到账 2466.82 万元，其中已开票未回款 46.71 万元。\n\n挂账客户中，四川其妙科技有限公司未回款最多，152.65 万元。"
    },
    reference: {
      source: "golden_reference",
      answer: "2025-10~2026-06 DB金标口径先看项目汇总：项目应收 2590598.17 元。"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase accepts primary metric followed by a markdown amount line", () => {
  const score = scoreCase({
    id: "case-13b",
    expected: {
      referenceChecks: {
        amounts: { labels: ["客户项目应收", "项目应收", "应收未收"] },
        periods: true,
        sources: true
      },
      amountLabelGroups: [["客户项目应收", "项目应收", "应收未收", "未回款"]]
    } as any,
    actual: {
      source: "agent",
      answer: [
        "辽宁金程信息科技有限公司 2025年10月至2026年6月底的应收未收情况：",
        "",
        "**期间：2025-10~2026-06**",
        "**口径：项目应收（应收未收）**",
        "**金额：347301.31 元**",
        "",
        "来源：《优集收入、成本计算表 - 上传.xlsx》"
      ].join("\n")
    },
    reference: {
      source: "golden_reference",
      answer: "2025-10~2026-06 DB金标口径先看项目汇总：客户辽宁金程信息科技有限公司：客户项目应收 347301.31 元。 来源：《优集收入、成本计算表 - 上传.xlsx》"
    }
  });

  assert.equal(score.pass, true);
  assert.deepEqual(score.failures, []);
});

test("scoreCase does not let an alias detail amount override a wrong primary labeled amount", () => {
  const score = scoreCase({
    id: "case-14",
    expected: {
      amounts: [{ label: "项目应收", value: 2065398.17 }],
      amountLabelGroups: [["项目应收", "应收未收", "未回款"]]
    } as any,
    actual: {
      source: "agent",
      answer: "项目应收 2,000,000.00 元。明细：应收未收 2,065,398.17 元。"
    },
    reference: {
      source: "financeqa_mcp",
      answer: "项目应收 2065398.17 元。"
    }
  });

  assert.equal(score.pass, false);
  assert.deepEqual(score.failureDetails.map((failure) => failure.type), ["agent_changed_amount"]);
});
