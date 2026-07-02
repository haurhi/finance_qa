package query

import (
	"database/sql"
	"strings"

	"financeqa/internal/accounting"
)

func (e *Engine) latestAvailableFinancialPeriod() string {
	companyKey := strings.TrimSpace(e.Company)
	e.cacheMu.RLock()
	if cached := strings.TrimSpace(e.availablePeriod[companyKey]); cached != "" {
		e.cacheMu.RUnlock()
		return cached
	}
	e.cacheMu.RUnlock()

	queries := []string{
		`SELECT MAX(period) FROM income_statement WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')`,
		`SELECT MAX(period) FROM balance_detail WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')`,
		`SELECT MAX(period) FROM journal WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')`,
		`SELECT MAX(SUBSTR(CAST(voucher_date AS TEXT), 1, 7)) FROM journal WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')`,
		`SELECT MAX(SUBSTR(CAST(transaction_date AS TEXT), 1, 7)) FROM bank_statement WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')`,
	}
	best := ""
	for _, sqlTxt := range queries {
		var period sql.NullString
		if err := e.db.QueryRow(sqlTxt, e.Company, e.Company).Scan(&period); err != nil {
			continue
		}
		candidate := strings.TrimSpace(period.String)
		if candidate != "" && candidate > best {
			best = candidate
		}
	}
	if best != "" {
		e.cacheMu.Lock()
		e.availablePeriod[companyKey] = best
		e.cacheMu.Unlock()
		return best
	}
	return ""
}

func (e *Engine) latestAvailableFinancialPeriodForRequest(request coreMetricRequest) string {
	metrics := availabilityMetricKeys(request)
	if len(metrics) == 0 {
		return e.latestAvailableFinancialPeriod()
	}

	companyKey := strings.TrimSpace(e.Company)
	cacheKey := companyKey + "|core_metric:" + strings.Join(metrics, ",")
	e.cacheMu.RLock()
	if cached := strings.TrimSpace(e.availablePeriod[cacheKey]); cached != "" {
		e.cacheMu.RUnlock()
		return cached
	}
	e.cacheMu.RUnlock()

	incomeStatementAvailability := e.collectIncomeStatementMetricAvailability()
	journalAvailability := e.collectJournalMetricAvailability()

	overall := ""
	for _, metric := range metrics {
		candidate := maxPeriodString(incomeStatementAvailability[metric], journalAvailability[metric])
		if strings.TrimSpace(candidate) == "" {
			return ""
		}
		if overall == "" || candidate < overall {
			overall = candidate
		}
	}
	if overall != "" {
		e.cacheMu.Lock()
		e.availablePeriod[cacheKey] = overall
		e.cacheMu.Unlock()
	}
	return overall
}

type incomeStatementPeriodEvidence struct {
	revenue             bool
	cost                bool
	sellingExpense      bool
	adminExpense        bool
	financeExpense      bool
	taxSurcharge        bool
	nonOperatingIncome  bool
	nonOperatingExpense bool
	operatingProfit     bool
	totalProfit         bool
	netProfit           bool
	incomeTax           bool
}

func (e incomeStatementPeriodEvidence) supportsMonthlyBookSummary() bool {
	return e.revenue && (e.totalProfit || e.netProfit || e.operatingProfit || e.nonOperatingIncome || e.nonOperatingExpense || e.incomeTax)
}

func (e *Engine) collectIncomeStatementMetricAvailability() map[string]string {
	cfg := e.currentRuleConfig()
	rows, err := e.db.Query(`
SELECT period, item_name
FROM income_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND IFNULL(TRIM(period), '') <> ''
ORDER BY period, item_name
`, e.Company, e.Company)
	if err != nil {
		return map[string]string{}
	}
	defer rows.Close()

	perPeriod := map[string]incomeStatementPeriodEvidence{}
	for rows.Next() {
		var period, item string
		if scanErr := rows.Scan(&period, &item); scanErr != nil {
			continue
		}
		period = strings.TrimSpace(period)
		if period == "" {
			continue
		}
		evidence := perPeriod[period]
		switch {
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("revenue")):
			evidence.revenue = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("cost")):
			evidence.cost = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("selling_expense")):
			evidence.sellingExpense = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("admin_expense")):
			evidence.adminExpense = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("finance_expense")):
			evidence.financeExpense = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("tax_surcharge")):
			evidence.taxSurcharge = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("non_operating_income")):
			evidence.nonOperatingIncome = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("non_operating_expense")):
			evidence.nonOperatingExpense = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("operating_profit")):
			evidence.operatingProfit = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("profit_total")):
			evidence.totalProfit = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("net_profit")):
			evidence.netProfit = true
		case matchIncomeStatementItem(item, cfg.IncomeStatementPatterns("income_tax")):
			evidence.incomeTax = true
		}
		perPeriod[period] = evidence
	}

	availability := map[string]string{}
	for period, evidence := range perPeriod {
		if !evidence.supportsMonthlyBookSummary() {
			continue
		}
		for _, metric := range []string{metricKeyRevenue, metricKeyCost, metricKeyProfit} {
			availability[metric] = maxPeriodString(availability[metric], period)
		}
	}
	return availability
}

