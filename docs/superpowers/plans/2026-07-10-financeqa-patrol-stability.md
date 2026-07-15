# FinanceQA Patrol Stability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make FinanceQA/OpenClaw preserve the current user question, keep company-wide finance queries out of entity routes, and expose complete multi-metric facts without changing existing entity-specific or explanation-oriented behavior.

**Architecture:** OpenClaw transports the model rewrite and current user question as two separate fields; Go MCP is the only semantic merge point. Query routing rejects ungrounded subjects only for clearly company-scope questions. Multi-metric producers explicitly declare required fact atoms, while reconciliation facts are always complete and headline promotion remains question-sensitive.

**Tech Stack:** Go 1.24, Node.js built-in test runner, OpenClaw plugin `dist/index.esm.js`, SQLite/PostgreSQL-backed FinanceQA query engine, shell-based version/deploy tooling.

---

## Execution status as of 2026-07-15

This plan has been implemented and deployed as FinanceQA / OpenClaw plugin
`2.2.64` on `lzh`.

| Check | Result |
| --- | --- |
| Current branch | `fix/financeqa-patrol-20260629` |
| Verified commit | `265e8bf` |
| Production runtime | `lzh:/root/finance_qa @ 265e8bf` |
| Production version | Go binary `2.2.64`; OpenClaw plugin `2.2.64` |
| Latest production patrol | `/var/log/agent-patrol/financeqa/20260715T171110`, `14/14`, invalid `0` |
| Last 48h production patrol | `666/672 = 99.11%`; valid accuracy `666/671 = 99.25%` |
| Local pre-merge verification | `git diff --check`; version preflight; OpenClaw plugin tests `39/39`; `make test-full`; `make test-business` |

Remaining release action: fast-forward merge this branch into `main`, push
`main`, and switch production checkout to `main@265e8bf` without changing the
already validated runtime logic.

## File map

- `plugin/openclaw-finance/dist/index.esm.js`: merge tool-factory/runtime scope, forward model `query` unchanged, and attach the run-scoped original question as `raw_user_query`.
- `plugin/openclaw-finance/test/remote-client.test.mjs`: red tests for partial runtime context, concurrent runs, unchanged model query, and multi-metric output guards.
- `internal/mcp/service.go`: sole protected merge for raw/rewrite, including continuation, company roster, and explicit `收入表` semantics.
- `internal/mcp/service_test.go`: table-driven red tests for merge decisions and preserved entity hints.
- `internal/query/query_entity_router.go`: shared structural-fragment rejection used by entity resolution.
- `internal/query/contract_dimension_routing.go`: company-scope roster recognition without blocking explicit subjects.
- `internal/query/query_context_resolution.go`: clear ungrounded entities at the single routing-resolution boundary.
- `internal/query/query_router_entity_test.go`: focused entity-fragment tests.
- `internal/query/contract_arap_priority_test.go`: end-to-end company roster and entity-route regressions.
- `internal/query/reconciliation.go`: always attach complete reconciliation facts; promote headline/message only for quantitative comparison wording.
- `internal/query/reconciliation_message.go`: quantitative-comparison predicate and message behavior.
- `internal/query/reconciliation_result_data.go`: split fact annotation from headline promotion.
- `internal/query/reconciliation_range_test.go`: comparison, explanation, range, and single-metric regressions.
- `internal/query/finance_facts.go`: reusable ordered metric-atom formatter.
- `internal/query/arap_queries.go`: official AR/AP metrics and required atoms.
- `internal/query/query_finalize_test.go` and `internal/query/query_context_resolution_test.go`: structured fact and route regressions.
- Version surfaces updated by `tests/scripts/bump_version.sh`: Go version, plugin manifests/docs, and version assertions.

### Task 1: Make OpenClaw transport the original question reliably

**Files:**
- Modify: `plugin/openclaw-finance/test/remote-client.test.mjs`
- Modify: `plugin/openclaw-finance/dist/index.esm.js:979-1125,1831-1854`

- [ ] **Step 1: Write failing tests for scope merging and transport ownership**

Update the harness so a tool factory can be created with one context and executed with a different partial runtime context. Add tests equivalent to:

```js
test("finance-query keeps factory run scope when runtime context is partial", async () => {
  const question = "老板，从账上看，最近完整月份的净利润是多少？";
  // before_prompt_build stores under run-a/session-a
  // tool factory is created with run-a/session-a
  // execute receives only { sessionKey: "session-a" }
  // assert outbound arguments.query is the model rewrite unchanged
  // assert outbound arguments.raw_user_query === question
});

test("finance-query transports model query unchanged and raw question separately", async () => {
  const question = "收入表中最新月份的营收数据是多少？";
  const rewrite = "收入表最新月份营收数据";
  // assert query === rewrite
  // assert raw_user_query === question
});

test("finance-query overwrites model supplied raw_user_query with the current run question", async () => {
  // model sends raw_user_query without the user's “从账上看” qualifier
  // assert outbound raw_user_query is the stored current-run question, not model input
});

test("finance-query concurrent runs do not exchange raw questions", async () => {
  // register run-a bank question and run-b revenue question on one session
  // execute in reverse order and assert each outbound raw_user_query matches its run
});

test("finance-query fails closed when one session has multiple ambiguous active runs", async () => {
  // execute without a run id after registering two active runs on the same session
  // assert neither run's question is borrowed and model-supplied raw_user_query is omitted
});
```

Change existing tests that currently expect JavaScript to merge the protected question into `query`; their new RED expectation is “query unchanged, raw_user_query protected.”

- [ ] **Step 2: Run the focused Node tests and verify RED**

Run:

```bash
node --test --test-name-pattern='finance-query|finance prompt hook' plugin/openclaw-finance/test/remote-client.test.mjs
```

Expected: new scope/transport assertions fail because `executionCtx` currently chooses rather than merges contexts and `query` is still rewritten in JavaScript.

- [ ] **Step 3: Implement the minimal context merge and transport-only plugin behavior**

Implement a single context helper and simplify execute:

```js
function financeExecutionContext(toolCtx, runtimeCtx) {
  const factory = isRecord(toolCtx) ? toolCtx : {};
  const runtime = isRecord(runtimeCtx) && !isAbortSignal(runtimeCtx) ? runtimeCtx : {};
  return { ...factory, ...runtime };
}

// createFinanceTool.execute
const executionCtx = financeExecutionContext(toolCtx, runtimeCtx);
const protectedQuestion = name === "finance-query"
  ? takeLatestFinanceQuestionForTool(executionCtx)
  : "";
const rawParamsObject = isRecord(rawParams) ? rawParams : {};
const { raw_user_query: _discardModelRawQuery, ...forwardedParams } = rawParamsObject;
const modelQuery = name === "finance-query"
  ? financeQuestionText(rawParamsObject.query || "")
  : "";
const params = name === "finance-query" && modelQuery
  ? {
      ...forwardedParams,
      query: modelQuery,
      ...(protectedQuestion ? { raw_user_query: protectedQuestion } : {})
    }
  : rawParams;
```

Remove now-unused JavaScript semantic-merge helpers only after `rg` confirms they have no other callers. Preserve the existing run/session maps and cleanup lifecycle.

- [ ] **Step 4: Run the focused Node tests and verify GREEN**

Run the Step 2 command. Expected: all selected tests pass, with no query/raw cross-run leakage.

- [ ] **Step 5: Commit the plugin transport change**

```bash
git add plugin/openclaw-finance/dist/index.esm.js plugin/openclaw-finance/test/remote-client.test.mjs
git commit -m "fix(openclaw): preserve run scoped finance questions"
```

### Task 2: Centralize protected raw/rewrite semantics in Go MCP

**Files:**
- Modify: `internal/mcp/service_test.go`
- Modify: `internal/mcp/service.go:107-285`

- [ ] **Step 1: Add a table-driven RED matrix**

Add cases covering the observed failures and controls:

```go
func TestEffectiveFinanceQueryProtectsCompanyScopeAndContinuationSemantics(t *testing.T) {
    cases := []struct {
        name, raw, rewritten string
        wantContains, wantNotContains []string
    }{
        {
            name: "unpaid project roster rejects structural rewrite",
            raw: "25年到26年未付款的项目都有哪些，对应的金额是多少？",
            rewritten: "2025年到2026年未付款的项目明细，包括项目名称和对应金额",
            wantContains: []string{"25年到26年", "未付款的项目", "对应的金额"},
            wantNotContains: []string{"补充识别：", "包括项目名称"},
        },
        {
            name: "income table source is protected",
            raw: "帮我看下收入表中最新月份的营收数据。",
            rewritten: "最新月份营收数据",
            wantContains: []string{"收入表", "最新月份", "营收"},
        },
        {
            name: "uninformative continuation falls back to raw",
            raw: "最近完整月份净利润是多少？",
            rewritten: "再算一次",
            wantContains: []string{"最近完整月份", "净利润"},
            wantNotContains: []string{"再算一次"},
        },
        {
            name: "specific entity hint remains available",
            raw: "上个完整自然月百度这个客户还有多少没收回来？",
            rewritten: "百度在线网络技术(北京)有限公司 项目应收未收",
            wantContains: []string{"百度在线网络技术(北京)有限公司"},
        },
    }
    // call effectiveFinanceQuery and assert all required/forbidden fragments
}
```

