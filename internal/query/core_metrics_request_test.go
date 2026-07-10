package query

import (
	"strings"
	"testing"
)

func TestResolveCoreMetricRequestForNetProfit(t *testing.T) {
	got := resolveCoreMetricRequest("2026年2月账上净利润是多少", "核心指标")

	if !got.ExplicitNetProfit {
		t.Fatalf("ExplicitNetProfit = false, want true")
	}
	assertMetricListEqual(t, got.RequestedMetrics, []string{"净利润"})
	if got.PrimaryMetric != "净利润" {
		t.Fatalf("PrimaryMetric = %q, want %q", got.PrimaryMetric, "净利润")
	}
	if got.MetricLabel != "净利润" {
		t.Fatalf("MetricLabel = %q, want %q", got.MetricLabel, "净利润")
	}
}

func TestResolveCoreMetricRequestForMultipleMetricsUsesProvidedLabel(t *testing.T) {
	got := resolveCoreMetricRequest("2026年3月收入、成本、利润分别是多少？", "核心指标")

	assertMetricListEqual(t, got.RequestedMetrics, []string{"收入", "成本", "利润"})
	if got.MetricLabel != "核心指标" {
		t.Fatalf("MetricLabel = %q, want %q", got.MetricLabel, "核心指标")
	}
	if got.PrimaryMetric != "收入" {
		t.Fatalf("PrimaryMetric = %q, want %q", got.PrimaryMetric, "收入")
	}
}

func TestResolveCoreMetricRequestForIncomeCostAndNetProfit(t *testing.T) {
	got := resolveCoreMetricRequest("序时账看，最新完整月份账上收入、成本费用和净利润是多少？", "核心指标")

	if !got.ExplicitNetProfit {
		t.Fatalf("ExplicitNetProfit = false, want true")
	}
	assertMetricListEqual(t, got.RequestedMetrics, []string{"收入", "成本", "净利润"})
	if got.MetricLabel != "净利润" {
		t.Fatalf("MetricLabel = %q, want %q", got.MetricLabel, "净利润")
	}
}

func TestBuildAccrualCoreMetricsMessageUsesNetProfitWhenRequested(t *testing.T) {
	book := monthlyBookView{
		Revenue:   3106310.34,
		TotalCost: 2815018.91,
		Profit:    291291.55,
		NetProfit: 291291.55,
	}

	got := buildAccrualCoreMetricsMessage("2026-03", []string{"收入", "成本", "净利润"}, book)
	for _, want := range []string{"账上收入 3106310.34", "成本及费用 2815018.91", "净利润 291291.55"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message = %q, want fragment %q", got, want)
		}
	}
}

func TestResolveCoreMetricRequestForOverviewUsesThreeCoreMetrics(t *testing.T) {
	got := resolveCoreMetricRequest("2026年第一季度经营概览", "核心指标")

	assertMetricListEqual(t, got.RequestedMetrics, []string{"收入", "成本", "利润"})
	if got.MetricLabel != "核心指标" {
		t.Fatalf("MetricLabel = %q, want %q", got.MetricLabel, "核心指标")
	}
	if got.PrimaryMetric != "收入" {
		t.Fatalf("PrimaryMetric = %q, want %q", got.PrimaryMetric, "收入")
	}
}

func TestBuildCoreMetricMetricsMap(t *testing.T) {
	book := monthlyBookView{
		Revenue:   100,
		TotalCost: 80,
		Profit:    23,
		NetProfit: 20,
	}

	got := buildCoreMetricMetricsMap(book)
	if got["收入"] != float64(100) {
		t.Fatalf("收入 = %v, want 100", got["收入"])
	}
	if got["成本"] != float64(80) {
		t.Fatalf("成本 = %v, want 80", got["成本"])
	}
	if got["利润"] != float64(23) {
		t.Fatalf("利润 = %v, want 23", got["利润"])
	}
	if got["净利润"] != float64(20) {
		t.Fatalf("净利润 = %v, want 20", got["净利润"])
	}
}

func TestBuildCoreMetricSummaryPayloadForMonth(t *testing.T) {
	book := monthlyBookView{
		Revenue: 100,
	}
	got := buildCoreMetricSummaryPayload("2026-03", "2026-03", "income_statement", book)

	if got["source"] != "income_statement" {
		t.Fatalf("source = %v, want income_statement", got["source"])
	}
	if got["year"] != 2026 {
		t.Fatalf("year = %v, want 2026", got["year"])
	}
	if got["month"] != 3 {
		t.Fatalf("month = %v, want 3", got["month"])
	}
}

func TestBuildCoreMetricSummaryPayloadForRange(t *testing.T) {
	book := monthlyBookView{
		Revenue: 100,
	}
	got := buildCoreMetricSummaryPayload("2026-01", "2026-03", "income_statement", book)

	if got["from"] != "2026-01" {
		t.Fatalf("from = %v, want 2026-01", got["from"])
	}
	if got["to"] != "2026-03" {
		t.Fatalf("to = %v, want 2026-03", got["to"])
	}
}

func assertMetricListEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item[%d] = %q, want %q (%v vs %v)", i, got[i], want[i], got, want)
		}
	}
}
