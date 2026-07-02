package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinanceQueryToolAddsBridgeEnvelope(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "mcp.sqlite")
	skillPath := filepath.Join(tmp, "SKILL.md")
	appendixPath := filepath.Join(tmp, "docs", "SKILL_APPENDIX_FULL.md")
	if err := os.WriteFile(skillPath, []byte("1. `skill_contract_version`: `2026-04-26.1`\n2. `bridge_protocol_version`: `v2`\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(appendixPath), 0o755); err != nil {
		t.Fatalf("mkdir appendix: %v", err)
	}
	if err := os.WriteFile(appendixPath, []byte("# appendix\n"), 0o644); err != nil {
		t.Fatalf("write appendix: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"finance-query","arguments":{"query":"2026年3月营收是多少"}}}`,
	}, "\n") + "\n"
	var stdout, stderr bytes.Buffer
	server := NewServer(
		WithDBPath(dbPath),
		WithSkillPath(skillPath),
		WithAppendixPath(appendixPath),
		WithIO(strings.NewReader(input), &stdout, &stderr),
	)

	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("run server: %v; stderr=%s", err, stderr.String())
	}

	response := responseByID(t, stdout.String(), float64(2))
	result := response["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal tool payload: %v; text=%s", err, text)
	}
	if _, ok := payload["final_answer"].(string); !ok {
		t.Fatalf("Go MCP payload should expose top-level final_answer, got %#v", payload)
	}
	bridgeMeta, ok := payload["bridge_meta"].(map[string]any)
	if !ok {
		t.Fatalf("Go MCP payload should expose bridge_meta, got %#v", payload)
	}
	if bridgeMeta["protocol_version"] != "v2" {
		t.Fatalf("protocol_version = %v, want v2", bridgeMeta["protocol_version"])
	}
	if bridgeMeta["skill_contract_version"] != "2026-04-26.1" {
		t.Fatalf("skill_contract_version = %v, want 2026-04-26.1", bridgeMeta["skill_contract_version"])
	}
	if bridgeMeta["tool_name"] != "finance-query" {
		t.Fatalf("tool_name = %v, want finance-query", bridgeMeta["tool_name"])
	}
}

func TestInitializeReportsCurrentMCPServerVersion(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	var stdout, stderr bytes.Buffer
	server := NewServer(WithIO(strings.NewReader(input), &stdout, &stderr))

	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("run server: %v; stderr=%s", err, stderr.String())
	}

	response := responseByID(t, stdout.String(), float64(1))
	result := response["result"].(map[string]any)
	serverInfo := result["serverInfo"].(map[string]any)
	if got := serverInfo["version"]; got != "2.2.26" {
		t.Fatalf("MCP server version = %v, want 2.2.26", got)
	}
}

func TestBridgeEnvelopeHidesInternalFieldsDeeply(t *testing.T) {
	t.Parallel()

	server := NewServer()
	payload := server.bridgeEnvelope("finance-query", "query", map[string]any{
		"success":       true,
		"message":       "合同应收 2568793.24 元，来源 tenant_uhub.fin_contracts。",
		"answer_method": "sql",
		"data": map[string]any{
			"source_tables": []any{"tenant_uhub.fin_contracts", "tenant_uhub.fin_fund_income"},
			"contract_summary": map[string]any{
				"contract_id":        "C012",
				"account_code":       "1122",
				"source_report_type": "contract_fund_income",
				"receivable":         2568793.24,
			},
			"contracts": []any{
				map[string]any{
					"contract_id":      "C012",
					"id":               1,
					"storage_key":      "s3://bucket/contract.pdf",
					"customer_name":    "百度在线网络技术（北京）有限公司",
					"contract_content": "数据服务",
				},
			},
		},
		"executed_sql":      []any{"SELECT contract_id FROM tenant_uhub.fin_contracts WHERE contract_id='C012'"},
		"calculation_logs":  []any{"contract_id=C012 account_code=1122"},
		"source_sheet_name": "26年Q1收入明细",
	})

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	rawText := string(raw)
	for _, forbidden := range []string{
		"contract_id",
		"account_code",
		"source_report_type",
		"source_sheet_name",
		"executed_sql",
		"calculation_logs",
		"tenant_uhub.fin_contracts",
		"C012",
		"1122",
		"s3://bucket/contract.pdf",
	} {
		if strings.Contains(rawText, forbidden) {
			t.Fatalf("bridge payload should hide %q, got %s", forbidden, rawText)
		}
	}
	if !strings.Contains(rawText, "百度在线网络技术") {
		t.Fatalf("bridge payload should keep business-facing fields, got %s", rawText)
	}
}