- [ ] **Step 2: Run the focused Go test and verify RED**

```bash
go test ./internal/mcp -run TestEffectiveFinanceQueryProtectsCompanyScopeAndContinuationSemantics -count=1 -v
```

Expected: roster, `收入表`, and continuation subtests fail under the current merge policy; entity control documents existing behavior.

- [ ] **Step 3: Implement minimal semantic predicates in one file**

Add `收入表` to the protected source terms and small helpers in `service.go`:

```go
func looksLikeFinanceContinuation(text string) bool {
    normalized := strings.TrimSpace(financeUserQuestionBlock(text))
    return normalized != "" && containsAnyText(normalized, []string{
        "继续", "接着", "再算一次", "重新算", "重算", "再查一次", "重新查",
    }) && !looksLikeFinanceQueryText(normalized)
}

func looksLikeCompanyScopeProjectRoster(text string) bool {
    q := strings.TrimSpace(text)
    return strings.Contains(q, "项目") &&
        containsAnyText(q, []string{"未付款", "应付未付", "应收未收", "未回款"}) &&
        containsAnyText(q, []string{"哪些", "有哪些", "都有哪些", "列出", "列表", "明细", "每个", "各"})
}
```

Integrate them into `shouldPreferRawFinanceSemantics`/`mergeProtectedFinanceQuery` so an uninformative continuation returns raw, and a company roster keeps raw as the semantic frame unless the rewrite contributes a validated specific subject. Do not make all raw questions authoritative.

- [ ] **Step 4: Run all MCP tests and verify GREEN**

```bash
go test ./internal/mcp -count=1
```

Expected: all MCP tests pass, including existing dynamic-period and entity-hint controls.

- [ ] **Step 5: Commit the Go MCP semantic merge**

```bash
git add internal/mcp/service.go internal/mcp/service_test.go
git commit -m "fix(mcp): protect finance roster and continuation semantics"
```

### Task 3: Keep company-scope queries out of entity routes

**Files:**
- Modify: `internal/query/query_router_entity_test.go`
- Modify: `internal/query/contract_arap_priority_test.go`
- Modify: `internal/query/query_entity_router.go`
- Modify: `internal/query/contract_dimension_routing.go`
- Modify: `internal/query/query_context_resolution.go`
- Modify: `internal/query/arap_queries.go`

- [ ] **Step 1: Write RED tests for structural and ungrounded entities**

Add focused entity tests:

```go
func TestIsRealishQueryEntityRejectsStructuralQuestionFragments(t *testing.T) {
    for _, entity := range []string{"包括", "包含", "期间", "每个", "对应"} {
        if isRealishQueryEntity(entity) {
            t.Fatalf("%q should not be a business entity", entity)
        }
    }
}
```

Add fixture-backed routing tests for:

- `2025年到2026年未付款的项目明细，包括项目名称和对应金额` -> company aggregate, empty entity, actual project coverage start, payable total and details.
- `收入表最新月份营收数据` with an unrelated real DB customer seeded -> company latest revenue aggregate, no fuzzy customer hijack.
- Official AR/AP wording with an account-fragment/fuzzy counterparty candidate -> company official balance route, using the same grounding primitive as other company-scope families.
- Explicit `南京林悦智能科技有限公司项目应付未付还有多少` -> entity route unchanged.
- Explicit named-customer latest revenue -> entity behavior unchanged.

- [ ] **Step 2: Run the focused query tests and verify RED**

```bash
go test ./internal/query -run 'TestIsRealishQueryEntityRejectsStructuralQuestionFragments|TestUnpaidProjectRoster.*Structural|TestLatestRevenue.*Ungrounded|Test.*Named.*Route' -count=1 -v
```

Expected: structural fragment and company aggregate tests fail; named-entity controls pass.

- [ ] **Step 3: Implement one company-scope grounding guard**

Extend the existing synthetic-fragment vocabulary for grammatical connectors, then add one shared grounding primitive in `query_entity_router.go`:

```go
func entityAppearsInQuestionText(question, entity string) bool {
    // normalized exact/fragment grounding only; no DB fuzzy inference
}

func shouldKeepCompanyScopeQuestionEntity(question, entity string, cfg RuleConfig) bool {
    if !isRealishQueryEntity(entity) {
        return false
    }
    if looksLikeCompanyScopeProjectAggregateQuestion(question) ||
        shouldUseLatestRevenueContractAggregate(question, cfg) ||
        shouldTreatAsCompanyOfficialARAPQuestion(question, entity) {
        return entityAppearsInQuestionText(question, entity)
    }
    return true
}
```

The grounding check must use the user-semantic question, reject a fuzzy DB candidate not mentioned by the user, and preserve explicit organization/contract/project names. Consolidate the primitive currently embedded in `entityAppearsAsCounterparty`; official AR/AP may pass `stripBalanceSheetAccountTerms(question)` to the shared primitive so account names cannot self-ground fragments. Keep only one primitive for text grounding and one company-scope policy gate; do not add case-specific company names.

- [ ] **Step 4: Run the full query package and verify GREEN**

```bash
go test ./internal/query -count=1
```

Expected: the complete query package passes, including existing customer/supplier/contract routing tests.

- [ ] **Step 5: Commit the company-scope routing guard**

```bash
git add internal/query/query_router_entity_test.go internal/query/contract_arap_priority_test.go internal/query/query_entity_router.go internal/query/contract_dimension_routing.go internal/query/query_context_resolution.go internal/query/arap_queries.go
git commit -m "fix(query): keep company finance questions entityless"
```

### Task 4: Always build complete reconciliation facts without always changing the headline

**Files:**
- Modify: `internal/query/reconciliation_range_test.go`
- Modify: `internal/query/reconciliation.go`
- Modify: `internal/query/reconciliation_message.go`
- Modify: `internal/query/reconciliation_result_data.go`
- Modify: `internal/query/finance_facts.go`

- [ ] **Step 1: Write RED table tests for comparison and explanation behavior**

Refactor the existing reconciliation fixture into a helper and add a table:

```go
func TestReconciliationAlwaysPublishesThreeFacts(t *testing.T) {
    cases := []struct {
        question string
        wantPromotedHeadline bool
    }{
        {"对比一下最近完整月份账上利润和银行流水净流入。", true},
        {"比较最近完整月份账上利润和银行流水净流入。", true},
        {"最近完整月份账上利润和银行流水净流入差了多少？", true},
        {"为什么最近完整月份账上利润和银行净流入差这么多？", false},
    }
    // Every case asserts cash_profit_reconciliation and three required atoms.
    // Quantitative cases assert nominal-difference headline/message.
    // Explanation case asserts its current narrative/headline is not replaced.
}
```

Keep existing range reconciliation and single-metric tests as controls.

- [ ] **Step 2: Run focused reconciliation tests and verify RED**

```bash
go test ./internal/query -run 'TestReconciliationAlwaysPublishesThreeFacts|TestCompareBookProfitAndBankNetInflow|TestReconciliationRange' -count=1 -v
```

Expected: comparison/explanation cases lack complete facts; explanation control proves why headline promotion must remain conditional.

- [ ] **Step 3: Split fact annotation from presentation promotion**

Add a reusable formatter in `finance_facts.go`:

```go
type financeMetricAtom struct {
    Label string
    Value float64
}

func financeMetricRequiredAtoms(metrics ...financeMetricAtom) []string {
    atoms := make([]string, 0, len(metrics))
    for _, metric := range metrics {
        atoms = append(atoms, fmt.Sprintf("%s：%.2f 元", metric.Label, metric.Value))
    }
    return atoms
}
```

Replace `annotateReconciliationNominalDifference` with:

```go
func annotateReconciliationFacts(data map[string]any, book monthlyBookView, cash *accounting.CashPerspective, difference float64) {
    // always add metrics, difference_summary.nominal_difference,
    // cash_profit_reconciliation, required_atoms, and explanation_hints
}

func promoteReconciliationDifferenceHeadline(data map[string]any, difference float64) {
    data["headline_metric"] = "账上净利润-银行净流入名义差额"
    data["headline_amount"] = difference
}
```

In `queryReconciliation`, always calculate/annotate facts; call headline promotion and `withReconciliationNominalDifferenceLine` only when a renamed predicate such as `shouldPromoteNominalReconciliationDifference(question)` matches quantitative comparison wording, including `对比/比较/差了多少`.

- [ ] **Step 4: Run all query tests and verify GREEN**

```bash
go test ./internal/query -count=1
```

Expected: all reconciliation, range, core metric, and routing tests pass.

- [ ] **Step 5: Commit reconciliation facts**

