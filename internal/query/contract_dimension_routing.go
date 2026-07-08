package query

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

func shouldUseContractDimension(question string) bool {
	return shouldUseContractDimensionWithConfig(question, getRuleConfig())
}

func shouldUseContractDimensionWithConfig(question string, cfg RuleConfig) bool {
	q := strings.TrimSpace(question)
	if !looksLikeContractDimensionSubject(q) {
		return false
	}
	if shouldForceCompanyScopeContractAggregateWithConfig(q, cfg) {
		return false
	}
	entity := extractNamedEntityFromQuestion(q)
	if looksLikeBossRewriteNonEntity(entity) {
		entity = ""
	}
	hasEntity := isRealishQueryEntity(entity)
	hasSpecificLexicalSubject := looksLikeSpecificContractDimensionSubject(q)
	hasOrganizationSubject := strings.TrimSpace(extractOrganizationEntityMatch(q)) != ""
	if !hasSpecificLexicalSubject && !hasOrganizationSubject && !hasEntity && looksLikeCompanyScopeProjectAggregateQuestion(q) {
		return false
	}
	hasSpecificSubject := hasSpecificLexicalSubject || hasOrganizationSubject || hasEntity
	if shouldUseContractAggregateAnalysisQuestion(q, cfg) && !hasEntity {
		return false
	}
	if !hasEntity && shouldUseCompanyScopeContractAggregateWithConfig(q, cfg) {
		return false
	}
	if !hasSpecificSubject {
		return false
	}
	if contractAskedTopicTriggersDimension(inferContractAskedTopic(q)) {
		return true
	}
	return containsAny(q, contractPriorityKeywordsWithConfig(cfg))
}

func looksLikeContractDimensionSubject(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	return containsAny(q, []string{"合同", "协议", "项目"}) ||
		hasContractContentCodeSubject(q) ||
		looksLikeContractCounterpartyOperatingSubject(q)
}

func looksLikeContractCounterpartyOperatingSubject(q string) bool {
	if shouldUseExplicitFinancialAccountQuestion(q) || isCounterpartyClassificationQuestion(q) {
		return false
	}
	if !containsAny(q, []string{"客户", "供应商"}) {
		return false
	}
	switch inferContractAskedTopic(q) {
	case "receivable", "payable", "invoice", "revenue", "cost", "profit":
		return true
	default:
		return false
	}
}

func looksLikeSpecificContractDimensionSubject(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	return strings.Contains(q, "协议") || hasContractContentCodeSubject(q) || hasNamedContractPhrase(q)
}

func looksLikeCompanyScopeProjectAggregateQuestion(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" || shouldUseExplicitFinancialAccountQuestion(q) {
		return false
	}
	if containsAny(q, []string{
		"所有项目", "全部项目", "全量项目",
		"项目口径", "项目成本口径", "按项目口径", "按项目成本口径", "从项目口径",
		"项目应收", "项目应付", "项目结算", "项目营收", "项目收入",
		"未付款的项目", "没有付款的项目", "未支付的项目", "没付款的项目",
		"项目及对应金额", "项目和金额", "项目金额",
		"哪些项目", "有哪些项目", "每个项目", "项目分别", "项目清单", "项目列表",
	}) {
		return true
	}
	if looksLikeProjectPayableUnpaidQuestion(q) {
		return containsAny(q, []string{
			"合计", "总计", "一共", "多少", "有哪些", "哪些", "列一下", "分别",
			"对应金额", "金额各", "金额是多少",
		})
	}
	return false
}

func hasNamedContractPhrase(q string) bool {
	for _, idx := range markerIndexes(q, "合同") {
		prefix := stripKnownPeriodTokens(q[:idx])
		prefix = strings.Trim(prefix, " \t\n，,。；;：:的从按看")
		if len([]rune(normalizeEntityText(prefix))) < 2 {
			continue
		}
		if looksLikeBossRewriteNonEntity(prefix) || shouldSkipEntityFragment(prefix, 2) {
			continue
		}
		if containsAny(normalizeEntityText(prefix), []string{"所有", "全部", "全量", "整体", "项目口径"}) {
			continue
		}
		return true
	}
	return false
}

func markerIndexes(s, marker string) []int {
	indexes := make([]int, 0, 2)
	offset := 0
	for {
		idx := strings.Index(s[offset:], marker)
		if idx < 0 {
			return indexes
		}
		absolute := offset + idx
		indexes = append(indexes, absolute)
		offset = absolute + len(marker)
	}
}

func hasContractContentCodeSubject(q string) bool {
	for _, match := range contractContentCodePattern.FindAllString(strings.TrimSpace(q), -1) {
		upper := strings.ToUpper(match)
		if regexp.MustCompile(`^Q[1-4]$`).MatchString(upper) {
			continue
		}
		digitCount := 0
		for _, r := range upper {
			if r >= '0' && r <= '9' {
				digitCount++
			}
		}
		if digitCount >= 2 {
			return true
		}
	}
	return false
}

func contractAskedTopicTriggersDimension(askedTopic string) bool {
	switch askedTopic {
	case "receivable", "payable", "invoice", "revenue", "receipts", "payments", "cost", "profit":
		return true
	default:
		return false
	}
}

