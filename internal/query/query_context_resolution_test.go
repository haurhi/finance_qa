package query

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestResolveQueryRoutingPromotesContractPriorityToContractDimension(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("飞未云科2026年累计销售额多少？")
	if route.entity != "飞未云科（深圳）技术有限公司" {
		t.Fatalf("entity = %q, want %q", route.entity, "飞未云科（深圳）技术有限公司")
	}
	if route.spec.QueryFamily != QueryFamilyContractDimension {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyContractDimension)
	}
	if route.spec.PeriodFrom != "2026-01" || route.spec.PeriodTo != "2026-03" {
		t.Fatalf("period = %s~%s, want 2026-01~2026-03", route.spec.PeriodFrom, route.spec.PeriodTo)
	}
	if !route.hasRealEntity {
		t.Fatalf("expected hasRealEntity=true")
	}
}

func TestResolveQueryRoutingPromotesBareCumulativeContractQuestionToYTD(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("飞未云科累计销售额多少？")
	if route.entity != "飞未云科（深圳）技术有限公司" {
		t.Fatalf("entity = %q, want %q", route.entity, "飞未云科（深圳）技术有限公司")
	}
	if route.spec.QueryFamily != QueryFamilyContractDimension {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyContractDimension)
	}
	if route.spec.PeriodFrom != "2026-01" || route.spec.PeriodTo != "2026-03" {
		t.Fatalf("period = %s~%s, want 2026-01~2026-03", route.spec.PeriodFrom, route.spec.PeriodTo)
	}
	if !route.hasRealEntity {
		t.Fatalf("expected hasRealEntity=true")
	}
}

func TestResolveQueryRoutingTreatsExplicitBankCashAsCompanyCash(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("2026年3月银行卡实际到账多少？")
	if route.spec.SourceConstraint != BossSourceBankStatement {
		t.Fatalf("source_constraint = %q, want %q", route.spec.SourceConstraint, BossSourceBankStatement)
	}
	if route.spec.QueryFamily == QueryFamilyCounterparty {
		t.Fatalf("query_family = %s, want non-counterparty company cash route", route.spec.QueryFamily)
	}
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want no business entity", route.entity, route.hasRealEntity)
	}
}

func TestExplicitBankCashReceiptQueryAnswersFromBankStatement(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2026年3月银行卡实际到账多少？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	if !strings.Contains(res.Message, "实际到账 1200.00 元") {
		t.Fatalf("message = %q, want actual receipt amount", res.Message)
	}
	if !strings.Contains(res.Message, "来源：《银行流水》") {
		t.Fatalf("message = %q, want bank source disclosure", res.Message)
	}
}

func TestExplicitBankCashFlowQueryAnswersAllRequestedAmounts(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2026年3月银行卡上实际到账、实际支出、净增加分别是多少？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	for _, want := range []string{"实际到账 1200.00 元", "实际支出 500.00 元", "净增加 700.00 元"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("message = %q, want include %q", res.Message, want)
		}
	}
}

func TestLatestCompleteBankCashFlowUsesLatestBankDataMonth(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("银行卡最近完整月份实际到账和实际支出分别是多少？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period_to"]; got != "2026-03" {
		t.Fatalf("period_to = %v, want latest bank data month 2026-03; message=%s data=%+v", got, res.Message, res.Data)
	}
	if !strings.Contains(res.Message, "2026-03") {
		t.Fatalf("message should answer from latest bank data month, got: %s", res.Message)
	}
	if strings.Contains(res.Message, "2026-06") {
		t.Fatalf("message should not answer the natural previous month when bank data is missing, got: %s", res.Message)
	}
	if got := anyToFloat64(res.Data["bank_credit_total"]); got != 1200 {
		t.Fatalf("bank_credit_total = %v, want 1200; data=%+v", got, res.Data)
	}
	if got := anyToFloat64(res.Data["bank_debit_total"]); got != 500 {
		t.Fatalf("bank_debit_total = %v, want 500; data=%+v", got, res.Data)
	}
}

