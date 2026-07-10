# FinanceQA 2.2.57 巡检稳定性修复设计

## 背景与目标

FinanceQA 2.2.57 在 `lzh` 部署后的 17 轮巡检中累计通过 220/238，runner invalid 为 0，但随机问法仍反复暴露五类业务失败：余额表 AR/AP、未付款项目、账银对账、账上净利润和最新项目营收。问题集中在三条边界：OpenClaw 没有始终把当前用户原问题传入 `raw_user_query`，MCP 对原问题与模型改写的合并允许未落地的伪实体改变路由，以及多指标结果没有形成完整的 `finance_facts.required_atoms`。

本次目标是用最小、可复用的规则修复这些边界，使等价问法得到相同的期间、口径、金额和来源，同时保留 OpenClaw 的自然语言表达自由。不修改巡检 scorer、不硬编码具体 case、公司、金额或月份，也不恢复整段复制 `final_answer`。

## 约束与成功标准

- 保持 KISS：每条保护规则只放在一个权威边界，不在多个 router 重复打补丁。
- 原问题负责保护用户明确给出的期间、来源口径和公司级/实体级范围；模型改写仍可补充真实且可落地的主体信息。
- `finance_facts` 是跨 FinanceQA/OpenClaw 的唯一事实契约；OpenClaw 只补齐事实原子，不复制整段工具答案。
- 普通“对比/比较账上净利润和银行净流入”也必须返回银行净流入、账上净利润和名义差额三项，并明确“名义差额只作对账入口，两个口径不同”。
- 修复后原有客户、供应商、合同、项目实体问法和单指标问法不得改变路由。
- 验收必须包含红绿 TDD、完整本地测试、生产只读 replay 和完整巡检；不能用 direct query 通过替代 OpenClaw 真链路通过。

## 方案比较

### 方案 A：修复语义边界与结构化事实契约（采用）

在 OpenClaw execute hook 保证 `raw_user_query` 来自当前 run 的原始用户问题；在 MCP 只保护明确的来源/期间/公司级 roster 语义；在 FinanceQA producer 生成完整的多指标 facts。优点是修根因、改动集中、能覆盖随机改写；代价是需要同时补 Go 与插件回归测试。

### 方案 B：只在 OpenClaw 输出层补金额

可快速提高可见答案命中率，但无法修复错误路由、错误期间和错误工具结果，且会让 bridge 逐渐复制业务逻辑。拒绝。

### 方案 C：所有 finance-query 永远使用 raw_user_query

能避免大部分改写污染，但会丢失模型对简称、合同名和多轮上下文的有效补全，违反既定“保护性优先”边界。拒绝。

## 设计

### 1. 当前用户原问题的 run 级绑定

`plugin/openclaw-finance/dist/index.esm.js` 的 `finance-query` execute hook 应把当前 run/session 已解析出的最新财务用户原问题写入 `raw_user_query`，而不是只在模型未提供该字段时补充。模型生成的 `query` 保持不变，用于实体补全和简化表达。

该状态必须按 run 隔离并在工具调用后按现有生命周期清理，避免复用 session key 时串入上一题。execute 计算 scope 时合并 tool factory context 与 runtime context，不能因为 runtime context 只带部分字段就丢掉 factory context 中的 `runId/sessionKey`。现有 session/run guard 继续复用，不新增第二套状态表；scope 无法唯一对应当前问题时 fail closed，不借用其他 active run 的问题。

插件侧不再同时改写 `query`。它只负责准确传递“模型 query + 当前用户 raw_user_query”，所有语义取舍集中到 Go MCP，删除 JS/Go 两套近似合并规则造成的漂移。

结果数据流为：

`当前用户原问题 -> raw_user_query`，`模型改写 -> query`，随后由 Go MCP 决定两者如何合并。

### 2. MCP 的保护性语义合并

`internal/mcp/service.go` 的 `effectiveFinanceQuery` 是唯一的语义合并位置。它保留现有动态期间、明确来源和具体主体保护，并增加两种通用保护：

- 公司级项目 roster：原问题表达“哪些/都有哪些/列出/明细/各”等复数范围，同时包含项目和应收/应付/未付款等指标时，原问题决定公司级 aggregate 语义。改写只能补充真实主体，不能把结构词变成 entity。
- 明确业务表口径：`收入表` 与现有 `账上/余额表/序时账/银行流水` 一样属于受保护来源词；改写不能删除它或换成另一数据源。

另外补齐 continuation 边界：当原问题是完整财务问题，而模型 query 只是“继续/再算一次/重新查”等无独立财务语义的重试或延续语时，直接使用原问题。该规则只处理 rewritten 本身不能独立回答的情况，不影响带有真实实体补全的改写。

合并时仅保留能在原问题中找到依据、或符合现有具体合同/实体候选规则的主体提示。没有新增有效信息的改写直接丢弃，避免把整句以“补充识别”附加后再次触发实体抽取。

### 3. 公司级问法不接受未落地伪实体

在 query entity resolution 的单一权威位置复用一个“主体是否由问题文本支持”的判断。仅当问题已经被识别为公司级 roster、官方 AR/AP 或最新公司汇总时启用该守卫；明确客户、供应商、合同、项目问法仍允许正常实体解析。

守卫依据原问题的语义范围和文本落地性，不维护针对巡检句子的黑名单。`包括`、`期间`、`各`等结构片段会因为不是可落地业务主体而自然被拒绝；真实简称或合同名仍可通过现有 resolver 补全。即使数据库模糊匹配出一个真实存在但用户原问题未提及的主体，也不得把公司级问法改成实体级路由。

