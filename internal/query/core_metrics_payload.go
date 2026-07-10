package query

import (
	"fmt"
	"strings"

	"financeqa/internal/accounting"
)

func buildCoreMetricBookView(book monthlyBookView, displayedBookProfit float64) map[string]any {
	return map[string]any{
		"营业收入":    book.Revenue,
		"营业成本及费用": book.TotalCost,
		"营业外收入":   book.NonOperatingIncome,
		"营业外支出":   book.NonOperatingExpense,
		"账面利润":    displayedBookProfit,
		"净利润":     book.NetProfit,
	}
}

func buildCoreMetricCashFlowSummary(cash *accounting.CashPerspective) map[string]any {
	if cash == nil {
		return map[string]any{
			"现金流入": float64(0),
			"现金流出": float64(0),
			"净现金流": float64(0),
		}
	}
	return map[string]any{
		"现金流入": cash.Income,
		"现金流出": cash.Expense,
		"净现金流": cash.Net,
	}
}

func metricValueFromBook(metric string, book monthlyBookView) float64 {
	if strings.TrimSpace(metric) == "净利润" {
		return book.NetProfit
	}
	switch metricDisplayName(metric) {
	case "利润":
		return book.Profit
	case "成本":
		return book.TotalCost
	default:
		return book.Revenue
	}
}

func buildAccrualCoreMetricsMessage(period string, requestedMetrics []string, book monthlyBookView) string {
	if len(requestedMetrics) <= 1 {
		if strings.TrimSpace(firstMetricOrDefault(requestedMetrics, "收入")) == "净利润" {
			return fmt.Sprintf("%s 账上净利润 %.2f 元（利润 %.2f 元，所得税 %.2f 元）。", period, book.NetProfit, book.Profit, book.IncomeTax)
		}
		switch metricDisplayName(firstMetricOrDefault(requestedMetrics, "收入")) {
		case "利润":
			return fmt.Sprintf("%s 账面利润 %.2f 元（收入 %.2f 元，成本及费用 %.2f 元，营业外收入 %.2f 元，营业外支出 %.2f 元）。", period, book.Profit, book.Revenue, book.TotalCost, book.NonOperatingIncome, book.NonOperatingExpense)
		case "成本":
			return fmt.Sprintf("%s 账上成本及费用 %.2f 元。", period, book.TotalCost)
		default:
			return fmt.Sprintf("%s 账上收入 %.2f 元。", period, book.Revenue)
		}
	}
	if containsString(requestedMetrics, "净利润") {
		return fmt.Sprintf("%s 账上收入 %.2f 元，成本及费用 %.2f 元，净利润 %.2f 元（利润 %.2f 元，所得税 %.2f 元）。", period, book.Revenue, book.TotalCost, book.NetProfit, book.Profit, book.IncomeTax)
	}
	return fmt.Sprintf("%s 账上收入 %.2f 元，成本及费用 %.2f 元，利润 %.2f 元（含营业外收支）。", period, book.Revenue, book.TotalCost, book.Profit)
}

func asksExplicitNetProfit(q string) bool {
	q = strings.TrimSpace(q)
	return strings.Contains(q, "净利润") || strings.Contains(q, "净利")
}
