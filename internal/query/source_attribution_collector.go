package query

import "strings"

func (e *Engine) collectSourceTables(spec QuerySpec, data map[string]any) []string {
	return resolveSourceAttributionPlan(spec, data).tables
}

func contractSourceTablesFromData(data map[string]any) []string {
	role := strings.TrimSpace(anyToString(data["role"]))
	askedTopic := strings.TrimSpace(anyToString(data["asked_topic"]))
	return contractSourceTablesForRoleAndTopicWithConfig(role, askedTopic, getRuleConfig())
}

func contractSourceTablesForRoleAndTopic(role, askedTopic string) []string {
	return contractSourceTablesForRoleAndTopicWithConfig(role, askedTopic, getRuleConfig())
}

func contractSourceTablesForRoleAndTopicWithConfig(role, askedTopic string, cfg RuleConfig) []string {
	configured := cfg.ContractSourceTables(role)
	allowedBases := contractSourceBaseTablesForRoleAndTopic(role, askedTopic)
	if len(allowedBases) == 0 {
		return configured
	}
	filtered := filterSourceTablesByBase(configured, allowedBases)
	if len(filtered) > 0 {
		return filtered
	}
	return allowedBases
}

func contractSourceBaseTablesForRoleAndTopic(role, askedTopic string) []string {
	switch askedTopic {
	case "content":
		return []string{"fin_contracts"}
	case "revenue", "receipts":
		return []string{"fin_contracts", "fin_fund_income", "fin_fund_income_groups", "fin_fund_income_group_members"}
	case "cost", "payments", "payable", "invoice":
		if role == "supplier_contract" || role == "mixed_contract" {
			return []string{"fin_contracts", "fin_cost_settlements", "fin_cost_settlement_groups", "fin_cost_settlement_group_members", "fin_bank_statement"}
		}
		return []string{"fin_contracts", "fin_cost_settlements", "fin_cost_settlement_groups", "fin_cost_settlement_group_members"}
	case "profit":
		if role == "mixed_contract" {
			return []string{
				"fin_contracts",
				"fin_fund_income", "fin_fund_income_groups", "fin_fund_income_group_members",
				"fin_cost_settlements", "fin_cost_settlement_groups", "fin_cost_settlement_group_members",
				"fin_bank_statement",
			}
		}
		if role == "supplier_contract" {
			return []string{"fin_contracts", "fin_cost_settlements", "fin_cost_settlement_groups", "fin_cost_settlement_group_members", "fin_bank_statement"}
		}
		return []string{"fin_contracts", "fin_fund_income", "fin_fund_income_groups", "fin_fund_income_group_members"}
	default:
		return nil
	}
}

func filterSourceTablesByBase(tables, allowedBases []string) []string {
	if len(tables) == 0 || len(allowedBases) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, tableName := range allowedBases {
		base := strings.TrimSpace(baseSourceTableName(tableName))
		if base == "" {
			continue
		}
		allowed[base] = struct{}{}
	}
	out := make([]string, 0, len(tables))
	for _, tableName := range tables {
		base := strings.TrimSpace(baseSourceTableName(tableName))
		if _, ok := allowed[base]; ok {
			out = append(out, tableName)
		}
	}
	return dedupeSourceTables(out...)
}

func contractAggregateTablesForMetric(metric string) []string {
	switch strings.TrimSpace(metric) {
	case "成本":
		return []string{"fin_cost_settlements"}
	case "利润":
		return []string{"fin_fund_income", "fin_cost_settlements"}
	default:
		return []string{"fin_fund_income"}
	}
}

func contractAggregateTablesForRequestedMetrics(spec QuerySpec, data map[string]any) []string {
	requested := anySourceStringSlice(data["requested_metrics"])
	if len(requested) == 0 {
		return contractAggregateTablesForMetric(detectSourceMetric(spec, data))
	}
	tables := make([]string, 0, 2)
	if contractAggregateNeedsRevenueData(requested) {
		tables = append(tables, "fin_fund_income")
	}
	if contractAggregateNeedsCostData(requested) {
		tables = append(tables, "fin_cost_settlements")
	}
	if len(tables) == 0 {
		return contractAggregateTablesForMetric(detectSourceMetric(spec, data))
	}
	return dedupeSourceTables(tables...)
}

func detectSourceMetric(spec QuerySpec, data map[string]any) string {
	if metric := strings.TrimSpace(anyToString(data["metric"])); metric != "" && metric != "核心指标" {
		return metric
	}
	switch spec.MetricKind {
	case MetricKindCost:
		return "成本"
	case MetricKindProfit:
		return "利润"
	default:
		return "收入"
	}
}

func detectAccrualSource(data map[string]any) string {
	if monthly, ok := data["monthly"].(map[string]any); ok {
		if source := strings.TrimSpace(anyToString(monthly["source"])); source != "" {
			return source
		}
	}
	if summary, ok := data["range_summary"].(map[string]any); ok {
		if source := strings.TrimSpace(anyToString(summary["source"])); source != "" {
			return source
		}
	}
	return strings.TrimSpace(anyToString(data["source"]))
}

func hasCashPerspective(data map[string]any) bool {
	if _, ok := data["money_view"]; ok {
		return true
	}
	if _, ok := data["cash_view"]; ok {
		return true
	}
	if _, ok := data["cash_flow"]; ok {
		return true
	}
	return false
}

func anySourceStringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return dedupeSourceTables(typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(anyToString(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return dedupeSourceTables(out...)
	default:
		return nil
	}
}
