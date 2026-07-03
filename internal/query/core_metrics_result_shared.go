package query

import "fmt"

func buildCoreMetricSharedResultFields(bookSource string, book monthlyBookView, displayedBookProfit float64, cashFlowSummary, bridgeMap map[string]any, includeCash bool) map[string]any {
	data := map[string]any{
		"source_tables":      sourceTablesForCoreMetric(bookSource, includeCash),
		"profit_cash_bridge": bridgeMap,
		"财务做账口径(看利润)":        buildCoreMetricBookView(book, displayedBookProfit),
	}
	if includeCash {
		data["现金流入"] = cashFlowSummary["现金流入"]
		data["现金流出"] = cashFlowSummary["现金流出"]
		data["净现金流"] = cashFlowSummary["净现金流"]
		data["cash_flow"] = cashFlowSummary
	}
	return data
}

func buildCoreMetricMonthlyPayload(year, month int, bookSource string, book monthlyBookView) map[string]any {
	payload := buildCoreMetricSummaryPayload(
		formatYearMonth(year, month),
		formatYearMonth(year, month),
		bookSource,
		book,
	)
	payload["cost_detail"] = map[string]any{
		"operating_cost":  book.Cost,
		"tax_surcharge":   book.TaxSurcharge,
		"selling_expense": book.SellingExpense,
		"admin_expense":   book.AdminExpense,
		"finance_expense": book.FinanceExpense,
	}
	return payload
}

func formatYearMonth(year, month int) string {
	return fmt.Sprintf("%04d-%02d", year, month)
}
