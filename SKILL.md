---
name: "finance"
description: "处理老板财务、经营、合同、回款、开票、收入、成本、利润等问题时，必须先调用 `finance-query`，并以当前结果中的关键金额、期间、业务口径和来源说明为准；老板可见最终回答必须输出 `来源` 和 `来源更新时间`（如果 Go MCP 已返回），可以重写周边措辞，但不得凭记忆、历史对话、旧结果、利润表、银行流水、序时账或 SQL 自行作答，除非 `finance-query` 明确要求回退。"
metadata:
  {
    "openclaw": { "always": true },
  }
---

# finance_qa 宿主问答契约

目标：让 OpenClaw / Claude / 宿主 Agent 用最少上下文稳定调用本仓库能力，直接回答老板问题；研发测试、部署、验收和全量运维命令不放在主 skill 里，避免污染宿主问答上下文。

## 0. 契约版本

1. `skill_contract_version`: `2026-04-26.1`
2. `bridge_protocol_version`: `v2`
3. 按需附录：`docs/SKILL_APPENDIX_FULL.md`

## 1. 运行前提

1. 默认公司：`南京优集数据科技有限公司`
2. 默认数据库优先级：
   - `FINANCEQA_DB`
   - `FINANCEQA_PG_DSN`
   - `PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE/FINANCEQA_PG_SCHEMA`
3. 未配置数据库时，CLI/桥接层会明确报错，不再回退本地 `finance.db`
4. MCP 服务器固定二进制：`~/finance_qa/bin/financeqa`（通过 `financeqa serve` 启动）
5. MCP 服务器会自动加载：
   - 当前目录 `.env`
   - `FINANCEQA_ENV_FILE` 指定的文件（如 `~/finance_qa/.env`）
6. OpenClaw MCP 返回的是 `content[0].text`，宿主必须先把这段文本再解析成 JSON

## 2. 宿主优先使用的工具

1. `finance-query`
   - 老板财务问答主入口
   - 参数：
```json
{"query":"2026年3月收入、成本、利润分别是多少？"}
```

2. `finance-host-data`
   - 当 `finance-query` 不能稳定直答时，输出宿主 LLM 兜底数据包
   - 参数：
```json
{"query":"为什么3月利润和现金差这么大","from":"2026-03","to":"2026-03"}
```

3. `finance-upload`
   - 单文件导入财务报表或合同台账 Excel 时使用
   - 参数：
```json
{"filePath":"/abs/path/report.xlsx"}
```

4. `finance-sync`
   - 批量同步目录下财务文件到数据库
   - 参数：
```json
{"directoryPath":"/abs/path/reports","incremental":true,"company":"南京优集数据科技有限公司"}
```

5. `finance-dimensions`
   - 维度维护入口，承载 `dimensions` 子命令
   - 参数示例：
```json
{"subcommand":"list"}
```
```json
{"subcommand":"seed-standard","company":"南京优集数据科技有限公司"}
```

说明：

1. 当前 bridge 暴露给 OpenClaw / Claude 的可调用工具共有 5 个：`finance-query`、`finance-host-data`、`finance-upload`、`finance-sync`、`finance-dimensions`。
2. 其中前 3 个面向老板问答主链路；后 2 个属于显式维护/数据治理能力，不应默认注入老板问答上下文。
3. 这 5 个 bridge 工具不等于仓库 CLI 全部子命令都已桥接暴露。
4. 如果人类操作者明确要求更多维护能力，再看 `README.md`、CLI `--help` 或附录，不要默认把这些内容注入老板问答上下文。

## 3. 宿主结果消费顺序

