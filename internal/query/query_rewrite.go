package query

import (
	"strings"
	"time"
)

type BossMetric string

const (
	BossMetricUnknown  BossMetric = "unknown"
	BossMetricRevenue  BossMetric = "revenue"
	BossMetricCost     BossMetric = "cost"
	BossMetricProfit   BossMetric = "profit"
	BossMetricReceipts BossMetric = "receipts"
	BossMetricPayments BossMetric = "payments"
	BossMetricInvoice  BossMetric = "invoice"
	BossMetricARAP     BossMetric = "arap"
	BossMetricTax      BossMetric = "tax"
	BossMetricHRCost   BossMetric = "hr_cost"
	BossMetricCashFlow BossMetric = "cash_flow"
	BossMetricHealth   BossMetric = "health"
)

type BossScope string

const (
	BossScopeUnknown  BossScope = "unknown"
	BossScopeCompany  BossScope = "company"
	BossScopeEntity   BossScope = "entity"
	BossScopeContract BossScope = "contract"
)

type BossGranularity string

const (
	BossGranularityUnknown   BossGranularity = "unknown"
	BossGranularityAggregate BossGranularity = "aggregate"
	BossGranularityDetail    BossGranularity = "detail"
	BossGranularityBreakdown BossGranularity = "breakdown"
	BossGranularityAnalysis  BossGranularity = "analysis"
	BossGranularityReconcile BossGranularity = "reconciliation"
	BossGranularityBalance   BossGranularity = "balance"
	BossGranularitySubperiod BossGranularity = "aggregate_with_subperiod"
)

type BossPerspective string

const (
	BossPerspectiveUnknown              BossPerspective = "unknown"
	BossPerspectiveContractFirst        BossPerspective = "boss_contract_first"
	BossPerspectiveExplicitCash         BossPerspective = "explicit_cash"
	BossPerspectiveOfficialThenEvidence BossPerspective = "official_then_evidence"
	BossPerspectiveFinancialAccount     BossPerspective = "financial_account"
)

const (
	BossSourceBankStatement = "bank_statement"
	BossSourceJournal       = "journal"
	BossSourceBalance       = "balance"
	BossSourceContract      = "contract"
)

type BossQueryRewrite struct {
	Metric              BossMetric
	Scope               BossScope
	Entity              string
	PeriodFrom          string
	PeriodTo            string
	SubPeriod           string
	Granularity         BossGranularity
	Perspective         BossPerspective
	SourceConstraint    string
	RequiresSourceProbe bool
}

func RewriteBossQuery(question string, anchor time.Time) BossQueryRewrite {
	return RewriteBossQueryWithConfig(question, anchor, getRuleConfig())
}

func RewriteBossQueryWithConfig(question string, anchor time.Time, cfg RuleConfig) BossQueryRewrite {
	q := NormalizeQuestion(question)
	from, to := ExtractPeriodWithNow(q, anchor)
	subPeriod, hasSubPeriod := extractReceiptSubPeriod(q, from, to)
	entity := extractNamedEntityFromQuestion(q)
	if looksLikeBossRewriteNonEntity(entity) {
		entity = ""
	}
	if shouldForceCompanyScopeContractAggregateWithConfig(q, cfg) {
		entity = ""
	}
	if shouldTreatAsCompanyOfficialARAPQuestion(q, entity) {
		entity = ""
	}
	metric := detectBossMetricWithConfig(q, cfg)
	perspective, sourceConstraint := detectBossPerspectiveAndSourceWithConfig(q, metric, cfg)
	scope := detectBossScopeWithConfig(q, entity, cfg)
	granularity := detectBossGranularity(q, metric, hasSubPeriod)

	return BossQueryRewrite{
		Metric:              metric,
		Scope:               scope,
		Entity:              entity,
		PeriodFrom:          from,
		PeriodTo:            to,
		SubPeriod:           subPeriod,
		Granularity:         granularity,
		Perspective:         perspective,
		SourceConstraint:    sourceConstraint,
		RequiresSourceProbe: shouldBossRewriteProbe(metric, perspective),
	}
}

func detectBossMetric(q string) BossMetric {
	return detectBossMetricWithConfig(q, getRuleConfig())
}

