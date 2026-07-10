package query

import (
	"financeqa/internal/accounting"
)

func buildReconciliationHighlightMaps(highlights []counterpartySnapshot) []map[string]any {
	highlightMaps := make([]map[string]any, 0, len(highlights))
	for _, snap := range highlights {
		highlightMaps = append(highlightMaps, map[string]any{
			"name":                      snap.Name,
			"role":                      snap.Role,
			"bank_in":                   round2(snap.BankIn),
			"bank_out":                  round2(snap.BankOut),
			"ar_decrease":               round2(snap.ARDecrease),
			"ar_increase":               round2(snap.ARIncrease),
			"ap_decrease":               round2(snap.APDecrease),
			"ap_increase":               round2(snap.APIncrease),
			"prepayment_increase":       round2(snap.PrepaymentIncrease),
			"prepayment_cleared":        round2(snap.PrepaymentCleared),
			"revenue_net":               round2(snap.RevenueNet),
			"output_vat":                round2(snap.OutputVAT),
			"input_vat":                 round2(snap.InputVAT),
			"book_cost":                 round2(snap.BookCost),
			"book_expense":              round2(snap.BookExpense),
			"comparison_basis":          snap.ComparisonBasis,
			"difference_reason":         snap.DifferenceReason,
			"evidence_level":            string(snap.EvidenceLevel),
			"requires_month_disclosure": snap.RequiresMonthDisclosure,
			"support":                   append([]string{}, snap.Support...),
		})
	}
	return highlightMaps
}

func buildReconciliationResultData(period string, book monthlyBookView, bookSource string, cash *accounting.CashPerspective, highlights []counterpartySnapshot, bridgeMap map[string]any) map[string]any {
	return map[string]any{
		"period":         period,
		"business_basis": "账上利润与银行流水双口径对账",
		"book_view":      book,
		"cash_view":      cash,
		"highlights":     buildReconciliationHighlightMaps(highlights),
		"book_source":    bookSource,
		"source_tables":  sourceTablesForReconciliation(bookSource),
		"dual_perspective": map[string]any{
			"cash": map[string]any{
				"说明":   "银行卡上看",
				"现金流入": cash.Income,
				"现金流出": cash.Expense,
				"净现金流": cash.Net,
			},
			"accrual": map[string]any{
				"说明":      "账上看",
				"营业收入":    book.Revenue,
				"营业成本及费用": book.TotalCost,
				"营业外收入":   book.NonOperatingIncome,
				"营业外支出":   book.NonOperatingExpense,
				"账面利润":    book.Profit,
			},
		},
		"difference_summary": map[string]any{
			"book_profit":        book.Profit,
			"cash_net_inflow":    cash.Net,
			"profit_cash_bridge": bridgeMap,
			"notices": []string{
				"银行卡收付和账上利润不是同一口径，差异需要拆成回款、税额、供应商付款和成本确认来看。",
				"若数据库没有结算月份字段，只能确认是历史应收回款，不能硬说对应哪一个结算月份。",
			},
		},
		"现金流入": cash.Income,
		"现金流出": cash.Expense,
		"净现金流": cash.Net,
		"账上看利润": map[string]any{
			"营业收入":    book.Revenue,
			"营业成本及费用": book.TotalCost,
			"营业外收入":   book.NonOperatingIncome,
			"营业外支出":   book.NonOperatingExpense,
			"账面利润":    book.Profit,
		},
	}
}

func annotateReconciliationFacts(data map[string]any, book monthlyBookView, cash *accounting.CashPerspective, nominalDifference float64) {
	if data == nil || cash == nil {
		return
	}
	bookProfit := round2(book.NetProfit)
	bankNetInflow := round2(cash.Net)
	data["metrics"] = map[string]any{
		"账上净利润":       bookProfit,
		"银行净流入":       bankNetInflow,
		"差异金额":        nominalDifference,
		"名义差额":        nominalDifference,
		"账上净利润-银行净流入": nominalDifference,
		"名义差额（账上净利润-银行净流入）": nominalDifference,
	}
	data["cash_profit_reconciliation"] = map[string]any{
		"period":            data["period"],
		"bank_net_flow":     bankNetInflow,
		"book_net_profit":   bookProfit,
		"difference_amount": nominalDifference,
		"formula":           "账上净利润 - 银行净流入",
	}
	if summary, ok := data["difference_summary"].(map[string]any); ok {
		summary["book_net_profit"] = bookProfit
		summary["bank_net_inflow"] = bankNetInflow
		summary["nominal_difference"] = nominalDifference
	}
	data["finance_facts"] = map[string]any{
		"required_atoms": financeMetricRequiredAtoms(
			financeMetricAtom{Label: "银行净流入", Value: bankNetInflow},
			financeMetricAtom{Label: "账上净利润", Value: bookProfit},
			financeMetricAtom{Label: "差异金额（名义差额，账上净利润-银行净流入）", Value: nominalDifference},
		),
	}
	data["explanation_hints"] = []string{
		"名义差额按账上净利润减银行净流入计算。",
		"账上利润和银行流水不是同一口径，差额只作对账入口。",
	}
}

func promoteReconciliationDifferenceHeadline(data map[string]any, nominalDifference float64) {
	if data == nil {
		return
	}
	data["headline_metric"] = "账上净利润-银行净流入名义差额"
	data["headline_amount"] = nominalDifference
}
