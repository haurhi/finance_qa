# FinanceQA Low-frequency Dry-run Schedules

These examples run the real OpenClaw FinanceQA path as a low-frequency dry-run and write local report artifacts only. They do not send IM messages and do not call OpenClaw with delivery flags.

Before installing either cron or systemd templates:

1. Copy `financeqa-daily.env.example` to `financeqa-daily.env`.
2. Install this checkout on a non-production OpenClaw host.
3. Confirm that host has `openclaw-finance` installed and test data loaded.
4. Adjust `/opt/finance_qa/agent-patrol` to the actual checkout path.
5. Confirm `FINANCEQA_MCP_URL` and `FINANCEQA_MCP_READ_TOKEN_FILE` point at a read-only FinanceQA MCP endpoint/token. This is the direct-tool baseline, not the golden answer.
6. Confirm the command manually:

```bash
test -d /root/.openclaw/extensions/openclaw-finance
```

```bash
cd /opt/finance_qa/agent-patrol
AGENT_PATROL_ENV_FILE=examples/schedules/financeqa-daily.env \
examples/schedules/run-financeqa-dry-run.sh
```

The default schedule runs two `smoke` suite dry-runs per day, with jitter in the systemd timer. Reports are written under `tmp/financeqa-dry-run/`. The wrapper treats a generated `summary.json` as schedule success and records business accuracy separately in `dry-run.log` as `business_status=threshold_passed` or `business_status=threshold_failed`; set `AGENT_PATROL_FAIL_ON_THRESHOLD=1` only when a scheduler should fail on low accuracy. Without a configured structured `goldenReference`, these reports use direct `finance-query` only as a diagnostic baseline. Do not use that mode as a 90% business-accuracy gate.

The systemd service templates pass `AGENT_PATROL_ENV_FILE` to the wrapper instead of relying on systemd `EnvironmentFile=` expansion. This keeps quoted commands and same-file references such as `${FINANCEQA_REFERENCE_SNAPSHOT}` evaluated by bash `source`, matching manual dry-runs.

For FinanceQA dry-runs that need structured golden evidence, prefer a local snapshot reference:

```bash
# On a host with read-only RDS env such as PGHOST/PGUSER/PGDATABASE/FINANCEQA_PG_SCHEMA:
FINANCEQA_SNAPSHOT_OUTPUT=tmp/reference-snapshots/financeqa-latest.json.gz \
FINANCEQA_SQLITE_MIRROR_OUTPUT=tmp/reference-snapshots/financeqa-latest.sqlite \
examples/golden/export_financeqa_snapshot.sh
```

Then set `FINANCEQA_REFERENCE_SNAPSHOT`, `FINANCEQA_SQLITE_MIRROR_OUTPUT`, `AGENT_PATROL_PREPARE_CMD=examples/schedules/prepare-financeqa-snapshot-mirror.sh`, and `FINANCEQA_GOLDEN_CMD` as shown in `financeqa-daily.env.example`. Copy the preset YAML to a local config, enable its `goldenReference` block, and point `AGENT_PATROL_CONFIG` at that file.

The snapshot command reads `fin_*` rows from the local `.json.gz` and does not call `finance-query`; the export includes direct rows, merged-group rows, and group-member rows so the reference can net merged amounts against member receipts/payments. The SQLite mirror command builds the local FinanceQA MCP database from that same snapshot, so the actual agent path and golden provider compare against the same data. The older `financeqa_canonical_golden.mjs` command is still available for diagnostics, but it canonicalizes the prompt and then calls `finance-query`, so it is not an independent business reference.

FinanceQA payable templates intentionally separate two cost-side metrics:

- `未付款`, `应付未付`, and `项目成本口径未付款` use project payable: project cost minus paid amount.
- `已收票未付款`, `收到发票未付款`, and `发票未付` use invoice payable: received invoice amount minus paid amount and invoice-open offsets.

## Current FinanceQA Patrol Flow

1. A low-frequency scheduler starts `examples/schedules/run-financeqa-dry-run.sh`.
2. The wrapper loads `examples/schedules/financeqa-daily.env`.
3. If configured, `AGENT_PATROL_PREPARE_CMD` rebuilds the local FinanceQA SQLite mirror from `FINANCEQA_REFERENCE_SNAPSHOT`, keeping the OpenClaw actual path and the golden provider on the same snapshot.
4. `agent-patrol run` loads `AGENT_PATROL_CONFIG` and selects deterministic cases from the requested suite.
5. If configured, target-level `questionGenerator` calls `AGENT_PATROL_QUESTION_GEN_CMD` to rewrite only the natural-language question. The case id, template, intent, period mode, scoring rules, and golden answer stay deterministic.
6. The actual path asks OpenClaw through `OPENCLAW_AGENT_CMD`; FinanceQA profiles add `--require-tool finance-query` so the patrol message explicitly requires a fresh read-only FinanceQA tool call before answering.
7. The golden path calls `financeqa_snapshot_reference.mjs` against the local snapshot, not OpenClaw and not direct `finance-query`.
8. The optional direct FinanceQA MCP baseline is kept only for diagnostics.
9. Reports are written to `tmp/financeqa-dry-run/<run_id>/`, including `summary.md`, `summary.json`, `cases.json`, `case_evidence.jsonl`, and `failed_cases/*.json`.
10. The wrapper separates scheduler health from answer quality: `report_status=generated` means the report artifact was produced, while `business_status=runner_invalid`, `valid_business_threshold_passed`, `valid_business_threshold_failed`, `threshold_passed`, or `threshold_failed` records how the run should be interpreted.
11. If configured, `AGENT_PATROL_CLEANUP_CMD` prunes only patrol-owned session transcripts for the runner kinds listed in `AGENT_PATROL_CLEANUP_KINDS`.

