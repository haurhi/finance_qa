package query

import (
	"context"
	"time"
)

type resolvedQueryRouting struct {
	normalizedQuestion string
	intent             Intent
	traceMap           map[string]any
	spec               QuerySpec
	entity             string
	hasRealEntity      bool
	anchor             time.Time
	cfg                RuleConfig
}

func buildIntentTraceMap(intentTrace IntentTrace) map[string]any {
	return map[string]any{
		"router_version": intentTrace.RouterVersion,
		"matched":        append([]string{}, intentTrace.Matched...),
		"scores":         intentTrace.Scores,
		"final_intent":   intentTrace.FinalIntent,
		"confidence":     intentTrace.Confidence,
	}
}

func (e *Engine) normalizeQuestionAndResolveCompany(question string) string {
	q := NormalizeQuestion(question)
	resolved := ResolveCompanyMention(q, e.available)
	if resolved != "" && resolved != e.Company {
		e.Company = resolved
	}
	return q
}

func (e *Engine) resolveQueryEntity(q string, spec QuerySpec) string {
	if shouldForceCompanyScopeContractAggregateWithConfig(q, e.currentRuleConfig()) {
		return ""
	}
	if shouldTreatAsCompanyOfficialARAPQuestion(q, spec.Entity) {
		return ""
	}
	entity := spec.Entity
	if shouldResolveEntityDeeply(spec) {
		if resolved := e.resolveEntityByScoredCandidates(q); resolved != "" {
			entity = resolved
		} else {
			entity = ""
		}
	}
	if looksLikeBossRewriteNonEntity(entity) {
		return ""
	}
	return entity
}

func (e *Engine) applyQueryPriorityAdjustments(q string, intent Intent, spec QuerySpec, entity string, hasRealEntity bool, anchor time.Time) (QuerySpec, string, bool, time.Time) {
	cfg := e.currentRuleConfig()
	if intent == IntentIdentityQuery || isCounterpartyClassificationQuestionWithConfig(q, cfg) {
		return spec, entity, hasRealEntity, anchor
	}
	if shouldUseExpenseBreakdownWithConfig(q, cfg) {
		return spec, entity, hasRealEntity, anchor
	}
	if spec.QueryFamily == QueryFamilyContractDetail {
		return spec, entity, hasRealEntity, anchor
	}
	if !e.shouldPrioritizeContractQuery(q, entity, hasRealEntity) {
		return spec, entity, hasRealEntity, anchor
	}
	if matched := e.resolveContractSubject(q, entity); matched != "" {
		entity = matched
		spec.Entity = matched
	}
	anchor = e.periodParserAnchorForQuestion(q, e.getLatestContractPeriodAnchor())
	from, to := extractContractQuestionPeriods(q, anchor)
	subPeriod, _ := extractReceiptSubPeriod(q, from, to)
	spec.QueryFamily = QueryFamilyContractDimension
	spec.PeriodFrom = from
	spec.PeriodTo = to
	spec.SubPeriod = subPeriod
	spec.TimeScope = detectTimeScope(q, from, to, anchor)
	spec.NeedsContractDimension = true
	spec.PerspectivePolicy = PerspectiveCashThenAccrual
	spec.PreferContractAggregate = false
	hasRealEntity = true
	return spec, entity, hasRealEntity, anchor
}

func normalizeExplicitCashCompanyRoute(q string, spec QuerySpec, entity string, hasRealEntity bool, cfg RuleConfig) (QuerySpec, string, bool) {
	if spec.SourceConstraint != BossSourceBankStatement || spec.BossRewrite.Perspective != BossPerspectiveExplicitCash {
		return spec, entity, hasRealEntity
	}
	if !looksLikeBossRewriteNonEntity(entity) {
		return spec, entity, hasRealEntity
	}
	entity = ""
	spec.Entity = ""
	spec.QueryFamily = detectQueryFamily(q, spec.Intent, "", spec.PeriodFrom, spec.PeriodTo, cfg, spec.NeedsContractDimension)
	spec = applyQuerySpecPolicy(spec, deriveQuerySpecPolicy(spec, cfg))
	return spec, entity, false
}

func (e *Engine) resolveQueryRouting(question string) resolvedQueryRouting {
	q := e.normalizeQuestionAndResolveCompany(question)
	cfg := e.currentRuleConfig()
	anchor := e.getLatestPeriodAnchor()
	periodAnchor := e.periodParserAnchorForQuestion(q, anchor)

	intent, intentTrace := classifyIntentV2(q, cfg)
	spec := buildQuerySpec(q, periodAnchor, cfg)
	entity := e.resolveQueryEntity(q, spec)
	spec = reconcileQuerySpec(spec, entity, cfg)
	spec = e.applyLegacyContractContentFallback(q, spec)
	entity = spec.Entity
	hasRealEntity := e.isRealBusinessEntity(q, entity)
	spec, entity, hasRealEntity = normalizeExplicitCashCompanyRoute(q, spec, entity, hasRealEntity, cfg)
	spec, entity, hasRealEntity, periodAnchor = e.applyQueryPriorityAdjustments(q, intent, spec, entity, hasRealEntity, periodAnchor)
	spec, _ = e.decideBossRoute(context.Background(), spec)
	entity = spec.Entity
	hasRealEntity = e.isRealBusinessEntity(q, entity)
	spec, entity, hasRealEntity = normalizeExplicitCashCompanyRoute(q, spec, entity, hasRealEntity, cfg)

	return resolvedQueryRouting{
		normalizedQuestion: q,
		intent:             intent,
		traceMap:           buildIntentTraceMap(intentTrace),
		spec:               spec,
		entity:             entity,
		hasRealEntity:      hasRealEntity,
		anchor:             periodAnchor,
		cfg:                cfg,
	}
}

func (e *Engine) applyLegacyContractContentFallback(q string, spec QuerySpec) QuerySpec {
	if spec.QueryFamily != QueryFamilyContractDetail {
		return spec
	}
	if len(e.tableColumns("contract_main")) > 0 {
		return spec
	}
	intent := inferContractDetailIntent(q)
	if intent != ContractDetailIntentField && intent != ContractDetailIntentPage {
		return spec
	}
	if !containsAny(q, []string{"合同内容", "内容是什么", "是什么"}) {
		return spec
	}
	spec.QueryFamily = QueryFamilyContractDimension
	spec.NeedsContractDimension = true
	spec.PreferContractAggregate = false
	spec.PerspectivePolicy = PerspectiveCashThenAccrual
	return spec
}
