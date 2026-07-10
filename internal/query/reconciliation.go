package query

import (
	"context"
	"fmt"
	"strings"

	"financeqa/internal/analysis"
)

func shouldUseReconciliation(q string) bool {
	if containsAny(q, []string{"对比", "比较", "差多少", "差了多少", "相差", "差额"}) &&
		containsAny(q, []string{"银行", "银行卡", "银行流水", "现金", "现金流", "净流入", "净流出", "净现金流"}) &&
		containsAny(q, []string{"账上", "账面", "序时账", "会计账", "利润", "净利润", "净利"}) {
		return true
	}
	if containsAny(q, []string{"为什么", "怎么回事", "差异", "原因", "拆开看", "看看具体", "具体差异", "实际利润"}) {
		return containsAny(q, []string{"利润", "营收", "收入", "销售额", "成本"})
	}
	if strings.Contains(q, "营收情况") {
		return containsAny(q, []string{"为什么", "怎么回事", "差异", "拆开", "账上", "银行卡", "现金", "利润", "成本", "费用", "到账", "回款", "收款"})
	}
	if strings.Contains(q, "营收") && strings.Contains(q, "怎么样") {
		return true
	}
	return false
}

func (e *Engine) queryReconciliation(question, from, to string) Result {
	requestedPeriodLabel := displayPeriod(from, to)
	actualFrom, actualTo, adjusted, coverageNote := e.resolveReconciliationPeriod(question, from, to)
	from, to = actualFrom, actualTo
	periodLabel := displayPeriod(from, to)
	e.calc.ResetTrace()

	book, bookSource, _, bookSQLs, bookLogs, err := e.bookSummaryForRange(from, to)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}
	cash, err := e.calc.ComputeCashFlow(e.Company, from, to)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}

	highlights := e.collectReconciliationHighlights(from, to, 8, 4)

	logs := append([]string{}, e.calc.CalculationLogs...)
	if coverageNote != "" {
		logs = append([]string{coverageNote}, logs...)
	}
	logs = append(logs, bookLogs...)
	logs = append(logs,
		fmt.Sprintf("[差异解释] %s 账上收入 %.2f, 账上成本及费用 %.2f, 净利润 %.2f, 利润 %.2f (营业外收入 %.2f, 营业外支出 %.2f)", periodLabel, book.Revenue, book.TotalCost, book.NetProfit, book.Profit, book.NonOperatingIncome, book.NonOperatingExpense),
		fmt.Sprintf("[差异解释] %s 银行卡上收款 %.2f, 付款 %.2f, 净流入 %.2f", periodLabel, cash.Income, cash.Expense, cash.Net),
	)
	var bridge *analysis.ProfitCashBridge
	var bridgeMap map[string]any
	sqls := append([]string{}, e.calc.ExecutedSQLs...)
	sqls = append(sqls, bookSQLs...)
	if resolvedBridge, bridgeSQLs, bridgeLogs := e.rangeProfitCashBridge(context.Background(), from, to); len(bridgeLogs) > 0 || len(bridgeSQLs) > 0 || resolvedBridge != nil {
		logs = append(logs, bridgeLogs...)
		sqls = append(sqls, bridgeSQLs...)
		if resolvedBridge != nil {
			bridge = resolvedBridge
			bridgeMap = bridgeToMap(bridge)
		}
	}
	for _, snap := range highlights {
		logs = append(logs, fmt.Sprintf("[对手方归因] %s role=%s basis=%s in=%.2f out=%.2f revenue=%.2f cost=%.2f expense=%.2f vat_out=%.2f vat_in=%.2f reason=%s",
			snap.Name, snap.Role, snap.ComparisonBasis, snap.BankIn, snap.BankOut, snap.RevenueNet, snap.BookCost, snap.BookExpense, snap.OutputVAT, snap.InputVAT, snap.DifferenceReason))
	}

	sqls = append(sqls,
		"reconciliation(bank_statement): SELECT counterparty_name, SUM(credit_amount), SUM(debit_amount) FROM bank_statement WHERE ... GROUP BY counterparty_name ORDER BY ABS(net) DESC",
		"reconciliation(journal): SELECT account_code, direction, amount, summary, counterparty FROM journal WHERE ... AND (summary LIKE ? OR counterparty LIKE ?) ",
	)

	msg := e.composeBossReconciliationMessage(periodLabel, book, bookSource, cash, bridge, highlights)
	data := buildReconciliationResultData(periodLabel, book, bookSource, cash, highlights, bridgeMap)
	nominalDifference := reconciliationNominalDifference(book, cash)
	annotateReconciliationFacts(data, book, cash, nominalDifference)
	if shouldPromoteNominalReconciliationDifference(question) {
		msg = withReconciliationNominalDifferenceLine(msg, book, cash, nominalDifference)
		promoteReconciliationDifferenceHeadline(data, nominalDifference)
	}
	if adjusted {
		data["period_adjusted"] = true
		data["requested_period"] = requestedPeriodLabel
	}
	data["period_from"] = from
	data["period_to"] = to
	data["query_spec_overrides"] = map[string]any{
		"period_from": from,
		"period_to":   to,
		"time_scope":  string(detectTimeScope(question, from, to, e.getLatestPeriodAnchor())),
	}

	return Result{
		Success:         true,
		Message:         msg,
		AnswerMethod:    "sql",
		Data:            data,
		ExecutedSQL:     sqls,
		CalculationLogs: logs,
	}
}