type journalPeriodEvidence struct {
	revenue bool
	cost    bool
	profit  bool
}

func (e *Engine) collectJournalMetricAvailability() map[string]string {
	rows, err := e.db.Query(`
SELECT SUBSTR(CAST(voucher_date AS TEXT), 1, 7) AS period,
       account_code,
       COALESCE(summary, '')
FROM journal
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND IFNULL(TRIM(voucher_date), '') <> ''
  AND COALESCE(summary, '') NOT LIKE '%期间损益结转%'
ORDER BY period
`, e.Company, e.Company)
	if err != nil {
		return map[string]string{}
	}
	defer rows.Close()

	perPeriod := map[string]journalPeriodEvidence{}
	for rows.Next() {
		var period, code, summary string
		if scanErr := rows.Scan(&period, &code, &summary); scanErr != nil {
			continue
		}
		period = strings.TrimSpace(period)
		if period == "" {
			continue
		}
		evidence := perPeriod[period]
		finalCode := strings.TrimSpace(code)
		if e.calc != nil && e.calc.Mapper != nil {
			if mapped, ok := e.calc.Mapper.MapAccount(finalCode, "", summary, ""); ok && strings.TrimSpace(mapped) != "" {
				finalCode = strings.TrimSpace(mapped)
			}
		}
		category := accounting.CategoryForCode(finalCode)
		parentCode := finalCode
		if len(parentCode) > 4 {
			parentCode = parentCode[:4]
		}
		if category == accounting.CategoryRevenue {
			evidence.revenue = true
		}
		switch parentCode {
		case "6401", "6403", "6601", "6602", "6603":
			evidence.cost = true
		}
		switch parentCode {
		case "6001", "6051", "6401", "6403", "6601", "6602", "6603", "6301", "6711", "6801":
			evidence.profit = true
		}
		perPeriod[period] = evidence
	}

	availability := map[string]string{}
	for period, evidence := range perPeriod {
		if evidence.revenue {
			availability[metricKeyRevenue] = maxPeriodString(availability[metricKeyRevenue], period)
		}
		if evidence.cost {
			availability[metricKeyCost] = maxPeriodString(availability[metricKeyCost], period)
		}
		if evidence.profit {
			availability[metricKeyProfit] = maxPeriodString(availability[metricKeyProfit], period)
		}
	}
	return availability
}

func availabilityMetricKeys(request coreMetricRequest) []string {
	keys := make([]string, 0, len(request.RequestedMetrics))
	seen := map[string]struct{}{}
	add := func(metric string) {
		metric = strings.TrimSpace(metric)
		if metric == "" {
			return
		}
		if _, ok := seen[metric]; ok {
			return
		}
		seen[metric] = struct{}{}
		keys = append(keys, metric)
	}
	for _, metric := range request.RequestedMetrics {
		switch strings.TrimSpace(metric) {
		case "收入":
			add(metricKeyRevenue)
		case "成本":
			add(metricKeyCost)
		case "利润", "净利润":
			add(metricKeyProfit)
		}
	}
	if len(keys) > 0 {
		return keys
	}
	switch strings.TrimSpace(request.PrimaryMetric) {
	case "收入":
		add(metricKeyRevenue)
	case "成本":
		add(metricKeyCost)
	case "利润", "净利润":
		add(metricKeyProfit)
	}
	return keys
}

func maxPeriodString(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	case right > left:
		return right
	default:
		return left
	}
}
