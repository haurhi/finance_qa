package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	dbpkg "financeqa/internal/db"
)

func (e *Engine) annotateSourceAttribution(spec QuerySpec, result Result) Result {
	if !result.Success {
		return result
	}
	if result.Data == nil {
		result.Data = map[string]any{}
	}
	if spec.QueryFamily == QueryFamilyContractDetail {
		return e.annotateContractDetailSourceAttribution(result, nil)
	}

	plan := resolveSourceAttributionPlan(spec, result.Data)
	collected := plan.tables
	if len(collected) == 0 {
		return result
	}

	metadata, err := dbpkg.LoadTableSourceMetadata(context.Background(), e.db, e.dbPath, collected)
	if err != nil {
		metadata = make(map[string]dbpkg.TableSourceMetadata, len(collected))
		for _, tableName := range collected {
			metadata[tableName] = dbpkg.DefaultTableSourceMetadata(tableName)
		}
	}

	primaryTables, supportingTables := choosePrimaryAndSupportingTables(spec, result.Data, collected)
	partitions := e.collectSourcePartitions(spec, result.Data, collected)
	primaryPartitions := filterSourcePartitionsByTable(partitions, primaryTables)
	supportingPartitions := filterSourcePartitionsByTable(partitions, supportingTables)
	primaryDocs := e.sourceDisplaysForTables(spec, primaryTables, metadata, primaryPartitions)
	supportingDocs := e.sourceDisplaysForTables(spec, supportingTables, metadata, supportingPartitions)
	sourceNote := buildSourceNote(primaryDocs, supportingDocs)
	sourceUpdateNote := e.buildSourceUpdateNote(spec, collected, metadata)
	if sourceNote == "" {
		sourceNote = "来源：未记录"
	}
	if sourceUpdateNote == "" {
		sourceUpdateNote = "来源更新时间：未记录"
	}

	result.Data["source_tables"] = collected
	result.Data["primary_source_tables"] = primaryTables
	result.Data["source_documents"] = primaryDocs
	if sourceVersionIDs := e.sourceVersionIDsForTables(spec, collected); len(sourceVersionIDs) > 0 {
		result.Data["source_version_ids"] = sourceVersionIDs
	}
	if len(supportingDocs) > 0 {
		result.Data["supporting_source_documents"] = supportingDocs
	}
	if len(partitions) > 0 {
		if len(primaryPartitions) > 0 {
			result.Data["source_partitions"] = primaryPartitions
			result.Data["primary_source_partitions"] = primaryPartitions
		} else {
			result.Data["source_partitions"] = partitions
		}
		if len(supportingPartitions) > 0 {
			result.Data["supporting_source_partitions"] = supportingPartitions
		}
	}
	result.Data["source_note"] = sourceNote
	result.Data["source_summary"] = sourceNote
	result.Data["source_update_note"] = sourceUpdateNote

	if !strings.Contains(result.Message, "来源：") {
		result.Message = strings.TrimSpace(result.Message) + "\n" + sourceNote
	}
	if !strings.Contains(result.Message, "来源更新时间：") {
		result.Message = strings.TrimSpace(result.Message) + "\n" + sourceUpdateNote
	}
	return result
}

func (e *Engine) annotateContractDetailSourceAttribution(result Result, fallbackTables []string) Result {
	if result.Data == nil {
		result.Data = map[string]any{}
	}
	sourceNote := strings.TrimSpace(anyToString(result.Data["source_note"]))
	if sourceNote == "" {
		sourceNote = contractDetailSourceNote(fallbackTables)
	}
	sourceUpdateNote := strings.TrimSpace(anyToString(result.Data["source_update_note"]))
	if sourceNote != "" {
		result.Data["source_note"] = sourceNote
		result.Data["source_summary"] = sourceNote
		if !strings.Contains(result.Message, "来源：") {
			result.Message = strings.TrimSpace(result.Message) + "\n" + sourceNote
		}
	}
	if sourceNote != "" && sourceUpdateNote == "" {
		sourceUpdateNote = "来源更新时间：未记录"
	}
	if sourceUpdateNote != "" {
		result.Data["source_update_note"] = sourceUpdateNote
		if !strings.Contains(result.Message, "来源更新时间：") {
			result.Message = strings.TrimSpace(result.Message) + "\n" + sourceUpdateNote
		}
	}
	if len(fallbackTables) > 0 {
		result.Data["source_tables"] = fallbackTables
	}
	return result
}

