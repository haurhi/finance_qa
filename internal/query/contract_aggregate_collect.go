package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const contractAggregateRole = "aggregate_summary"

type contractAggregateSummary struct {
	OriginalQuestion        string
	Entity                  string
	Scope                   string
	Period                  string
	RequestedPeriod         string
	PeriodFrom              string
	PeriodTo                string
	RequestedPeriodFrom     string
	RequestedPeriodTo       string
	PeriodAdjusted          bool
	RequestedMetrics        []string
	ContractCount           int
	RevenueSettlement       float64
	RevenueReceived         float64
	RevenueInvoiced         float64
	RevenueReceivable       float64
	RevenueInvoiceOpen      float64
	CostSettlement          float64
	CostPaid                float64
	CostInvoiced            float64
	CostPayable             float64
	CostInvoiceOpen         float64
	Profit                  float64
	NetProfitContext        float64
	HasNetProfitContext     bool
	NetProfitContextSource  string
	RevenueComparison       *contractAggregatePeriodComparison
	RevenueItems            []contractAggregateOpenItem
	RevenueInvoiceOpenItems []contractAggregateOpenItem
	CostItems               []contractAggregateOpenItem
	CostInvoiceOpenItems    []contractAggregateOpenItem
	RevenueCustomerRanking  []contractAggregateDimensionRow
	RevenueOpenRanking      []contractAggregateDimensionRow
	RevenueOpenBuckets      []contractAggregateOpenBucket
	CostSupplierRanking     []contractAggregateDimensionRow
	TopRevenueShare         float64
	Top2RevenueShare        float64
	Top2RevenueSettlement   float64
	HasRevenueCoverage      bool
	HasCostCoverage         bool
	SourceTables            []string
	ExecutedSQL             []string
	CalculationLogs         []string
}

type contractAggregateOpenItem struct {
	CustomerName     string
	ContractContent  string
	SettlementAmount float64
	InvoiceAmount    float64
	ReceivedAmount   float64
	OpenAmount       float64
}

type contractAggregateDimensionRow struct {
	Name             string
	SettlementAmount float64
	InvoiceAmount    float64
	MovementAmount   float64
	OpenAmount       float64
	Share            float64
}

type contractAggregatePeriodComparison struct {
	CurrentLabel           string
	CurrentFrom            string
	CurrentTo              string
	CurrentRevenue         float64
	BaselineLabel          string
	BaselineFrom           string
	BaselineTo             string
	BaselineRevenue        float64
	BaselineMonthlyAverage float64
	DifferenceVsAverage    float64
	RatioVsAverage         float64
}