func TestBridgeEnvelopeFinalAnswerKeepsHumanSourceNotes(t *testing.T) {
	t.Parallel()

	server := NewServer()
	payload := server.bridgeEnvelope("finance-query", "query", map[string]any{
		"success":       true,
		"message":       "合同台账结算 3600800.00 元。",
		"answer_method": "sql",
		"data": map[string]any{
			"entity":             "飞未云科（深圳）技术有限公司",
			"period":             "2026-01~2026-03",
			"asked_topic":        "revenue",
			"role":               "customer_contract",
			"cash_view":          map[string]any{"received_amount": 3600800},
			"book_view":          map[string]any{"settlement_amount": 3600800, "invoice_amount": 3600800},
			"source_note":        "来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q1收入明细】",
			"source_update_note": "来源更新时间：2026-05-05 20:46:23",
			"query_spec":         map[string]any{"query_family": "contract_dimension"},
		},
		"executed_sql":     []any{"SELECT contract_id FROM tenant_uhub.fin_fund_income"},
		"calculation_logs": []any{"contract_id=C001"},
	})

	finalAnswer, _ := payload["final_answer"].(string)
	for _, want := range []string{
		"合同到账 3600800.00 元",
		"合同结算 3600800.00 元",
		"来源：《优集收入、成本计算表 - 上传.xlsx》的【26年Q1收入明细】",
		"来源更新时间：2026-05-05 20:46:23",
	} {
		if !strings.Contains(finalAnswer, want) {
			t.Fatalf("final_answer should include %q, got %s", want, finalAnswer)
		}
	}
	for _, forbidden := range []string{"tenant_uhub", "contract_id", "C001", "executed_sql"} {
		if strings.Contains(finalAnswer, forbidden) {
			t.Fatalf("final_answer should hide %q, got %s", forbidden, finalAnswer)
		}
	}
}

func TestMCPListsToolsAndReadsConfiguredResources(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	skillPath := filepath.Join(tmp, "SKILL.md")
	appendixPath := filepath.Join(tmp, "docs", "SKILL_APPENDIX_FULL.md")
	if err := os.WriteFile(skillPath, []byte("skill body"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(appendixPath), 0o755); err != nil {
		t.Fatalf("mkdir appendix: %v", err)
	}
	if err := os.WriteFile(appendixPath, []byte("appendix body"), 0o644); err != nil {
		t.Fatalf("write appendix: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"financeqa://skill"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"financeqa://appendix"}}`,
	}, "\n") + "\n"
	var stdout, stderr bytes.Buffer
	server := NewServer(
		WithSkillPath(skillPath),
		WithAppendixPath(appendixPath),
		WithIO(strings.NewReader(input), &stdout, &stderr),
	)

	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("run server: %v; stderr=%s", err, stderr.String())
	}

	toolsResult := responseByID(t, stdout.String(), float64(2))["result"].(map[string]any)
	tools := toolsResult["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("tools/list should expose 5 tools, got %d", len(tools))
	}
	if first := tools[0].(map[string]any)["name"]; first != "finance-query" {
		t.Fatalf("first tool = %v, want finance-query", first)
	}

	resourcesResult := responseByID(t, stdout.String(), float64(3))["result"].(map[string]any)
	resources := resourcesResult["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("resources/list should expose skill and appendix, got %#v", resources)
	}
	if got := resourceReadText(t, responseByID(t, stdout.String(), float64(4))); got != "skill body" {
		t.Fatalf("skill resource text = %q", got)
	}
	if got := resourceReadText(t, responseByID(t, stdout.String(), float64(5))); got != "appendix body" {
		t.Fatalf("appendix resource text = %q", got)
	}
}

