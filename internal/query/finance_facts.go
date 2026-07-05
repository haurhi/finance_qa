package query

import (
	"fmt"
	"regexp"
	"strings"
)

var financeFactSourceDocPattern = regexp.MustCompile(`《[^》]+》(?:的【[^】]+】)?`)

func buildFinanceFacts(spec QuerySpec, data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	facts := map[string]any{
		"schema_version": "finance_facts.v1",
	}
	if existing, ok := data["finance_facts"].(map[string]any); ok {
		for key, value := range existing {
			facts[key] = value
		}
	}

	resolvedPeriod := financeFactResolvedPeriod(spec, data)
	if resolvedPeriod != "" {
		facts["resolved_period"] = resolvedPeriod
	}
	if requestedPeriod := firstFinanceFactString(data["requested_period"], data["requested_period_label"]); requestedPeriod != "" {
		facts["requested_period"] = requestedPeriod
	} else if period := displayPeriod(spec.PeriodFrom, spec.PeriodTo); period != "" {
		facts["requested_period"] = period
	}
	if basis := financeFactBasis(spec, data); basis != "" {
		facts["basis"] = basis
	}
	if tables := financeFactSourceTables(data); len(tables) > 0 {
		facts["source_tables"] = tables
	}
	if files := financeFactSourceFiles(data); len(files) > 0 {
		facts["source_files"] = files
	}
	if note := strings.TrimSpace(anyToString(data["source_note"])); note != "" {
		facts["source_note"] = note
	}
	if updateNote := strings.TrimSpace(anyToString(data["source_update_note"])); updateNote != "" {
		facts["source_update_note"] = updateNote
		if updatedAt := strings.TrimSpace(strings.TrimPrefix(updateNote, "来源更新时间：")); updatedAt != "" && updatedAt != updateNote {
			facts["source_updated_at"] = updatedAt
		}
	}

	metrics := financeFactMetrics(data)
	if len(metrics) > 0 {
		facts["metrics"] = metrics
	}
	headlineMetric := firstFinanceFactString(data["headline_metric"], data["metric_label"], data["metric"])
	if headlineMetric != "" {
		facts["headline_metric"] = headlineMetric
	}
	if amount, ok := financeFactHeadlineAmount(data, metrics); ok {
		facts["headline_amount"] = amount
	}
	if warnings := financeFactStringSlice(data["warnings"], data["warning"]); len(warnings) > 0 {
		facts["warnings"] = warnings
	}
	if hints := financeFactStringSlice(data["explanation_hints"], data["tax_inclusion_note"], data["contract_fallback_reason"]); len(hints) > 0 {
		facts["explanation_hints"] = hints
	}
	if atoms := financeFactRequiredAtoms(facts); len(atoms) > 0 {
		facts["required_atoms"] = atoms
	}
	return facts
}

func financeFactResolvedPeriod(spec QuerySpec, data map[string]any) string {
	if period := strings.TrimSpace(anyToString(data["period"])); period != "" {
		return period
	}
	from := firstFinanceFactString(data["period_from"], spec.PeriodFrom)
	to := firstFinanceFactString(data["period_to"], spec.PeriodTo)
	return displayPeriod(from, to)
}

func financeFactBasis(spec QuerySpec, data map[string]any) string {
	for _, value := range []any{
		data["business_basis"],
		data["basis"],
		data["source_constraint"],
		data["source"],
		firstFinanceFactSourceTableBasis(data),
		spec.SourceConstraint,
		data["source_priority"],
	} {
		if label := financeFactBasisLabel(strings.TrimSpace(anyToString(value))); label != "" {
			return label
		}
	}
	return ""
}

func firstFinanceFactSourceTableBasis(data map[string]any) string {
	for _, table := range financeFactSourceTables(data) {
		if label := financeFactBasisLabel(table); label != "" {
			return label
		}
	}
	return ""
}

