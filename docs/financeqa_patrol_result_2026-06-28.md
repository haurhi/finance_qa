# FinanceQA 巡检全量重跑结果报告（2026-06-28）

## 1. 结论摘要

本次在 `ssh clawdbot` 上重跑了 2026-06-28 当天所有 FinanceQA 巡检问题，不是只跑上一轮 3 条。重跑范围来自当天 20 个历史巡检报告目录，按 `template + question` 去重后共 41 条。

严格评分结果为 `0/41 = 0.00%`，未达到日常巡检目标 `>= 90%`。需要注意，这个 0% 是严格口径：所有回答都缺少来源证据，导致 41 条全部失败；其中 21 条还存在金额、期间或口径等核心准确性问题，另外 20 条主要是来源证据或格式问题。

本报告的目的不是调整巡检打分放水，而是给后续修 FinanceQA 代码的 agent 提供可复现证据和优先排查方向。

## 2. 本次运行信息

| 项目 | 内容 |
| --- | --- |
| 测试环境 | `ssh clawdbot` |
| 生产环境影响 | 未使用 `ssh lzh`，未影响生产环境 |
| 报告目录 | `/opt/finance_qa/agent-patrol/tmp/financeqa-dry-run/baseline-today-allq-20260628T154737` |
| 来源问题集 | `/opt/finance_qa/agent-patrol/tmp/financeqa-baseline-today-all-questions.sources.json` |
| 配置文件 | `/opt/finance_qa/agent-patrol/tmp/financeqa-baseline-today-all-questions.yaml` |
| 历史报告目录数 | 20 |
| 去重后问题数 | 41 |
| 运行耗时 | 3,503,012 ms，约 58 分钟 |
| 运行状态 | 报告生成成功，业务阈值失败 |

本次重跑后已检查残留进程，没有发现 `baseline_today_all_questions`、`baseline-today-allq`、`openclaw_local_runner` 或 `tsx src/index` 相关残留进程。

## 3. 问题覆盖范围

| 模板 | 问题数 | 说明 |
| --- | ---: | --- |
| `finance_project_receivable_unpaid` | 10 | 项目应收未收 |
| `finance_project_payable_unpaid` | 8 | 项目应付未付 |
| `finance_unpaid_projects` | 6 | 未付款项目及金额 |
| `finance_project_invoiced_receivable_unpaid` | 6 | 已开票未回款/未收款 |
| `finance_latest_month_revenue` | 6 | 收入表最新月份营收 |
| `finance_project_invoiced_payable_unpaid` | 5 | 已收票未付款 |

## 4. 金标口径

金标来自 FinanceQA snapshot reference，而不是直接拿 agent 回答当标准答案。快照事实源：

- 来源文件：`优集收入、成本计算表 - 上传.xlsx`
- 来源版本：`优集收入、成本计算表 - 上传.xlsx:5c3690919021`
- 来源更新时间：`2026-06-26T20:29:51.639076`
- 快照生成时间：`2026-06-26T21:54:44+08`
- 快照数据库：`bossagent_app`
- 快照 schema：`tenant_uhub_etl_shadow`

核心金标如下：

| 问题类型 | 金标指标 | 金标期间 | 金标金额 | 补充口径 |
| --- | --- | --- | ---: | --- |
| 项目应收未收 | 项目应收 | `2025-10~2026-05` | 2,065,398.17 | 项目结算 26,346,051.47；已到账 24,280,653.30；已开票 16,121,179.98 |
| 已开票未回款 | 已开票未回款 | `2025-10~2026-05` | 16,973.15 | 项目结算 26,346,051.47；已到账 24,280,653.30；已开票 16,121,179.98 |
| 项目应付未付 | 项目应付 | `2025-10~2026-05` | 1,946,918.51 | 项目成本 11,949,440.63；已付款 10,002,522.12；已收票 12,562,385.58 |
| 已收票未付款 | 已收票未付款 | `2025-10~2026-05` | 2,559,863.46 | 项目成本 11,949,440.63；已付款 10,002,522.12；已收票 12,562,385.58 |
| 未付款项目明细 | 已收票未付款 | `2025-10~2026-05` | 2,559,863.46 | 前 5 项见下表 |
| 最新月份营收 | 项目结算 | `2026-06` | 912,713.97 | 已到账 387,513.97；已开票 837,613.97 |

