package query

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

type contractAggregatePeriodCoverage struct {
	RequestedFrom string
	RequestedTo   string
	ActualFrom    string
	ActualTo      string
	Note          string
}

func (c contractAggregatePeriodCoverage) Adjusted() bool {
	return strings.TrimSpace(c.RequestedFrom) != strings.TrimSpace(c.ActualFrom) ||
		strings.TrimSpace(c.RequestedTo) != strings.TrimSpace(c.ActualTo)
}

func (e *Engine) resolveContractAggregatePeriodCoverage(spec QuerySpec, requestedMetrics []string, like string) contractAggregatePeriodCoverage {
	coverage := contractAggregatePeriodCoverage{
		RequestedFrom: strings.TrimSpace(spec.PeriodFrom),
		RequestedTo:   strings.TrimSpace(spec.PeriodTo),
		ActualFrom:    strings.TrimSpace(spec.PeriodFrom),
		ActualTo:      strings.TrimSpace(spec.PeriodTo),
	}
	if coverage.ActualFrom == "" || coverage.ActualTo == "" {
		return coverage
	}

	specs := contractFinanceSpecsForRequestedMetrics(requestedMetrics)
	if len(specs) == 0 {
		return coverage
	}
	if e.contractFinanceHasRowsForAllSpecs(specs, coverage.RequestedFrom, coverage.RequestedTo, like) {
		if contractAggregateShouldUseActualDataBounds(spec.OriginalQuestion) {
			if actualFrom, actualTo, ok := e.contractFinanceDataBoundsForAllSpecs(specs, coverage.RequestedFrom, coverage.RequestedTo, like); ok {
				if projectFrom, projectOK := e.earliestContractProjectDataPeriod(coverage.RequestedFrom, coverage.RequestedTo, like); projectOK && projectFrom < actualFrom {
					actualFrom = projectFrom
				}
				if contractAggregateShouldPreserveRequestedBusinessCutoff(spec) &&
					contractAggregateCanExtendActualToBusinessCutoff(actualTo, coverage.RequestedTo) {
					actualTo = coverage.RequestedTo
				}
				coverage.ActualFrom = actualFrom
				coverage.ActualTo = actualTo
				if coverage.Adjusted() {
					coverage.Note = fmt.Sprintf("[项目口径覆盖] requested=%s actual=%s reason=请求宽区间前后存在无项目数据月份，按项目表实际覆盖期间回答",
						displayPeriod(coverage.RequestedFrom, coverage.RequestedTo),
						displayPeriod(coverage.ActualFrom, coverage.ActualTo))
				}
			}
		}
		return coverage
	}
	if !contractAggregateCanUseLatestAvailablePeriod(spec.OriginalQuestion) {
		return coverage
	}

	latest := e.latestContractFinancePeriodForSpecs(specs)
	if latest == "" {
		return coverage
	}
	fallbackFrom := contractAggregateFallbackPeriodFrom(spec.OriginalQuestion, latest)
	if fallbackFrom == "" {
		fallbackFrom = latest
	}
	if !e.contractFinanceHasRowsForAllSpecs(specs, fallbackFrom, latest, like) {
		fallbackFrom = latest
		if !e.contractFinanceHasRowsForAllSpecs(specs, fallbackFrom, latest, like) {
			return coverage
		}
	}
	coverage.ActualFrom = fallbackFrom
	coverage.ActualTo = latest
	if coverage.Adjusted() {
		coverage.Note = fmt.Sprintf("[项目口径覆盖] requested=%s actual=%s reason=请求期间无收入/成本表记录，改用项目表最新可用期间",
			displayPeriod(coverage.RequestedFrom, coverage.RequestedTo),
			displayPeriod(coverage.ActualFrom, coverage.ActualTo))
	}
	return coverage
}

func contractAggregateShouldUseActualDataBounds(question string) bool {
	return contractAggregateWantsDetailItems(question)
}

func contractAggregateShouldPreserveRequestedBusinessCutoff(spec QuerySpec) bool {
	q := strings.TrimSpace(spec.OriginalQuestion)
	if q == "" {
		return false
	}
	if contractAggregateHasRelativeCurrentPeriodToken(q) {
		return true
	}
	return contractAggregateLooseYearRangeEndsAtAsOfYear(q, spec.PeriodTo, spec.AsOf)
}

