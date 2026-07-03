package query

import (
	"fmt"
	"strings"
)

func (e *Engine) queryIdentityResult(entity string) Result {
	role, _ := e.detectEntityRole(entity)
	return Result{
		Success: true,
		Message: fmt.Sprintf("识别结果: [%s] 是 [%s]", entity, role),
		Data:    map[string]any{"entity": entity, "role": role},
		ExecutedSQL: []string{
			"detectEntityRole: SELECT SUM(debit_amount), SUM(credit_amount) FROM bank_statement WHERE counterparty_name LIKE ?",
			"detectEntityRole: SELECT account_code, summary FROM journal WHERE summary LIKE ? OR account_name LIKE ?",
		},
		CalculationLogs: []string{
			fmt.Sprintf("[身份识别] entity=%s role=%s", entity, role),
		},
	}
}

func buildQuerySpecEnvelope(spec QuerySpec) map[string]any {
	envelope := map[string]any{
		"query_family":                  spec.QueryFamily,
		"metric_kind":                   spec.MetricKind,
		"entity":                        spec.Entity,
		"period_from":                   spec.PeriodFrom,
		"period_to":                     spec.PeriodTo,
		"sub_period":                    spec.SubPeriod,
		"time_scope":                    spec.TimeScope,
		"perspective_policy":            spec.PerspectivePolicy,
		"needs_contract_dimension":      spec.NeedsContractDimension,
		"prefer_contract_aggregate":     spec.PreferContractAggregate,
		"readiness_check_required":      spec.ReadinessCheckRequired,
		"authoritative_source_required": spec.AuthoritativeSourceRequired,
		"opening_period_aware":          spec.OpeningPeriodAware,
		"source_constraint":             spec.SourceConstraint,
		"lexicon_profile":               spec.LexiconProfile,
	}
	if strings.TrimSpace(spec.AsOf) != "" {
		envelope["as_of"] = spec.AsOf
	}
	if len(spec.SemanticFamilies) > 0 {
		envelope["semantic_families"] = append([]string{}, spec.SemanticFamilies...)
	}
	if spec.BossRewrite.Metric != "" {
		envelope["boss_rewrite"] = buildBossRewriteEnvelope(spec.BossRewrite)
	}
	if spec.RouteDecision.SelectedSource != "" || len(spec.RouteDecision.ProbeResults) > 0 {
		envelope["route_decision"] = buildRouteDecisionEnvelope(spec.RouteDecision)
	}
	return envelope
}

func buildBossRewriteEnvelope(rewrite BossQueryRewrite) map[string]any {
	return map[string]any{
		"metric":                rewrite.Metric,
		"scope":                 rewrite.Scope,
		"entity":                rewrite.Entity,
		"period_from":           rewrite.PeriodFrom,
		"period_to":             rewrite.PeriodTo,
		"sub_period":            rewrite.SubPeriod,
		"granularity":           rewrite.Granularity,
		"perspective":           rewrite.Perspective,
		"source_constraint":     rewrite.SourceConstraint,
		"requires_source_probe": rewrite.RequiresSourceProbe,
	}
}

func buildRouteDecisionEnvelope(decision RouteDecision) map[string]any {
	return map[string]any{
		"selected_source":   decision.SelectedSource,
		"primary_tables":    append([]string{}, decision.PrimaryTables...),
		"supporting_tables": append([]string{}, decision.SupportingTables...),
		"fallback_reason":   decision.FallbackReason,
		"probe_results":     buildProbeResultEnvelopes(decision.ProbeResults),
	}
}

func buildProbeResultEnvelopes(probes []SourceProbeResult) []map[string]any {
	out := make([]map[string]any, 0, len(probes))
	for _, probe := range probes {
		out = append(out, map[string]any{
			"source":            probe.Source,
			"semantic_match":    probe.SemanticMatch,
			"can_answer":        probe.CanAnswer,
			"coverage_status":   probe.CoverageStatus,
			"metric":            probe.Metric,
			"period_from":       probe.PeriodFrom,
			"period_to":         probe.PeriodTo,
			"row_count":         probe.RowCount,
			"missing_reason":    probe.MissingReason,
			"primary_tables":    append([]string{}, probe.PrimaryTables...),
			"supporting_tables": append([]string{}, probe.SupportingTables...),
			"source_documents":  append([]string{}, probe.SourceDocuments...),
		})
	}
	return out
}