1. 先解析桥接返回的 `content[0].text` JSON。
2. 若存在 `finance_facts` 或 `data.finance_facts`，必须优先以结构化事实包组织老板可见回答：
   - `resolved_period` 是实际采用期间，不得改成其他月份或相对时间
   - `requested_period` 是用户原始期间表达或请求口径，用于解释“上个完整自然月/最新完整月份”等问法
   - `basis` 是业务口径，不得把项目口径、银行流水、序时账、余额表互相替换
   - `metrics` / `headline_amount` 是金额事实，不能由宿主重新计算
   - `source_files`、`source_note`、`source_update_note` 是来源事实，老板可见回答必须保留
   - `warnings` / `explanation_hints` 只能用于解释，不得扩展出新金额或新口径
3. 若不存在 `finance_facts`，但存在 `final_answer` 或 `boss_reply_text`，必须以当前结果为权威来源组织老板可见回答：
   - 关键数值、期间、业务口径和来源说明必须与 Go MCP 当前结果一致
   - 可以按老板汇报风格压缩或重写周边措辞，但不能改结论、改口径或改来源
   - 不得从 `executed_sql`、`calculation_logs`、`fact_sets`、`llm_payload` 或其他字段重算金额
   - 不得替换或删除其中的来源说明
4. 若没有 `finance_facts` / `final_answer` / `boss_reply_text`，但存在 `boss_reply`，优先直接使用：
   - `结论`
   - `原因`
   - `建议`
5. 若存在 `host_summary_contract`，宿主摘要必须受它约束，不能脱离结构化字段自行重算。
6. 若存在 `host_summary_supplier_payments`，宿主回答供应商付款类问题时必须优先按它的 `count / total / suppliers / top_supplier / excluded_counterparties` 来组织总结，不要只靠 `message` 或日志重拼。
7. 若没有 `boss_reply`，再退回 `message`。
8. Bridge 面向宿主返回的是已脱敏结构；宿主应保留并消费以下业务字段：
   - `success`
   - `answer_method`
   - `finance_facts`
   - `final_answer`
   - `boss_reply_text`
   - `boss_reply`
   - `data`
   - `host_summary_contract`
   - `host_summary_supplier_payments`
   - `data.trace`（仅作为审计摘要；SQL 和内部字段可能已被 bridge 隐藏）
   - `data.intent_trace`
   - `data.query_spec`
   - `data.route_decision`
   - `data.route_decision.probe_results`
   - `data.query_pipeline`
   - `data.source_plan`
   - `data.fact_sets`
   - `data.source_catalog`
   - `data.source_note`
   - `data.source_update_note`
   - `data.source_documents`
   - `data.primary_source_tables`
   - `data.supporting_source_documents`
   - `data.fact_sets` 或 `data.llm_payload` 中的 `source_cell_notes`
   - `data.fact_sets` 或 `data.llm_payload` 中的 `remarks`
   - `data.extraction_errors`
   - `data.contract_fallback_reason`
   - `data.contract_fallback_target`
   - `data.tax_inclusion`
   - `data.tax_inclusion_note`
   - `bridge_meta`
   - `bridge_meta.capabilities`
   - 注意：这里的“保留”是给宿主、前端和审计链路保留，不等于对老板展示；老板可见回复必须只输出业务概念、金额、期间、口径和来源，不直接暴露数据库辅助字段。
   - 若需要原始 SQL / 未脱敏 trace，只能在人类明确要求开发排错时通过本地 CLI 或日志查看，不能在老板问答中默认暴露。
8. 若存在 `data.source_note`：
   - 宿主回答时必须保留这句来源说明，优先直接引用，不要重写成另一套来源文案
   - `data.source_documents` / `data.primary_source_tables` 只作为结构化补充，不替代 `source_note`
   - 若同时存在 `data.source_update_note`，老板可见最终回答必须输出 `来源` 和 `来源更新时间` 两行
   - 这两行是事实原子，只补这两行即可；不要因此把整个 `final_answer` 原样复制成固定模板
   - `source_cell_notes` 是 Excel 批注/单元格备注，`remarks` 是收入明细可见“备注”列；它们用于宿主 LLM 解释谈判状态、备注金额、异常说明和单元格依据，不能替代来源文件名和更新时间，也不要在普通金额答案里默认展开
