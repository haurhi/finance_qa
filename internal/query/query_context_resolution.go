package query

import (
	"context"
	"strings"
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
	return e.normalizeQuestionAndResolveCompanyWithUserQuestion(question, question)
}

func (e *Engine) normalizeQuestionAndResolveCompanyWithUserQuestion(question, userQuestion string) string {
	q := NormalizeQuestion(question)
	groundingQuestion := NormalizeQuestion(userQuestion)
	if groundingQuestion == "" {
		groundingQuestion = q
	}
	resolved := ResolveCompanyMention(groundingQuestion, e.available)
	if resolved != "" && resolved != e.Company {
		e.Company = resolved
	}
	return q
}

func (e *Engine) resolveQueryEntity(q string, spec QuerySpec) string {
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

func shouldKeepEntityForQuestionScope(question, entity string, cfg RuleConfig) bool {
	return shouldKeepEntityForQuestionScopeWithUserQuestion(question, question, entity, cfg)
}

func shouldKeepEntityForQuestionScopeWithUserQuestion(question, userQuestion, entity string, cfg RuleConfig) bool {
	groundingQuestion := NormalizeQuestion(userQuestion)
	if groundingQuestion == "" {
		groundingQuestion = question
	}
	officialCompanyARAP := shouldTreatAsCompanyOfficialARAPQuestion(groundingQuestion, "")
	companyScope := looksLikeCompanyScopeProjectAggregateQuestion(groundingQuestion) ||
		shouldUseLatestRevenueContractAggregate(groundingQuestion, cfg) ||
		officialCompanyARAP
	if !companyScope {
		return true
	}
	if officialCompanyARAP {
		groundingQuestion = stripBalanceSheetAccountTerms(groundingQuestion)
	}
	return entityAppearsInQuestionText(groundingQuestion, entity)
}

func (e *Engine) applyQueryPriorityAdjustments(q, userQuestion string, intent Intent, spec QuerySpec, entity string, hasRealEntity bool, anchor time.Time) (QuerySpec, string, bool, time.Time) {
	cfg := e.currentRuleConfig()
	if strings.TrimSpace(entity) == "" {
		if matched := e.resolveContractSubject(q, ""); shouldUseGroundedContractSubjectForDimension(q, matched) {
			entity = matched
			spec.Entity = matched
			spec.BossRewrite.Entity = matched
			hasRealEntity = true
		}
	}
	if !shouldKeepEntityForQuestionScopeWithUserQuestion(q, userQuestion, entity, cfg) {
		entity = ""
		spec.Entity = ""
		spec.BossRewrite.Entity = ""
		spec.NeedsContractDimension = false
		spec = reconcileQuerySpec(spec, entity, cfg)
		return spec, entity, false, anchor
	}
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
	return e.resolveQueryRoutingWithUserQuestion(question, question)
}

func (e *Engine) resolveQueryRoutingWithUserQuestion(question, userQuestion string) resolvedQueryRouting {
	q := e.normalizeQuestionAndResolveCompanyWithUserQuestion(question, userQuestion)
	groundingQuestion := NormalizeQuestion(userQuestion)
	if groundingQuestion == "" {
		groundingQuestion = q
	}
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
	spec, entity, hasRealEntity, periodAnchor = e.applyQueryPriorityAdjustments(q, groundingQuestion, intent, spec, entity, hasRealEntity, periodAnchor)
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