func TestFinanceToolsDefinitionIsStandalone(t *testing.T) {
	t.Parallel()

	tools := financeTools()
	if len(tools) != 5 {
		t.Fatalf("financeTools should expose 5 tools, got %d", len(tools))
	}
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"finance-query", "finance-host-data", "finance-upload", "finance-sync", "finance-dimensions"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("financeTools missing %s", name)
		}
	}
	required := byName["finance-query"].InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("finance-query required = %#v, want [query]", required)
	}
	dimProps := byName["finance-dimensions"].InputSchema["properties"].(map[string]any)
	action := dimProps["action"].(map[string]any)
	enum := action["enum"].([]string)
	if len(enum) == 0 || enum[0] != "list" {
		t.Fatalf("finance-dimensions action enum = %#v", enum)
	}
}

func TestMCPReturnsJSONRPCErrorsForInvalidRequests(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"unknown","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"finance-query","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"missing-tool","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"finance-upload","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"finance-sync","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"financeqa://missing"}}`,
	}, "\n") + "\n"
	var stdout, stderr bytes.Buffer
	server := NewServer(WithIO(strings.NewReader(input), &stdout, &stderr))

	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("run server: %v; stderr=%s", err, stderr.String())
	}

	assertJSONRPCError(t, stdout.String(), 1, -32601, "Method not found")
	assertJSONRPCError(t, stdout.String(), 2, -32602, "Missing required argument")
	assertJSONRPCError(t, stdout.String(), 3, -32602, "Unknown tool")
	assertJSONRPCError(t, stdout.String(), 4, -32602, "Missing required argument")
	assertJSONRPCError(t, stdout.String(), 5, -32602, "Missing required argument")
	assertJSONRPCError(t, stdout.String(), 6, -32602, "Resource not found")
}

func TestMCPToolRunnerUsesProductionEnvelopeAndToolErrors(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"finance-query","arguments":{"query":"2026年3月收入"}}}` + "\n"
	var stdout, stderr bytes.Buffer
	runner := recordingToolRunner{
		result: ToolRunResult{
			Operation: "query",
			Payload: map[string]any{
				"success": true,
				"message": "2026-03 营收 123.45 元。",
				"data":    map[string]any{"metric": "营收", "period": "2026-03", "total": 123.45},
			},
		},
	}
	server := NewServer(
		WithToolRunner(&runner),
		WithIO(strings.NewReader(input), &stdout, &stderr),
	)

	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("run server: %v; stderr=%s", err, stderr.String())
	}
	if runner.name != "finance-query" || runner.args["query"] != "2026年3月收入" {
		t.Fatalf("runner saw name=%q args=%#v", runner.name, runner.args)
	}
	payload := toolPayloadByID(t, stdout.String(), float64(1))
	if payload["tool_name"] != "finance-query" {
		t.Fatalf("tool_name = %v", payload["tool_name"])
	}
	meta := payload["bridge_meta"].(map[string]any)
	if meta["tool_operation"] != "query" {
		t.Fatalf("tool_operation = %v", meta["tool_operation"])
	}

	errorInput := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"finance-query","arguments":{"query":"bad"}}}` + "\n"
	stdout.Reset()
	stderr.Reset()
	errorRunner := recordingToolRunner{err: &ToolError{Code: -32099, Message: "tool rejected", Data: "bad query"}}
	errorServer := NewServer(
		WithToolRunner(&errorRunner),
		WithIO(strings.NewReader(errorInput), &stdout, &stderr),
	)
	if err := errorServer.Run(context.Background()); err != nil {
		t.Fatalf("run error server: %v; stderr=%s", err, stderr.String())
	}
	assertJSONRPCError(t, stdout.String(), 2, -32099, "tool rejected")
	if got := errorRunner.err.Error(); got != "tool rejected: bad query" {
		t.Fatalf("ToolError.Error() = %q", got)
	}
}

