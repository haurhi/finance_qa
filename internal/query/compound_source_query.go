package query

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type compoundPeriodRange struct {
	Label string
	From  string
	To    string
}

type compoundSourceMetric struct {
	Key    string
	Label  string
	Source string
}

const (
	compoundSourceContractRevenue = "contract_revenue"
	compoundSourceContractCost    = "contract_cost"
	compoundSourceBankStatement   = "bank_statement"
	compoundSourceJournal         = "journal"
)

type compoundEntityMention struct {
	Name     string
	Position int
	Length   int
}

func (e *Engine) tryCompoundSourceQuery(ctx queryExecutionContext) (Result, QuerySpec, bool) {
	metrics := detectCompoundSourceMetrics(ctx.q)
	if len(metrics) == 0 {
		return Result{}, ctx.spec, false
	}

	entities := e.resolveMentionedContractCustomers(ctx.q)
	if len(entities) == 0 {
		return Result{}, ctx.spec, false
	}
	periods := extractCompoundPeriodRanges(ctx.q, ctx.anchor, ctx.from, ctx.to)
	if len(periods) == 0 {
		return Result{}, ctx.spec, false
	}
	if len(entities) < 2 && len(periods) < 2 {
		return Result{}, ctx.spec, false
	}

	items := make([]map[string]any, 0, len(entities)*len(periods))
	wantDetailItems := contractAggregateWantsDetailItems(ctx.q)
	detailItems := []map[string]any{}
	lines := []string{"按复合口径统计："}
	executedSQL := compoundMetricExecutedSQL(metrics)
	logs := make([]string, 0, len(entities)*len(periods))
	for _, entity := range entities {
		like := "%" + entity + "%"
		for _, period := range periods {
			metricValues, sourceRows, err := e.collectCompoundMetricValues(context.Background(), metrics, period.From, period.To, like)
			if err != nil {
				return Result{}, ctx.spec, false
			}
			coverage := "matched"
			if compoundSourceRowCount(sourceRows) == 0 {
				coverage = "missing"
			}
			item := map[string]any{
				"entity":           entity,
				"period_label":     period.Label,
				"period_from":      period.From,
				"period_to":        period.To,
				"metrics":          filterCompoundMetricValues(metricValues, metrics),
				"row_count":        compoundSourceRowCount(sourceRows),
				"source_row_count": sourceRows,
				"coverage_status":  coverage,
			}
			applyCompoundLegacyItemFields(item, metricValues)
			if wantDetailItems {
				itemDetails, err := e.collectCompoundDetailItems(metrics, period, entity, like)
				if err != nil {
					return Result{}, ctx.spec, false
				}
				item["detail_items"] = itemDetails
				detailItems = append(detailItems, itemDetails...)
			}
			items = append(items, item)
			if coverage == "missing" {
				lines = append(lines, fmt.Sprintf("- %s %s：未匹配到相关记录。", entity, period.Label))
			} else {
				lines = append(lines, fmt.Sprintf("- %s %s：%s。", entity, period.Label, formatCompoundMetricParts(metricValues, metrics)))
			}
			logs = append(logs, fmt.Sprintf("[复合口径] entity=%s period=%s rows=%d metrics=%s", entity, period.Label, compoundSourceRowCount(sourceRows), strings.Join(compoundMetricKeys(metrics), ",")))
		}
	}

	spec := ctx.spec
	spec.QueryFamily = QueryFamilyContractDimension
	spec.MetricKind = compoundSourceMetricKind(metrics)
	spec.Entity = strings.Join(entities, "、")
	spec.PeriodFrom, spec.PeriodTo = mergedPeriodBounds(periods)
	spec.TimeScope = TimeScopeCustom
	spec.PerspectivePolicy = PerspectiveCashThenAccrual
	spec.NeedsContractDimension = true
	spec.PreferContractAggregate = false
	spec.SourceConstraint = ""
	spec.BossRewrite.Metric = compoundBossMetric(metrics)
	spec.BossRewrite.Scope = BossScopeEntity
	spec.BossRewrite.Entity = spec.Entity
	spec.BossRewrite.PeriodFrom = spec.PeriodFrom
	spec.BossRewrite.PeriodTo = spec.PeriodTo
	spec.BossRewrite.SubPeriod = ""
	spec.BossRewrite.Granularity = BossGranularityAggregate
	spec.BossRewrite.Perspective = BossPerspectiveContractFirst
	spec.BossRewrite.SourceConstraint = ""
	spec.BossRewrite.RequiresSourceProbe = false
	spec.RouteDecision = RouteDecision{
		SelectedSource:   "compound_source_query",
		PrimaryTables:    compoundPrimaryTables(metrics),
		SupportingTables: compoundSupportingTables(metrics),
	}

	return Result{
		Success: true,
		Message: strings.Join(lines, "\n"),
		Data: map[string]any{
			"role":                     "customer_contract",
			"asked_topic":              compoundAskedTopic(metrics),
			"metric":                   compoundMetricDisplay(metrics),
			"requested_metrics":        compoundMetricKeys(metrics),
			"query_pipeline":           "compound_source_query",
			"entities":                 entities,
			"periods":                  compoundPeriodsData(periods),
			"items":                    items,
			"detail_items":             detailItems,
			"source_primary_tables":    compoundPrimaryTables(metrics),
			"source_supporting_tables": compoundSupportingTables(metrics),
		},
		ExecutedSQL:     executedSQL,
		CalculationLogs: logs,
	}, spec, true
}

