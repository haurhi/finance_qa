package query

import (
	"fmt"
	"math"
	"strings"
)

func (e *Engine) queryBankCashFlow(question, from, to string) Result {
	actualFrom, actualTo, adjusted, coverageNote := e.resolveBankCashFlowPeriod(question, from, to)
	from, to = actualFrom, actualTo
	e.calc.ResetTrace()
	cash, err := e.calc.ComputeCashFlow(e.Company, from, to)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}

	q := NormalizeQuestion(question)
	periodLabel := displayPeriod(from, to)
	message := fmt.Sprintf("%s 银行卡实际到账 %.2f 元，实际支出 %.2f 元，净增加 %.2f 元。", periodLabel, cash.Income, cash.Expense, cash.Net)
	switch {
	case asksFullBankCashFlow(q):
		message = fmt.Sprintf("%s 银行卡实际到账 %.2f 元，实际支出 %.2f 元，净增加 %.2f 元。", periodLabel, cash.Income, cash.Expense, cash.Net)
	case containsAny(q, []string{"实际支出", "支出", "付款", "支付"}):
		message = fmt.Sprintf("%s 银行卡实际支出 %.2f 元。", periodLabel, cash.Expense)
	case containsAny(q, []string{"净增加", "净流入", "净流出", "净现金流"}):
		message = fmt.Sprintf("%s 银行卡净增加 %.2f 元（实际到账 %.2f 元，实际支出 %.2f 元）。", periodLabel, cash.Net, cash.Income, cash.Expense)
	case containsAny(q, []string{"实际到账", "到账", "回款", "收款"}):
		message = fmt.Sprintf("%s 银行卡实际到账 %.2f 元。", periodLabel, cash.Income)
	}

	return Result{
		Success:      true,
		Message:      message,
		AnswerMethod: "sql",
		Data: map[string]any{
			"period":                   periodLabel,
			"period_from":              from,
			"period_to":                to,
			"period_adjusted":          adjusted,
			"bank_credit_total":        cash.Income,
			"bank_debit_total":         cash.Expense,
			"net_cash_inflow":          cash.Net,
			"cash_flow":                buildCoreMetricCashFlowSummary(cash),
			"cash_view":                buildCoreMetricCashFlowSummary(cash),
			"source_primary_tables":    []string{"fin_bank_statement"},
			"source_supporting_tables": []string{},
		},
		ExecutedSQL:     append([]string{}, e.calc.ExecutedSQLs...),
		CalculationLogs: appendBankCoverageLog(coverageNote, e.calc.CalculationLogs),
	}
}

func (e *Engine) resolveBankCashFlowPeriod(question, from, to string) (string, string, bool, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return from, to, false, ""
	}
	if e.bankStatementHasAmountRows(from, to) {
		return from, to, false, ""
	}
	if !contractAggregateCanUseLatestAvailablePeriod(question) {
		return from, to, false, ""
	}
	latest := e.latestBankStatementPeriodWithAmountRows()
	if latest == "" {
		return from, to, false, ""
	}
	fallbackFrom := bankCashFlowFallbackPeriodFrom(question, latest)
	if fallbackFrom == "" {
		fallbackFrom = latest
	}
	if !e.bankStatementHasAmountRows(fallbackFrom, latest) {
		fallbackFrom = latest
		if !e.bankStatementHasAmountRows(fallbackFrom, latest) {
			return from, to, false, ""
		}
	}
	if fallbackFrom == from && latest == to {
		return from, to, false, ""
	}
	note := fmt.Sprintf("[银行流水覆盖] requested=%s actual=%s reason=请求期间无银行流水金额记录，改用银行表最新可用期间",
		displayPeriod(from, to),
		displayPeriod(fallbackFrom, latest))
	return fallbackFrom, latest, true, note
}

