package query_test

import (
	"database/sql"
	"strings"
	"testing"

	"financeqa/internal/query"

	_ "modernc.org/sqlite"
)

func TestCompanyAggregateMetricPrimarySourceTablesPreferMetricTableOverContractDimension(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		_ = db.Close()
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()
	if _, err := db.Exec(`
UPDATE fin_fund_income SET updated_at = '2026-05-05 08:30:00';
UPDATE fin_fund_income_groups SET updated_at = '2026-05-05 09:00:00';
UPDATE fin_contracts SET updated_at = '2026-05-04 18:00:00';
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed source updated_at: %v", err)
	}
	_ = db.Close()

	res := engine.Query("2025年10月收入是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	primary, ok := res.Data["primary_source_tables"].([]string)
	if !ok {
		t.Fatalf("primary_source_tables missing: %#v", res.Data["primary_source_tables"])
	}
	if len(primary) != 1 || primary[0] != "tenant_uhub.fin_fund_income" {
		t.Fatalf("primary_source_tables = %#v, want only tenant_uhub.fin_fund_income", primary)
	}
	sourceTables, ok := res.Data["source_tables"].([]string)
	if !ok {
		t.Fatalf("source_tables missing: %#v", res.Data["source_tables"])
	}
	for _, tableName := range sourceTables {
		if tableName == "tenant_uhub.fin_cost_settlements" {
			t.Fatalf("source_tables should not include tenant_uhub.fin_cost_settlements for revenue-only aggregate, got %#v", sourceTables)
		}
	}
	if !containsString(sourceTables, "tenant_uhub.fin_fund_income_groups") || !containsString(sourceTables, "tenant_uhub.fin_fund_income_group_members") {
		t.Fatalf("source_tables should include merged fund income group tables, got %#v", sourceTables)
	}
	if containsString(sourceTables, "tenant_uhub.fin_cost_settlement_groups") || containsString(sourceTables, "tenant_uhub.fin_cost_settlement_group_members") {
		t.Fatalf("source_tables should not include cost group tables for revenue-only aggregate, got %#v", sourceTables)
	}

	supporting, ok := res.Data["supporting_source_documents"].([]string)
	if !ok || len(supporting) == 0 {
		t.Fatalf("supporting_source_documents missing: %#v", res.Data["supporting_source_documents"])
	}
	for _, doc := range supporting {
		if doc == "《优集成本计算表-4.23-池.xlsx》" {
			t.Fatalf("supporting_source_documents should not include cost workbook for revenue-only aggregate, got %#v", supporting)
		}
	}

	partitions := extractSourcePartitionsForTest(t, res.Data["source_partitions"])
	if len(partitions) != 1 {
		t.Fatalf("source_partitions = %#v, want exactly one partition", res.Data["source_partitions"])
	}
	if got := partitions[0]["table"]; got != "tenant_uhub.fin_fund_income" {
		t.Fatalf("source partition table = %v, want tenant_uhub.fin_fund_income", got)
	}
	if got := partitions[0]["source_report_type"]; got != "contract_fund_income" {
		t.Fatalf("source partition report type = %v, want contract_fund_income", got)
	}
	if got := partitions[0]["source_sheet_name"]; got != "25年Q4收入明细" {
		t.Fatalf("source partition sheet = %v, want 25年Q4收入明细", got)
	}

	primaryPartitions := extractSourcePartitionsForTest(t, res.Data["primary_source_partitions"])
	if len(primaryPartitions) != 1 {
		t.Fatalf("primary_source_partitions = %#v, want one primary partition", res.Data["primary_source_partitions"])
	}
	if res.Data["supporting_source_partitions"] != nil {
		t.Fatalf("supporting_source_partitions should be nil for revenue-only aggregate, got %#v", res.Data["supporting_source_partitions"])
	}

	sourceSummary, _ := res.Data["source_summary"].(string)
	if sourceSummary == "" {
		t.Fatalf("source_summary missing: %#v", res.Data["source_summary"])
	}
	sourceNote, _ := res.Data["source_note"].(string)
	if sourceSummary != sourceNote {
		t.Fatalf("source_summary = %q, want match source_note %q", sourceSummary, sourceNote)
	}
	if sourceSummary == "" || !containsAll(sourceSummary, "优集资金收入计算表-副本.xlsx", "25年Q4收入明细") {
		t.Fatalf("source_summary should expose concrete workbook lineage, got %q", sourceSummary)
	}
	sourceUpdateNote, _ := res.Data["source_update_note"].(string)
	if sourceUpdateNote == "" || !strings.Contains(sourceUpdateNote, "来源更新时间：") || !strings.Contains(sourceUpdateNote, "2026-05-05 09:00:00") {
		t.Fatalf("source_update_note should expose latest source update time, got %q", sourceUpdateNote)
	}
	if !strings.Contains(res.Message, sourceUpdateNote) {
		t.Fatalf("message should append source update note %q, got %q", sourceUpdateNote, res.Message)
	}
}

func TestCompanyAggregateMultiMetricSourceNoteIncludesRevenueAndCostWorkbooks(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2025年10月收入、成本、利润分别是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	primary, ok := res.Data["primary_source_tables"].([]string)
	if !ok {
		t.Fatalf("primary_source_tables missing: %#v", res.Data["primary_source_tables"])
	}
	if !containsString(primary, "tenant_uhub.fin_fund_income") || !containsString(primary, "tenant_uhub.fin_cost_settlements") {
		t.Fatalf("primary_source_tables should include revenue and cost tables, got %#v", primary)
	}
	sourceTables, ok := res.Data["source_tables"].([]string)
	if !ok {
		t.Fatalf("source_tables missing: %#v", res.Data["source_tables"])
	}
	for _, tableName := range []string{
		"tenant_uhub.fin_fund_income_groups",
		"tenant_uhub.fin_fund_income_group_members",
		"tenant_uhub.fin_cost_settlement_groups",
		"tenant_uhub.fin_cost_settlement_group_members",
	} {
		if !containsString(sourceTables, tableName) {
			t.Fatalf("source_tables should include %s, got %#v", tableName, sourceTables)
		}
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if !containsAll(sourceNote, "优集资金收入计算表-副本.xlsx", "优集成本计算表-4.23-池.xlsx") {
		t.Fatalf("source_note should include both revenue and cost workbook lineage, got %q", sourceNote)
	}
	if !strings.Contains(sourceNote, "补充参考：《合同信息表》") {
		t.Fatalf("source_note should keep contract table as supporting source, got %q", sourceNote)
	}
}

func TestSourceNoteDoesNotExposeFeishuTokenOnlyWorkbookNames(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT OR REPLACE INTO meta_table_comments(table_name, comment) VALUES
('fin_fund_income', 'financeqa_source: {"version":"v1","display":"《优集资金收入计算表-2026.xlsx》；《Iel5bFZWSoGF7hxjyPpcn5Elnqd.xlsx》","logical_label":"fin_fund_income","file_names":["优集资金收入计算表-2026.xlsx","Iel5bFZWSoGF7hxjyPpcn5Elnqd.xlsx"],"sheet_names":["26年Q1收入明细"],"report_types":["contract_fund_income"],"updated_at":"2026-05-05T11:38:30Z"}'),
('fin_cost_settlements', 'financeqa_source: {"version":"v1","display":"《优集成本计算表-4.23-池.xlsx》；《Iel5bFZWSoGF7hxjyPpcn5Elnqd.xlsx》","logical_label":"fin_cost_settlements","file_names":["优集成本计算表-4.23-池.xlsx","Iel5bFZWSoGF7hxjyPpcn5Elnqd.xlsx"],"sheet_names":["成本-月度结算"],"report_types":["contract_revenue_cost"],"updated_at":"2026-05-05T11:38:30Z"}')
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed dirty source comments: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2025年10月收入、成本、利润分别是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if strings.Contains(sourceNote, "Iel5bFZWSoGF7hxjyPpcn5Elnqd") || strings.Contains(res.Message, "Iel5bFZWSoGF7hxjyPpcn5Elnqd") {
		t.Fatalf("boss-facing source should hide Feishu token-only workbook, source_note=%q message=%q", sourceNote, res.Message)
	}
	if !containsAll(sourceNote, "优集资金收入计算表-2026.xlsx", "优集成本计算表-4.23-池.xlsx") {
		t.Fatalf("source_note should keep real workbook names, got %q", sourceNote)
	}
}

func TestSourceNotePrefersCurrentFinanceFileMappingName(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS fin_file_mappings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_type TEXT,
	period TEXT,
	company TEXT,
	storage_key TEXT,
	file_name TEXT,
	description TEXT,
	file_size INTEGER,
	created_at TEXT,
	updated_at TEXT
);
INSERT OR REPLACE INTO meta_table_comments(table_name, comment) VALUES
('fin_fund_income', 'financeqa_source: {"version":"v1","display":"《历史收入表.xlsx》","logical_label":"fin_fund_income","file_names":["历史收入表.xlsx"],"sheet_names":["26年Q1收入明细"],"report_types":["contract_mixed_finance"],"updated_at":"2026-05-05T11:38:30Z"}'),
('fin_cost_settlements', 'financeqa_source: {"version":"v1","display":"《历史成本表.xlsx》","logical_label":"fin_cost_settlements","file_names":["历史成本表.xlsx"],"sheet_names":["成本-月度结算"],"report_types":["contract_mixed_finance"],"updated_at":"2026-05-05T11:38:30Z"}');
INSERT INTO fin_file_mappings(table_type, period, company, storage_key, file_name, updated_at)
VALUES
('fund-income', '2025-Q4', '南京优集数据科技有限公司', 'tenant/uhub/finance/2025/优集收入、成本计算表 - 上传.xlsx', '优集收入、成本计算表 - 上传.xlsx', '2026-05-06 09:30:00'),
('cost-settlements', '2025-Q4', '南京优集数据科技有限公司', 'tenant/uhub/finance/2025/优集收入、成本计算表 - 上传.xlsx', '优集收入、成本计算表 - 上传.xlsx', '2026-05-06 09:30:00');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed source comments: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2025年10月收入、成本、利润分别是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if !strings.Contains(sourceNote, "优集收入、成本计算表 - 上传.xlsx") {
		t.Fatalf("source_note should use current finance file mapping from DB, got %q", sourceNote)
	}
	if strings.Contains(sourceNote, "历史收入表.xlsx") || strings.Contains(sourceNote, "历史成本表.xlsx") {
		t.Fatalf("source_note should not expose stale table-comment workbook names when current finance file mapping exists, got %q", sourceNote)
	}
	if strings.Contains(sourceNote, "合同信息表") {
		t.Fatalf("source_note should not fall back to hardcoded fin_contracts label when finance mappings exist, got %q", sourceNote)
	}
	probeSourceDocuments := collectRouteProbeSourceDocumentsForTest(t, res.Data["route_decision"])
	if !containsString(probeSourceDocuments, "《优集收入、成本计算表 - 上传.xlsx》的【25年Q4收入明细】") {
		t.Fatalf("route probe source_documents should use current finance file mapping from DB, got %#v", probeSourceDocuments)
	}
	for _, stale := range []string{"历史收入表.xlsx", "历史成本表.xlsx", "合同信息表"} {
		if strings.Contains(strings.Join(probeSourceDocuments, "\n"), stale) {
			t.Fatalf("route probe source_documents should not fall back to stale table comments or hardcoded labels, got %#v", probeSourceDocuments)
		}
	}
	sourceUpdateNote, _ := res.Data["source_update_note"].(string)
	if !strings.Contains(sourceUpdateNote, "2026-05-06 09:30:00") {
		t.Fatalf("source_update_note should use fin_file_mappings.updated_at, got %q", sourceUpdateNote)
	}
}

func TestSourceNoteDoesNotFallBackToTableCommentWhenFinanceFileMappingIsPartial(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS fin_file_mappings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_type TEXT,
	period TEXT,
	company TEXT,
	storage_key TEXT,
	file_name TEXT,
	description TEXT,
	file_size INTEGER,
	created_at TEXT,
	updated_at TEXT
);
INSERT INTO fin_file_mappings(table_type, period, company, storage_key, file_name, updated_at)
VALUES ('fund-income', '2025-Q4', '南京优集数据科技有限公司', 'tenant/uhub/finance/2025/优集收入、成本计算表 - 上传.xlsx', '优集收入、成本计算表 - 上传.xlsx', '2026-05-06 09:30:00');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed partial finance file mapping: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2025年10月收入、成本、利润分别是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if !strings.Contains(sourceNote, "优集收入、成本计算表 - 上传.xlsx") {
		t.Fatalf("source_note should use mapped revenue file, got %q", sourceNote)
	}
	if strings.Contains(sourceNote, "优集成本计算表-4.23-池.xlsx") || strings.Contains(sourceNote, "合同成本结算表") {
		t.Fatalf("source_note should not fall back to cost source metadata when cost mapping is missing, got %q", sourceNote)
	}
}

func TestSourceNoteDoesNotFallBackToDifferentPeriodFinanceFileMapping(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS fin_file_mappings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_type TEXT,
	period TEXT,
	company TEXT,
	storage_key TEXT,
	file_name TEXT,
	description TEXT,
	file_size INTEGER,
	created_at TEXT,
	updated_at TEXT
);
INSERT INTO fin_file_mappings(table_type, period, company, storage_key, file_name, updated_at)
VALUES ('fund-income', '2026-Q1', '南京优集数据科技有限公司', 'tenant/uhub/finance/2026/优集收入、成本计算表 - 上传.xlsx', '优集收入、成本计算表 - 上传.xlsx', '2026-05-06 09:30:00');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed different-period finance file mapping: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2025年10月收入是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if strings.Contains(sourceNote, "优集收入、成本计算表 - 上传.xlsx") {
		t.Fatalf("source_note should not use a finance file mapping from a different period, got %q", sourceNote)
	}
	if strings.Contains(sourceNote, "优集资金收入计算表-副本.xlsx") || strings.Contains(sourceNote, "合同信息表") {
		t.Fatalf("source_note should not fall back to table comments when current-period mapping is missing, got %q", sourceNote)
	}
	if sourceNote != "来源：未记录" {
		t.Fatalf("source_note should still expose an explicit unknown-source marker, got %q", sourceNote)
	}
	sourceUpdateNote, _ := res.Data["source_update_note"].(string)
	if sourceUpdateNote != "来源更新时间：未记录" {
		t.Fatalf("source_update_note should still expose an explicit unknown-update marker, got %q", sourceUpdateNote)
	}
	if !strings.Contains(res.Message, sourceNote) || !strings.Contains(res.Message, sourceUpdateNote) {
		t.Fatalf("message should include explicit source and update markers, source=%q update=%q message=%q", sourceNote, sourceUpdateNote, res.Message)
	}
}

func TestSourceNoteUsesFinanceFileMappingForBankStatement(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildEntityRoutingTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS fin_file_mappings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_type TEXT,
	period TEXT,
	company TEXT,
	storage_key TEXT,
	file_name TEXT,
	description TEXT,
	file_size INTEGER,
	created_at TEXT,
	updated_at TEXT
);
INSERT INTO fin_file_mappings(table_type, period, company, storage_key, file_name, updated_at)
VALUES ('bank-statement', '2026-Q1', '南京优集数据科技有限公司', 'tenant/uhub/finance/2026/交易查询-2026Q1.xlsx', '交易查询-2026Q1.xlsx', '2026-05-06 10:15:00');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed finance file mapping: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("2026年2月最大的单笔流入对手方是谁，金额多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceSummary, _ := res.Data["source_summary"].(string)
	if !strings.Contains(sourceSummary, "交易查询-2026Q1.xlsx") {
		t.Fatalf("source_summary should use fin_file_mappings file_name, got %q", sourceSummary)
	}
	if strings.Contains(sourceSummary, "《银行流水》") {
		t.Fatalf("source_summary should not fall back to generic bank statement label when mapping exists, got %q", sourceSummary)
	}
	sourceUpdateNote, _ := res.Data["source_update_note"].(string)
	if !strings.Contains(sourceUpdateNote, "2026-05-06 10:15:00") {
		t.Fatalf("source_update_note should use fin_file_mappings.updated_at, got %q", sourceUpdateNote)
	}
}

func TestSourceNoteDoesNotExposeMergedGroupHelperTablesToBossReply(t *testing.T) {
	runParallelHeavyQueryTest(t)

	dbPath := buildContractQueryTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
INSERT OR REPLACE INTO meta_table_comments(table_name, comment) VALUES
('fin_cost_settlement_groups', '合同成本结算合并金额组，记录 Excel 合并单元格代表的供应商级成本事实，不拆分到单个合同。'),
('fin_cost_settlement_group_members', '合同成本结算合并金额组成员表，关联合并金额组与其覆盖的真实合同。')
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed helper table comments: %v", err)
	}
	_ = db.Close()

	engine, err := query.NewEngine(dbPath, testCompany)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close()

	res := engine.Query("南京林悦智能科技有限公司的应收/应付是多少？")
	if !res.Success {
		t.Fatalf("query failed: %+v", res)
	}

	sourceTables, ok := res.Data["source_tables"].([]string)
	if !ok {
		t.Fatalf("source_tables missing: %#v", res.Data["source_tables"])
	}
	if len(sourceTables) == 0 {
		t.Fatalf("source_tables should not be empty")
	}

	sourceNote, _ := res.Data["source_note"].(string)
	if strings.Contains(sourceNote, "合并金额组") || strings.Contains(res.Message, "合并金额组") {
		t.Fatalf("boss-facing source should not expose helper table descriptions, source_note=%q message=%q", sourceNote, res.Message)
	}
	if !strings.Contains(sourceNote, "优集成本计算表") {
		t.Fatalf("source_note should still show business workbook source, got %q", sourceNote)
	}
}

func extractSourcePartitionsForTest(t *testing.T, v any) []map[string]any {
	t.Helper()

	if rows, ok := v.([]map[string]any); ok {
		return rows
	}
	rawRows, ok := v.([]any)
	if !ok {
		t.Fatalf("source partitions missing or wrong type: %#v", v)
	}
	out := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("source partition row has wrong type: %#v", raw)
		}
		out = append(out, row)
	}
	return out
}

func collectRouteProbeSourceDocumentsForTest(t *testing.T, v any) []string {
	t.Helper()

	route, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("route_decision missing or wrong type: %#v", v)
	}
	rawProbes, ok := route["probe_results"].([]map[string]any)
	if !ok {
		t.Fatalf("route_decision.probe_results missing or wrong type: %#v", route["probe_results"])
	}
	out := []string{}
	for _, probe := range rawProbes {
		docs, ok := probe["source_documents"].([]string)
		if !ok {
			t.Fatalf("probe source_documents missing or wrong type: %#v", probe["source_documents"])
		}
		out = append(out, docs...)
	}
	return dedupeStringsForTest(out)
}

func dedupeStringsForTest(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
