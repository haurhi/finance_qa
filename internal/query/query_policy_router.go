package query

import (
	"regexp"
	"strings"
	"time"
)

func detectMetricKind(q string, cfg RuleConfig) MetricKind {
	switch {
	case shouldUseContractFirstARAP(q) && contractAggregateNeedsCostData([]string{detectContractARAPMetric(q)}):
		return MetricKindCost
	case shouldUseContractFirstARAP(q):
		return MetricKindRevenue
	case shouldUseContractMarginAnalysisQuestion(q, cfg):
		return MetricKindProfit
	case containsAny(q, []string{"回款", "到账", "收款"}):
		return MetricKindReceipts
	case containsAny(q, cfg.MetricKeywords(metricKeyProfit)):
		return MetricKindProfit
	case containsAny(q, cfg.MetricKeywords(metricKeyCost)):
		return MetricKindCost
	case containsAny(q, cfg.MetricKeywords(metricKeyRevenue)):
		return MetricKindRevenue
	default:
		return MetricKindUnknown
	}
}

func detectTimeScope(q, from, to string, anchor time.Time) TimeScope {
	switch {
	case strings.Contains(q, "季度") || regexp.MustCompile(`Q\s*[1-4]`).MatchString(strings.ToUpper(q)):
		return TimeScopeQuarter
	case strings.Contains(q, "上半年") || strings.Contains(q, "下半年"):
		return TimeScopeHalfYear
	case strings.Contains(q, "全年") || strings.Contains(q, "全年度") || strings.Contains(q, "整年") || strings.Contains(q, "年度"):
		return TimeScopeYearFull
	case strings.Contains(q, "今年") || strings.Contains(q, "本年") || strings.Contains(q, "累计") || strings.Contains(q, "年内"):
		return TimeScopeYearToDate
	case from != "" && to != "" && from != to:
		return TimeScopeCustom
	default:
		return TimeScopeMonth
	}
}

func detectPerspectivePolicy(q string, intent Intent, needsContractDimension bool, cfg RuleConfig) PerspectivePolicy {
	if needsContractDimension {
		return PerspectiveCashThenAccrual
	}
	if shouldUseContractFirstARAP(q) {
		return PerspectiveCashThenAccrual
	}
	if intent == IntentARAPQuery || isOpeningPeriodQuestion(q) {
		return PerspectiveOfficialThenEvidence
	}
	if containsAny(q, cfg.MetricKeywords(metricKeyRevenue)) || containsAny(q, cfg.MetricKeywords(metricKeyCost)) || containsAny(q, []string{"回款", "到账", "收款"}) {
		return PerspectiveCashThenAccrual
	}
	if containsAny(q, cfg.MetricKeywords(metricKeyProfit)) {
		return PerspectiveAccrualOnly
	}
	return PerspectiveUnknown
}

func isOpeningPeriodQuestion(q string) bool {
	return isARAPQuestion(q) && shouldUseOfficialARAPQuestion(q)
}

func isAuthoritativeSourceQuestion(q string) bool {
	return isARAPQuestion(q) && shouldUseOfficialARAPQuestion(q)
}

func isARAPQuestion(q string) bool {
	return containsAny(q, []string{
		"应收账款", "应付账款", "应收/应付", "应收", "应付",
		"应收未收", "客户未付款", "客户没付款", "客户未支付",
		"含未开票未付款", "未开票未付款", "未开票未回款",
		"已收发票未付款", "已收票未付款", "收到发票未付款",
		"已开发票未收款", "已开票未收款", "已开票未回款", "已开票未付款",
	})
}

func shouldUseOfficialARAPQuestion(q string) bool {
	return containsAny(q, []string{
		"科目余额", "发生额及余额", "余额表", "资产负债表",
		"财务账", "会计账", "报表口径", "账上",
		"序时账", "凭证",
		"从账上看", "账上看", "官方余额表",
	}) || looksLikeBalanceSheetARAPQuestion(q)
}

func shouldUseContractFirstARAP(q string) bool {
	return isARAPQuestion(q) && !shouldUseOfficialARAPQuestion(q)
}