func contractAggregateLooseYearRangeEndsAtAsOfYear(q, requestedTo, asOf string) bool {
	requestedYear, _ := parsePeriod(requestedTo)
	asOfYear := contractAggregateYearFromAsOf(asOf)
	if requestedYear == 0 || asOfYear == 0 || requestedYear != asOfYear {
		return false
	}
	return looseYearRangeEndsAtYear(q, asOfYear)
}

func contractAggregateCanExtendActualToBusinessCutoff(actualTo, requestedTo string) bool {
	if strings.TrimSpace(actualTo) == "" || strings.TrimSpace(requestedTo) == "" || requestedTo <= actualTo {
		return false
	}
	return contractAggregateMonthGap(actualTo, requestedTo) == 1
}

func contractAggregateMonthGap(from, to string) int {
	fromYear, fromMonth := parsePeriod(from)
	toYear, toMonth := parsePeriod(to)
	if fromYear == 0 || fromMonth == 0 || toYear == 0 || toMonth == 0 {
		return -1
	}
	return (toYear-fromYear)*12 + (toMonth - fromMonth)
}

func contractAggregateYearFromAsOf(asOf string) int {
	asOf = strings.TrimSpace(asOf)
	if len(asOf) < 4 {
		return 0
	}
	year := mustAtoi(asOf[:4])
	if year < 2000 {
		return 0
	}
	return year
}

func looseYearRangeEndsAtYear(q string, year int) bool {
	m := regexp.MustCompile(`(?i)(20\d{2}|\d{2})\s*年?\s*(?:到|至|-|~)\s*(20\d{2}|\d{2})\s*年`).FindStringSubmatch(q)
	if len(m) != 3 {
		return false
	}
	return normalizeYearTokenForQuery(m[2]) == year
}

func normalizeYearTokenForQuery(raw string) int {
	year := mustAtoi(strings.TrimSpace(raw))
	if year >= 0 && year < 100 {
		return 2000 + year
	}
	return year
}

func contractFinanceSpecsForRequestedMetrics(requestedMetrics []string) []contractFinanceTotalsSpec {
	specs := make([]contractFinanceTotalsSpec, 0, 2)
	if contractAggregateNeedsRevenueData(requestedMetrics) {
		specs = append(specs, fundIncomeTotalsSpec())
	}
	if contractAggregateNeedsCostData(requestedMetrics) {
		specs = append(specs, costSettlementTotalsSpec())
	}
	return specs
}

func (e *Engine) contractFinanceHasRowsForAllSpecs(specs []contractFinanceTotalsSpec, from, to, like string) bool {
	for _, spec := range specs {
		totals, err := e.collectContractFinanceTotals(context.Background(), spec, from, to, like)
		if err != nil || totals.RowCount == 0 {
			return false
		}
	}
	return len(specs) > 0
}

func (e *Engine) contractFinanceDataBoundsForAllSpecs(specs []contractFinanceTotalsSpec, from, to, like string) (string, string, bool) {
	actualFrom := ""
	actualTo := ""
	for _, spec := range specs {
		specFrom, specTo, ok := e.contractFinanceDataBounds(spec, from, to, like)
		if !ok {
			return "", "", false
		}
		if actualFrom == "" || specFrom > actualFrom {
			actualFrom = specFrom
		}
		if actualTo == "" || specTo < actualTo {
			actualTo = specTo
		}
	}
	if actualFrom == "" || actualTo == "" || actualFrom > actualTo {
		return "", "", false
	}
	return actualFrom, actualTo, true
}

func (e *Engine) earliestContractProjectDataPeriod(from, to, like string) (string, bool) {
	earliest := ""
	for _, spec := range []contractFinanceTotalsSpec{fundIncomeTotalsSpec(), costSettlementTotalsSpec()} {
		specFrom, _, ok := e.contractFinanceDataBounds(spec, from, to, like)
		if !ok {
			continue
		}
		earliest = minPeriodString(earliest, specFrom)
	}
	if earliest == "" {
		return "", false
	}
	return earliest, true
}

