import test from "node:test";
import assert from "node:assert/strict";
import { generateCases } from "../src/cases.ts";

test("generateCases creates deterministic agent-level cases", () => {
  const config = {
    templates: {
      latest_revenue: {
        questions: ["收入表中最新月份的营收是多少？", "按最新可见月份，看一下当月营收。"]
      },
      customer_detail: {
        question: "{{customer.name}} 最近进展怎么样？",
        fallbackQuestion: "一个活跃客户 最近进展怎么样？"
      }
    },
    targets: {
      finance: {
        kind: "openclaw_finance_agent",
        runner: { type: "openclaw_agent_cli" },
        oracle: { type: "financeqa_readonly" },
        suites: { smoke: { templates: ["latest_revenue"], caseCount: 1 } }
      },
      bossa: {
        kind: "bossa_claude_agent",
        runner: { type: "claude_agent_sdk" },
        oracle: { type: "bossa_readonly_mcp" },
        suites: { smoke: { templates: ["customer_detail"], caseCount: 1 } }
      }
    }
  };
  const anchors = {
    bossa: { customers: [{ name: "测试客户A" }] }
  };

  const first = generateCases(config, { suite: "smoke", seed: "2026-06-25", anchors });
  const second = generateCases(config, { suite: "smoke", seed: "2026-06-25", anchors });

  assert.deepEqual(first, second);
  assert.equal(first.length, 2);
  assert.equal(first[0].actualRunner, "openclaw_agent_cli");
  assert.equal(first[0].oracle, "financeqa_readonly");
  assert.equal(first[1].actualRunner, "claude_agent_sdk");
  assert.equal(first[1].oracle, "bossa_readonly_mcp");
  assert.equal(first[1].question, "测试客户A 最近进展怎么样？");
  assert.notEqual(first[0].actualRunner, "direct_mcp");
});

test("generateCases rejects unknown templates instead of using built-in business defaults", () => {
  const config = {
    templates: {},
    targets: {
      finance: {
        runner: { type: "openclaw_agent_cli" },
        oracle: { type: "financeqa_readonly" },
        suites: { smoke: { templates: ["finance_latest_month_revenue"], caseCount: 1 } }
      }
    }
  };

  assert.throws(() => generateCases(config, { suite: "smoke", seed: "fixed" }), /unknown case template/i);
});

test("generateCases samples template variable combinations deterministically", () => {
  const config = {
    templates: {
      finance_matrix: {
        questions: [
          "{{period}}，所有项目的{{metric}}是多少？",
          "项目口径看，{{period}}还有多少{{metric}}？"
        ],
        variables: {
          period: ["收入表中最新月份", "从2025年10月起到上一个完整自然月月底"],
          metric: ["应收未收", "应付未付"]
        },
        scoring: { mustContainAny: [["项目口径", "所有项目"]] }
      }
    },
    targets: {
      finance: {
        runner: { type: "openclaw_agent_cli" },
        oracle: { type: "financeqa_readonly" },
        suites: { smoke: { templates: ["finance_matrix"], caseCount: 3 } }
      }
    }
  };

  const first = generateCases(config, { suite: "smoke", seed: "2026-06-25" });
  const second = generateCases(config, { suite: "smoke", seed: "2026-06-25" });
  const nextDay = generateCases(config, { suite: "smoke", seed: "2026-06-26" });

  assert.deepEqual(first, second);
  assert.equal(first.length, 3);
  assert.equal(new Set(first.map((item) => item.question)).size, 3);
  assert.equal(first.some((item) => item.question.includes("收入表中最新月份")), true);
  assert.equal(first.some((item) => item.question.includes("上一个完整自然月月底")), true);
  assert.equal(first.some((item) => item.question.includes("应收未收")), true);
  assert.equal(first.some((item) => item.question.includes("应付未付")), true);
  assert.equal(first.some((item) => item.question.includes("{{")), false);
  assert.notDeepEqual(nextDay.map((item) => item.question), first.map((item) => item.question));
});

test("generateCases expands per-template dynamic case variables and keeps them as anchors", () => {
  const config = {
    templates: {
      customer_receivable: {
        questions: [
          "{{customer_name}} {{period}}应收未收是多少？",
          "按项目口径看，{{customer_name}} {{period}}还有多少没收回来？"
        ],
        variables: {
          period: ["从2025年10月起到上一个完整自然月月底"]
        },
        questionAnchors: [["应收未收", "没收回来"]]
      }
    },
    caseVariables: {
      customer_receivable: {
        customer_name: ["客户甲", "客户乙"]
      }
    },
    targets: {
      finance: {
        runner: { type: "openclaw_agent_cli" },
        oracle: { type: "financeqa_readonly" },
        suites: { smoke: { templates: ["customer_receivable"], caseCount: 2 } }
      }
    }
  };

  const cases = generateCases(config, { suite: "smoke", seed: "entity-seed" });

  assert.equal(cases.length, 2);
  assert.equal(cases.some((item) => item.question.includes("客户甲")), true);
  assert.equal(cases.some((item) => item.question.includes("客户乙")), true);
  assert.equal(cases.some((item) => item.question.includes("{{")), false);
  assert.deepEqual(
    cases.map((item) => item.questionAnchors?.at(-1)),
    cases.map((item) => [item.question.match(/客户[甲乙]/)?.[0]])
  );
  assert.equal(cases.some((item) => item.questionAnchors?.some((group) => group.includes("从2025年10月起到上一个完整自然月月底"))), false);
});
