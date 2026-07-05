package query

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestContractAggregateCustomerConcentrationRanksCustomers(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 22, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("Q1 的收入主要靠哪几个客户?会不会太依赖某一两家?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["source_priority"]; got != "contract_first" {
		t.Fatalf("source_priority = %v, want contract_first; message=%s data=%+v", got, res.Message, res.Data)
	}
	if got := res.Data["total"]; got != float64(2000) {
		t.Fatalf("total = %v, want settlement total 2000", got)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	ranking := requireMapSlice(t, summary["revenue_customer_ranking"], "revenue_customer_ranking")
	if len(ranking) != 3 {
		t.Fatalf("ranking len = %d, want 3: %#v", len(ranking), ranking)
	}
	if ranking[0]["customer_name"] != "甲客户" || ranking[0]["settlement_amount"] != float64(1000) {
		t.Fatalf("top customer = %#v, want 甲客户 1000", ranking[0])
	}
	if summary["top2_revenue_share"] != float64(0.8) {
		t.Fatalf("top2_revenue_share = %v, want 0.8", summary["top2_revenue_share"])
	}
	if summary["top2_revenue_settlement"] != float64(1600) {
		t.Fatalf("top2_revenue_settlement = %v, want 1600", summary["top2_revenue_settlement"])
	}
	if !strings.Contains(res.Message, "甲客户") ||
		!strings.Contains(res.Message, "前两家合计 1600.00 元") ||
		!strings.Contains(res.Message, "约80%") {
		t.Fatalf("message should include customer concentration detail, got: %s", res.Message)
	}
}

func TestContractAggregateCostRankingUsesLatestAvailableProjectPeriod(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("我们的钱主要花在哪几家供应商身上?最大的几笔采购成本是什么?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-01~2026-03" {
		t.Fatalf("period = %v, want latest available project quarter 2026-01~2026-03; data=%+v", got, res.Data)
	}
	if got := res.Data["total"]; got != float64(900) {
		t.Fatalf("total = %v, want project cost 900", got)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	items := requireMapSlice(t, summary["cost_items"], "cost_items")
	if len(items) != 3 {
		t.Fatalf("cost_items len = %d, want 3: %#v", len(items), items)
	}
	if items[0]["supplier_name"] != "甲供应商" || items[0]["settlement_amount"] != float64(500) {
		t.Fatalf("top supplier cost = %#v, want 甲供应商 500", items[0])
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	if got := spec["period_to"]; got != "2026-03" {
		t.Fatalf("query_spec.period_to = %v, want 2026-03", got)
	}
}

func TestContractAggregateReceivableOutstandingDoesNotRequireEntity(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 22, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("有哪些客户这季度结算了、但发票或回款还没到位的?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["source_priority"]; got != "contract_first" {
		t.Fatalf("source_priority = %v, want contract_first; message=%s data=%+v", got, res.Message, res.Data)
	}
	if strings.Contains(res.Message, "没有识别到合同/项目主体") {
		t.Fatalf("company-scope receivable outstanding should not require entity, got: %s", res.Message)
	}
	if got := res.Data["total"]; got != float64(600) {
		t.Fatalf("total = %v, want project receivable 600", got)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	items := requireMapSlice(t, summary["receivable_open_items"], "receivable_open_items")
	if len(items) != 2 {
		t.Fatalf("receivable_open_items len = %d, want 2: %#v", len(items), items)
	}
	customerRollup := requireMapSlice(t, summary["receivable_open_customer_ranking"], "receivable_open_customer_ranking")
	if len(customerRollup) != 2 {
		t.Fatalf("receivable_open_customer_ranking len = %d, want 2: %#v", len(customerRollup), customerRollup)
	}
	if customerRollup[0]["open_amount"] != float64(300) {
		t.Fatalf("top receivable open_amount = %v, want 300", customerRollup[0]["open_amount"])
	}
	if !strings.Contains(res.Message, "甲客户") || !strings.Contains(res.Message, "乙客户") {
		t.Fatalf("message should include open receivable customers, got: %s", res.Message)
	}
	if !strings.Contains(res.Message, "客户挂账汇总") {
		t.Fatalf("message should include customer-level receivable rollup, got: %s", res.Message)
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	families := anySourceStringSlice(spec["semantic_families"])
	if !containsString(families, "receivable_outstanding") {
		t.Fatalf("semantic_families = %#v, want receivable_outstanding", families)
	}
}

func TestContractAggregateCustomerReceivableUsesEntityHeadlineFacts(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 22, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("帮我看下客户甲客户，从2026年1月到2026年3月，项目应收未收合计是多少？")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["total"]; got != float64(300) {
		t.Fatalf("total = %v, want customer-scoped receivable 300 instead of company total; message=%s data=%+v", got, res.Message, res.Data)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	if got := summary["scope"]; got != "entity" {
		t.Fatalf("contract_summary.scope = %v, want entity; summary=%+v", got, summary)
	}
	if got := summary["entity"]; got != "甲客户" {
		t.Fatalf("contract_summary.entity = %v, want 甲客户; summary=%+v", got, summary)
	}
	facts := requireMap(t, res.Data["finance_facts"], "finance_facts")
	if got := facts["headline_amount"]; got != float64(300) {
		t.Fatalf("finance_facts.headline_amount = %v, want customer receivable 300; facts=%+v", got, facts)
	}
	if got, _ := facts["headline_metric"].(string); !strings.Contains(got, "项目应收") {
		t.Fatalf("finance_facts.headline_metric = %q, want 项目应收 label; facts=%+v", got, facts)
	}
}

func TestContractAggregateCollectionPriorityUsesCustomerOpenRollup(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("现在应收挂账最多、最该去催的是哪一家?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	customerRollup := requireMapSlice(t, summary["receivable_open_customer_ranking"], "receivable_open_customer_ranking")
	if len(customerRollup) != 2 {
		t.Fatalf("receivable_open_customer_ranking len = %d, want 2: %#v", len(customerRollup), customerRollup)
	}
	if customerRollup[0]["open_amount"] != float64(300) {
		t.Fatalf("top receivable open_amount = %v, want 300", customerRollup[0]["open_amount"])
	}
	if !strings.Contains(res.Message, "优先催收") || !strings.Contains(res.Message, "催") {
		t.Fatalf("collection priority answer should use催收 wording, got: %s", res.Message)
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	families := anySourceStringSlice(spec["semantic_families"])
	if !containsString(families, "collection_priority") {
		t.Fatalf("semantic_families = %#v, want collection_priority", families)
	}
}

func TestContractAggregateCollectionPrioritySplitsCurrentQuarterOpen(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('R-APR-001','甲客户','甲项目三');
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('R-APR-001','2026-04','contract_mixed_finance','Q2收入明细',500,100,'否',0)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed april receivable: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("现在应收挂账最多、最该去催的是哪一家?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	buckets := requireMapSlice(t, summary["receivable_open_customer_period_buckets"], "receivable_open_customer_period_buckets")
	if len(buckets) == 0 {
		t.Fatalf("period buckets should not be empty: %#v", summary)
	}
	if buckets[0]["customer_name"] != "甲客户" || buckets[0]["prior_open_amount"] != float64(300) || buckets[0]["current_open_amount"] != float64(400) {
		t.Fatalf("top bucket = %#v, want 甲客户 prior=300 current=400", buckets[0])
	}
	if !strings.Contains(res.Message, "2026-01~2026-03未回款 300.00 元") ||
		!strings.Contains(res.Message, "2026-04未回款 400.00 元") {
		t.Fatalf("message should include prior/current quarter open split, got: %s", res.Message)
	}
}

func TestContractAggregateMarginUsesLatestSharedRevenueCostPeriod(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("按收入减成本算,大概的毛利是什么水平?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-01~2026-03" {
		t.Fatalf("period = %v, want latest shared revenue/cost project quarter; data=%+v", got, res.Data)
	}
	metrics := requireMap(t, res.Data["metrics"], "metrics")
	if metrics["收入"] != float64(2000) || metrics["成本"] != float64(900) || metrics["利润"] != float64(1100) {
		t.Fatalf("metrics = %#v, want revenue=2000 cost=900 profit=1100", metrics)
	}
	if !strings.Contains(res.Message, "项目毛利") || !strings.Contains(res.Message, "财报净利口径 275.00 元") {
		t.Fatalf("message should distinguish project gross margin and financial net profit, got: %s", res.Message)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	if summary["gross_margin"] != float64(1100) || summary["net_profit_context"] != float64(275) {
		t.Fatalf("contract_summary = %#v, want gross_margin=1100 net_profit_context=275", summary)
	}
}

func TestContractAggregateRevenueComparisonAddsCurrentMonthAndBaselineAverage(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('R-APR-001','丁客户','4月项目');
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('R-APR-001','2026-04','contract_mixed_finance','Q2收入明细',300,0,'否',0)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed april revenue: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("进入 4 月,收入和 Q1 比起来怎么样?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	summary := requireMap(t, res.Data["contract_summary"], "contract_summary")
	comparison := requireMap(t, summary["period_comparison"], "period_comparison")
	if comparison["current_revenue"] != float64(300) {
		t.Fatalf("current_revenue = %v, want 300", comparison["current_revenue"])
	}
	if comparison["baseline_revenue"] != float64(2000) {
		t.Fatalf("baseline_revenue = %v, want 2000", comparison["baseline_revenue"])
	}
	if comparison["baseline_monthly_average"] != float64(666.67) {
		t.Fatalf("baseline_monthly_average = %v, want 666.67", comparison["baseline_monthly_average"])
	}
	if !strings.Contains(res.Message, "4月收入 300.00 元") || !strings.Contains(res.Message, "月均 666.67 元") {
		t.Fatalf("message should include current month vs baseline average, got: %s", res.Message)
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	families := anySourceStringSlice(spec["semantic_families"])
	if !containsString(families, "period_revenue_comparison") {
		t.Fatalf("semantic_families = %#v, want period_revenue_comparison", families)
	}
}

func TestFinanceTrendQuestionUsesProjectWorkbookTrend(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('R-APR-001','丁客户','4月项目');
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('R-APR-001','2026-04','contract_mixed_finance','Q2收入明细',300,0,'否',0)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed april revenue: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("到现在这版,跟上个月那版比,账面是好转还是变差了?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	trend := requireMap(t, res.Data["workbook_trend"], "workbook_trend")
	if trend["q1_settlement"] != float64(2000) || trend["q1_cost"] != float64(900) || trend["business_margin"] != float64(1100) {
		t.Fatalf("workbook_trend = %#v, want q1 settlement/cost/margin", trend)
	}
	if trend["first_month_after_baseline_settlement"] != float64(300) {
		t.Fatalf("first_month_after_baseline_settlement = %v, want 300", trend["first_month_after_baseline_settlement"])
	}
	if !strings.Contains(res.Message, "变差") || !strings.Contains(res.Message, "回款") {
		t.Fatalf("trend message should include degraded collection wording, got: %s", res.Message)
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"versioned_workbook_trend", "business_settlement_trend"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %s", families, want)
		}
	}
}

func TestFinanceAnomalyQuestionSummarizesProjectAndCashSignals(t *testing.T) {
	dbPath := buildContractAggregateAnalysisDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('R-APR-001','丁客户','4月项目'),
       ('R-USDT-001','外币客户','USDT'),
       ('C-BIG-001','大额供应商','一次性历史数据采购');
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('R-APR-001','2026-04','contract_mixed_finance','Q2收入明细',300,0,'否',0),
       ('R-USDT-001','2026-03','contract_mixed_finance','Q1收入明细',91,91,'是',91),
       ('R-USDT-001','2026-04','contract_mixed_finance','Q2收入明细',123,123,'是',123);
INSERT INTO fin_cost_settlements(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, paid_amount, is_invoiced, invoice_amount)
VALUES ('C-BIG-001','2026-01','contract_mixed_finance','成本-月度结算',2450,2450,'是',2450)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed anomaly facts: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("这些账里有没有对不上、或者看着不太对劲的地方?")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	anomaly := requireMap(t, res.Data["finance_anomaly"], "finance_anomaly")
	if anomaly["top2_revenue_share"] != float64(0.7652) {
		t.Fatalf("top2_revenue_share = %v, want 0.7652", anomaly["top2_revenue_share"])
	}
	usdt := requireMap(t, anomaly["tagged_revenue"], "tagged_revenue")
	if usdt["q1_amount"] != float64(91) || usdt["q2_amount"] != float64(123) {
		t.Fatalf("tagged_revenue = %#v, want q1/q2 USDT-tagged amounts", usdt)
	}
	if !strings.Contains(res.Message, "USDT") || !strings.Contains(res.Message, "一次性") || !strings.Contains(res.Message, "客户集中度") {
		t.Fatalf("anomaly message should include generic anomaly signals, got: %s", res.Message)
	}
	spec := requireMap(t, res.Data["query_spec"], "query_spec")
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"finance_anomaly", "versioned_workbook_anomaly"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %s", families, want)
		}
	}
}

func buildContractAggregateAnalysisDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "contract-aggregate-analysis.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE income_statement (
			company TEXT,
			period TEXT,
			item_name TEXT,
			current_amount REAL,
			cumulative_amount REAL
		)`,
		`CREATE TABLE journal (
			company TEXT,
			period TEXT,
			voucher_date TEXT,
			voucher_no TEXT,
			account_code TEXT,
			account_name TEXT,
			summary TEXT,
			direction TEXT,
			amount REAL,
			debit_amount REAL,
			credit_amount REAL,
			counterparty TEXT
		)`,
		`CREATE TABLE bank_statement (
			company TEXT,
			transaction_date TEXT,
			counterparty_name TEXT,
			summary TEXT,
			debit_amount REAL,
			credit_amount REAL
		)`,
		`CREATE TABLE balance_sheet (
			company TEXT,
			period TEXT,
			account_code TEXT,
			account_name TEXT,
			opening_balance REAL,
			closing_balance REAL
		)`,
		`CREATE TABLE balance_detail (
			company TEXT,
			period TEXT,
			account_code TEXT,
			account_name TEXT,
			opening_debit REAL,
			opening_credit REAL,
			closing_debit REAL,
			closing_credit REAL
		)`,
		`CREATE TABLE fin_contracts (
			contract_id TEXT PRIMARY KEY,
			customer_name TEXT,
			contract_content TEXT
		)`,
		`CREATE TABLE fin_fund_income (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			contract_id TEXT,
			year_month TEXT,
			source_report_type TEXT,
			source_sheet_name TEXT,
			settlement_amount REAL,
			received_amount REAL,
			is_invoiced TEXT,
			invoice_amount REAL
		)`,
		`CREATE TABLE fin_cost_settlements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			contract_id TEXT,
			year_month TEXT,
			source_report_type TEXT,
			source_sheet_name TEXT,
			settlement_amount REAL,
			paid_amount REAL,
			is_invoiced TEXT,
			invoice_amount REAL
		)`,
		`INSERT INTO fin_contracts(contract_id, customer_name, contract_content) VALUES
			('R-001','甲客户','甲项目一'),
			('R-002','甲客户','甲项目二'),
			('R-003','乙客户','乙项目'),
			('R-004','丙客户','丙项目'),
			('C-001','甲供应商','甲采购项目'),
			('C-002','乙供应商','乙采购项目'),
			('C-003','丙供应商','丙采购项目')`,
		`INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount) VALUES
			('R-001','2026-01','contract_fund_income','Q1收入明细',600,600,'是',600),
			('R-002','2026-02','contract_fund_income','Q1收入明细',400,100,'是',300),
			('R-003','2026-03','contract_fund_income','Q1收入明细',600,300,'否',0),
			('R-004','2026-03','contract_fund_income','Q1收入明细',400,400,'是',400)`,
		`INSERT INTO fin_cost_settlements(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, paid_amount, is_invoiced, invoice_amount) VALUES
			('C-001','2026-01','contract_mixed_finance','Q1成本结算',500,200,'是',500),
			('C-002','2026-02','contract_mixed_finance','Q1成本结算',250,100,'是',250),
			('C-003','2026-03','contract_mixed_finance','Q1成本结算',150,0,'否',0)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty)
		 VALUES ('测试公司','2026-04','2026-04-30','J-NEW-1','6001','主营业务收入','4月账务更新','贷',100,0,100,'其他客户')`,
		`INSERT INTO income_statement(company, period, item_name, current_amount, cumulative_amount) VALUES
			('测试公司','2026-01','营业收入',700,700),
			('测试公司','2026-02','营业收入',600,1300),
			('测试公司','2026-03','营业收入',700,2000),
			('测试公司','2026-01','营业成本',200,200),
			('测试公司','2026-02','营业成本',300,500),
			('测试公司','2026-03','营业成本',400,900),
			('测试公司','2026-01','净利润',100,100),
			('测试公司','2026-02','净利润',75,175),
			('测试公司','2026-03','净利润',100,275)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}
	return dbPath
}

func requireMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want map[string]any", name, value)
	}
	return m
}

func requireMapSlice(t *testing.T, value any, name string) []map[string]any {
	t.Helper()
	items, ok := value.([]map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want []map[string]any", name, value)
	}
	return items
}
