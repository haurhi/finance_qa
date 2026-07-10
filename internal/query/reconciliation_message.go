package query

import (
	"fmt"
	"math"
	"strings"

	"financeqa/internal/accounting"
	"financeqa/internal/analysis"
)

func (e *Engine) composeBossReconciliationMessage(period string, book monthlyBookView, bookSource string, cash *accounting.CashPerspective, bridge *analysis.ProfitCashBridge, highlights []counterpartySnapshot) string {
	lines := []string{
		fmt.Sprintf("%s 我拆成两层给你看：账上看收入 %.2f 元、成本及费用 %.2f 元、净利润 %.2f 元；银行卡上看收款 %.2f 元、付款 %.2f 元，净流入 %.2f 元。", period, book.Revenue, book.TotalCost, book.NetProfit, cash.Income, cash.Expense, cash.Net),
	}
	if bookSource == "income_statement" {
		bookPeriodLabel := "当期"
		if strings.Contains(period, "~") {
			bookPeriodLabel = "期间"
		}
		lines = append(lines, fmt.Sprintf("账上这组数优先取利润表%s发生额，银行卡这组数取同%s真实收付。两边不是同一个口径，所以不能直接拿来做差。", bookPeriodLabel, bookPeriodLabel))
	}
	if bridgeNarrative := buildProfitCashBridgeNarrative(bridge, "合计净现金流"); bridgeNarrative != "" {
		lines = append(lines, bridgeNarrative)
		lines = append(lines, fmt.Sprintf("补充看现金分类：过滤后的经营性现金净额 %.2f 元，已识别非经营/混合现金净额 %.2f 元。", bridge.OperatingCashNet, bridge.ExcludedCashNet))
	}
	if residualNote := reconciliationResidualMessageWithConfig(cash, bridge, e.currentRuleConfig()); residualNote != "" {
		lines = append(lines, residualNote)
	}
	if shouldAppendReconciliationHighlights(cash, bridge, highlights) {
		lines = append(lines, "再看金额较大的对手方例子：")
	}
	supportingPeriodLabel := reconciliationSupportingPeriodLabel(period)
	for _, snap := range highlights {
		if !shouldAppendReconciliationHighlights(cash, bridge, highlights) {
			break
		}
		switch snap.ComparisonBasis {
		case "historical_receipt_and_current_revenue":
			lines = append(lines, fmt.Sprintf("1. %s：本月既有到账 %.2f 元，也有账上确认收入 %.2f 元和销项税 %.2f 元。库里能确认这是“历史应收回款 + 当月新确认收入”同时出现，不能把两笔直接相减；现有库里也看不出到账对应的是哪一个结算月份。", snap.Name, snap.BankIn, snap.RevenueNet, snap.OutputVAT))
		case "vat_gap_only":
			lines = append(lines, fmt.Sprintf("1. %s：到账 %.2f 元，账上收入 %.2f 元，差额 %.2f 元主要就是销项税，不是业务多赚少赚。", snap.Name, snap.BankIn, snap.RevenueNet, snap.OutputVAT))
		case "supplier_payment_or_cost":
			lines = append(lines, fmt.Sprintf("1. %s：这是供应商相关付款和成本确认。%s 付款 %.2f 元，账上成本/费用 %.2f 元，进项税 %.2f 元，不该放进收入差异里。", snap.Name, supportingPeriodLabel, snap.BankOut, snap.BookCost+snap.BookExpense, snap.InputVAT))
		case "historical_receipt":
			lines = append(lines, fmt.Sprintf("1. %s：这笔到账 %.2f 元能确认是在冲历史应收，但数据库没有字段直接说明对应哪一个结算月份，所以最多只能说是历史回款。", snap.Name, snap.BankIn))
		case "recognized_revenue":
			lines = append(lines, fmt.Sprintf("1. %s：本月账上确认收入 %.2f 元。", snap.Name, snap.RevenueNet))
		}
	}
	if shouldAppendReconciliationHighlights(cash, bridge, highlights) {
		lines = append(lines, "如果你要继续追问“这笔到底对应哪一月结算”，下一步得补结算单、开票记录或合同台账，单靠当前数据库里的财务表不能硬判。")
	}
	return strings.Join(lines, "\n")
}

func shouldAppendReconciliationHighlights(cash *accounting.CashPerspective, bridge *analysis.ProfitCashBridge, highlights []counterpartySnapshot) bool {
	if len(highlights) == 0 {
		return false
	}
	if bridge == nil || cash == nil {
		return true
	}
	return math.Abs(reconciliationResidualGap(cash, bridge)) > 0.01
}

func shouldPromoteNominalReconciliationDifference(question string) bool {
	if containsAny(question, []string{"为什么", "怎么回事"}) {
		return false
	}
	return containsAny(question, []string{"对比", "比较", "差多少", "差了多少", "相差", "差额", "差异"})
}

func reconciliationNominalDifference(book monthlyBookView, cash *accounting.CashPerspective) float64 {
	if cash == nil {
		return round2(book.NetProfit)
	}
	return round2(book.NetProfit - cash.Net)
}

func withReconciliationNominalDifferenceLine(message string, book monthlyBookView, cash *accounting.CashPerspective, nominalDifference float64) string {
	if cash == nil {
		return message
	}
	line := fmt.Sprintf("按名义差额看：账上净利润 %.2f 元 - 银行净流入 %.2f 元 = %.2f 元；这个差额只作对账入口，不能直接等同经营好坏。", book.NetProfit, cash.Net, nominalDifference)
	message = strings.TrimSpace(message)
	if message == "" {
		return line
	}
	parts := strings.SplitN(message, "\n", 2)
	if len(parts) == 1 {
		return parts[0] + "\n" + line
	}
	return parts[0] + "\n" + line + "\n" + parts[1]
}

func reconciliationResidualGap(cash *accounting.CashPerspective, bridge *analysis.ProfitCashBridge) float64 {
	if bridge == nil || cash == nil {
		return 0
	}
	return round2(cash.Net - bridge.EstimatedOperatingCash)
}

func reconciliationResidualMessage(cash *accounting.CashPerspective, bridge *analysis.ProfitCashBridge) string {
	return reconciliationResidualMessageWithConfig(cash, bridge, getRuleConfig())
}

func reconciliationResidualMessageWithConfig(cash *accounting.CashPerspective, bridge *analysis.ProfitCashBridge, cfg RuleConfig) string {
	unresolved := reconciliationResidualGap(cash, bridge)
	if unresolved == 0 {
		return ""
	}
	if shouldEscalateReconciliationResidualWithConfig(unresolved, cfg) {
		return fmt.Sprintf("当前这版利润调现金桥和银行卡净额还差 %.2f 元。当前只能先解释已识别部分，剩余 %.2f 元待核对，说明还有未归因的非经营/混合现金或科目口径差，下面的对手方例子只作辅助定位，不能直接当成完整对账结论。", unresolved, math.Abs(unresolved))
	}
	return fmt.Sprintf("当前这版利润调现金桥和银行卡净额还差 %.2f 元，说明还有未归因的非经营/混合现金或科目口径差，下面的对手方例子只作辅助定位。", unresolved)
}

func shouldEscalateReconciliationResidual(residual float64) bool {
	return shouldEscalateReconciliationResidualWithConfig(residual, getRuleConfig())
}

func shouldEscalateReconciliationResidualWithConfig(residual float64, cfg RuleConfig) bool {
	threshold := cfg.ReconciliationResidualGapEscalationThreshold()
	if threshold <= 0 {
		return false
	}
	return math.Abs(residual) >= threshold
}
