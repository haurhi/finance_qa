package query

import (
	"reflect"
	"testing"
)

func TestBuildCoreMetricSharedResultFields(t *testing.T) {
	book := monthlyBookView{
		Revenue:             1000,
		TotalCost:           800,
		NonOperatingIncome:  15,
		NonOperatingExpense: 5,
		Profit:              210,
		NetProfit:           180,
	}
	cashFlowSummary := map[string]any{
		"现金流入": float64(900),
		"现金流出": float64(650),
		"净现金流": float64(250),
	}
	bridge := map[string]any{"经营现金净额估算": float64(260)}

	got := buildCoreMetricSharedResultFields("income_statement", book, 180, cashFlowSummary, bridge, true)

	if got["source_tables"] == nil {
		t.Fatalf("source_tables missing: %+v", got)
	}
	if !reflect.DeepEqual(got["profit_cash_bridge"], bridge) {
		t.Fatalf("profit_cash_bridge = %#v, want original bridge %#v", got["profit_cash_bridge"], bridge)
	}
	if got["现金流入"] != float64(900) || got["现金流出"] != float64(650) || got["净现金流"] != float64(250) {
		t.Fatalf("cash summary mismatch: %+v", got)
	}
	bookView, ok := got["财务做账口径(看利润)"].(map[string]any)
	if !ok {
		t.Fatalf("book view missing: %+v", got)
	}
	if bookView["账面利润"] != float64(180) {
		t.Fatalf("账面利润 = %v, want 180", bookView["账面利润"])
	}
	if bookView["净利润"] != float64(180) {
		t.Fatalf("净利润 = %v, want 180", bookView["净利润"])
	}
}

func TestBuildAccrualCoreMetricResultDataUsesSharedFields(t *testing.T) {
	book := monthlyBookView{
		Revenue:             1000,
		Cost:                650,
		TaxSurcharge:        15,
		SellingExpense:      20,
		AdminExpense:        35,
		FinanceExpense:      10,
		TotalCost:           730,
		NonOperatingIncome:  12,
		NonOperatingExpense: 4,
		Profit:              278,
		NetProfit:           240,
	}
	cashFlowSummary := map[string]any{
		"现金流入": float64(900),
		"现金流出": float64(620),
		"净现金流": float64(280),
	}
	bridge := map[string]any{"经营现金净额估算": float64(265)}

	got := buildAccrualCoreMetricResultData(
		"2026-03",
		2026,
		3,
		"income_statement",
		[]string{"利润"},
		"利润",
		278,
		240,
		book,
		cashFlowSummary,
		bridge,
	)

	if got["period"] != "2026-03" {
		t.Fatalf("period = %v, want 2026-03", got["period"])
	}
	if got["metric"] != "利润" {
		t.Fatalf("metric = %v, want 利润", got["metric"])
	}
	if got["account_value"] != float64(278) || got["total"] != float64(278) {
		t.Fatalf("account_value/total mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got["profit_cash_bridge"], bridge) {
		t.Fatalf("profit_cash_bridge = %#v, want original bridge %#v", got["profit_cash_bridge"], bridge)
	}
	if got["source_tables"] == nil {
		t.Fatalf("source_tables missing: %+v", got)
	}

	monthly, ok := got["monthly"].(map[string]any)
	if !ok {
		t.Fatalf("monthly payload missing: %+v", got)
	}
	if monthly["year"] != 2026 || monthly["month"] != 3 {
		t.Fatalf("monthly year/month = %v/%v, want 2026/3", monthly["year"], monthly["month"])
	}
	if monthly["source"] != "income_statement" {
		t.Fatalf("monthly source = %v, want income_statement", monthly["source"])
	}
	costDetail, ok := monthly["cost_detail"].(map[string]any)
	if !ok {
		t.Fatalf("cost_detail missing: %+v", monthly)
	}
	if costDetail["operating_cost"] != float64(650) || costDetail["admin_expense"] != float64(35) {
		t.Fatalf("cost_detail mismatch: %+v", costDetail)
	}

	bookView, ok := got["财务做账口径(看利润)"].(map[string]any)
	if !ok {
		t.Fatalf("book view missing: %+v", got)
	}
	if bookView["账面利润"] != float64(240) || bookView["净利润"] != float64(240) {
		t.Fatalf("book view profit mismatch: %+v", bookView)
	}
}