未付款项目明细金标前 5 项：

| 项目 | 金额 |
| --- | ---: |
| 南京林悦智能科技有限公司 / 行业商品数据采购合同 | 1,512,127.10 |
| 北京欧特欧国际咨询有限公司 / 商指针产品服务协议-天猫 | 500,000.00 |
| 北京欧特欧国际咨询有限公司 / 产品服务协议-京东 | 391,666.67 |
| 南京聪明狗网络技术有限公司 / 电商价格和库存监测服务 | 85,773.85 |
| 南京众信数通智能科技有限公司 / 推广数据合同-京东 | 33,057.54 |

## 5. 评分结果

总体：

| 指标 | 结果 |
| --- | ---: |
| 总问题数 | 41 |
| 通过数 | 0 |
| 失败数 | 41 |
| 严格准确率 | 0.00% |
| 阈值 | 90% |
| 是否达标 | 否 |

失败类型计数：

| Failure type | 数量 | 说明 |
| --- | ---: | --- |
| `missing_source` | 41 | 回答缺少 `优集收入、成本计算表 - 上传.xlsx` 来源证据 |
| `agent_changed_amount` | 20 | agent 最终答案未包含金标金额，或金额被改写成其他口径 |
| `period_mismatch` | 20 | 期间未覆盖金标期间，例如只回答 `2025-10`、`2026-01~2026-05` 或 `2026-06` |
| `scorer_term_miss` | 5 | 回答缺少必须的口径词，例如项目口径、项目应收、项目成本等 |

按模板拆分：

| 模板 | 总数 | 通过 | 核心准确性问题 | 仅来源/格式问题 |
| --- | ---: | ---: | ---: | ---: |
| `finance_project_invoiced_receivable_unpaid` | 6 | 0 | 3 | 3 |
| `finance_project_payable_unpaid` | 8 | 0 | 3 | 5 |
| `finance_project_invoiced_payable_unpaid` | 5 | 0 | 1 | 4 |
| `finance_unpaid_projects` | 6 | 0 | 6 | 0 |
| `finance_latest_month_revenue` | 6 | 0 | 1 | 5 |
| `finance_project_receivable_unpaid` | 10 | 0 | 7 | 3 |

## 6. 端到端证据链

本次不是只测 direct finance-query baseline。巡检 runner 是 `openclaw_agent_cli`，即从 OpenClaw agent 入口执行。

证据情况：

- 41 条都有 `openclaw_agent_cli` 运行证据。
- 39 条记录到 `finance-query` tool call。
- 2 条没有记录到 tool call，需要单独看作 OpenClaw/巡检证据链缺口：
  - `从2025年10月起到上一个完整自然月月底，所有项目已开票未回款是多少？`
  - `老板，从收入表看最新一个月的营收是多少？`

因此，本轮可以作为 OpenClaw + FinanceQA MCP 路径的业务基准，但后续巡检工具仍应增强 tool call 证据提取，确保每条财务问题都能明确证明是否调用了 `finance-query`。

## 7. 主要问题判断

### 7.1 来源证据全量缺失

41 条回答全部缺少来源文件名 `优集收入、成本计算表 - 上传.xlsx`。这不是单个问题，而是最终回答层没有稳定保留 FinanceQA 来源信息。

需要重点检查：

- FinanceQA 查询结果里 `source_note`、`source_summary`、`source_update_note` 是否生成。
- MCP bridge / OpenClaw plugin 的 `final_answer` 或 `boss_reply` 是否丢掉了来源字段。
- contract aggregate 路径是否经过统一的来源渲染逻辑。

本地相关代码入口：

- `internal/query/source_attribution_render.go`
- `internal/mcp/bridge_reply.go`
- `internal/query/contract_aggregate_message.go`
- `internal/query/contract_aggregate_payload.go`

注意：不要通过放宽巡检 scorer 来掩盖这个问题。老板侧财务回答必须包含来源和来源更新时间，否则无法审计。

