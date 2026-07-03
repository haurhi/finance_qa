package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var skillVersionPattern = regexp.MustCompile("`?([a-z_]*version)`?\\s*:\\s*`?([^`\\n]+)`?")

func (s *Server) bridgeMeta(toolName, operation string, payload map[string]any) map[string]any {
	skillVersion, protocolVersion := readContractVersions(s.skillPath)
	if protocolVersion == "" {
		protocolVersion = "v2"
	}
	appendixPath, appendixExists := resolvedPathState(s.appendixPath)

	meta := map[string]any{
		"tool_name":                    toolName,
		"tool_operation":               operation,
		"db":                           redactDBTarget(s.dbPath),
		"skill_contract_version":       skillVersion,
		"protocol_version":             protocolVersion,
		"skill_appendix_relative_path": appendixRelativePath(s.skillPath, s.appendixPath),
		"skill_appendix_path":          appendixPath,
		"skill_appendix_exists":        appendixExists,
		"capabilities":                 bridgeCapabilities(payload),
	}
	if finalAnswer := firstString(payload["final_answer"], payload["boss_reply_text"]); finalAnswer != "" {
		meta["final_answer_available"] = true
		if payload["boss_reply"] != nil {
			meta["final_answer_source"] = "boss_reply"
		} else if firstString(payload["boss_reply_text"]) != "" {
			meta["final_answer_source"] = "boss_reply_text"
		} else {
			meta["final_answer_source"] = "message"
		}
	}
	return meta
}

func readContractVersions(skillPath string) (string, string) {
	if strings.TrimSpace(skillPath) == "" {
		return "", ""
	}
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return "", ""
	}
	versions := map[string]string{}
	for _, match := range skillVersionPattern.FindAllStringSubmatch(string(raw), -1) {
		if len(match) == 3 {
			versions[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
		}
	}
	return versions["skill_contract_version"], versions["bridge_protocol_version"]
}

func bridgeCapabilities(payload map[string]any) map[string]any {
	capabilities := map[string]any{
		"boss_reply":               true,
		"finance_facts":            true,
		"final_answer":             true,
		"contract_summary":         true,
		"route_decision":           true,
		"tax_disclosure":           true,
		"supplier_payment_summary": true,
		"result_structures": []any{
			"finance_facts",
			"boss_reply",
			"host_summary_contract",
			"host_summary_supplier_payments",
			"route_decision",
		},
		"exposed_tools": []any{
			"finance-query",
			"finance-host-data",
			"finance-upload",
			"finance-sync",
			"finance-dimensions",
		},
		"exposed_fields": []any{
			"dual_perspective",
			"hr_breakdown",
			"arithmetic_checks",
			"intent_trace",
			"source_cell_notes",
			"remarks",
			"tax_inclusion",
			"tax_inclusion_note",
		},
	}
	return capabilities
}

func redactDBTarget(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return ""
	}
	if strings.Contains(dbPath, "host=") || strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
		schema := postgresSchema(dbPath)
		if schema == "" {
			return "postgresql"
		}
		return "postgresql(schema=" + schema + ")"
	}
	return "sqlite(local)"
}

func postgresSchema(dbPath string) string {
	if idx := strings.Index(dbPath, "search_path="); idx >= 0 {
		value := dbPath[idx+len("search_path="):]
		if end := strings.IndexAny(value, " ?&"); end >= 0 {
			value = value[:end]
		}
		if comma := strings.Index(value, ","); comma >= 0 {
			value = value[:comma]
		}
		return strings.Trim(value, "'\" ")
	}
	if idx := strings.Index(dbPath, "search_path%3D"); idx >= 0 {
		value := dbPath[idx+len("search_path%3D"):]
		if end := strings.IndexAny(value, "& "); end >= 0 {
			value = value[:end]
		}
		if comma := strings.Index(value, "%2C"); comma >= 0 {
			value = value[:comma]
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func resolvedPathState(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, false
	}
	return resolved, true
}

func appendixRelativePath(skillPath, appendixPath string) string {
	if strings.TrimSpace(skillPath) == "" || strings.TrimSpace(appendixPath) == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Dir(skillPath), appendixPath)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