func detectCompoundSourceMetrics(q string) []compoundSourceMetric {
	metrics := make([]compoundSourceMetric, 0, 4)
	add := func(source, key, label string) {
		for _, metric := range metrics {
			if metric.Key == key {
				return
			}
		}
		metrics = append(metrics, compoundSourceMetric{Key: key, Label: label, Source: source})
	}

	bankContext := containsAny(q, []string{"银行流水", "银行", "银行卡", "现金流水", "流水"})
	journalContext := containsAny(q, []string{"序时账", "序时帐", "凭证", "账务", "会计账"})
	costContext := containsAny(q, []string{"合同成本", "成本表", "合同成本表", "成本结算", "供应商", "采购", "应付", "收票", "已收票", "收到发票", "已收发票"})
	bankReceivedContext := containsAny(q, []string{"银行流水收款", "银行收款", "银行卡收款", "现金流水收款", "流水收款", "银行流水收入", "银行收入", "流水收入", "银行流水流入", "银行流入"})
	bankPaidContext := containsAny(q, []string{"银行流水付款", "银行付款", "银行卡付款", "现金流水付款", "流水付款", "银行流水支付", "银行支付", "银行流水流出", "银行流出", "银行流水支出", "银行支出"})
	explicitRevenueContext := containsAny(q, []string{"合同收入", "收入表", "收入明细", "收入结算", "客户合同", "营收", "销售额", "GMV", "gmv"})
	if strings.Contains(q, "收入") && !bankReceivedContext {
		explicitRevenueContext = true
	}
	onlyRevenueContext := !costContext && !bankContext && !journalContext
	explicitCostPaidContext := containsAny(q, []string{"合同成本已付款", "合同成本付款", "合同成本支付", "成本已付款", "成本付款", "成本支付", "供应商付款", "供应商支付", "应付已付款", "应付付款"})
	explicitCostUnpaidContext := containsAny(q, []string{"合同成本未付款", "合同成本未支付", "成本未付款", "成本未支付", "供应商未付款", "供应商未支付", "应付未付款", "应付未支付"})

	if bankContext {
		if bankReceivedContext || (!costContext && !journalContext && !explicitRevenueContext && containsAny(q, []string{"收款", "回款", "到账", "流入"})) {
			add(compoundSourceBankStatement, "bank_statement.received", "银行收款")
		}
		if bankPaidContext || (!costContext && !journalContext && containsAny(q, []string{"付款", "支付", "流出", "支出"})) {
			add(compoundSourceBankStatement, "bank_statement.paid", "银行付款")
		}
	}
	if journalContext {
		if containsAny(q, []string{"借方", "借方金额", "借方发生额"}) {
			add(compoundSourceJournal, "journal.debit", "序时账借方")
		}
		if containsAny(q, []string{"贷方", "贷方金额", "贷方发生额"}) {
			add(compoundSourceJournal, "journal.credit", "序时账贷方")
		}
	}
	if costContext {
		if containsAny(q, []string{"合同成本", "成本表", "合同成本表", "成本结算", "成本"}) {
			add(compoundSourceContractCost, "contract_cost.settlement", "合同成本")
		}
		if containsAny(q, []string{"已付款", "付款", "支付"}) && (!bankContext || explicitCostPaidContext) {
			add(compoundSourceContractCost, "contract_cost.paid", "已付款")
		}
		if containsAny(q, []string{"收票金额", "已收票", "收票", "收到发票", "已收发票"}) {
			add(compoundSourceContractCost, "contract_cost.invoice", "已收票")
		}
		if containsAny(q, []string{"已收票未付款", "已收发票未付款", "收到发票未付款", "收票未付款"}) {
			add(compoundSourceContractCost, "contract_cost.invoiced_unpaid", "已收票未付款")
		} else if containsAny(q, []string{"未付款", "未支付"}) && (!bankContext || explicitCostUnpaidContext) {
			add(compoundSourceContractCost, "contract_cost.unpaid", "未付款")
		}
	}

	if explicitRevenueContext || (onlyRevenueContext && containsAny(q, []string{"结算金额", "合同结算", "结算", "收入", "营收", "销售额", "GMV", "gmv"})) {
		add(compoundSourceContractRevenue, "contract_revenue.settlement", "结算")
	}
	if (explicitRevenueContext || onlyRevenueContext) && containsAny(q, []string{"收款金额", "合同收款", "客户收款", "收入收款", "已收款", "收款", "回款", "到账"}) && !bankReceivedContext {
		add(compoundSourceContractRevenue, "contract_revenue.received", "已收")
	}
	if (explicitRevenueContext || onlyRevenueContext) && containsAny(q, []string{"开票金额", "合同开票", "收入开票", "已开票", "开票"}) {
		add(compoundSourceContractRevenue, "contract_revenue.invoice", "已开票")
	}
	if containsAny(q, []string{"未回款", "未收款", "客户未付款", "客户未支付", "合同未回款", "合同未收款", "合同收入未回款", "合同收入未付款", "收入未回款", "收入未付款"}) || (onlyRevenueContext && looksLikeCustomerContractUnpaidQuestion(q)) {
		add(compoundSourceContractRevenue, "contract_revenue.unpaid", "未付款")
	}
	return metrics
}