func buildSourceNote(primaryDocs, supportingDocs []string) string {
	if len(primaryDocs) == 0 && len(supportingDocs) == 0 {
		return ""
	}
	if len(primaryDocs) == 0 {
		return "来源：" + strings.Join(supportingDocs, "；")
	}
	note := "来源：" + strings.Join(primaryDocs, "；")
	if len(supportingDocs) > 0 {
		note += "；补充参考：" + strings.Join(supportingDocs, "；")
	}
	return note
}

func sourceDisplaysForTables(tables []string, metadata map[string]dbpkg.TableSourceMetadata) []string {
	out := make([]string, 0, len(tables))
	for _, tableName := range tables {
		if shouldHideSourceTableFromBossNote(tableName) {
			continue
		}
		meta, ok := metadata[tableName]
		if !ok {
			meta = dbpkg.DefaultTableSourceMetadata(tableName)
		}
		display := strings.TrimSpace(meta.Display)
		if display == "" {
			continue
		}
		out = append(out, display)
	}
	return dedupeSourceTables(out...)
}

func (e *Engine) sourceDisplaysForTables(spec QuerySpec, tables []string, metadata map[string]dbpkg.TableSourceMetadata, partitions []map[string]any) []string {
	if e.hasFinanceFileMappingsTable() && sourceTablesRequireFinanceFileMappings(tables) {
		return e.financeFileMappingSourceDisplays(spec, tables, partitions)
	}
	return sourceDisplaysForTables(tables, metadata)
}

func (e *Engine) sourceCatalogForTables(spec QuerySpec, tables []string, metadata map[string]dbpkg.TableSourceMetadata, partitions []map[string]any) map[string]dbpkg.TableSourceMetadata {
	if !e.hasFinanceFileMappingsTable() {
		return metadata
	}
	out := make(map[string]dbpkg.TableSourceMetadata, len(tables))
	for _, tableName := range tables {
		base := baseSourceTableName(tableName)
		if base == "" || shouldHideSourceTableFromBossNote(base) {
			continue
		}
		if !sourceTableRequiresFinanceFileMapping(base) {
			if meta, ok := metadata[tableName]; ok {
				out[tableName] = meta
			} else if meta, ok := metadata[base]; ok {
				out[tableName] = meta
			} else {
				out[tableName] = dbpkg.DefaultTableSourceMetadata(base)
			}
			continue
		}
		displays := e.financeFileMappingSourceDisplays(spec, []string{base}, filterSourcePartitionsByTable(partitions, []string{base}))
		if len(displays) == 0 {
			continue
		}
		meta := dbpkg.TableSourceMetadata{
			Version:      "v1",
			Display:      strings.Join(displays, "；"),
			LogicalLabel: base,
			ReportTypes:  financeFileMappingTypesForSourceTable(base),
			UpdatedAt:    e.latestFinanceFileMappingUpdatedAt(spec, []string{base}),
		}
		for _, mapping := range e.financeFileMappingsForTables(spec, []string{base}) {
			if strings.TrimSpace(mapping.FileName) != "" {
				meta.FileNames = append(meta.FileNames, mapping.FileName)
			}
		}
		for _, partition := range filterSourcePartitionsByTable(partitions, []string{base}) {
			sheetName := strings.TrimSpace(anyToString(partition["source_sheet_name"]))
			if sheetName != "" {
				meta.SheetNames = append(meta.SheetNames, sheetName)
			}
		}
		meta.FileNames = dedupeStrings(meta.FileNames)
		meta.SheetNames = dedupeStrings(meta.SheetNames)
		out[tableName] = meta
	}
	return out
}

func (e *Engine) financeFileMappingSourceDisplays(spec QuerySpec, tables []string, partitions []map[string]any) []string {
	mappings := e.financeFileMappingsForTables(spec, tables)
	if len(mappings) == 0 {
		return nil
	}
	sheetsByTable := map[string][]string{}
	for _, partition := range partitions {
		tableName := strings.TrimSpace(baseSourceTableName(anyToString(partition["table"])))
		if tableName == "" {
			continue
		}
		sheetName := strings.TrimSpace(anyToString(partition["source_sheet_name"]))
		if sheetName == "" {
			continue
		}
		sheetsByTable[tableName] = append(sheetsByTable[tableName], sheetName)
	}
	out := make([]string, 0, len(tables))
	for _, tableName := range tables {
		base := baseSourceTableName(tableName)
		if base == "" {
			continue
		}
		typeSet := stringSet(financeFileMappingTypesForSourceTable(base))
		if len(typeSet) == 0 {
			continue
		}
		sheets := dedupeStrings(sheetsByTable[base])
		tableDocs := make([]string, 0, 2)
		for _, mapping := range mappings {
			if _, ok := typeSet[strings.ToLower(mapping.TableType)]; !ok {
				continue
			}
			if mapping.FileName == "" {
				continue
			}
			tableDocs = append(tableDocs, formatWorkbookSheetDisplay(mapping.FileName, sheets))
		}
		if len(tableDocs) > 0 {
			out = append(out, tableDocs...)
			continue
		}
	}
	return dedupeSourceTables(out...)
}