### 7.2 期间解析不稳定

典型失败：

- 问：`从2025年10月起至今，已开票但还没回款的项目金额是多少？`
- 金标：`2025-10~2026-05`，已开票未回款 `16,973.15`
- 实际：只回答 `2025-10`，已开票未回款 `0.00`

另一类失败：

- 问：`老板，从2025年10月到上个完整自然月底，按项目口径已开票未收款还有多少？`
- 金标：`2025-10~2026-05`
- 实际：解释为 `2025-10`，并提示系统覆盖到 2025-10

还出现过把 `25年至26年` / `未付款项目` 类问题解析到 `2026-06`，导致 FinanceQA 认为没有匹配项目记录。

需要统一的规则：

- 巡检基准日为 `2026-06-28`。
- `上一个完整自然月月底`、`上个月底`、`上个完整自然月底` 应解析到 `2026-05`。
- 对于定时巡检或老板 cron 问法，`从2025年10月起至今` 当前也应按已完整沉淀月份处理，即 `2025-10~2026-05`，不要落到 `2025-10` 单月或 `2026-06` 单月。

本地相关代码入口：

- `internal/query/period/period_parser.go`
- `internal/query/contract_aggregate_period.go`
- `internal/query/query_rewrite_test.go`
- `internal/query/period/period_parser_test.go`

### 7.3 项目口径和发票口径混淆

用户原则是：当问题含项目、合同、客户、供应商、业务汇总等语义时，优先项目口径；之后才考虑财务、流水、序时账等口径。

当前需要稳定区分：

| 问法 | 应优先指标 |
| --- | --- |
| `应收未收`、`未回款`、`还有多少应收` | 项目应收 |
| `已开票未回款`、`已开票未收款` | 已开票未回款 |
| `应付未付`、`未付款`、`项目成本口径未付款` | 项目应付，除非明确提到收票/发票 |
| `已收票未付款`、`已收票但未付款`、`供应商发票未付款` | 已收票未付款 |
| `收入表最新月份营收` | 最新月份项目结算 |

典型失败：

- `按项目成本口径，从2025年10月起到上一个完整自然月月底未付款合计多少？`
- 金标按项目应付：`1,946,918.51`
- 实际有时偏向 `已收票未付款 2,559,863.46`，或者拒答。

本地相关代码入口：

- `internal/query/metric_detection_helpers.go`
- `internal/query/query_policy_router.go`
- `internal/query/contract_aggregate_selection.go`
- `internal/query/contract_aggregate_collect.go`
- `internal/query/contract_aggregate_payload.go`

### 7.4 未付款项目明细路由错误

`25年至26年未付款的项目及对应金额有哪些？` 这类问题 6/6 都有核心准确性问题。

金标是成本侧/供应商侧项目明细，金额为已收票未付款 `2,559,863.46`，并给出项目列表。

实际回答示例：

> 目前合同收入/成本结算表在 2026-06 期间没有匹配到可判断成本侧项目口径的记录，无法按项目口径给出答案。

这说明至少有两个问题叠加：

- 期间解析落到了错误月份。
- `未付款项目及金额` 的主题路由没有稳定进入成本/应付/已收票未付款明细路径。

这类问题应单独补回归测试，因为它最接近老板真实问法。

### 7.5 OpenClaw 层仍有 2 条没有 tool call 证据

这不是 FinanceQA 本体的第一优先级问题，但需要记录：

- 41 条中 39 条有 `finance-query` tool call。
- 2 条没有 tool call 记录，但仍产生了财务式回答。

后续如果要把巡检作为生产告警依据，需要保证证据里能明确区分：

- OpenClaw 没有调用工具却直接回答。
- OpenClaw 调用了工具，但插件层没有记录到。
- 工具调用成功，但 FinanceQA 结果错误。

## 8. 建议修复优先级

### P0：不要硬编码测试题或金额

严禁为这 41 条问题写特殊 case，也不要把金标金额写死进业务代码。修复必须落在通用的时间解析、指标识别、项目口径优先级、来源渲染链路上。

### P1：先修期间解析

