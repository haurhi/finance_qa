package query

import "strings"

type sourceAttributionPlan struct {
	tables               []string
	primaryBaseTables    []string
	supportingBaseTables []string
}

func resolveSourceAttributionPlan(spec QuerySpec, data map[string]any) sourceAttributionPlan {
	plan := sourceAttributionPlan{
		tables: resolveSourceAttributionTables(spec, data),
	}
	plan.primaryBaseTables, plan.supportingBaseTables = resolvePrimaryAndSupportingBaseTables(spec, data)
	plan.tables = dedupeSourceTables(plan.tables...)
	plan.primaryBaseTables = dedupeSourceTables(plan.primaryBaseTables...)
	plan.supportingBaseTables = stripPrimaryBaseTables(plan.supportingBaseTables, plan.primaryBaseTables)
	return plan
}

func resolveSourceAttributionTables(spec QuerySpec, data map[string]any) []string {
	if tables := anySourceStringSlice(data["source_tables"]); len(tables) > 0 {
		return dedupeSourceTables(tables...)
	}
	primaryOverride := anySourceStringSlice(data["source_primary_tables"])
	supportingOverride := anySourceStringSlice(data["source_supporting_tables"])
	if len(primaryOverride) > 0 || len(supportingOverride) > 0 {
		return dedupeSourceTables(append(primaryOverride, supportingOverride...)...)
	}

	switch spec.QueryFamily {
	case QueryFamilyContractDimension:
		return dedupeSourceTables(contractSourceTablesFromData(data)...)
	case QueryFamilyCoreMetric:
		if strings.TrimSpace(anyToString(data["source_priority"])) == "contract_first" {
			return dedupeSourceTables(append([]string{"fin_contracts"}, contractAggregateTablesForRequestedMetrics(spec, data)...)...)
		}
		accrualSource := detectAccrualSource(data)
		tables := make([]string, 0, 2)
		if strings.Contains(accrualSource, "journal") {
			tables = append(tables, "fin_journal")
		} else {
			tables = append(tables, "fin_income_statement")
		}
		if hasCashPerspective(data) {
			tables = append(tables, "fin_bank_statement")
		}
		return dedupeSourceTables(tables...)
	case QueryFamilySupplierPayments:
		return []string{"fin_bank_statement"}
	case QueryFamilyHRCost:
		return []string{"fin_journal", "fin_bank_statement"}
	case QueryFamilyARAP:
		source := strings.TrimSpace(anyToString(data["source"]))
		if strings.Contains(source, "journal") {
			return []string{"fin_journal", "fin_balance_detail"}
		}
		return []string{"fin_balance_detail"}
	case QueryFamilyReconciliation:
		return sourceTablesForReconciliation(anyToString(data["book_source"]))
	case QueryFamilyCounterparty:
		return sourceTablesForCounterparty(spec.NormalizedQuestion)
	default:
		return nil
	}
}

func resolvePrimaryAndSupportingBaseTables(spec QuerySpec, data map[string]any) ([]string, []string) {
	if primaryOverride := normalizeBaseSourceTables(anySourceStringSlice(data["source_primary_tables"])); len(primaryOverride) > 0 {
		return primaryOverride, normalizeBaseSourceTables(anySourceStringSlice(data["source_supporting_tables"]))
	}
	switch spec.QueryFamily {
	case QueryFamilyContractDimension:
		role := strings.TrimSpace(anyToString(data["role"]))
		askedTopic := strings.TrimSpace(anyToString(data["asked_topic"]))
		switch askedTopic {
		case "content":
			return []string{"fin_contracts"}, nil
		case "revenue", "receipts":
			return []string{"fin_fund_income"}, []string{"fin_contracts"}
		case "cost", "payments":
			return []string{"fin_cost_settlements"}, []string{"fin_contracts", "fin_bank_statement"}
		case "profit":
			if role == "mixed_contract" {
				return []string{"fin_fund_income", "fin_cost_settlements"}, []string{"fin_contracts", "fin_bank_statement"}
			}
			if role == "customer_contract" {
				return []string{"fin_fund_income"}, []string{"fin_contracts"}
			}
			return []string{"fin_cost_settlements"}, []string{"fin_contracts", "fin_bank_statement"}
		default:
			return nil, nil
		}
	case QueryFamilyCoreMetric:
		if strings.TrimSpace(anyToString(data["source_priority"])) == "contract_first" {
			supporting := []string{"fin_contracts"}
			if contractAggregateHasNetProfitContext(data) {
				supporting = append(supporting, "fin_income_statement")
			}
			return contractAggregateTablesForRequestedMetrics(spec, data), supporting
		}
		accrualSource := detectAccrualSource(data)
		if strings.Contains(accrualSource, "journal") {
			return []string{"fin_journal"}, []string{"fin_bank_statement"}
		}
		return []string{"fin_income_statement"}, []string{"fin_bank_statement"}
	case QueryFamilyARAP:
		if strings.Contains(strings.TrimSpace(anyToString(data["source"])), "journal") {
			return []string{"fin_journal"}, []string{"fin_balance_detail"}
		}
		return []string{"fin_balance_detail"}, nil
	case QueryFamilyReconciliation:
		if strings.Contains(strings.TrimSpace(anyToString(data["book_source"])), "journal") {
			return []string{"fin_journal", "fin_bank_statement"}, nil
		}
		return []string{"fin_income_statement", "fin_bank_statement"}, []string{"fin_journal"}
	case QueryFamilySupplierPayments:
		return []string{"fin_bank_statement"}, nil
	case QueryFamilyHRCost:
		return []string{"fin_journal"}, []string{"fin_bank_statement"}
	case QueryFamilyCounterparty:
		tables := sourceTablesForCounterparty(spec.NormalizedQuestion)
		if len(tables) == 0 {
			return nil, nil
		}
		primary := []string{baseSourceTableName(tables[0])}
		supporting := make([]string, 0, len(tables)-1)
		for _, tableName := range tables[1:] {
			supporting = append(supporting, baseSourceTableName(tableName))
		}
		return primary, supporting
	default:
		return nil, nil
	}
}

func contractAggregateHasNetProfitContext(data map[string]any) bool {
	summary, ok := data["contract_summary"].(map[string]any)
	if !ok || summary == nil {
		return false
	}
	_, ok = summary["net_profit_context"]
	return ok
}

func normalizeBaseSourceTables(tables []string) []string {
	if len(tables) == 0 {
		return nil
	}
	out := make([]string, 0, len(tables))
	for _, tableName := range tables {
		base := strings.TrimSpace(baseSourceTableName(tableName))
		if base == "" {
			continue
		}
		out = append(out, base)
	}
	return dedupeSourceTables(out...)
}

func stripPrimaryBaseTables(items, primary []string) []string {
	if len(items) == 0 {
		return nil
	}
	primarySet := map[string]struct{}{}
	for _, item := range primary {
		primarySet[strings.TrimSpace(item)] = struct{}{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := primarySet[trimmed]; ok {
			continue
		}
		out = append(out, trimmed)
	}
	return dedupeSourceTables(out...)
}
