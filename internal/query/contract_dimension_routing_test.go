package query

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)


func TestBalanceSheetARAPQuestionUsesFinancialAccountNotContractDimension(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "balance-sheet-arap.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE fin_contracts (contract_id TEXT PRIMARY KEY, customer_name TEXT, contract_content TEXT)`,
		`CREATE TABLE fin_fund_income (id INTEGER PRIMARY KEY AUTOINCREMENT, contract_id TEXT, year_month TEXT, source_report_type TEXT, source_sheet_name TEXT, settlement_amount REAL, received_amount REAL, is_invoiced TEXT, invoice_amount REAL)`,
		`CREATE TABLE fin_cost_settlements (id INTEGER PRIMARY KEY AUTOINCREMENT, contract_id TEXT, year_month TEXT, source_report_type TEXT, source_sheet_name TEXT, settlement_amount REAL, paid_amount REAL, is_invoiced TEXT, invoice_amount REAL)`,
		`CREATE TABLE fin_journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount TEXT)`,
		`CREATE TABLE fin_file_mappings (table_type TEXT, period TEXT, company TEXT, storage_key TEXT, file_name TEXT, updated_at TEXT)`,
		`INSERT INTO fin_file_mappings(table_type, period, company, storage_key, file_name, updated_at) VALUES
		 ('balance','2026-Q1','测试公司','finance/南京优集2026.1-3月发生额及余额表.xls','南京优集2026.1-3月发生额及余额表.xls','2026-04-27 13:33:40')`,
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

	res := engine.Query("从账上看，上个完整自然月的应收账款、应付账款和其他应付款分别有多少？")
	if got, ok := res.Data["source_priority"].(string); ok && got == "contract_first" {
		t.Fatalf("balance sheet AR/AP should not go through contract_first, got source_priority=%v", got)
	}
	if strings.Contains(res.Message, "没有识别到合同/项目主体") || strings.Contains(res.Message, "合同信息表") {
		t.Fatalf("balance sheet question should not require contract/project subject, got: %s", res.Message)
	}
	if res.Success && res.Data["source_priority"] == "contract_first" {
		t.Fatalf("balance sheet AR/AP should not use contract_first source priority")
	}
}
