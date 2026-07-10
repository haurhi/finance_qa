package query

import (
	"strings"
	"time"
)

type queryExecutionContext struct {
	engine       *Engine
	question     string
	userQuestion string
	q            string
	intent       Intent
	spec         QuerySpec
	traceMap     map[string]any
	anchor       time.Time
	from         string
	to           string
	cfg          RuleConfig
	entity       string

	hasRealEntity bool
}

func shouldResolveEntityDeeply(spec QuerySpec) bool {
	if spec.QueryFamily == QueryFamilyContractDetail {
		return false
	}
	seedEntity := strings.TrimSpace(spec.Entity)
	meaningfulSeedEntity := seedEntity != "" && !looksLikeSyntheticQuestionFragment(seedEntity) && !looksLikeAccountFragment(seedEntity) && !looksLikePeriodOnlyEntity(seedEntity)
	if meaningfulSeedEntity {
		return true
	}
	switch spec.QueryFamily {
	case QueryFamilyHRCost:
		return false
	case QueryFamilyCounterparty, QueryFamilyContractDimension, QueryFamilyReadiness:
		return true
	case QueryFamilyARAP:
		return strings.Contains(spec.NormalizedQuestion, "项目") && meaningfulSeedEntity
	}
	switch spec.Intent {
	case IntentIdentityQuery:
		return true
	case IntentFallback:
		return meaningfulSeedEntity
	default:
		return false
	}
}

func (e *Engine) Query(question string) Result {
	return e.QueryWithUserQuestion(question, question)
}

// QueryWithUserQuestion uses question for routing while userQuestion controls
// company selection and grounding for protected company-scope queries.
func (e *Engine) QueryWithUserQuestion(question, userQuestion string) Result {
	ctx := e.prepareQueryExecutionContextWithUserQuestion(question, userQuestion)
	return e.executeQuery(ctx)
}

func (e *Engine) prepareQueryExecutionContext(question string) queryExecutionContext {
	return e.prepareQueryExecutionContextWithUserQuestion(question, question)
}

func (e *Engine) prepareQueryExecutionContextWithUserQuestion(question, userQuestion string) queryExecutionContext {
	route := e.resolveQueryRoutingWithUserQuestion(question, userQuestion)
	groundingQuestion := strings.TrimSpace(userQuestion)
	if groundingQuestion == "" {
		groundingQuestion = question
	}

	return queryExecutionContext{
		engine:        e,
		question:      question,
		userQuestion:  groundingQuestion,
		q:             route.normalizedQuestion,
		intent:        route.intent,
		spec:          route.spec,
		traceMap:      route.traceMap,
		anchor:        route.anchor,
		from:          route.spec.PeriodFrom,
		to:            route.spec.PeriodTo,
		cfg:           route.cfg,
		entity:        route.entity,
		hasRealEntity: route.hasRealEntity,
	}
}
