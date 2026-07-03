package mcp

import (
	"regexp"
	"strings"
)

var technicalTablePattern = regexp.MustCompile(`tenant_[A-Za-z0-9_]+\.[A-Za-z0-9_]+`)
var contractCodePattern = regexp.MustCompile(`\bC[0-9]{3,}\b`)
var storageURLPattern = regexp.MustCompile(`\b(?:s3|oss|cos|gs)://[^\s"']+`)

var internalPayloadKeys = map[string]bool{
	"account_code":       true,
	"calculation_logs":   true,
	"contract_id":        true,
	"executed_sql":       true,
	"file_hash":          true,
	"id":                 true,
	"job_id":             true,
	"page_id":            true,
	"primary_tables":     true,
	"raw_ocr_json":       true,
	"source_report_type": true,
	"source_sheet_name":  true,
	"source_tables":      true,
	"storage_key":        true,
}

func sanitizePayloadMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if internalPayloadKeys[key] {
			continue
		}
		if key == "finance_facts" {
			if facts := mapValue(value); facts != nil {
				if clean := sanitizeFinanceFactsMap(facts); len(clean) > 0 {
					out[key] = clean
				}
				continue
			}
		}
		clean, keep := sanitizePayloadValue(value)
		if keep {
			out[key] = clean
		}
	}
	return out
}

func sanitizeFinanceFactsMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if key == "source_tables" {
			tables := []any{}
			for _, table := range stringSlice(value) {
				table = safeFinanceFactSourceTable(table)
				if table != "" {
					tables = append(tables, table)
				}
			}
			if len(tables) > 0 {
				out[key] = tables
			}
			continue
		}
		if internalPayloadKeys[key] {
			continue
		}
		clean, keep := sanitizePayloadValue(value)
		if keep {
			out[key] = clean
		}
	}
	return out
}

func safeFinanceFactSourceTable(value string) string {
	table := strings.TrimSpace(value)
	if idx := strings.LastIndex(table, "."); idx >= 0 {
		table = strings.TrimSpace(table[idx+1:])
	}
	return sanitizePayloadString(table)
}

func sanitizePayloadValue(value any) (any, bool) {
	switch v := value.(type) {
	case map[string]any:
		out := sanitizePayloadMap(v)
		return out, len(out) > 0
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			clean, keep := sanitizePayloadValue(item)
			if keep {
				out = append(out, clean)
			}
		}
		return out, len(out) > 0
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			clean := sanitizePayloadString(item)
			if clean != "" {
				out = append(out, clean)
			}
		}
		return out, len(out) > 0
	case []map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			clean := sanitizePayloadMap(item)
			if len(clean) > 0 {
				out = append(out, clean)
			}
		}
		return out, len(out) > 0
	case string:
		clean := sanitizePayloadString(v)
		return clean, clean != ""
	default:
		return value, value != nil
	}
}

func sanitizePayloadString(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	clean = storageURLPattern.ReplaceAllString(clean, "")
	clean = technicalTablePattern.ReplaceAllString(clean, "")
	clean = contractCodePattern.ReplaceAllString(clean, "")
	for _, token := range []string{
		"contract_id",
		"account_code",
		"source_report_type",
		"source_sheet_name",
		"executed_sql",
		"storage_key",
		"file_hash",
		"job_id",
		"raw_ocr_json",
		"page_id",
	} {
		clean = strings.ReplaceAll(clean, token, "")
	}
	clean = strings.Join(strings.Fields(clean), " ")
	clean = strings.Trim(clean, " +,，;；:：")
	if strings.HasPrefix(strings.ToUpper(clean), "SELECT ") || strings.Contains(strings.ToUpper(clean), " FROM ") {
		return ""
	}
	return clean
}

func restoreFallbackTraceLogs(sanitized, original map[string]any) {
	if original["success"] != false {
		return
	}
	originalData := mapValue(original["data"])
	if originalData == nil {
		return
	}
	originalTrace := mapValue(originalData["trace"])
	if originalTrace == nil {
		return
	}
	logs, ok := sanitizePayloadValue(originalTrace["calculation_logs"])
	if !ok {
		return
	}
	sanitizedData := mapValue(sanitized["data"])
	if sanitizedData == nil {
		sanitizedData = map[string]any{}
		sanitized["data"] = sanitizedData
	}
	trace := mapValue(sanitizedData["trace"])
	if trace == nil {
		trace = map[string]any{}
		sanitizedData["trace"] = trace
	}
	trace["calculation_logs"] = logs
}