func (e *Engine) collectCompoundDetailItems(metrics []compoundSourceMetric, period compoundPeriodRange, entity, like string) ([]map[string]any, error) {
	details := []map[string]any{}
	if hasCompoundMetricSource(metrics, compoundSourceContractRevenue) {
		items, err := e.collectRevenueItems(period.From, period.To, like)
		if err != nil {
			return nil, err
		}
		onlyOpen := hasCompoundMetricKey(metrics, "contract_revenue.unpaid")
		for _, item := range items {
			if onlyOpen && item.OpenAmount <= 0 {
				continue
			}
			details = append(details, buildCompoundRevenueDetailItem(period, entity, item))
		}
	}
	if hasCompoundMetricSource(metrics, compoundSourceContractCost) {
		var (
			items []contractAggregateOpenItem
			err   error
		)
		onlyOpen := hasCompoundMetricKey(metrics, "contract_cost.unpaid") || hasCompoundMetricKey(metrics, "contract_cost.invoiced_unpaid")
		if hasCompoundMetricKey(metrics, "contract_cost.invoiced_unpaid") && !hasCompoundMetricKey(metrics, "contract_cost.unpaid") {
			items, err = e.collectCostInvoiceOpenItems(period.From, period.To, like)
		} else {
			items, err = e.collectCostItems(period.From, period.To, like)
		}
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if onlyOpen && item.OpenAmount <= 0 {
				continue
			}
			details = append(details, buildCompoundCostDetailItem(period, entity, item))
		}
	}
	return details, nil
}

func buildCompoundRevenueDetailItem(period compoundPeriodRange, entity string, item contractAggregateOpenItem) map[string]any {
	return map[string]any{
		"source":             compoundSourceContractRevenue,
		"entity":             entity,
		"period_label":       period.Label,
		"period_from":        period.From,
		"period_to":          period.To,
		"customer_name":      item.CustomerName,
		"contract_content":   item.ContractContent,
		"settlement_amount":  round2(item.SettlementAmount),
		"invoice_amount":     round2(item.InvoiceAmount),
		"received_amount":    round2(item.ReceivedAmount),
		"unreceived_amount":  round2(item.OpenAmount),
		"unpaid_amount":      round2(item.OpenAmount),
		"unpaid_metric_note": "settlement_amount - received_amount, floored at 0",
	}
}

