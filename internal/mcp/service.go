package mcp

import (
	"context"
	"database/sql"
	"regexp"
	"strings"

	"financeqa/internal/db"
	"financeqa/internal/dimensions"
	"financeqa/internal/ingest"
	"financeqa/internal/query"
)

var financeQueryKeywords = []string{
	"财务", "经营", "合同", "项目", "客户", "供应商",
	"回款", "收款", "付款", "开票", "发票",
	"收入", "营收", "成本", "费用", "利润", "净利润",
	"应收", "应付", "到账", "支出", "现金", "银行", "余额",
}

var protectedFinanceTerms = []string{
	"账上", "序时账", "序时帐", "凭证",
	"科目余额", "资产负债表", "利润表", "收入表", "余额表",
	"银行流水", "银行卡", "银行账户", "官方余额表",
	"财务口径", "项目口径", "合同口径", "项目成本口径",
	"实际到账", "实际支出", "应收账款", "应付账款",
	"差多少", "差异金额", "名义差额", "差异", "差额", "相差", "差了",
}

var relativeFinancePeriodTerms = []string{
	"上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份",
	"上一个完整月", "上个完整月", "最近完整月份", "最新完整月份",
	"最近月份", "最新月份", "上个月", "上月",
	"至今", "到现在", "现在", "当前", "目前",
}

var financeRetryOrContinuationTerms = []string{
	"继续", "接着", "再算一次", "重新算", "重算", "再查一次", "重新查",
}

var financeContinuationFillerTerms = []string{
	"请", "一下", "吧", "麻烦", "帮我",
}

var projectRosterMetricTerms = []string{
	"应付", "应收", "未付款", "未回款",
}

var projectRosterScopeTerms = []string{
	"哪些", "有哪些", "都有哪些", "列出", "列表", "明细", "每个", "各",
}

var (
	absoluteFinanceMonthChinesePattern = regexp.MustCompile(`20\d{2}\s*年\s*(0?[1-9]|1[0-2])\s*月`)
	absoluteFinanceMonthDashPattern    = regexp.MustCompile(`20\d{2}\s*[-/.]\s*(0?[1-9]|1[0-2])`)
	absoluteFinanceMonthChineseKey     = regexp.MustCompile(`(20\d{2})\s*年\s*(0?[1-9]|1[0-2])\s*月`)
	absoluteFinanceMonthDashKey        = regexp.MustCompile(`(20\d{2})\s*[-/.]\s*(0?[1-9]|1[0-2])`)
	specificFinanceSubjectPattern      = regexp.MustCompile(`[\p{Han}A-Za-z0-9_（）()·\-－]{2,}(?:合同|协议|项目)[\-－]?[A-Za-z0-9]+`)
)

// ServiceConfig contains the business configuration shared by MCP transports.
type ServiceConfig struct {
	DBPath       string
	Company      string
	SkillPath    string
	AppendixPath string
}

// Service executes FinanceQA tools without owning a transport.
type Service struct {
	config ServiceConfig
}

// NewService creates a transport-independent FinanceQA MCP tool service.
func NewService(config ServiceConfig) *Service {
	return &Service{config: config}
}

// Tools returns the public FinanceQA MCP tool definitions.
func (s *Service) Tools() []Tool {
	return financeTools()
}

// RunTool executes a FinanceQA MCP tool and returns the native payload.
func (s *Service) RunTool(ctx context.Context, name string, args map[string]any) (ToolRunResult, error) {
	if args == nil {
		args = map[string]any{}
	}

	switch name {
	case "finance-query":
		return s.runFinanceQuery(ctx, args)
	case "finance-host-data":
		return s.runFinanceHostData(ctx, args)
	case "finance-upload":
		return s.runFinanceUpload(ctx, args)
	case "finance-sync":
		return s.runFinanceSync(ctx, args)
	case "finance-dimensions":
		return s.runFinanceDimensions(ctx, args)
	default:
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Unknown tool", Data: name}
	}
}

func (s *Service) runFinanceQuery(ctx context.Context, args map[string]any) (ToolRunResult, error) {
	queryStr, _ := args["query"].(string)
	queryStr = financeUserQuestionBlock(queryStr)
	if queryStr == "" {
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Missing required argument", Data: "query"}
	}
	rawUserQuery, _ := args["raw_user_query"].(string)
	rawUserQuery = financeUserQuestionBlock(rawUserQuery)
	queryStr = effectiveFinanceQuery(queryStr, rawUserQuery)

	engine, err := query.NewReadOnlyEngine(s.config.DBPath, s.config.Company)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to create query engine", Data: err.Error()}
	}
	defer engine.Close()

	return ToolRunResult{Operation: "query", Payload: engine.Query(queryStr)}, nil
}

