package query

import (
	"fmt"
	"strings"
)

func buildContractDimensionMessage(summary contractDimensionSummary) string {
	bookView, _ := summary.Data["book_view"].(map[string]any)
	cashView, _ := summary.Data["cash_view"].(map[string]any)
	askedTopic := anyToString(summary.Data["asked_topic"])

	if askedTopic == "content" {
		return buildContractContentMessage(summary)
	}
	if askedTopic == "profit" {
		return buildContractProfitMessage(summary, bookView, cashView)
	}

	switch summary.Role {
	case "customer_contract":
		invoiceNote := anyToString(bookView["invoice_attribution_note"])
		if invoiceNote != "" && askedTopic == "invoice" {
			return fmt.Sprintf("[%s] %s 财务口径：可归属到当前合同的开票 %.2f 元。%s合同台账结算 %.2f 元。现金口径：%s。",
				summary.Entity,
				summary.Period,
				anyToFloat64(bookView["invoice_amount"]),
				invoiceNote,
				anyToFloat64(bookView["settlement_amount"]),
				buildCustomerContractCashSummary(anyToFloat64(cashView["received_amount"]), summary.SubPeriod, anyToFloat64(summary.Data["sub_period_receipts"])))
		}
		if askedTopic == "receivable" {
			return fmt.Sprintf("[%s] %s 应收未收 %.2f 元（按各账期结算金额减到账金额后的未收部分汇总）。补充合同台账结算 %.2f 元、实际到账 %.2f 元；其中已开票未回款 %.2f 元，开票 %.2f 元。",
				summary.Entity,
				summary.Period,
				anyToFloat64(bookView["receivable_amount"]),
				anyToFloat64(bookView["settlement_amount"]),
				anyToFloat64(cashView["received_amount"]),
				anyToFloat64(bookView["invoice_open_amount"]),
				anyToFloat64(bookView["invoice_amount"]))
		}
		message := fmt.Sprintf("[%s] %s 先看现金口径：%s。再看财务口径：合同台账结算 %.2f 元，开票 %.2f 元。",
			summary.Entity,
			summary.Period,
			buildCustomerContractCashSummary(anyToFloat64(cashView["received_amount"]), summary.SubPeriod, anyToFloat64(summary.Data["sub_period_receipts"])),
			anyToFloat64(bookView["settlement_amount"]),
			anyToFloat64(bookView["invoice_amount"]))
		if invoiceNote != "" {
			message += invoiceNote
		}
		return message
	case "supplier_contract":
		if askedTopic == "payable" {
			return fmt.Sprintf("[%s] %s 项目应付（应付未付/未付款）：应付未付 %.2f 元。补充项目成本 %.2f 元、已付款 %.2f 元；现金口径实际付款 %.2f 元。",
				summary.Entity,
				summary.Period,
				anyToFloat64(bookView["payable_amount"]),
				anyToFloat64(bookView["contract_cost"]),
				anyToFloat64(bookView["contract_paid"]),
				anyToFloat64(cashView["cash_paid_amount"]))
		}
		return fmt.Sprintf("[%s] %s 先看现金口径：实际付款 %.2f 元。再看财务口径：合同成本 %.2f 元。",
			summary.Entity,
			summary.Period,
			anyToFloat64(cashView["cash_paid_amount"]),
			anyToFloat64(bookView["contract_cost"]))
	case "mixed_contract":
		if askedTopic == "payable" {
			return fmt.Sprintf("[%s] %s 项目应付（应付未付/未付款）：应付未付 %.2f 元。补充项目成本 %.2f 元、已付款 %.2f 元；现金口径付款 %.2f 元。",
				summary.Entity,
				summary.Period,
				anyToFloat64(bookView["payable_amount"]),
				anyToFloat64(bookView["cost_settlement"]),
				anyToFloat64(bookView["contract_paid"]),
				anyToFloat64(cashView["cash_paid_amount"]))
		}
		return fmt.Sprintf("[%s] %s 先看现金口径：到账 %.2f 元、付款 %.2f 元。再看财务口径：收入结算 %.2f 元、合同成本 %.2f 元。",
			summary.Entity,
			summary.Period,
			anyToFloat64(cashView["received_amount"]),
			anyToFloat64(cashView["cash_paid_amount"]),
			anyToFloat64(bookView["revenue_settlement"]),
			anyToFloat64(bookView["cost_settlement"]))
	default:
		return fmt.Sprintf("[%s] %s 合同数据已汇总。", summary.Entity, summary.Period)
	}
}

func buildContractContentMessage(summary contractDimensionSummary) string {
	contents := make([]string, 0, len(summary.Contracts))
	seen := map[string]struct{}{}
	for _, contract := range summary.Contracts {
		content := anyToString(contract["contract_content"])
		if content == "" {
			content = anyToString(contract["customer_name"])
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
	if len(contents) == 0 {
		return fmt.Sprintf("[%s] 暂未匹配到合同内容。", summary.Entity)
	}
	return fmt.Sprintf("[%s] 匹配到 %d 份合同，合同内容：%s。", summary.Entity, len(contents), strings.Join(contents, "；"))
}

func buildContractProfitMessage(summary contractDimensionSummary, bookView, cashView map[string]any) string {
	switch summary.Role {
	case "customer_contract":
		return fmt.Sprintf("[%s] %s 当前合同台账只匹配到收入/回款，未匹配到合同成本，暂不能直接给完整合同利润。先看现金口径：实际到账 %.2f 元。再看经营口径：合同结算 %.2f 元，开票 %.2f 元。",
			summary.Entity,
			summary.Period,
			anyToFloat64(cashView["received_amount"]),
			anyToFloat64(bookView["settlement_amount"]),
			anyToFloat64(bookView["invoice_amount"]))
	case "supplier_contract":
		return fmt.Sprintf("[%s] %s 这是供应商合同，只有成本/付款，没有营收，合同利润不适用。先看现金口径：实际付款 %.2f 元。再看经营口径：合同成本 %.2f 元。",
			summary.Entity,
			summary.Period,
			anyToFloat64(cashView["cash_paid_amount"]),
			anyToFloat64(bookView["contract_cost"]))
	case "mixed_contract":
		cashReceived := anyToFloat64(cashView["received_amount"])
		cashPaid := anyToFloat64(cashView["cash_paid_amount"])
		bookRevenue := anyToFloat64(bookView["revenue_settlement"])
		bookCost := anyToFloat64(bookView["cost_settlement"])
		return fmt.Sprintf("[%s] %s 先看现金口径：净回款 %.2f 元（到账 %.2f 元 - 付款 %.2f 元）。再看经营口径：合同利润 %.2f 元（结算收入 %.2f 元 - 合同成本 %.2f 元）。",
			summary.Entity,
			summary.Period,
			round2(cashReceived-cashPaid),
			cashReceived,
			cashPaid,
			round2(bookRevenue-bookCost),
			bookRevenue,
			bookCost)
	}
	return fmt.Sprintf("[%s] %s 合同数据已汇总。", summary.Entity, summary.Period)
}

func buildCustomerContractCashSummary(cashReceived float64, subPeriod string, subReceipts float64) string {
	parts := []string{fmt.Sprintf("实际到账 %.2f 元", cashReceived)}
	if strings.TrimSpace(subPeriod) != "" {
		parts = append(parts, fmt.Sprintf("其中%s到账 %.2f 元", displaySubPeriodLabel(subPeriod), subReceipts))
	}
	return strings.Join(parts, "，")
}