func detectBossMetricWithConfig(q string, cfg RuleConfig) BossMetric {
	switch {
	case shouldUsePreciseBalanceQuestion(q):
		return BossMetricUnknown
	case shouldUseCashOnHandBalanceQuestion(q):
		return BossMetricCashFlow
	case shouldUseContractMarginAnalysisQuestion(q, cfg):
		return BossMetricProfit
	case looksLikeRevenueInvoiceOpenQuestion(q):
		return BossMetricInvoice
	case isARAPQuestion(q):
		return BossMetricARAP
	case containsAny(q, []string{"回款", "到账", "收款"}):
		return BossMetricReceipts
	case containsAny(q, []string{"付款", "支付", "付了"}):
		return BossMetricPayments
	case containsAny(q, []string{"开票", "未开票", "发票"}):
		return BossMetricInvoice
	case containsAny(q, cfg.intentKeywordGroup(routerGroupHRCost)) || shouldUseHRBreakdown(q, cfg):
		return BossMetricHRCost
	case containsAny(q, cfg.intentKeywordGroup(string(IntentTaxQuery))):
		return BossMetricTax
	case containsAny(q, cfg.intentKeywordGroup(string(IntentARAPQuery))) || isOpeningPeriodQuestion(q):
		return BossMetricARAP
	case containsAny(q, cfg.intentKeywordGroup(routerGroupHealth)):
		return BossMetricHealth
	case containsAny(q, cfg.MetricKeywords(metricKeyProfit)):
		return BossMetricProfit
	case containsAny(q, cfg.MetricKeywords(metricKeyCost)):
		return BossMetricCost
	case containsAny(q, cfg.MetricKeywords(metricKeyRevenue)):
		return BossMetricRevenue
	case containsAny(q, []string{"现金流", "净现金流", "净增加", "净流入", "净流出", "实际支出", "实际到账"}):
		return BossMetricCashFlow
	default:
		return BossMetricUnknown
	}
}

func detectBossPerspectiveAndSource(q string, metric BossMetric) (BossPerspective, string) {
	return detectBossPerspectiveAndSourceWithConfig(q, metric, getRuleConfig())
}

func detectBossPerspectiveAndSourceWithConfig(q string, metric BossMetric, cfg RuleConfig) (BossPerspective, string) {
	switch {
	case shouldUsePreciseBalanceQuestion(q):
		return BossPerspectiveOfficialThenEvidence, BossSourceBalance
	case shouldUseCashOnHandBalanceQuestion(q):
		return BossPerspectiveOfficialThenEvidence, BossSourceBalance
	case containsAny(q, []string{"银行", "银行卡", "流水", "现金流", "实际到账", "实际支出", "现金口径"}):
		return BossPerspectiveExplicitCash, BossSourceBankStatement
	case shouldUseExplicitFinancialAccountQuestion(q):
		return BossPerspectiveFinancialAccount, BossSourceJournal
	case metric == BossMetricARAP && shouldUseContractFirstARAP(q):
		return BossPerspectiveContractFirst, ""
	case metric == BossMetricARAP:
		return BossPerspectiveOfficialThenEvidence, BossSourceBalance
	case metric == BossMetricTax || metric == BossMetricHRCost:
		return BossPerspectiveFinancialAccount, BossSourceJournal
	case metric == BossMetricProfit && shouldUseContractMarginAnalysisQuestion(q, cfg):
		return BossPerspectiveContractFirst, ""
	case metric == BossMetricProfit && containsAny(q, cfg.ContractSummaryKeywords()):
		return BossPerspectiveContractFirst, ""
	case isBossContractFirstMetric(metric):
		return BossPerspectiveContractFirst, ""
	default:
		return BossPerspectiveUnknown, ""
	}
}

func shouldUsePreciseBalanceQuestion(q string) bool {
	if !containsAny(q, []string{"余额", "期末", "期初", "截至"}) {
		return false
	}
	return containsAny(q, []string{"货币资金", "银行存款"})
}

func shouldUseCashOnHandBalanceQuestion(q string) bool {
	if shouldUseReconciliation(q) {
		return false
	}
	if !strings.Contains(q, "现金") {
		return false
	}
	if containsAny(q, []string{"现金流", "实际到账", "实际支出", "现金口径", "银行卡", "银行流水"}) {
		return false
	}
	return containsAny(q, []string{"账上", "现在", "还有", "余额", "结余", "存量", "年初", "多了", "少了", "剩"})
}

func shouldUseExplicitFinancialAccountQuestion(q string) bool {
	return containsAny(q, []string{
		"序时账", "序时帐", "凭证", "利润表", "财务账", "会计账", "报表口径", "账上",
		"科目余额", "发生额及余额", "余额表", "资产负债表",
	}) || looksLikeBalanceSheetARAPQuestion(q)
}

