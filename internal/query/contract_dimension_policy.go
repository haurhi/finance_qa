package query

import "strings"

func inferContractAskedTopic(question string) string {
	q := strings.TrimSpace(question)
	switch {
	case containsAny(q, []string{"内容", "合同内容", "是什么"}):
		return "content"
	case containsAny(q, []string{"利润", "毛利", "净利"}):
		return "profit"
	case containsAny(q, []string{"应收未收", "客户未付款", "客户没付款", "客户未支付"}):
		return "receivable"
	case containsAny(q, []string{"已收票未付款", "已收票未支付", "收票未付款", "收票未支付", "发票未付款", "发票未支付"}):
		return "invoice"
	case containsAny(q, []string{"项目应付", "应付未付", "应付未付款", "应付未支付", "未付款", "未支付", "没付款", "没有付款"}):
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

func (e *Engine) shouldPrioritizeContractQuery(question, entity string, hasRealEntity bool) bool {
	cfg := e.currentRuleConfig()
	if shouldUseContractDetailQuestion(question) {
		return false
	}
	if shouldUseContractAggregateAnalysisQuestion(question, cfg) {
		return false
	}
	if shouldUseCompanyScopeContractAggregate(question) && strings.TrimSpace(entity) == "" && !hasRealEntity {
		return false
	}
	if shouldUseContractDimensionWithConfig(question, cfg) {
		return true
	}
	if !isContractPriorityQuestionWithConfig(question, cfg) {
		return false
	}
	if matched := e.resolveContractSubject(question, entity); matched != "" {
		return true
	}
	return hasRealEntity && e.hasContractDimensionEntity(entity)
}