```bash
git add internal/query/reconciliation_range_test.go internal/query/reconciliation.go internal/query/reconciliation_message.go internal/query/reconciliation_result_data.go internal/query/finance_facts.go
git commit -m "fix(query): publish complete reconciliation facts"
```

### Task 5: Publish complete official AR/AP facts

**Files:**
- Modify: `internal/query/query_context_resolution_test.go`
- Modify: `internal/query/arap_queries.go`
- Reuse: `internal/query/finance_facts.go`

- [ ] **Step 1: Write a RED assertion on the existing official AR/AP regression**

Extend the seeded counterparty-hijack test to assert:

```go
facts := res.Data["finance_facts"].(map[string]any)
required := anySourceStringSlice(facts["required_atoms"])
for _, want := range []string{
    "应收账款：1897602.46 元",
    "应付账款：4585127.70 元",
    "其他应付款：500002.00 元",
    "应付端合计：5085129.70 元",
} {
    if !containsString(required, want) {
        t.Fatalf("required_atoms missing %q: %#v", want, required)
    }
}
```

Use fixture values in the actual test rather than production amounts. Also assert `finance_facts.metrics` contains all four labels.

Add a Node bridge contract test with a tool result containing all four AR/AP required atoms and an assistant answer containing only one amount. Assert the generic guard adds the other three, does not duplicate the existing one, and does not copy `final_answer`. This is a characterization test and may already be GREEN because the bridge's generic atom logic is not the root bug; the mandatory RED for this task is the producer-side Go assertion above.

- [ ] **Step 2: Run the focused AR/AP test and verify RED**

```bash
go test ./internal/query -run 'Test.*Official.*ARAP|Test.*BalanceSheet.*Hijack' -count=1 -v
```

Expected: routing and amounts pass, but the four structured fact atoms are absent.

- [ ] **Step 3: Add producer-owned metrics and required atoms**

In `queryCompanyOfficialARAP`, add:

```go
data["metrics"] = map[string]any{
    "应收账款": receivableTotal,
    "应付账款": payableTotal,
    "其他应付款": otherPayableTotal,
    "应付端合计": payableSideTotal,
}
data["finance_facts"] = map[string]any{
    "required_atoms": financeMetricRequiredAtoms(
        financeMetricAtom{"应收账款", receivableTotal},
        financeMetricAtom{"应付账款", payableTotal},
        financeMetricAtom{"其他应付款", otherPayableTotal},
        financeMetricAtom{"应付端合计", payableSideTotal},
    ),
}
```

Let `buildFinanceFacts` merge period, basis, source, and source-update atoms as it already does. Do not force every generic metric map into required atoms.

- [ ] **Step 4: Run query and OpenClaw fact-guard tests**

```bash
go test ./internal/query -count=1
node --test --test-name-pattern='fact atoms|reconciliation|denial' plugin/openclaw-finance/test/remote-client.test.mjs
```

Expected: all pass; the plugin consumes producer atoms without copying `final_answer`.

- [ ] **Step 5: Commit official AR/AP facts**

```bash
git add internal/query/arap_queries.go internal/query/query_context_resolution_test.go internal/query/finance_facts.go plugin/openclaw-finance/test/remote-client.test.mjs
git commit -m "fix(query): expose official arap fact atoms"
```

### Task 6: Run layered regression and MCP-pair A/B checks

**Files:**
- Modify only if a regression reveals an in-scope root cause; return to the relevant RED/GREEN task before editing.

- [ ] **Step 1: Run formatting and diff checks**

```bash
gofmt -w internal/mcp/service.go internal/mcp/service_test.go internal/query/*.go
git diff --check
```

Limit `gofmt` to actually touched Go files if `internal/query/*.go` would create unrelated churn.

- [ ] **Step 2: Run targeted package and plugin suites**

```bash
go test ./internal/mcp ./internal/query -count=1
node --test plugin/openclaw-finance/test/*.test.mjs
```

Expected: Go packages pass and Node reports all tests passing (baseline was 22/22 before changes).

- [ ] **Step 3: Run the repository regression ladder**

The isolated worktree intentionally has no `.env`. Reuse the main worktree's
read-only environment file without copying secrets, and keep the worktree's
rules file authoritative:

```bash
export FINANCEQA_ENV_FILE="${FINANCEQA_ENV_FILE:-/Users/gaorongvc/work/other/finance_qa/.env}"
export FINANCEQA_RULES_PATH="$PWD/config/rules.json"
export FINANCEQA_LIVE_DB_PARALLELISM=1
test -r "$FINANCEQA_ENV_FILE"
make test-fast
make test-integration
go test ./internal/... ./tests/integration ./tests/unit/... -count=1
GOFLAGS='-timeout=20m' make test-business
make test-full
```

