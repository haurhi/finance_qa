package query

type executionStagePlanBuilder struct {
	stages []executionStage
}

func newExecutionStagePlanBuilder(capacity int) *executionStagePlanBuilder {
	if capacity < 1 {
		capacity = 1
	}
	return &executionStagePlanBuilder{stages: make([]executionStage, 0, capacity)}
}

func (b *executionStagePlanBuilder) add(stage executionStage) {
	for _, existing := range b.stages {
		if existing == stage {
			return
		}
	}
	b.stages = append(b.stages, stage)
}

func (b *executionStagePlanBuilder) addAll(stages ...executionStage) {
	for _, stage := range stages {
		b.add(stage)
	}
}

func (b *executionStagePlanBuilder) stagesOrEmpty() []executionStage {
	return append([]executionStage{}, b.stages...)
}

func resolveOperationalExecutionStages(ctx queryExecutionContext) []executionStage {
	stages := make([]executionStage, 0, 2)
	if shouldUseFinanceHealthQuestion(ctx.q) {
		stages = append(stages, executionStageFinanceHealth)
	}
	if shouldUseExpenseBreakdownWithConfig(ctx.q, ctx.cfg) {
		stages = append(stages, executionStageExpenseBreakdown)
	}
	if ctx.spec.QueryFamily == QueryFamilyHRCost || shouldUseHRBreakdown(ctx.q, ctx.cfg) {
		stages = append(stages, executionStageHRBreakdown)
	}
	return stages
}

func resolveSourceExecutionStages(ctx queryExecutionContext) []executionStage {
	builder := newExecutionStagePlanBuilder(4)
	if shouldUseDirectBankCashFlow(ctx) {
		builder.add(executionStageDirectBankCashFlow)
	}
	if shouldUseDirectCashOnHandBalance(ctx) {
		builder.add(executionStageDirectCashOnHandBalance)
	}
	if shouldUseDirectPreciseBalance(ctx) {
		builder.add(executionStageDirectPreciseBalance)
	}
	if shouldUseDirectAccrualCoreMetric(ctx) {
		builder.add(executionStageDirectAccrualCoreMetric)
	}
	if ctx.spec.QueryFamily == QueryFamilyARAP {
		builder.add(executionStageDirectARAP)
	}
	if shouldUseOrchestratorForSpec(ctx.spec) {
		builder.add(executionStageOrchestrator)
	}
	builder.addAll(resolveLegacySourceFallbackStages(ctx)...)
	return builder.stagesOrEmpty()
}

func shouldUseDirectBankCashFlow(ctx queryExecutionContext) bool {
	if shouldUseReconciliation(ctx.q) {
		return false
	}
	if ctx.hasRealEntity {
		return false
	}
	if ctx.spec.SourceConstraint != BossSourceBankStatement {
		return false
	}
	return ctx.spec.BossRewrite.Perspective == BossPerspectiveExplicitCash
}

func shouldUseDirectCashOnHandBalance(ctx queryExecutionContext) bool {
	if ctx.hasRealEntity {
		return false
	}
	if ctx.spec.SourceConstraint != BossSourceBalance {
		return false
	}
	return shouldUseCashOnHandBalanceQuestion(ctx.q)
}

func shouldUseDirectPreciseBalance(ctx queryExecutionContext) bool {
	if ctx.hasRealEntity {
		return false
	}
	if ctx.spec.SourceConstraint != BossSourceBalance {
		return false
	}
	return containsAny(ctx.q, []string{"余额", "期末", "期初", "货币资金", "银行存款"})
}

func shouldUseDirectAccrualCoreMetric(ctx queryExecutionContext) bool {
	if ctx.hasRealEntity || shouldUseReconciliation(ctx.q) {
		return false
	}
	if ctx.spec.QueryFamily != QueryFamilyCoreMetric {
		return false
	}
	if !asksExplicitAccrualOnlySource(ctx.q) {
		return false
	}
	if ctx.spec.SourceConstraint != BossSourceJournal && ctx.spec.BossRewrite.Perspective != BossPerspectiveFinancialAccount {
		return false
	}
	switch ctx.spec.MetricKind {
	case MetricKindRevenue, MetricKindCost, MetricKindProfit:
		return true
	default:
		return containsAny(ctx.q, []string{"收入", "营收", "成本", "费用", "利润", "净利润", "净利"})
	}
}

func asksExplicitAccrualOnlySource(q string) bool {
	return containsAny(q, []string{
		"按序时账口径", "序时账口径", "序时帐口径", "序时账", "序时帐",
		"按财务账口径", "财务账口径",
		"按会计账口径", "会计账口径",
		"按利润表口径", "利润表口径",
		"按凭证口径", "凭证口径",
	}) || asksBookOnlyNetProfit(q)
}

func resolveLegacySourceFallbackStages(ctx queryExecutionContext) []executionStage {
	builder := newExecutionStagePlanBuilder(4)
	if ctx.spec.NeedsContractDimension || ctx.spec.QueryFamily == QueryFamilyContractDimension || shouldUseContractDimensionWithConfig(ctx.q, ctx.cfg) {
		builder.add(executionStageDirectContractDimension)
	}
	if ctx.spec.QueryFamily == QueryFamilyReconciliation || shouldUseReconciliation(ctx.q) {
		builder.add(executionStageDirectReconciliation)
	}
	if !shouldSuppressCoreMetricRangeFallback(ctx) &&
		(isIntervalCoreMetricQuestionWithConfig(ctx.q, ctx.entity, ctx.hasRealEntity, ctx.from, ctx.to, ctx.cfg) ||
			shouldPreferCoreMetricSummaryWithConfig(ctx.q, ctx.entity, ctx.hasRealEntity, ctx.from, ctx.to, ctx.cfg)) {
		builder.add(executionStageDirectCoreMetricRange)
	}
	if ctx.spec.QueryFamily == QueryFamilySupplierPayments || shouldUseSupplierPaymentStats(ctx.q) {
		builder.add(executionStageDirectSupplierPayments)
	}
	return builder.stagesOrEmpty()
}

func shouldSuppressCoreMetricRangeFallback(ctx queryExecutionContext) bool {
	if !ctx.spec.PreferContractAggregate {
		return false
	}
	for _, probe := range ctx.spec.RouteDecision.ProbeResults {
		if probe.Source == BossSourceContractAggregate && probe.CanAnswer {
			return true
		}
	}
	return false
}

func resolveCounterpartyExecutionStages(ctx queryExecutionContext) []executionStage {
	if ctx.spec.NeedsContractDimension || ctx.spec.QueryFamily == QueryFamilyContractDimension {
		return nil
	}
	builder := newExecutionStagePlanBuilder(2)
	if ctx.hasRealEntity && isCounterpartyClassificationQuestionWithConfig(ctx.q, ctx.cfg) {
		builder.add(executionStageCounterpartyClassification)
	}
	if ctx.engine != nil && ctx.engine.shouldUseCounterpartyAuditFallback(ctx) {
		builder.add(executionStageCounterpartyAuditFallback)
	}
	return builder.stagesOrEmpty()
}