func applyQuerySpecOverrides(spec QuerySpec, data map[string]any) QuerySpec {
	if data == nil {
		return spec
	}
	raw, ok := data["query_spec_overrides"].(map[string]any)
	if !ok {
		return spec
	}
	overrides := cloneMap(raw)
	if len(overrides) == 0 {
		return spec
	}
	if periodFrom := strings.TrimSpace(anyToString(overrides["period_from"])); periodFrom != "" {
		spec.PeriodFrom = periodFrom
		if spec.BossRewrite.Metric != "" {
			spec.BossRewrite.PeriodFrom = periodFrom
		}
	}
	if periodTo := strings.TrimSpace(anyToString(overrides["period_to"])); periodTo != "" {
		spec.PeriodTo = periodTo
		if spec.BossRewrite.Metric != "" {
			spec.BossRewrite.PeriodTo = periodTo
		}
	}
	if subPeriod := strings.TrimSpace(anyToString(overrides["sub_period"])); subPeriod != "" {
		spec.SubPeriod = subPeriod
		if spec.BossRewrite.Metric != "" {
			spec.BossRewrite.SubPeriod = subPeriod
		}
	}
	if timeScope := strings.TrimSpace(anyToString(overrides["time_scope"])); timeScope != "" {
		spec.TimeScope = TimeScope(timeScope)
	}
	if perspective := strings.TrimSpace(anyToString(overrides["perspective_policy"])); perspective != "" {
		spec.PerspectivePolicy = PerspectivePolicy(perspective)
	}
	if families := anySourceStringSlice(overrides["semantic_families"]); len(families) > 0 {
		spec.SemanticFamilies = families
	}
	if strings.TrimSpace(anyToString(data["source_priority"])) == "contract_first" {
		spec.RouteDecision.FallbackReason = ""
		for i := range spec.RouteDecision.ProbeResults {
			if spec.RouteDecision.ProbeResults[i].Source == BossSourceContractAggregate {
				spec.RouteDecision.ProbeResults[i].PeriodFrom = spec.PeriodFrom
				spec.RouteDecision.ProbeResults[i].PeriodTo = spec.PeriodTo
				spec.RouteDecision.ProbeResults[i].MissingReason = ""
				spec.RouteDecision.ProbeResults[i].CanAnswer = true
				spec.RouteDecision.ProbeResults[i].CoverageStatus = CoverageFull
			}
		}
	}
	return spec
}

func finalizeQueryResult(ctx queryExecutionContext, r Result) Result {
	if r.Data == nil {
		r.Data = map[string]any{}
	}
	r.Data["intent_trace"] = ctx.traceMap
	finalSpec := applyQuerySpecOverrides(ctx.spec, r.Data)
	r.Data["query_spec"] = buildQuerySpecEnvelope(finalSpec)
	if finalSpec.RouteDecision.SelectedSource != "" || len(finalSpec.RouteDecision.ProbeResults) > 0 {
		r.Data["route_decision"] = buildRouteDecisionEnvelope(finalSpec.RouteDecision)
		if reason := strings.TrimSpace(finalSpec.RouteDecision.FallbackReason); reason != "" && strings.TrimSpace(anyToString(r.Data["source_priority"])) != "contract_first" {
			if _, exists := r.Data["contract_fallback_reason"]; !exists {
				r.Data["contract_fallback_reason"] = reason
			}
		}
	}

	if ctx.engine != nil {
		r = ctx.engine.annotateSourceAttribution(finalSpec, r)
	}

	if financeFacts := buildFinanceFacts(finalSpec, r.Data); financeFacts != nil {
		r.Data["finance_facts"] = financeFacts
	}

	// 新增 bridge 兼容字段：final_answer 和 host_summary_contract。
	// 来源归因会改写老板可见 message，兼容字段必须在归因后生成。
	r.Data["final_answer"] = buildFinalAnswer(r)
	if hostSummary := buildHostSummaryContract(r.Data, ctx.spec.NormalizedQuestion); hostSummary != nil {
		r.Data["host_summary_contract"] = hostSummary
	}
	return r.WithTraceData()
}

func (ctx queryExecutionContext) finalize(r Result) Result {
	return finalizeQueryResult(ctx, r)
}
