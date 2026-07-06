# Agent Patrol MVP

## Goal

Build a standalone patrol tool that can run from cron/systemd, generate varied questions, ask the real agent surface, and write a report without sending IM messages or writing business data.

The first version is intentionally a health patrol for agent paths: can the agent be invoked, can the answer be parsed, is the session isolated, and did the answer satisfy deterministic scoring rules. Business-answer accuracy checks use an explicit golden reference track when one is configured.

## Boundary

```text
                              dynamic cases
                                   |
                         actual agent command
                  OpenClaw / Claude SDK dry-run / other agent CLI
                                   |
                         final answer + trace
                                   |
        golden reference scoring + direct-tool baseline diagnostics
                                   |
                             report artifacts
```

Direct `finance-query` or bossa MCP calls are not the system under test. They are a direct-tool baseline for diagnostics, anchor discovery, and fallback legacy checks when no golden reference exists. A run that has only direct MCP output and no OpenClaw/Claude Agent SDK output is invalid.

The config field currently named `oracle` is a legacy schema name. In the MVP it means read-only tool policy and direct-tool baseline metadata; it does not mean the patrol tool is computing a business standard answer.

## Reference Tracks

Each patrol case can carry three answer tracks:

- `actual`: the real agent answer from OpenClaw, Claude Agent SDK, Hermes, or another configured agent runner. This is always the system under test.
- `goldenReference`: an optional structured command output that represents the business standard answer. When a target config includes `goldenReference`, scoring uses it and fails closed if it is missing or empty.
- `directToolBaseline`: optional read-only MCP output such as direct `finance-query`. When a golden reference exists, this baseline is diagnostic only and must not decide pass/fail. When no golden reference exists, it remains the backward-compatible reference source for deterministic checks.

The standalone patrol core only knows how to execute a generic golden command and parse JSON fields such as `answer`, `final_answer`, `finalAnswer`, or MCP-style `result.content[].text`. It does not embed FinanceQA SQL, workbook parsing, or bossa business logic.

## Independent Project Shape

The implementation lives under `agent-patrol/` with its own TypeScript `package.json`, tests, docs, and CLI. It does not import `finance_qa` Go packages or bossa internals. The core has one runtime integration shape:

- A command adapter executes an external agent command.
- The external command returns a common JSON envelope with answer, session, and optional tool-call metadata.
- YAML presets provide target-specific commands, templates, and scoring.
- Example wrappers live under `examples/`; they are not core SDK adapters.

This keeps the巡检工具 portable to other agent systems that expose an agent runner plus simple scoring rules.

## KISS Boundary

- Do not add OpenClaw-specific or Claude-SDK-specific code to `src`.
- Do not put finance/bossa business rules in `src` or generic examples.
- Do not implement write-capable tools.
- Do not treat direct MCP output as a business golden reference.
- Keep live runners opt-in and example-only.

## Safety Rules

- No production IM delivery.
- No boss cron trigger.
- No business write tools.
- Explicit write-tool denylist even when a config allowlist is wrong.
- Isolated patrol session IDs.
- Fail closed when the actual agent envelope is missing.
- Redact tokens and credential-looking strings from reports.

## Scoring Rules

MVP scoring is deterministic string/amount matching, not an LLM judge.

- `mustContain`: every listed term must appear in the answer.
- `mustContainAny`: every listed group must have at least one matching term. Use this for acceptable wording variants such as `["项目口径", "所有项目", "项目应付"]`.
- `mustNotContain`: none of the listed terms may appear.
- `amounts`: each amount must appear in a normalized numeric form.
- `amountLabelGroups`: config-provided aliases for labels and term checks. Put domain wording here instead of hardcoding synonyms in the generic scorer.
- `referenceChecks`: derive amounts, periods, sources, or perspective terms from the active scoring reference. If `goldenReference` is configured, this derives from the golden answer; otherwise it derives from the direct-tool baseline.

## Case Variation

Templates can define `variables` and use `{{name}}` placeholders in `question` or `questions`. The generator expands question wording and variable combinations into a candidate pool, then samples by suite `caseCount` and seed.

Sampling is deterministic for the same seed and rotates across template groups before taking additional variants. This keeps a daily patrol from over-selecting one topic when a template has many date or wording combinations.

FinanceQA uses this to keep `smoke` small while drawing from a larger pool, and `daily` to cover more themes: latest-month revenue, project receivables, invoiced receivables, project payables, invoiced payables, and unpaid project lists.

## MVP Modules

