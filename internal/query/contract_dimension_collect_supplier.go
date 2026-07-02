package query

import (
	"context"
	"fmt"
)

func (e *Engine) collectSupplierContractSummary(summary contractDimensionSummary, like string) (contractDimensionSummary, error) {
	var contractCost, contractPaid, contractPayable, contractInvoice, contractInvoiceOpen, cashPaid float64
	costTotals, err := e.collectCostSettlementTotals(context.Background(), summary.PeriodFrom, summary.PeriodTo, like)
	if err != nil {
		return contractDimensionSummary{}, err
	}
	contractCost = costTotals.Settlement
	contractPaid = costTotals.Paid
	contractPayable = costTotals.Payable
	contractInvoice = costTotals.Invoice
	contractInvoiceOpen = costTotals.InvoiceOpen
	e.db.QueryRow(`
SELECT COALESCE(SUM(debit_amount), 0)
FROM bank_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND counterparty_name LIKE ?
  AND transaction_date BETWEEN ? AND ?
`, e.Company, e.Company, like, summary.PeriodFrom+"-01", monthEndDay(summary.PeriodTo)).Scan(&cashPaid)

	contractCost = round2(contractCost)
	contractPaid = round2(contractPaid)
	contractPayable = round2(contractPayable)
	contractInvoice = round2(contractInvoice)
	contractInvoiceOpen = round2(contractInvoiceOpen)
	cashPaid = round2(cashPaid)
	summary.Data["book_view"] = map[string]any{
		"contract_cost":          contractCost,
		"contract_paid":          contractPaid,
		"payable_amount":         contractPayable,
		"invoice_amount":         contractInvoice,
		"invoiced_unpaid_amount": contractInvoiceOpen,
		"view":                   "contract_ledger",
	}
	summary.Data["cash_view"] = map[string]any{
		"cash_paid_amount": cashPaid,
		"view":             "bank_cash_payment",
	}
	summary.Data["cash_paid_amount"] = cashPaid
	if summary.Data["asked_topic"] == "payable" {
		summary.Data["total"] = contractPayable
		summary.Data["metric_label"] = "项目应付（应付未付/未付款）"
		summary.Data["business_basis"] = "项目成本口径：项目成本减已付款，表示应付未付/未付款，不按收入未回款口径。"
	}
	summary.CalculationLog = append(summary.CalculationLog, fmt.Sprintf("[合同维度-供应商] cost=%.2f contract_paid=%.2f payable=%.2f cash_paid=%.2f", contractCost, contractPaid, contractPayable, cashPaid))
	summary.ExecutedSQL = append(summary.ExecutedSQL,
		"supplier_contract_book: SELECT SUM(settlement_amount), SUM(paid_amount), SUM(invoice_amount), SUM(payable), SUM(invoiced_unpaid) FROM fin_cost_settlements + fin_cost_settlement_groups ... WHERE year_month BETWEEN ? AND ?",
		"supplier_contract_cash: SELECT SUM(debit_amount) FROM bank_statement WHERE counterparty_name LIKE ? AND transaction_date BETWEEN ? AND ?",
	)
	return summary, nil
}

func (e *Engine) collectMixedContractSummary(summary contractDimensionSummary, like string) (contractDimensionSummary, error) {
	var revenueSettlement, costSettlement, costPaid, costPayable, cashReceived, cashPaid float64
	totals, err := e.collectFundIncomeTotals(context.Background(), summary.PeriodFrom, summary.PeriodTo, like)
	if err != nil {
		return contractDimensionSummary{}, err
	}
	revenueSettlement = totals.Settlement
	cashReceived = totals.Received
	costTotals, err := e.collectCostSettlementTotals(context.Background(), summary.PeriodFrom, summary.PeriodTo, like)
	if err != nil {
		return contractDimensionSummary{}, err
	}
	costSettlement = costTotals.Settlement
	costPaid = costTotals.Paid
	costPayable = costTotals.Payable
	e.db.QueryRow(`
SELECT COALESCE(SUM(debit_amount), 0)
FROM bank_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND counterparty_name LIKE ?
  AND transaction_date BETWEEN ? AND ?
`, e.Company, e.Company, like, summary.PeriodFrom+"-01", monthEndDay(summary.PeriodTo)).Scan(&cashPaid)

	revenueSettlement = round2(revenueSettlement)
	costSettlement = round2(costSettlement)
	costPaid = round2(costPaid)
	costPayable = round2(costPayable)
	cashReceived = round2(cashReceived)
	cashPaid = round2(cashPaid)
	summary.Data["book_view"] = map[string]any{
		"revenue_settlement": revenueSettlement,
		"cost_settlement":    costSettlement,
		"contract_paid":      costPaid,
		"payable_amount":     costPayable,
		"view":               "contract_ledger",
	}
	summary.Data["cash_view"] = map[string]any{
		"received_amount":  cashReceived,
		"cash_paid_amount": cashPaid,
		"view":             "bank_cash_flow",
	}
	summary.Data["cash_paid_amount"] = cashPaid
	if summary.Data["asked_topic"] == "payable" {
		summary.Data["total"] = costPayable
		summary.Data["metric_label"] = "项目应付（应付未付/未付款）"
		summary.Data["business_basis"] = "项目成本口径：项目成本减已付款，表示应付未付/未付款，不按收入未回款口径。"
	}
	summary.CalculationLog = append(summary.CalculationLog, fmt.Sprintf("[合同维度-混合] revenue=%.2f cost=%.2f contract_paid=%.2f payable=%.2f received=%.2f cash_paid=%.2f", revenueSettlement, costSettlement, costPaid, costPayable, cashReceived, cashPaid))
	summary.ExecutedSQL = append(summary.ExecutedSQL,
		"mixed_contract_book: SELECT SUM(settlement_amount) FROM fin_fund_income/fin_cost_settlements + group tables ... WHERE year_month BETWEEN ? AND ?",
		"mixed_contract_cash: SELECT SUM(received_amount) FROM fin_fund_income JOIN fin_contracts ...; SELECT SUM(debit_amount) FROM bank_statement ...",
	)
	return summary, nil
}
