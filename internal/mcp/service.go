package mcp

import (
	"context"
	"database/sql"
	"strings"

	"financeqa/internal/db"
	"financeqa/internal/dimensions"
	"financeqa/internal/ingest"
	"financeqa/internal/query"
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
	if queryStr == "" {
		return ToolRunResult{}, &ToolError{Code: -32602, Message: "Missing required argument", Data: "query"}
	}
	rawUserQuery, _ := args["raw_user_query"].(string)
	queryStr = effectiveFinanceQuery(queryStr, rawUserQuery)

	engine, err := query.NewReadOnlyEngine(s.config.DBPath, s.config.Company)
	if err != nil {
		return ToolRunResult{}, &ToolError{Code: -32603, Message: "Failed to create query engine", Data: err.Error()}
	}
	defer engine.Close()

	return ToolRunResult{Operation: "query", Payload: engine.Query(queryStr)}, nil
}

func effectiveFinanceQuery(rewritten, rawUser string) string {
	queryText := strings.TrimSpace(rewritten)
	rawText := strings.TrimSpace(rawUser)
	if rawText == "" || rawText == queryText || !looksLikeFinanceQueryText(rawText) {
		return queryText
	}
	if queryText == "" {
		return rawText
	}
	if missingProtectedFinanceTerms(rawText, queryText) || lostRelativeFinancePeriod(rawText, queryText) {
		return rawText
	}
	return queryText
}

func looksLikeFinanceQueryText(text string) bool {
	return containsAnyText(text, []string{
		"财务", "经营", "合同", "项目", "客户", "供应商",
		"回款", "收款", "付款", "开票", "发票",
		"收入", "营收", "成本", "费用", "利润", "净利润",
		"应收", "应付", "到账", "支出", "现金", "银行", "余额",
	})
}

func missingProtectedFinanceTerms(rawUser, rewritten string) bool {
	for _, term := range []string{
		"账上", "序时账", "序时帐", "凭证",
		"科目余额", "资产负债表", "利润表", "余额表",
		"银行流水", "银行卡", "银行账户", "官方余额表",
		"财务口径", "项目口径", "合同口径", "项目成本口径",
		"实际到账", "实际支出", "应收账款", "应付账款",
	} {
		if strings.Contains(rawUser, term) && !strings.Contains(rewritten, term) {
			return true
		}
	}
	return false
}

func lostRelativeFinancePeriod(rawUser, rewritten string) bool {
	hasRelative := containsAnyText(rawUser, []string{
		"上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份",
		"最近完整月份", "最新完整月份", "最近月份", "最新月份",
		"至今", "到现在", "现在", "当前", "目前",
	})
	if !hasRelative {
		return false
	}
	return !containsAnyText(rewritten, []string{
		"上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份",
		"最近完整月份", "最新完整月份", "最近月份", "最新月份",
		"至今", "到现在", "现在", "当前", "目前",
	})
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