建议补测试覆盖以下问法：

- `从2025年10月起到上一个完整自然月月底`
- `从2025年10月到上个月底`
- `从2025年10月起至今`
- `2025年10月至今`
- `25年至26年`
- `最新月份`、`收入表中最新月份`

预期：

- 基准日 `2026-06-28` 下，前四类应落到 `2025-10~2026-05`。
- `最新月份营收` 应落到收入表最新可见月份 `2026-06`。
- `25年至26年` 在该巡检上下文中需要结合数据和业务口径，避免退化为单月 `2026-06`。

### P2：修项目口径指标识别

建议把指标识别规则写成明确优先级：

1. 明确发票词：`已开票未回款`、`已收票未付款` 优先走发票口径。
2. 明确项目/合同/客户/供应商/业务汇总：优先 contract/project aggregate。
3. `未付款` 默认不能直接等同于 `已收票未付款`；只有出现 `已收票`、`发票`、`收到发票` 等词才走已收票未付款。
4. `项目成本口径未付款` 更接近项目应付，除非用户明确说 `已收票未付款`。

### P3：修来源输出链路

最终给 OpenClaw 的 `final_answer` / `boss_reply` 必须包含：

- `来源：《优集收入、成本计算表 - 上传.xlsx》`
- `来源更新时间：2026-06-26T20:29:51.639076` 或格式化后的等价时间

需要检查 contract aggregate 的最终消息是否绕过 `annotateSourceAttribution`，以及 MCP bridge 是否把带来源的字段替换成了不带来源的 `boss_reply`。

### P4：补业务回归测试

建议至少补以下测试层级：

- `internal/query/period/period_parser_test.go`：只测日期解析。
- `internal/query/contract_arap_priority_test.go` 或新增相邻测试：测项目应收/应付、已开票未回款、已收票未付款优先级。
- `tests/business/contract_first_accuracy_test.go`：用真实库或测试库验证最终 answer 包含金额、期间、来源。
- OpenClaw 插件/bridge 层测试：验证 `final_answer` 不丢来源。

## 9. 建议验收标准

修复后不要只跑单元测试，至少需要以下四层验证：

1. 本地 Go 测试：
   - `go test ./internal/query/... ./internal/mcp/...`
2. FinanceQA 业务测试：
   - `make test-query-heavy`
   - `make test-business`
3. clawdbot 上跑同一套 41 条基准问题：
   - 目标严格准确率 `>= 90%`
   - 不能再出现全量 `missing_source`
4. 抽样看 failed case evidence：
   - 每条财务问题应有 `finance-query` tool call 证据。
   - actual answer 应包含金标金额、期间、口径、来源。

## 10. 可复现命令

在 `ssh clawdbot` 上查看本次报告：

```bash
cd /opt/finance_qa/agent-patrol
jq '{aggregate, failureCategoryCounts}' tmp/financeqa-dry-run/baseline-today-allq-20260628T154737/summary.json
find tmp/financeqa-dry-run/baseline-today-allq-20260628T154737 -maxdepth 2 -type f | sort
```

查看问题来源范围：

```bash
cd /opt/finance_qa/agent-patrol
jq '{date, reportDirCount:(.reportDirs|length), uniqueCaseCount:(.uniqueCases|length)}' tmp/financeqa-baseline-today-all-questions.sources.json
jq -r '.uniqueCases[].template' tmp/financeqa-baseline-today-all-questions.sources.json | sort | uniq -c | sort -nr
```

查看失败类型：

```bash
cd /opt/finance_qa/agent-patrol
jq -r '.[] | select(.pass==false) | .failureDetails[].type' \
  tmp/financeqa-dry-run/baseline-today-allq-20260628T154737/scores.json \
  | sort | uniq -c | sort -nr
```

查看 OpenClaw / finance-query 证据：

```bash
cd /opt/finance_qa/agent-patrol
node <<'NODE'
const fs = require("fs");
const lines = fs.readFileSync("tmp/financeqa-dry-run/baseline-today-allq-20260628T154737/case_evidence.jsonl", "utf8")
  .trim().split(/\n/).filter(Boolean).map(JSON.parse);
let toolCalls = 0;
for (const e of lines) {
  if ((e.actual?.toolCalls || []).some(t => t.name === "finance-query")) toolCalls++;
}
console.log({ total: lines.length, financeQueryToolCalls: toolCalls });
NODE
```

