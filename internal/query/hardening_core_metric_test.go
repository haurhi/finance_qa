package query

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMonthlySummaryYTDFallbackUsesRequestedYear(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardening-ytd.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2027-02','1002','货币资金',100,100)`,
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

	res := engine.Query("2027年2月经营状况")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}
	if !strings.Contains(res.Message, "2027年1月以来（YTD）累计") {
		t.Fatalf("message should use requested year, got: %s", res.Message)
	}
	if strings.Contains(res.Message, "2026年1月以来（YTD）累计") {
		t.Fatalf("message should not use hardcoded year, got: %s", res.Message)
	}
}

func TestFallbackHintUsesGenericPlaceholders(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardening-hint.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2026-03','1002','货币资金',100,150)`,
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

	res := engine.Query("帮我随便看一下")
	if res.Success {
		t.Fatalf("expected fallback result, got success with message: %s", res.Message)
	}
	hint, _ := res.Data["hint"].(string)
	if hint == "" {
		t.Fatalf("expected non-empty hint")
	}
	if strings.Contains(hint, "2026") || strings.Contains(hint, "飞未云科") {
		t.Fatalf("hint should be generic instead of hardcoded example, got: %s", hint)
	}
}

func TestMonthEndDayAcceptsFullDateInput(t *testing.T) {
	if got := monthEndDay("2027-02-15"); got != "2027-02-15" {
		t.Fatalf("monthEndDay(full-date) = %q, want %q", got, "2027-02-15")
	}
}

func TestStripTemporalNoiseRemovesAnyYearMonthDayTokens(t *testing.T) {
	got := stripTemporalNoise("2027年金程3月26日")
	if got != "金程" {
		t.Fatalf("stripTemporalNoise() = %q, want %q", got, "金程")
	}
}

func TestIntervalCoreMetricQuestionRoutesToRangeSummary(t *testing.T) {
	cases := []struct {
		question string
		from     string
		to       string
	}{
		{question: "2026年上半年营收", from: "2026-01", to: "2026-06"},
		{question: "2026年全年营收", from: "2026-01", to: "2026-12"},
		{question: "2026年第一季度收入", from: "2026-01", to: "2026-03"},
		{question: "2026年累计利润", from: "2026-01", to: "2026-04"},
	}

	for _, tc := range cases {
		if !isIntervalCoreMetricQuestion(tc.question, "", false, tc.from, tc.to) {
			t.Fatalf("isIntervalCoreMetricQuestion(%q) = false, want true", tc.question)
		}
	}
	if isIntervalCoreMetricQuestion("飞未云科2026年累计销售额多少", "飞未云科", true, "2026-01", "2026-04") {
		t.Fatalf("counterparty cumulative question should not be treated as company range summary")
	}
}