func buildCompoundCostDetailItem(period compoundPeriodRange, entity string, item contractAggregateOpenItem) map[string]any {
	return map[string]any{
		"source":            compoundSourceContractCost,
		"entity":            entity,
		"period_label":      period.Label,
		"period_from":       period.From,
		"period_to":         period.To,
		"supplier_name":     item.CustomerName,
		"contract_content":  item.ContractContent,
		"settlement_amount": round2(item.SettlementAmount),
		"invoice_amount":    round2(item.InvoiceAmount),
		"paid_amount":       round2(item.ReceivedAmount),
		"unpaid_amount":     round2(item.OpenAmount),
	}
}

func hasCompoundMetricKey(metrics []compoundSourceMetric, key string) bool {
	for _, metric := range metrics {
		if metric.Key == key {
			return true
		}
	}
	return false
}

func looksLikeCustomerContractUnpaidQuestion(q string) bool {
	if !containsAny(q, []string{"未付款", "未支付", "未付", "未回款", "未收款", "客户未付款", "客户未支付"}) {
		return false
	}
	if containsAny(q, []string{"已开票未付款", "已开发票未付款", "开票未付款", "发票未付款"}) {
		return false
	}
	return !containsAny(q, []string{"供应商", "采购", "成本", "应付", "收票", "收到发票", "已收发票", "已收票"})
}

type compoundBankTotals struct {
	Received float64
	Paid     float64
	RowCount int
}

type compoundJournalTotals struct {
	Debit    float64
	Credit   float64
	RowCount int
}

func (e *Engine) collectCompoundMetricValues(ctx context.Context, metrics []compoundSourceMetric, periodFrom, periodTo, like string) (map[string]any, map[string]int, error) {
	values := map[string]any{}
	sourceRows := map[string]int{}
	if hasCompoundMetricSource(metrics, compoundSourceContractRevenue) {
		totals, err := e.collectFundIncomeTotals(ctx, periodFrom, periodTo, like)
		if err != nil {
			return nil, nil, err
		}
		values["contract_revenue.settlement"] = round2(totals.Settlement)
		values["contract_revenue.received"] = round2(totals.Received)
		values["contract_revenue.invoice"] = round2(totals.Invoice)
		values["contract_revenue.unpaid"] = round2(compoundNetOpen(totals.Settlement, totals.Received))
		sourceRows[compoundSourceContractRevenue] = totals.RowCount
	}
	if hasCompoundMetricSource(metrics, compoundSourceContractCost) {
		totals, err := e.collectCostSettlementTotals(ctx, periodFrom, periodTo, like)
		if err != nil {
			return nil, nil, err
		}
		values["contract_cost.settlement"] = round2(totals.Settlement)
		values["contract_cost.paid"] = round2(totals.Paid)
		values["contract_cost.invoice"] = round2(totals.Invoice)
		values["contract_cost.unpaid"] = round2(compoundNetOpen(totals.Settlement, totals.Paid))
		values["contract_cost.invoiced_unpaid"] = round2(totals.InvoiceOpen)
		sourceRows[compoundSourceContractCost] = totals.RowCount
	}
	if hasCompoundMetricSource(metrics, compoundSourceBankStatement) {
		totals, err := e.collectCompoundBankTotals(ctx, periodFrom, periodTo, like)
		if err != nil {
			return nil, nil, err
		}
		values["bank_statement.received"] = round2(totals.Received)
		values["bank_statement.paid"] = round2(totals.Paid)
		sourceRows[compoundSourceBankStatement] = totals.RowCount
	}
	if hasCompoundMetricSource(metrics, compoundSourceJournal) {
		totals, err := e.collectCompoundJournalTotals(ctx, periodFrom, periodTo, like)
		if err != nil {
			return nil, nil, err
		}
		values["journal.debit"] = round2(totals.Debit)
		values["journal.credit"] = round2(totals.Credit)
		sourceRows[compoundSourceJournal] = totals.RowCount
	}
	return values, sourceRows, nil
}

func compoundNetOpen(total, movement float64) float64 {
	if total <= movement {
		return 0
	}
	return total - movement
}