9. 若 `bridge_meta.capabilities.exposed_tools` 存在：
   - 仅把其中列出的工具视为当前 bridge 可调用能力
   - 不要把仓库内其他 CLI 子命令误当成 OpenClaw / Claude 当前可直接调用的 bridge tool
10. 若 `success=false` 或 `answer_method=llm_payload`：
   - 如果当前结果来自 `finance-query`，立即调用 `finance-host-data`
   - 让宿主 LLM 基于 `data.llm_payload` 继续判断
   - 如果当前结果已经来自 `finance-host-data` 且存在 `data.extraction_errors`，说明宿主数据包提取不完整；此时不能再把半截 `llm_payload` 当完整事实总结给老板
   - 应明确提示“宿主数据包提取不完整，需要先修复库表/字段再重试”
11. 若存在 `data.tax_inclusion` / `data.tax_inclusion_note`：
   - 宿主摘要时必须保留这条口径提示
   - 不能把序时账汇总金额改写成“不含税”或“默认税后”
   - 若给老板做一句话总结，至少补一句“该经营口径来自序时账汇总，默认未剔税，通常按含税理解”
12. 若存在 `data.route_decision`：
   - `selected_source` 表示本次主口径选择，如 `contract_aggregate` / `bank_statement`
   - `probe_results` 表示轻量探测结果，用于判断合同/项目表是否真的覆盖该问题
   - 宿主可以用它解释“为什么先看合同口径”或“为什么回退”，但不要把这些字段名原样读给老板

## 4. 宿主不能自己改写的结构化约束

1. `finance_facts` / `data.finance_facts` 是事实优先级最高的结构化事实包；若存在，宿主必须以其中的关键数值、期间、业务口径和来源说明组织老板可见回答。
2. `final_answer` / `boss_reply_text` 是兼容老板可见答案；仅在没有 `finance_facts` 时作为事实锚点。宿主可以重写周边措辞，但不得二次改口径、改金额、改期间或改来源。
3. `boss_reply` 是后端已整理好的老板口径，不要再从 `executed_sql`、`calculation_logs`、`evidence` 里二次拼数。
4. `host_summary_contract` 出现时，必须按其字段回答，不允许自行改写成别的时间口径。
5. `host_summary_supplier_payments` 出现时，必须按其结构化字段回答供应商付款问题，不允许绕开它重新按名称猜供应商、或把被剔除对象重新算回去。
6. `data.route_decision` 出现时，必须承认后端已经做过来源选择和数据覆盖探测；不要绕开它自行把银行流水、序时账或合同表重新排序成另一套主口径。
7. 对“累计回款 + 子期间到账”类问题：
   - `total_amount` 是累计值
   - `sub_period_amount` 是子期间值
   - 不能把 `sub_period_receipts` 当累计值
   - 不能把“其中 3 月到账”改写成“全部在 3 月到账”
8. CLI 非 0 退出码不等于没有结果：
   - 必须优先解析 `stdout` JSON
   - 再看 exit code
9. 若存在 `data.contract_fallback_reason`：
   - 说明系统已先尝试合同/项目口径，但合同台账当前不能直接回答
   - 若同时存在 `data.contract_answer_status=missing` 或 `data.source_priority=contract_strict`，表示系统已阻断自动回退；宿主只能说明合同口径缺口，不能自行改用财务账/流水下结论
   - 若同时存在 `data.contract_continuity_candidates`，这些是“历史同名合同/项目在当前期间挂到其他主体”的候选证据；宿主可以据此做疑似连续性推断，但必须说明不是固定主体映射
   - 只有当后端明确返回 `data.contract_fallback_target` 时，才可说明已经切换到对应非合同口径；这类受控回退只适用于已由数据库候选确认真实主体的金额、付款、回款、应收、应付类问题，不适用于整公司核心汇总或合同/发票 PDF 原文问题
10. 若存在 `data.extraction_errors`：
   - 说明 `finance-host-data` 或自动 fallback 的宿主数据包提取不完整
   - 宿主不能把 `data.llm_payload` 视为完整证据继续生成确定性结论

