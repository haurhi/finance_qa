package query

import (
	"context"
	"strings"
)

type RouteDecision struct {
	SelectedSource   string
	PrimaryTables    []string
	SupportingTables []string
	ProbeResults     []SourceProbeResult
	FallbackReason   string
}

func (e *Engine) decideBossRoute(ctx context.Context, spec QuerySpec) (QuerySpec, RouteDecision) {
	rewrite := spec.BossRewrite
	resolvedEntity := strings.TrimSpace(spec.Entity)
	alreadyContractDimension := spec.NeedsContractDimension || spec.QueryFamily == QueryFamilyContractDimension
	resolvedPeriodFrom := strings.TrimSpace(spec.PeriodFrom)
	resolvedPeriodTo := strings.TrimSpace(spec.PeriodTo)
	resolvedSubPeriod := strings.TrimSpace(spec.SubPeriod)
	if rewrite.Metric == "" || rewrite.Metric == BossMetricUnknown {
		return spec, RouteDecision{}
	}
	if shouldSkipBossProbeRouting(spec, rewrite) {
		return spec, RouteDecision{}
	}

	if rewrite.Perspective == BossPerspectiveContractFirst || rewrite.RequiresSourceProbe {
		if alreadyContractDimension {
			if resolvedEntity != "" {
				rewrite.Entity = resolvedEntity
			}
			rewrite.PeriodFrom = spec.PeriodFrom
			rewrite.PeriodTo = spec.PeriodTo
			rewrite.SubPeriod = spec.SubPeriod
			spec.BossRewrite = rewrite
		} else {
			contractAnchor := e.periodParserAnchorForQuestion(spec.NormalizedQuestion, e.getLatestContractPeriodAnchor())
			rewrite = RewriteBossQuery(spec.NormalizedQuestion, contractAnchor)
			if rewrite.Perspective == BossPerspectiveContractFirst {
				from, to := extractContractQuestionPeriods(spec.NormalizedQuestion, contractAnchor)
				if resolvedPeriodFrom != "" && resolvedPeriodTo != "" {
					from, to = resolvedPeriodFrom, resolvedPeriodTo
					rewrite.SubPeriod = resolvedSubPeriod
				}
				rewrite.PeriodFrom = from
				rewrite.PeriodTo = to
			}
			if resolvedEntity != "" {
				rewrite.Entity = resolvedEntity
			}
			spec.BossRewrite = rewrite
			if resolvedEntity == "" {
				rewrite.Entity = ""
				spec.BossRewrite = rewrite
				spec.Entity = ""
			}
			if strings.TrimSpace(rewrite.PeriodFrom) != "" && strings.TrimSpace(rewrite.PeriodTo) != "" {
				spec.PeriodFrom = rewrite.PeriodFrom
				spec.PeriodTo = rewrite.PeriodTo
				spec.SubPeriod = rewrite.SubPeriod
				spec.TimeScope = detectTimeScope(spec.NormalizedQuestion, spec.PeriodFrom, spec.PeriodTo, contractAnchor)
			}
		}
	}

	probes := e.ProbeBossSources(ctx, rewrite)
	decision := RouteDecision{ProbeResults: probes}
	if len(probes) == 0 {
		return spec, decision
	}

	first := probes[0]
	if first.Source == BossSourceContractAggregate && !first.CanAnswer && !e.hasAnyContractLedgerRows(ctx, first.PrimaryTables) {
		decision.FallbackReason = first.MissingReason
		spec.SourceConstraint = ""
		spec.PreferContractAggregate = false
		spec.RouteDecision = decision
		return spec, decision
	}
	decision.SelectedSource = first.Source
	decision.PrimaryTables = append([]string{}, first.PrimaryTables...)
	decision.SupportingTables = append([]string{}, first.SupportingTables...)
	if !first.CanAnswer && strings.TrimSpace(first.MissingReason) != "" {
		decision.FallbackReason = first.MissingReason
	}

	switch first.Source {
	case BossSourceBankStatement:
		spec.SourceConstraint = BossSourceBankStatement
		spec.PreferContractAggregate = false
		spec.NeedsContractDimension = false
	case BossSourceContractAggregate:
		spec.SourceConstraint = BossSourceContractAggregate
		spec.PerspectivePolicy = PerspectiveCashThenAccrual
		if alreadyContractDimension || shouldRouteContractProbeAsDimension(rewrite) {
			spec.QueryFamily = QueryFamilyContractDimension
			spec.NeedsContractDimension = true
			spec.PreferContractAggregate = false
		} else {
			spec.QueryFamily = QueryFamilyCoreMetric
			spec.NeedsContractDimension = false
			spec.PreferContractAggregate = true
		}
		if metricKind := metricKindFromBossMetric(rewrite.Metric); metricKind != MetricKindUnknown {
			spec.MetricKind = metricKind
		} else if rewrite.Metric == BossMetricARAP {
			requested := detectRequestedMetrics(spec.OriginalQuestion)
			if contractAggregateNeedsCostData(requested) {
				spec.MetricKind = MetricKindCost
			} else {
				spec.MetricKind = MetricKindRevenue
			}
		}
	}

	spec.RouteDecision = decision
	return spec, decision
}

func shouldSkipBossProbeRouting(spec QuerySpec, rewrite BossQueryRewrite) bool {
	if rewrite.Perspective == BossPerspectiveExplicitCash || rewrite.SourceConstraint == BossSourceBankStatement {
		return false
	}
	if spec.QueryFamily == QueryFamilyHRCost {
		return true
	}
	if spec.QueryFamily == QueryFamilyContractDetail {
		return true
	}
	if rewrite.Metric == BossMetricARAP && rewrite.Perspective == BossPerspectiveContractFirst {
		return false
	}
	switch spec.QueryFamily {
	case QueryFamilyARAP, QueryFamilyReadiness, QueryFamilyReconciliation, QueryFamilyHRCost, QueryFamilySupplierPayments:
		return true
	}
	switch rewrite.Metric {
	case BossMetricARAP, BossMetricTax, BossMetricHRCost, BossMetricHealth:
		return true
	default:
		return false
	}
}

func shouldRouteContractProbeAsDimension(rewrite BossQueryRewrite) bool {
	if rewrite.Metric == BossMetricARAP && strings.TrimSpace(rewrite.Entity) == "" {
		return false
	}
	if rewrite.Scope == BossScopeEntity && strings.TrimSpace(rewrite.Entity) != "" {
		return true
	}
	if rewrite.Scope == BossScopeContract && strings.TrimSpace(rewrite.Entity) != "" {
		return true
	}
	switch rewrite.Metric {
	case BossMetricReceipts, BossMetricPayments, BossMetricInvoice:
		return strings.TrimSpace(rewrite.Entity) != ""
	default:
		return false
	}
}

func metricKindFromBossMetric(metric BossMetric) MetricKind {
	switch metric {
	case BossMetricRevenue:
		return MetricKindRevenue
	case BossMetricCost, BossMetricPayments:
		return MetricKindCost
	case BossMetricProfit:
		return MetricKindProfit
	case BossMetricReceipts:
		return MetricKindReceipts
	default:
		return MetricKindUnknown
	}
}