func TestIntervalCoreMetricQuestionClampsToAvailablePeriods(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardening-range-clamp.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2026-03','1002','货币资金',100,150)`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
		 ('测试公司','2026-01','营业收入',100,100),
		 ('测试公司','2026-01','净利润',20,20),
		 ('测试公司','2026-02','营业收入',200,300),
		 ('测试公司','2026-02','净利润',50,70),
		 ('测试公司','2026-03','营业收入',300,600),
		 ('测试公司','2026-03','净利润',80,150)`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary)
		 VALUES ('测试公司','2026-03-31',600,450,'客户A','3月回款')`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty) VALUES
		 ('测试公司','2026-03','2026-03-31','记-0001','600101','技术服务费','确认3月收入','贷',300,0,300,'客户A'),
		 ('测试公司','2026-03','2026-03-31','记-0001','640101','信息服务费','确认3月成本','借',220,220,0,'供应商A')`,
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

	res := engine.Query("2026年上半年营收")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}
	coverage, _ := res.Data["coverage"].(map[string]any)
	if coverage == nil {
		t.Fatalf("expected coverage metadata, got: %+v", res.Data)
	}
	if got := coverage["actual_to"]; got != "2026-03" {
		t.Fatalf("coverage.actual_to = %v, want 2026-03", got)
	}
	if got := coverage["requested_to"]; got != "2026-06" {
		t.Fatalf("coverage.requested_to = %v, want 2026-06", got)
	}
	if !strings.Contains(res.Message, "当前账务数据仅到 2026-03") {
		t.Fatalf("message should disclose available cutoff, got: %s", res.Message)
	}
	if got := res.Data["period"]; got != "2026-01~2026-03" {
		t.Fatalf("period = %v, want 2026-01~2026-03", got)
	}
}

func TestLatestCompleteBookMetricUsesLatestBookDataMonth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardening-latest-book-month.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL)`,
		`CREATE TABLE fin_contracts (contract_id TEXT PRIMARY KEY, customer_name TEXT, contract_content TEXT)`,
		`CREATE TABLE fin_fund_income (id INTEGER PRIMARY KEY AUTOINCREMENT, contract_id TEXT, year_month TEXT, source_report_type TEXT, source_sheet_name TEXT, settlement_amount REAL, received_amount REAL, is_invoiced TEXT, invoice_amount REAL)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty) VALUES
		 ('测试公司','2026-03','2026-03-31','记-0001','600101','主营业务收入','确认3月收入','贷',1000,0,1000,'客户A'),
		 ('测试公司','2026-03','2026-03-31','记-0002','640101','主营业务成本','确认3月成本','借',700,700,0,'供应商A')`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary)
		 VALUES ('测试公司','2026-03-31',800,600,'客户A','3月收付')`,
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

	res := engine.Query("最新完整月份账上净利润是多少？")
	if !res.Success {
		t.Fatalf("query failed: message=%s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-03" {
		t.Fatalf("period = %v, want latest book data month 2026-03; message=%s data=%+v", got, res.Message, res.Data)
	}
	if got := res.Data["data_ready"]; got != true {
		t.Fatalf("data_ready = %v, want true; message=%s data=%+v", got, res.Message, res.Data)
	}
	if strings.Contains(res.Message, "2026-06") || strings.Contains(res.Message, "没有可用数据") {
		t.Fatalf("message should proactively answer the latest book data month instead of June no-data, got: %s", res.Message)
	}
}

func TestExplicitReconciliationQuestionBypassesCoreMetricSummaryRoute(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hardening-reconciliation-route.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL)`,
		`CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT)`,
		`CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT)`,
		`CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL)`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
		 ('测试公司','2026-03','一、营业收入',1000,3000),
		 ('测试公司','2026-03','五、净利润',200,600)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty) VALUES
		 ('测试公司','2026-03','2026-03-10','记-0001','600101','技术服务费','确认客户A收入','贷',1000,0,1000,'客户A'),
		 ('测试公司','2026-03','2026-03-12','记-0002','640101','营业成本','确认供应商A成本','借',700,700,0,'供应商A')`,
		`INSERT INTO bank_statement(company, transaction_date, credit_amount, debit_amount, counterparty_name, summary) VALUES
		 ('测试公司','2026-03-08',650,0,'客户A','3月回款'),
		 ('测试公司','2026-03-20',0,900,'供应商A','3月付款')`,
		`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2026-03','1002','货币资金',100,150)`,
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

	res := engine.Query("为什么2026年3月银行卡上看和账上看的利润不一样？差异最大的3个原因是什么？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	highlights, ok := res.Data["highlights"].([]map[string]any)
	if !ok || len(highlights) == 0 {
		t.Fatalf("explicit reconciliation question should expose highlights from queryReconciliation, got %#v", res.Data["highlights"])
	}
	if _, exists := res.Data["query_pipeline"]; exists {
		t.Fatalf("explicit reconciliation question should not be intercepted by core metric orchestrator, got query_pipeline=%v", res.Data["query_pipeline"])
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok || spec["query_family"] != QueryFamilyReconciliation {
		t.Fatalf("query_spec.query_family = %#v, want %q", spec, QueryFamilyReconciliation)
	}
}

func TestReconciliationQuestionsDoNotMatchCoreMetricShortcuts(t *testing.T) {
	monthlyQuestion := "为什么2026年3月银行卡上看和账上看的利润不一样？差异最大的3个原因是什么？"
	if shouldPreferCoreMetricSummary(monthlyQuestion, "", false, "2026-03", "2026-03") {
		t.Fatalf("monthly reconciliation question should not prefer core metric summary")
	}

	rangeQuestion := "为什么2026年第一季度利润和现金差这么多？"
	if shouldPreferCoreMetricSummary(rangeQuestion, "", false, "2026-01", "2026-03") {
		t.Fatalf("range reconciliation question should not prefer core metric summary")
	}
	if isIntervalCoreMetricQuestion(rangeQuestion, "", false, "2026-01", "2026-03") {
		t.Fatalf("range reconciliation question should not match interval core metric shortcut")
	}
}
