package query

import (
	"context"
	"fmt"
	"math"
	"strings"
)

func (e *Engine) queryARAP(question, entity, from, to string) Result {
	period := to
	var result Result
	if entity != "" && strings.Contains(question, "项目") {
		result = e.queryProjectARAP(entity, from, to)
	} else if entity == "" && shouldUseCompanyOfficialARAPComparison(question) {
		result = e.queryCompanyOfficialARAP(period)
	} else if entity != "" && e.isRealBusinessEntity(question, entity) && strings.Contains(question, "应付") && !strings.Contains(question, "应收/应付") && !strings.Contains(question, "应收") {
		result = e.queryAccountPayableReceivable(period, "应付账款", "2202", "payable", entity)
	} else if entity != "" && e.isRealBusinessEntity(question, entity) && strings.Contains(question, "应收") && !strings.Contains(question, "应收/应付") && !strings.Contains(question, "应付") {
		result = e.queryAccountPayableReceivable(period, "应收账款", "1122", "receivable", entity)
	} else if entity != "" && e.isRealBusinessEntity(question, entity) && containsAny(question, []string{"应收", "应付"}) {
		result = e.queryEntityARAP(entity, period)
	} else if strings.Contains(question, "应收") {
		result = e.queryAccountPayableReceivable(period, "应收账款", "1122", "receivable", "")
	} else if strings.Contains(question, "应付") {
		result = e.queryAccountPayableReceivable(period, "应付账款", "2202", "payable", "")
	} else if entity != "" {
		result = e.queryCounterpartyAmountFallback(question, entity, from, to)
	} else {
		result = Result{
			Success: false,
			Message: "未识别应收/应付对象",
			CalculationLogs: []string{
				"[AR/AP分流] 问题未命中应收/应付且未识别实体",
			},
		}
	}

	if result.Success {
		spec := BuildQuerySpec(question, e.getLatestPeriodAnchor())
		if strings.TrimSpace(entity) != "" && !looksLikeAccountFragment(entity) && e.isRealBusinessEntity(question, entity) {
			spec.Entity = entity
		} else {
			spec.Entity = ""
		}
		spec.PeriodFrom = from
		spec.PeriodTo = to
		if factSet, ok := buildARAPFactSetFromQueryResult(spec, result); ok && len(factSet.Facts) > 0 {
			if result.Data == nil {
				result.Data = map[string]any{}
			}
			result.Data["fact_sets"] = []FactSet{factSet}
		} else if factSet, err := NewARAPSourceAdapter(e).Fetch(context.Background(), spec); err == nil && len(factSet.Facts) > 0 {
			if result.Data == nil {
				result.Data = map[string]any{}
			}
			result.Data["fact_sets"] = []FactSet{factSet}
		}
	}
	return result
}

func shouldUseCompanyOfficialARAPComparison(question string) bool {
	if !strings.Contains(question, "应收") || !strings.Contains(question, "应付") {
		return false
	}
	return shouldUseOfficialARAPQuestion(question) || containsAny(question, []string{"分别", "哪头", "更重"})
}

// shouldTreatAsCompanyOfficialARAPQuestion reports whether a question should be
// answered as company-scope official balance-sheet AR/AP without binding any
// hallucinated counterparty entity. A non-empty entity is only trusted when it
// names a real counterparty mentioned in the question — that is, a fragment of
// the entity survives in the question text AFTER the canonical balance-sheet
// account terms are removed. This rejects both leftover account fragments such
// as "和其他应" (a substring of "其他应付款") and fuzzy DB matches such as
// "对公中间业务收入-网上其他收入" that would otherwise hijack a company-level
// balance question into the wrong counterparty/audit path.
func shouldTreatAsCompanyOfficialARAPQuestion(question, entity string) bool {
	if strings.TrimSpace(entity) != "" && entityAppearsAsCounterparty(question, entity) {
		return false
	}
	if !looksLikeBalanceSheetARAPQuestion(question) {
		return false
	}
	if !strings.Contains(question, "应收") && !strings.Contains(question, "应付") &&
		!strings.Contains(question, "其他应付款") && !strings.Contains(question, "其他应收款") {
		return false
	}
	return true
}