func (e *Engine) collectContractAggregateSummary(spec QuerySpec) (contractAggregateSummary, error) {
	cfg := e.currentRuleConfig()
	entity := strings.TrimSpace(spec.Entity)
	if e.shouldResolveContractAggregateEntity(spec.OriginalQuestion, entity) {
		if resolved := e.resolveContractSubject(spec.OriginalQuestion, entity); resolved != "" {
			entity = resolved
		}
	}

	scope := "company"
	entityLike := ""
	if entity != "" {
		scope = "entity"
		entityLike = "%" + entity + "%"
	}

	requestedMetrics := detectRequestedMetricsWithConfig(spec.OriginalQuestion, cfg)
	needRevenue := contractAggregateNeedsRevenueData(requestedMetrics)
	needCost := contractAggregateNeedsCostData(requestedMetrics)
	coverage := e.resolveContractAggregatePeriodCoverage(spec, requestedMetrics, entityLike)
	periodFrom := coverage.ActualFrom
	periodTo := coverage.ActualTo

	summary := contractAggregateSummary{
		OriginalQuestion:    spec.OriginalQuestion,
		Entity:              entity,
		Scope:               scope,
		Period:              displayPeriod(periodFrom, periodTo),
		RequestedPeriod:     displayPeriod(spec.PeriodFrom, spec.PeriodTo),
		PeriodFrom:          periodFrom,
		PeriodTo:            periodTo,
		RequestedPeriodFrom: spec.PeriodFrom,
		RequestedPeriodTo:   spec.PeriodTo,
		PeriodAdjusted:      coverage.Adjusted(),
		RequestedMetrics:    requestedMetrics,
		SourceTables:        contractAggregateSourceTablesForMetricsWithConfig(requestedMetrics, cfg),
	}
	if coverage.Note != "" {
		summary.CalculationLogs = append(summary.CalculationLogs, coverage.Note)
	}

	if needRevenue {
		summary.ExecutedSQL = append(summary.ExecutedSQL,
			"contract_aggregate(revenue): SELECT SUM(settlement_amount), SUM(received_amount), SUM(invoice_amount), SUM(unreceived), SUM(invoiced_unreceived), COUNT(DISTINCT contract_id) FROM fin_fund_income + fin_fund_income_groups ... WHERE year_month BETWEEN ? AND ?",
		)
		revenueTotals, err := e.collectFundIncomeTotals(context.Background(), periodFrom, periodTo, entityLike)
		if err != nil {
			return contractAggregateSummary{}, err
		}
		summary.RevenueSettlement = revenueTotals.Settlement
		summary.RevenueReceived = revenueTotals.Received
		summary.RevenueInvoiced = revenueTotals.Invoice
		summary.RevenueReceivable = revenueTotals.Receivable
		summary.RevenueInvoiceOpen = revenueTotals.InvoiceOpen
		summary.ContractCount = revenueTotals.ContractCount
		if contractAggregateWantsRevenueItems(spec.OriginalQuestion, requestedMetrics, cfg) {
			items, err := e.collectRevenueItems(periodFrom, periodTo, entityLike)
			if err != nil {
				return contractAggregateSummary{}, err
			}
			summary.RevenueItems = items
			summary.RevenueCustomerRanking = rollupContractAggregateItemsByName(items, revenueTotals.Settlement)
			summary.TopRevenueShare = topNContractAggregateShare(summary.RevenueCustomerRanking, 1)
			summary.Top2RevenueShare = topNContractAggregateShare(summary.RevenueCustomerRanking, 2)
			summary.Top2RevenueSettlement = topNContractAggregateSettlement(summary.RevenueCustomerRanking, 2)
			openItems := filterOpenContractAggregateItems(items)
			summary.RevenueOpenRanking = rollupContractAggregateOpenItemsByName(openItems, revenueTotals.Receivable)
			summary.RevenueOpenBuckets = e.collectRevenueOpenBuckets(periodFrom, periodTo, summary.RevenueOpenRanking)
		}
		if comparison, ok := e.collectContractRevenuePeriodComparison(spec.OriginalQuestion, periodFrom, periodTo, entityLike); ok {
			summary.RevenueComparison = &comparison
		}
		if contractAggregateIncludesMetric(requestedMetrics, "已开票未回款") {
			items, err := e.collectRevenueInvoiceOpenItems(periodFrom, periodTo, entityLike)
			if err != nil {
				return contractAggregateSummary{}, err
			}
			summary.RevenueInvoiceOpenItems = items
		}
	}

	var costContractCount int
	if needCost {
		summary.ExecutedSQL = append(summary.ExecutedSQL,
			"contract_aggregate(cost): SELECT SUM(settlement_amount), SUM(paid_amount), SUM(invoice_amount), SUM(payable), SUM(invoiced_unpaid), COUNT(DISTINCT contract_id) FROM fin_cost_settlements + fin_cost_settlement_groups ... WHERE year_month BETWEEN ? AND ?",
		)
		costTotals, err := e.collectCostSettlementTotals(context.Background(), periodFrom, periodTo, entityLike)
		if err != nil {
			return contractAggregateSummary{}, err
		}
		summary.CostSettlement = costTotals.Settlement
		summary.CostPaid = costTotals.Paid
		summary.CostInvoiced = costTotals.Invoice
		summary.CostPayable = costTotals.Payable
		summary.CostInvoiceOpen = costTotals.InvoiceOpen
		costContractCount = costTotals.ContractCount
		if contractAggregateWantsCostItems(spec.OriginalQuestion, requestedMetrics, cfg) {
			items, err := e.collectCostItems(periodFrom, periodTo, entityLike)
			if err != nil {
				return contractAggregateSummary{}, err
			}
			summary.CostItems = items
			summary.CostSupplierRanking = rollupContractAggregateItemsByName(items, costTotals.Settlement)
		}
		if contractAggregateIncludesMetric(requestedMetrics, "已收票未付款") {
			items, err := e.collectCostInvoiceOpenItems(periodFrom, periodTo, entityLike)
			if err != nil {
				return contractAggregateSummary{}, err
			}
			summary.CostInvoiceOpenItems = items
		}
	}

	if contractAggregateIncludesMetric(requestedMetrics, "利润") {
		book, source, _, sqls, logs, err := e.bookSummaryForRange(periodFrom, periodTo)
		if err == nil {
			summary.NetProfitContext = round2(book.NetProfit)
			summary.HasNetProfitContext = true
			summary.NetProfitContextSource = source
			summary.SourceTables = dedupeSourceTables(append(summary.SourceTables, "tenant_uhub.fin_income_statement")...)
			summary.ExecutedSQL = append(summary.ExecutedSQL, sqls...)
			summary.CalculationLogs = append(summary.CalculationLogs, logs...)
		}
	}

	summary.RevenueSettlement = round2(summary.RevenueSettlement)
	summary.RevenueReceived = round2(summary.RevenueReceived)
	summary.RevenueInvoiced = round2(summary.RevenueInvoiced)
	summary.RevenueReceivable = round2(summary.RevenueReceivable)
	summary.RevenueInvoiceOpen = round2(summary.RevenueInvoiceOpen)
	summary.CostSettlement = round2(summary.CostSettlement)
	summary.CostPaid = round2(summary.CostPaid)
	summary.CostInvoiced = round2(summary.CostInvoiced)
	summary.CostPayable = round2(summary.CostPayable)
	summary.CostInvoiceOpen = round2(summary.CostInvoiceOpen)
	summary.Profit = round2(summary.RevenueSettlement - summary.CostSettlement)
	summary.HasRevenueCoverage = summary.ContractCount > 0
	summary.HasCostCoverage = costContractCount > 0
	if summary.ContractCount == 0 && costContractCount > 0 {
		summary.ContractCount = costContractCount
	}

	summary.CalculationLogs = append(summary.CalculationLogs,
		fmt.Sprintf("[合同汇总优先] scope=%s entity=%s period=%s revenue=%.2f received=%.2f invoice=%.2f receivable=%.2f cost=%.2f paid=%.2f payable=%.2f profit=%.2f", summary.Scope, summary.Entity, summary.Period, summary.RevenueSettlement, summary.RevenueReceived, summary.RevenueInvoiced, summary.RevenueReceivable, summary.CostSettlement, summary.CostPaid, summary.CostPayable, summary.Profit),
	)

	return summary, nil
}

