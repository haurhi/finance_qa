package accounting

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// AccountCategory represents the nature of an accounting subject (Assets, Liabilities, etc.)
type AccountCategory string

const (
	CategoryAsset     AccountCategory = "资产"
	CategoryLiability AccountCategory = "负债"
	CategoryEquity    AccountCategory = "权益"
	CategoryCost      AccountCategory = "成本"
	CategoryRevenue   AccountCategory = "收入"
	CategoryExpense   AccountCategory = "费用"
)

// NormalDirection returns the standard balance side ("借" or "贷") for a category.
func NormalDirection(cat AccountCategory) string {
	switch cat {
	case CategoryAsset, CategoryCost, CategoryExpense:
		return "借"
	default:
		return "贷"
	}
}

// CategoryForCode provides a rule-based categorization based on Chinese Accounting Standards (CAS).
func CategoryForCode(code string) AccountCategory {
	if len(code) == 0 {
		return ""
	}
	switch code[0] {
	case '1':
		return CategoryAsset
	case '2':
		return CategoryLiability
	case '3':
		return CategoryEquity
	case '4':
		return CategoryCost
	case '5':
		return CategoryCost // Manufacturing/Common
	case '6':
		// In CAS 2013, 60xx/63xx are typically Revenue/Income, others are Expenses
		if strings.HasPrefix(code, "60") || strings.HasPrefix(code, "61") || strings.HasPrefix(code, "63") {
			return CategoryRevenue
		}
		return CategoryExpense
	}
	return ""
}

// AccountMapper defines the interface for mapping company-specific accounts to standard categories.
type AccountMapper interface {
	MapAccount(code, name, summary, counterparty string) (string, bool)
	GetCode(name string) string
}

// Calculator computes financial reports from journal (序时帐) data.
type Calculator struct {
	db              *sql.DB
	Mapper          AccountMapper
	ExecutedSQLs    []string
	CalculationLogs []string
}

// NewCalculator creates a calculator backed by the given database.
func NewCalculator(db *sql.DB) *Calculator {
	return &Calculator{db: db}
}

// ResetTrace clears the recorded SQLs and logs.
func (c *Calculator) GetCode(name string) string {
	if c.Mapper == nil {
		return ""
	}
	return c.Mapper.GetCode(name)
}

func (c *Calculator) ResetTrace() {
	c.ExecutedSQLs = nil
	c.CalculationLogs = nil
}

func (c *Calculator) trace(sql string, log string) {
	if sql != "" {
		c.ExecutedSQLs = append(c.ExecutedSQLs, sql)
	}
	if log != "" {
		c.CalculationLogs = append(c.CalculationLogs, log)
	}
}

// BalanceDetailRow represents one computed row of the balance detail (科目余额表).
type BalanceDetailRow struct {
	AccountCode  string  `json:"account_code"`
	AccountName  string  `json:"account_name"`
	Category     string  `json:"category"`
	OpeningDebit float64 `json:"opening_debit"`
	OpeningCred  float64 `json:"opening_credit"`
	CurrentDebit float64 `json:"current_debit"`
	CurrentCred  float64 `json:"current_credit"`
	ClosingDebit float64 `json:"closing_debit"`
	ClosingCred  float64 `json:"closing_credit"`
}

// IncomeStatementResult holds the computed income statement (利润表).
type IncomeStatementResult struct {
	Period          string  `json:"period"`
	Revenue         float64 `json:"revenue"`          // 营业收入
	Cost            float64 `json:"cost"`             // 营业成本
	TaxSurcharge    float64 `json:"tax_surcharge"`    // 税金及附加
	SellingExpense  float64 `json:"selling_expense"`  // 销售费用
	AdminExpense    float64 `json:"admin_expense"`    // 管理费用
	FinanceExpense  float64 `json:"finance_expense"`  // 财务费用
	NonOpIncome     float64 `json:"non_op_income"`    // 营业外收入
	NonOpExpense    float64 `json:"non_op_expense"`   // 营业外支出
	OperatingProfit float64 `json:"operating_profit"` // 营业利润
	TotalProfit     float64 `json:"total_profit"`     // 利润总额
	IncomeTax       float64 `json:"income_tax"`       // 所得税费用
	NetProfit       float64 `json:"net_profit"`       // 净利润
}