## Optional LLM Question Variation

FinanceQA patrol can ask more boss-like, varied questions without letting an LLM define the expected answer. Enable the target-level `questionGenerator` block in the YAML and set `AGENT_PATROL_QUESTION_GEN_CMD` to the bundled wrapper:

```bash
AGENT_PATROL_QUESTION_GEN_CMD="node examples/question-generators/llm_command_rewriter.mjs --input {inputFile}"
AGENT_PATROL_LLM_CMD="node examples/question-generators/openai_compatible_chat.mjs"
AGENT_PATROL_LLM_ENV_FILE=tmp/secrets/agent-patrol-llm.env
AGENT_PATROL_LLM_API_KEY_ENV=DEEPSEEK_API_KEY
AGENT_PATROL_LLM_BASE_URL_ENV=DEEPSEEK_BASE_URL
AGENT_PATROL_LLM_MODEL_ENV=DEEPSEEK_MODEL
AGENT_PATROL_LLM_RESPONSE_FORMAT=json_object
AGENT_PATROL_LLM_MAX_TOKENS=4096
```

The secret file should contain only provider settings and should not be committed:

```bash
DEEPSEEK_API_KEY=...
DEEPSEEK_BASE_URL=https://api.deepseek.com
DEEPSEEK_MODEL=deepseek-v4-flash
```

The recommended `openai_compatible_chat.mjs` command calls an OpenAI-compatible chat completions API directly, so it does not create OpenClaw/Hermes/Claude sessions. `AGENT_PATROL_LLM_CMD` can also point at any local LLM/agent CLI wrapper that reads the prompt from stdin and writes a JSON answer to stdout, but that may create agent session artifacts depending on the CLI.

`AGENT_PATROL_LLM_RESPONSE_FORMAT=json_object` sends `response_format: {"type":"json_object"}` to compatible chat-completions providers. `AGENT_PATROL_LLM_MAX_TOKENS` defaults to `4096` so a 14-case daily JSON rewrite is not easily truncated. If a provider rejects `response_format`, unset it; the parser still validates malformed output and fails closed without recursive stack overflows.

The wrapper asks the LLM to return this protocol:

- Input is provided as JSON on stdin and also written to `{inputFile}`.
- Input cases include `caseId`, `template`, `originalQuestion`, and scoring metadata.
- Output must be a JSON object containing `questions` with `caseId`, `template`, and `question`. Markdown JSON fences are tolerated by the bundled wrapper.
- The generator may only rewrite the natural-language question. It must not change intent, metric, period mode, or expected answer.
- The patrol runner validates `caseId` and `template`, rejects unsafe actions such as upload/sync/delete/restart/send, and falls back to the template question if the generated question is invalid.

Example output:

```json
{
  "source": "llm_question_generator",
  "questions": [
    {
      "caseId": "finance_qa_finance_project_receivable_unpaid_004",
      "template": "finance_project_receivable_unpaid",
      "question": "老板，从去年10月到上个完整月，项目上还有多少款没收回来？"
    }
  ]
}
```

The golden answer still comes from `financeqa_snapshot_reference.mjs`, not the LLM.

If you bypass the bundled wrapper and set `AGENT_PATROL_QUESTION_GEN_CMD` to a custom command, the patrol runner also accepts JSONL with one generated question per line.

This snapshot pattern is a FinanceQA-specific provider implementation. Other agent targets can use a different command, MCP, or HTTP reference provider as long as it returns the same golden-reference envelope.

The script can run `AGENT_PATROL_CLEANUP_CMD` after each run; `AGENT_PATROL_CLEANUP_KINDS` must explicitly match the agent runner(s) used by that patrol job, such as `openclaw`, `hermes`, `claude`, or a comma-separated list. Do not enable cleanup for agent runtimes that the patrol job does not use. The cleanup adapters prune old `patrol-*` transcripts only. Tune `AGENT_PATROL_SESSION_RETENTION_DAYS` in the env file.

## Production Hourly Profile

For the production host, use `financeqa-production-hourly.env.example`, `financeqa-production-hourly.service`, and `financeqa-production-hourly.timer` as the production profile. This profile differs from non-production test-host setup:

- It sets `AGENT_PATROL_ENV=production` and refuses `prepare-financeqa-snapshot-mirror.sh`.
- It exports a read-only snapshot from the live `fin_*` tables before each run with `examples/golden/export_financeqa_snapshot.sh`.
- It uses `presets/financeqa-production.yaml`, where `goldenReference` is enabled and direct `finance-query` remains diagnostic only.
- Its OpenClaw runner command includes `--require-tool finance-query`; this is a patrol-only instruction and does not change boss-facing cron prompts.
- It enables LLM question rewriting through `AGENT_PATROL_QUESTION_GEN_CMD`; the LLM may vary wording but never supplies the expected answer.
- It runs hourly with local report artifacts only and cleans only OpenClaw `patrol-*` sessions.
- It prunes old report directories with `AGENT_PATROL_REPORT_RETENTION_DAYS`.

Do not configure `AGENT_PATROL_PREPARE_CMD=examples/schedules/prepare-financeqa-snapshot-mirror.sh` on production. That prepare mode is for test hosts where the local FinanceQA MCP SQLite mirror may be rebuilt and the MCP service may be restarted.
