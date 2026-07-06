# FinanceQA Production Patrol

This document records the production-safe FinanceQA patrol profile for the production host.

## Goal

Run hourly local dry-runs against the real OpenClaw + FinanceQA MCP path, compare answers with a read-only snapshot golden reference, and write reports locally without sending boss messages or restarting FinanceQA/OpenClaw services.

## Runtime Paths

- Local source worktree: `/Users/gaorongvc/.config/superpowers/worktrees/finance_qa/agent-patrol-standalone/agent-patrol`
- Production install path: `/opt/finance_qa/agent-patrol`
- Production report path: `/var/log/agent-patrol/financeqa`
- Production OpenClaw sessions: `/root/.openclaw/agents/main/sessions`
- Production FinanceQA MCP: `http://127.0.0.1:3009/mcp`
- Production FinanceQA read token: `/root/finance_qa/secrets/mcp_read_token`

## Production Profile

Use these files:

- `presets/financeqa-production.yaml`
- `examples/schedules/financeqa-production-hourly.env`
- `examples/schedules/financeqa-production-hourly.service`
- `examples/schedules/financeqa-production-hourly.timer`

The timer runs hourly with jitter:

```ini
OnCalendar=*-*-* *:07:00
RandomizedDelaySec=5m
```

The systemd service sets `AGENT_PATROL_ENV_FILE=/opt/finance_qa/agent-patrol/examples/schedules/financeqa-production-hourly.env` and lets the wrapper source that file. Do not replace it with systemd `EnvironmentFile=`, because same-file references in command variables need bash expansion.

The production question-rewrite profile sets `AGENT_PATROL_LLM_RESPONSE_FORMAT=json_object`, so compatible LLM providers are asked for JSON output at the API layer. The wrapper defaults `AGENT_PATROL_LLM_MAX_TOKENS` to `4096` to reduce truncated multi-case JSON. The parser still falls back safely if the provider returns malformed text.

## Reference Source

The golden reference comes from a read-only snapshot export of the live `fin_*` tables using:

```bash
examples/golden/export_financeqa_snapshot.sh
```

The snapshot is written to:

```bash
tmp/reference-snapshots/financeqa-production-latest.json.gz
```

The golden provider then reads that snapshot:

```bash
node examples/golden/financeqa_snapshot_reference.mjs \
  --template {template} \
  --question-file {questionFile} \
  --snapshot tmp/reference-snapshots/financeqa-production-latest.json.gz
```

Direct `finance-query` remains diagnostic only. It must not be treated as the 90% accuracy reference when `goldenReference` is configured.

The production runner requires a fresh `finance-query` tool call for every FinanceQA case. If OpenClaw answers from session context, skill text, memory, or any other route without invoking `finance-query`, the case is marked `required_tool_missing` and counted as runner-invalid rather than a valid business pass.

Treat runner-invalid cases as patrol-path failures first. They prove the test did not exercise the intended OpenClaw + FinanceQA tool-call chain, even when the final wording happens to contain plausible amounts. Do not use runner-invalid cases to judge FinanceQA calculation accuracy until the agent path is isolated enough to show a real `finance-query` call in the session evidence.

FinanceQA schedules should pass `--require-tool finance-query` to the OpenClaw runner. This wraps the natural question with a patrol-only instruction requiring one fresh read-only tool call before answering. The original question is still stored in the case record; the wrapper is only for proving the agent path, not for changing golden references or boss-facing prompts.

The same snapshot export also writes dynamic case variables to:

```bash
tmp/reference-snapshots/financeqa-production-case-variables.json
```

Those variables provide non-zero customer, supplier, project, and contract keywords for named-entity patrol cases. They are data-derived from the snapshot and are not hardcoded in the preset.

The snapshot includes the project-operation tables and the explicit accounting/cash tables needed by the production preset:

- Project operation: `fin_contracts`, `fin_fund_income*`, `fin_cost_settlement*`.
- Accounting and cash: `fin_bank_statement`, `fin_balance_detail`, `fin_balance_sheet`, `fin_journal`.
- Source attribution: `fin_file_mappings`.

## Coverage

The production `daily` suite samples 14 cases per run so each current FinanceQA template family is covered every hour:

- Latest project settlement revenue.
- Project receivable unpaid and invoiced receivable unpaid.
- Project payable unpaid, invoiced payable unpaid, and unpaid project list.
- Named customer receivable and named supplier payable.
- Single contract/project receivable or payable by snapshot-derived keyword.
- Explicit balance-sheet wording: `账上` / `官方余额表` / `余额表`.
- Explicit bank-flow wording: `银行流水` / `银行卡上` / cash in/out/net.
- Explicit journal wording: `序时账` / `账上净利润`, including the default tax-inclusion note.
- Reconciliation wording: bank net inflow vs book net profit difference.

## FinanceQA Metric Boundaries

The FinanceQA golden templates keep project payable and invoice payable separate:

- `finance_project_payable_unpaid` and `finance_unpaid_projects` use project payable: project cost minus paid amount.
- `finance_project_invoiced_payable_unpaid` uses invoice payable: received invoice amount minus paid amount and invoice-open offsets.

Questions that only say `未付款`, `应付未付`, or `项目成本口径未付款` are scored against project payable. Questions that explicitly say `已收票`, `收到发票`, or `发票未付` are scored against invoice payable.

## Safety Rules

- Do not configure `AGENT_PATROL_PREPARE_CMD=examples/schedules/prepare-financeqa-snapshot-mirror.sh` on production.
- Do not restart `financeqa-mcp.service` or `openclaw-gateway.service` for patrol deployment.
- Do not use OpenClaw delivery flags.
- Clean up only the agent runtime actually used by the patrol job. The production profile uses OpenClaw only:

```bash
AGENT_PATROL_CLEANUP_KINDS=openclaw
```

Cleanup only removes old `patrol-*` session files. It does not remove manual user sessions.

## Verification Commands

Run doctor before enabling the timer:

```bash
cd /opt/finance_qa/agent-patrol
set -a
source examples/schedules/financeqa-production-hourly.env
source /root/finance_qa/.env
source tmp/secrets/agent-patrol-llm.env
set +a
npm run start -- doctor \
  --config "$AGENT_PATROL_CONFIG" \
  --require-golden-reference \
  --require-resolved-env
```

Run one local-report dry-run:

```bash
cd /opt/finance_qa/agent-patrol
AGENT_PATROL_ENV_FILE=examples/schedules/financeqa-production-hourly.env \
examples/schedules/run-financeqa-dry-run.sh
```

Check the timer:

```bash
systemctl list-timers --all 'financeqa-production-hourly.timer' --no-pager
systemctl status financeqa-production-hourly.timer --no-pager
```

Check latest report summary:

```bash
node -e '
const fs = require("fs");
const path = require("path");
const base = "/var/log/agent-patrol/financeqa";
const latest = fs.readdirSync(base).filter((name) => /^20|deploy-smoke-/.test(name)).sort().at(-1);
const summary = JSON.parse(fs.readFileSync(path.join(base, latest, "summary.json"), "utf8"));
console.log(JSON.stringify({ latest, aggregate: summary.aggregate, failedCases: (summary.failedCases || []).length }, null, 2));
'
```

Interpret the report in layers:

- `accuracy` / `businessAccuracy`: strict all-case metrics; runner-invalid cases count as failures so the headline cannot be inflated by cached answers.
- `validAccuracy` / `validBusinessAccuracy`: accuracy after excluding runner-invalid cases. Use this to judge FinanceQA/OpenClaw answer quality among cases that actually called the required tool.
- `runnerHealthPassed`: false when any case skipped the required tool, reused the wrong path, or failed before a valid agent answer.
- `validThresholdPassed` / `validBusinessThresholdPassed`: threshold checks over valid cases only. These help separate real answer quality from runner contamination.
- `failureTypes`: coarse scorer categories such as `agent_changed_amount`, `period_mismatch`, `missing_source`, or `required_tool_missing`.
- `failureDiagnoses`: narrower evidence labels. For example, `agent_changed_after_direct_tool` means the direct `finance-query` baseline contained the expected amount, but the agent-visible answer did not.

Response order for failures:

1. Fix `runner_health` failures first, especially `required_tool_missing:finance-query`. A report with runner-invalid cases is not a clean business-accuracy sample.
2. Fix `format_quality` scorer misses only when the answer is materially correct but uses acceptable wording, such as `应付余额` or `还有 0 元未付` for project payable.
3. Escalate valid `core_accuracy` failures to FinanceQA/OpenClaw analysis, such as wrong period resolution or missing required reconciliation amounts.

## Rollback

Disable the patrol timer:

```bash
systemctl disable --now financeqa-production-hourly.timer
```

This stops future patrol runs only. It does not change FinanceQA MCP, OpenClaw gateway, Feishu scan timers, or boss cron jobs.