func TestBridgeEnvelopeBuildsContractAggregateDetailsAndContinuityInference(t *testing.T) {
	t.Parallel()

	server := NewServer()
	aggregate := server.bridgeEnvelope("finance-query", "query", map[string]any{
		"success": true,
		"message": "已开票未回款 52094.16 元。",
		"data": map[string]any{
			"period":          "2026-03",
			"source_priority": "contract_first",
			"money_view":      map[string]any{"回款": 3043720.05},
			"account_view":    map[string]any{"已开票未回款": 52094.16},
			"contract_summary": map[string]any{
				"scope":                      "company",
				"invoiced_unreceived_amount": 52094.16,
				"invoice_open_items": []map[string]any{
					{"customer_name": "百度在线网络技术(北京)有限公司", "contract_content": "边缘计算资源服务协议", "open_amount": 41500.0},
				},
			},
			"query_spec": map[string]any{"query_family": "core_metric"},
		},
	})
	finalAnswer, _ := aggregate["final_answer"].(string)
	for _, want := range []string{"已开票未回款 52094.16 元", "百度在线网络技术(北京)有限公司", "未回款 41500.00 元"} {
		if !strings.Contains(finalAnswer, want) {
			t.Fatalf("aggregate final_answer should include %q, got %s", want, finalAnswer)
		}
	}

	continuity := server.bridgeEnvelope("finance-query", "query", map[string]any{
		"success": true,
		"message": "合同口径当前不能直接回答。",
		"data": map[string]any{
			"contract_answer_status":   "missing",
			"contract_source_required": true,
			"source_priority":          "contract_strict",
			"contract_fallback_reason": "合同信息表没有匹配到该主体/项目",
			"contract_continuity_candidates": []map[string]any{
				{"candidate_received_amount": 100.25},
				{"candidate_received_amount": 200.25},
			},
		},
	})
	finalAnswer, _ = continuity["final_answer"].(string)
	for _, want := range []string{"300.50", "疑似", "推断", "不是固定映射表"} {
		if !strings.Contains(finalAnswer, want) {
			t.Fatalf("continuity final_answer should include %q, got %s", want, finalAnswer)
		}
	}
}

func responseByID(t *testing.T, output string, id float64) map[string]any {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatalf("unmarshal response line: %v; line=%s", err, scanner.Text())
		}
		if message["id"] == id {
			return message
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	t.Fatalf("response id %v not found in output:\n%s", id, output)
	return nil
}

func resourceReadText(t *testing.T, response map[string]any) string {
	t.Helper()

	result := response["result"].(map[string]any)
	contents := result["contents"].([]any)
	first := contents[0].(map[string]any)
	return first["text"].(string)
}

func toolPayloadByID(t *testing.T, output string, id float64) map[string]any {
	t.Helper()

	response := responseByID(t, output, id)
	result := response["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal tool payload: %v; text=%s", err, text)
	}
	return payload
}

func assertJSONRPCError(t *testing.T, output string, id int, code int, message string) {
	t.Helper()

	response := responseByID(t, output, float64(id))
	errorObject := response["error"].(map[string]any)
	if got := int(errorObject["code"].(float64)); got != code {
		t.Fatalf("response %d code = %d, want %d", id, got, code)
	}
	if got := errorObject["message"].(string); got != message {
		t.Fatalf("response %d message = %q, want %q", id, got, message)
	}
}

type recordingToolRunner struct {
	name   string
	args   map[string]any
	result ToolRunResult
	err    error
}

func (r *recordingToolRunner) RunTool(ctx context.Context, name string, args map[string]any) (ToolRunResult, error) {
	r.name = name
	r.args = args
	if r.err != nil {
		return ToolRunResult{}, r.err
	}
	return r.result, nil
}