func (e *Engine) hasFinanceFileMappingsTable() bool {
	cols := e.tableColumns("fin_file_mappings")
	return len(cols) > 0 && cols["table_type"]
}

func sourceTablesRequireFinanceFileMappings(tables []string) bool {
	for _, tableName := range tables {
		if sourceTableRequiresFinanceFileMapping(tableName) {
			return true
		}
	}
	return false
}

func sourceTableRequiresFinanceFileMapping(tableName string) bool {
	if len(financeFileMappingTypesForSourceTable(tableName)) > 0 {
		return true
	}
	base := strings.TrimSpace(baseSourceTableName(tableName))
	return strings.TrimPrefix(base, "fin_") == "contracts"
}

type financeFileMappingSource struct {
	TableType       string
	Period          string
	FileName        string
	SourceFileHash  string
	SourceVersionID string
	UpdatedAt       string
}

func (e *Engine) financeFileMappingsForTables(spec QuerySpec, tables []string) []financeFileMappingSource {
	if e == nil || e.db == nil || len(tables) == 0 {
		return nil
	}
	cols := e.tableColumns("fin_file_mappings")
	if len(cols) == 0 || !cols["table_type"] {
		return nil
	}
	typeSet := map[string]struct{}{}
	for _, tableName := range tables {
		for _, tableType := range financeFileMappingTypesForSourceTable(tableName) {
			typeSet[strings.ToLower(tableType)] = struct{}{}
		}
	}
	if len(typeSet) == 0 {
		return nil
	}
	tableTypes := setKeys(typeSet)
	periodAliases := financeSourcePeriodAliases(spec)
	return e.queryFinanceFileMappings(cols, tableTypes, periodAliases, false)
}

func (e *Engine) queryFinanceFileMappings(cols map[string]bool, tableTypes, periodAliases []string, requirePeriodMatch bool) []financeFileMappingSource {
	if len(tableTypes) == 0 {
		return nil
	}
	selects := []string{
		contractDetailTextSelectExpr(cols, "table_type"),
		contractDetailTextSelectExpr(cols, "period"),
		contractDetailTextSelectExpr(cols, "file_name"),
		contractDetailTextSelectExpr(cols, "storage_key"),
		contractDetailTextSelectExpr(cols, "source_file_hash"),
		contractDetailTextSelectExpr(cols, "source_version_id"),
		contractDetailTextSelectExpr(cols, "updated_at"),
	}
	typePlaceholders := placeholders(len(tableTypes))
	args := make([]any, 0, len(tableTypes)+len(periodAliases)+2)
	for _, tableType := range tableTypes {
		args = append(args, strings.ToLower(strings.TrimSpace(tableType)))
	}
	where := []string{"LOWER(COALESCE(CAST(table_type AS TEXT), '')) IN (" + typePlaceholders + ")"}
	if len(periodAliases) > 0 && cols["period"] {
		periodPlaceholders := placeholders(len(periodAliases))
		periodArgs := make([]any, 0, len(periodAliases))
		for _, period := range periodAliases {
			periodArgs = append(periodArgs, strings.TrimSpace(period))
		}
		if requirePeriodMatch {
			where = append(where, "COALESCE(CAST(period AS TEXT), '') IN ("+periodPlaceholders+")")
		} else {
			where = append(where, "(COALESCE(CAST(period AS TEXT), '') = '' OR COALESCE(CAST(period AS TEXT), '') IN ("+periodPlaceholders+"))")
		}
		args = append(args, periodArgs...)
	}
	if cols["company"] && strings.TrimSpace(e.Company) != "" {
		where = append(where, "(COALESCE(CAST(company AS TEXT), '') = '' OR ? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')")
		args = append(args, e.Company, e.Company)
	}
	sqlText := fmt.Sprintf(
		"SELECT %s FROM fin_file_mappings WHERE %s ORDER BY COALESCE(CAST(updated_at AS TEXT), '') DESC, COALESCE(CAST(file_name AS TEXT), '')",
		strings.Join(selects, ", "),
		strings.Join(where, " AND "),
	)
	rows, err := e.db.Query(sqlText, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]financeFileMappingSource, 0, len(tableTypes))
	seen := map[string]struct{}{}
	for rows.Next() {
		var tableType, period, fileName, storageKey, sourceFileHash, sourceVersionID, updatedAt string
		if err := rows.Scan(&tableType, &period, &fileName, &storageKey, &sourceFileHash, &sourceVersionID, &updatedAt); err != nil {
			return nil
		}
		fileName = sourceFileName(fileName, storageKey)
		tableType = strings.ToLower(strings.TrimSpace(tableType))
		if tableType == "" || fileName == "" {
			continue
		}
		sourceFileHash = strings.TrimSpace(sourceFileHash)
		sourceVersionID = strings.TrimSpace(sourceVersionID)
		if sourceVersionID == "" && sourceFileHash != "" {
			sourceVersionID = fileName + ":" + sourceFileHash
		}
		key := strings.Join([]string{tableType, strings.TrimSpace(period), fileName}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, financeFileMappingSource{
			TableType:       tableType,
			Period:          strings.TrimSpace(period),
			FileName:        fileName,
			SourceFileHash:  sourceFileHash,
			SourceVersionID: sourceVersionID,
			UpdatedAt:       normalizeSourceUpdatedAt(updatedAt),
		})
	}
	return out
}

