package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildHostSummaryContract 构建宿主摘要结构
// 对应 Python bridge 的 build_host_summary_contract，保持字段兼容
func BuildHostSummaryContract(data map[string]any, query string) map[string]any {
	if data == nil {
		return nil
	}

	// 检查 contract_strict_missing 状态
	if isContractStrictMissing(data) {
		return buildContractStrictMissingSummary(data)
	}

	querySpec := getMap(data, "query_spec")
	queryFamily := getString(querySpec, "query_family")
	askedTopic := getString(data, "asked_topic")
	sourcePriority := getString(data, "source_priority")

	// 调试：打印关键值
	_ = fmt.Sprintf("%s%s%s", queryFamily, askedTopic, sourcePriority)

	// contract_dimension 且满足条件
	if queryFamily == "contract_dimension" && (askedTopic != "" || sourcePriority == "contract_first" || sourcePriority == "contract_strict") {
		return buildContractDimensionSummary(data, querySpec)
	}

	// contract_aggregate 且满足条件 - 支持 core_metric 和 arap_dimension
	if (queryFamily == "core_metric" || queryFamily == "arap_dimension") && sourcePriority == "contract_first" && data["contract_summary"] != nil {
		return buildContractAggregateSummary(data, querySpec)
	}

	// counterparty_receipts_with_subperiod（回款子期间）
	totalAmount := firstFloat(data["amount"], data["total"], data["bank_in"])
	subPeriod := getString(data, "sub_period")
	subPeriodAmount := firstFloat(data["sub_period_receipts"])

	if isReceiptQuestion(query) && totalAmount != nil && subPeriod != "" && subPeriodAmount != nil {
		return buildCounterpartyReceiptsSummary(data, totalAmount, subPeriodAmount, subPeriod)
	}

	return nil
}

// BuildFinalAnswer 构建 final_answer 字段
// 优先使用 message，如有 boss_reply_text 则使用
func BuildFinalAnswer(data map[string]any, message string) string {
	text := ""
	// 如果 Data 里已有 boss_reply_text，直接用它
	if text := getString(data, "boss_reply_text"); text != "" {
		return appendFinalAnswerSourceNotes(text, data)
	}
	// 否则使用 Message
	text = message
	return appendFinalAnswerSourceNotes(text, data)
}

func appendFinalAnswerSourceNotes(text string, data map[string]any) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	for _, note := range []string{
		firstNonEmptyString(getString(data, "source_note"), getString(data, "source_summary")),
		getString(data, "source_update_note"),
	} {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		if strings.Contains(text, note) {
			continue
		}
		text += "\n" + note
	}
	return text
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// isContractStrictMissing 检查是否是合同严格缺失状态
func isContractStrictMissing(data map[string]any) bool {
	status := getString(data, "contract_answer_status")
	sourcePriority := getString(data, "source_priority")

	if status == "missing" {
		return true
	}

	if sourcePriority == "contract_strict" && getBool(data, "contract_source_required") {
		return true
	}

	return false
}

// buildContractStrictMissingSummary 构建严格缺失摘要
func buildContractStrictMissingSummary(data map[string]any) map[string]any {
	sourceTables := getStringSlice(data, "source_tables")
	if len(sourceTables) == 0 {
		sourceTables = getStringSlice(data, "source_primary_tables")
	}

	sourceDocuments := cleanSourceDocuments(getStringSlice(data, "source_documents"))

	reason := getString(data, "contract_fallback_reason")
	if reason == "" {
		reason = getString(data, "message")
	}
	if reason == "" {
		reason = "项目台账在请求期间没有足够记录"
	}

	sourceNote := cleanSourceNote(getString(data, "source_note"))
	if sourceNote == "" {
		sourceNote = cleanSourceNote(getString(data, "source_summary"))
	}
	if sourceNote == "" && len(sourceDocuments) > 0 {
		sourceNote = "本次尝试的项目口径来源：" + strings.Join(sourceDocuments, "、")
	}

	return map[string]any{
		"kind":                  "contract_strict_missing",
		"period":                data["period"],
		"entity":                getString(data, "entity"),
		"reason":                reason,
		"source_note":           sourceNote,
		"source_documents":      sourceDocuments,
		"continuity_candidates": getMapSlice(data, "contract_continuity_candidates"),
		"continuity_note":       getString(data, "contract_continuity_note"),
		"source_tables":         sourceTables,
		"safe_to_quote_message": true,
	}
}

