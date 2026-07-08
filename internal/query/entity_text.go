package query

import (
	"regexp"
	"strings"
)

func normalizeEntityText(s string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "（", "", "）", "", "(", "", ")", "", "-", "", "_", "", ",", "", "，", "", ".", "", "。", "")
	return replacer.Replace(strings.TrimSpace(s))
}

func stripTemporalNoise(entity string) string {
	return strings.TrimSpace(compactSpaces(stripKnownPeriodTokens(entity)))
}

func stripKnownPeriodTokens(text string) string {
	out := strings.TrimSpace(text)
	if out == "" {
		return ""
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:20)?\d{2}\s*(?:年)?\s*q\s*[1-4]`),
		regexp.MustCompile(`(?i)(?:20)?\d{2}\s*年\s*第?\s*[一二三四1234]\s*季度`),
		regexp.MustCompile(`(?i)(?:20)?\d{2}\s*年\s*(?:上半年|下半年|全年|全年度|整年|年度|累计|年内)`),
		regexp.MustCompile(`(?i)(?:20)?\d{2}\s*年\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})\s*月`),
		regexp.MustCompile(`20\d{2}年?`),
		regexp.MustCompile(`[0-3]?\d月`),
		regexp.MustCompile(`[0-3]?\d日`),
	}
	for _, pattern := range patterns {
		out = pattern.ReplaceAllString(out, " ")
	}
	return strings.TrimSpace(out)
}

func compactSpaces(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func looksLikeIsolatedYearToken(entity string) bool {
	normalized := strings.ToLower(strings.TrimSpace(entity))
	if normalized == "" {
		return false
	}
	return regexp.MustCompile(`^(?:20)?\d{2}年?$`).MatchString(normalized)
}

func looksLikePeriodOnlyEntity(entity string) bool {
	trimmed := strings.TrimSpace(entity)
	if trimmed == "" {
		return false
	}
	if looksLikeLooseYearRangeEntity(trimmed) {
		return true
	}
	stripped := stripKnownPeriodTokens(trimmed)
	if strings.TrimSpace(stripped) == "" && strings.TrimSpace(stripped) != strings.TrimSpace(trimmed) {
		return true
	}
	return looksLikeIsolatedYearToken(trimmed)
}

func looksLikeLooseYearRangeEntity(entity string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(entity)), ""))
	if compact == "" {
		return false
	}
	return regexp.MustCompile(`^(?:20)?\d{2}年?(?:到|至|~|-)(?:20)?\d{2}年?$`).MatchString(compact)
}

func trimEntityNoiseSuffixes(entity string) string {
	return trimEntityNoiseSuffixesWithConfig(entity, getRuleConfig())
}

func trimEntityNoiseSuffixesWithConfig(entity string, cfg RuleConfig) string {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return ""
	}
	suffixes := append([]string{
		"报销了", "报销", "报账", "到账", "回款", "收款", "付款",
		"费用", "支出", "收入", "成本", "利润", "明细", "金额",
		"产生了", "产生", "多少", "报",
	}, cfg.GenericMetricStopwords...)
	for {
		trimmed := entity
		for _, suffix := range suffixes {
			suffix = strings.TrimSpace(suffix)
			if suffix == "" {
				continue
			}
			if strings.HasSuffix(trimmed, suffix) {
				trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
			}
		}
		if trimmed == entity {
			return trimmed
		}
		entity = trimmed
	}
}

func shouldSkipEntityFragment(fragment string, minLen int) bool {
	return shouldSkipEntityFragmentWithConfig(fragment, minLen, getRuleConfig())
}

func shouldSkipEntityFragmentWithConfig(fragment string, minLen int, cfg RuleConfig) bool {
	if len([]rune(fragment)) < minLen {
		return true
	}
	if isGenericMetricEntityWithConfig(fragment, cfg) || looksLikeTemporalMetricEntity(fragment) || looksLikeBusinessDimensionLabel(fragment) {
		return true
	}
	return containsAny(fragment, []string{
		"帮我", "一下", "查询", "多少", "哪些", "价格", "一共", "支出", "报销",
		"起至今", "至今", "到现在", "当前", "目前",
		"上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份", "上一个完整月", "上个完整月", "上个", "到上", "到上个",
		"最新", "最新可见月份", "可见月份", "最新一个完整月份", "最新一个完整月", "最近完整月份", "最近一个完整月份", "最近一个完整月",
		"完整月份", "完整月", "一个完整", "一个",
		"对应金额", "对应",
		"主要靠", "依赖", "太依赖", "集中", "集中度", "占比", "排名", "前几", "最多", "最大", "某一两家", "一两家",
		"经营", "分析", "风险", "健康", "评价", "应收", "应付", "账款", "费用",
		"资金", "货币", "流水", "工资", "社保", "公积金", "人力成本", "薪酬",
		"营收", "收入", "成本", "利润", "季度", "半年", "全年", "年度", "累计", "年内",
	})
}