func financeFactBasisLabel(text string) string {
	if text == "" {
		return ""
	}
	if containsCJK(text) {
		return text
	}
	switch strings.ToLower(text) {
	case BossSourceBankStatement, "fin_bank_statement", "explicit_cash":
		return "银行流水口径"
	case BossSourceJournal, "fin_journal", "financial_account":
		return "序时账口径"
	case BossSourceBalance, "balance_sheet", "fin_balance_sheet", "fin_balance_detail", "official_then_evidence":
		return "余额表口径"
	case BossSourceContract, BossSourceContractAggregate, "contract_first", string(BossPerspectiveContractFirst), "contract_strict":
		return "项目/合同口径"
	case "accrual_only":
		return "权责发生制口径"
	case "cash_then_accrual":
		return "现金优先、权责补充口径"
	default:
		return text
	}
}

func containsCJK(text string) bool {
	for _, r := range text {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func financeFactSourceTables(data map[string]any) []string {
	raw := append(anySourceStringSlice(data["primary_source_tables"]), anySourceStringSlice(data["source_tables"])...)
	out := make([]string, 0, len(raw))
	for _, table := range raw {
		if table = strings.TrimSpace(baseSourceTableName(table)); table != "" {
			out = append(out, table)
		}
	}
	return dedupeSourceTables(out...)
}

func financeFactSourceFiles(data map[string]any) []string {
	files := append(anySourceStringSlice(data["source_documents"]), anySourceStringSlice(data["supporting_source_documents"])...)
	for _, key := range []string{"source_note", "source_summary"} {
		for _, match := range financeFactSourceDocPattern.FindAllString(strings.TrimSpace(anyToString(data[key])), -1) {
			files = append(files, match)
		}
	}
	if len(files) == 0 {
		note := strings.TrimSpace(anyToString(data["source_note"]))
		note = strings.TrimSpace(strings.TrimPrefix(note, "来源："))
		if note != "" && !strings.Contains(note, "未记录") {
			files = append(files, note)
		}
	}
	return dedupeSourceTables(files...)
}

func financeFactMetrics(data map[string]any) map[string]any {
	metrics := map[string]any{}
	if raw, ok := data["metrics"].(map[string]any); ok {
		for key, value := range raw {
			if strings.TrimSpace(key) != "" && value != nil {
				metrics[key] = value
			}
		}
	}
	if len(metrics) == 0 {
		label := firstFinanceFactString(data["metric_label"], data["metric"])
		if amount, ok := financeFactAnyNumber(data["total"]); ok && label != "" {
			metrics[label] = amount
		}
	}
	return metrics
}

func financeFactHeadlineAmount(data map[string]any, metrics map[string]any) (float64, bool) {
	for _, value := range []any{data["headline_amount"], data["total"], data["amount"], data["bank_in"], data["bank_out"]} {
		if amount, ok := financeFactAnyNumber(value); ok {
			return amount, true
		}
	}
	if len(metrics) == 1 {
		for _, value := range metrics {
			return financeFactAnyNumber(value)
		}
	}
	return 0, false
}

func financeFactRequiredAtoms(facts map[string]any) []string {
	atoms := []string{}
	if period := strings.TrimSpace(anyToString(facts["resolved_period"])); period != "" {
		atoms = append(atoms, "期间："+period)
	}
	if basis := strings.TrimSpace(anyToString(facts["basis"])); basis != "" {
		atoms = append(atoms, "口径："+basis)
	}
	if amount, ok := financeFactAnyNumber(facts["headline_amount"]); ok {
		atoms = append(atoms, fmt.Sprintf("金额：%.2f 元", amount))
	}
	for _, key := range []string{"source_note", "source_update_note"} {
		if note := strings.TrimSpace(anyToString(facts[key])); note != "" {
			atoms = append(atoms, note)
		}
	}
	return dedupeSourceTables(atoms...)
}

func financeFactStringSlice(values ...any) []string {
	out := []string{}
	for _, value := range values {
		switch typed := value.(type) {
		case []string:
			out = append(out, typed...)
		case []any:
			for _, item := range typed {
				if text := strings.TrimSpace(anyToString(item)); text != "" {
					out = append(out, text)
				}
			}
		default:
			if text := strings.TrimSpace(anyToString(value)); text != "" {
				out = append(out, text)
			}
		}
	}
	return dedupeSourceTables(out...)
}

func firstFinanceFactString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(anyToString(value)); text != "" {
			return text
		}
	}
	return ""
}

func financeFactAnyNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.ReplaceAll(typed, ",", "")), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}
