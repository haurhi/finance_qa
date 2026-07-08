package query

import "strings"

func shouldPreferContractAggregate(q string, intent Intent, family QueryFamily, metricKind MetricKind, cfg RuleConfig) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	if family != QueryFamilyCoreMetric {
		return false
	}
	if shouldUseContractAggregateAnalysisQuestion(q, cfg) {
		return true
	}
	if shouldUseContractFirstARAP(q) {
		return true
	}
	if shouldUseLatestRevenueContractAggregate(q, cfg) {
		return true
	}
	if intent == IntentARAPQuery || intent == IntentTaxQuery || intent == IntentAnalysis || intent == IntentHostPayload {
		return false
	}
	if shouldUseExplicitFinancialAccountQuestion(q) {
		return false
	}
	if metricKind == MetricKindUnknown || metricKind == MetricKindReceipts {
		return false
	}
	if asksExplicitNetProfit(q) {
		return false
	}
	if containsAny(q, cfg.ContractCashFallbackKeywords()) {
		return false
	}
	if shouldUseSupplierPaymentStats(q) || shouldUseHRBreakdown(q, cfg) || shouldUseReconciliation(q) {
		return false
	}
	return containsAny(q, cfg.ContractSummaryKeywords())
}

func shouldUseLatestRevenueContractAggregate(q string, cfg RuleConfig) bool {
	if shouldUseExplicitFinancialAccountQuestion(q) {
		return false
	}
	if !containsAny(q, cfg.MetricKeywords(metricKeyRevenue)) {
		return false
	}
	return hasLatestAvailableMonthToken(q)
}

func hasLatestAvailableMonthToken(q string) bool {
	return containsAny(q, []string{
		"最新可见月份", "可见月份",
		"最新完整月份", "最新完整月",
		"最新一个完整月份", "最新一个完整月",
		"最新的完整月份", "最新的完整月",
		"最新月份", "最新的月份", "最新一个月", "最新月",
		"最近完整月份", "最近完整月",
		"最近一个完整月份", "最近一个完整月",
		"最近月份", "最近一个月",
	})
}