## 11. 给修复 agent 的注意事项

- 不要修改巡检 expected 来迎合当前错误输出。
- 不要用硬编码处理这 41 条问题。
- 不要把 `项目应付` 和 `已收票未付款` 混成同一指标。
- 不要把 `至今`、`上个月底`、`上一个完整自然月月底` 分散在多个解析器里各自处理成不同结果。
- 先修 FinanceQA 返回的结构化结果和最终 answer，再看 OpenClaw 是否改写或省略。
- 修完后必须用同源数据在 clawdbot 上重跑这 41 条，作为第一轮验收。

## 12. 2026-07-03 FinanceQA 本体修复补充

本轮修复只改 FinanceQA 查询本体，不修改巡检 scorer，也不让 OpenClaw bridge 直出 `final_answer`。修复原则是把巡检暴露的问题收敛为通用口径规则、统一来源归因和回归测试，不按生产金额、供应商、客户、合同名或某条问题文本做硬编码。

### 12.1 来源归因使用实际执行期间

问题现象：

- `银行卡上，最新完整月份净现金流是多少？` 金额和期间正确，但来源显示 `未记录`。
- 合同/客户/供应商维度题金额正确，但来源或来源更新时间缺失。

修复点：

- 来源归因在查 `fin_file_mappings` 前，优先从结果结构里的 `period_from`、`period_to`、`period` 反推实际执行期间。
- 当问题问“最新完整月份”但执行期回退到最新有数据月份时，来源映射也随执行期回退，而不是继续用原始请求期。

对应代码：

- `internal/query/source_attribution_render.go`
- `tests/unit/query/source_attribution_test.go`

### 12.2 显式序时账口径必须归因到序时账

问题现象：

- `按序时账口径，最新完整月份账上净利润是多少？` 金额和回答风格正确，但 `source_constraint=journal` 时仍归因到 `fin_income_statement`，导致 `来源：未记录` 或来源口径不一致。

修复点：

- 明确出现 `序时账口径`、`序时帐口径`、`凭证口径` 的核心指标问题，使用 journal-only 月度账务汇总。
- 普通 `账上利润/账上收入` 仍保留原有利润表优先和现金/账务对照能力；只有显式指定序时账/凭证时才强制序时账来源。
- 原有“利润表缺失时自动回退序时账”的路径仍保留 `journal_fallback` 标识，避免改变既有月度 fallback 语义。

对应代码：

- `internal/query/query_execution_stage_policy.go`
- `internal/query/core_metrics_accrual_query.go`
- `internal/query/reconciliation_book.go`
- `internal/query/hardening_core_metric_test.go`

### 12.3 合同维度应付题过滤收入 sheet 噪声

问题现象：

- 南京众信等混合主体在问“项目应付未付/未付款”时，金额正确，但来源中同时出现收入明细和成本月度结算，老板可见来源略噪。

修复点：

- 合同维度来源表选择由单纯 `role` 改为 `role + asked_topic`。
- `payable`、`invoice`、`cost`、`payments` 只保留成本结算/付款相关来源；`revenue`、`receipts` 只保留收入相关来源；`profit` 保留收入和成本。
- 表名仍来自规则配置并保留 `tenant_uhub.*` 前缀，只按 base table 做通用过滤，避免破坏结构化契约。

对应代码：

- `internal/query/source_attribution_collector.go`
- `internal/query/contract_dimension_collect.go`
- `internal/query/contract_arap_priority_test.go`

### 12.4 验证命令

本轮本地验证已覆盖：

```bash
go test ./internal/query -run 'TestExplicitJournalNetProfitUsesSingleBookPerspective|TestSupplierProjectPayableUsesLatestProjectPaidAmount' -count=1
go test ./internal/query ./tests/unit/query ./tests/integration -count=1
make test-full
make test-business
go build ./cmd/financeqa/...
git diff --check
```