// MonthlyMetrics holds revenue/profit for a single month.
type MonthlyMetrics struct {
	Year    int     `json:"year"`
	Month   int     `json:"month"`
	Revenue float64 `json:"revenue"`
	Cost    float64 `json:"cost"`
	Profit  float64 `json:"profit"`
}

// DualPerspective provides both "钱" (cash) and "帐" (accrual) views.
type DualPerspective struct {
	Cash    CashPerspective    `json:"cash"`
	Accrual AccrualPerspective `json:"accrual"`
}

// CashPerspective is the "钱" view — actual bank cash flows.
type CashPerspective struct {
	Description string  `json:"说明"`
	Income      float64 `json:"现金流入"` // 银行收入
	Expense     float64 `json:"现金流出"` // 银行支出
	Net         float64 `json:"净现金流"` // 净现金流
}

// AccrualPerspective is the "帐" view — accrual-basis accounting.
type AccrualPerspective struct {
	Description string  `json:"说明"`
	Revenue     float64 `json:"营业收入"`    // 营业收入
	TotalCost   float64 `json:"营业成本及费用"` // 营业成本+费用
	Profit      float64 `json:"账面利润"`    // 净利润
}

func monthDateBounds(year, month int) (string, string) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// ComputeMonthlyFromJournal calculates revenue, cost and profit for a specific
// month by aggregating journal entries. It excludes "期间损益结转" entries to
// avoid double counting.
func (c *Calculator) ComputeMonthlyFromJournal(company string, year, month int) (*MonthlyMetrics, error) {
	startDate, endDate := monthDateBounds(year, month)

	sqlTxt := `
SELECT account_code, direction, COALESCE(amount, 0) as amount, summary
FROM journal
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND DATE(voucher_date) >= DATE(?)
  AND DATE(voucher_date) <= DATE(?)
  AND summary NOT LIKE '%期间损益结转%'
`
	c.trace(fmt.Sprintf("ComputeMonthlyFromJournal: %s [args: %s, %s, %s]", sqlTxt, company, startDate, endDate),
		fmt.Sprintf("汇总 %s %d年%d月 序时账数据（排除利润结转）", company, year, month))

	rows, err := c.db.Query(sqlTxt, company, company, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("query journal for month %d: %w", month, err)
	}
	defer rows.Close()

	var revenue, cost float64
	for rows.Next() {
		var code, direction, summary string
		var amount float64
		if err := rows.Scan(&code, &direction, &amount, &summary); err != nil {
			return nil, fmt.Errorf("scan journal row: %w", err)
		}

		// Map account to standard code
		finalCode := code
		if c.Mapper != nil {
			if mapped, ok := c.Mapper.MapAccount(code, "", summary, ""); ok {
				finalCode = mapped
				c.trace("", fmt.Sprintf("  - 科目 %s 匹配到映射规则 -> %s", code, mapped))
			}
		}

		cat := CategoryForCode(finalCode)
		switch cat {
		case CategoryRevenue:
			// Revenue accounts: normal direction is 贷 (credit increases revenue)
			if direction == "贷" {
				revenue += amount
			} else {
				revenue -= amount // 借方冲减收入
			}
		case CategoryExpense:
			// Expense accounts: normal direction is 借 (debit increases expense)
			if direction == "借" {
				cost += amount
			} else {
				cost -= amount // 贷方冲减费用
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate journal rows: %w", err)
	}

	return &MonthlyMetrics{
		Year:    year,
		Month:   month,
		Revenue: roundTo2(revenue),
		Cost:    roundTo2(cost),
		Profit:  roundTo2(revenue - cost),
	}, nil
}

// ComputeIncomeStatement computes the income statement from journal entries
// up to and including the specified month (cumulative from January).
func (c *Calculator) ComputeIncomeStatement(company string, year, month int) (*IncomeStatementResult, error) {
	startDate := fmt.Sprintf("%d-01-01", year)
	_, endDate := monthDateBounds(year, month)
	return c.computeIncomeStatementRange(company, fmt.Sprintf("%d-%02d", year, month), startDate, endDate, "计算年度累计利润表 (YTD)")
}

// ComputeMonthlyIncomeStatement computes the income statement from journal
// entries for one month only.
func (c *Calculator) ComputeMonthlyIncomeStatement(company string, year, month int) (*IncomeStatementResult, error) {
	startDate, endDate := monthDateBounds(year, month)
	return c.computeIncomeStatementRange(company, fmt.Sprintf("%d-%02d", year, month), startDate, endDate, "计算月度利润表")
}

func (c *Calculator) computeIncomeStatementRange(company, period, startDate, endDate, traceLabel string) (*IncomeStatementResult, error) {
	rows, err := c.db.Query(`
SELECT account_code, direction, COALESCE(amount, 0) as amount
FROM journal
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND DATE(voucher_date) >= DATE(?)
  AND DATE(voucher_date) <= DATE(?)
  AND summary NOT LIKE '%期间损益结转%'
`, company, company, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("query journal for income statement: %w", err)
	}
	defer rows.Close()

	result := &IncomeStatementResult{
		Period: period,
	}
	c.trace("", fmt.Sprintf("%s: 起点 %s, 终点 %s", traceLabel, startDate, endDate))

	for rows.Next() {
		var code, direction string
		var amount float64
		if err := rows.Scan(&code, &direction, &amount); err != nil {
			return nil, fmt.Errorf("scan income row: %w", err)
		}

		// Map account to standard code
		finalCode := code
		if c.Mapper != nil {
			if mapped, ok := c.Mapper.MapAccount(code, "", "", ""); ok {
				finalCode = mapped
				if mapped != code {
					c.trace("", fmt.Sprintf("  - 科目 %s 匹配到映射规则 -> %s", code, mapped))
				}
			}
		}

		// Determine the sign: credits are positive for revenue, debits are positive for expense
		signedAmount := amount
		cat := CategoryForCode(finalCode)
		normalDir := NormalDirection(cat)
		if direction != normalDir {
			signedAmount = -amount
		}

		parentCode := finalCode
		if len(finalCode) > 4 {
			parentCode = finalCode[:4]
		}

		switch parentCode {
		case "6001":
			result.Revenue += signedAmount
			c.trace("", fmt.Sprintf("  + 营业收入 %s: %.2f (摘要匹配)", code, signedAmount))
		case "6051":
			result.Revenue += signedAmount // 其他业务收入
			c.trace("", fmt.Sprintf("  + 其他收入 %s: %.2f", code, signedAmount))
		case "6401":
			result.Cost += signedAmount
			c.trace("", fmt.Sprintf("  - 营业成本 %s: %.2f", code, signedAmount))
		case "6403":
			result.TaxSurcharge += signedAmount
			c.trace("", fmt.Sprintf("  - 税金及附加 %s: %.2f", code, signedAmount))
		case "6601":
			result.SellingExpense += signedAmount
			c.trace("", fmt.Sprintf("  - 销售费用 %s: %.2f", code, signedAmount))
		case "6602":
			result.AdminExpense += signedAmount
			c.trace("", fmt.Sprintf("  - 管理费用 %s: %.2f", code, signedAmount))
		case "6603":
			result.FinanceExpense += signedAmount
			c.trace("", fmt.Sprintf("  - 财务费用 %s: %.2f", code, signedAmount))
		case "6301":
			result.NonOpIncome += signedAmount
			c.trace("", fmt.Sprintf("  + 营业外收入 %s: %.2f", code, signedAmount))
		case "6711":
			result.NonOpExpense += signedAmount
			c.trace("", fmt.Sprintf("  - 营业外支出 %s: %.2f", code, signedAmount))
		case "6801":
			result.IncomeTax += signedAmount
			c.trace("", fmt.Sprintf("  - 所得税费用 %s: %.2f", code, signedAmount))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate income rows: %w", err)
	}

	result.Revenue = roundTo2(result.Revenue)
	result.Cost = roundTo2(result.Cost)
	result.TaxSurcharge = roundTo2(result.TaxSurcharge)
	result.SellingExpense = roundTo2(result.SellingExpense)
	result.AdminExpense = roundTo2(result.AdminExpense)
	result.FinanceExpense = roundTo2(result.FinanceExpense)
	result.NonOpIncome = roundTo2(result.NonOpIncome)
	result.NonOpExpense = roundTo2(result.NonOpExpense)
	result.IncomeTax = roundTo2(result.IncomeTax)

	result.OperatingProfit = roundTo2(result.Revenue - result.Cost - result.TaxSurcharge -
		result.SellingExpense - result.AdminExpense - result.FinanceExpense)
	result.TotalProfit = roundTo2(result.OperatingProfit + result.NonOpIncome - result.NonOpExpense)
	result.NetProfit = roundTo2(result.TotalProfit - result.IncomeTax)

	return result, nil
}

// ComputeCashFlow computes bank-based cash in/out for the given period.
// This is the "钱" (real money) perspective.
func (c *Calculator) ComputeCashFlow(company, from, to string) (*CashPerspective, error) {
	startDate := from + "-01"
	endDate := lastDayOfMonth(to)

	sqlTxt := `
SELECT COALESCE(SUM(COALESCE(credit_amount, 0)), 0) AS income, COALESCE(SUM(COALESCE(debit_amount, 0)), 0) AS expense
FROM bank_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%') AND DATE(transaction_date) >= DATE(?) AND DATE(transaction_date) <= DATE(?)
`
	c.trace(fmt.Sprintf("ComputeCashFlow: %s [args: %s, %s, %s]", sqlTxt, company, startDate, endDate),
		fmt.Sprintf("汇总 %s 在 %s-%s 期间的银行现金流入流出", company, from, to))

	row := c.db.QueryRow(sqlTxt, company, company, startDate, endDate)

	var income, expense float64
	if err := row.Scan(&income, &expense); err != nil {
		return nil, fmt.Errorf("query cash flow: %w", err)
	}

	return &CashPerspective{
		Description: "业务现金流（实收实付）",
		Income:      roundTo2(income),
		Expense:     roundTo2(expense),
		Net:         roundTo2(income - expense),
	}, nil
}

// ComputeDualPerspective provides both cash and accrual views for a period.
// This is the key feature: answering the boss from both "钱" and "帐" perspectives.
func (c *Calculator) ComputeDualPerspective(company string, year, month int) (*DualPerspective, error) {
	period := fmt.Sprintf("%d-%02d", year, month)

	// 钱 perspective
	cash, err := c.ComputeCashFlow(company, period, period)
	if err != nil {
		return nil, fmt.Errorf("compute cash flow: %w", err)
	}

	// 帐 perspective — try income statement first, fallback to journal calculation
	accrual, err := c.computeAccrualPerspective(company, year, month)
	if err != nil {
		return nil, fmt.Errorf("compute accrual: %w", err)
	}

	return &DualPerspective{Cash: *cash, Accrual: *accrual}, nil
}

func (c *Calculator) computeAccrualPerspective(company string, year, month int) (*AccrualPerspective, error) {
	// First try: use the income_statement table if available for this period
	period := fmt.Sprintf("%d-%02d", year, month)
	var revenue, totalCost, profit float64
	var found bool

	row := c.db.QueryRow(`
SELECT current_amount FROM income_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%') AND period = ? AND item_name LIKE '%营业收入%'
LIMIT 1
`, company, company, period)
	var currentRevenue sql.NullFloat64
	if err := row.Scan(&currentRevenue); err == nil && currentRevenue.Valid {
		found = true
		revenue = currentRevenue.Float64

		// Get net profit
		row2 := c.db.QueryRow(`
SELECT current_amount FROM income_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period = ?
  AND item_name LIKE '%净利润%'
LIMIT 1
`, company, company, period)
		var netProfit sql.NullFloat64
		if err := row2.Scan(&netProfit); err == nil && netProfit.Valid {
			profit = netProfit.Float64
			totalCost = revenue - profit
		}
	}

	if !found {
		// Fallback: compute from journal
		metrics, err := c.ComputeMonthlyFromJournal(company, year, month)
		if err != nil {
			return nil, err
		}
		revenue = metrics.Revenue
		totalCost = metrics.Cost
		profit = metrics.Profit
	}

	return &AccrualPerspective{
		Description: "财务记账（权责发生制，含年底调整/预提）",
		Revenue:     roundTo2(revenue),
		TotalCost:   roundTo2(totalCost),
		Profit:      roundTo2(profit),
	}, nil
}

// ComputeBalanceFromJournal computes the trial balance from journal entries
// using opening balances from balance_detail.
func (c *Calculator) ComputeBalanceFromJournal(company string, year, month int) ([]BalanceDetailRow, error) {
	_, endDate := monthDateBounds(year, month)

	// Step 1: Load opening balances from balance_detail (period end)
	openings := make(map[string]BalanceDetailRow)
	oRows, err := c.db.Query(`
SELECT account_code, account_name, opening_debit, opening_credit
FROM balance_detail
WHERE company = ? AND year = ?
ORDER BY account_code
`, company, year)
	if err == nil {
		defer oRows.Close()
		for oRows.Next() {
			var code, name string
			var od, oc float64
			if err := oRows.Scan(&code, &name, &od, &oc); err == nil {
				openings[code] = BalanceDetailRow{
					AccountCode:  code,
					AccountName:  name,
					Category:     string(CategoryForCode(code)),
					OpeningDebit: od,
					OpeningCred:  oc,
				}
			}
		}
	}

	// Step 2: Aggregate journal entries
	jRows, err := c.db.Query(`
SELECT account_code, account_name,
       COALESCE(SUM(CASE WHEN direction='借' THEN amount ELSE 0 END), 0) as total_debit,
       COALESCE(SUM(CASE WHEN direction='贷' THEN amount ELSE 0 END), 0) as total_credit
FROM journal
WHERE company = ?
  AND DATE(voucher_date) >= DATE(?)
  AND DATE(voucher_date) <= DATE(?)
GROUP BY account_code, account_name
ORDER BY account_code
`, company, fmt.Sprintf("%d-01-01", year), endDate)
	if err != nil {
		return nil, fmt.Errorf("query journal aggregates: %w", err)
	}
	defer jRows.Close()

	results := make(map[string]*BalanceDetailRow)
	for jRows.Next() {
		var code, name string
		var debit, credit float64
		if err := jRows.Scan(&code, &name, &debit, &credit); err != nil {
			return nil, err
		}

		row := &BalanceDetailRow{
			AccountCode:  code,
			AccountName:  name,
			Category:     string(CategoryForCode(code)),
			CurrentDebit: roundTo2(debit),
			CurrentCred:  roundTo2(credit),
		}

		// Apply opening balance
		if ob, ok := openings[code]; ok {
			row.OpeningDebit = ob.OpeningDebit
			row.OpeningCred = ob.OpeningCred
		}

		// Compute closing: depends on direction
		cat := CategoryForCode(code)
		if NormalDirection(cat) == "借" {
			// 借方余额 = 期初借方 - 期初贷方 + 本期借方 - 本期贷方
			balance := (row.OpeningDebit - row.OpeningCred) + (row.CurrentDebit - row.CurrentCred)
			if balance >= 0 {
				row.ClosingDebit = roundTo2(balance)
			} else {
				row.ClosingCred = roundTo2(-balance)
			}
		} else {
			// 贷方余额 = 期初贷方 - 期初借方 + 本期贷方 - 本期借方
			balance := (row.OpeningCred - row.OpeningDebit) + (row.CurrentCred - row.CurrentDebit)
			if balance >= 0 {
				row.ClosingCred = roundTo2(balance)
			} else {
				row.ClosingDebit = roundTo2(-balance)
			}
		}

		results[code] = row
	}

	// Merge with any opening balances that had no journal activity
	for code, ob := range openings {
		if _, exists := results[code]; !exists {
			row := ob
			row.ClosingDebit = ob.OpeningDebit
			row.ClosingCred = ob.OpeningCred
			results[code] = &row
		}
	}

	// Sort and return
	sorted := make([]BalanceDetailRow, 0, len(results))
	for _, v := range results {
		sorted = append(sorted, *v)
	}
	return sorted, nil
}

// IsCashRelated returns true if a journal entry involves bank accounts (1001/1002).
// Used to separate "钱" vs "帐" perspectives.
func IsCashRelated(accountCode string) bool {
	return strings.HasPrefix(accountCode, "1001") || strings.HasPrefix(accountCode, "1002")
}

func lastDayOfMonth(period string) string {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return period + "-28"
	}
	// Add one month and subtract one day to get last day of current month
	return t.AddDate(0, 1, -1).Format("2006-01-02")
}

func roundTo2(v float64) float64 {
	return math.Round(v*100) / 100
}
