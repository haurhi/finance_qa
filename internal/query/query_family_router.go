package query

import "strings"

func detectQueryFamily(q string, intent Intent, entity, from, to string, cfg RuleConfig, needsContractDimension bool) QueryFamily {
	if shouldUseContractDetailQuestion(q) {
		return QueryFamilyContractDetail
	}
	if family, ok := resolveForcedQueryFamily(needsContractDimension); ok {
		return family
	}
	if family, ok := resolveOperationalQueryFamily(q, intent, cfg); ok {
		return family
	}
	hasRealishEntity := isRealishQueryEntity(entity)
	if family, ok := resolveMetricDrivenQueryFamily(q, entity, from, to, cfg, hasRealishEntity); ok {
		return family
	}
	return resolveFallbackQueryFamily(intent, hasRealishEntity)
}

func resolveForcedQueryFamily(needsContractDimension bool) (QueryFamily, bool) {
	if needsContractDimension {
		return QueryFamilyContractDimension, true
	}
	return "", false
}

func resolveOperationalQueryFamily(q string, intent Intent, cfg RuleConfig) (QueryFamily, bool) {
	switch {
	case strings.Contains(q, "数据出来"):
		return QueryFamilyReadiness, true
	case shouldUseSupplierPaymentStats(q):
		return QueryFamilySupplierPayments, true
	case shouldUseHRBreakdown(q, cfg):
		return QueryFamilyHRCost, true
	case shouldUseContractFirstARAP(q):
		return QueryFamilyCoreMetric, true
	case intent == IntentARAPQuery || isOpeningPeriodQuestion(q):
		return QueryFamilyARAP, true
	case shouldUseReconciliation(q):
		return QueryFamilyReconciliation, true
	default:
		return "", false
	}
}

func resolveMetricDrivenQueryFamily(q, entity, from, to string, cfg RuleConfig, hasRealishEntity bool) (QueryFamily, bool) {
	switch {
	case isBossAggregateSummaryQuestion(q, entity, from, to, cfg):
		return QueryFamilyCoreMetric, true
	case isIntervalCoreMetricQuestionWithConfig(q, entity, hasRealishEntity, from, to, cfg):
		return QueryFamilyCoreMetric, true
	case shouldPreferCoreMetricSummaryWithConfig(q, entity, hasRealishEntity, from, to, cfg):
		return QueryFamilyCoreMetric, true
	case hasRealishEntity && containsAny(q, counterpartyMetricKeywords(cfg)):
		if looksLikeBalanceSheetARAPQuestion(q) {
			return QueryFamilyCoreMetric, true
		}
		return QueryFamilyCounterparty, true
	default:
		return "", false
	}
}

func resolveFallbackQueryFamily(intent Intent, hasRealishEntity bool) QueryFamily {
	switch intent {
	case IntentMonthlySummary:
		return QueryFamilyCoreMetric
	case IntentFallback:
		if hasRealishEntity {
			return QueryFamilyCounterparty
		}
	}
	return QueryFamilyGeneral
}

func isBossAggregateSummaryQuestion(q, entity, from, to string, cfg RuleConfig) bool {
	if !containsAny(q, metricQuestionKeywords(cfg)) {
		return false
	}
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return false
	}
	// 公司汇总型提问不能被时间片段/问句碎片误判成实体后抢走路由。
	if isRealishQueryEntity(entity) {
		return false
	}
	return true
}
