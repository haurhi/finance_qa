package query

import "testing"

func TestExecutionStageHandlersCoverOperationalAndSourceStages(t *testing.T) {
	handlers := executionStageHandlers()
	required := []executionStage{
		executionStageHRBreakdown,
		executionStageOrchestrator,
		executionStageDirectAccrualCoreMetric,
		executionStageDirectContractDimension,
		executionStageDirectCoreMetricRange,
		executionStageDirectSupplierPayments,
		executionStageIntentRoute,
	}

	for _, stage := range required {
		if _, ok := handlers[stage]; !ok {
			t.Fatalf("missing execution stage handler for %s", stage)
		}
	}
}

func TestExecutionDomainStageHandlersCoverDomainStages(t *testing.T) {
	handlers := executionDomainStageHandlers()
	required := []executionStage{
		executionStageDirectReconciliation,
		executionStageCounterpartyClassification,
		executionStageCounterpartyAuditFallback,
	}

	for _, stage := range required {
		if _, ok := handlers[stage]; !ok {
			t.Fatalf("missing execution domain stage handler for %s", stage)
		}
	}
}