func (e *Engine) contractFinanceDataBounds(spec contractFinanceTotalsSpec, from, to, like string) (string, string, bool) {
	if err := spec.validate(); err != nil {
		return "", "", false
	}
	minPeriod, maxPeriod, ok := e.contractFinanceDirectDataBounds(spec, from, to, like)
	if e.hasContractFinanceGroupTables(spec) {
		groupMin, groupMax, groupOK := e.contractFinanceGroupDataBounds(spec, from, to, like)
		if groupOK {
			minPeriod = minPeriodString(minPeriod, groupMin)
			maxPeriod = maxPeriodString(maxPeriod, groupMax)
			ok = true
		}
	}
	if !ok || minPeriod == "" || maxPeriod == "" {
		return "", "", false
	}
	return minPeriod, maxPeriod, true
}

func (e *Engine) contractFinanceDirectDataBounds(spec contractFinanceTotalsSpec, from, to, like string) (string, string, bool) {
	predicate := e.contractFinanceNonZeroPredicate("d", spec.DirectTable, spec.MovementColumn)
	sqlText := fmt.Sprintf(`
SELECT MIN(d.year_month), MAX(d.year_month)
FROM %[1]s d
JOIN fin_contracts c ON c.contract_id = d.contract_id
WHERE d.year_month BETWEEN ? AND ?
  AND %[2]s`, spec.DirectTable, predicate)
	args := []any{from, to}
	if strings.TrimSpace(like) != "" {
		sqlText += ` AND (c.customer_name LIKE ? OR c.contract_content LIKE ?)`
		args = append(args, like, like)
	}
	var minPeriod, maxPeriod sql.NullString
	if err := e.db.QueryRow(sqlText, args...).Scan(&minPeriod, &maxPeriod); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(minPeriod.String), strings.TrimSpace(maxPeriod.String), minPeriod.Valid && maxPeriod.Valid
}

func (e *Engine) contractFinanceGroupDataBounds(spec contractFinanceTotalsSpec, from, to, like string) (string, string, bool) {
	filter, args := e.contractFinanceGroupAmountFilter(spec, from, to, like)
	predicate := e.contractFinanceNonZeroPredicate("g", spec.GroupTable, spec.MovementColumn)
	sqlText := fmt.Sprintf(`
SELECT MIN(g.year_month), MAX(g.year_month)
FROM %[1]s g
WHERE %[2]s
  AND %[3]s`, spec.GroupTable, filter, predicate)
	var minPeriod, maxPeriod sql.NullString
	if err := e.db.QueryRow(sqlText, args...).Scan(&minPeriod, &maxPeriod); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(minPeriod.String), strings.TrimSpace(maxPeriod.String), minPeriod.Valid && maxPeriod.Valid
}

func (e *Engine) contractFinanceNonZeroPredicate(alias, tableName, movementColumn string) string {
	cols := e.tableColumns(tableName)
	amountPredicates := make([]string, 0, 3)
	for _, col := range []string{"settlement_amount", movementColumn, "invoice_amount"} {
		col = strings.TrimSpace(col)
		if col != "" && cols[col] {
			amountPredicates = append(amountPredicates, fmt.Sprintf("COALESCE(%s.%s, 0) <> 0", alias, col))
		}
	}
	if len(amountPredicates) == 0 {
		return "1=1"
	}
	return "(" + strings.Join(amountPredicates, " OR ") + ")"
}

func (e *Engine) latestContractFinancePeriodForSpecs(specs []contractFinanceTotalsSpec) string {
	latest := ""
	for _, spec := range specs {
		candidate := e.latestContractFinancePeriod(spec)
		if candidate == "" {
			return ""
		}
		if latest == "" || candidate < latest {
			latest = candidate
		}
	}
	return latest
}

func (e *Engine) latestContractFinancePeriod(spec contractFinanceTotalsSpec) string {
	best := e.latestContractFinancePeriodFromTable(spec.DirectTable, spec.MovementColumn)
	if e.hasContractFinanceGroupTables(spec) {
		best = maxPeriodString(best, e.latestContractFinancePeriodFromTable(spec.GroupTable, spec.MovementColumn))
	}
	return best
}