func shouldUseContractDetailQuestion(question string) bool {
	q := strings.TrimSpace(question)
	if !containsAny(q, []string{"合同", "协议", "发票"}) {
		return false
	}
	if shouldUseCompanyScopeContractAggregate(q) {
		return false
	}
	detailKeywords := []string{
		"条款", "细节", "正文", "原文", "全文", "具体内容", "合同内容", "内容是什么", "写了什么", "合同里", "协议里",
		"付款条款", "付款方式", "结算周期", "结算方式", "服务范围", "服务内容",
		"交付", "验收", "保密", "违约", "税率", "签署", "签约", "起止", "到期",
		"续约", "第几页", "哪一页", "发票金额", "发票明细", "发票号码", "发票号",
		"发票内容", "发票里", "这张发票", "开票日期", "票面金额", "不含税", "含税", "税额", "购买方", "销售方",
	}
	if !containsAny(q, detailKeywords) {
		return false
	}
	operatingKeywords := []string{"收入", "营收", "成本", "利润", "回款", "到账", "付款", "应收", "应付", "未回款", "未付款", "已开票", "未支付"}
	if containsAny(q, operatingKeywords) && !containsAny(q, detailKeywords) {
		return false
	}
	return true
}

func shouldUseCompanyScopeContractAggregate(question string) bool {
	return shouldUseCompanyScopeContractAggregateWithConfig(question, getRuleConfig())
}

func shouldUseCompanyScopeContractAggregateWithConfig(question string, cfg RuleConfig) bool {
	q := strings.TrimSpace(question)
	if shouldUseContractAggregateAnalysisQuestion(q, cfg) {
		return true
	}
	if !containsAny(q, []string{"合同", "项目"}) {
		return false
	}
	if shouldForceCompanyScopeContractAggregateWithConfig(q, cfg) {
		return true
	}
	if looksLikeCompanyScopeProjectAggregateQuestion(q) {
		return true
	}
	entity := extractNamedEntityFromQuestion(q)
	if looksLikeBossRewriteNonEntity(entity) {
		entity = ""
	}
	if isRealishQueryEntity(entity) {
		return false
	}
	if shouldUseExplicitFinancialAccountQuestion(q) {
		return false
	}
	metric := detectBossMetricWithConfig(q, cfg)
	if isBossContractFirstMetric(metric) {
		return true
	}
	return containsAny(q, []string{
		"结算", "执行", "情况", "多少", "有哪些", "哪些", "明细", "列表", "分别", "汇总", "统计",
	})
}

func shouldForceCompanyScopeContractAggregate(question string) bool {
	return shouldForceCompanyScopeContractAggregateWithConfig(question, getRuleConfig())
}

func shouldForceCompanyScopeContractAggregateWithConfig(question string, cfg RuleConfig) bool {
	q := strings.TrimSpace(question)
	if q == "" || shouldUseExplicitFinancialAccountQuestion(q) {
		return false
	}
	if containsAny(q, []string{"所有", "全部", "全量"}) && looksLikeProjectPayableUnpaidQuestion(q) {
		return true
	}
	if !containsAny(q, []string{
		"所有项目", "全部项目", "全量项目",
		"所有合同", "全部合同", "全量合同",
	}) {
		return false
	}
	metric := detectBossMetricWithConfig(q, cfg)
	if isBossContractFirstMetric(metric) {
		return true
	}
	return containsAny(q, []string{
		"结算", "执行", "情况", "多少", "有哪些", "哪些", "明细", "列表", "分别", "汇总", "统计",
	})
}

func contractPriorityKeywords() []string {
	return contractPriorityKeywordsWithConfig(getRuleConfig())
}

func contractPriorityKeywordsWithConfig(cfg RuleConfig) []string {
	return cfg.ContractPriorityKeywords()
}

func isContractPriorityQuestion(question string) bool {
	return isContractPriorityQuestionWithConfig(question, getRuleConfig())
}

func isContractPriorityQuestionWithConfig(question string, cfg RuleConfig) bool {
	q := strings.TrimSpace(question)
	return containsAny(q, contractPriorityKeywordsWithConfig(cfg))
}

func extractContractBaseQuestion(question string) string {
	q := strings.TrimSpace(question)
	if idx := strings.Index(q, "其中"); idx >= 0 {
		q = strings.TrimSpace(q[:idx])
	}
	return strings.TrimSpace(strings.TrimRight(q, "，,。；;？?"))
}

func extractContractQuestionPeriods(question string, anchor time.Time) (string, string) {
	baseQuestion := extractContractBaseQuestion(question)
	if year, ok := extractExplicitStandaloneYear(baseQuestion); ok {
		return fmt.Sprintf("%04d-01", year), fmt.Sprintf("%04d-12", year)
	}
	return ExtractPeriodWithNow(baseQuestion, anchor)
}

func extractExplicitStandaloneYear(question string) (int, bool) {
	q := strings.TrimSpace(question)
	if q == "" {
		return 0, false
	}
	if strings.Contains(q, "今年") || strings.Contains(q, "本年") {
		return 0, false
	}
	specificPeriodPatterns := []*regexp.Regexp{
		regexp.MustCompile(`20\d{2}年\s*(?:到|至|-|~)\s*20\d{2}年`),
		regexp.MustCompile(`20\d{2}年\s*(?:上半年|下半年|全年|整年|全年度|年度|累计|年内)`),
		regexp.MustCompile(`20\d{2}年\s*(?:第?\s*[一二三四1234]\s*季度|Q\s*[1-4])`),
		regexp.MustCompile(`20\d{2}年\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月`),
		regexp.MustCompile(`20\d{2}年\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?\s*(?:到|至|-|~)`),
	}
	for _, pattern := range specificPeriodPatterns {
		if pattern.MatchString(q) {
			return 0, false
		}
	}
	m := regexp.MustCompile(`(20\d{2})年`).FindStringSubmatch(q)
	if len(m) != 2 {
		return 0, false
	}
	return mustAtoi(m[1]), true
}
