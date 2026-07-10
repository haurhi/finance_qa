package query

import (
	"database/sql"
	"financeqa/internal/accounting"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newReconciliationComparisonEngine(t *testing.T) *Engine {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "reconciliation-comparison.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`CREATE TABLE fin_contracts (contract_id TEXT PRIMARY KEY, customer_name TEXT, contract_content TEXT)`,
		`CREATE TABLE fin_fund_income (id INTEGER PRIMARY KEY AUTOINCREMENT, contract_id TEXT, year_month TEXT, source_report_type TEXT, source_sheet_name TEXT, settlement_amount REAL, received_amount REAL, is_invoiced TEXT, invoice_amount REAL)`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
		 ('测试公司','2026-03','一、营业收入',1000,3000),
		 ('测试公司','2026-03','五、净利润',200,600)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty) VALUES
		 ('测试公司','2026-03','2026-03-31','记-0001','600101','技术服务费','确认3月收入','贷',1000,0,1000,'客户A'),
		 ('测试公司','2026-03','2026-03-31','记-0002','640101','营业成本','确认3月成本','借',800,800,0,'供应商A')`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary) VALUES
		 ('测试公司','2026-03-20',650,0,'客户A','3月回款'),
		 ('测试公司','2026-03-25',0,900,'供应商A','3月付款')`,
		`INSERT INTO fin_contracts(contract_id, customer_name, contract_content) VALUES ('C-JUNE','六月客户','六月项目')`,
		`INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
		 VALUES ('C-JUNE','2026-06','contract_fund_income','26年6月收入明细',100,100,'是',100)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func TestQueryReconciliationAggregatesBookSummaryAcrossRequestedRange(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reconciliation-range.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
		 ('测试公司','2026-01','营业收入',100,100),
		 ('测试公司','2026-01','营业成本',60,60),
		 ('测试公司','2026-01','利润总额',40,40),
		 ('测试公司','2026-01','净利润',40,40),
		 ('测试公司','2026-02','营业收入',200,300),
		 ('测试公司','2026-02','营业成本',120,180),
		 ('测试公司','2026-02','利润总额',80,120),
		 ('测试公司','2026-02','净利润',80,120),
		 ('测试公司','2026-03','营业收入',300,600),
		 ('测试公司','2026-03','营业成本',150,330),
		 ('测试公司','2026-03','利润总额',150,270),
		 ('测试公司','2026-03','净利润',150,270)`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary) VALUES
		 ('测试公司','2026-01-15',100,0,'客户A','1月回款'),
		 ('测试公司','2026-02-18',200,0,'客户A','2月回款'),
		 ('测试公司','2026-03-22',300,0,'客户A','3月回款'),
		 ('测试公司','2026-03-25',0,180,'供应商A','3月付款')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}

	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.queryReconciliation("为什么2026年第一季度利润和现金差这么多？", "2026-01", "2026-03")
	if !res.Success {
		t.Fatalf("queryReconciliation failed: %+v", res)
	}

	bookView, ok := res.Data["book_view"].(monthlyBookView)
	if !ok {
		t.Fatalf("book_view type mismatch: %T", res.Data["book_view"])
	}
	if bookView.Revenue != 600 {
		t.Fatalf("book_view.Revenue = %.2f, want 600.00", bookView.Revenue)
	}
	if bookView.TotalCost != 330 {
		t.Fatalf("book_view.TotalCost = %.2f, want 330.00", bookView.TotalCost)
	}
	if bookView.Profit != 270 {
		t.Fatalf("book_view.Profit = %.2f, want 270.00", bookView.Profit)
	}
	if got, _ := res.Data["period"].(string); got != "2026-01~2026-03" {
		t.Fatalf("period = %q, want 2026-01~2026-03", got)
	}
	if got := res.Message; got == "" || strings.HasPrefix(got, "2026-03 我拆成两层给你看") {
		t.Fatalf("message should use range period label, got: %s", got)
	}
}

func TestReconciliationQuestionIsNotCashOnHandBalance(t *testing.T) {
	question := "为什么2026年第一季度利润和现金差这么多？；补充识别：账上利润和银行净流入差异"
	if !shouldUseReconciliation(question) {
		t.Fatalf("question should retain reconciliation intent: %q", question)
	}
	if shouldUseCashOnHandBalanceQuestion(question) {
		t.Fatalf("cash-on-hand balance must not capture reconciliation question: %q", question)
	}
}

