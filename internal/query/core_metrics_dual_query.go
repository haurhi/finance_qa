package query

import (
	"fmt"
	"strings"
)

func (e *Engine) queryDualPerspectiveForCoreMetric(question, from, to string) Result {
	cfg := e.currentRuleConfig()
	request := resolveCoreMetricRequestWithConfig(question, metricDisplayName(detectCoreMetricWithConfig(question, cfg)), cfg)
	coverage := e.resolveCoreMetricCoverageForRequest(from, to, request)
	requestedPeriodLabel := displayPeriod(from, to)
	if !coverage.HasData && contractAggregateCanUseLatestAvailablePeriod(question) {
		if fallback := latestAvailableCoreMetricCoverage(coverage); fallback.HasData {
			coverage = fallback
		}
	}
	if !coverage.HasData {
		cutoff := coverage.AvailableTo
		if strings.TrimSpace(cutoff) == "" {
			cutoff = "当前已入库账期"
		}
		return Result{
			Success:      true,
			Message:      "你问的是 " + requestedPeriodLabel + "，但当前账务数据仅到 " + cutoff + "，这个期间还没有可用数据。",
			AnswerMethod: "sql",
			Data: map[string]any{
				"period":           requestedPeriodLabel,
				"requested_period": requestedPeriodLabel,
				"data_ready":       false,
				"coverage": map[string]any{
					"requested_from": from,
					"requested_to":   to,
					"actual_from":    coverage.ActualFrom,
					"actual_to":      coverage.ActualTo,
					"available_to":   coverage.AvailableTo,
					"truncated":      coverage.Truncated,
					"data_ready":     false,
				},
			},
			ExecutedSQL: []string{
				"coverage_guard: inspect latest available period across income_statement / balance_detail / journal / bank_statement",
			},
			CalculationLogs: []string{
				fmt.Sprintf("[覆盖范围] requested=%s actual=%s available_to=%s truncated=%t data_ready=false", requestedPeriodLabel, displayPeriod(coverage.ActualFrom, coverage.ActualTo), coverage.AvailableTo, coverage.Truncated),
			},
		}
	}

	e.calc.ResetTrace()
	unified, sqls, logs, err := e.getUnifiedCoreMetricsCached(coverage.ActualFrom, coverage.ActualTo)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}
	periodLabel := unified.Period
	snapshot := buildCoreMetricDualSnapshot(question, QuerySpec{
		OriginalQuestion:   question,
		NormalizedQuestion: NormalizeQuestion(question),
		QueryFamily:        QueryFamilyCoreMetric,
		MetricKind:         detectMetricKind(question, cfg),
		PeriodFrom:         from,
		PeriodTo:           to,
	}, coverage, unified)

	logs = append(logs, fmt.Sprintf("[核心指标-默认双口径] metric=%s requested=%v cash=%.2f accrual=%.2f", snapshot.Metric, snapshot.RequestedMetrics, snapshot.CashValue, snapshot.AccrualValue))
	logs = append(logs, fmt.Sprintf("[覆盖范围] requested=%s actual=%s available_to=%s truncated=%t data_ready=true", requestedPeriodLabel, periodLabel, coverage.AvailableTo, coverage.Truncated))
	sqls = appendUniqueStrings(sqls,
		"dual_perspective(cash): ComputeCashFlow over bank_statement in selected period",
		"dual_perspective(accrual): aggregate monthlyBookSummary across selected period and cross-check with income_statement.cumulative_amount when available",
		"coverage_guard: inspect latest available period across income_statement / balance_detail / journal / bank_statement",
	)
	spec := buildQuerySpec(question, e.getLatestPeriodAnchor(), cfg)
	spec.PeriodFrom = from
	spec.PeriodTo = to
	factSet := attachCoreMetricsSnapshotTrace(buildCoreMetricsFactSet(spec, coverage, unified, sqls, logs), snapshot)
	data := cloneMap(snapshot.Data)
	data["fact_sets"] = []FactSet{factSet}
	result := Result{
		Success:         true,
		Message:         snapshot.Message,
		AnswerMethod:    "sql",
		Data:            data,
		ExecutedSQL:     sqls,
		CalculationLogs: logs,
	}
	return annotateJournalTaxDisclosure(result, strings.Contains(unified.AccrualFrom, "journal"))
}

func latestAvailableCoreMetricCoverage(coverage coreMetricCoverage) coreMetricCoverage {
	latest := strings.TrimSpace(coverage.AvailableTo)
	if latest == "" {
		return coverage
	}
	coverage.ActualFrom = latest
	coverage.ActualTo = latest
	coverage.Truncated = coverage.RequestedFrom != latest || coverage.RequestedTo != latest
	coverage.HasData = true
	return coverage
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func formatStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ",") + "]"
}