- `src/config.ts`: load YAML/JSON config, expand environment variables, validate runner and read-only policy shape.
- `src/guard.ts`: read-only allowlist and write-tool denylist.
- `src/cases.ts`: deterministic dynamic case generation from templates and anchors.
- `src/runners/command_runner.ts`: generic JSON command adapter for OpenClaw and Claude SDK dry-runs.
- `src/run.ts`: run generated cases through the actual agent runner, optional golden reference, and direct-tool baseline, then score the answers and write reports.
- `src/reference.ts`: read-only MCP baseline calls and generic golden-reference command execution.
- `src/scorer.ts`: hard-rule scoring for actual-vs-expected checks.
- `src/report.ts`: JSONL/Markdown report writer with redaction.
- `src/index.ts`: CLI for `doctor`, `generate`, and `run`.

## Report Artifacts

Each `run` writes a directory of local artifacts:

- `manifest.json`: suite, seed, and generation timestamp.
- `cases.json`: generated questions and scoring rules.
- `raw_results.jsonl`: one raw agent answer per line, including runner type, session id, and case duration.
- `scores.json`: deterministic scoring output.
- `summary.json`: cron-friendly structured summary with aggregate accuracy and failed case details.
- `summary.md`: human-readable summary.

The CLI also prints the aggregate JSON to stdout and exits non-zero when accuracy is below `report.minAccuracy`.

Failed-case evidence separates `Golden Reference` from `Direct Baseline`. If a FinanceQA daily patrol is intended to measure 90% business accuracy, configure a structured `goldenReference`; otherwise the report is only comparing against the direct-tool baseline and should be treated as diagnostic.

## First Acceptance Target

Local acceptance:

```bash
cd agent-patrol
npm install
npm test
npm run typecheck
npm run start -- generate --config config.example.yaml --suite smoke --out tmp/generate
npm run start -- doctor --config config.example.yaml
npm run start -- run --config examples/stub.command-agent.yaml --suite smoke --out tmp/stub-command-agent
```

Business presets are optional YAML examples:

```bash
npm run start -- generate --config presets/financeqa.yaml --suite smoke --out tmp/financeqa-generate
npm run start -- generate --config presets/bossa.yaml --suite smoke --out tmp/bossa-generate
```

Live acceptance is opt-in only. The first live smoke test uses OpenClaw on `ssh clawdbot` and must prove:

- The answer came from `openclaw agent --json`, not a direct MCP call.
- The command did not include `--deliver`.
- Session metadata is present and does not reuse `agent:main:main`.
- The result report is written locally.

```bash
AGENT_PATROL_LIVE=1 \
AGENT_PATROL_OPENCLAW_HOST=clawdbot \
npm run start -- run --config examples/live/clawdbot-openclaw.example.yaml --suite smoke --out tmp/live-clawdbot-openclaw
```

FinanceQA live smoke should run against a non-production OpenClaw agent surface that has the FinanceQA extension installed:

```bash
AGENT_PATROL_LIVE=1 \
AGENT_PATROL_OPENCLAW_HOST=clawdbot \
OPENCLAW_AGENT_CMD='node examples/runners/openclaw_ssh_runner.mjs --host ${AGENT_PATROL_OPENCLAW_HOST} --question-file {questionFile} --session-id {sessionId} --thinking off --timeout 300' \
FINANCEQA_MCP_URL=http://127.0.0.1/stub \
npm run start -- run --config presets/financeqa.yaml --suite smoke --out tmp/live-financeqa-smoke
```

For the default `main` agent, do not pass `--agent main`: that path can map to the protected `agent:main:main` session and defeat explicit patrol session IDs. The runner rejects this when session isolation is required.

## Local Scheduling

Schedule templates live in `examples/schedules/`:

- `run-financeqa-dry-run.sh`: shared low-frequency dry-run entrypoint with a non-overlap lock.
- `financeqa-daily.cron.example`: cron entry for two daily local-report dry-runs.
- `financeqa-daily.service` and `financeqa-daily.timer`: systemd timer equivalent with jitter.
- `financeqa-daily.env.example`: environment variables shared by both examples.

These examples are not installed automatically. They are intended to run on a non-production OpenClaw host, use `examples/runners/openclaw_local_runner.mjs`, run `presets/financeqa.yaml` with `--suite smoke`, write reports under `tmp/financeqa-dry-run/`, and do not pass OpenClaw delivery flags. Run the manual command in `examples/schedules/README.md` before enabling a timer.
