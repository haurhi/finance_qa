package query

import "financeqa/internal/accounting"

func isGenericMetricEntity(entity string) bool {
	key := normalizeEntityText(entity)
	if key == "" {
		return true
	}
	return isGenericMetricEntityWithConfig(entity, getRuleConfig())
}

func isGenericMetricEntityWithConfig(entity string, cfg RuleConfig) bool {
	key := normalizeEntityText(entity)
	if key == "" {
		return true
	}
	for _, s := range cfg.GenericMetricStopwords {
		if normalizeEntityText(s) == key {
			return true
		}
	}
	return false
}

func detectRequestedMetrics(q string) []string {
	return detectRequestedMetricsWithConfig(q, getRuleConfig())
}

func detectRequestedMetricsWithConfig(q string, cfg RuleConfig) []string {
	metrics := make([]string, 0, 3)
	if containsAny(q, []string{"应收"}) && containsAny(q, []string{"应付"}) &&
		!containsAny(q, []string{"已开票未回款", "已开票未收款", "已收票未付款", "收到发票未付款"}) {
		return []string{"应收", "应付"}
	}
	if side := detectContractARAPMetric(q); side != "" {
		return []string{side}
	}
	if shouldUseContractReceivableOutstandingQuestion(q, cfg) {
		return []string{"应收", "已开票未回款"}
	}
	if containsAny(q, cfg.MetricKeywords(metricKeyRevenue)) {
		metrics = append(metrics, "收入")
	}
	if containsAny(q, cfg.MetricKeywords(metricKeyCost)) {
		metrics = append(metrics, "成本")
	}
	if containsAny(q, cfg.MetricKeywords(metricKeyProfit)) {
		metrics = append(metrics, "利润")
	}
	if len(metrics) == 0 {
		if containsAny(q, cfg.IntentKeywords(IntentMonthlySummary)) {
			return []string{"收入", "成本", "利润"}
		}
		metrics = append(metrics, detectCoreMetricWithConfig(q, cfg))
	}
	return metrics
}

func detectContractARAPMetric(q string) string {
	switch {
	case looksLikeSupplierInvoiceUnpaidRosterQuestion(q):
		return "已收票未付款"
	case looksLikeSupplierInvoiceUnpaidQuestion(q):
		return "已收票未付款"
	case looksLikeRevenueInvoiceOpenQuestion(q):
		return "已开票未回款"
	case containsAny(q, []string{"含未开票未付款", "未开票未付款", "未开票未回款", "应收未收", "没收回来", "未收回", "还没收回来", "没有收回来"}):
		return "应收"
	case containsAny(q, []string{"客户未付款", "客户没付款", "客户未支付"}):
		return "已开票未回款"
	case containsAny(q, []string{"已收发票未付款", "已收票未付款", "收到发票未付款", "供应商发票未付款"}):
		return "已收票未付款"
	case containsAny(q, []string{"已开发票未收款", "已开票未收款", "已开票未回款", "已开票未付款"}):
		return "已开票未回款"
	case looksLikeProjectPayableUnpaidQuestion(q):
		return "应付"
	case containsAny(q, []string{"应付账款", "应付", "已收发票未付款", "已收票未付款", "收到发票未付款", "供应商发票未付款"}):
		return "应付"
	case containsAny(q, []string{"应收账款", "应收", "已开发票未收款", "已开票未收款", "已开票未回款", "已开票未付款"}):
		return "应收"
	default:
		return ""
	}
}

func looksLikeRevenueInvoiceOpenQuestion(q string) bool {
	if !containsAny(q, []string{"已开票", "已开发票", "开了票", "开发票"}) {
		return false
	}
	if containsAny(q, []string{"收票", "已收票", "收到发票", "供应商发票"}) {
		return false
	}
	return containsAny(q, []string{
		"未回款", "未收款", "未到账",
		"还没回款", "还没收款", "还没到账",
		"没回款", "没收款", "没到账",
		"没有回款", "没有收款", "没有到账",
	})
}

func looksLikeSupplierInvoiceUnpaidQuestion(q string) bool {
	if containsAny(q, []string{"未回款", "未收款", "客户未付款", "客户没付款", "客户未支付", "没收回来", "未收回", "还没收回来", "没有收回来"}) {
		return false
	}
	if !containsAny(q, []string{"未付款", "未支付", "未付", "没付款", "没支付", "没有付款", "没有支付"}) {
		return false
	}
	if containsAny(q, []string{"已收票", "收票", "收到发票", "已收发票", "供应商发票"}) {
		return true
	}
	if containsAny(q, []string{"已开票未付款", "已开发票未付款", "开票未付款", "发票未付款"}) {
		return containsAny(q, []string{"供应商", "采购", "成本", "应付", "合同", "项目"})
	}
	return false
}

func looksLikeSupplierInvoiceUnpaidRosterQuestion(q string) bool {
	if containsAny(q, []string{"应收"}) {
		return false
	}
	if !looksLikeSupplierInvoiceUnpaidQuestion(q) {
		return false
	}
	if !containsAny(q, []string{"项目", "合同", "供应商", "采购", "成本"}) {
		return false
	}
	return containsAny(q, []string{
		"项目及对应金额",
		"项目和金额",
		"项目分别",
		"每个未付款项目",
		"每个项目",
		"哪些项目",
		"有哪些项目",
		"列一下",
		"分别是多少",
		"对应金额",
		"金额分别",
	})
}

func looksLikeProjectPayableUnpaidQuestion(q string) bool {
	if !containsAny(q, []string{"未付款", "未支付", "未付", "没付款", "没支付", "没有付款", "没有支付", "应付未付"}) {
		return false
	}
	if containsAny(q, []string{"未回款", "未收款", "客户未付款", "客户没付款", "客户未支付", "没收回来", "未收回", "还没收回来", "没有收回来", "应收"}) {
		return false
	}
	if looksLikeSupplierInvoiceUnpaidQuestion(q) || containsAny(q, []string{"已开票未回款", "已开票未收款", "已开发票未收款"}) {
		return false
	}
	return containsAny(q, []string{"项目成本", "成本口径", "项目口径", "项目", "合同", "供应商", "采购", "应付", "成本"})
}

func looksLikeProjectInvoiceOpenQuestion(q string) bool {
	if !containsAny(q, []string{"已开票", "已开发票", "开了票", "开票", "已出票"}) {
		return false
	}
	if !containsAny(q, []string{"未回款", "未收款", "未收", "没收", "还没回", "但还没回", "但未回", "没回款", "没收回", "未回来"}) {
		return false
	}
	return containsAny(q, []string{"项目", "合同", "金额", "多少", "合计", "总计", "一共"})
}

func detectCoreMetric(q string) string {
	return detectCoreMetricWithConfig(q, getRuleConfig())
}

func detectCoreMetricWithConfig(q string, cfg RuleConfig) string {
	switch {
	case containsAny(q, cfg.MetricKeywords(metricKeyProfit)):
		return "利润"
	case containsAny(q, cfg.MetricKeywords(metricKeyCost)):
		return "成本"
	default:
		return "收入"
	}
}

func pickMetricValue(metric string, dual *accounting.DualPerspective) (float64, float64) {
	switch metric {
	case "利润":
		return dual.Cash.Net, dual.Accrual.Profit
	case "成本":
		return dual.Cash.Expense, dual.Accrual.TotalCost
	case "销售额", "收入":
		return dual.Cash.Income, dual.Accrual.Revenue
	default:
		return dual.Cash.Income, dual.Accrual.Revenue
	}
}
