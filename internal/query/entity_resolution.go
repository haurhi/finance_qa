package query

import (
	"regexp"
	"strings"
)

var namedEntityPattern = regexp.MustCompile(`([A-Za-z0-9_\-\(\)（）\x{4e00}-\x{9fa5}]{2,})(?:客户|供应商|公司|项目|单位|人|报销|报账|支出|往来|金|账|款|明细)`)
var organizationEntityPattern = regexp.MustCompile(`([A-Za-z0-9_\-\(\)（）\x{4e00}-\x{9fa5}]{2,}(?:有限责任公司|有限公司|分公司|事务所|管理中心|研究院|学院|大学|中心|公司))`)
var temporalNoisePattern = regexp.MustCompile(`20\d{2}年?|[0-3]?\d月|[0-3]?\d日`)
var chineseEntitySegmentPattern = regexp.MustCompile(`[\x{4e00}-\x{9fa5}]+`)

func (e *Engine) extractNamedEntity(question string) string {
	q := strings.TrimSpace(question)
	if c := e.matchCounterpartyByName(q); c != "" {
		return c
	}
	if entity := extractOrganizationEntityMatch(q); len(entity) >= 2 && !isGenericMetricEntity(entity) {
		return entity
	}
	if entity := e.extractNamedEntityPatternMatch(q); len(entity) >= 2 && !isGenericMetricEntity(entity) {
		return entity
	}
	if c := e.matchCounterpartyByAliasSegment(q); c != "" {
		return c
	}

	best := ""
	for _, seg := range chineseEntitySegmentPattern.FindAllString(q, -1) {
		runes := []rune(seg)
		for length := len(runes); length >= 2; length-- {
			for i := 0; i <= len(runes)-length; i++ {
				sub := string(runes[i : i+length])
				if shouldSkipEntityFragment(sub, 2) {
					continue
				}
				if candidates := e.resolveCounterpartyCandidates(sub); len(candidates) > 0 {
					return candidates[0]
				}
				var exists int
				e.db.QueryRow(`SELECT 1 FROM bank_statement WHERE counterparty_name LIKE ? LIMIT 1`, "%"+sub+"%").Scan(&exists)
				if exists == 0 {
					e.db.QueryRow(`SELECT 1 FROM journal WHERE summary LIKE ? OR counterparty LIKE ? LIMIT 1`, "%"+sub+"%", "%"+sub+"%").Scan(&exists)
				}
				if exists == 0 {
					e.db.QueryRow(`SELECT 1 FROM fin_contracts WHERE customer_name LIKE ? OR contract_content LIKE ? LIMIT 1`, "%"+sub+"%", "%"+sub+"%").Scan(&exists)
				}
				if exists == 1 && len(sub) > len(best) {
					best = sub
				}
			}
		}
	}
	best = trimEntityNoiseSuffixes(best)
	if best != "" && !isGenericMetricEntity(best) {
		return best
	}
	return ""
}

func (e *Engine) extractNamedEntityPatternMatch(question string) string {
	if m := namedEntityPattern.FindStringSubmatch(strings.TrimSpace(question)); len(m) == 2 {
		raw := trimEntityNoiseSuffixes(strings.TrimSpace(stripTemporalNoise(m[1])))
		if containsAny(raw, []string{"这个主体", "更像", "目前", "还是", "请给判断依据", "这笔"}) {
			raw = trimEntityNoiseSuffixes(stripQuestionFragments(raw))
		}
		if resolved := e.resolveEntityByPrefix(raw); resolved != "" {
			return resolved
		}
		return trimEntityNoiseSuffixes(stripQuestionFragments(raw))
	}
	return ""
}

func extractOrganizationEntityMatch(question string) string {
	matches := organizationEntityPattern.FindAllString(strings.TrimSpace(question), -1)
	best := ""
	for _, match := range matches {
		match = trimEntityNoiseSuffixes(strings.TrimSpace(stripTemporalNoise(match)))
		if len([]rune(match)) >= len([]rune(best)) {
			best = match
		}
	}
	return best
}