func TestCashOnHandQuestionAnswersBalanceAndBankFlow(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("账上现在还有多少现金?比年初是多了还是少了?")
	if route.spec.SourceConstraint != BossSourceBalance {
		t.Fatalf("source_constraint = %q, want balance", route.spec.SourceConstraint)
	}
	res := engine.Query("账上现在还有多少现金?比年初是多了还是少了?")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	for key, want := range map[string]float64{
		"cash_opening_balance": 100,
		"cash_closing_balance": 150,
		"bank_credit_total":    1200,
		"bank_debit_total":     500,
		"net_cash_inflow":      700,
	} {
		if got := anyToFloat64(res.Data[key]); got != want {
			t.Fatalf("%s = %v, want %v; data=%+v", key, got, want, res.Data)
		}
	}
	for _, want := range []string{"现金期末余额 150.00 元", "年初 100.00 元", "多了 50.00 元", "实际流入 1200.00 元", "实际流出 500.00 元"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("message = %q, want include %q", res.Message, want)
		}
	}
	if got := anySourceStringSlice(res.Data["source_tables"]); !containsString(got, "fin_balance_detail") {
		t.Fatalf("source_tables = %#v, want balance_detail as cash balance source", got)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"cash_balance", "bank_cash_flow", "balance_sheet"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %q", families, want)
		}
	}
}

func TestCashOnHandWithAsOfUsesLatestAvailableBalancePeriod(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("账上现在还有多少现金?比年初是多了还是少了?")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	if got := res.Data["period_to"]; got != "2026-03" {
		t.Fatalf("period_to = %v, want latest available balance period 2026-03; data=%+v", got, res.Data)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	if got := spec["period_to"]; got != "2026-03" {
		t.Fatalf("query_spec.period_to = %v, want 2026-03", got)
	}
	if got := anyToFloat64(res.Data["cash_closing_balance"]); got != 150 {
		t.Fatalf("cash_closing_balance = %v, want 150", got)
	}
}

func TestCompanyOfficialARAPQuestionCarriesBalanceSemanticFamily(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
VALUES ('测试公司','2026-03','1122','应收账款',0,100),
       ('测试公司','2026-03','2202','应付账款',0,200),
       ('测试公司','2026-03','2241','其他应付款',0,50)`); err != nil {
		t.Fatalf("seed arap balance: %v", err)
	}
	_ = db.Close()
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("当前账上应收应付分别是多少，哪头更重？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"balance_ar_ap", "balance_sheet"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %q", families, want)
		}
	}
}

func TestLargeTransactionRosterQuestionCarriesBankSemanticFamily(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("Q1 有哪几笔大额的进账和支出？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	families := anySourceStringSlice(spec["semantic_families"])
	for _, want := range []string{"large_transactions", "bank_statement"} {
		if !containsString(families, want) {
			t.Fatalf("semantic_families = %#v, want %q", families, want)
		}
	}
}

func TestLargeTransactionQuestionDoesNotSilentlyFallbackToPriorQuarter(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("Q2 有哪几笔大额的进账和支出？")
	if res.Success {
		t.Fatalf("Q2 query should not succeed by silently returning Q1 transactions, message=%s data=%+v", res.Message, res.Data)
	}
	if strings.Contains(res.Message, "2026-03") || strings.Contains(res.Message, "2026-01~2026-03") {
		t.Fatalf("message should not answer from Q1 when Q2 was requested, got: %s", res.Message)
	}
	if !strings.Contains(res.Message, "2026-04~2026-06") {
		t.Fatalf("message should disclose requested Q2 period, got: %s", res.Message)
	}
}

func TestRelativeLargeTransactionQuestionUsesLatestAvailableBankQuarter(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("这季度有哪几笔大额的进账和支出？")
	if !res.Success {
		t.Fatalf("relative current quarter should use latest available bank quarter, message=%s data=%+v", res.Message, res.Data)
	}
	if !strings.Contains(res.Message, "2026-01~2026-03") {
		t.Fatalf("message should answer from latest available bank quarter, got: %s", res.Message)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	if spec["period_from"] != "2026-01" || spec["period_to"] != "2026-03" {
		t.Fatalf("query_spec period = %v~%v, want 2026-01~2026-03", spec["period_from"], spec["period_to"])
	}
}

func TestBalanceQuestionUsesBalanceRecordsInsteadOfBankCashFlow(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2026-03','100201','银行存款',80,130)`); err != nil {
		t.Fatalf("seed bank deposit balance: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("截至2026年3月末，货币资金余额和银行存款余额分别是多少？")
	if route.spec.SourceConstraint != BossSourceBalance {
		t.Fatalf("source_constraint = %q, want balance", route.spec.SourceConstraint)
	}
	res := engine.Query("截至2026年3月末，货币资金余额和银行存款余额分别是多少？")
	if !res.Success {
		t.Fatalf("query success = false, message=%s", res.Message)
	}
	for _, want := range []string{"货币资金/银行存款期末余额 150.00 元"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("message = %q, want include %q", res.Message, want)
		}
	}
	if got := anySourceStringSlice(res.Data["source_tables"]); !containsString(got, "fin_balance_detail") {
		t.Fatalf("source_tables = %#v, want balance_detail source", got)
	}
	if strings.Contains(res.Message, "实际到账") || strings.Contains(res.Message, "实际支出") {
		t.Fatalf("balance query should not answer bank cash flow: %s", res.Message)
	}
}

func TestResolveQueryRoutingDoesNotTreatOverallExpenseAsEntity(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("2026年3月整体支出按大类拆一下")
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want no business entity", route.entity, route.hasRealEntity)
	}
	if route.spec.QueryFamily == QueryFamilyCounterparty {
		t.Fatalf("query_family = %s, want non-counterparty route", route.spec.QueryFamily)
	}

	contractViewRoute := engine.resolveQueryRouting("2026年3月整体支出按合同拆一下")
	if contractViewRoute.spec.NeedsContractDimension || contractViewRoute.spec.QueryFamily == QueryFamilyContractDimension {
		t.Fatalf("overall expense contract-view breakdown should not require a specific contract entity, spec=%+v", contractViewRoute.spec)
	}
	if contractViewRoute.spec.BossRewrite.Scope != BossScopeCompany || contractViewRoute.spec.BossRewrite.Granularity != BossGranularityBreakdown {
		t.Fatalf("boss rewrite = %+v, want company breakdown", contractViewRoute.spec.BossRewrite)
	}
}

