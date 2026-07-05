package query

type executionStageHandler func(*Engine, queryExecutionContext) (Result, bool)

func executionStageHandlers() map[executionStage]executionStageHandler {
	return map[executionStage]executionStageHandler{
		executionStageExpenseBreakdown: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryExpenseBreakdown(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageHRBreakdown: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryHRBreakdown(ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageOrchestrator: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			return e.tryOrchestratedQuery(ctx.spec)
		},
		executionStageDirectCashOnHandBalance: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryCashOnHandBalance(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageDirectBankCashFlow: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryBankCashFlow(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageDirectPreciseBalance: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryPrecise(ctx.q, ctx.to)
			return result, result.Success
		},
		executionStageDirectAccrualCoreMetric: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryAccrualCoreMetricWithCoverage(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageDirectARAP: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryARAP(ctx.q, ctx.entity, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageDirectContractDimension: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryContractDimension(ctx.q, ctx.entity, ctx.anchor)
			return result, result.Success
		},
		executionStageDirectCoreMetricRange: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryDualPerspectiveForCoreMetric(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageDirectSupplierPayments: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.querySupplierPayments(ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageFinanceHealth: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryFinanceHealth(ctx.q)
			return result, result.Success
		},
		executionStageIntentRoute: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.executeIntentRoute(ctx)
			return result, result.Success
		},
	}
}

func executionDomainStageHandlers() map[executionStage]executionStageHandler {
	return map[executionStage]executionStageHandler{
		executionStageDirectReconciliation: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryReconciliation(ctx.q, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageCounterpartyClassification: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryCounterpartyAmountFallback(ctx.q, ctx.entity, ctx.from, ctx.to)
			return result, result.Success
		},
		executionStageCounterpartyAuditFallback: func(e *Engine, ctx queryExecutionContext) (Result, bool) {
			result := e.queryCounterpartyAmountFallback(ctx.q, ctx.entity, ctx.from, ctx.to)
			return result, result.Success
		},
	}
}