func (e *Engine) collectCompoundBankTotals(ctx context.Context, periodFrom, periodTo, like string) (compoundBankTotals, error) {
	var totals compoundBankTotals
	sqlText := `
SELECT COALESCE(SUM(credit_amount), 0), COALESCE(SUM(debit_amount), 0), COUNT(*)
FROM bank_statement
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND counterparty_name LIKE ?
  AND transaction_date BETWEEN ? AND ?`
	err := e.db.QueryRowContext(ctx, sqlText, e.Company, e.Company, like, periodFrom+"-01", monthEndDay(periodTo)).Scan(&totals.Received, &totals.Paid, &totals.RowCount)
	return totals, err
}

func (e *Engine) collectCompoundJournalTotals(ctx context.Context, periodFrom, periodTo, like string) (compoundJournalTotals, error) {
	var totals compoundJournalTotals
	sqlText := `
SELECT COALESCE(SUM(debit_amount), 0), COALESCE(SUM(credit_amount), 0), COUNT(*)
FROM journal
WHERE (? LIKE '%' || company || '%' OR company LIKE '%' || ? || '%')
  AND (counterparty LIKE ? OR summary LIKE ?)
  AND (
    period BETWEEN ? AND ?
    OR SUBSTR(CAST(voucher_date AS TEXT), 1, 7) BETWEEN ? AND ?
  )`
	err := e.db.QueryRowContext(ctx, sqlText, e.Company, e.Company, like, like, periodFrom, periodTo, periodFrom, periodTo).Scan(&totals.Debit, &totals.Credit, &totals.RowCount)
	return totals, err
}

func hasCompoundMetricSource(metrics []compoundSourceMetric, source string) bool {
	for _, metric := range metrics {
		if metric.Source == source {
			return true
		}
	}
	return false
}

func compoundSourceRowCount(sourceRows map[string]int) int {
	total := 0
	for _, count := range sourceRows {
		total += count
	}
	return total
}

func applyCompoundLegacyItemFields(item map[string]any, values map[string]any) {
	if v, ok := values["contract_revenue.settlement"]; ok {
		item["settlement"] = v
	}
	if v, ok := values["contract_revenue.received"]; ok {
		item["received"] = v
	}
	if v, ok := values["contract_revenue.invoice"]; ok {
		item["invoice_amount"] = v
	}
	if v, ok := values["contract_revenue.unpaid"]; ok {
		item["unpaid_amount"] = v
	}
}

func filterCompoundMetricValues(values map[string]any, metrics []compoundSourceMetric) map[string]any {
	out := make(map[string]any, len(metrics))
	for _, metric := range metrics {
		out[metric.Key] = values[metric.Key]
	}
	return out
}

func formatCompoundMetricParts(values map[string]any, metrics []compoundSourceMetric) string {
	parts := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		parts = append(parts, fmt.Sprintf("%s %.2f 元", metric.Label, anyToFloat64(values[metric.Key])))
	}
	return strings.Join(parts, "，")
}

func compoundMetricKeys(metrics []compoundSourceMetric) []string {
	out := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		out = append(out, metric.Key)
	}
	return out
}

func compoundMetricDisplay(metrics []compoundSourceMetric) string {
	labels := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		labels = append(labels, metric.Label)
	}
	return strings.Join(labels, "、")
}

func compoundSourceMetricKind(metrics []compoundSourceMetric) MetricKind {
	for _, metric := range metrics {
		if metric.Source == compoundSourceContractCost || metric.Key == "bank_statement.paid" {
			return MetricKindCost
		}
		if metric.Key == "contract_revenue.received" || metric.Key == "contract_revenue.unpaid" || metric.Key == "bank_statement.received" {
			return MetricKindReceipts
		}
	}
	return MetricKindRevenue
}

func compoundBossMetric(metrics []compoundSourceMetric) BossMetric {
	for _, metric := range metrics {
		if metric.Source == compoundSourceContractCost || metric.Key == "bank_statement.paid" {
			return BossMetricCost
		}
		if metric.Key == "contract_revenue.received" || metric.Key == "contract_revenue.unpaid" || metric.Key == "bank_statement.received" {
			return BossMetricReceipts
		}
	}
	for _, metric := range metrics {
		if strings.Contains(metric.Key, "invoice") {
			return BossMetricInvoice
		}
	}
	return BossMetricRevenue
}

func compoundAskedTopic(metrics []compoundSourceMetric) string {
	for _, metric := range metrics {
		if metric.Source == compoundSourceBankStatement || strings.Contains(metric.Key, "received") || strings.Contains(metric.Key, "paid") || strings.Contains(metric.Key, "unpaid") {
			return "receipts"
		}
	}
	return "revenue"
}

