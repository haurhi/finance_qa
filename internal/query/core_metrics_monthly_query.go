package query

import (
	"context"
	"fmt"
	"strings"

	"financeqa/internal/accounting"
	"financeqa/internal/analysis"
)

func (e *Engine) queryMonthlySummary(question, from, to string) Result {
	year, month := parsePeriod(to)
	e.calc.ResetTrace()

	book, bookSource, err := e.monthlyBookSummary(year, month)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}
	is, _ := e.calc.ComputeIncomeStatement(e.Company, year, month)
	cash, _ := e.calc.ComputeCashFlow(e.Company, from, to)
	logs := append([]string{}, e.calc.CalculationLogs...)
	sqls := append([]string{}, e.calc.ExecutedSQLs...)
	var bridgeMap map[string]any
	if bridge, bridgeErr := analysis.AnalyzeProfitCashBridgeWithDB(context.Background(), e.db, e.Company, to); bridgeErr == nil {
		bridgeMap = bridgeToMap(&bridge)
		logs = append(logs, fmt.Sprintf("[利润调现金桥] period=%s estimated_operating_cash=%.2f bank_net_cash=%.2f non_operating_delta=%.2f", to, bridge.EstimatedOperatingCash, bridge.BankNetCash, bridge.NonOperatingCashDelta))
		sqls = appendUniqueStrings(sqls,
			"profit_cash_bridge(balance_detail): SELECT closing_debit, closing_credit FROM balance_detail WHERE ... AND period IN (?, previous_period) AND account_code IN ('1602','1122','1123','1221','2202','2203','2211','2221','2241','22210101','22210106')",
			"profit_cash_bridge(income_statement): SELECT current_amount FROM income_statement WHERE ... AND period = ? AND item_name LIKE '%净利润%'",
		)
	}
	sqls, logs = appendCoreMetricBookSummaryTrace(sqls, logs, to, bookSource)

	revenue := book.Revenue
	expense := book.TotalCost
	mainMsg := fmt.Sprintf("%s 月度经营分析：账上收入 %.2f 元，成本及费用 %.2f 元，利润 %.2f 元；同时银行卡收款 %.2f 元、付款 %.2f 元。", to, revenue, expense, book.Profit, cash.Income, cash.Expense)
	if revenue == 0 && expense == 0 && book.Profit == 0 {
		logs = append(logs, fmt.Sprintf("[智能回溯] %s 当月无经营记账，正在为您还原年度累计经营体量...", to))
		if month > 1 {
			mainMsg = fmt.Sprintf("%s 暂无经营数据。%d年1月以来（YTD）累计：收入 %.2f, 支出 %.2f, 累计利润 %.2f", to, year, is.Revenue, is.Cost, is.NetProfit)
			logs = append(logs, fmt.Sprintf("[审计结论] 虽当月静默，但年度累计体量已达 %.2f 万元", is.Revenue/10000.0))
		} else {
			mainMsg = fmt.Sprintf("%s 暂无经营数据，且为年度首月，无历史数据可回溯", to)
		}
	}

	result := Result{
		Success:         true,
		Message:         mainMsg,
		AnswerMethod:    "sql",
		Data:            buildMonthlyCoreMetricResultData(year, month, bookSource, book, is, cash, bridgeMap),
		ExecutedSQL:     sqls,
		CalculationLogs: logs,
	}
	return annotateJournalTaxDisclosure(result, strings.Contains(bookSource, "journal"))
}

func buildMonthlyCoreMetricResultData(year, month int, bookSource string, book monthlyBookView, cumulative *accounting.IncomeStatementResult, cash *accounting.CashPerspective, bridgeMap map[string]any) map[string]any {
	cashFlowSummary := buildCoreMetricCashFlowSummary(cash)
	data := buildCoreMetricSharedResultFields(bookSource, book, book.Profit, cashFlowSummary, bridgeMap, true)
	data["monthly"] = buildCoreMetricMonthlyPayload(year, month, bookSource, book)
	data["cumulative"] = cumulative
	data["cash_flow"] = cash
	return data
}

func appendCoreMetricBookSummaryTrace(sqls, logs []string, period, bookSource string) ([]string, []string) {
	sqls = appendUniqueStrings(sqls,
		"monthlyBookSummary(income_statement): SELECT item_name, current_amount FROM income_statement WHERE ... AND period = ?",
		"monthlyBookSummary(fallback_journal): ComputeMonthlyFromJournal + ComputeIncomeStatement when income_statement missing required rows",
	)
	logs = append(logs, fmt.Sprintf("[月度口径] period=%s source=%s", period, bookSource))
	return sqls, logs
}
