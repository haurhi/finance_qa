package query

import (
	"context"
	"fmt"
	"strings"

	"financeqa/internal/analysis"
)

func (e *Engine) queryAccrualCoreMetrics(question, from, to string) Result {
	year, month := parsePeriod(to)
	e.calc.ResetTrace()

	book, bookSource, err := e.monthlyBookSummary(year, month)
	if asksExplicitJournalOnlySource(question) {
		book, bookSource, err = e.monthlyJournalBookSummary(year, month)
	}
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}
	logs := append([]string{}, e.calc.CalculationLogs...)
	sqls := append([]string{}, e.calc.ExecutedSQLs...)
	request := resolveCoreMetricRequest(question, metricDisplayName(detectCoreMetric(question)))
	requestedMetrics := request.RequestedMetrics
	explicitNetProfit := request.ExplicitNetProfit
	metric := request.MetricLabel
	accountValue := round2(metricValueFromBook(metric, book))
	displayedBookProfit := book.Profit
	if explicitNetProfit {
		displayedBookProfit = book.NetProfit
	}

	var bridgeMap map[string]any
	if containsString(requestedMetrics, "利润") {
		if bridge, bridgeErr := analysis.AnalyzeProfitCashBridgeWithDB(context.Background(), e.db, e.Company, to); bridgeErr == nil {
			bridgeMap = bridgeToMap(&bridge)
			logs = append(logs, fmt.Sprintf("[核心指标-单口径] period=%s profit=%.2f net_profit=%.2f estimated_operating_cash=%.2f", to, book.Profit, book.NetProfit, bridge.EstimatedOperatingCash))
			sqls = appendUniqueStrings(sqls,
				"profit_cash_bridge(balance_detail): SELECT closing_debit, closing_credit FROM balance_detail WHERE ... AND period IN (?, previous_period) AND account_code IN ('1602','1122','1123','1221','2202','2203','2211','2221','2241','22210101','22210106')",
				"profit_cash_bridge(income_statement): SELECT current_amount FROM income_statement WHERE ... AND period = ? AND item_name LIKE '%净利润%'",
			)
		}
	}

	sqls, logs = appendCoreMetricBookSummaryTrace(sqls, logs, to, bookSource)
	logs = append(logs, fmt.Sprintf("[核心指标-单口径] period=%s source=%s requested=%v metric=%s account_value=%.2f", to, bookSource, requestedMetrics, metric, accountValue))

	result := Result{
		Success:         true,
		AnswerMethod:    "sql",
		Message:         buildAccrualCoreMetricsMessage(to, requestedMetrics, book),
		Data:            buildAccrualCoreMetricResultData(to, year, month, bookSource, requestedMetrics, metric, accountValue, displayedBookProfit, book, nil, bridgeMap),
		ExecutedSQL:     sqls,
		CalculationLogs: logs,
	}
	return annotateJournalTaxDisclosure(result, strings.Contains(bookSource, "journal"))
}

func (e *Engine) queryAccrualCoreMetricWithCoverage(question, from, to string) Result {
	cfg := e.currentRuleConfig()
	request := resolveCoreMetricRequestWithConfig(question, metricDisplayName(detectCoreMetricWithConfig(question, cfg)), cfg)
	coverage := e.resolveCoreMetricCoverageForRequest(from, to, request)
	if !coverage.HasData && contractAggregateCanUseLatestAvailablePeriod(question) {
		if fallback := latestAvailableCoreMetricCoverage(coverage); fallback.HasData {
			coverage = fallback
		}
	}
	if !coverage.HasData {
		cutoff := strings.TrimSpace(coverage.AvailableTo)
		if cutoff == "" {
			cutoff = "当前已入库账期"
		}
		return Result{
			Success:      true,
			Message:      "你问的是 " + displayPeriod(from, to) + "，但当前账务数据仅到 " + cutoff + "，这个期间还没有可用数据。",
			AnswerMethod: "sql",
			Data: map[string]any{
				"period":           displayPeriod(from, to),
				"requested_period": displayPeriod(from, to),
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
				"coverage_guard: inspect latest available period across income_statement / balance_detail / journal",
			},
			CalculationLogs: []string{
				fmt.Sprintf("[账务口径覆盖] requested=%s actual=%s available_to=%s truncated=%t data_ready=false", displayPeriod(from, to), displayPeriod(coverage.ActualFrom, coverage.ActualTo), coverage.AvailableTo, coverage.Truncated),
			},
		}
	}
	result := e.queryAccrualCoreMetrics(question, coverage.ActualFrom, coverage.ActualTo)
	if result.Data == nil {
		result.Data = map[string]any{}
	}
	if coverage.Truncated {
		result.Data["requested_period"] = displayPeriod(from, to)
		result.Data["period_adjusted"] = true
		result.CalculationLogs = append([]string{
			fmt.Sprintf("[账务口径覆盖] requested=%s actual=%s available_to=%s truncated=true", displayPeriod(from, to), displayPeriod(coverage.ActualFrom, coverage.ActualTo), coverage.AvailableTo),
		}, result.CalculationLogs...)
	}
	return result
}

func (e *Engine) queryAccrualProfitOnly(from, to string) Result {
	return e.queryAccrualCoreMetrics("利润", from, to)
}

func asksExplicitJournalOnlySource(q string) bool {
	return containsAny(q, []string{"按序时账口径", "序时账口径", "序时帐口径", "序时账", "序时帐", "按凭证口径", "凭证口径"})
}

func buildAccrualCoreMetricResultData(period string, year, month int, bookSource string, requestedMetrics []string, metric string, accountValue, displayedBookProfit float64, book monthlyBookView, cashFlowSummary, bridgeMap map[string]any) map[string]any {
	data := buildCoreMetricSharedResultFields(bookSource, book, displayedBookProfit, cashFlowSummary, bridgeMap, false)
	data["period"] = period
	data["period_from"] = period
	data["period_to"] = period
	data["metric"] = metric
	data["requested_metrics"] = requestedMetrics
	data["account_value"] = accountValue
	data["total"] = accountValue
	data["data_ready"] = true
	data["metrics"] = buildCoreMetricMetricsMap(book)
	data["monthly"] = buildCoreMetricMonthlyPayload(year, month, bookSource, book)
	data["query_spec_overrides"] = map[string]any{
		"period_from": period,
		"period_to":   period,
		"time_scope":  string(TimeScopeMonth),
	}
	return data
}