func compoundMetricExecutedSQL(metrics []compoundSourceMetric) []string {
	sqls := make([]string, 0, 4)
	if hasCompoundMetricSource(metrics, compoundSourceContractRevenue) {
		sqls = append(sqls, "compound(contract_revenue): SELECT SUM(settlement_amount), SUM(received_amount), SUM(invoice_amount), SUM(open_amount) FROM fin_fund_income + fin_fund_income_groups WHERE customer/member matches AND year_month BETWEEN ? AND ?")
	}
	if hasCompoundMetricSource(metrics, compoundSourceContractCost) {
		sqls = append(sqls, "compound(contract_cost): SELECT SUM(settlement_amount), SUM(paid_amount), SUM(invoice_amount), SUM(open_amount) FROM fin_cost_settlements + fin_cost_settlement_groups WHERE customer/member matches AND year_month BETWEEN ? AND ?")
	}
	if hasCompoundMetricSource(metrics, compoundSourceBankStatement) {
		sqls = append(sqls, "compound(bank_statement): SELECT SUM(credit_amount), SUM(debit_amount) FROM bank_statement WHERE counterparty_name LIKE ? AND transaction_date BETWEEN ? AND ?")
	}
	if hasCompoundMetricSource(metrics, compoundSourceJournal) {
		sqls = append(sqls, "compound(journal): SELECT SUM(debit_amount), SUM(credit_amount) FROM journal WHERE counterparty/summary matches AND period BETWEEN ? AND ?")
	}
	return sqls
}

func compoundPrimaryTables(metrics []compoundSourceMetric) []string {
	tables := make([]string, 0, 8)
	if hasCompoundMetricSource(metrics, compoundSourceContractRevenue) {
		tables = append(tables, "fin_fund_income", "fin_fund_income_groups")
	}
	if hasCompoundMetricSource(metrics, compoundSourceContractCost) {
		tables = append(tables, "fin_cost_settlements", "fin_cost_settlement_groups")
	}
	if hasCompoundMetricSource(metrics, compoundSourceBankStatement) {
		tables = append(tables, "fin_bank_statement")
	}
	if hasCompoundMetricSource(metrics, compoundSourceJournal) {
		tables = append(tables, "fin_journal")
	}
	return dedupeSourceTables(tables...)
}

func compoundSupportingTables(metrics []compoundSourceMetric) []string {
	tables := make([]string, 0, 4)
	if hasCompoundMetricSource(metrics, compoundSourceContractRevenue) {
		tables = append(tables, "fin_contracts", "fin_fund_income_group_members")
	}
	if hasCompoundMetricSource(metrics, compoundSourceContractCost) {
		tables = append(tables, "fin_contracts", "fin_cost_settlement_group_members")
	}
	return dedupeSourceTables(tables...)
}

func (e *Engine) resolveMentionedContractCustomers(question string) []string {
	nq := normalizeEntityText(question)
	if nq == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]compoundEntityMention, 0)
	candidates := append([]string{}, e.contractCustomerCandidates()...)
	candidates = append(candidates, e.counterpartyNameCandidates()...)
	for _, name := range candidates {
		if _, ok := seen[name]; ok {
			continue
		}
		position, length, ok := compoundEntityMentionPosition(nq, name)
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, compoundEntityMention{Name: name, Position: position, Length: length})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position == out[j].Position {
			return out[i].Length > out[j].Length
		}
		return out[i].Position < out[j].Position
	})
	entities := make([]string, 0, len(out))
	for _, item := range out {
		if compoundEntityCoveredByExisting(entities, item.Name) {
			continue
		}
		entities = append(entities, item.Name)
	}
	if len(entities) > 8 {
		return entities[:8]
	}
	return entities
}

func compoundEntityCoveredByExisting(existing []string, candidate string) bool {
	cn := normalizeEntityText(candidate)
	if cn == "" {
		return true
	}
	for _, item := range existing {
		en := normalizeEntityText(item)
		if en == "" {
			continue
		}
		if strings.Contains(en, cn) || strings.Contains(cn, en) {
			return true
		}
	}
	return false
}