func TestResolveQueryRoutingCoreMetricQuestionsStayEntityless(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	cases := []struct {
		name     string
		question string
		from     string
		to       string
	}{
		{name: "slash revenue cost", question: "2026年1月收入/成本多少", from: "2026-01", to: "2026-01"},
		{name: "multi metric", question: "2026年2月账上收入、成本、利润分别是多少？", from: "2026-02", to: "2026-02"},
	}

	for _, tc := range cases {
		route := engine.resolveQueryRouting(tc.question)
		if route.entity != "" || route.hasRealEntity {
			t.Fatalf("%s: entity = %q hasRealEntity=%t, want company-level metric route", tc.name, route.entity, route.hasRealEntity)
		}
		if route.spec.QueryFamily != QueryFamilyCoreMetric {
			t.Fatalf("%s: query_family = %s, want %s", tc.name, route.spec.QueryFamily, QueryFamilyCoreMetric)
		}
		if route.spec.PeriodFrom != tc.from || route.spec.PeriodTo != tc.to {
			t.Fatalf("%s: period = %s~%s, want %s~%s", tc.name, route.spec.PeriodFrom, route.spec.PeriodTo, tc.from, tc.to)
		}
	}
}

func TestResolveQueryRoutingDoesNotTreatCurrentModifierAsARAPEntity(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("当前的应收账款汇总")
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want no business entity", route.entity, route.hasRealEntity)
	}
	if route.spec.NeedsContractDimension {
		t.Fatalf("NeedsContractDimension = true, want false")
	}
}