// looksLikeBalanceSheetARAPQuestion detects company-level balance sheet AR/AP questions
// like "从账上看，上个完整自然月的应收账款、应付账款和其他应付款分别有多少？".
// It requires a balance-sheet account term and either an explicit balance-sheet
// source/context word or a multi-account balance comparison shape.
func looksLikeBalanceSheetARAPQuestion(q string) bool {
	accountTermCount := 0
	for _, term := range []string{"应收账款", "应付账款", "其他应付款", "应收票据", "应付票据", "预付账款", "其他应收款"} {
		if strings.Contains(q, term) {
			accountTermCount++
		}
	}
	if accountTermCount == 0 {
		return false
	}
	if containsAny(q, []string{
		"从账上看", "账上看", "官方余额表", "余额表", "科目余额", "资产负债表", "报表口径",
		"期末余额", "期初余额", "发生额及余额",
	}) {
		return true
	}
	return accountTermCount >= 2 && containsAny(q, []string{"余额", "分别", "哪头", "更重", "合计"})
}

func detectBossScope(q, entity string) BossScope {
	return detectBossScopeWithConfig(q, entity, getRuleConfig())
}

func detectBossScopeWithConfig(q, entity string, cfg RuleConfig) BossScope {
	if shouldUseExpenseBreakdownWithConfig(q, cfg) {
		return BossScopeCompany
	}
	if shouldUseCompanyScopeContractAggregateWithConfig(q, cfg) {
		return BossScopeCompany
	}
	if strings.Contains(q, "合同") || (strings.Contains(q, "项目") && strings.TrimSpace(entity) != "") {
		return BossScopeContract
	}
	if strings.TrimSpace(entity) != "" {
		return BossScopeEntity
	}
	return BossScopeCompany
}

func looksLikeBossRewriteNonEntity(entity string) bool {
	normalized := normalizeEntityText(entity)
	if normalized == "" {
		return false
	}
	if looksLikeBusinessDimensionLabel(entity) {
		return true
	}
	if looksLikePeriodStateFragment(normalized) {
		return true
	}
	if looksLikeStateQuestionResidual(normalized) {
		return true
	}
	if looksLikeDistributionQuestionFragment(normalized) {
		return true
	}
	if looksLikeProjectAggregateSyntheticEntity(entity) {
		return true
	}
	return containsAny(normalized, []string{
		"银行卡", "银行", "实际", "到账", "回款", "收款", "付款", "现金", "现金流",
		"官方", "表中", "表里",
		"账上", "现在", "当前", "目前", "至今", "到现在", "起至今", "最新", "最新可见月份", "可见月份", "最近完整月份", "最近一个完整月份", "最近一个完整月", "最新完整月份", "最新一个完整月份", "最新一个完整月", "上一个完整自然月", "上个完整自然月", "完整自然月", "完整月份", "完整月", "一个完整", "一个", "月底", "还有", "年初", "多了", "少了",
		"老板", "帮我", "查一下", "看下", "看一下",
		"应收", "应付", "账款", "开票", "收票", "发票", "未付", "未回", "未收",
		"挂着", "还挂", "挂账", "哪头", "更重",
		"主要靠", "依赖", "太依赖", "集中", "集中度", "占比", "排名", "前几", "最多", "最大", "某一两家", "一两家",
		"整体", "大类", "构成", "分类", "类别", "支出", "费用", "开支",
		"金额", "结算", "利润", "毛利", "净利", "口径", "合计", "总计", "列一下",
	})
}

func looksLikeProjectAggregateSyntheticEntity(entity string) bool {
	normalized := normalizeEntityText(entity)
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "年至") && strings.Contains(normalized, "未") {
		return true
	}
	if strings.Contains(normalized, "到") && strings.Contains(normalized, "未") && strings.Contains(normalized, "年") {
		return true
	}
	return false
}

func detectBossGranularity(q string, metric BossMetric, hasSubPeriod bool) BossGranularity {
	switch {
	case hasSubPeriod:
		return BossGranularitySubperiod
	case shouldUseReconciliation(q):
		return BossGranularityReconcile
	case metric == BossMetricARAP || containsAny(q, []string{"期初", "期末", "余额"}):
		return BossGranularityBalance
	case containsAny(q, []string{"明细", "列表", "每笔", "逐笔"}):
		return BossGranularityDetail
	case containsAny(q, []string{"拆", "拆分", "拆开", "分别", "构成"}):
		return BossGranularityBreakdown
	case metric == BossMetricHealth || containsAny(q, []string{"分析", "建议", "风险", "健康"}):
		return BossGranularityAnalysis
	default:
		return BossGranularityAggregate
	}
}

func shouldBossRewriteProbe(metric BossMetric, perspective BossPerspective) bool {
	if perspective == BossPerspectiveExplicitCash {
		return false
	}
	return perspective == BossPerspectiveContractFirst || isBossContractFirstMetric(metric)
}

func isBossContractFirstMetric(metric BossMetric) bool {
	switch metric {
	case BossMetricRevenue, BossMetricCost, BossMetricReceipts, BossMetricPayments, BossMetricInvoice, BossMetricARAP:
		return true
	default:
		return false
	}
}