func (e *Engine) shouldResolveContractAggregateEntity(question, entity string) bool {
	if strings.TrimSpace(entity) != "" {
		return true
	}
	if shouldForceCompanyScopeContractAggregate(question) {
		return false
	}
	if !shouldUseCompanyScopeContractAggregate(question) {
		return true
	}
	if !looksLikeContractCounterpartyOperatingSubject(question) &&
		!looksLikeSpecificContractDimensionSubject(question) &&
		!hasContractContentCodeSubject(question) {
		return false
	}
	return e.resolveContractSubject(question, "") != ""
}

type contractAggregateOpenBucket struct {
	Name         string
	PriorLabel   string
	PriorFrom    string
	PriorTo      string
	PriorOpen    float64
	CurrentLabel string
	CurrentFrom  string
	CurrentTo    string
	CurrentOpen  float64
	TotalOpen    float64
}

func contractAggregateWantsDetailItems(question string) bool {
	return containsAny(question, []string{
		"明细", "列表", "列一下", "有哪些", "哪些", "分别", "拆", "拆分", "构成",
		"各是多少", "各多少", "各是", "项目和金额", "项目及金额", "项目及对应金额", "对应金额", "金额各是", "金额分别",
	})
}

func contractAggregateWantsRevenueItems(question string, requestedMetrics []string, cfg RuleConfig) bool {
	return contractAggregateWantsDetailItems(question) ||
		shouldUseCustomerRevenueAnalysisQuestion(question, cfg) ||
		contractAggregateIncludesMetric(requestedMetrics, "应收")
}

func contractAggregateWantsCostItems(question string, requestedMetrics []string, cfg RuleConfig) bool {
	return contractAggregateWantsDetailItems(question) ||
		shouldUseContractCostAnalysisQuestion(question, cfg) ||
		contractAggregateIncludesMetric(requestedMetrics, "应付")
}

func (e *Engine) mergedGroupContractContentSQL(memberTable, groupAlias string) string {
	if e.usesPostgresSQL() {
		return fmt.Sprintf(`COALESCE((
	SELECT '合并行合计（覆盖合同/项目：' || STRING_AGG(COALESCE(NULLIF(TRIM(mc.contract_content), ''), mc.contract_id), '、' ORDER BY gm.source_row_number, mc.contract_id) || '）'
	FROM %s gm
	JOIN fin_contracts mc ON mc.contract_id = gm.contract_id
	WHERE gm.group_id = %s.id
), '合并行合计')`, memberTable, groupAlias)
	}
	return fmt.Sprintf(`COALESCE((
	SELECT '合并行合计（覆盖合同/项目：' || GROUP_CONCAT(COALESCE(NULLIF(TRIM(mc.contract_content), ''), mc.contract_id), '、') || '）'
	FROM %s gm
	JOIN fin_contracts mc ON mc.contract_id = gm.contract_id
	WHERE gm.group_id = %s.id
), '合并行合计')`, memberTable, groupAlias)
}