func TestResolveQueryRoutingKeepsReadinessFamilyAndResolvedEntity(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("南京林悦智能科技有限公司3月数据出来了吗？")
	if route.entity != "南京林悦智能科技有限公司" {
		t.Fatalf("entity = %q, want %q", route.entity, "南京林悦智能科技有限公司")
	}
	if route.spec.QueryFamily != QueryFamilyReadiness {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyReadiness)
	}
	if !route.spec.ReadinessCheckRequired {
		t.Fatalf("expected readiness flag to stay true")
	}
}

func TestResolveQueryRoutingKeepsClassificationQuestionOffContractPriorityPath(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("飞未云科这个主体目前更像客户、供应商还是混合往来？")
	if route.entity != "飞未云科（深圳）技术有限公司" {
		t.Fatalf("entity = %q, want %q", route.entity, "飞未云科（深圳）技术有限公司")
	}
	if route.spec.QueryFamily == QueryFamilyContractDimension {
		t.Fatalf("query_family = %s, want non-contract classification route", route.spec.QueryFamily)
	}
}

func TestResolveQueryRoutingUsesContractAnchorForRelativeContractQuestions(t *testing.T) {
	dbPath := buildQueryContextContractAnchorDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("飞未云科本月销售额多少？")
	if route.spec.QueryFamily != QueryFamilyContractDimension {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyContractDimension)
	}
	if route.spec.PeriodFrom != "2026-03" || route.spec.PeriodTo != "2026-03" {
		t.Fatalf("period = %s~%s, want 2026-03~2026-03", route.spec.PeriodFrom, route.spec.PeriodTo)
	}
	if got := route.anchor.Format("2006-01"); got != "2026-03" {
		t.Fatalf("anchor = %s, want 2026-03", got)
	}
}

func TestCustomerReceivableQuestionUsesContractDimensionWithoutProjectKeyword(t *testing.T) {
	dbPath := buildQueryContextContractAnchorDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("飞未云科客户应收未收还有多少？")
	if route.entity != "飞未云科（深圳）技术有限公司" {
		t.Fatalf("entity = %q, want %q", route.entity, "飞未云科（深圳）技术有限公司")
	}
	if route.spec.QueryFamily != QueryFamilyContractDimension {
		t.Fatalf("query_family = %s, want %s; spec=%+v", route.spec.QueryFamily, QueryFamilyContractDimension, route.spec)
	}
	if !route.spec.NeedsContractDimension {
		t.Fatalf("NeedsContractDimension = false, want true; spec=%+v", route.spec)
	}
}

func TestResolveQueryRoutingShortQuarterRevenueStaysCompanyAggregate(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("26Q1 收入有多少")
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want company aggregate", route.entity, route.hasRealEntity)
	}
	if route.spec.QueryFamily != QueryFamilyCoreMetric {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyCoreMetric)
	}
	if route.spec.NeedsContractDimension {
		t.Fatalf("NeedsContractDimension = true, want false")
	}
	if !route.spec.PreferContractAggregate {
		t.Fatalf("PreferContractAggregate = false, want contract aggregate")
	}
	if route.spec.PeriodFrom != "2026-01" || route.spec.PeriodTo != "2026-03" {
		t.Fatalf("period = %s~%s, want 2026-01~2026-03", route.spec.PeriodFrom, route.spec.PeriodTo)
	}
}

