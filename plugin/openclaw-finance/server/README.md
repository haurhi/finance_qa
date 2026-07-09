# FinanceQA Go MCP Runtime

## 迁移说明

旧版 Python bridge 脚本 (`finance_bridge.py`) 已废弃，改为使用 Go 原生的 MCP 服务器。

## 当前线上接入方式

当前线上 OpenClaw 不需要在 `openclaw.json` 里新增 `mcpServers`。OpenClaw 加载 `openclaw-finance` extension 后，由 extension 的 `dist/index.esm.js` 作为 MCP client。默认本机模式通过 stdio 启动 Go MCP：

```text
OpenClaw extension -> dist/index.esm.js -> ~/finance_qa/bin/financeqa serve
```

当 OpenClaw Agent 与 FinanceQA 不在同一台机器时，extension 仍安装在 OpenClaw Agent 主机，但只作为 remote connector：

```text
OpenClaw extension -> HTTPS /mcp -> FinanceQA host -> financeqa serve-http
```

远程模式下 OpenClaw Agent 主机不需要 `financeqa` binary、数据库、飞书、OSS 或 Gemini 环境；这些都保留在 FinanceQA 主机。

`FINANCEQA_BIN` 可覆盖二进制路径；线上固定路径是：

```bash
~/finance_qa/bin/financeqa
```

OpenClaw 这层会在 `before_prompt_build` 加强财务题约束。直接财务提问和模型超时后的 `Continue where you left off` fallback 都必须从当前 prompt 或最近 user 历史里恢复最新财务问题，并预先调用 `finance-query` 注入当前 Go MCP 结果上下文。若结果含 `finance_facts`，OpenClaw 以结构化事实包为事实源；`final_answer` 只作为兼容老板答案。最终回答仍由模型自行组织；只要求关键数值、期间、口径和来源一致，不要求逐字复述或完全一致，也不使用 `before_dispatch` 直接拦截输出。

为避免模型偶发漏掉老板可见事实，finance extension 还会在 `llm_output` 和 `before_message_write` 做窄范围补强：仅当同一会话刚产生当前 `finance-query` 结果，且最终 assistant 文本缺少或误用期间、口径、金额、来源或来源更新时间等 FinanceQA fact atom 时，修补缺失或冲突的标准事实行。事实原子优先来自 `finance_facts.required_atoms`，没有事实包时才从兼容字段抽取。这个逻辑不复制完整 `final_answer`，也不处理其他插件或其他 MCP 的回答。

### 1. 直接运行 Go MCP 服务器

```bash
~/finance_qa/bin/financeqa serve [--db <dsn>] [--company <name>]
```

远程 MCP server 使用 HTTP transport，并要求 token 文件：

```bash
FINANCEQA_MCP_LISTEN=127.0.0.1:3009 \
FINANCEQA_MCP_READ_TOKEN_FILE=/root/finance_qa/secrets/mcp_read_token \
FINANCEQA_MCP_ADMIN_TOKEN_FILE=/root/finance_qa/secrets/mcp_admin_token \
~/finance_qa/bin/financeqa serve-http
```

token 文件必须由部署环境生成并 `chmod 600`；不要把 token 写入仓库、`.env` 或日志。

### 2. OpenClaw 配置要求

线上只需要确保 OpenClaw 已启用 finance extension 和全局 skill 路径：

```json
{
  "skills": {
    "load": {
      "extraDirs": ["/root/.openclaw/skills/finance"]
    }
  },
  "plugins": {
    "entries": {
      "openclaw-finance": {
        "enabled": true,
        "hooks": {
          "allowPromptInjection": true
        }
      }
    },
    "installs": {
      "openclaw-finance": {
        "source": "path",
        "sourcePath": "/root/.openclaw/extensions/openclaw-finance",
        "installPath": "/root/.openclaw/extensions/openclaw-finance",
        "version": "2.2.57"
      }
    }
  }
}
```

如果这几项已经存在，部署 Go MCP runtime、skill 或二进制时不需要修改 `openclaw.json` 的运行开关。当前线上 `openclaw.json` 的 `plugins.installs.openclaw-finance.version` 是 OpenClaw install metadata，需要与 Go MCP binary 和 OpenClaw plugin metadata 同步。另一个关键点是 `package.json` 必须声明 `openclaw.extensions: ["./dist/index.esm.js"]`，这才是 OpenClaw 发现插件的入口。

远程模式在 `plugins.entries.openclaw-finance.config` 下增加：

```json
{
  "transport": "remote",
  "mcp_url": "https://financeqa.example.com/mcp",
  "mcp_token_file": "/root/finance_qa/secrets/mcp_read_token",
  "timeout_ms": 60000
}
```

发布路径固定为：OpenClaw extension 目录保留拷贝后的 runtime 实文件；`~/.openclaw/skills/finance` 和 `~/.claude/skills/finance` 使用目录级 symlink 指向 `/root/finance_qa`。不要把 extension 目录改成 symlink，也不要使用文件级 skill symlink；后者会被 OpenClaw skill loader 跳过。

替换 extension runtime 文件后必须重启 OpenClaw Gateway；运行中的 Gateway 不会自动重新加载已注册的插件实例。同步脚本默认执行 `openclaw gateway restart` 并检查 `RPC probe: ok`。

插件通过 stdio 启动 `~/finance_qa/bin/financeqa serve` 时，工作目录固定为 `~/finance_qa`，确保 Gateway 进程也能加载仓库 `.env` 和默认数据库配置。
部署新二进制前，脚本会先停止旧的 `~/finance_qa/bin/financeqa serve` 子进程，避免正在执行的二进制导致上传失败。

## 暴露的工具

- `finance-query` - 财务问答
- `finance-host-data` - 宿主LLM兜底数据
- `finance-upload` - 导入单个文件
- `finance-sync` - 批量同步目录
- `finance-dimensions` - 维度管理

## 兼容性

- Go MCP 工具 payload 保持原 Python bridge 兼容字段
- `finance_facts`、`final_answer` 和 `host_summary_contract` 字段已原生支持
- 无需 Python 环境依赖
