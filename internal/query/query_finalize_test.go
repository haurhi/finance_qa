package query

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizeQueryResultInjectsTraceSpecAndSourceAttribution(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "query-finalize.sqlite")
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	ctx := queryExecutionContext{
		engine: engine,
		traceMap: map[string]any{
			"router_version": "v2",
			"final_intent":   IntentFallback,
		},
		spec: QuerySpec{
			QueryFamily:        QueryFamilySupplierPayments,
			MetricKind:         MetricKindCost,
			PeriodFrom:         "2026-03",
			PeriodTo:           "2026-03",
			PerspectivePolicy:  PerspectiveCashThenAccrual,
			OpeningPeriodAware: true,
			LexiconProfile:     "rules_config",
		},
	}

	res := finalizeQueryResult(ctx, Result{Success: true, Message: "供应商付款统计完成"})
	if res.Data == nil {
		t.Fatalf("expected data envelope to be initialized")
	}
	if _, ok := res.Data["intent_trace"].(map[string]any); !ok {
		t.Fatalf("intent_trace missing: %+v", res.Data)
	}
	querySpec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	if got := querySpec["query_family"]; got != QueryFamilySupplierPayments {
		t.Fatalf("query_family = %v, want %v", got, QueryFamilySupplierPayments)
	}
	if got := querySpec["opening_period_aware"]; got != true {
		t.Fatalf("opening_period_aware = %v, want true", got)
	}
	if got := res.Data["answer_method"]; got != "sql" {
		t.Fatalf("answer_method = %v, want sql", got)
	}
	sourceNote, _ := res.Data["source_note"].(string)
	if !strings.Contains(sourceNote, "《银行流水》") {
		t.Fatalf("source_note = %q, want bank statement attribution", sourceNote)
	}
	if !strings.Contains(res.Message, "来源：") {
		t.Fatalf("message should append source note, got: %s", res.Message)
	}
}

func TestFinalizeQueryResultCarriesSemanticFamiliesOverride(t *testing.T) {
	ctx := queryExecutionContext{
		spec: QuerySpec{
			QueryFamily: QueryFamilyGeneral,
			PeriodFrom:  "2026-01",
			PeriodTo:    "2026-03",
			TimeScope:   TimeScopeQuarter,
		},
	}

	res := finalizeQueryResult(ctx, Result{
		Success: true,
		Message: "现金余额已计算",
		Data: map[string]any{
			"query_spec_overrides": map[string]any{
				"semantic_families": []string{"cash_balance", "bank_cash_flow", "balance_sheet"},
			},
		},
	})
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"cash_balance", "bank_cash_flow", "balance_sheet"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %q", families, want)
		}
	}
}

func TestFinalizeQueryResultAppliesPeriodOverrideToBossRewrite(t *testing.T) {
	ctx := queryExecutionContext{
		spec: QuerySpec{
			QueryFamily:             QueryFamilyCoreMetric,
			MetricKind:              MetricKindRevenue,
			PeriodFrom:              "2026-07",
			PeriodTo:                "2026-07",
			TimeScope:               TimeScopeMonth,
			PreferContractAggregate: true,
			BossRewrite: BossQueryRewrite{
				Metric:              BossMetricRevenue,
				Scope:               BossScopeCompany,
				PeriodFrom:          "2026-07",
				PeriodTo:            "2026-07",
				Granularity:         BossGranularityAggregate,
				Perspective:         BossPerspectiveContractFirst,
				RequiresSourceProbe: true,
			},
		},
	}

	res := finalizeQueryResult(ctx, Result{
		Success: true,
		Message: "2026-06 项目结算收入 100.00 元。",
		Data: map[string]any{
			"source_priority": "contract_first",
			"query_spec_overrides": map[string]any{
				"period_from": "2026-06",
				"period_to":   "2026-06",
				"time_scope":  string(TimeScopeMonth),
			},
		},
	})
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	if got := spec["period_to"]; got != "2026-06" {
		t.Fatalf("query_spec.period_to = %v, want 2026-06", got)
	}
	rewrite, ok := spec["boss_rewrite"].(map[string]any)
	if !ok {
		t.Fatalf("boss_rewrite missing: %+v", spec)
	}
	if got := rewrite["period_to"]; got != "2026-06" {
		t.Fatalf("boss_rewrite.period_to = %v, want actual business period 2026-06", got)
	}
	if got := rewrite["period_from"]; got != "2026-06" {
		t.Fatalf("boss_rewrite.period_from = %v, want actual business period 2026-06", got)
	}
}

