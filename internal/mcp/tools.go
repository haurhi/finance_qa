package mcp

import (
	"context"
	"encoding/json"
)

func financeTools() []Tool {
	return []Tool{
		{
			Name:        "finance-query",
			Description: "Query financial data using natural language. Supports revenue, cost, profit, AR/AP, receipts, payments, and contract dimension queries.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language query in Chinese, e.g., '2026年3月应收账款有多少' or '金程科技的收入是多少'",
					},
					"raw_user_query": map[string]any{
						"type":        "string",
						"description": "Original user finance question before agent rewrite; used to protect intent, period, and source basis.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "finance-host-data",
			Description: "Provide full financial data payload to host LLM for complex reasoning when direct query fails or ambiguous.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Context question or data request",
					},
					"from": map[string]any{
						"type":        "string",
						"description": "Period start in YYYY-MM format",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "Period end in YYYY-MM format",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "finance-upload",
			Description: "Import a single financial Excel file (income statement, balance sheet, journal, etc.)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "Absolute path to the Excel file to import",
					},
					"company": map[string]any{
						"type":        "string",
						"description": "Override company name for this file",
					},
					"incremental": map[string]any{
						"type":        "boolean",
						"description": "Incremental import (don't clear existing data)",
					},
				},
				"required": []string{"file"},
			},
		},
		{
			Name:        "finance-sync",
			Description: "Synchronize a directory of financial Excel files",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": map[string]any{
						"type":        "string",
						"description": "Directory path containing Excel files",
					},
					"company": map[string]any{
						"type":        "string",
						"description": "Override company name for all files",
					},
					"incremental": map[string]any{
						"type":        "boolean",
						"description": "Incremental sync",
					},
				},
				"required": []string{"directory"},
			},
		},
		{
			Name:        "finance-dimensions",
			Description: "Manage dimension mappings: list dimensions, add members, import/export, or preview",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "mapping-stats", "seed-standard", "export-package", "import-dimensions", "import-members", "import-rules", "preview-import"},
						"description": "Dimension management action",
					},
					"company": map[string]any{
						"type":        "string",
						"description": "Company name for seed-standard action",
					},
					"file": map[string]any{
						"type":        "string",
						"description": "File path for import/preview actions",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Type for preview-import: dimensions or members",
					},
					"dimension": map[string]any{
						"type":        "string",
						"description": "Dimension code for add-member action",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (s *Server) handleToolsList(req *Request) error {
	return s.sendResponse(req.ID, ToolsListResult{Tools: financeTools()})
}

func (s *Server) handleToolsCall(ctx context.Context, req *Request) error {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.sendErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	runner := s.toolRunner
	if runner == nil {
		runner = NewService(ServiceConfig{
			DBPath:       s.dbPath,
			Company:      s.company,
			SkillPath:    s.skillPath,
			AppendixPath: s.appendixPath,
		})
	}

	result, err := runner.RunTool(ctx, params.Name, params.Arguments)
	if err != nil {
		if toolErr, ok := err.(*ToolError); ok {
			return s.sendErrorResponse(req.ID, toolErr.Code, toolErr.Message, toolErr.Data)
		}
		return s.sendErrorResponse(req.ID, -32603, "Tool failed", err.Error())
	}
	operation := result.Operation
	if operation == "" {
		operation = params.Name
	}
	return s.sendToolResponse(req.ID, params.Name, operation, result.Payload)
}