func (e *Engine) usesPostgresSQL() bool {
	dbPath := strings.ToLower(strings.TrimSpace(e.dbPath))
	return strings.Contains(dbPath, "host=") ||
		strings.HasPrefix(dbPath, "postgres://") ||
		strings.HasPrefix(dbPath, "postgresql://")
}

func (e *Engine) collectRevenueItems(periodFrom, periodTo, like string) ([]contractAggregateOpenItem, error) {
	unionSQL := `
SELECT c.customer_name,
       c.contract_content,
       COALESCE(f.settlement_amount, 0) AS settlement_amount,
       COALESCE(f.invoice_amount, 0) AS invoice_amount,
       COALESCE(f.received_amount, 0) AS received_amount,
       CASE WHEN COALESCE(f.settlement_amount, 0) > COALESCE(f.received_amount, 0) THEN COALESCE(f.settlement_amount, 0) - COALESCE(f.received_amount, 0) ELSE 0 END AS open_amount
FROM fin_fund_income f
JOIN fin_contracts c ON c.contract_id = f.contract_id
WHERE f.year_month BETWEEN ? AND ?`
	args := []any{periodFrom, periodTo}
	if strings.TrimSpace(like) != "" {
		unionSQL += ` AND (c.customer_name LIKE ? OR c.contract_content LIKE ?)`
		args = append(args, like, like)
	}
	if e.hasFundIncomeGroupTables() {
		if e.hasContractFinanceGroupOwnerColumns(fundIncomeTotalsSpec()) {
			groupMemberCount := `(SELECT COUNT(*) FROM fin_fund_income_group_members gm_attr WHERE gm_attr.group_id = g.id)`
			unionSQL += fmt.Sprintf(`
UNION ALL
SELECT c.customer_name,
       c.contract_content,
       COALESCE(g.settlement_amount, 0) AS settlement_amount,
       CASE WHEN %[1]s <= 1 THEN COALESCE(g.invoice_amount, 0) ELSE 0 END AS invoice_amount,
       COALESCE(g.received_amount, 0) AS received_amount,
       CASE WHEN COALESCE(g.settlement_amount, 0) > COALESCE(g.received_amount, 0) THEN COALESCE(g.settlement_amount, 0) - COALESCE(g.received_amount, 0) ELSE 0 END AS open_amount
FROM fin_fund_income_groups g
JOIN fin_fund_income_group_members gm ON gm.group_id = g.id AND %[2]s
JOIN fin_contracts c ON c.contract_id = gm.contract_id
WHERE g.year_month BETWEEN ? AND ?`, groupMemberCount, e.contractFinanceGroupOwnerPredicate(fundIncomeTotalsSpec()))
			args = append(args, periodFrom, periodTo)
			if strings.TrimSpace(like) != "" {
				unionSQL += ` AND (g.customer_name LIKE ? OR c.customer_name LIKE ? OR c.contract_content LIKE ?)`
				args = append(args, like, like, like)
			}
		} else {
			groupContent := e.mergedGroupContractContentSQL("fin_fund_income_group_members", "g")
			unionSQL += `
UNION ALL
SELECT g.customer_name,
       ` + groupContent + ` AS contract_content,
       COALESCE(g.settlement_amount, 0) AS settlement_amount,
       COALESCE(g.invoice_amount, 0) AS invoice_amount,
       COALESCE(g.received_amount, 0) AS received_amount,
       CASE WHEN COALESCE(g.settlement_amount, 0) > COALESCE(g.received_amount, 0) THEN COALESCE(g.settlement_amount, 0) - COALESCE(g.received_amount, 0) ELSE 0 END AS open_amount
FROM fin_fund_income_groups g
WHERE g.year_month BETWEEN ? AND ?`
			args = append(args, periodFrom, periodTo)
			if strings.TrimSpace(like) != "" {
				unionSQL += ` AND g.customer_name LIKE ?`
				args = append(args, like)
			}
		}
	}
	sqlText := `
SELECT customer_name,
       contract_content,
       COALESCE(SUM(settlement_amount), 0),
       COALESCE(SUM(invoice_amount), 0),
       COALESCE(SUM(received_amount), 0),
       CASE WHEN COALESCE(SUM(settlement_amount), 0) > COALESCE(SUM(received_amount), 0)
            THEN COALESCE(SUM(settlement_amount), 0) - COALESCE(SUM(received_amount), 0)
            ELSE 0 END
FROM (` + unionSQL + `) revenue_items
GROUP BY customer_name, contract_content
ORDER BY 3 DESC, customer_name, contract_content`
	return e.collectContractAggregateItems(sqlText, args)
}

