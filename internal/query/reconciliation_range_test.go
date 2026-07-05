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
	dbPath := filepath.Join(t.TempDir(), "reconciliation-compare-phrase.sqlite")
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
	defer engine.Close()

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
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok || spec["query_family"] != QueryFamilyReconciliation {
		t.Fatalf("query_spec.query_family = %#v, want reconciliation", spec)
	}
	if strings.Contains(res.Message, "还需要单独查") || !strings.Contains(res.Message, "银行卡上看") || !strings.Contains(res.Message, "账上看") {
		t.Fatalf("message should answer both book and bank in one reconciliation response, got: %s", res.Message)
	}
}
