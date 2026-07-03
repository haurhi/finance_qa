package query

import "testing"

func TestResolveSourceAttributionPlanForContractFirstRevenue(t *testing.T) {
	spec := QuerySpec{
		QueryFamily: QueryFamilyCoreMetric,
		MetricKind:  MetricKindRevenue,
	}
	data := map[string]any{
		"source_priority": "contract_first",
		"source_tables": []string{
			"tenant_uhub.fin_contracts",
			"tenant_uhub.fin_fund_income",
		},
		"metric": "收入",
	}

	plan := resolveSourceAttributionPlan(spec, data)
	assertStringSlicesEqual(t, plan.tables, []string{
		"tenant_uhub.fin_contracts",
		"tenant_uhub.fin_fund_income",
	})
	assertStringSlicesEqual(t, plan.primaryBaseTables, []string{"fin_fund_income"})
	assertStringSlicesEqual(t, plan.supportingBaseTables, []string{"fin_contracts"})
}

func TestResolveSourceAttributionPlanForCounterpartyReceipts(t *testing.T) {
	spec := QuerySpec{
		QueryFamily:        QueryFamilyCounterparty,
		NormalizedQuestion: "金程今年回款多少？其中3月到账多少？",
	}

	plan := resolveSourceAttributionPlan(spec, map[string]any{})
	assertStringSlicesEqual(t, plan.tables, []string{"fin_bank_statement"})
	assertStringSlicesEqual(t, plan.primaryBaseTables, []string{"fin_bank_statement"})
	if len(plan.supportingBaseTables) != 0 {
		t.Fatalf("supportingBaseTables = %v, want empty", plan.supportingBaseTables)
	}
}

func TestResolveSourceAttributionPlanForReconciliationUsesBookAndBankPrimarySources(t *testing.T) {
	spec := QuerySpec{
		QueryFamily: QueryFamilyReconciliation,
	}
	data := map[string]any{
		"book_source": "income_statement",
		"source_tables": []string{
			"fin_income_statement",
			"fin_bank_statement",
			"fin_journal",
		},
	}

	plan := resolveSourceAttributionPlan(spec, data)
	assertStringSlicesEqual(t, plan.tables, []string{
		"fin_income_statement",
		"fin_bank_statement",
		"fin_journal",
	})
	assertStringSlicesEqual(t, plan.primaryBaseTables, []string{
		"fin_income_statement",
		"fin_bank_statement",
	})
	assertStringSlicesEqual(t, plan.supportingBaseTables, []string{"fin_journal"})
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v want=%v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item[%d] = %q, want %q (got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}