// entityAppearsAsCounterparty reports whether the entity names a real
// counterparty that the user mentioned in the question. It first strips the
// canonical balance-sheet account terms from the question so that leftover
// fragments such as "和其他应" (a substring of "其他应付款") are not mistaken
// for real mentions, then checks whether a multi-character fragment of the
// entity survives in the remaining text.
func entityAppearsAsCounterparty(question, entity string) bool {
	name := strings.TrimSpace(entity)
	if len([]rune(name)) < 2 {
		return false
	}
	strippedQuestion := stripBalanceSheetAccountTerms(question)
	if strings.Contains(strippedQuestion, name) {
		return true
	}
	normalized := normalizeEntityText(name)
	if normalized == "" {
		return false
	}
	if len([]rune(normalized)) >= 2 && strings.Contains(stripBalanceSheetAccountTerms(normalizeEntityText(question)), normalized) {
		return true
	}
	return false
}

// stripBalanceSheetAccountTerms removes the canonical balance AR/AP account
// names from the text so that leftover fragments from entity extraction (for
// example "和其他应", a substring of "其他应付款") are not mistaken for real
// counterparty mentions.
func stripBalanceSheetAccountTerms(text string) string {
	terms := []string{
		"其他应付款", "其他应收款", "应收账款", "应付账款", "应收票据", "应付票据", "预付账款",
		"应收", "应付",
	}
	s := text
	for _, term := range terms {
		s = strings.ReplaceAll(s, term, "")
	}
	return s
}
func (e *Engine) queryCompanyOfficialARAP(period string) Result {
	if fallbackPeriod := e.latestARAPBalancePeriodAtOrBefore(period); fallbackPeriod != "" {
		period = fallbackPeriod
	}
	receivable := e.queryAccountPayableReceivable(period, "应收账款", "1122", "receivable", "")
	payable := e.queryAccountPayableReceivable(period, "应付账款", "2202", "payable", "")
	otherPayable := e.queryAccountPayableReceivable(period, "其他应付款", "2241", "other_payable", "")
	if !receivable.Success && !payable.Success && !otherPayable.Success {
		return Result{Success: false, Message: "该期间未找到应收/应付余额"}
	}

	receivableTotal := resultTotal(receivable)
	payableTotal := resultTotal(payable)
	otherPayableTotal := resultTotal(otherPayable)
	payableSideTotal := round2(payableTotal + otherPayableTotal)
	heavier := "应收端更重"
	if payableSideTotal > receivableTotal {
		heavier = "应付端更重"
	} else if approxEqual(payableSideTotal, receivableTotal) {
		heavier = "两边接近"
	}

	data := map[string]any{
		"type":                  "company_official_arap",
		"period":                period,
		"source":                "balance_sheet",
		"receivable_total":      receivableTotal,
		"payable_total":         payableTotal,
		"other_payable_total":   otherPayableTotal,
		"payable_side_total":    payableSideTotal,
		"heavier_side":          heavier,
		"receivable":            receivable.Data,
		"payable":               payable.Data,
		"other_payable":         otherPayable.Data,
		"source_tables":         []string{"fin_balance_sheet", "fin_balance_detail"},
		"source_primary_tables": []string{"fin_balance_sheet", "fin_balance_detail"},
		"query_spec_overrides": map[string]any{
			"period_from":       period,
			"period_to":         period,
			"time_scope":        TimeScopeMonth,
			"semantic_families": []string{"balance_ar_ap", "balance_sheet"},
		},
	}

	return Result{
		Success: true,
		Message: fmt.Sprintf("%s 账上挂账：应收账款 %.2f 元，应付账款 %.2f 元，其他应付款 %.2f 元；合计应付端 %.2f 元，%s。",
			period, receivableTotal, payableTotal, otherPayableTotal, payableSideTotal, heavier),
		Data:        data,
		ExecutedSQL: dedupeStrings(append(append(append([]string{}, receivable.ExecutedSQL...), payable.ExecutedSQL...), otherPayable.ExecutedSQL...)),
		CalculationLogs: append(append(append([]string{
			fmt.Sprintf("[公司AR/AP官方余额] period=%s receivable=%.2f payable=%.2f other_payable=%.2f payable_side=%.2f heavier=%s", period, receivableTotal, payableTotal, otherPayableTotal, payableSideTotal, heavier),
		}, receivable.CalculationLogs...), payable.CalculationLogs...), otherPayable.CalculationLogs...),
	}
}