func (e *Engine) collectCostItems(periodFrom, periodTo, like string) ([]contractAggregateOpenItem, error) {
	unionSQL := `
SELECT c.customer_name,
       c.contract_content,
       COALESCE(cs.settlement_amount, 0) AS settlement_amount,
       COALESCE(cs.invoice_amount, 0) AS invoice_amount,
       COALESCE(cs.paid_amount, 0) AS paid_amount,
       CASE WHEN COALESCE(cs.settlement_amount, 0) > COALESCE(cs.paid_amount, 0) THEN COALESCE(cs.settlement_amount, 0) - COALESCE(cs.paid_amount, 0) ELSE 0 END AS open_amount
FROM fin_cost_settlements cs
JOIN fin_contracts c ON c.contract_id = cs.contract_id
WHERE cs.year_month BETWEEN ? AND ?`
	args := []any{periodFrom, periodTo}
	if strings.TrimSpace(like) != "" {
		unionSQL += ` AND (c.customer_name LIKE ? OR c.contract_content LIKE ?)`
		args = append(args, like, like)
	}
	if e.hasCostSettlementGroupTables() {
		if e.hasContractFinanceGroupOwnerColumns(costSettlementTotalsSpec()) {
			groupMemberCount := `(SELECT COUNT(*) FROM fin_cost_settlement_group_members gm_attr WHERE gm_attr.group_id = g.id)`
			unionSQL += fmt.Sprintf(`
	UNION ALL
	SELECT c.customer_name,
       c.contract_content,
       COALESCE(g.settlement_amount, 0) AS settlement_amount,
       CASE WHEN %[1]s <= 1 THEN COALESCE(g.invoice_amount, 0) ELSE 0 END AS invoice_amount,
       COALESCE(g.paid_amount, 0) AS paid_amount,
       CASE WHEN COALESCE(g.settlement_amount, 0) > COALESCE(g.paid_amount, 0) THEN COALESCE(g.settlement_amount, 0) - COALESCE(g.paid_amount, 0) ELSE 0 END AS open_amount
FROM fin_cost_settlement_groups g
JOIN fin_cost_settlement_group_members gm ON gm.group_id = g.id AND %[2]s
JOIN fin_contracts c ON c.contract_id = gm.contract_id
	WHERE g.year_month BETWEEN ? AND ?`, groupMemberCount, e.contractFinanceGroupOwnerPredicate(costSettlementTotalsSpec()))
			args = append(args, periodFrom, periodTo)
			if strings.TrimSpace(like) != "" {
				unionSQL += ` AND (g.customer_name LIKE ? OR c.customer_name LIKE ? OR c.contract_content LIKE ?)`
				args = append(args, like, like, like)
			}
			unionSQL += fmt.Sprintf(`
	UNION ALL
	SELECT g.customer_name,
	       '合并行合计' AS contract_content,
	       COALESCE(g.settlement_amount, 0) AS settlement_amount,
	       COALESCE(g.invoice_amount, 0) AS invoice_amount,
	       COALESCE(g.paid_amount, 0) AS paid_amount,
	       CASE WHEN COALESCE(g.settlement_amount, 0) > COALESCE(g.paid_amount, 0) THEN COALESCE(g.settlement_amount, 0) - COALESCE(g.paid_amount, 0) ELSE 0 END AS open_amount
	FROM fin_cost_settlement_groups g
	WHERE g.year_month BETWEEN ? AND ?
	  AND NOT EXISTS (
	      SELECT 1
	      FROM fin_cost_settlement_group_members gm
	      WHERE gm.group_id = g.id AND %[1]s
	  )`, e.contractFinanceGroupOwnerPredicate(costSettlementTotalsSpec()))
			args = append(args, periodFrom, periodTo)
			if strings.TrimSpace(like) != "" {
				unionSQL += ` AND g.customer_name LIKE ?`
				args = append(args, like)
			}
		} else {
			groupContent := e.mergedGroupContractContentSQL("fin_cost_settlement_group_members", "g")
			unionSQL += `
	UNION ALL
SELECT g.customer_name,
       ` + groupContent + ` AS contract_content,
       COALESCE(g.settlement_amount, 0) AS settlement_amount,
       COALESCE(g.invoice_amount, 0) AS invoice_amount,
       COALESCE(g.paid_amount, 0) AS paid_amount,
       CASE WHEN COALESCE(g.settlement_amount, 0) > COALESCE(g.paid_amount, 0) THEN COALESCE(g.settlement_amount, 0) - COALESCE(g.paid_amount, 0) ELSE 0 END AS open_amount
FROM fin_cost_settlement_groups g
WHERE g.year_month BETWEEN ? AND ?`
			args = append(args, periodFrom, periodTo)
			if strings.TrimSpace(like) != "" {
				unionSQL += ` AND g.customer_name LIKE ?`
				args = append(args, like)
			}
		}
	}
	sqlText := `
SELECT customer_name,
       contract_content,
       COALESCE(SUM(settlement_amount), 0),
       COALESCE(SUM(invoice_amount), 0),
       COALESCE(SUM(paid_amount), 0),
       CASE WHEN COALESCE(SUM(settlement_amount), 0) > COALESCE(SUM(paid_amount), 0)
            THEN COALESCE(SUM(settlement_amount), 0) - COALESCE(SUM(paid_amount), 0)
            ELSE 0 END
FROM (` + unionSQL + `) cost_items
GROUP BY customer_name, contract_content
ORDER BY 3 DESC, customer_name, contract_content`
	return e.collectContractAggregateItems(sqlText, args)
}