func TestShortQuarterRevenueQueryDoesNotMatchFY26ContractContent(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('C-FY26-PDD-1','辽宁金程信息科技有限公司','FY26指定平台商品数据服务采购-pdd'),
       ('C-FY26-PDD-2','四川其妙科技有限公司','FY26指定平台商品数据服务采购-pdd')`); err != nil {
		t.Fatalf("seed fy26 contracts: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('C-FY26-PDD-1','2026-01','contract_fund_income','26年Q1收入明细',103500,103500,'是',103500),
       ('C-FY26-PDD-1','2026-02','contract_fund_income','26年Q1收入明细',103500,0,'否',0),
       ('C-FY26-PDD-2','2026-03','contract_fund_income','26年Q1收入明细',103500,0,'否',0)`); err != nil {
		t.Fatalf("seed fy26 income: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("26Q1 收入有多少")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if strings.Contains(res.Message, "FY26指定平台商品数据服务采购-pdd") {
		t.Fatalf("company aggregate should not be hijacked by FY26 contract content, got %q", res.Message)
	}
	if got := res.Data["source_priority"]; got != "contract_first" {
		t.Fatalf("source_priority = %v, want contract_first; data=%+v message=%s", got, res.Data, res.Message)
	}
	if got := res.Data["total"]; got != float64(310500+900) {
		t.Fatalf("total = %v, want company contract revenue 311400", got)
	}
	spec, ok := res.Data["query_spec"].(map[string]any)
	if !ok {
		t.Fatalf("query_spec missing: %+v", res.Data)
	}
	if got := spec["entity"]; got != "" {
		t.Fatalf("query_spec.entity = %v, want empty company aggregate", got)
	}
}

func TestCustomerConcentrationQuestionUsesCompanyContractAggregate(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.April, 22, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("Q1 的收入主要靠哪几个客户?会不会太依赖某一两家?")
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want company-scope customer dimension", route.entity, route.hasRealEntity)
	}
	if route.spec.QueryFamily != QueryFamilyCoreMetric {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyCoreMetric)
	}
	if route.spec.NeedsContractDimension {
		t.Fatalf("NeedsContractDimension = true, want false")
	}
	if !route.spec.PreferContractAggregate {
		t.Fatalf("PreferContractAggregate = false, want contract aggregate")
	}
}

func TestSupplierCostRankingQuestionUsesCompanyContractAggregate(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE fin_cost_settlements (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	contract_id TEXT,
	year_month TEXT,
	source_report_type TEXT,
	source_sheet_name TEXT,
	settlement_amount REAL,
	paid_amount REAL,
	is_invoiced TEXT,
	invoice_amount REAL
)`); err != nil {
		t.Fatalf("create cost table: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('C-SUP-001','测试供应商','测试采购项目');
INSERT INTO fin_cost_settlements(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, paid_amount, is_invoiced, invoice_amount)
VALUES ('C-SUP-001','2026-03','contract_mixed_finance','成本-月度结算',500,200,'是',500)`); err != nil {
		t.Fatalf("seed cost: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("我们的钱主要花在哪几家供应商身上?最大的几笔采购成本是什么?")
	if route.spec.QueryFamily != QueryFamilyCoreMetric {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyCoreMetric)
	}
	if route.spec.MetricKind != MetricKindCost {
		t.Fatalf("metric_kind = %s, want %s", route.spec.MetricKind, MetricKindCost)
	}
	if !route.spec.PreferContractAggregate {
		t.Fatalf("PreferContractAggregate = false, want contract aggregate")
	}
}

func TestGrossMarginQuestionUsesCompanyContractAggregate(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("按收入减成本算,大概的毛利是什么水平?")
	if route.spec.QueryFamily != QueryFamilyCoreMetric {
		t.Fatalf("query_family = %s, want %s", route.spec.QueryFamily, QueryFamilyCoreMetric)
	}
	if route.spec.NeedsContractDimension {
		t.Fatalf("NeedsContractDimension = true, want false")
	}
	if !route.spec.PreferContractAggregate {
		t.Fatalf("PreferContractAggregate = false, want contract aggregate")
	}
}

