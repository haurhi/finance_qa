package query

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

var contractContentCodePattern = regexp.MustCompile(`(?i)[A-Z]{1,8}\d{1,8}`)
var contractCustomerMentionSeparators = regexp.MustCompile(`[的和与及、,，;；:\s]+`)

func contractSubjectCandidatesQuery(column string) string {
	return `
SELECT ` + column + ` AS name
FROM fin_contracts
WHERE COALESCE(TRIM(` + column + `), '') <> ''
ORDER BY LENGTH(` + column + `) DESC, ` + column + `
`
}

func (e *Engine) contractSubjectCandidates(column string) []string {
	rows, err := e.db.Query(contractSubjectCandidatesQuery(column))
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]string, 0, 64)
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func (e *Engine) contractCustomerCandidates() []string {
	return e.contractSubjectCandidates("customer_name")
}

func (e *Engine) contractContentCandidates() []string {
	return e.contractSubjectCandidates("contract_content")
}

func (e *Engine) resolveContractSubjectCandidates(alias string) []string {
	alias = trimEntityNoiseSuffixes(stripTemporalNoise(strings.TrimSpace(alias)))
	if looksLikeBusinessDimensionLabel(alias) || looksLikePeriodOnlyEntity(alias) {
		return nil
	}
	if looksLikeContractContentAlias(alias) {
		if matches := rankContractContentAliasMatches(alias, e.contractContentCandidates()); len(matches) > 0 {
			return matches
		}
	}
	matches := rankCounterpartyAliasMatches(alias, e.contractCustomerCandidates())
	if len(matches) > 0 {
		return matches
	}
	return rankContractContentAliasMatches(alias, e.contractContentCandidates())
}