func TestLatestReconciliationUsesLatestCommonBookAndBankMonth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reconciliation-latest-common-month.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`CREATE TABLE fin_contracts (contract_id TEXT PRIMARY KEY, customer_name TEXT, contract_content TEXT)`,
		`CREATE TABLE fin_fund_income (id INTEGER PRIMARY KEY AUTOINCREMENT, contract_id TEXT, year_month TEXT, source_report_type TEXT, source_sheet_name TEXT, settlement_amount REAL, received_amount REAL, is_invoiced TEXT, invoice_amount REAL)`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
		 ('测试公司','2026-03','一、营业收入',1000,3000),
		 ('测试公司','2026-03','五、净利润',200,600)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty) VALUES
		 ('测试公司','2026-03','2026-03-31','记-0001','600101','技术服务费','确认3月收入','贷',1000,0,1000,'客户A'),
		 ('测试公司','2026-03','2026-03-31','记-0002','640101','营业成本','确认3月成本','借',800,800,0,'供应商A')`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary) VALUES
		 ('测试公司','2026-03-20',650,900,'客户A','3月收付')`,
		`INSERT INTO fin_contracts(contract_id, customer_name, contract_content) VALUES ('C-JUNE','六月客户','六月项目')`,
		`INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
		 VALUES ('C-JUNE','2026-06','contract_fund_income','26年6月收入明细',100,100,'是',100)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("为什么最新完整月份银行卡上看和账上看的利润不一样？差异是多少？")
	if !res.Success {
		t.Fatalf("query failed: message=%s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-03" {
		t.Fatalf("period = %v, want latest common book/bank month 2026-03; message=%s data=%+v", got, res.Message, res.Data)
	}
	cash, ok := res.Data["cash_view"].(*accounting.CashPerspective)
	if !ok {
		t.Fatalf("cash_view = %#v, want *accounting.CashPerspective", res.Data["cash_view"])
	}
	if cash.Net != float64(-250) {
		t.Fatalf("net_cash_inflow = %v, want -250; data=%+v", cash.Net, res.Data)
	}
	book, ok := res.Data["book_view"].(monthlyBookView)
	if !ok {
		t.Fatalf("book_view = %#v, want monthlyBookView", res.Data["book_view"])
	}
	if book.NetProfit != float64(200) {
		t.Fatalf("book net profit = %v, want 200; data=%+v", book.NetProfit, res.Data)
	}
	if strings.Contains(res.Message, "2026-06") || strings.Contains(res.Message, "0.00 元；银行卡上看收款 0.00") {
		t.Fatalf("message should not answer unavailable June zeros, got: %s", res.Message)
	}
}

func TestCompareBookProfitAndBankNetInflowUsesReconciliationRoute(t *testing.T) {
	engine := newReconciliationComparisonEngine(t)

	res := engine.Query("对比一下最近完整月份账上利润和银行流水净流入。")
	if !res.Success {
		t.Fatalf("query failed: message=%s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-03" {
		t.Fatalf("period = %v, want latest common book/bank month 2026-03; message=%s data=%+v", got, res.Message, res.Data)
	}
	if _, ok := res.Data["cash_view"].(*accounting.CashPerspective); !ok {
		t.Fatalf("cash_view = %#v, want reconciliation cash view", res.Data["cash_view"])
	}
	if _, ok := res.Data["book_view"].(monthlyBookView); !ok {
		t.Fatalf("book_view = %#v, want reconciliation book view", res.Data["book_view"])
	}
	sourceTables := anySourceStringSlice(res.Data["source_tables"])
	if !containsString(sourceTables, "fin_bank_statement") || !containsString(sourceTables, "fin_journal") {
		t.Fatalf("source_tables = %#v, want both bank and journal sources; data=%+v", sourceTables, res.Data)
	}
	facts, ok := res.Data["finance_facts"].(map[string]any)
	if !ok {
		t.Fatalf("finance_facts missing: data=%+v", res.Data)
	}
	if got := anyToString(facts["basis"]); got != "账上利润与银行流水双口径对账" {
		t.Fatalf("finance_facts.basis = %q, want reconciliation business basis; facts=%+v data=%+v", got, facts, res.Data)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok || spec["query_family"] != QueryFamilyReconciliation {
		t.Fatalf("query_spec.query_family = %#v, want reconciliation", spec)
	}
	if strings.Contains(res.Message, "还需要单独查") || !strings.Contains(res.Message, "银行卡上看") || !strings.Contains(res.Message, "账上看") {
		t.Fatalf("message should answer both book and bank in one reconciliation response, got: %s", res.Message)
	}
}

func TestReconciliationAlwaysPublishesThreeFacts(t *testing.T) {
	const (
		bankNetFlowLabel   = "银行净流入"
		bookNetProfitLabel = "账上净利润"
		nominalDifference  = "名义差额（账上净利润-银行净流入）"
		differenceAtom     = "差异金额（名义差额，账上净利润-银行净流入）"
		differenceHeadline = "账上净利润-银行净流入名义差额"
	)

	tests := []struct {
		name    string
		query   string
		promote bool
	}{
		{name: "compare", query: "对比一下最近完整月份账上利润和银行流水净流入。", promote: true},
		{name: "compare synonym", query: "比较最近完整月份账上利润和银行流水净流入。", promote: true},
		{name: "difference amount", query: "最近完整月份账上利润和银行流水净流入差了多少？", promote: true},
		{name: "explanation with explicit amount", query: "为什么最近完整月份账上利润和银行净流入不一样？差异是多少？", promote: true},
		{name: "explanation", query: "为什么最近完整月份账上利润和银行净流入差这么多？", promote: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := newReconciliationComparisonEngine(t)
			res := engine.Query(tc.query)
			if !res.Success {
				t.Fatalf("query failed: message=%s data=%+v", res.Message, res.Data)
			}

			spec, ok := res.Data["query_spec"].(map[string]any)
			if !ok || spec["query_family"] != QueryFamilyReconciliation {
				t.Errorf("query_spec.query_family = %#v, want reconciliation; data=%+v", spec, res.Data)
			}
			if got := anyToString(res.Data["business_basis"]); got != "账上利润与银行流水双口径对账" {
				t.Errorf("business_basis = %q, want reconciliation result", got)
			}

			facts, ok := res.Data["finance_facts"].(map[string]any)
			if !ok {
				t.Fatalf("finance_facts missing: data=%+v", res.Data)
			}
			metrics, ok := facts["metrics"].(map[string]any)
			if !ok {
				t.Errorf("finance_facts.metrics missing: facts=%+v", facts)
				metrics = map[string]any{}
			}
			for label, want := range map[string]float64{
				bankNetFlowLabel:   -250,
				bookNetProfitLabel: 200,
				nominalDifference:  450,
			} {
				if got := anyToFloat64(metrics[label]); got != want {
					t.Errorf("finance_facts.metrics[%q] = %v, want %v; metrics=%+v", label, got, want, metrics)
				}
			}

			requiredAtoms := anySourceStringSlice(facts["required_atoms"])
			wantRequiredAtoms := []string{
				bankNetFlowLabel + "：-250.00 元",
				bookNetProfitLabel + "：200.00 元",
				differenceAtom + "：450.00 元",
			}
			if len(requiredAtoms) < len(wantRequiredAtoms) {
				t.Errorf("finance_facts.required_atoms = %#v, want ordered reconciliation facts %#v", requiredAtoms, wantRequiredAtoms)
			} else {
				for i, want := range wantRequiredAtoms {
					if got := requiredAtoms[i]; got != want {
						t.Errorf("finance_facts.required_atoms[%d] = %q, want %q; atoms=%#v", i, got, want, requiredAtoms)
					}
				}
			}

			reconcileFacts, ok := res.Data["cash_profit_reconciliation"].(map[string]any)
			if !ok {
				t.Errorf("cash_profit_reconciliation missing: data=%+v", res.Data)
			} else {
				for key, want := range map[string]float64{
					"bank_net_flow":     -250,
					"book_net_profit":   200,
					"difference_amount": 450,
				} {
					if got := anyToFloat64(reconcileFacts[key]); got != want {
						t.Errorf("cash_profit_reconciliation[%s] = %v, want %v; facts=%+v", key, got, want, reconcileFacts)
					}
				}
			}

			differenceSummary, ok := res.Data["difference_summary"].(map[string]any)
			if !ok || anyToFloat64(differenceSummary["nominal_difference"]) != 450 {
				t.Errorf("difference_summary.nominal_difference = %v, want 450; summary=%+v", differenceSummary["nominal_difference"], differenceSummary)
			}
			hints := strings.Join(anySourceStringSlice(facts["explanation_hints"]), "\n")
			if !strings.Contains(hints, "不是同一口径") || !strings.Contains(hints, "只作对账入口") {
				t.Errorf("finance_facts.explanation_hints must preserve basis and nominal-difference caveats: %q", hints)
			}

			if tc.promote {
				if got := anyToString(facts["headline_metric"]); got != differenceHeadline {
					t.Errorf("finance_facts.headline_metric = %q, want %q", got, differenceHeadline)
				}
				if got := anyToFloat64(facts["headline_amount"]); got != 450 {
					t.Errorf("finance_facts.headline_amount = %v, want 450", got)
				}
				if !strings.Contains(res.Message, "按名义差额看") || !strings.Contains(res.Message, "450.00 元") {
					t.Errorf("quantitative reconciliation message should promote nominal difference, got: %s", res.Message)
				}
			} else {
				if _, ok := res.Data["headline_metric"]; ok {
					t.Errorf("explanation reconciliation must keep narrative headline, data=%+v", res.Data)
				}
				if _, ok := res.Data["headline_amount"]; ok {
					t.Errorf("explanation reconciliation must keep narrative headline amount, data=%+v", res.Data)
				}
				if got := anyToString(facts["headline_metric"]); got == differenceHeadline {
					t.Errorf("explanation reconciliation must not promote difference headline: facts=%+v", facts)
				}
				if got := anyToFloat64(facts["headline_amount"]); got != -250 {
					t.Errorf("explanation reconciliation headline_amount = %v, want existing narrative cash amount -250", got)
				}
				if strings.Contains(res.Message, "按名义差额看") || !strings.Contains(res.Message, "我拆成两层给你看") {
					t.Errorf("explanation reconciliation must keep narrative message, got: %s", res.Message)
				}
			}
		})
	}
}

func TestCompareBookProfitAndBankNetInflowStatesNominalDifference(t *testing.T) {
	engine := newReconciliationComparisonEngine(t)

	res := engine.Query("上个月银行净流入和账上净利润差多少？")
	if !res.Success {
		t.Fatalf("query failed: message=%s data=%+v", res.Message, res.Data)
	}
	if !strings.Contains(res.Message, "名义差额") || !strings.Contains(res.Message, "450.00") {
		t.Fatalf("message should explicitly state nominal difference 450.00, got: %s", res.Message)
	}
	diff, ok := res.Data["difference_summary"].(map[string]any)
	if !ok {
		t.Fatalf("difference_summary missing: data=%+v", res.Data)
	}
	if got := diff["nominal_difference"]; got != float64(450) {
		t.Fatalf("nominal_difference = %v, want 450; difference_summary=%+v", got, diff)
	}
	facts, ok := res.Data["finance_facts"].(map[string]any)
	if !ok {
		t.Fatalf("finance_facts missing: data=%+v", res.Data)
	}
	if got := anyToFloat64(facts["headline_amount"]); got != float64(450) {
		t.Fatalf("finance_facts.headline_amount = %v, want 450; facts=%+v", got, facts)
	}
	if got := anyToString(facts["headline_metric"]); got != "账上净利润-银行净流入名义差额" {
		t.Fatalf("finance_facts.headline_metric = %q, want fixed reconciliation metric; facts=%+v", got, facts)
	}
	metrics, ok := facts["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("finance_facts.metrics missing: facts=%+v", facts)
	}
	for key, want := range map[string]float64{
		"银行净流入":       -250,
		"账上净利润":       200,
		"差异金额":        450,
		"账上净利润-银行净流入": 450,
	} {
		if got := anyToFloat64(metrics[key]); got != want {
			t.Fatalf("finance_facts.metrics[%s] = %v, want %v; metrics=%+v facts=%+v", key, got, want, metrics, facts)
		}
	}
	requiredAtoms := anySourceStringSlice(facts["required_atoms"])
	for _, want := range []string{
		"银行净流入：-250.00 元",
		"账上净利润：200.00 元",
		"差异金额（名义差额，账上净利润-银行净流入）：450.00 元",
	} {
		if !containsString(requiredAtoms, want) {
			t.Fatalf("finance_facts.required_atoms missing %q: %#v", want, requiredAtoms)
		}
	}
	reconcileFacts, ok := res.Data["cash_profit_reconciliation"].(map[string]any)
	if !ok {
		t.Fatalf("cash_profit_reconciliation missing: data=%+v", res.Data)
	}
	for key, want := range map[string]float64{
		"bank_net_flow":     -250,
		"book_net_profit":   200,
		"difference_amount": 450,
	} {
		if got := anyToFloat64(reconcileFacts[key]); got != want {
			t.Fatalf("cash_profit_reconciliation[%s] = %v, want %v; facts=%+v", key, got, want, reconcileFacts)
		}
	}
	if got := anyToString(reconcileFacts["formula"]); got != "账上净利润 - 银行净流入" {
		t.Fatalf("cash_profit_reconciliation.formula = %q, want fixed formula; facts=%+v", got, reconcileFacts)
	}
}