func bankCashFlowFallbackPeriodFrom(question, latest string) string {
	latest = strings.TrimSpace(latest)
	if latest == "" {
		return ""
	}
	q := strings.TrimSpace(question)
	if containsAny(q, []string{"至今", "到现在", "截至", "截止", "累计", "今年", "本年", "年初"}) {
		if year, _ := parsePeriod(latest); year > 0 {
			return fmt.Sprintf("%04d-01", year)
		}
	}
	if containsAny(q, []string{"这季度", "这个季度", "本季度", "季度"}) {
		if year, month := parsePeriod(latest); year > 0 && month > 0 {
			startMonth := ((month - 1) / 3 * 3) + 1
			return fmt.Sprintf("%04d-%02d", year, startMonth)
		}
	}
	return latest
}

func appendBankCoverageLog(note string, logs []string) []string {
	out := make([]string, 0, len(logs)+1)
	if strings.TrimSpace(note) != "" {
		out = append(out, note)
	}
	return append(out, logs...)
}

func asksFullBankCashFlow(q string) bool {
	hasIncome := containsAny(q, []string{"实际到账", "到账", "回款", "收款", "流入"})
	hasExpense := containsAny(q, []string{"实际支出", "支出", "付款", "支付", "流出"})
	hasNet := containsAny(q, []string{"净增加", "净流入", "净流出", "净现金流"})
	return (hasIncome && hasExpense) || (hasNet && (hasIncome || hasExpense))
}

func (e *Engine) queryCashOnHandBalance(question, from, to string) Result {
	balancePeriod := to
	balanceSourceTable := "fin_balance_detail"
	opening, closing, ok := e.queryCashBalanceDetailOpeningClosing(balancePeriod)
	if !ok {
		if fallbackPeriod := e.latestCashBalanceDetailPeriodAtOrBefore(to); fallbackPeriod != "" {
			balancePeriod = fallbackPeriod
			opening, closing, ok = e.queryCashBalanceDetailOpeningClosing(balancePeriod)
		}
	}
	if !ok {
		balanceSourceTable = "fin_balance_sheet"
		balancePeriod = to
		opening, closing, ok = e.queryCashBalanceSheetOpeningClosing(balancePeriod)
		if !ok {
			if fallbackPeriod := e.latestCashBalancePeriodAtOrBefore(to); fallbackPeriod != "" {
				balancePeriod = fallbackPeriod
				opening, closing, ok = e.queryCashBalanceSheetOpeningClosing(balancePeriod)
			}
		}
	}
	if !ok {
		return Result{Success: false, Message: "未找到货币资金余额"}
	}
	flowFrom := from
	if strings.Contains(question, "年初") && len(balancePeriod) >= 4 {
		flowFrom = balancePeriod[:4] + "-01"
	}
	if strings.TrimSpace(flowFrom) == "" || flowFrom > balancePeriod {
		flowFrom = balancePeriod
	}
	e.calc.ResetTrace()
	cash, err := e.calc.ComputeCashFlow(e.Company, flowFrom, balancePeriod)
	if err != nil {
		return Result{Success: false, Message: err.Error()}
	}

	delta := round2(closing - opening)
	direction := "多了"
	if delta < 0 {
		direction = "少了"
	}
	periodLabel := displayPeriod(flowFrom, balancePeriod)
	message := fmt.Sprintf("%s 现金期末余额 %.2f 元，年初 %.2f 元，比年初%s %.2f 元；银行流水实际流入 %.2f 元，实际流出 %.2f 元，净流入 %.2f 元。",
		periodLabel, closing, opening, direction, math.Abs(delta), cash.Income, cash.Expense, cash.Net)

	return Result{
		Success:      true,
		Message:      message,
		AnswerMethod: "sql",
		Data: map[string]any{
			"period":                   periodLabel,
			"period_from":              flowFrom,
			"period_to":                balancePeriod,
			"account":                  "货币资金",
			"cash_opening_balance":     opening,
			"cash_closing_balance":     closing,
			"cash_balance_delta":       delta,
			"bank_credit_total":        cash.Income,
			"bank_debit_total":         cash.Expense,
			"net_cash_inflow":          cash.Net,
			"source_tables":            []string{balanceSourceTable, "fin_bank_statement"},
			"source_primary_tables":    []string{balanceSourceTable, "fin_bank_statement"},
			"source_supporting_tables": []string{},
			"query_spec_overrides": map[string]any{
				"period_from":       flowFrom,
				"period_to":         balancePeriod,
				"time_scope":        detectTimeScope(question, flowFrom, balancePeriod, e.getLatestPeriodAnchor()),
				"semantic_families": []string{"cash_balance", "bank_cash_flow", "balance_sheet"},
			},
		},
		ExecutedSQL: append([]string{
			"queryCashBalanceOpeningClosing: SELECT cash opening/closing balance from balance_detail first, fallback balance_sheet",
		}, e.calc.ExecutedSQLs...),
		CalculationLogs: append([]string{
			fmt.Sprintf("[现金余额] source=%s period=%s opening=%.2f closing=%.2f delta=%.2f", balanceSourceTable, balancePeriod, opening, closing, delta),
		}, e.calc.CalculationLogs...),
	}
}

