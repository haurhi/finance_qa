package query

import (
	"context"
	"fmt"
	"strings"
)

func (e *Engine) collectCustomerContractSummary(summary contractDimensionSummary, like string, hasSubPeriod bool, subPeriod string) (contractDimensionSummary, error) {
	totals, err := e.collectFundIncomeTotals(context.Background(), summary.PeriodFrom, summary.PeriodTo, like)
	if err != nil {
		return contractDimensionSummary{}, err
	}

	settlementAmount := round2(totals.Settlement)
	invoiceAmount := round2(totals.Invoice)
	invoiceOpenAmount := round2(totals.InvoiceOpen)
	unattributedInvoiceAmount := round2(totals.UnattributedInvoice)
	cashReceived := round2(totals.Received)
	receivableAmount := round2(totals.Receivable)
	summary.Data["book_view"] = map[string]any{
		"settlement_amount":              settlementAmount,
		"invoice_amount":                 invoiceAmount,
		"invoice_open_amount":            invoiceOpenAmount,
		"receivable_amount":              receivableAmount,
		"unattributed_invoice_amount":    unattributedInvoiceAmount,
		"unattributed_invoice_contracts": contractDimensionRowsToMaps(totals.UnattributedInvoiceContracts),
		"invoice_attribution_note":       contractInvoiceAttributionNote(unattributedInvoiceAmount, totals.UnattributedInvoiceContracts),
		"view":                           "contract_ledger",
	}
	summary.Data["cash_view"] = map[string]any{
		"received_amount": cashReceived,
		"view":            "bank_cash_collection",
	}
	if summary.Data["asked_topic"] == "receivable" {
		summary.Data["total"] = receivableAmount
		summary.Data["metric_label"] = "项目应收（应收未收）"
		summary.Data["business_basis"] = "项目收入口径：结算金额减已到账，表示应收未收。"
	}
	if hasSubPeriod {
		subTotals, err := e.collectFundIncomeTotals(context.Background(), subPeriod, subPeriod, like)
		if err != nil {
			return contractDimensionSummary{}, err
		}
		subReceipts := round2(subTotals.Received)
		summary.SubPeriod = subPeriod
		summary.Data["sub_period"] = subPeriod
		summary.Data["sub_period_receipts"] = subReceipts
		summary.CalculationLog = append(summary.CalculationLog, fmt.Sprintf("[合同维度-客户] sub_period=%s receipts=%.2f", subPeriod, subReceipts))
		summary.ExecutedSQL = append(summary.ExecutedSQL, "customer_contract_subperiod_cash: SELECT SUM(received_amount) FROM fin_fund_income + fin_fund_income_groups ... WHERE year_month = ?")
	}
	summary.CalculationLog = append(summary.CalculationLog, fmt.Sprintf("[合同维度-客户] settlement=%.2f invoice=%.2f received=%.2f", settlementAmount, invoiceAmount, cashReceived))
	summary.ExecutedSQL = append(summary.ExecutedSQL,
		"customer_contract_book: SELECT SUM(settlement_amount), SUM(invoice_amount) FROM fin_fund_income + fin_fund_income_groups ... WHERE year_month BETWEEN ? AND ?",
		"customer_contract_cash: SELECT SUM(received_amount) FROM fin_fund_income + fin_fund_income_groups ... WHERE year_month BETWEEN ? AND ?",
	)
	return summary, nil
}

func contractInvoiceAttributionNote(unattributedInvoiceAmount float64, contracts []contractDimensionRow) string {
	if round2(unattributedInvoiceAmount) <= 0 {
		return ""
	}
	contractList := contractDimensionContentList(contracts)
	if contractList == "" {
		return fmt.Sprintf("另有合并开票 %.2f 元覆盖多个合同，不能归属到当前单个合同。", round2(unattributedInvoiceAmount))
	}
	return fmt.Sprintf("另有合并开票 %.2f 元覆盖多个合同（%s），不能归属到当前单个合同。", round2(unattributedInvoiceAmount), contractList)
}

func contractDimensionRowsToMaps(rows []contractDimensionRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"contract_id":      row.ContractID,
			"customer_name":    row.CustomerName,
			"contract_content": row.ContractContent,
		})
	}
	return out
}

func contractDimensionContentList(rows []contractDimensionRow) string {
	seen := map[string]struct{}{}
	contents := make([]string, 0, len(rows))
	for _, row := range rows {
		content := strings.TrimSpace(row.ContractContent)
		if content == "" {
			content = strings.TrimSpace(row.ContractID)
		}
		if content == "" {
			continue
		}
		if _, ok := seen[content]; ok {
			continue
		}
		seen[content] = struct{}{}
		contents = append(contents, content)
	}
	return strings.Join(contents, "、")
}