func (e *Engine) resolveReconciliationPeriod(question, from, to string) (string, string, bool, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return from, to, false, ""
	}
	if e.reconciliationHasDataForRange(question, from, to) {
		return from, to, false, ""
	}
	if !contractAggregateCanUseLatestAvailablePeriod(question) {
		return from, to, false, ""
	}
	latest := e.latestCommonReconciliationPeriod(question)
	if latest == "" {
		return from, to, false, ""
	}
	actualFrom := bankCashFlowFallbackPeriodFrom(question, latest)
	if actualFrom == "" {
		actualFrom = from
	}
	if actualFrom == "" || actualFrom > latest {
		actualFrom = latest
	}
	actualTo := latest
	if actualFrom > actualTo {
		actualFrom = actualTo
	}
	if !e.reconciliationHasDataForRange(question, actualFrom, actualTo) {
		return from, to, false, ""
	}
	if actualFrom == from && actualTo == to {
		return from, to, false, ""
	}
	note := fmt.Sprintf("[账银对比覆盖] requested=%s actual=%s reason=请求期间缺少账务或银行流水数据，改用两类数据共同可用的最新期间",
		displayPeriod(from, to),
		displayPeriod(actualFrom, actualTo))
	return actualFrom, actualTo, true, note
}

func (e *Engine) reconciliationHasDataForRange(question, from, to string) bool {
	if !e.bankStatementHasAmountRows(from, to) {
		return false
	}
	if latestBank := e.latestBankStatementPeriodWithAmountRows(); latestBank != "" && to > latestBank {
		return false
	}
	request := resolveCoreMetricRequestWithConfig(question, "利润", e.currentRuleConfig())
	coverage := e.resolveCoreMetricCoverageForRequest(from, to, request)
	return coverage.HasData && !coverage.Truncated
}

func (e *Engine) latestCommonReconciliationPeriod(question string) string {
	bankLatest := e.latestBankStatementPeriodWithAmountRows()
	if bankLatest == "" {
		return ""
	}
	request := resolveCoreMetricRequestWithConfig(question, "利润", e.currentRuleConfig())
	bookLatest := e.latestAvailableFinancialPeriodForRequest(request)
	if bookLatest == "" {
		return ""
	}
	if bookLatest < bankLatest {
		return bookLatest
	}
	return bankLatest
}