func (e *Engine) sourceVersionIDsForTables(spec QuerySpec, tables []string) []string {
	ids := make([]string, 0, len(tables))
	for _, mapping := range e.financeFileMappingsForTables(spec, tables) {
		if strings.TrimSpace(mapping.SourceVersionID) != "" {
			ids = append(ids, mapping.SourceVersionID)
		}
	}
	return dedupeStrings(ids)
}

func financeFileMappingTypesForSourceTable(tableName string) []string {
	base := strings.TrimSpace(baseSourceTableName(tableName))
	normalized := strings.TrimPrefix(base, "fin_")
	switch normalized {
	case "fund_income", "fund_income_groups", "fund_income_group_members":
		return []string{"fund-income"}
	case "cost_settlements", "cost_settlement_groups", "cost_settlement_group_members":
		return []string{"cost-settlements"}
	case "bank_statement":
		return []string{"bank-statement"}
	case "journal":
		return []string{"journal"}
	case "income_statement":
		return []string{"income-statement"}
	case "balance_sheet":
		return []string{"balance-sheet"}
	case "balance_detail":
		return []string{"balance-detail"}
	default:
		return nil
	}
}

func formatWorkbookSheetDisplay(fileName string, sheets []string) string {
	fileName = normalizeSourceFileName(fileName)
	if fileName == "" {
		return ""
	}
	sheets = dedupeStrings(sheets)
	if len(sheets) == 0 {
		return "《" + fileName + "》"
	}
	formatted := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		sheet = strings.TrimSpace(sheet)
		if sheet == "" {
			continue
		}
		formatted = append(formatted, "【"+sheet+"】")
	}
	if len(formatted) == 0 {
		return "《" + fileName + "》"
	}
	return "《" + fileName + "》的" + strings.Join(formatted, "和")
}

