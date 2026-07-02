package query

import (
	"fmt"
	"strings"
)

func (e *Engine) queryLargeBankTransactions(question, from, to string) Result {
	q := NormalizeQuestion(question)
	actualFrom, actualTo, adjusted, coverageNote := e.resolveLargeBankTransactionPeriod(question, from, to)
	from, to = actualFrom, actualTo
	if shouldReturnLargeTransactionRoster(q) {
		return e.queryLargeBankTransactionRoster(from, to, adjusted, coverageNote)
	}

	var name string
	var amount float64
	directionLabel := "流入"
	amountColumn := "credit_amount"
	if containsAny(question, []string{"流出", "支出", "付款", "付出"}) {
		directionLabel = "流出"
		amountColumn = "debit_amount"
	}
	companyClause, companyArgs := e.scopedCompanyClause("company")
	sqlTxt := fmt.Sprintf(`SELECT counterparty_name, %s
FROM bank_statement
WHERE %s
  AND transaction_date BETWEEN ? AND ?
  AND COALESCE(%s, 0) > 0
ORDER BY COALESCE(%s, 0) DESC, transaction_date DESC, counterparty_name
LIMIT 1`, amountColumn, companyClause, amountColumn, amountColumn)
	args := append(companyArgs, from+"-01", monthEndDay(to))
	e.db.QueryRow(sqlTxt, args...).Scan(&name, &amount)
	if name == "" {
		return Result{
			Success: false,
			Message: "未发现大额记录",
			ExecutedSQL: []string{
				fmt.Sprintf("queryLargeBankTransactions: %s [args: %v]", sqlTxt, args),
			},
		}
	}
	return Result{
		Success: true,
		Message: fmt.Sprintf("%s 最大%s对手方为 [%s]，流水 %.2f 元", displayPeriod(from, to), directionLabel, name, amount),
		Data: map[string]any{
			"counterparty":  name,
			"amount":        amount,
			"direction":     directionLabel,
			"period_from":   from,
			"period_to":     to,
			"source_tables": sourceTablesForLargeBankTransaction(),
		},
		ExecutedSQL: []string{
			fmt.Sprintf("queryLargeBankTransactions: %s [args: %v]", sqlTxt, args),
		},
		CalculationLogs: []string{
			coverageNote,
			fmt.Sprintf("[大额流水] direction=%s top_counterparty=%s amount=%.2f", directionLabel, name, amount),
		},
	}
}

func shouldReturnLargeTransactionRoster(question string) bool {
	if !containsAny(question, []string{"哪几笔", "几笔", "大额"}) {
		return false
	}
	return containsAny(question, []string{"进账", "流入", "到账", "收入"}) &&
		containsAny(question, []string{"支出", "流出", "付款", "付出"})
}

func (e *Engine) queryLargeBankTransactionRoster(from, to string, adjusted bool, coverageNote string) Result {
	inbound := e.topBankTransactions(from, to, "credit_amount", 5)
	outbound := e.topBankTransactions(from, to, "debit_amount", 5)
	if len(inbound) == 0 && len(outbound) == 0 {
		return Result{Success: false, Message: fmt.Sprintf("%s 未发现大额流水记录。", displayPeriod(from, to))}
	}

	message := fmt.Sprintf("%s 大额进账：%s。大额支出：%s。",
		displayPeriod(from, to),
		formatLargeTransactionItems(inbound),
		formatLargeTransactionItems(outbound))
	return Result{
		Success:      true,
		Message:      message,
		AnswerMethod: "sql",
		Data: map[string]any{
			"period_from":              from,
			"period_to":                to,
			"period_adjusted":          adjusted,
			"inbound_transactions":     inbound,
			"outbound_transactions":    outbound,
			"source_tables":            []string{"fin_bank_statement", "fin_journal"},
			"source_primary_tables":    []string{"fin_bank_statement"},
			"source_supporting_tables": []string{"fin_journal"},
			"query_spec_overrides": map[string]any{
				"period_from":       from,
				"period_to":         to,
				"time_scope":        detectTimeScope("Q", from, to, e.getLatestPeriodAnchor()),
				"semantic_families": []string{"large_transactions", "bank_statement"},
			},
		},
		ExecutedSQL: []string{
			"topBankTransactions(inbound): SELECT transaction_date, counterparty_name, summary, credit_amount FROM bank_statement WHERE ... ORDER BY credit_amount DESC LIMIT 5",
			"topBankTransactions(outbound): SELECT transaction_date, counterparty_name, summary, debit_amount FROM bank_statement WHERE ... ORDER BY debit_amount DESC LIMIT 5",
		},
		CalculationLogs: []string{
			coverageNote,
			fmt.Sprintf("[大额流水列表] period=%s inbound=%d outbound=%d", displayPeriod(from, to), len(inbound), len(outbound)),
		},
	}
}

