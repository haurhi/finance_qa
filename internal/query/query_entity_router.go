package query

import (
	"regexp"
	"strings"
)

func extractNamedEntityFromQuestion(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		return ""
	}
	candidate := q
	replacements := []string{
		"帮我查一下", "帮我查", "查一下", "查看", "看一下", "看下", "有哪些", "哪些", "哪个", "哪几个", "哪几家",
		"今年", "本年", "累计", "年内", "至今", "到现在", "当前", "目前",
		"上一个完整自然月", "上个完整自然月", "上一个完整月份", "上个完整月份", "上一个完整月", "上个完整月",
		"最新一个完整月份", "最新一个完整月", "最新完整月份", "最新月份", "最新可见月份", "最新",
		"可见月份", "最近一个完整月份", "最近一个完整月", "最近完整月份", "最近月份",
		"完整月份", "完整月", "一个完整", "一个", "当月", "月份", "其中", "到账", "回款", "收款", "付款", "支付", "收入", "营收", "销售额", "GMV", "gmv",
		"成本", "利润", "应收账款", "应付账款", "应收", "应付", "未付款", "未支付", "未付", "还有", "数据出来了吗", "数据出来了没", "数据出来", "多少", "是什么", "分别",
		"主要靠", "依赖", "太依赖", "集中", "集中度", "占比", "排名", "前几", "最多", "最大", "某一两家", "一两家", "会不会",
		"合同", "项目", "项目口径", "项目结算", "结算", "情况", "余额", "明细", "数据", "账上", "财务账", "会计账", "科目余额", "余额表", "资产负债表", "报表口径", "期初", "期末",
		"3月", "2月", "1月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月",
		"第一季度", "第二季度", "第三季度", "第四季度", "季度", "上半年", "下半年", "全年", "全年度", "整年", "年度", "Q1", "Q2", "Q3", "Q4", "q1", "q2", "q3", "q4",
	}
	for _, token := range replacements {
		candidate = strings.ReplaceAll(candidate, token, " ")
	}
	candidate = regexp.MustCompile(`20\d{2}年`).ReplaceAllString(candidate, " ")
	candidate = regexp.MustCompile(`[\?\?,，。；;！!（）()\s]+`).ReplaceAllString(candidate, " ")
	parts := regexp.MustCompile(`[\x{4e00}-\x{9fa5}A-Za-z0-9()（）]+`).FindAllString(candidate, -1)
	for _, part := range parts {
		part = trimEntityNoiseSuffixes(stripTemporalNoise(strings.TrimSpace(part)))
		if !isRealishQueryEntity(part) || looksLikeAccountFragment(part) {
			continue
		}
		return part
	}
	if m := namedEntityPattern.FindStringSubmatch(q); len(m) == 2 {
		entity := trimEntityNoiseSuffixes(stripTemporalNoise(strings.TrimSpace(m[1])))
		if len([]rune(entity)) >= 2 && !isGenericMetricEntity(entity) && !looksLikeFinancialStateFragment(entity) && !looksLikeAccountFragment(entity) && !looksLikeSyntheticQuestionFragment(entity) && !looksLikeBusinessDimensionLabel(entity) {
			return entity
		}
	}
	return ""
}

func isRealishQueryEntity(entity string) bool {
	trimmed := strings.TrimSpace(entity)
	return len([]rune(trimmed)) >= 2 &&
		!isGenericMetricEntity(trimmed) &&
		!looksLikeFinancialStateFragment(trimmed) &&
		!looksLikeTemporalMetricEntity(trimmed) &&
		!looksLikePeriodOnlyEntity(trimmed) &&
		!looksLikeBusinessDimensionLabel(trimmed) &&
		!looksLikeSyntheticQuestionFragment(trimmed)
}

func looksLikeAccountFragment(entity string) bool {
	return containsAny(entity, []string{"应收", "应付", "账款", "余额", "情况", "明细", "数据"})
}

func looksLikeFinancialStateFragment(entity string) bool {
	normalized := normalizeEntityText(entity)
	if normalized == "" {
		return false
	}
	if !containsAny(normalized, []string{"开票", "收票", "发票", "付款", "回款", "收款", "支付", "未付", "未回", "未收"}) {
		return false
	}
	replacer := strings.NewReplacer(
		"已开票", "", "未开票", "", "开票", "",
		"已收票", "", "未收票", "", "收票", "",
		"发票", "",
		"未付款", "", "已付款", "", "未付", "", "已付", "", "付款", "", "付", "",
		"未回款", "", "已回款", "", "未回", "", "已回", "", "回款", "", "回", "",
		"未收款", "", "已收款", "", "未收", "", "已收", "", "收款", "", "收", "",
		"未支付", "", "已支付", "", "支付", "",
		"已", "", "未", "", "的", "",
	)
	residual := strings.TrimSpace(replacer.Replace(normalized))
	if residual == "" {
		return true
	}
	return containsString([]string{"供应商", "客户", "合同", "项目", "款项", "金额"}, residual)
}

func looksLikeSyntheticQuestionFragment(entity string) bool {
	normalized := normalizeEntityText(entity)
	if normalized == "" {
		return false
	}
	questionFragments := []string{
		"有哪些", "哪些", "哪个", "哪几个", "哪几家", "帮我查", "查一下",
		"查看", "看一下", "看下",
		"起至今", "至今", "到现在", "当前", "目前", "最新", "最新月份", "最新完整月份", "最新可见月份", "可见月份", "最近完整月份", "当月", "月份",
		"还有", "未付款", "未支付", "未付",
		"主要靠", "依赖", "集中", "集中度", "占比", "排名", "前几", "最多", "最大", "某一两家", "一两家",
		"单笔", "最大", "最小", "来自谁", "是谁", "多少", "金额", "对应金额", "对应", "是多少", "什么",
		"流入", "流出", "到账", "回款", "收款", "付款",
		"税额", "销项", "进项", "净税额",
	}
	matchCount := 0
	for _, fragment := range questionFragments {
		if strings.Contains(normalized, normalizeEntityText(fragment)) {
			matchCount++
		}
	}
	return matchCount >= 1
}