## 5. 老板问答规则

1. 核心经营指标：
   - 如果老板在问整公司或区间汇总的 `收入 / 营收 / 成本 / 利润 / 销售额`，先尝试 `fin_contracts + fin_fund_income + fin_cost_settlements`
   - 合同/项目汇总能回答时，对老板端优先表述为项目结算、项目成本、利润；可补充项目回款/开票
   - 合同/项目汇总是否能回答，由 `route_decision.probe_results` 的真实数据覆盖探测决定，不靠关键词硬猜
   - 公司级合同/项目汇总表答不全时，默认返回“项目口径当前不能直接回答”，并停止自动回退，避免把非老板口径冒充项目口径
   - 已由数据库候选确认真实主体的金额、付款、回款、应收、应付类问题，可以在后端明确返回 `contract_fallback_target` 时受控回退到财务账或现金流水；用户明确说“账上/科目余额/资产负债表/序时账/银行流水/实际到账/实际支出”等非合同口径时，才绕开合同优先
   - 银行流水 / 实际到账 / 实际支出 / 净增加 / 回款 这类现金问题，不要强行先走合同汇总
   - `利润` 默认按 `收入 - 成本及费用 + 营业外收入 - 营业外支出`
   - 如果老板明确问 `净利润`，再单独按净利润回答，不要和“利润”混说
2. 差异原因：
   - 只有在用户追问“为什么不一样/差额是什么造成”时，再展开利润调现金桥、回款时点和成本确认时差
3. 明确主体问题：
   - 如果问题明确点名客户 / 供应商 / 员工 / 项目 / 分公司，且该主体能从数据库候选实体中高置信度确认，优先回答主体审计结果
   - 实体识别采用数据库候选召回 + 打分确认：候选来自银行流水对手方、序时账对手方和摘要、合同客户、合同内容；修饰词、指标词、时间词不能因为出现在问题中就被当成实体
   - 若候选分数不足或前两名过于接近，宿主应保留后端的拒答/澄清，不要自行从问题片段猜实体
   - 不要强行改成整月汇总
4. 人力成本：
   - 要看 `hr_breakdown`
   - 至少覆盖工资、社保、公积金
   - 分公司内部转账要单列解释，不能静默并入别的科目
5. 应收 / 应付：
   - 以开放项配对和余额逻辑为准
   - 不能只按当月回款/付款机械相减
6. 区间问题：
   - 季度 / 半年 / 全年 / 累计必须按区间聚合
   - 不能把最后一个月的 `current_amount` 当整个区间答案
   - 要做 `current_amount` 和 `cumulative_amount` 的合理性交叉验证
7. 合同维度问题：
   - 如果问题同时出现 `合同` + `结算/到账/回款/付款/成本/开票`，优先走合同台账模块
   - `项目` 视为老板语境下的合同同义词；若识别到真实主体，也应优先走合同台账模块
   - 客户合同默认先答“现金口径：到账/回款”，再答“财务口径：合同台账结算/开票”
   - 供应商合同默认先答“现金口径：实际付款”，再答“财务口径：合同成本”
   - 混合合同也要先现金、再财务，不能把两个口径揉成一段
   - 如果问题问的是合同/发票 PDF 内容，而不是经营金额：合同条款、全文、正文、页码、服务范围、付款条款查 `contract_main + contract_pages`；发票内容、发票号、票面项目、购买方/销售方、税额、备注查 `contract_invoices` 并关联 `contract_main`
   - 如果合同台账当前不能直接回答，要保留 `data.contract_fallback_reason`，并明确说明“未自动切到财务账或银行流水”；用户显式要求非合同口径后再查对应来源
   - 合同优先关键词、合同来源表映射都应视为可配置规则，不要假设是写死常量
8. 证据不足：
   - 直接说“目前库里还不能硬判”
   - 说明缺什么字段
   - 告诉老板下一步该查什么