func TestFinalizeQueryResultBuildsStructuredFinanceFacts(t *testing.T) {
	ctx := queryExecutionContext{
		spec: QuerySpec{
			OriginalQuestion:  "按序时账口径，最新完整月份账上净利润是多少？",
			QueryFamily:       QueryFamilyCoreMetric,
			MetricKind:        MetricKindProfit,
			PeriodFrom:        "2026-04",
			PeriodTo:          "2026-04",
			SourceConstraint:  "journal",
			PerspectivePolicy: PerspectiveAccrualOnly,
		},
	}

	res := finalizeQueryResult(ctx, Result{
		Success: true,
		Message: "2026-03 账上净利润 291291.55 元。",
		Data: map[string]any{
			"period":             "2026-03",
			"requested_period":   "最新完整月份",
			"business_basis":     "序时账口径",
			"metric_label":       "账上净利润",
			"total":              291291.55,
			"metrics":            map[string]any{"账上净利润": 291291.55},
			"source_tables":      []string{"tenant_uhub.fin_journal"},
			"source_documents":   []string{"《测试序时账.xls》"},
			"source_note":        "来源：《测试序时账.xls》",
			"source_update_note": "来源更新时间：2026-07-01 12:00:00",
			"warnings":           []string{"数据只到 2026-03，2026-04 未入库"},
			"explanation_hints":  []string{"按序时账本年利润科目取数"},
		},
	})

	facts, ok := res.Data["finance_facts"].(map[string]any)
	if !ok {
		t.Fatalf("finance_facts missing: %+v", res.Data)
	}
	for key, want := range map[string]any{
		"schema_version":     "finance_facts.v1",
		"resolved_period":    "2026-03",
		"requested_period":   "最新完整月份",
		"basis":              "序时账口径",
		"headline_metric":    "账上净利润",
		"headline_amount":    291291.55,
		"source_note":        "来源：《测试序时账.xls》",
		"source_update_note": "来源更新时间：2026-07-01 12:00:00",
	} {
		if got := facts[key]; got != want {
			t.Fatalf("finance_facts[%s] = %v, want %v; facts=%+v", key, got, want, facts)
		}
	}
	if got := anySourceStringSlice(facts["source_tables"]); len(got) != 1 || got[0] != "fin_journal" {
		t.Fatalf("finance_facts.source_tables = %#v, want safe logical table fin_journal", got)
	}
	if got := anySourceStringSlice(facts["source_files"]); len(got) != 1 || got[0] != "《测试序时账.xls》" {
		t.Fatalf("finance_facts.source_files = %#v", got)
	}
	metrics, ok := facts["metrics"].(map[string]any)
	if !ok || metrics["账上净利润"] != 291291.55 {
		t.Fatalf("finance_facts.metrics = %#v", facts["metrics"])
	}
	required := anySourceStringSlice(facts["required_atoms"])
	for _, want := range []string{
		"期间：2026-03",
		"口径：序时账口径",
		"金额：291291.55 元",
		"来源：《测试序时账.xls》",
		"来源更新时间：2026-07-01 12:00:00",
	} {
		if !containsString(required, want) {
			t.Fatalf("required_atoms = %#v, want %q", required, want)
		}
	}
}

func TestFinalizeQueryResultLabelsSourceConstraintBasis(t *testing.T) {
	ctx := queryExecutionContext{
		spec: QuerySpec{
			OriginalQuestion: "按序时账口径，最新完整月份账上净利润是多少？",
			QueryFamily:      QueryFamilyCoreMetric,
			MetricKind:       MetricKindProfit,
			PeriodFrom:       "2026-06",
			PeriodTo:         "2026-06",
			SourceConstraint: "journal",
		},
	}

	res := finalizeQueryResult(ctx, Result{
		Success: true,
		Message: "2026-03 账上净利润 291291.55 元。",
		Data: map[string]any{
			"period":        "2026-03",
			"metric_label":  "账上净利润",
			"total":         291291.55,
			"source_tables": []string{"tenant_uhub.fin_journal"},
			"source_note":   "来源：《测试序时账.xls》",
		},
	})

	facts, ok := res.Data["finance_facts"].(map[string]any)
	if !ok {
		t.Fatalf("finance_facts missing: %+v", res.Data)
	}
	if got := facts["basis"]; got != "序时账口径" {
		t.Fatalf("finance_facts.basis = %v, want user-facing source constraint label; facts=%+v", got, facts)
	}
	required := anySourceStringSlice(facts["required_atoms"])
	if !containsString(required, "口径：序时账口径") {
		t.Fatalf("required_atoms = %#v, want user-facing basis atom", required)
	}
}