func stripQuestionFragments(entity string) string {
	trimmed := strings.TrimSpace(entity)
	if trimmed == "" {
		return ""
	}
	noiseTokens := []string{
		"这个主体", "更像", "目前", "还是", "请给判断依据", "这笔",
		"今年", "其中", "一共", "总计", "多少",
		"回款", "到账", "收款", "付款",
		"收入", "支出", "费用", "成本", "利润", "营收", "销售额", "GMV", "gmv",
	}
	for _, token := range noiseTokens {
		trimmed = strings.ReplaceAll(trimmed, token, "")
	}
	return strings.TrimSpace(trimmed)
}

func (e *Engine) resolveEntityByPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	for end := len(runes); end >= 2; end-- {
		candidate := trimEntityNoiseSuffixes(strings.TrimSpace(string(runes[:end])))
		if len([]rune(candidate)) < 2 || isGenericMetricEntity(candidate) || looksLikeTemporalMetricEntity(candidate) {
			continue
		}
		if matches := e.resolveCounterpartyCandidates(candidate); len(matches) > 0 {
			return matches[0]
		}
		if e.entityExistsInSources(candidate) {
			return candidate
		}
	}
	return ""
}

func (e *Engine) entityExistsInSources(name string) bool {
	like := "%" + strings.TrimSpace(name) + "%"
	if like == "%%" {
		return false
	}
	var exists int
	e.db.QueryRow(`SELECT 1 FROM bank_statement WHERE counterparty_name LIKE ? LIMIT 1`, like).Scan(&exists)
	if exists == 1 {
		return true
	}
	e.db.QueryRow(`SELECT 1 FROM journal WHERE summary LIKE ? OR counterparty LIKE ? LIMIT 1`, like, like).Scan(&exists)
	if exists == 1 {
		return true
	}
	e.db.QueryRow(`SELECT 1 FROM fin_contracts WHERE customer_name LIKE ? OR contract_content LIKE ? LIMIT 1`, like, like).Scan(&exists)
	return exists == 1
}

func (e *Engine) isRealBusinessEntity(question, entity string) bool {
	name := strings.TrimSpace(entity)
	if len([]rune(name)) < 2 || isGenericMetricEntity(name) || looksLikeTemporalMetricEntity(name) || looksLikeBusinessDimensionLabel(name) {
		return false
	}
	if strings.Contains(question, "项目") {
		return true
	}

	like := "%" + name + "%"
	var exists int
	e.db.QueryRow(`SELECT 1 FROM bank_statement WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%') AND counterparty_name LIKE ? LIMIT 1`, e.Company, e.Company, like).Scan(&exists)
	if exists == 1 {
		return true
	}
	e.db.QueryRow(`SELECT 1 FROM journal WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%') AND (summary LIKE ? OR (IFNULL(TRIM(counterparty),'') <> '' AND counterparty LIKE ?)) LIMIT 1`, e.Company, e.Company, like, like).Scan(&exists)
	if exists == 1 {
		return true
	}
	e.db.QueryRow(`SELECT 1 FROM fin_contracts WHERE customer_name LIKE ? OR contract_content LIKE ? LIMIT 1`, like, like).Scan(&exists)
	return exists == 1
}

func looksLikeTemporalMetricEntity(entity string) bool {
	normalized := strings.ToLower(normalizeEntityText(entity))
	if normalized == "" {
		return false
	}
	if looksLikePeriodOnlyEntity(entity) {
		return true
	}
	if looksLikeLooseYearRangeEntity(entity) {
		return true
	}
	if regexp.MustCompile(`^q[1-4]$`).MatchString(normalized) {
		return true
	}
	if regexp.MustCompile(`^(?:20)?\d{2}q[1-4]$`).MatchString(normalized) {
		return true
	}
	temporalKeywords := []string{
		"第一", "第二", "第三", "第四",
		"q1", "q2", "q3", "q4",
		"季度", "第一季度", "第二季度", "第三季度", "第四季度",
		"上半年", "下半年", "全年", "全年度", "整年", "年度",
		"今年", "本年", "去年", "累计", "年内",
		"本月", "这个月", "当月", "上个月", "上月", "上一个完整自然月", "最新月份", "最新的月份",
	}
	for _, keyword := range temporalKeywords {
		if strings.ToLower(normalizeEntityText(keyword)) == normalized {
			return true
		}
	}
	return false
}