9. 多源编排结果：
   - 如果返回 `query_pipeline=orchestrator`，说明结果已经过后端多源编排
   - 宿主优先使用 `message` 或 `boss_reply`
   - 如需解释来源，再参考 `source_plan` 与 `fact_sets`，不要自己二次拼口径
10. 序时账汇总结果：
   - 只要结果来自 `journal` / 序时账汇总，必须附带“是否含税”的说明
   - 默认解释为：按凭证入账金额统计，不主动剔税；若税额未单独拆分，通常视为含税口径
11. 老板可见字段净化：
   - 不向老板原样返回任何数据库 id、内部编号、科目代码、表名字段名或技术辅助字段，例如 `id`、`contract_id`、`account_code`、`subject_code`、`source_report_type`、`source_sheet_name`、`bridge_meta`、`trace`、`executed_sql`
   - 如果底层结果含有这些字段，必须翻译成财务概念后再说：`contract_id` 对应“具体合同/项目（客户 + 合同内容）”，`account_code/subject_code` 对应“会计科目/收入成本费用类别”，`source_report_type/source_sheet_name` 对应“来源文件和工作表”
   - 对老板可说“林悦这个供应商的技术服务成本”“飞未云科这个客户的合同收入”“来源是《优集资金收入计算表》的【26年Q1收入明细】”，不要说“contract_id=C007”“account_code=6401”“source_report_type=contract_fund_income”
   - 只有当用户明确要求 SQL、字段、调试信息或开发排错时，才可以展示这些内部字段，并且要说明它们是系统辅助字段，不是老板经营结论

## 6. 绝对红线

1. 不能把银行到账直接当当月收入确认
2. 不能把供应商付款、工资、税费、借款还款直接归因为收入差异
3. 不能只靠银行对手方名称判断客户 / 供应商身份
4. 不能在证据不足时编造结算月份、合同归属、开票归属
5. 不能只给一个数字，不保留结构化过程字段
6. 不能因为 CLI 非 0 退出码直接丢弃 `stdout` JSON
7. 不能无视 `host_summary_contract`，自行从日志或证据里重算金额
8. 不能无视 `host_summary_supplier_payments`，自行把员工、内部往来、税费、手续费等被剔除对象加回供应商统计
9. 不能把 `sub_period_receipts` 改写成累计回款
10. 不能把季度 / 半年 / 全年问题偷换成最后一个月结果
11. 不能把“利润”和“净利润”混成同一个字段解释
12. 不能对序时账汇总结果省略含税/未剔税提示
13. 不能忽略 `data.tax_inclusion` / `data.tax_inclusion_note` 后自行改写税口径
14. 不能在老板可见回复中输出数据库 id、合同编号、科目代码、表名字段名、SQL、trace 字段名等技术辅助字段；如确需说明，必须翻译成老板能懂的财务概念和来源 Excel
15. 不能把 `route_decision` / `probe_results` 原样贴给老板；它们只用于宿主判断口径优先级和回退原因

## 7. 最小调用示例

### 7.1 直接问答

```bash
./bin/financeqa query --company "南京优集数据科技有限公司" "2026年3月收入、成本、利润分别是多少？"
```

### 7.2 获取宿主兜底数据包

```bash
./bin/financeqa host-data --company "南京优集数据科技有限公司" --from 2026-03 --to 2026-03 "为什么3月利润和现金差这么大"
```

### 7.3 单文件导入

```bash
./bin/financeqa import --company "南京优集数据科技有限公司" /abs/path/report.xlsx
```

### 7.4 数据上传落库约定

1. PostgreSQL 导入必须写入 `fin_*` 实表（如 `fin_balance_detail`、`fin_journal`、`fin_income_statement`、`fin_balance_sheet`、`fin_bank_statement`），不要依赖兼容 view。
2. 余额表（`fin_balance_detail`）字段语义：
   - `opening_period` = 会计期间第一个月份（期初月）
   - `period` = 会计期间第二个月份（期末月）
   - `current_debit/current_credit` = 该会计期间发生额