func (e *Engine) resolveContractSubject(question, entity string) string {
	if strings.TrimSpace(entity) == "" && shouldUseCompanyScopeContractAggregate(question) {
		return ""
	}
	nq := normalizeEntityText(question)
	if nq != "" {
		if content := e.resolveExactContractContentMention(question); content != "" {
			return content
		}
		for _, name := range e.contractCustomerCandidates() {
			nm := normalizeEntityText(name)
			if len([]rune(nm)) < 2 {
				continue
			}
			if strings.Contains(nq, nm) {
				return name
			}
		}
		if matches := rankContractContentAliasMatches(question, e.contractContentCandidates()); len(matches) > 0 {
			return matches[0]
		}
	}

	terms := []string{
		strings.TrimSpace(entity),
		extractNamedEntityFromQuestion(question),
		extractOrganizationEntityMatch(question),
	}
	seen := map[string]struct{}{}
	for _, term := range terms {
		term = trimEntityNoiseSuffixes(stripTemporalNoise(strings.TrimSpace(term)))
		if len([]rune(term)) < 2 || looksLikeBusinessDimensionLabel(term) || looksLikePeriodOnlyEntity(term) {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		if matches := e.resolveContractSubjectCandidates(term); len(matches) > 0 {
			return matches[0]
		}
	}

	if len(seen) > 0 {
		return ""
	}

	for _, seg := range chineseEntitySegmentPattern.FindAllString(strings.TrimSpace(question), -1) {
		runes := []rune(seg)
		for length := len(runes); length >= 2; length-- {
			for i := 0; i <= len(runes)-length; i++ {
				sub := trimEntityNoiseSuffixes(stripTemporalNoise(string(runes[i : i+length])))
				if shouldSkipEntityFragment(sub, 2) || looksLikeBusinessDimensionLabel(sub) || looksLikePeriodOnlyEntity(sub) {
					continue
				}
				if matches := e.resolveContractSubjectCandidates(sub); len(matches) > 0 {
					return matches[0]
				}
			}
		}
	}
	return ""
}

func (e *Engine) resolveExactContractContentMention(question string) string {
	nq := normalizeEntityText(question)
	if nq == "" {
		return ""
	}
	best := ""
	bestLen := 0
	for _, name := range e.contractContentCandidates() {
		nm := normalizeEntityText(name)
		nameLen := len([]rune(nm))
		if nameLen < 2 || !strings.Contains(nq, nm) {
			continue
		}
		if nameLen > bestLen {
			best = name
			bestLen = nameLen
		}
	}
	return best
}

func (e *Engine) detectContractRole(entity, from, to string) string {
	if resolved := e.resolveContractSubject("", entity); resolved != "" {
		entity = resolved
	}
	like := "%" + entity + "%"
	var costRows, fundRows int
	if totals, err := e.collectCostSettlementTotals(context.Background(), from, to, like); err == nil {
		costRows = totals.RowCount
	}
	if totals, err := e.collectFundIncomeTotals(context.Background(), from, to, like); err == nil {
		fundRows = totals.RowCount
	}

	hasCustomer := fundRows > 0
	hasSupplier := costRows > 0
	switch {
	case hasCustomer && hasSupplier:
		return "mixed_contract"
	case hasCustomer:
		return "customer_contract"
	case hasSupplier:
		return "supplier_contract"
	default:
		return "unknown"
	}
}

func (e *Engine) queryMatchingContracts(entity string) []contractDimensionRow {
	if resolved := e.resolveContractSubject("", entity); resolved != "" {
		entity = resolved
	}
	rows, err := e.db.Query(`
SELECT contract_id, customer_name, contract_content
FROM fin_contracts
WHERE customer_name LIKE ? OR contract_content LIKE ?
ORDER BY contract_id
`, "%"+entity+"%", "%"+entity+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]contractDimensionRow, 0)
	for rows.Next() {
		var row contractDimensionRow
		if err := rows.Scan(&row.ContractID, &row.CustomerName, &row.ContractContent); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (e *Engine) queryMatchingContractsForQuestion(question, entity string) []contractDimensionRow {
	contracts := e.queryMatchingContracts(entity)
	if len(contracts) <= 1 {
		return contracts
	}

	customers := e.mentionedContractCustomers(question)
	if len(customers) != 1 {
		return contracts
	}
	customer := normalizeEntityText(customers[0])
	if customer == "" || customer == normalizeEntityText(entity) {
		return contracts
	}

	filtered := make([]contractDimensionRow, 0, len(contracts))
	for _, contract := range contracts {
		rowCustomer := normalizeEntityText(contract.CustomerName)
		if rowCustomer == customer || strings.Contains(rowCustomer, customer) || strings.Contains(customer, rowCustomer) {
			filtered = append(filtered, contract)
		}
	}
	if len(filtered) == 0 {
		return contracts
	}
	return filtered
}

func (e *Engine) matchContractSubjectByName(question string) string {
	return e.resolveContractSubject(question, "")
}

func looksLikeContractContentAlias(alias string) bool {
	trimmed := strings.TrimSpace(alias)
	if trimmed == "" {
		return false
	}
	if containsAny(trimmed, []string{"合同", "协议", "项目"}) {
		return true
	}
	return contractContentCodePattern.MatchString(trimmed)
}

func normalizeContractContentText(s string) string {
	normalized := normalizeEntityText(s)
	normalized = strings.ReplaceAll(normalized, "合同", "")
	normalized = strings.ReplaceAll(normalized, "协议", "")
	return normalized
}

func rankContractContentAliasMatches(alias string, names []string) []string {
	nAlias := normalizeEntityText(alias)
	looseAlias := normalizeContractContentText(alias)
	if len([]rune(looseAlias)) < 2 {
		return nil
	}

	type candidateScore struct {
		name  string
		score int
	}
	matches := make([]candidateScore, 0, 8)
	seen := map[string]struct{}{}
	for _, name := range names {
		nName := normalizeEntityText(name)
		looseName := normalizeContractContentText(name)
		if len([]rune(looseName)) < 2 {
			continue
		}
		score := 0
		switch {
		case nName != "" && nName == nAlias:
			score = 20000 + len([]rune(nName))
		case looseName == looseAlias:
			score = 18000 + len([]rune(looseName))
		case nAlias != "" && strings.Contains(nName, nAlias):
			score = 15000 + len([]rune(nAlias))*100 - len([]rune(nName))
		case strings.Contains(looseName, looseAlias):
			score = 13000 + len([]rune(looseAlias))*100 - len([]rune(looseName))
		case nAlias != "" && strings.Contains(nAlias, nName):
			score = 11000 + len([]rune(nName))*60 - len([]rune(nAlias))
		case strings.Contains(looseAlias, looseName):
			score = 9000 + len([]rune(looseName))*60 - len([]rune(looseAlias))
		default:
			continue
		}
		if contractContentCodePattern.MatchString(alias) && !contractContentCodePattern.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		matches = append(matches, candidateScore{name: name, score: score})
		seen[name] = struct{}{}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return len([]rune(matches[i].name)) < len([]rune(matches[j].name))
		}
		return matches[i].score > matches[j].score
	})

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.name)
		if len(out) == 6 {
			break
		}
	}
	return out
}

func (e *Engine) mentionedContractCustomers(question string) []string {
	candidates := e.contractCustomerCandidates()
	if len(candidates) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
	}

	nq := normalizeEntityText(question)
	for _, candidate := range candidates {
		nc := normalizeEntityText(candidate)
		if len([]rune(nc)) >= 2 && strings.Contains(nq, nc) {
			add(candidate)
		}
	}

	for _, part := range contractCustomerMentionSeparators.Split(strings.TrimSpace(question), -1) {
		part = trimEntityNoiseSuffixes(stripTemporalNoise(strings.TrimSpace(part)))
		if len([]rune(part)) < 2 || shouldSkipEntityFragment(part, 2) {
			continue
		}
		if matches := rankCounterpartyAliasMatches(part, candidates); len(matches) > 0 {
			add(matches[0])
		}
	}

	out := make([]string, 0, len(seen))
	emitted := map[string]struct{}{}
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			if _, duplicated := emitted[candidate]; duplicated {
				continue
			}
			out = append(out, candidate)
			emitted[candidate] = struct{}{}
		}
	}
	return out
}