func (e *Engine) collectContractAggregateItems(sqlText string, args []any) ([]contractAggregateOpenItem, error) {
	rows, err := e.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []contractAggregateOpenItem{}
	for rows.Next() {
		var item contractAggregateOpenItem
		if err := rows.Scan(&item.CustomerName, &item.ContractContent, &item.SettlementAmount, &item.InvoiceAmount, &item.ReceivedAmount, &item.OpenAmount); err != nil {
			return nil, err
		}
		item.SettlementAmount = round2(item.SettlementAmount)
		item.InvoiceAmount = round2(item.InvoiceAmount)
		item.ReceivedAmount = round2(item.ReceivedAmount)
		item.OpenAmount = round2(item.OpenAmount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (e *Engine) collectRevenueInvoiceOpenItems(periodFrom, periodTo, like string) ([]contractAggregateOpenItem, error) {
	return e.collectContractAggregateInvoiceOpenItems(fundIncomeTotalsSpec(), "revenue_invoice_open_items", periodFrom, periodTo, like)
}

func (e *Engine) collectCostInvoiceOpenItems(periodFrom, periodTo, like string) ([]contractAggregateOpenItem, error) {
	items, err := e.collectContractAggregateInvoiceOpenItems(costSettlementTotalsSpec(), "cost_invoice_open_items", periodFrom, periodTo, like)
	if err != nil {
		return nil, err
	}
	return rollupSingleContractMergedOpenItems(items), nil
}

func (e *Engine) collectContractAggregateInvoiceOpenItems(spec contractFinanceTotalsSpec, alias, periodFrom, periodTo, like string) ([]contractAggregateOpenItem, error) {
	like = strings.TrimSpace(like)
	if !e.hasContractFinanceGroupTables(spec) {
		directOffsetExpr := e.contractFinanceInvoiceOpenOffsetExpr("d", spec.DirectTable)
		directSQL := fmt.Sprintf(`
SELECT customer_name,
       contract_content,
       COALESCE(SUM(invoice_amount), 0),
       COALESCE(SUM(movement_amount + invoice_open_offset_amount), 0),
       COALESCE(SUM(open_amount), 0)
FROM (
	SELECT c.customer_name,
	       c.contract_content,
	       COALESCE(d.invoice_amount, 0) AS invoice_amount,
	       COALESCE(d.%[1]s, 0) AS movement_amount,
	       %[3]s AS invoice_open_offset_amount,
	       CASE WHEN COALESCE(d.invoice_amount, 0) > COALESCE(d.%[1]s, 0) + %[3]s THEN COALESCE(d.invoice_amount, 0) - COALESCE(d.%[1]s, 0) - %[3]s ELSE 0 END AS open_amount
	FROM %[2]s d
	JOIN fin_contracts c ON c.contract_id = d.contract_id
	WHERE d.year_month BETWEEN ? AND ?`, spec.MovementColumn, spec.DirectTable, directOffsetExpr)
		args := []any{periodFrom, periodTo}
		if like != "" {
			directSQL += ` AND (c.customer_name LIKE ? OR c.contract_content LIKE ?)`
			args = append(args, like, like)
		}
		directSQL += fmt.Sprintf(`
) %s
GROUP BY customer_name, contract_content
HAVING COALESCE(SUM(open_amount), 0) > 0
ORDER BY 5 DESC, customer_name, contract_content`, alias)
		return e.collectContractAggregateInvoiceOpenItemsFromSQL(directSQL, args)
	}

	groupAmountFilter, groupAmountArgs := e.contractFinanceGroupAmountFilter(spec, periodFrom, periodTo, like)
	groupInvoicePredicate, groupInvoiceArgs := e.contractFinanceGroupInvoiceAttributionPredicate(spec, like)
	directOffsetExpr := e.contractFinanceInvoiceOpenOffsetExpr("d", spec.DirectTable)
	groupOffsetExpr := e.contractFinanceInvoiceOpenOffsetExpr("g", spec.GroupTable)
	directFilter := `d.year_month BETWEEN ? AND ?`
	directArgs := []any{periodFrom, periodTo}
	if like != "" {
		directFilter += ` AND (c.customer_name LIKE ? OR c.contract_content LIKE ?)`
		directArgs = append(directArgs, like, like)
	}
	groupContent := e.mergedGroupContractContentSQL(spec.GroupMemberTable, "g")
	sqlText := fmt.Sprintf(`
WITH selected_groups AS (
	SELECT g.id, g.year_month
	FROM %[2]s g
	WHERE `+groupAmountFilter+`
),
direct_rows AS (
	SELECT d.contract_id,
	       d.year_month,
	       c.customer_name,
	       c.contract_content,
	       COALESCE(d.invoice_amount, 0) AS invoice_amount,
	       COALESCE(d.%[1]s, 0) AS movement_amount,
	       %[8]s AS invoice_open_offset_amount
	FROM %[3]s d
	JOIN fin_contracts c ON c.contract_id = d.contract_id
	WHERE `+directFilter+`
),
uncovered_direct_rows AS (
	SELECT customer_name,
	       contract_content,
	       invoice_amount,
	       movement_amount,
	       invoice_open_offset_amount,
	       CASE WHEN invoice_amount > movement_amount + invoice_open_offset_amount THEN invoice_amount - movement_amount - invoice_open_offset_amount ELSE 0 END AS open_amount
	FROM direct_rows d
	WHERE NOT EXISTS (
		SELECT 1
		FROM selected_groups sg
		JOIN %[4]s gm ON gm.group_id = sg.id
		WHERE gm.contract_id = d.contract_id
		  AND sg.year_month = d.year_month
	)
),
group_scope_base_rows AS (
	SELECT g.customer_name,
	       %[6]s AS contract_content,
	       CASE WHEN %[5]s THEN COALESCE(g.invoice_amount, 0) ELSE 0 END + COALESCE(SUM(d.invoice_amount), 0) AS invoice_amount,
	       COALESCE(g.%[1]s, 0) + COALESCE(SUM(d.movement_amount), 0) AS movement_amount,
	       %[9]s + COALESCE(SUM(d.invoice_open_offset_amount), 0) AS invoice_open_offset_amount
	FROM %[2]s g
	JOIN selected_groups sg ON sg.id = g.id
	LEFT JOIN %[4]s gm ON gm.group_id = g.id
	LEFT JOIN direct_rows d ON d.contract_id = gm.contract_id AND d.year_month = g.year_month
	GROUP BY g.id, g.customer_name, g.invoice_amount, g.%[1]s, g.year_month
),
group_scope_rows AS (
	SELECT customer_name,
	       contract_content,
	       invoice_amount,
	       movement_amount,
	       invoice_open_offset_amount,
	       CASE WHEN invoice_amount > movement_amount + invoice_open_offset_amount THEN invoice_amount - movement_amount - invoice_open_offset_amount ELSE 0 END AS open_amount
	FROM group_scope_base_rows
),
%[7]s AS (
	SELECT customer_name, contract_content, invoice_amount, movement_amount, invoice_open_offset_amount, open_amount FROM uncovered_direct_rows
	UNION ALL
	SELECT customer_name, contract_content, invoice_amount, movement_amount, invoice_open_offset_amount, open_amount FROM group_scope_rows
)
SELECT customer_name,
       contract_content,
       COALESCE(SUM(invoice_amount), 0),
       COALESCE(SUM(movement_amount + invoice_open_offset_amount), 0),
       COALESCE(SUM(open_amount), 0)
FROM %[7]s
GROUP BY customer_name, contract_content
HAVING COALESCE(SUM(open_amount), 0) > 0
ORDER BY 5 DESC, customer_name, contract_content`, spec.MovementColumn, spec.GroupTable, spec.DirectTable, spec.GroupMemberTable, groupInvoicePredicate, groupContent, alias, directOffsetExpr, groupOffsetExpr)
	args := append([]any{}, groupAmountArgs...)
	args = append(args, directArgs...)
	args = append(args, groupInvoiceArgs...)
	return e.collectContractAggregateInvoiceOpenItemsFromSQL(sqlText, args)
}

func (e *Engine) collectContractAggregateInvoiceOpenItemsFromSQL(sqlText string, args []any) ([]contractAggregateOpenItem, error) {
	rows, err := e.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []contractAggregateOpenItem{}
	for rows.Next() {
		var item contractAggregateOpenItem
		if err := rows.Scan(&item.CustomerName, &item.ContractContent, &item.InvoiceAmount, &item.ReceivedAmount, &item.OpenAmount); err != nil {
			return nil, err
		}
		item.InvoiceAmount = round2(item.InvoiceAmount)
		item.ReceivedAmount = round2(item.ReceivedAmount)
		item.OpenAmount = round2(item.OpenAmount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func rollupSingleContractMergedOpenItems(items []contractAggregateOpenItem) []contractAggregateOpenItem {
	if len(items) == 0 {
		return nil
	}
	type key struct {
		customer string
		content  string
	}
	merged := make(map[key]contractAggregateOpenItem, len(items))
	for _, item := range items {
		item.CustomerName = strings.TrimSpace(item.CustomerName)
		item.ContractContent = normalizeSingleContractMergedContent(item.ContractContent)
		k := key{customer: item.CustomerName, content: item.ContractContent}
		current := merged[k]
		current.CustomerName = item.CustomerName
		current.ContractContent = item.ContractContent
		current.SettlementAmount = round2(current.SettlementAmount + item.SettlementAmount)
		current.InvoiceAmount = round2(current.InvoiceAmount + item.InvoiceAmount)
		current.ReceivedAmount = round2(current.ReceivedAmount + item.ReceivedAmount)
		current.OpenAmount = round2(current.OpenAmount + item.OpenAmount)
		merged[k] = current
	}
	out := make([]contractAggregateOpenItem, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OpenAmount != out[j].OpenAmount {
			return out[i].OpenAmount > out[j].OpenAmount
		}
		if out[i].CustomerName != out[j].CustomerName {
			return out[i].CustomerName < out[j].CustomerName
		}
		return out[i].ContractContent < out[j].ContractContent
	})
	return out
}

func normalizeSingleContractMergedContent(content string) string {
	content = strings.TrimSpace(content)
	const prefix = "合并行合计（覆盖合同/项目："
	const suffix = "）"
	if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
		return content
	}
	covered := strings.TrimSuffix(strings.TrimPrefix(content, prefix), suffix)
	if strings.TrimSpace(covered) == "" || strings.Contains(covered, "、") {
		return content
	}
	return strings.TrimSpace(covered)
}

func contractAggregateSourceTablesForMetrics(requestedMetrics []string) []string {
	return contractAggregateSourceTablesForMetricsWithConfig(requestedMetrics, getRuleConfig())
}

func contractAggregateSourceTablesForMetricsWithConfig(requestedMetrics []string, cfg RuleConfig) []string {
	configured := cfg.ContractSourceTables(contractAggregateRole)
	tables := []string{"fin_contracts"}
	if contractAggregateNeedsRevenueData(requestedMetrics) {
		tables = append(tables, "fin_fund_income", "fin_fund_income_groups", "fin_fund_income_group_members")
	}
	if contractAggregateNeedsCostData(requestedMetrics) {
		tables = append(tables, "fin_cost_settlements", "fin_cost_settlement_groups", "fin_cost_settlement_group_members")
	}
	return filterSourceTables(configured, tables...)
}

func contractAggregateNeedsRevenueData(requestedMetrics []string) bool {
	if len(requestedMetrics) == 0 {
		return true
	}
	for _, metric := range requestedMetrics {
		switch strings.TrimSpace(metric) {
		case "收入", "利润", "应收", "已开票未回款":
			return true
		}
	}
	return false
}

func contractAggregateNeedsCostData(requestedMetrics []string) bool {
	if len(requestedMetrics) == 0 {
		return true
	}
	for _, metric := range requestedMetrics {
		switch strings.TrimSpace(metric) {
		case "成本", "利润", "应付", "已收票未付款":
			return true
		}
	}
	return false
}