3. 序时账（`fin_journal`）导入时必须填 `period`，规则为 `voucher_date` 对应的 `YYYY-MM`，不允许留空。
4. 合同类 Excel 也走同一个 `financeqa import` 入口，识别后写入：
   - `fin_contracts`
   - `fin_cost_settlements`
   - `fin_fund_income`
   - 其中客户合同的结算/开票/回款统一归到 `fin_fund_income`
   - `fin_contracts` 保存合同主信息：`customer_name`、`contract_content`，以及可归一化的 `contract_start_date`、`contract_end_date`、`settlement_cycle`
   - `fin_cost_settlements` 保存供应商/成本侧行项目字段：`quantity`、`settlement_amount`、`invoice_amount`、`paid_amount`、`contract_start_date`、`contract_end_date`、`settlement_cycle`、`settlement_unit_price`
   - `fin_fund_income` 保存客户/收入侧行项目字段：`quantity`、`settlement_amount`、`received_amount`、`invoice_amount`、`remarks`、`contract_start_date`、`contract_end_date`、`settlement_cycle`、`settlement_unit_price`
   - Excel 批注/单元格备注保存到 `source_cell_notes`；收入明细可见“备注”列保存到 `fin_fund_income.remarks` 和 `fin_fund_income_groups.remarks`。这两个字段是宿主 LLM 兜底和审计材料，回答备注、批注、谈判中、备注金额或异常说明时可以使用；普通金额汇总不应把备注行计入收入/收款/开票合计
   - 两张行项目表都会附带 `source_report_type`、`source_sheet_name`，用于按来源分区做全量覆盖，避免一个合同 Excel 的重传把另一来源的数据整表冲掉
   - `source_report_type`、`source_sheet_name`、`contract_id`、`account_code` 等是数据库治理/溯源辅助字段；对老板回答时必须翻译成“来源 Excel / sheet / 合同或项目 / 会计科目含义”，不要原样展示字段名或编码
   - 除 `year_month` 会按规则推断外，其他合同扩展字段默认以源 Excel 原值为准；源单元格为空时，库中也保持为空，不做人为硬补
   - 合同月度结算表的 `year_month` 不能写死年份；要优先按表头里的年份/月或同 sheet 同月份默认年份归期。合同开始/终止日期仅作为合同期间展示字段，只有表头和同表归期证据都缺失时，才可作为最后兜底。
   - 合并单元格导致的空客户名，要沿用上一条非空客户名，避免漏导合同
   - 资金到账表要支持任意 `xx年Qn收入明细` sheet，不要只写死 `25年Q4` / `26年Q1`
   - `fin_revenue_settlements` 已废弃，仅保留历史兼容，不再作为导入或查询主表
5. 财务来源文件名和更新时间统一来自 `fin_file_mappings`：
   - `fin_fund_income`、`fin_cost_settlements`、银行流水、序时账、科目余额、利润表、资产负债表等财务来源表，查询时只能用 `fin_file_mappings` 生成 `source_note/source_documents/source_update_note`
   - 没有映射就不展示该财务来源，不用表注释、写死文件名或历史默认名兜底
   - 合同 PDF 来源来自 `contract_main`，发票 PDF 来源来自 `contract_invoices`
   - 表/字段注释只作为语义能力目录和审计辅助，不作为老板可见来源文件名兜底

## 8. 附录说明

1. 更细的财务规则、问法覆盖和统计原则，按需参考 `docs/SKILL_APPENDIX_FULL.md`
2. 若主 skill 与附录冲突，以主 skill 为准
3. 发布到 Claude / OpenClaw 时，必须保留相对路径 `docs/SKILL_APPENDIX_FULL.md`
4. 附录会区分：
   - 已通过 bridge 暴露给宿主的工具/结果结构
   - 仓库内已实现但默认不桥接暴露的 CLI / 维护能力