func effectiveFinanceQuery(rewritten, rawUser string) string {
	queryText := strings.TrimSpace(financeUserQuestionBlock(rewritten))
	rawText := strings.TrimSpace(financeUserQuestionBlock(rawUser))
	if rawText == "" || rawText == queryText || !looksLikeFinanceQueryText(rawText) {
		return queryText
	}
	if queryText == "" {
		return rawText
	}
	if shouldPreferRawFinanceSemantics(rawText, queryText) {
		return mergeProtectedFinanceQuery(rawText, queryText)
	}
	return queryText
}

func financeUserQuestionBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	type marker struct {
		token string
	}
	markers := []marker{
		{token: "[用户原问题]"},
		{token: "【用户原问题】"},
		{token: "用户原问题："},
		{token: "用户原问题:"},
	}
	bestIndex := -1
	bestToken := ""
	for _, marker := range markers {
		if idx := strings.LastIndex(trimmed, marker.token); idx > bestIndex {
			bestIndex = idx
			bestToken = marker.token
		}
	}
	if bestIndex < 0 {
		return trimmed
	}
	candidate := strings.TrimSpace(trimmed[bestIndex+len(bestToken):])
	if candidate == "" {
		return trimmed
	}
	return candidate
}

func looksLikeFinanceQueryText(text string) bool {
	return containsAnyText(text, financeQueryKeywords)
}

func shouldPreferRawFinanceSemantics(rawUser, rewritten string) bool {
	return isFinanceRetryOrContinuation(rewritten) ||
		isCompanyProjectRosterQuery(rawUser) ||
		missingProtectedFinanceTerms(rawUser, rewritten) ||
		lostSpecificFinanceSubject(rawUser, rewritten) ||
		addedAbsoluteFinanceMonthToRelativePeriod(rawUser, rewritten) ||
		lostRelativeFinancePeriod(rawUser, rewritten) ||
		lostAbsoluteFinanceMonth(rawUser, rewritten)
}

func isFinanceRetryOrContinuation(text string) bool {
	if looksLikeFinanceQueryText(text) {
		return false
	}
	normalized := strings.Trim(strings.TrimSpace(text), "，,。.！!？?；;：:")
	matched := false
	for _, term := range financeRetryOrContinuationTerms {
		if strings.Contains(normalized, term) {
			matched = true
			normalized = strings.ReplaceAll(normalized, term, "")
		}
	}
	if !matched {
		return false
	}
	for _, filler := range financeContinuationFillerTerms {
		normalized = strings.ReplaceAll(normalized, filler, "")
	}
	return strings.Trim(strings.TrimSpace(normalized), "，,。.！!？?；;：:") == ""
}

func isCompanyProjectRosterQuery(text string) bool {
	return strings.Contains(text, "项目") &&
		containsAnyText(text, projectRosterMetricTerms) &&
		containsAnyText(text, projectRosterScopeTerms)
}

func missingProtectedFinanceTerms(rawUser, rewritten string) bool {
	for _, term := range protectedFinanceTerms {
		if strings.Contains(rawUser, term) && !strings.Contains(rewritten, term) {
			return true
		}
	}
	return false
}

func lostRelativeFinancePeriod(rawUser, rewritten string) bool {
	if !containsAnyText(rawUser, relativeFinancePeriodTerms) {
		return false
	}
	return !containsAnyText(rewritten, relativeFinancePeriodTerms)
}

func lostAbsoluteFinanceMonth(rawUser, rewritten string) bool {
	rawMonths := absoluteFinanceMonthKeys(rawUser)
	if len(rawMonths) == 0 {
		return false
	}
	rewrittenMonths := absoluteFinanceMonthKeys(rewritten)
	for month := range rawMonths {
		if !rewrittenMonths[month] {
			return true
		}
	}
	return false
}

func addedAbsoluteFinanceMonthToRelativePeriod(rawUser, rewritten string) bool {
	if !containsAnyText(rawUser, relativeFinancePeriodTerms) {
		return false
	}
	rawMonths := absoluteFinanceMonthKeys(rawUser)
	for month := range absoluteFinanceMonthKeys(rewritten) {
		if !rawMonths[month] {
			return true
		}
	}
	return false
}