func (e *Engine) latestContractFinancePeriodFromTable(tableName, movementColumn string) string {
	cols := e.tableColumns(tableName)
	if !cols["year_month"] {
		return ""
	}
	amountPredicates := make([]string, 0, 3)
	for _, col := range []string{"settlement_amount", movementColumn, "invoice_amount"} {
		col = strings.TrimSpace(col)
		if col != "" && cols[col] {
			amountPredicates = append(amountPredicates, fmt.Sprintf("COALESCE(%s, 0) <> 0", col))
		}
	}
	where := "COALESCE(TRIM(year_month), '') <> ''"
	if len(amountPredicates) > 0 {
		where += " AND (" + strings.Join(amountPredicates, " OR ") + ")"
	}
	sqlText := fmt.Sprintf("SELECT MAX(year_month) FROM %s WHERE %s", tableName, where)
	var period sql.NullString
	if err := e.db.QueryRow(sqlText).Scan(&period); err != nil {
		return ""
	}
	return strings.TrimSpace(period.String)
}

func minPeriodString(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case b < a:
		return b
	default:
		return a
	}
}

func contractAggregateCanUseLatestAvailablePeriod(question string) bool {
	q := strings.TrimSpace(question)
	if q == "" {
		return true
	}
	if contractAggregateHasRelativeCurrentPeriodToken(q) {
		return true
	}
	return !contractAggregateHasAbsolutePeriodToken(q)
}

func contractAggregateHasRelativeCurrentPeriodToken(q string) bool {
	return containsAny(q, []string{
		"现在", "当前", "目前", "最近", "最新", "至今", "截至", "截止", "累计",
		"本月", "这个月", "当月", "上个月", "上月", "上一个完整自然月",
		"这季度", "这个季度", "本季度",
	})
}

func contractAggregateHasAbsolutePeriodToken(q string) bool {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`20\d{2}\s*年`),
		regexp.MustCompile(`(?i)(20\d{2}|\d{2})?\s*年?\s*Q\s*[1-4]`),
		regexp.MustCompile(`第?\s*[一二三四1234]\s*季度`),
		regexp.MustCompile(`(?:20\d{2}|\d{2})?\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})\s*月`),
		regexp.MustCompile(`上半年|下半年|全年|全年度|整年|年度`),
	}
	for _, pattern := range patterns {
		if pattern.MatchString(q) {
			return true
		}
	}
	return false
}

func contractAggregateFallbackPeriodFrom(question, latest string) string {
	if strings.TrimSpace(latest) == "" {
		return ""
	}
	if containsAny(question, []string{
		"本月", "这个月", "当月",
		"上个月", "上月", "上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份", "上一个完整月", "上个完整月",
	}) || hasLatestAvailableMonthToken(question) {
		return latest
	}
	year, month := parsePeriod(latest)
	if year == 0 || month == 0 {
		return latest
	}
	if containsAny(question, []string{"这季度", "这个季度", "本季度", "季度"}) || month%3 == 0 {
		startMonth := ((month - 1) / 3 * 3) + 1
		return fmt.Sprintf("%04d-%02d", year, startMonth)
	}
	return fmt.Sprintf("%04d-01", year)
}

func timeScopeFromPeriodRange(from, to string) TimeScope {
	fromYear, fromMonth := parsePeriod(from)
	toYear, toMonth := parsePeriod(to)
	if fromYear == 0 || toYear == 0 {
		return TimeScopeMonth
	}
	if from == to {
		return TimeScopeMonth
	}
	if fromYear == toYear {
		switch {
		case fromMonth == 1 && toMonth == 12:
			return TimeScopeYearFull
		case fromMonth == 1 && toMonth == 6:
			return TimeScopeHalfYear
		case fromMonth == 7 && toMonth == 12:
			return TimeScopeHalfYear
		case toMonth-fromMonth == 2 && (fromMonth == 1 || fromMonth == 4 || fromMonth == 7 || fromMonth == 10):
			return TimeScopeQuarter
		case fromMonth == 1:
			return TimeScopeYearToDate
		}
	}
	return TimeScopeCustom
}