func (e *Engine) resolveLargeBankTransactionPeriod(question, from, to string) (string, string, bool, string) {
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
	fallbackFrom := contractAggregateFallbackPeriodFrom(question, latest)
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

func (e *Engine) bankStatementHasAmountRows(from, to string) bool {
	if e == nil || e.db == nil {
		return false
	}
	cols := e.tableColumns("bank_statement")
	if !cols["transaction_date"] {
		return false
	}
	amountPredicate := bankStatementAmountPredicate(cols)
	if amountPredicate == "" {
		return false
	}
	companyClause, companyArgs := e.scopedCompanyClause("company")
	sqlTxt := fmt.Sprintf(`SELECT COUNT(*)
FROM bank_statement
WHERE %s
  AND DATE(transaction_date) >= DATE(?)
  AND DATE(transaction_date) <= DATE(?)
  AND (%s)`, companyClause, amountPredicate)
	args := append(companyArgs, from+"-01", monthEndDay(to))
	var count int
	if err := e.db.QueryRow(sqlTxt, args...).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (e *Engine) latestBankStatementPeriodWithAmountRows() string {
	if e == nil || e.db == nil {
		return ""
	}
	cols := e.tableColumns("bank_statement")
	if !cols["transaction_date"] {
		return ""
	}
	amountPredicate := bankStatementAmountPredicate(cols)
	if amountPredicate == "" {
		return ""
	}
	companyClause, companyArgs := e.scopedCompanyClause("company")
	sqlTxt := fmt.Sprintf(`SELECT MAX(SUBSTR(CAST(DATE(transaction_date) AS TEXT), 1, 7))
FROM bank_statement
WHERE %s
  AND COALESCE(TRIM(CAST(transaction_date AS TEXT)), '') <> ''
  AND DATE(transaction_date) IS NOT NULL
  AND (%s)`, companyClause, amountPredicate)
	var period string
	if err := e.db.QueryRow(sqlTxt, companyArgs...).Scan(&period); err != nil {
		return ""
	}
	return strings.TrimSpace(period)
}

func bankStatementAmountPredicate(cols map[string]bool) string {
	predicates := make([]string, 0, 2)
	if cols["credit_amount"] {
		predicates = append(predicates, "COALESCE(credit_amount, 0) <> 0")
	}
	if cols["debit_amount"] {
		predicates = append(predicates, "COALESCE(debit_amount, 0) <> 0")
	}
	return strings.Join(predicates, " OR ")
}

func (e *Engine) topBankTransactions(from, to, amountColumn string, limit int) []map[string]any {
	if limit <= 0 {
		limit = 5
	}
	companyClause, companyArgs := e.scopedCompanyClause("company")
	sqlTxt := fmt.Sprintf(`SELECT transaction_date, counterparty_name, summary, %s
FROM bank_statement
WHERE %s
  AND transaction_date BETWEEN ? AND ?
  AND COALESCE(%s, 0) > 0
ORDER BY COALESCE(%s, 0) DESC, transaction_date DESC, counterparty_name
LIMIT ?`, amountColumn, companyClause, amountColumn, amountColumn)
	args := append(companyArgs, from+"-01", monthEndDay(to), limit)
	rows, err := e.db.Query(sqlTxt, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		var date, counterparty, summary string
		var amount float64
		if err := rows.Scan(&date, &counterparty, &summary, &amount); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"date":         date,
			"counterparty": counterparty,
			"summary":      summary,
			"amount":       round2(amount),
		})
	}
	return out
}

func formatLargeTransactionItems(items []map[string]any) string {
	if len(items) == 0 {
		return "未发现"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		date := strings.TrimSpace(anyToString(item["date"]))
		counterparty := strings.TrimSpace(anyToString(item["counterparty"]))
		amount := anyToFloat64(item["amount"])
		if date != "" {
			parts = append(parts, fmt.Sprintf("%s %s %.2f 元", date, counterparty, amount))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %.2f 元", counterparty, amount))
	}
	return strings.Join(parts, "；")
}

func quarterStartPeriod(period string) string {
	if len(period) < 7 {
		return period
	}
	month := period[5:7]
	switch month {
	case "01", "02", "03":
		return period[:4] + "-01"
	case "04", "05", "06":
		return period[:4] + "-04"
	case "07", "08", "09":
		return period[:4] + "-07"
	default:
		return period[:4] + "-10"
	}
}

func (e *Engine) detectEntityRole(name string) (role string, log string) {
	endDate := monthEndDay(e.getLatestPeriodAnchor().Format("2006-01"))
	startDate := "2000-01-01"
	evidence := e.collectCounterpartyEvidence(name, startDate[:7], endDate[:7])
	classification := ClassifyCounterpartyWithConfig(name, evidence, e.currentRuleConfig())
	if classification.Role == CounterpartyUnknown {
		return "unknown", "unknown"
	}
	return string(classification.Role), fmt.Sprintf("role=%s confidence=%.3f signals=%v", classification.Role, classification.Confidence, classification.Signals)
}