func (e *Engine) queryCashBalanceDetailOpeningClosing(period string) (float64, float64, bool) {
	cols := e.tableColumns("balance_detail")
	if !cols["opening_debit"] || !cols["opening_credit"] || !cols["closing_debit"] || !cols["closing_credit"] {
		return 0, 0, false
	}
	var openingDebit, openingCredit, closingDebit, closingCredit float64
	err := e.db.QueryRow(`
SELECT COALESCE(SUM(opening_debit), 0), COALESCE(SUM(opening_credit), 0),
       COALESCE(SUM(closing_debit), 0), COALESCE(SUM(closing_credit), 0)
FROM balance_detail
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period = ?
  AND (account_name = '货币资金' OR account_code = '1002')`, e.Company, e.Company, period).Scan(&openingDebit, &openingCredit, &closingDebit, &closingCredit)
	if err != nil {
		return 0, 0, false
	}
	opening := round2(openingDebit - openingCredit)
	closing := round2(closingDebit - closingCredit)
	if opening == 0 && closing == 0 {
		return 0, 0, false
	}
	return opening, closing, true
}

func (e *Engine) queryCashBalanceSheetOpeningClosing(period string) (float64, float64, bool) {
	var opening, closing float64
	err := e.db.QueryRow(`
SELECT COALESCE(SUM(opening_balance), 0), COALESCE(SUM(closing_balance), 0)
FROM balance_sheet
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period = ?
  AND (account_name = '货币资金' OR account_code LIKE '1002%')`, e.Company, e.Company, period).Scan(&opening, &closing)
	if err != nil {
		return 0, 0, false
	}
	if opening == 0 && closing == 0 {
		return 0, 0, false
	}
	return round2(opening), round2(closing), true
}

func (e *Engine) latestCashBalanceDetailPeriodAtOrBefore(period string) string {
	cols := e.tableColumns("balance_detail")
	if !cols["closing_debit"] || !cols["closing_credit"] {
		return ""
	}
	var latest string
	if strings.TrimSpace(period) == "" {
		_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_detail
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND (account_name = '货币资金' OR account_code = '1002')`, e.Company, e.Company).Scan(&latest)
		return latest
	}
	_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_detail
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period <= ?
  AND (account_name = '货币资金' OR account_code = '1002')`, e.Company, e.Company, period).Scan(&latest)
	return latest
}

func (e *Engine) latestCashBalancePeriodAtOrBefore(period string) string {
	var latest string
	if strings.TrimSpace(period) == "" {
		_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_sheet
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND (account_name = '货币资金' OR account_code LIKE '1002%')`, e.Company, e.Company).Scan(&latest)
		return latest
	}
	_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_sheet
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period <= ?
  AND (account_name = '货币资金' OR account_code LIKE '1002%')`, e.Company, e.Company, period).Scan(&latest)
	return latest
}