// buildContractDimensionSummary 构建合同维度摘要
func buildContractDimensionSummary(data map[string]any, querySpec map[string]any) map[string]any {
	entity := getString(data, "entity")
	if entity == "" {
		entity = getString(data, "counterparty")
		if entity == "" {
			entity = getString(data, "name")
		}
	}

	routeDecision := getMap(data, "route_decision")
	if routeDecision == nil && querySpec != nil {
		routeDecision = getMap(querySpec, "route_decision")
	}

	contract := map[string]any{
		"kind":                     "contract_dimension",
		"entity":                   strings.TrimSpace(entity),
		"role":                     data["role"],
		"period":                   data["period"],
		"asked_topic":              getString(data, "asked_topic"),
		"contracts":                getSlice(data, "contracts"),
		"cash_view":                firstNonNil(data["cash_view"], data["money_view"]),
		"book_view":                firstNonNil(data["book_view"], data["account_view"]),
		"source_tables":            getStringSlice(data, "source_tables"),
		"source_summary":           cleanSourceNote(getString(data, "source_summary")),
		"source_note":              cleanSourceNote(getString(data, "source_note")),
		"source_documents":         cleanSourceDocuments(getStringSlice(data, "source_documents")),
		"route_decision":           routeDecision,
		"contract_fallback_reason": getString(data, "contract_fallback_reason"),
		"safe_to_quote_message":    true,
	}

	// 可选字段
	if subPeriod, ok := data["sub_period"].(string); ok && subPeriod != "" {
		contract["sub_period"] = subPeriod
	}
	if subPeriodReceipts, ok := data["sub_period_receipts"]; ok {
		contract["sub_period_receipts"] = subPeriodReceipts
	}

	return contract
}

// buildContractAggregateSummary 构建合同汇总摘要
func buildContractAggregateSummary(data map[string]any, querySpec map[string]any) map[string]any {
	contractSummary := getMap(data, "contract_summary")
	if contractSummary == nil {
		return nil
	}

	entity := getString(contractSummary, "entity")
	if entity == "" {
		entity = getString(data, "entity")
	}

	routeDecision := getMap(data, "route_decision")
	if routeDecision == nil && querySpec != nil {
		routeDecision = getMap(querySpec, "route_decision")
	}

	return map[string]any{
		"kind":                     "contract_aggregate",
		"entity":                   strings.TrimSpace(entity),
		"period":                   data["period"],
		"metric":                   data["metric"],
		"requested_metrics":        getStringSlice(data, "requested_metrics"),
		"cash_view":                firstNonNil(data["money_view"], data["cash_view"]),
		"book_view":                firstNonNil(data["account_view"], data["book_view"]),
		"contract_summary":         contractSummary,
		"source_tables":            getStringSlice(data, "source_tables"),
		"source_summary":           cleanSourceNote(getString(data, "source_summary")),
		"source_note":              cleanSourceNote(getString(data, "source_note")),
		"source_documents":         cleanSourceDocuments(getStringSlice(data, "source_documents")),
		"route_decision":           routeDecision,
		"contract_fallback_reason": getString(data, "contract_fallback_reason"),
		"safe_to_quote_message":    true,
	}
}

