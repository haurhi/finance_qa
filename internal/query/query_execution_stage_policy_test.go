package query

import "testing"

func TestResolveSourceExecutionStagesPrefersOrchestratedContractAggregate(t *testing.T) {
	ctx := queryExecutionContext{
		q:             "2025年10月收入、成本、利润分别是多少？",
		hasRealEntity: false,
		entity:        "",
		from:          "2025-10",
		to:            "2025-10",
		cfg:           getRuleConfig(),
		spec: QuerySpec{
			QueryFamily:             QueryFamilyCoreMetric,
			MetricKind:              MetricKindRevenue,
			PreferContractAggregate: true,
			PeriodFrom:              "2025-10",
			PeriodTo:                "2025-10",
			RouteDecision: RouteDecision{
				SelectedSource: BossSourceContractAggregate,
				ProbeResults: []SourceProbeResult{
					{Source: BossSourceContractAggregate, CanAnswer: true},
				},
			},
		},
	}

	stages := resolveSourceExecutionStages(ctx)
	want := []executionStage{
		executionStageOrchestrator,
	}
	assertExecutionStagesEqual(t, stages, want)
}

func TestResolveSourceExecutionStagesKeepsCoreMetricFallbackWhenContractAggregateMissing(t *testing.T) {
	cfg := defaultRuleConfig()
	cfg.MetricKeywordLexicon[metricKeyProfit] = []string{"净赚"}
	ctx := queryExecutionContext{
		q:             "2026年2月净赚是多少？",
		hasRealEntity: false,
		entity:        "",
		from:          "2026-02",
		to:            "2026-02",
		cfg:           cfg,
		spec: QuerySpec{
			QueryFamily:             QueryFamilyCoreMetric,
			MetricKind:              MetricKindProfit,
			PreferContractAggregate: true,
			PeriodFrom:              "2026-02",
			PeriodTo:                "2026-02",
			RouteDecision: RouteDecision{
				SelectedSource: BossSourceContractAggregate,
				ProbeResults: []SourceProbeResult{
					{Source: BossSourceContractAggregate, CanAnswer: false},
				},
			},
		},
	}

	stages := resolveSourceExecutionStages(ctx)
	want := []executionStage{
		executionStageOrchestrator,
		executionStageDirectCoreMetricRange,
	}
	assertExecutionStagesEqual(t, stages, want)
}

func TestResolveSourceExecutionStagesPrefersOrchestratedSupplierPayments(t *testing.T) {
	ctx := queryExecutionContext{
		q:    "2026年3月有多少家供应商发生付款？",
		from: "2026-03",
		to:   "2026-03",
		cfg:  getRuleConfig(),
		spec: QuerySpec{
			QueryFamily: QueryFamilySupplierPayments,
			PeriodFrom:  "2026-03",
			PeriodTo:    "2026-03",
		},
	}

	stages := resolveSourceExecutionStages(ctx)
	want := []executionStage{
		executionStageOrchestrator,
		executionStageDirectSupplierPayments,
	}
	assertExecutionStagesEqual(t, stages, want)
}

func TestResolveSourceExecutionStagesKeepsReconciliationOnDirectPath(t *testing.T) {
	ctx := queryExecutionContext{
		q:    "为什么2026年3月收入和利润差异这么大？",
		from: "2026-03",
		to:   "2026-03",
		cfg:  getRuleConfig(),
		spec: QuerySpec{
			QueryFamily: QueryFamilyReconciliation,
			PeriodFrom:  "2026-03",
			PeriodTo:    "2026-03",
		},
	}

	stages := resolveSourceExecutionStages(ctx)
	want := []executionStage{
		executionStageDirectReconciliation,
	}
	assertExecutionStagesEqual(t, stages, want)
}

func TestResolveSourceExecutionStagesPreservesLegacyCoreMetricFallbackWithoutCoreFamily(t *testing.T) {
	ctx := queryExecutionContext{
		q:             "2026年2月账上净利润是多少",
		hasRealEntity: false,
		from:          "2026-02",
		to:            "2026-02",
		cfg:           getRuleConfig(),
		spec: QuerySpec{
			QueryFamily: QueryFamilyGeneral,
			MetricKind:  MetricKindProfit,
			PeriodFrom:  "2026-02",
			PeriodTo:    "2026-02",
		},
	}

	stages := resolveSourceExecutionStages(ctx)
	want := []executionStage{
		executionStageDirectCoreMetricRange,
	}
	assertExecutionStagesEqual(t, stages, want)
}

func assertExecutionStagesEqual(t *testing.T, got, want []executionStage) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("stage count = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %s, want %s (all=%+v)", i, got[i], want[i], got)
		}
	}
}
