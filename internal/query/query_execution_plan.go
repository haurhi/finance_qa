package query

type executionStage string

const (
	executionStageHRBreakdown                executionStage = "hr_breakdown"
	executionStageExpenseBreakdown           executionStage = "expense_breakdown"
	executionStageOrchestrator               executionStage = "orchestrator"
	executionStageDirectCashOnHandBalance    executionStage = "direct_cash_on_hand_balance"
	executionStageDirectBankCashFlow         executionStage = "direct_bank_cash_flow"
	executionStageDirectPreciseBalance       executionStage = "direct_precise_balance"
	executionStageDirectAccrualCoreMetric    executionStage = "direct_accrual_core_metric"
	executionStageDirectARAP                 executionStage = "direct_arap"
	executionStageDirectContractDimension    executionStage = "direct_contract_dimension"
	executionStageDirectReconciliation       executionStage = "direct_reconciliation"
	executionStageDirectCoreMetricRange      executionStage = "direct_core_metric_range"
	executionStageDirectSupplierPayments     executionStage = "direct_supplier_payments"
	executionStageFinanceHealth              executionStage = "finance_health"
	executionStageCounterpartyClassification executionStage = "counterparty_classification"
	executionStageCounterpartyAuditFallback  executionStage = "counterparty_audit_fallback"
	executionStageIntentRoute                executionStage = "intent_route"
)

func buildExecutionPlan(ctx queryExecutionContext) []executionStage {
	builder := newExecutionStagePlanBuilder(8)
	builder.addAll(resolveOperationalExecutionStages(ctx)...)
	builder.addAll(resolveSourceExecutionStages(ctx)...)
	builder.addAll(resolveCounterpartyExecutionStages(ctx)...)
	if shouldAppendIntentRoute(ctx) {
		builder.add(executionStageIntentRoute)
	}
	return builder.stagesOrEmpty()
}

func shouldAppendIntentRoute(ctx queryExecutionContext) bool {
	return !(ctx.spec.NeedsContractDimension || ctx.spec.QueryFamily == QueryFamilyContractDimension)
}

func (e *Engine) executeStage(stage executionStage, ctx queryExecutionContext) (Result, bool) {
	if result, ok := e.executeDomainStage(stage, ctx); ok {
		return result, true
	}
	if handler, ok := executionStageHandlers()[stage]; ok {
		return handler(e, ctx)
	}
	return Result{}, false
}

func (e *Engine) finalizeExecutionResult(ctx queryExecutionContext, result Result) Result {
	if result.Success {
		return ctx.finalize(result)
	}
	if fallback, ok := e.tryExplicitContractFallback(ctx, result); ok {
		return ctx.finalize(fallback)
	}
	if shouldStopAtStrictContractSource(ctx) {
		return ctx.finalize(buildStrictContractMissingResult(ctx, result))
	}
	if shouldUseStrictContractSourceForSpec(ctx.spec) {
		return ctx.finalize(buildStrictContractMissingResult(ctx, result))
	}
	if shouldFallbackExecutionResult(result) {
		return ctx.finalize(e.queryFallback(ctx.q, ctx.from, ctx.to, result.Message))
	}
	if result.Message != "" {
		return ctx.finalize(result)
	}
	return ctx.finalize(e.queryFallback(ctx.q, ctx.from, ctx.to, result.Message))
}