Expected: every command exits 0. The business suite must actually connect to
the configured database; `database is not configured` followed by `SKIP` is a
failed precondition, not a green business run. If a pre-existing live DB test
fails, capture exact evidence and investigate before making any completion
claim.

- [ ] **Step 4: Build a local binary and replay exact query/raw pairs through MCP**

```bash
go build -o bin/financeqa ./cmd/financeqa
```

For each pair below, send a JSON-RPC `tools/call` to `./bin/financeqa serve` with both arguments; do not use the single-input `financeqa query` shortcut:

```json
{"query":"2025年到2026年未付款的项目明细，包括项目名称和对应金额","raw_user_query":"25年到26年未付款的项目都有哪些，对应的金额是多少？"}
{"query":"2026年6月应收账款、应付账款、其他应付款余额","raw_user_query":"老板，账上看一下上个完整自然月的应收账款、应付账款和其他应付款分别是多少？"}
{"query":"2026年6月净利润","raw_user_query":"老板，从账上看，最近完整月份的净利润是多少？"}
{"query":"收入表最新月份营收数据","raw_user_query":"帮我看下收入表中最新月份的营收数据。"}
{"query":"最近完整月份账上利润和银行流水净流入","raw_user_query":"对比一下最近完整月份账上利润和银行流水净流入。"}
{"query":"最近完整月份账上利润和银行净流入差异原因","raw_user_query":"为什么最近完整月份账上利润和银行净流入差这么多？"}
{"query":"2026年1月到2026年3月账上利润和银行净流入差异","raw_user_query":"为什么2026年第一季度利润和现金差这么多？"}
```

Use the same short Python JSON-RPC driver pattern already present in `tests/scripts/claude_finance_final_answer.sh`, but print the full payload so `query_spec`, `finance_facts`, and `required_atoms` can be asserted. Expected: roster/ARAP/journal/revenue pairs preserve raw period/source/scope while keeping valid model entity hints; comparison and explanation both publish three facts without moving the explanation headline; the range reconciliation control preserves `2026-01~2026-03`.

Run the driver with the same `FINANCEQA_ENV_FILE` and
`FINANCEQA_RULES_PATH` exported in Step 3 so the worktree binary uses the
intended live database and the branch's rules without copying `.env`.

- [ ] **Step 5: Review the complete diff for KISS and regression risk**

```bash
git status --short
git diff --stat HEAD~5..HEAD
git diff HEAD~5..HEAD -- internal/mcp internal/query plugin/openclaw-finance
```

Confirm there is one semantic merge owner, one company-scope grounding guard, no case-specific amounts/company names in production code, and no full-answer copy path.

### Task 7: Prepare and validate release 2.2.58

**Files:**
- Modify via script: `internal/buildinfo/version.go`
- Modify via script: `plugin/openclaw-finance/package.json`
- Modify via script: `plugin/openclaw-finance/openclaw.plugin.json`
- Modify via script: `plugin/openclaw-finance/server/README.md`
- Modify via script: `docs/architecture/03-deployment-runtime.md`
- Modify via script: `internal/mcp/server_test.go`
- Modify via script: `tests/integration/openclaw_finance_plugin_test.go`

- [ ] **Step 1: Bump once, only after implementation and regression are green**

```bash
tests/scripts/bump_version.sh 2.2.58
```

Expected: all canonical surfaces become `2.2.58`; the script's skip-diff precheck passes.

- [ ] **Step 2: Run the real version preflight and version tests**

```bash
tests/scripts/check_version_preflight.sh
go test ./internal/mcp ./tests/integration -run 'Version|PluginMetadata' -count=1
```

Expected: `version preflight ok: version 2.2.58` and version tests pass.

- [ ] **Step 3: Re-run the post-bump smoke suites**

```bash
go test ./internal/mcp ./internal/query -count=1
node --test plugin/openclaw-finance/test/*.test.mjs
git diff --check
```

- [ ] **Step 4: Commit the release surfaces**

```bash
git add internal/buildinfo/version.go plugin/openclaw-finance/package.json plugin/openclaw-finance/openclaw.plugin.json plugin/openclaw-finance/server/README.md docs/architecture/03-deployment-runtime.md internal/mcp/server_test.go tests/integration/openclaw_finance_plugin_test.go
git commit -m "chore: bump financeqa to 2.2.58"
```