func resultTotal(result Result) float64 {
	if !result.Success || result.Data == nil {
		return 0
	}
	return round2(anyToFloat64(result.Data["total"]))
}

func (e *Engine) latestARAPBalancePeriodAtOrBefore(period string) string {
	var latest string
	if strings.TrimSpace(period) == "" {
		_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_sheet
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND (account_name IN ('应收账款', '应付账款', '其他应付款') OR account_code LIKE '1122%' OR account_code LIKE '2202%' OR account_code LIKE '2241%')`,
			e.Company, e.Company).Scan(&latest)
		return latest
	}
	_ = e.db.QueryRow(`
SELECT COALESCE(MAX(period), '')
FROM balance_sheet
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND period <= ?
  AND (account_name IN ('应收账款', '应付账款', '其他应付款') OR account_code LIKE '1122%' OR account_code LIKE '2202%' OR account_code LIKE '2241%')`,
		e.Company, e.Company, period).Scan(&latest)
	return latest
}

func (e *Engine) queryEntityARAP(entity, period string) Result {
	receivable := e.queryAccountPayableReceivable(period, "应收账款", "1122", "receivable", entity)
	payable := e.queryAccountPayableReceivable(period, "应付账款", "2202", "payable", entity)
	if !receivable.Success && !payable.Success {
		return Result{Success: false, Message: fmt.Sprintf("[%s] 未找到应收/应付余额", entity)}
	}

	receivableTotal := 0.0
	receivableDetails := []map[string]any{}
	if receivable.Success {
		receivableTotal = anyToFloat64(receivable.Data["total"])
		receivableDetails = mapsFromAnySlice(receivable.Data["details"])
	}
	payableTotal := 0.0
	payableDetails := []map[string]any{}
	if payable.Success {
		payableTotal = anyToFloat64(payable.Data["total"])
		payableDetails = mapsFromAnySlice(payable.Data["details"])
	}
	inferencePrefix := ""
	if resultUsesInferredOpenItemSettlement(receivable) || resultUsesInferredOpenItemSettlement(payable) {
		inferencePrefix = "按开放项推断："
	}

	return Result{
		Success: true,
		Message: fmt.Sprintf("[%s] %s %s应收 %.2f 元，应付 %.2f 元", entity, period, inferencePrefix, receivableTotal, payableTotal),
		Data: map[string]any{
			"entity":           entity,
			"period":           period,
			"receivable_total": round2(receivableTotal),
			"payable_total":    round2(payableTotal),
			"receivable":       receivable.Data,
			"payable":          payable.Data,
			"details": map[string]any{
				"receivable": receivableDetails,
				"payable":    payableDetails,
			},
		},
		ExecutedSQL:     append(append([]string{}, receivable.ExecutedSQL...), payable.ExecutedSQL...),
		CalculationLogs: append(append([]string{}, receivable.CalculationLogs...), payable.CalculationLogs...),
	}
}

func (e *Engine) queryProjectARAP(entity, from, to string) Result {
	sqlTxt := `SELECT COALESCE(SUM(credit_amount), 0), COALESCE(SUM(debit_amount), 0) FROM bank_statement WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%') AND counterparty_name LIKE ? AND transaction_date BETWEEN ? AND ?`
	var inAmt, outAmt float64
	e.db.QueryRow(sqlTxt, e.Company, e.Company, "%"+entity+"%", from+"-01", monthEndDay(to)).Scan(&inAmt, &outAmt)
	receivable := math.Max(inAmt-outAmt, 0)
	payable := math.Max(outAmt-inAmt, 0)
	return Result{
		Success: true,
		Message: fmt.Sprintf("%s %s 项目应收 %.2f 元，应付 %.2f 元", to, entity, receivable, payable),
		Data: map[string]any{
			"entity": entity, "period": to, "receivable": receivable, "payable": payable,
		},
		ExecutedSQL: []string{
			fmt.Sprintf("queryProjectARAP: %s [args: %s, %s, %s]", sqlTxt, e.Company, "%"+entity+"%", from+"-01"),
		},
		CalculationLogs: []string{
			fmt.Sprintf("[项目应收应付] 收入=%.2f, 成本=%.2f, 应收=%.2f, 应付=%.2f", inAmt, outAmt, receivable, payable),
		},
	}
}
