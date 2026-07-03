package query

import (
	"fmt"
	"strings"
)

func (e *Engine) monthlyBookSummary(year, month int) (monthlyBookView, string, error) {
	var revenue, cost, adminExpense, sellingExpense, financeExpense, taxSurcharge float64
	var nonOperatingIncome, nonOperatingExpense, operatingProfit, totalProfit, netProfit, incomeTax float64
	hasRevenue := false
	hasCost := false
	hasExpenseComponent := false
	hasNonOperatingIncome := false
	hasNonOperatingExpense := false
	hasOperatingProfit := false
	hasTotalProfit := false
	hasNetProfit := false
	hasIncomeTax := false
	cfg := e.currentRuleConfig()
	rows, err := e.db.Query(`
SELECT item_name, current_amount
FROM income_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period = ?
`, e.Company, e.Company, fmt.Sprintf("%04d-%02d", year, month))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item string
			var amount float64
			if scanErr := rows.Scan(&item, &amount); scanErr != nil {
				continue
			}
			switch {
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("revenue")):
				revenue = amount
				hasRevenue = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("cost")):
				cost = amount
				hasCost = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("admin_expense")):
				adminExpense = amount
				hasExpenseComponent = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("selling_expense")):
				sellingExpense = amount
				hasExpenseComponent = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("finance_expense")):
				financeExpense = amount
				hasExpenseComponent = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("tax_surcharge")):
				taxSurcharge = amount
				hasExpenseComponent = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("non_operating_income")):
				nonOperatingIncome = amount
				hasNonOperatingIncome = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("non_operating_expense")):
				nonOperatingExpense = amount
				hasNonOperatingExpense = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("operating_profit")):
				operatingProfit = amount
				hasOperatingProfit = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("profit_total")):
				totalProfit = amount
				hasTotalProfit = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("net_profit")):
				netProfit = amount
				hasNetProfit = true
			case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("income_tax")):
				incomeTax = amount
				hasIncomeTax = true
			}
		}
	}

	if hasRevenue && (hasTotalProfit || hasNetProfit || hasOperatingProfit || hasNonOperatingIncome || hasNonOperatingExpense || hasIncomeTax) {
		totalCost := round2(cost + sellingExpense + adminExpense + financeExpense + taxSurcharge)
		derivedOperatingProfit := round2(revenue - totalCost)
		derivedTotalProfit := round2(derivedOperatingProfit + nonOperatingIncome - nonOperatingExpense)
		derivedNetProfit := round2(derivedTotalProfit - incomeTax)

		if !hasCost && totalCost == 0 {
			switch {
			case hasTotalProfit:
				totalCost = round2(revenue + nonOperatingIncome - nonOperatingExpense - totalProfit)
			case hasNetProfit:
				totalProfitEquivalent := netProfit
				if hasIncomeTax {
					totalProfitEquivalent = round2(netProfit + incomeTax)
				}
				totalCost = round2(revenue + nonOperatingIncome - nonOperatingExpense - totalProfitEquivalent)
			}
			derivedOperatingProfit = round2(revenue - totalCost)
			derivedTotalProfit = round2(derivedOperatingProfit + nonOperatingIncome - nonOperatingExpense)
			derivedNetProfit = round2(derivedTotalProfit - incomeTax)
		}
		if !hasCost {
			cost = totalCost
		}
		if !hasOperatingProfit {
			operatingProfit = derivedOperatingProfit
		}
		if !hasTotalProfit || hasCost || hasExpenseComponent || hasNonOperatingIncome || hasNonOperatingExpense {
			totalProfit = derivedTotalProfit
		}
		if !hasNetProfit {
			netProfit = derivedNetProfit
		}
		return monthlyBookView{
			Revenue:             round2(revenue),
			Cost:                round2(cost),
			TaxSurcharge:        round2(taxSurcharge),
			SellingExpense:      round2(sellingExpense),
			AdminExpense:        round2(adminExpense),
			FinanceExpense:      round2(financeExpense),
			NonOperatingIncome:  round2(nonOperatingIncome),
			NonOperatingExpense: round2(nonOperatingExpense),
			OperatingProfit:     round2(operatingProfit),
			Profit:              round2(totalProfit),
			NetProfit:           round2(netProfit),
			IncomeTax:           round2(incomeTax),
			TotalCost:           totalCost,
		}, "income_statement", nil
	}

	if !e.hasJournalActivityForMonth(year, month) {
		return monthlyBookView{}, "empty_month", nil
	}

	book, _, err := e.monthlyJournalBookSummary(year, month)
	return book, "journal_fallback", err
}

func (e *Engine) monthlyJournalBookSummary(year, month int) (monthlyBookView, string, error) {
	if !e.hasJournalActivityForMonth(year, month) {
		return monthlyBookView{}, "empty_month", nil
	}
	monthly, err := e.calc.ComputeMonthlyFromJournal(e.Company, year, month)
	if err != nil {
		return monthlyBookView{}, "", err
	}
	is, err := e.calc.ComputeIncomeStatement(e.Company, year, month)
	if err != nil {
		return monthlyBookView{}, "", err
	}
	return monthlyBookView{
		Revenue:             monthly.Revenue,
		Cost:                is.Cost,
		TaxSurcharge:        is.TaxSurcharge,
		SellingExpense:      is.SellingExpense,
		AdminExpense:        is.AdminExpense,
		FinanceExpense:      is.FinanceExpense,
		NonOperatingIncome:  is.NonOpIncome,
		NonOperatingExpense: is.NonOpExpense,
		OperatingProfit:     is.OperatingProfit,
		TotalCost:           round2(is.Cost + is.TaxSurcharge + is.SellingExpense + is.AdminExpense + is.FinanceExpense),
		Profit:              is.TotalProfit,
		NetProfit:           is.NetProfit,
		IncomeTax:           is.IncomeTax,
	}, "journal", nil
}

func matchIncomeStatementItem(item string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(item, pattern) {
			return true
		}
	}
	return false
}

func (e *Engine) hasJournalActivityForMonth(year, month int) bool {
	startDate := fmt.Sprintf("%d-%02d-01", year, month)
	endDate := monthEndDay(fmt.Sprintf("%d-%02d", year, month))
	var count int
	if err := e.db.QueryRow(`
SELECT COUNT(1)
FROM journal
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND DATE(voucher_date) >= DATE(?)
  AND DATE(voucher_date) <= DATE(?)
  AND summary NOT LIKE '%期间损益结转%'
`, e.Company, e.Company, startDate, endDate).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (e *Engine) topCounterpartiesByCashMovement(from, to string, limit int) []string {
	rows, err := e.db.Query(`
SELECT counterparty_name,
       COALESCE(SUM(credit_amount), 0) AS bank_in,
       COALESCE(SUM(debit_amount), 0) AS bank_out
FROM bank_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND transaction_date BETWEEN ? AND ?
  AND IFNULL(TRIM(counterparty_name), '') <> ''
GROUP BY counterparty_name
ORDER BY ABS(COALESCE(SUM(credit_amount), 0) - COALESCE(SUM(debit_amount), 0)) DESC,
         (COALESCE(SUM(credit_amount), 0) + COALESCE(SUM(debit_amount), 0)) DESC
LIMIT ?
`, e.Company, e.Company, from+"-01", monthEndDay(to), limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var name string
		var inAmt, outAmt float64
		if scanErr := rows.Scan(&name, &inAmt, &outAmt); scanErr != nil {
			continue
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}