func lostSpecificFinanceSubject(rawUser, rewritten string) bool {
	rewrittenKey := normalizeFinanceSubjectText(rewritten)
	if rewrittenKey == "" {
		return false
	}
	for _, subject := range specificFinanceSubjectCandidates(rawUser) {
		if !strings.Contains(rewrittenKey, normalizeFinanceSubjectText(subject)) {
			return true
		}
	}
	return false
}

func specificFinanceSubjectCandidates(text string) []string {
	matches := specificFinanceSubjectPattern.FindAllString(strings.TrimSpace(text), -1)
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		subject := trimSpecificFinanceSubject(match)
		key := normalizeFinanceSubjectText(subject)
		if key == "" || seen[key] || len([]rune(subject)) < 4 {
			continue
		}
		seen[key] = true
		out = append(out, subject)
	}
	return out
}

func trimSpecificFinanceSubject(subject string) string {
	trimmed := strings.Trim(strings.TrimSpace(subject), "，,。；;：:的从按看")
	for {
		next := strings.TrimSpace(trimmed)
		for _, prefix := range []string{"按合同", "按项目", "按协议", "合同", "项目", "协议"} {
			if strings.HasPrefix(next, prefix) && strings.Count(next, strings.TrimPrefix(prefix, "按")) > 1 {
				next = strings.TrimSpace(strings.TrimPrefix(next, prefix))
			}
		}
		next = strings.Trim(next, "，,。；;：:的从按看")
		if next == trimmed {
			return next
		}
		trimmed = next
	}
}

func normalizeFinanceSubjectText(text string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "（", "(", "）", ")", "－", "-", "，", "", ",", "", "。", "", "；", "", ";", "", "：", "", ":", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(text)))
}

func mergeProtectedFinanceQuery(rawUser, rewritten string) string {
	rawText := strings.TrimSpace(rawUser)
	hint := strings.TrimSpace(rewritten)
	if rawText == "" || hint == "" || rawText == hint {
		return rawText
	}
	if isFinanceRetryOrContinuation(hint) {
		return rawText
	}
	if isCompanyProjectRosterQuery(rawText) {
		subjects := specificFinanceRosterSubjectCandidates(hint)
		if len(subjects) == 0 {
			return rawText
		}
		hint = strings.Join(subjects, " ")
	}
	if containsAnyText(rawText, relativeFinancePeriodTerms) || len(absoluteFinanceMonthKeys(rawText)) > 0 {
		hint = stripFinancePeriodPhrases(hint)
	}
	if hint == "" || strings.Contains(rawText, hint) {
		return rawText
	}
	return rawText + "；补充识别：" + hint
}

func specificFinanceRosterSubjectCandidates(text string) []string {
	all := specificFinanceSubjectCandidates(text)
	validated := make([]string, 0, len(all)+1)
	seen := map[string]bool{}
	for _, subject := range all {
		if hasSpecificFinanceRosterIdentifier(subject) {
			key := normalizeFinanceSubjectText(subject)
			seen[key] = true
			validated = append(validated, subject)
		}
	}
	for _, field := range strings.Fields(text) {
		candidate := strings.Trim(field, "，,。.！!？?；;：:")
		if len([]rune(candidate)) < 4 || !hasFinanceOrganizationSuffix(candidate) {
			continue
		}
		key := normalizeFinanceSubjectText(candidate)
		if !seen[key] {
			seen[key] = true
			validated = append(validated, candidate)
		}
	}
	return validated
}

func hasSpecificFinanceRosterIdentifier(subject string) bool {
	for _, marker := range []string{"合同", "协议", "项目"} {
		idx := strings.LastIndex(subject, marker)
		if idx < 0 {
			continue
		}
		suffix := strings.TrimSpace(subject[idx+len(marker):])
		upperSuffix := strings.ToUpper(strings.TrimLeft(suffix, "-－"))
		if strings.HasPrefix(upperSuffix, "ID") {
			return false
		}
		return strings.HasPrefix(suffix, "-") || strings.HasPrefix(suffix, "－") || strings.ContainsAny(suffix, "0123456789")
	}
	return false
}

func hasFinanceOrganizationSuffix(text string) bool {
	for _, suffix := range []string{"有限责任公司", "股份有限公司", "有限公司", "集团公司", "集团"} {
		if strings.HasSuffix(text, suffix) {
			return true
		}
	}
	return false
}