func compoundEntityMentionPosition(normalizedQuestion, name string) (int, int, bool) {
	bestPosition := -1
	bestLength := 0
	for _, alias := range compoundEntityMentionAliases(name) {
		if len([]rune(alias)) < 4 {
			continue
		}
		position := strings.Index(normalizedQuestion, alias)
		if position < 0 {
			continue
		}
		length := len([]rune(alias))
		if bestPosition < 0 || position < bestPosition || (position == bestPosition && length > bestLength) {
			bestPosition = position
			bestLength = length
		}
	}
	return bestPosition, bestLength, bestPosition >= 0
}

func compoundEntityMentionAliases(name string) []string {
	normalized := normalizeEntityText(name)
	if normalized == "" {
		return nil
	}
	aliases := []string{normalized}
	short := normalized
	for _, suffix := range []string{"信息科技有限公司", "科技有限公司", "技术有限公司", "有限责任公司", "股份有限公司", "有限公司", "公司"} {
		ns := normalizeEntityText(suffix)
		if strings.HasSuffix(short, ns) {
			short = strings.TrimSuffix(short, ns)
			break
		}
	}
	if short != "" && short != normalized {
		aliases = append(aliases, short)
		if prefix := firstRunes(short, 4); prefix != "" && prefix != short {
			aliases = append(aliases, prefix)
		}
	}
	return dedupeSourceTables(aliases...)
}

func firstRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) < n {
		return ""
	}
	return string(runes[:n])
}

func extractCompoundPeriodRanges(question string, anchor time.Time, fallbackFrom, fallbackTo string) []compoundPeriodRange {
	if ranges := extractExplicitCompoundQuarterRanges(question, anchor); len(ranges) > 0 {
		return ranges
	}
	if strings.TrimSpace(fallbackFrom) == "" || strings.TrimSpace(fallbackTo) == "" {
		return nil
	}
	return []compoundPeriodRange{{
		Label: displayPeriod(fallbackFrom, fallbackTo),
		From:  fallbackFrom,
		To:    fallbackTo,
	}}
}

func extractExplicitCompoundQuarterRanges(question string, anchor time.Time) []compoundPeriodRange {
	quarterRe := regexp.MustCompile(`(?i)(20\d{2}|\d{2})?\s*年?\s*(?:第?\s*([一二三四1234])\s*季度|Q\s*([1-4]))`)
	matches := quarterRe.FindAllStringSubmatch(question, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]compoundPeriodRange, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if len(m) != 4 {
			continue
		}
		quarter := parseCompoundQuarterToken(m[2])
		if quarter == 0 {
			quarter = parseCompoundQuarterToken(m[3])
		}
		if quarter == 0 {
			continue
		}
		year := anchor.Year()
		if strings.TrimSpace(m[1]) != "" {
			year = normalizeCompoundYearToken(m[1])
		} else {
			from, _ := ExtractPeriodWithNow(m[0], anchor)
			year, _ = parsePeriod(from)
		}
		from := fmt.Sprintf("%04d-%02d", year, (quarter-1)*3+1)
		to := fmt.Sprintf("%04d-%02d", year, (quarter-1)*3+3)
		key := from + "~" + to
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, compoundPeriodRange{
			Label: fmt.Sprintf("%d年Q%d", year, quarter),
			From:  from,
			To:    to,
		})
	}
	return out
}

func parseCompoundQuarterToken(token string) int {
	token = strings.ToUpper(strings.TrimSpace(token))
	token = strings.TrimPrefix(token, "Q")
	switch token {
	case "1", "一":
		return 1
	case "2", "二", "两":
		return 2
	case "3", "三":
		return 3
	case "4", "四":
		return 4
	default:
		return 0
	}
}

func normalizeCompoundYearToken(raw string) int {
	y := mustAtoi(strings.TrimSpace(raw))
	if y >= 0 && y < 100 {
		return 2000 + y
	}
	return y
}

func mergedPeriodBounds(periods []compoundPeriodRange) (string, string) {
	if len(periods) == 0 {
		return "", ""
	}
	from := periods[0].From
	to := periods[0].To
	for _, period := range periods[1:] {
		if period.From < from {
			from = period.From
		}
		if period.To > to {
			to = period.To
		}
	}
	return from, to
}

func compoundPeriodsData(periods []compoundPeriodRange) []map[string]any {
	out := make([]map[string]any, 0, len(periods))
	for _, period := range periods {
		out = append(out, map[string]any{
			"label": period.Label,
			"from":  period.From,
			"to":    period.To,
		})
	}
	return out
}