func sourceFileName(fileName, storageKey string) string {
	fileName = normalizeSourceFileName(fileName)
	if fileName != "" {
		return fileName
	}
	storageKey = strings.TrimSpace(strings.ReplaceAll(storageKey, `\`, `/`))
	if storageKey == "" {
		return ""
	}
	parts := strings.Split(storageKey, "/")
	return normalizeSourceFileName(parts[len(parts)-1])
}

func preferredSourceFileName(fileNames ...string) string {
	for i, fileName := range fileNames {
		if i == len(fileNames)-1 {
			if name := sourceFileName("", fileName); name != "" {
				return name
			}
			continue
		}
		if name := normalizeSourceFileName(fileName); name != "" {
			return name
		}
	}
	return ""
}

func normalizeSourceFileName(name string) string {
	name = strings.TrimSpace(name)
	return name
}

func financeSourcePeriodAliases(spec QuerySpec) []string {
	from := strings.TrimSpace(spec.PeriodFrom)
	to := strings.TrimSpace(spec.PeriodTo)
	if from == "" {
		from = strings.TrimSpace(spec.BossRewrite.PeriodFrom)
	}
	if to == "" {
		to = strings.TrimSpace(spec.BossRewrite.PeriodTo)
	}
	if to == "" {
		to = from
	}
	if from == "" || to == "" {
		return nil
	}
	fromYear, fromMonth := parsePeriod(from)
	toYear, toMonth := parsePeriod(to)
	if fromYear == 0 || fromMonth == 0 || toYear == 0 || toMonth == 0 {
		return []string{from, to}
	}
	aliases := make([]string, 0, 8)
	for y, m := fromYear, fromMonth; y < toYear || (y == toYear && m <= toMonth); {
		aliases = append(aliases, fmt.Sprintf("%04d-%02d", y, m))
		aliases = append(aliases, fmt.Sprintf("%04d-Q%d", y, ((m-1)/3)+1))
		aliases = append(aliases, fmt.Sprintf("%04d", y))
		m++
		if m > 12 {
			y++
			m = 1
		}
		if len(aliases) > 120 {
			break
		}
	}
	return dedupeStrings(aliases)
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, "?")
	}
	return strings.Join(items, ", ")
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func setKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	return out
}

func shouldHideSourceTableFromBossNote(tableName string) bool {
	switch baseSourceTableName(tableName) {
	case "fin_fund_income_groups",
		"fin_fund_income_group_members",
		"fin_cost_settlement_groups",
		"fin_cost_settlement_group_members":
		return true
	default:
		return false
	}
}

func (e *Engine) buildSourceUpdateNote(spec QuerySpec, tables []string, metadata map[string]dbpkg.TableSourceMetadata) string {
	updatedAt := e.latestSourceUpdatedAt(spec, tables, metadata)
	if updatedAt == "" {
		return ""
	}
	return "来源更新时间：" + updatedAt
}

func (e *Engine) latestSourceUpdatedAt(spec QuerySpec, tables []string, metadata map[string]dbpkg.TableSourceMetadata) string {
	mappingLatest := e.latestFinanceFileMappingUpdatedAt(spec, tables)
	if e.hasFinanceFileMappingsTable() && sourceTablesRequireFinanceFileMappings(tables) {
		return mappingLatest
	}
	rowLatest := ""
	metaLatest := ""
	for _, tableName := range tables {
		if !shouldSkipSourceTableRowUpdate(tableName) {
			if fromRows := e.latestTableUpdatedAt(tableName); fromRows != "" && fromRows > rowLatest {
				rowLatest = fromRows
				continue
			}
		}
		if shouldHideSourceTableFromBossNote(tableName) {
			continue
		}
		if meta, ok := metadata[tableName]; ok {
			if fromMeta := normalizeSourceUpdatedAt(meta.UpdatedAt); fromMeta != "" && fromMeta > metaLatest {
				metaLatest = fromMeta
			}
		}
	}
	if rowLatest != "" {
		return maxSourceUpdatedAt(mappingLatest, rowLatest)
	}
	return maxSourceUpdatedAt(mappingLatest, metaLatest)
}

func (e *Engine) latestFinanceFileMappingUpdatedAt(spec QuerySpec, tables []string) string {
	latest := ""
	for _, mapping := range e.financeFileMappingsForTables(spec, tables) {
		if mapping.UpdatedAt != "" && mapping.UpdatedAt > latest {
			latest = mapping.UpdatedAt
		}
	}
	return latest
}

func shouldSkipSourceTableRowUpdate(tableName string) bool {
	switch baseSourceTableName(tableName) {
	case "fin_fund_income_group_members", "fin_cost_settlement_group_members":
		return true
	default:
		return false
	}
}

func (e *Engine) latestTableUpdatedAt(tableName string) string {
	base := baseSourceTableName(tableName)
	cols := e.tableColumns(base)
	if len(cols) == 0 {
		cols = e.tableColumns(tableName)
	}
	col := ""
	switch {
	case cols["updated_at"]:
		col = "updated_at"
	case cols["created_at"]:
		col = "created_at"
	default:
		return ""
	}
	target := tableName
	if len(e.tableColumns(target)) == 0 && len(e.tableColumns(base)) > 0 {
		target = base
	}
	var raw sql.NullString
	query := fmt.Sprintf("SELECT COALESCE(CAST(MAX(%s) AS TEXT), '') FROM %s", col, target)
	if err := e.db.QueryRow(query).Scan(&raw); err != nil {
		return ""
	}
	return normalizeSourceUpdatedAt(raw.String)
}

func normalizeSourceUpdatedAt(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.Index(value, "T"); idx > 0 {
		value = strings.Replace(value, "T", " ", 1)
	}
	value = strings.TrimSuffix(value, "Z")
	if idx := strings.Index(value, "."); idx > 0 {
		value = value[:idx]
	}
	if len(value) >= len("2006-01-02 15:04:05") {
		return value[:len("2006-01-02 15:04:05")]
	}
	return value
}

func maxSourceUpdatedAt(left, right string) string {
	left = normalizeSourceUpdatedAt(left)
	right = normalizeSourceUpdatedAt(right)
	if left == "" {
		return right
	}
	if right == "" || left > right {
		return left
	}
	return right
}
