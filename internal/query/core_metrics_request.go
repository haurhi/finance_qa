package query

import (
	"fmt"
	"strings"
)

type coreMetricRequest struct {
	RequestedMetrics  []string
	PrimaryMetric     string
	MetricLabel       string
	ExplicitNetProfit bool
}

func resolveCoreMetricRequest(question, multiMetricLabel string) coreMetricRequest {
	return resolveCoreMetricRequestWithConfig(question, multiMetricLabel, getRuleConfig())
}

func resolveCoreMetricRequestWithConfig(question, multiMetricLabel string, cfg RuleConfig) coreMetricRequest {
	requestedMetrics := detectRequestedMetricsWithConfig(question, cfg)
	explicitNetProfit := asksExplicitNetProfit(question)
	if explicitNetProfit {
		requestedMetrics = preferNetProfitMetric(requestedMetrics)
	}
	if len(requestedMetrics) == 0 {
		requestedMetrics = []string{detectCoreMetricWithConfig(question, cfg)}
	}

	primaryMetric := firstMetricOrDefault(requestedMetrics, detectCoreMetricWithConfig(question, cfg))
	metricLabel := multiMetricLabel
	if metricLabel == "" {
		metricLabel = primaryMetric
	}
	if len(requestedMetrics) == 1 {
		metricLabel = primaryMetric
	}
	if explicitNetProfit {
		metricLabel = "净利润"
	}

	return coreMetricRequest{
		RequestedMetrics:  requestedMetrics,
		PrimaryMetric:     primaryMetric,
		MetricLabel:       metricLabel,
		ExplicitNetProfit: explicitNetProfit,
	}
}

func preferNetProfitMetric(metrics []string) []string {
	out := make([]string, 0, len(metrics)+1)
	seenNetProfit := false
	for _, metric := range metrics {
		if metric == "利润" || metric == "净利润" {
			if !seenNetProfit {
				out = append(out, "净利润")
				seenNetProfit = true
			}
			continue
		}
		out = append(out, metric)
	}
	if len(out) == 0 || !seenNetProfit {
		out = append(out, "净利润")
	}
	return out
}

func buildCoreMetricMetricsMap(book monthlyBookView) map[string]any {
	return map[string]any{
		"收入":  round2(book.Revenue),
		"成本":  round2(book.TotalCost),
		"利润":  round2(book.Profit),
		"净利润": round2(book.NetProfit),
	}
}

func requestedCoreMetricRequiredAtoms(requestedMetrics []string, metrics map[string]any) []string {
	atoms := []string{}
	seen := map[string]bool{}
	for _, metric := range requestedMetrics {
		label := coreMetricRequiredAtomLabel(metric)
		key := metric
		if strings.TrimSpace(key) == "" || seen[label] {
			continue
		}
		amount, ok := financeFactAnyNumber(metrics[key])
		if !ok {
			continue
		}
		atoms = append(atoms, fmt.Sprintf("%s：%.2f 元", label, amount))
		seen[label] = true
	}
	return atoms
}

func coreMetricRequiredAtomLabel(metric string) string {
	if metric == "成本" {
		return "成本及费用"
	}
	return metric
}

func buildCoreMetricSummaryPayload(from, to, source string, book monthlyBookView) map[string]any {
	payload := map[string]any{
		"source":                source,
		"revenue":               book.Revenue,
		"cost":                  book.TotalCost,
		"profit":                book.Profit,
		"non_operating_income":  book.NonOperatingIncome,
		"non_operating_expense": book.NonOperatingExpense,
		"net_profit":            book.NetProfit,
	}
	if from == to {
		year, month := parsePeriod(to)
		payload["year"] = year
		payload["month"] = month
	} else {
		payload["from"] = from
		payload["to"] = to
	}
	return payload
}