func stripFinancePeriodPhrases(text string) string {
	cleaned := absoluteFinanceMonthChinesePattern.ReplaceAllString(text, " ")
	cleaned = absoluteFinanceMonthDashPattern.ReplaceAllString(cleaned, " ")
	for _, term := range relativeFinancePeriodTerms {
		cleaned = strings.ReplaceAll(cleaned, term, " ")
	}
	return strings.Join(strings.Fields(cleaned), " ")
}

func absoluteFinanceMonthKeys(text string) map[string]bool {
	keys := map[string]bool{}
	for _, match := range absoluteFinanceMonthChineseKey.FindAllStringSubmatch(text, -1) {
		keys[financeMonthKey(match[1], match[2])] = true
	}
	for _, match := range absoluteFinanceMonthDashKey.FindAllStringSubmatch(text, -1) {
		keys[financeMonthKey(match[1], match[2])] = true
	}
	return keys
}

func financeMonthKey(year, month string) string {
	month = strings.TrimLeft(month, "0")
	if len(month) == 1 {
		month = "0" + month
	}
	return year + "-" + month
}

func containsAnyText(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func (s *Service) runFinanceHostData(ctx context.Context, args map[string]any) (ToolRunResult, error) {
	queryStr, _ := args["query"].(string)
	from, _ := args["from"].(string)
	to, _ := args["to"].(string)

	if queryStr == "" {
		queryStr = "输出全量财报原始数据给宿主LLM"
	}

	engine, err := query.NewReadOnlyEngine(s.config.DBPath, s.config.Company)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to create query engine", Data: err.Error()}
	}
	defer engine.Close()

	return ToolRunResult{Operation: "host-data", Payload: engine.HostLLMPayload(from, to, queryStr)}, nil
}

func (s *Service) runFinanceUpload(ctx context.Context, args map[string]any) (ToolRunResult, error) {
	filePath, _ := args["file"].(string)
	if filePath == "" {
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Missing required argument", Data: "file"}
	}

	importer, err := s.newImporter(ctx)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to open database", Data: err.Error()}
	}
	defer importer.close()

	summary, err := importer.ingest.ImportFileWithOptions(ctx, s.config.DBPath, filePath, importOptionsFromArgs(args))
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Import failed", Data: err.Error()}
	}

	return ToolRunResult{Operation: "upload", Payload: summary}, nil
}

func (s *Service) runFinanceSync(ctx context.Context, args map[string]any) (ToolRunResult, error) {
	dirPath, _ := args["directory"].(string)
	if dirPath == "" {
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Missing required argument", Data: "directory"}
	}

	importer, err := s.newImporter(ctx)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to open database", Data: err.Error()}
	}
	defer importer.close()

	summary, err := importer.ingest.SyncDirectoryWithOptions(ctx, s.config.DBPath, dirPath, importOptionsFromArgs(args))
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Sync failed", Data: err.Error()}
	}

	return ToolRunResult{Operation: "sync", Payload: summary}, nil
}

func (s *Service) runFinanceDimensions(ctx context.Context, args map[string]any) (ToolRunResult, error) {
	action, _ := args["action"].(string)

	dbConn, err := db.Open(ctx, s.config.DBPath)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to open database", Data: err.Error()}
	}
	defer dbConn.Close()

	manager := dimensions.NewManager(dimensions.NewSQLiteRepository(dbConn))

	switch action {
	case "list":
		result, err := manager.ListDimensions(ctx, dimensions.DimensionQueryOptions{Limit: 100})
		if err != nil {
			return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to list dimensions", Data: err.Error()}
		}
		return ToolRunResult{Operation: "dimensions:list", Payload: result}, nil
	default:
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Unknown dimensions action", Data: action}
	}
}

func (s *Service) newImporter(ctx context.Context) (*mcpImporter, error) {
	dbConn, err := db.Open(ctx, s.config.DBPath)
	if err != nil {
		return nil, err
	}
	manager := dimensions.NewManager(dimensions.NewSQLiteRepository(dbConn))
	return &mcpImporter{
		ingest: ingest.NewImporter(manager),
		db:     dbConn,
	}, nil
}

type mcpImporter struct {
	ingest *ingest.Importer
	db     *sql.DB
}

func (i *mcpImporter) close() {
	if i != nil && i.db != nil {
		i.db.Close()
	}
}

func importOptionsFromArgs(args map[string]any) ingest.ImportOptions {
	company, _ := args["company"].(string)
	incremental, _ := args["incremental"].(bool)
	return ingest.ImportOptions{
		Incremental:     incremental,
		CompanyOverride: company,
	}
}