func TestProjectARAPAggregateRewriteDoesNotRequireEntity(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	questions := []string{
		"项目口径 2025年10月起至今 应付未付 未付款",
		"按项目应收口径，从2025年10月起至今未回款合计多少？",
	}
	for _, question := range questions {
		route := engine.resolveQueryRouting(question)
		if route.entity != "" || route.hasRealEntity {
			t.Fatalf("%q entity = %q hasRealEntity=%t, want company aggregate", question, route.entity, route.hasRealEntity)
		}
		if route.spec.QueryFamily != QueryFamilyCoreMetric {
			t.Fatalf("%q query_family = %s, want %s; spec=%+v", question, route.spec.QueryFamily, QueryFamilyCoreMetric, route.spec)
		}
		if route.spec.NeedsContractDimension {
			t.Fatalf("%q NeedsContractDimension = true, want false; spec=%+v", question, route.spec)
		}
		if !route.spec.PreferContractAggregate {
			t.Fatalf("%q PreferContractAggregate = false, want contract aggregate; spec=%+v", question, route.spec)
		}
	}
}

func TestLatestVisibleProjectSettlementRevenueUsesCompanyContractAggregate(t *testing.T) {
	dbPath := buildQueryContextResolutionDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
VALUES ('C-JUN-001','六月客户','六月项目');
INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
VALUES ('C-JUN-001','2026-06','contract_fund_income','26年Q2收入明细',300,100,'是',200)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed june income: %v", err)
	}
	_ = db.Close()

	engine, err := NewEngine(dbPath, "测试公司", WithAsOfAnchor(time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	route := engine.resolveQueryRouting("最新可见月份，项目结算营收是多少？")
	if route.entity != "" || route.hasRealEntity {
		t.Fatalf("entity = %q hasRealEntity=%t, want company project revenue", route.entity, route.hasRealEntity)
	}
	if route.spec.QueryFamily != QueryFamilyCoreMetric {
		t.Fatalf("query_family = %s, want %s; spec=%+v", route.spec.QueryFamily, QueryFamilyCoreMetric, route.spec)
	}
	if !route.spec.PreferContractAggregate {
		t.Fatalf("PreferContractAggregate = false, want contract aggregate; spec=%+v", route.spec)
	}

	res := engine.Query("最新可见月份，项目结算营收是多少？")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-06" {
		t.Fatalf("period = %v, want 2026-06; message=%s data=%+v", got, res.Message, res.Data)
	}
	if got := res.Data["total"]; got != float64(300) {
		t.Fatalf("total = %v, want project settlement revenue 300; data=%+v", got, res.Data)
	}
	if strings.Contains(res.Message, "账务数据仅到") || strings.Contains(res.Message, "银行卡") {
		t.Fatalf("project settlement revenue should not answer account/cash reconciliation, got: %s", res.Message)
	}

	res = engine.Query("收入表中最新月份项目结算营收是多少？")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-06" {
		t.Fatalf("period = %v, want 2026-06 for explicit revenue-table project settlement; message=%s data=%+v", got, res.Message, res.Data)
	}
	if got := res.Data["total"]; got != float64(300) {
		t.Fatalf("total = %v, want project settlement revenue 300; data=%+v", got, res.Data)
	}
	if strings.Contains(res.Message, "账务数据仅到") || strings.Contains(res.Message, "银行卡") || strings.Contains(res.Message, "营业收入") {
		t.Fatalf("explicit revenue-table project settlement should not answer account/cash reconciliation, got: %s", res.Message)
	}

	res = engine.Query("收入表中最新完整月份的营收是多少？")
	if !res.Success {
		t.Fatalf("query failed: %s data=%+v", res.Message, res.Data)
	}
	if got := res.Data["period"]; got != "2026-06" {
		t.Fatalf("period = %v, want 2026-06 for latest complete month revenue-table revenue; message=%s data=%+v", got, res.Message, res.Data)
	}
	if got := res.Data["total"]; got != float64(300) {
		t.Fatalf("total = %v, want latest single-month project settlement revenue 300; data=%+v", got, res.Data)
	}
	if strings.Contains(res.Message, "2026-04~2026-06") || strings.Contains(res.Message, "Q2") || strings.Contains(res.Message, "银行卡") || strings.Contains(res.Message, "营业收入") {
		t.Fatalf("latest complete month revenue-table query should not answer quarter/account/cash view, got: %s", res.Message)
	}

	for _, question := range []string{
		"按最新可见月份，查看当月营收（项目结算收入）",
		"最新一个完整月份的项目营收是多少",
		"最新月份项目结算收入是多少？",
	} {
		route := engine.resolveQueryRouting(question)
		if route.entity != "" || route.hasRealEntity {
			t.Fatalf("%q entity = %q hasRealEntity=%t, want company project revenue", question, route.entity, route.hasRealEntity)
		}
		if !route.spec.PreferContractAggregate {
			t.Fatalf("%q PreferContractAggregate = false, want contract aggregate; spec=%+v", question, route.spec)
		}
		res := engine.Query(question)
		if !res.Success {
			t.Fatalf("%q query failed: %s data=%+v", question, res.Message, res.Data)
		}
		if got := res.Data["period"]; got != "2026-06" {
			t.Fatalf("%q period = %v, want 2026-06; message=%s data=%+v", question, got, res.Message, res.Data)
		}
		if got := res.Data["total"]; got != float64(300) {
			t.Fatalf("%q total = %v, want project settlement revenue 300; data=%+v", question, got, res.Data)
		}
		if strings.Contains(res.Message, "账务数据仅到") || strings.Contains(res.Message, "银行卡") || strings.Contains(res.Message, "营业收入") {
			t.Fatalf("%q should not answer account/cash reconciliation, got: %s", question, res.Message)
		}
	}
}

func buildQueryContextResolutionDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-context-resolution.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
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
		`INSERT INTO balance_sheet(company, period, account_code, account_name, opening_balance, closing_balance)
		 VALUES ('测试公司','2026-03','1002','货币资金',100,150)`,
		`INSERT INTO balance_detail(company, period, account_code, account_name, opening_debit, opening_credit, closing_debit, closing_credit)
		 VALUES ('测试公司','2026-03','1002','银行存款',100,0,150,0)`,
		`INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
		 VALUES ('C-FW-001','飞未云科（深圳）技术有限公司','飞未项目-京东价格数据')`,
		`INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
		 VALUES ('C-FW-001','2026-03','contract_fund_income','26年Q1收入明细',900,900,'是',900)`,
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty)
		 VALUES ('测试公司','2026-03','2026-03-31','V-READY-1','6401','主营业务成本','林悦3月成本确认','借',500,500,0,'南京林悦智能科技有限公司')`,
		`INSERT INTO bank_statement(company, transaction_date, counterparty_name, summary, debit_amount, credit_amount)
		 VALUES ('测试公司','2026-03-20','南京林悦智能科技有限公司','合同款',500,0)`,
		`INSERT INTO bank_statement(company, transaction_date, counterparty_name, summary, debit_amount, credit_amount)
		 VALUES ('测试公司','2026-03-21','招商银行','收款',0,1200)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}

	return dbPath
}

func buildQueryContextContractAnchorDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-context-contract-anchor.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
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
		`INSERT INTO journal(company, period, voucher_date, voucher_no, account_code, account_name, summary, direction, amount, debit_amount, credit_amount, counterparty)
		 VALUES ('测试公司','2026-04','2026-04-30','J-NEW-1','6001','主营业务收入','4月账务更新','贷',100,0,100,'其他客户')`,
		`INSERT INTO fin_contracts(contract_id, customer_name, contract_content)
		 VALUES ('C-FW-ANCHOR-1','飞未云科（深圳）技术有限公司','飞未项目-京东价格数据')`,
		`INSERT INTO fin_fund_income(contract_id, year_month, source_report_type, source_sheet_name, settlement_amount, received_amount, is_invoiced, invoice_amount)
		 VALUES ('C-FW-ANCHOR-1','2026-03','contract_fund_income','26年Q1收入明细',900,900,'是',900)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec stmt failed: %v", err)
		}
	}

	return dbPath
}
