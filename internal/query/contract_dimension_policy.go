package query

import "strings"

func inferContractAskedTopic(question string) string {
	q := strings.TrimSpace(question)
	switch {
	case containsAny(q, []string{"内容", "合同内容", "是什么"}):
		return "content"
	case containsAny(q, []string{"利润", "毛利", "净利"}):
		return "profit"
	case containsAny(q, []string{"应收未收", "客户未付款", "客户没付款", "客户未支付", "没收回来", "未收回", "还没收回来", "没有收回来"}):
		return "receivable"
	case containsAny(q, []string{"已收票未付款", "已收票未支付", "收票未付款", "收票未支付", "发票未付款", "发票未支付"}):
		return "invoice"
	case containsAny(q, []string{"项目应付", "应付未付", "应付未付款", "应付未支付", "未付款", "未支付", "没付款", "没付", "没有付款"}):
		return "payable"
	case containsAny(q, []string{"营收", "收入", "销售额", "GMV", "gmv"}):
		return "revenue"
	case containsAny(q, []string{"成本", "支出"}):
		return "cost"
	case containsAny(q, []string{"回款", "到账", "收款"}):
		return "receipts"
	case containsAny(q, []string{"开票", "发票"}):
		return "invoice"
	case containsAny(q, []string{"付款", "支付"}):
		return "payments"
	default:
		return "generic"
	}
}

func contractSourceTablesForRole(role string) []string {
	return contractSourceTablesForRoleWithConfig(role, getRuleConfig())
}

func contractSourceTablesForRoleWithConfig(role string, cfg RuleConfig) []string {
	return cfg.ContractSourceTables(role)
}

func (e *Engine) hasContractDimensionEntity(entity string) bool {
	return len(e.resolveContractSubjectCandidates(entity)) > 0
}

func stronglyMentionsContractSubject(question, subject string) bool {
	normalizedQuestion := normalizeEntityText(question)
	normalizedSubject := normalizeEntityText(subject)
	return len([]rune(normalizedSubject)) >= 2 && strings.Contains(normalizedQuestion, normalizedSubject)
}

func shouldUseGroundedContractSubjectForDimension(question, subject string) bool {
	if strings.TrimSpace(subject) == "" {
		return false
	}
	if shouldUseExplicitFinancialAccountQuestion(question) || !stronglyMentionsContractSubject(question, subject) {
		return false
	}
	askedTopic := inferContractAskedTopic(question)
	if containsAny(question, []string{"合同", "协议"}) || hasContractContentCodeSubject(question) {
		return contractAskedTopicTriggersDimension(askedTopic)
	}
	if askedTopic == "payable" && containsAny(question, []string{"项目成本", "成本口径", "项目应付", "应付未付", "未付款", "未支付", "没付款", "没付"}) {
		return true
	}
	return false
}

func (e *Engine) shouldPrioritizeContractQuery(question, entity string, hasRealEntity bool) bool {
	cfg := e.currentRuleConfig()
	if shouldUseContractDetailQuestion(question) {
		return false
	}
	if shouldUseExplicitFinancialAccountQuestion(question) {
		return false
	}
	matchedSubject := e.resolveContractSubject(question, entity)
	if shouldUseGroundedContractSubjectForDimension(question, matchedSubject) {
		return true
	}
	if shouldUseContractAggregateAnalysisQuestion(question, cfg) {
		return false
	}
	companyAggregateScope := looksLikeCompanyScopeProjectAggregateQuestion(question) || shouldUseLatestRevenueContractAggregate(question, cfg)
	if companyAggregateScope && strings.TrimSpace(entity) != "" && !stronglyMentionsContractSubject(question, entity) {
		return false
	}
	if shouldUseCompanyScopeContractAggregate(question) && strings.TrimSpace(entity) == "" && !hasRealEntity && matchedSubject == "" {
		return false
	}
	if shouldUseContractDimensionWithConfig(question, cfg) {
		return true
	}
	if !isContractPriorityQuestionWithConfig(question, cfg) {
		return false
	}
	if matchedSubject != "" &&
		(stronglyMentionsContractSubject(question, matchedSubject) ||
			(!companyAggregateScope && (entityAppearsInQuestionText(question, matchedSubject) || strings.TrimSpace(entity) != ""))) {
		return true
	}
	return hasRealEntity &&
		(stronglyMentionsContractSubject(question, entity) || !companyAggregateScope) &&
		e.hasContractDimensionEntity(entity)
}