// buildCounterpartyReceiptsSummary 构建对手方回款摘要
func buildCounterpartyReceiptsSummary(data map[string]any, totalAmount *float64, subPeriodAmount *float64, subPeriod string) map[string]any {
	entity := getString(data, "entity")
	if entity == "" {
		entity = getString(data, "counterparty")
		if entity == "" {
			entity = getString(data, "name")
		}
	}

	contract := map[string]any{
		"kind":                  "counterparty_receipts_with_subperiod",
		"total_amount":          *totalAmount,
		"sub_period":            subPeriod,
		"sub_period_amount":     *subPeriodAmount,
		"safe_to_quote_message": true,
	}

	if entity := strings.TrimSpace(entity); entity != "" {
		contract["entity"] = entity
	}
	if role := getString(data, "role"); role != "" {
		contract["role"] = role
	}
	if comparison := getString(data, "comparison_basis"); comparison != "" {
		contract["comparison_basis"] = comparison
	}

	return contract
}

// helper 函数

func getMap(data map[string]any, key string) map[string]any {
	if data == nil {
		return nil
	}
	v, ok := data[key].(map[string]any)
	if ok {
		return v
	}
	// 兼容 struct 类型：通过 JSON 序列化/反序列化转换
	if data[key] == nil {
		return nil
	}
	b, err := json.Marshal(data[key])
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

func getMapSlice(data map[string]any, key string) []map[string]any {
	if data == nil {
		return nil
	}
	v, ok := data[key].([]map[string]any)
	if ok {
		return v
	}
	// 尝试通用 slice 转换
	if rawSlice, ok := data[key].([]any); ok {
		result := make([]map[string]any, 0, len(rawSlice))
		for _, item := range rawSlice {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

func getSlice(data map[string]any, key string) []any {
	if data == nil {
		return nil
	}
	v, ok := data[key].([]any)
	if ok {
		return v
	}
	return nil
}

func getString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key].(string)
	if ok {
		return v
	}
	// 处理 fmt.Stringer 接口类型（如 QueryFamily, MetricKind 等自定义类型）
	if s, ok := data[key].(interface{ String() string }); ok {
		return s.String()
	}
	// 兜底：使用 fmt.Sprint 转换
	if data[key] != nil {
		return fmt.Sprint(data[key])
	}
	return ""
}

func getStringSlice(data map[string]any, key string) []string {
	if data == nil {
		return nil
	}
	v, ok := data[key].([]string)
	if ok {
		return v
	}
	// 尝试通用 slice 转换
	if rawSlice, ok := data[key].([]any); ok {
		result := make([]string, 0, len(rawSlice))
		for _, item := range rawSlice {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func getBool(data map[string]any, key string) bool {
	if data == nil {
		return false
	}
	v, ok := data[key].(bool)
	return ok && v
}

func firstNonNil(v1, v2 any) any {
	if v1 != nil {
		return v1
	}
	return v2
}

// cleanSourceNote 清理来源注释
func cleanSourceNote(value string) string {
	s := strings.TrimSpace(value)
	// 去掉常见前缀
	s = strings.TrimPrefix(s, "来源：")
	s = strings.TrimSpace(s)
	return s
}

// cleanSourceDocuments 清理来源文档列表
func cleanSourceDocuments(docs []string) []string {
	if docs == nil {
		return nil
	}
	result := make([]string, 0, len(docs))
	seen := make(map[string]bool)
	for _, doc := range docs {
		s := strings.TrimSpace(doc)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		result = append(result, s)
	}
	return result
}

// firstFloat 从多个值中取第一个有效 float64
func firstFloat(values ...any) *float64 {
	for _, v := range values {
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case float64:
			return &val
		case float32:
			f := float64(val)
			return &f
		case int:
			f := float64(val)
			return &f
		case int64:
			f := float64(val)
			return &f
		}
	}
	return nil
}

// isReceiptQuestion 判断是否是回款问题
func isReceiptQuestion(query string) bool {
	text := strings.TrimSpace(query)
	if text == "" {
		return false
	}
	keywords := []string{"回款", "到账", "收款"}
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
