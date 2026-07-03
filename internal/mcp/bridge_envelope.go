package mcp

import (
	"encoding/json"

	"financeqa/internal/query"
)

func (s *Server) bridgeEnvelope(toolName, operation string, result any) map[string]any {
	payload := payloadMap(result)
	if _, ok := payload["success"]; !ok {
		payload["success"] = true
	}
	if _, ok := payload["answer_method"]; !ok {
		payload["answer_method"] = "mcp_json"
	}
	payload["tool_name"] = toolName

	if data := mapValue(payload["data"]); data != nil {
		addTraceData(payload, data)
		addExposedFields(data)
		addHostSummaries(payload, data)
		if bossReply := buildBossReply(payload, data); bossReply != nil {
			payload["boss_reply"] = bossReply
			payload["final_answer"] = formatBossReply(bossReply)
		}
		if finalAnswer := firstString(data["final_answer"], data["boss_reply_text"], payload["final_answer"], payload["message"]); finalAnswer != "" {
			payload["final_answer"] = appendSourceNotes(finalAnswer, data)
		}
		if hostSummary := data["host_summary_contract"]; hostSummary != nil {
			payload["host_summary_contract"] = hostSummary
		}
		if supplierSummary := data["host_summary_supplier_payments"]; supplierSummary != nil {
			payload["host_summary_supplier_payments"] = supplierSummary
		}
		if financeFacts := data["finance_facts"]; financeFacts != nil {
			payload["finance_facts"] = financeFacts
		}
	} else if finalAnswer := firstString(payload["final_answer"], payload["boss_reply_text"], payload["message"]); finalAnswer != "" {
		payload["final_answer"] = finalAnswer
	}

	if _, ok := payload["boss_reply"]; !ok {
		if finalAnswer := firstString(payload["final_answer"], payload["message"]); finalAnswer != "" {
			payload["boss_reply"] = map[string]any{"结论": finalAnswer}
		}
	}

	payload["bridge_meta"] = s.bridgeMeta(toolName, operation, payload)
	sanitized := sanitizePayloadMap(payload)
	restoreFallbackTraceLogs(sanitized, payload)
	return sanitized
}

func addHostSummaries(payload, data map[string]any) {
	if data["host_summary_contract"] == nil {
		if summary := query.BuildHostSummaryContract(data, firstString(payload["query"])); summary != nil {
			delete(summary, "source_tables")
			restoreHumanSourceNotes(summary, data)
			data["host_summary_contract"] = summary
			payload["host_summary_contract"] = summary
		}
	}
	if data["host_summary_supplier_payments"] == nil {
		if summary := buildSupplierPaymentSummary(data); summary != nil {
			data["host_summary_supplier_payments"] = summary
			payload["host_summary_supplier_payments"] = summary
		}
	}
}

func addTraceData(payload, data map[string]any) {
	if data["trace"] != nil {
		return
	}
	executedSQL := payload["executed_sql"]
	calculationLogs := payload["calculation_logs"]
	if executedSQL == nil && calculationLogs == nil {
		return
	}
	trace := map[string]any{
		"answer_method": firstString(payload["answer_method"]),
	}
	if executedSQL != nil {
		trace["executed_sql"] = executedSQL
		data["executed_sql"] = executedSQL
	}
	if calculationLogs != nil {
		trace["calculation_logs"] = calculationLogs
		data["calculation_logs"] = calculationLogs
	}
	data["trace"] = trace
	data["process"] = trace
	if data["answer_method"] == nil {
		data["answer_method"] = payload["answer_method"]
	}
}

func payloadMap(result any) map[string]any {
	if m, ok := result.(map[string]any); ok {
		return cloneMap(m)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return map[string]any{"success": false, "message": err.Error(), "answer_method": "mcp_json"}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err == nil && out != nil {
		return out
	}
	return map[string]any{"success": true, "data": result}
}

func addExposedFields(data map[string]any) {
	exposed := mapValue(data["exposed_fields"])
	if exposed == nil {
		exposed = map[string]any{}
	}
	for _, key := range []string{
		"dual_perspective",
		"hr_breakdown",
		"arithmetic_checks",
		"intent_trace",
		"source_cell_notes",
		"remarks",
		"tax_inclusion",
		"tax_inclusion_note",
	} {
		if value, ok := data[key]; ok {
			exposed[key] = value
		}
	}
	if len(exposed) > 0 {
		data["exposed_fields"] = exposed
	}
}