### Task 8: Review, integrate, deploy, and verify production

**Files:**
- No new source files expected.

- [ ] **Step 1: Run an independent code review**

Use `superpowers:requesting-code-review` against the full branch diff. Address only verified issues, rerun affected tests, and repeat review if changes are material.

- [ ] **Step 2: Push the isolated branch and integrate it into the requested release branch**

```bash
git push -u origin fix/financeqa-patrol-stability-20260710
```

Use a non-destructive merge/cherry-pick strategy after checking the target branch has not moved. Do not overwrite unrelated work.

- [ ] **Step 3: Deploy with the repository's full sync path**

From the integrated release branch:

```bash
MODE=all SERVER=lzh tests/scripts/sync_openclaw_bridge_and_skill.sh
```

Expected: preflight passes, binary/plugin/skill sync completes, gateway restarts, and the script reports FinanceQA 2.2.58.

- [ ] **Step 4: Read back production runtime state**

```bash
ssh lzh '/root/finance_qa/bin/financeqa version'
ssh lzh 'systemctl is-active financeqa-mcp.service; systemctl --user is-active openclaw-gateway.service'
```

Expected: version `2.2.58`; both actual service scopes are active.

- [ ] **Step 5: Replay exact failed and control query/raw pairs on the production MCP path**

Run the same seven Task 6 JSON-RPC pairs through the production MCP process,
explicitly pointing the CLI at its protected env file:

```bash
ssh lzh 'cd /root/finance_qa && FINANCEQA_ENV_FILE=/root/finance_qa/.env ./bin/financeqa serve --skill SKILL.md --appendix docs/SKILL_APPENDIX_FULL.md'
```

Drive that stdin/stdout process with the same JSON-RPC helper used locally and
confirm structured fields, not only prose. A single-input direct query is an
additional control, not evidence for the repaired merge boundary.

- [ ] **Step 6: Replay the OpenClaw true chain at least five times per failure family**

The patrol CLI has no case/template selector, so do not present a selective
patrol command that does not exist. Use the same local OpenClaw runner and
`--require-tool` control as production for six exact controls, five fresh
sessions each. The currently deployed runner has no `--forbid-tools` option,
so put the prohibition in the patrol wrapper and fail the run unless the saved
transcript contains only `finance-query` tool calls:

```bash
ssh lzh 'bash -s' <<'REMOTE'
set -euo pipefail
export PATH=/root/.nvm/versions/node/v22.22.2/bin:/usr/local/bin:/usr/bin:/bin
export AGENT_PATROL_LIVE=1
cd /opt/finance_qa/agent-patrol
stamp=$(date +%Y%m%dT%H%M%S)
out=/var/tmp/financeqa-openclaw-release-$stamp
mkdir -p "$out"
while IFS='|' read -r family question; do
  [ -n "$family" ] || continue
  for n in 1 2 3 4 5; do
    qf=$(mktemp)
    {
      printf '%s\n' '[发布验收要求]'
      printf '%s\n' '只能调用 `finance-query`；禁止调用 message、feishu、exec、edit、cron、gateway 或任何写入/投递动作。'
      printf '%s\n' '调用后只回答用户原问题，不展示内部 JSON、工具过程或 final_answer。'
      printf '\n[用户原问题]\n%s\n' "$question"
    } >"$qf"
    sid="patrol-finance-release-${stamp}-${family}-${n}"
    node examples/runners/openclaw_local_runner.mjs \
      --question-file "$qf" --session-id "$sid" --thinking off --timeout 360 \
      --require-tool finance-query \
      >"$out/${family}-${n}.json"
    rm -f "$qf"
    python3 - \
      "$out/${family}-${n}.json" \
      /root/.openclaw/agents/main/sessions \
      "$sid" \
      "$out/${family}-${n}.tool-results.json" \
      "$out/${family}-${n}.answer.txt" <<'PY'
import json
import pathlib
import sys

raw_path, session_dir, expected_sid, tool_out, answer_out = sys.argv[1:]
raw = pathlib.Path(raw_path).read_text(encoding="utf-8")
try:
    payload = json.loads(raw)
except json.JSONDecodeError:
    decoder = json.JSONDecoder()
    payload = None
    start = raw.find("{")
    while start >= 0:
        try:
            payload, _ = decoder.raw_decode(raw[start:])
            break
        except json.JSONDecodeError:
            start = raw.find("{", start + 1)
    if payload is None:
        raise

result = payload.get("result", payload) if isinstance(payload, dict) else {}
meta = result.get("meta", {}) if isinstance(result, dict) else {}
agent_meta = meta.get("agentMeta", {}) if isinstance(meta, dict) else {}
session_id = (
    result.get("sessionId")
    or result.get("session_id")
    or agent_meta.get("sessionId")
    or expected_sid
)
session_path = pathlib.Path(session_dir, f"{session_id}.jsonl")
if not session_path.is_file() or session_path.stat().st_size == 0:
    raise SystemExit(f"missing session transcript: {session_path}")

answer = ""
for candidate in (
    result.get("answer"),
    result.get("final_answer"),
    result.get("output"),
):
    if isinstance(candidate, str) and candidate.strip():
        answer = candidate.strip()
        break
if not answer:
    payloads = result.get("payloads", [])
    answer = "\n\n".join(
        item.get("text", "").strip()
        for item in payloads
        if isinstance(item, dict) and isinstance(item.get("text"), str) and item.get("text", "").strip()
    )
if not answer:
    raise SystemExit("OpenClaw result has no visible answer")

events = [json.loads(line) for line in session_path.read_text(encoding="utf-8").splitlines() if line.strip()]
tool_calls = []
tool_results = []
for event in events:
    message = event.get("message", event) if isinstance(event, dict) else {}
    if message.get("role") == "assistant":
        content = message.get("content", [])
        if isinstance(content, list):
            for item in content:
                if isinstance(item, dict) and item.get("type") == "toolCall":
                    tool_calls.append(item.get("name", ""))
    if message.get("role") == "toolResult" and message.get("toolName") == "finance-query":
        tool_results.append(event)

if not tool_calls or any(name != "finance-query" for name in tool_calls):
    raise SystemExit(f"unexpected tool calls: {tool_calls}")
if not tool_results:
    raise SystemExit("missing finance-query tool result")

pathlib.Path(tool_out).write_text(json.dumps(tool_results, ensure_ascii=False, indent=2), encoding="utf-8")
pathlib.Path(answer_out).write_text(answer + "\n", encoding="utf-8")
PY
  done
done <<'CASES'
unpaid_roster|25年到26年未付款的项目都有哪些，对应的金额是多少？
official_arap|老板，账上看一下上个完整自然月的应收账款、应付账款和其他应付款分别是多少？
journal_profit|老板，从账上看，最近完整月份的净利润是多少？
latest_revenue|帮我看下收入表中最新月份的营收数据。
reconcile_compare|对比一下最近完整月份账上利润和银行流水净流入。
reconcile_explain|为什么最近完整月份账上利润和银行净流入差这么多？
CASES
printf '%s\n' "$out"
REMOTE
```

Expected: all 30 runs contain a non-empty visible answer, a fresh
`finance-query` tool call, and an actual tool result. Inspect the saved visible
answers and tool-result rows for the required period/basis/amount/source atoms.
This is true-chain replay evidence, not a substitute for the scored hourly
patrol in Step 7.

- [ ] **Step 7: Verify a complete post-deploy hourly patrol**

The production job is the system-scope
`financeqa-production-hourly.timer/.service` on `lzh`. Record the deployment
timestamp and the service's current `InvocationID`, then either wait for the
already-enabled timer (`*:07` plus up to five minutes jitter) or explicitly
start the service once:

```bash
ssh lzh 'systemctl list-timers --all financeqa-production-hourly.timer --no-pager'
ssh lzh 'systemctl show financeqa-production-hourly.service -p InvocationID -p ActiveState -p Result -p ExecMainStatus'
# Optional explicit run; this writes patrol reports/sessions and is not read-only.
ssh lzh 'systemctl start --no-block financeqa-production-hourly.service'
```

Poll until `InvocationID` changes and the service returns to inactive or failed.
Require `Result=success`, `ExecMainStatus=0`, and a matching final line in
`/var/log/agent-patrol/financeqa/dry-run.log` containing
`report_status=generated` and `service_exit=0`. Resolve the `out=` directory
from that line, then read its non-empty:

```text
/var/log/agent-patrol/financeqa/<run>/summary.json
/var/log/agent-patrol/financeqa/<run>/scores.json
```

Systemd success alone is insufficient because production sets
`AGENT_PATROL_FAIL_ON_THRESHOLD=0`. Require the new summary to report 14 total,
0 invalid, `runnerHealthPassed=true`, and
`validBusinessThresholdPassed=true`; then inspect every named failure family in
`scores.json`. Separate direct FinanceQA, OpenClaw visible-answer, runner, and
scorer states. Do not call the release complete until the named families have
current production evidence and no required work remains.