### 4. 账银对账总是产出完整三项 facts，但按问法决定 headline

只要 query family 已确定为 reconciliation，就统一计算：

- 银行净流入；
- 账上净利润；
- 名义差额 = 账上净利润 - 银行净流入。

将当前 `annotateReconciliationNominalDifference` 拆成两个职责：

- facts 注解总是执行，负责 `cash_profit_reconciliation`、三项 metrics 和 required atoms；
- headline/message 提升只在用户明确要求定量比较时执行，包括“对比/比较/差多少/差了多少/相差/差额/差异”等表达。

这样普通比较问法都能得到完整三项事实；“为什么利润和到账差这么多/怎么回事”等解释型问法也带完整结构化 facts，但保留现有解释叙事和输出重心，不强制把 headline 改成名义差额。所有路径都保留“两个口径不同、名义差额只作对账入口”的事实提示。

### 5. 多指标结果显式声明 required facts

不把 `financeFactRequiredAtoms` 改成“自动强制所有 metrics”，以免单指标或解释型回答被大量次要数字绑死。改为由多指标 producer 显式声明关键事实：

- 官方 AR/AP producer 声明应收账款、应付账款、其他应付款、应付端合计；
- reconciliation producer 声明银行净流入、账上净利润、差异金额；
- 项目 roster producer 保持项目应付 headline，并让公司级路由先返回正确事实；
- 最新营收和账上净利润继续使用现有 headline amount，重点修复原问题绑定和路由，不另加输出特例。

使用一个小型 helper 统一格式化“标签：金额 元”原子，producer 只提供有序标签和值，避免每个分支重复拼字符串。

### 6. OpenClaw 输出守卫

OpenClaw 继续优先消费 `finance_facts.required_atoms`。当模型回答缺失或冲突时，只追加/替换这些原子；不读取或复制完整 `final_answer`。现有否定冲突、金额冲突、session/run 隔离逻辑保持不变。

### 7. 插件源码形态

`plugin/openclaw-finance/dist/index.esm.js` 在当前仓库中就是被部署和测试的维护源文件；`package.json` 没有独立 build script，`index.ts` 仅做 re-export。本次直接修改并测试 dist，不新增构建链。

## 测试设计

所有生产代码修改前先写红测并确认按预期失败。

### Go MCP

- 当前用户原问题含“从账上看”，模型 query/raw 参数丢失该词时，execute 后传给 Go MCP 的 `raw_user_query` 仍保留原词（插件测试）。
- tool factory context 含 run/session 标识而 runtime context 只含部分字段时，execute 仍取得同一 run 的原问题；两个并发 run 反序调用时不串题。
- 原问题为未付款项目 roster，改写加入“包括项目名称和对应金额”时，有效 query 保持公司级 roster 语义。
- 原问题明确“收入表最新月份”，改写不得丢掉收入表口径。
- 原问题是完整财务题、模型 query 只有“继续/再算一次/重新查”时，有效 query 回退原问题；真正带实体补全的改写仍保留。
- 真实客户简称/合同名补全仍保留，证明没有退化为全量 raw 替换。
- 插件传给 MCP 的模型 `query` 保持原样，保护性合并只在 Go 测试中验证一次。

### Query router

- “2025年到2026年未付款的项目明细，包括项目名称和对应金额”以及同义 variants 均得到空 entity、`needs_contract_dimension=false`、`prefer_contract_aggregate=true`、正确数据覆盖期。
- 明确客户/供应商/合同项目问法仍进入 entity/contract dimension 路径。
- 公司级问法即使模糊匹配到数据库中真实但原问题未提及的主体，也仍保持公司级路由。
- 官方 AR/AP 多指标 facts 包含四个关键金额原子。
- 普通 comparison、`比较`、`差了多少`、`差异是多少` 均返回同一个 `cash_profit_reconciliation` 和三个 required atoms。
- 解释型 reconciliation 也包含三项 facts，但保持原有解释型 headline/message；把当前已通过的解释型与范围型 reconciliation case 纳入 A/B 回归。
- 单独问银行净流入或账上净利润不进入 reconciliation。

### OpenClaw bridge

- 当前 run 原问题覆盖模型提供的降级 `raw_user_query`，且不同 run/session 不串题。
- 多指标 AR/AP 与 reconciliation 回答缺少部分数字时，输出守卫补齐 required atoms。
- 回答已经包含正确事实时不重复；非 FinanceQA 工具和普通文本不受影响。

## 验收顺序

1. 逐组红绿测试：MCP 语义合并、query routing、reconciliation facts、AR/AP facts、OpenClaw guard。
2. `go test ./internal/mcp ./internal/query -count=1`。
3. `node --test plugin/openclaw-finance/test/*.test.mjs`。
4. `go test ./internal/... ./tests/integration ./tests/unit/... -count=1`，再跑仓库规定的 business/full 流程。
5. 用最新失败原题、对应改写文本，以及当前已通过的解释型/范围型 reconciliation case 做本地/`lzh` 只读 A/B replay。
6. 全量同步前执行版本 preflight；部署后验证二进制、插件和两个服务的实际运行态。
7. OpenClaw 真链路逐 case 重放至少 5 次，最后以新的完整 hourly patrol 的 `scores.json` 为最终验收证据。

## 非目标

- 不调整 scorer 或降低 golden 要求。
- 不修改财务金标金额、来源文件或数据覆盖月份。
- 不重构整个 query router、规则配置系统或 OpenClaw 插件架构。
- 不把 FinanceQA `final_answer` 整段复制给用户。
