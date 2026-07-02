package query

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (e *Engine) collectContractDimensionSummary(question, entity string, anchor time.Time) (contractDimensionSummary, error) {
	from, to := extractContractQuestionPeriods(question, anchor)
	return e.collectContractDimensionSummaryForPeriod(question, entity, from, to)
}

func (e *Engine) collectContractDimensionSummaryForPeriod(question, entity, from, to string) (contractDimensionSummary, error) {
	cfg := e.currentRuleConfig()
	subPeriod, hasSubPeriod := extractReceiptSubPeriod(question, from, to)
	askedTopic := inferContractAskedTopic(question)

	if resolved := e.resolveContractSubject(question, entity); resolved != "" {
		entity = resolved
	}
	if strings.TrimSpace(entity) == "" {
		return contractDimensionSummary{}, errors.New("contract entity not found")
	}

	contracts := e.queryMatchingContractsForQuestion(question, entity)
	if len(contracts) == 0 {
		return contractDimensionSummary{}, errors.New("contract not found")
	}

	role := "unknown"
	coverageNote := ""
	if askedTopic != "content" {
		role = e.detectContractRole(entity, from, to)
		if role == "unknown" {
			actualFrom, actualTo, fallbackRole, note := e.resolveContractDimensionFallbackPeriod(question, entity, from, to, askedTopic)
			if fallbackRole == "unknown" {
				return contractDimensionSummary{}, errors.New("contract role not found")
			}
			from, to, role = actualFrom, actualTo, fallbackRole
			coverageNote = note
		}
	}

	summary := newContractDimensionSummary(entity, role, from, to, askedTopic, contracts, cfg)
	if strings.TrimSpace(coverageNote) != "" {
		summary.CalculationLog = append(summary.CalculationLog, coverageNote)
	}
	if askedTopic == "content" {
		summary.Role = "contract_content"
		summary.Data["role"] = "contract_content"
		summary.Data["source_tables"] = contractSourceTablesForRoleWithConfig("contract_content", cfg)
		applyContractPerspectiveAliases(summary.Data)
		return summary, nil
	}

	like := "%" + entity + "%"
	var err error
	switch role {
	case "customer_contract":
		summary, err = e.collectCustomerContractSummary(summary, like, hasSubPeriod, subPeriod)
	case "supplier_contract":
		summary, err = e.collectSupplierContractSummary(summary, like)
	case "mixed_contract":
		summary, err = e.collectMixedContractSummary(summary, like)
	default:
		err = errors.New("unsupported contract role")
	}
	if err != nil {
		return contractDimensionSummary{}, err
	}

	applyContractPerspectiveAliases(summary.Data)
	return summary, nil
}

func (e *Engine) resolveContractDimensionFallbackPeriod(question, entity, from, to, askedTopic string) (string, string, string, string) {
	if !contractDimensionCanUseCostLatestPeriod(question, askedTopic) {
		return from, to, "unknown", ""
	}
	latest := e.latestContractFinancePeriodForEntity(costSettlementTotalsSpec(), "%"+entity+"%", to)
	if latest == "" {
		return from, to, "unknown", ""
	}
	role := e.detectContractRole(entity, latest, latest)
	if role == "unknown" {
		return from, to, "unknown", ""
	}
	note := fmt.Sprintf("[合同维度覆盖] requested=%s actual=%s reason=请求期间无项目成本记录，改用该合同/供应商最新项目成本账期",
		displayPeriod(from, to),
		displayPeriod(latest, latest))
	return latest, latest, role, note
}

func contractDimensionCanUseCostLatestPeriod(question, askedTopic string) bool {
	switch askedTopic {
	case "payable", "cost":
	default:
		return false
	}
	return contractAggregateCanUseLatestAvailablePeriod(question)
}

func (e *Engine) latestContractFinancePeriodForEntity(spec contractFinanceTotalsSpec, like, atOrBefore string) string {
	upper := strings.TrimSpace(atOrBefore)
	if upper == "" {
		upper = "9999-12"
	}
	_, latest, ok := e.contractFinanceDataBounds(spec, "", upper, like)
	if !ok {
		return ""
	}
	return latest
}

func newContractDimensionSummary(entity, role, from, to, askedTopic string, contracts []contractDimensionRow, cfg RuleConfig) contractDimensionSummary {
	periodLabel := displayPeriod(from, to)
	contractList := make([]map[string]any, 0, len(contracts))
	for _, contract := range contracts {
		contractList = append(contractList, map[string]any{
			"contract_id":      contract.ContractID,
			"customer_name":    contract.CustomerName,
			"contract_content": contract.ContractContent,
		})
	}

	return contractDimensionSummary{
		Entity:     entity,
		Role:       role,
		Period:     periodLabel,
		PeriodFrom: from,
		PeriodTo:   to,
		Contracts:  contractList,
		Data: map[string]any{
			"entity":         entity,
			"role":           role,
			"period":         periodLabel,
			"period_from":    from,
			"period_to":      to,
			"contract_count": len(contracts),
			"contracts":      contractList,
			"asked_topic":    askedTopic,
			"source_tables":  contractSourceTablesForRoleWithConfig(role, cfg),
		},
		ExecutedSQL: []string{
			"contract_lookup: SELECT contract_id, customer_name, contract_content FROM fin_contracts WHERE customer_name LIKE ? OR contract_content LIKE ? ORDER BY contract_id",
		},
		CalculationLog: []string{
			fmt.Sprintf("[合同维度] entity=%s period=%s matched_contracts=%d", entity, periodLabel, len(contracts)),
		},
	}
}
