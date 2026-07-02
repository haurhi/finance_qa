package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestOpenClawFinancePluginLetsModelUseFinanceToolWithoutHardIntercept(t *testing.T) {
	t.Parallel()

	pluginPath := filepath.Join("..", "..", "plugin", "openclaw-finance", "dist", "index.esm.js")
	plugin, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read OpenClaw finance plugin runtime: %v", err)
	}
	pluginText := string(plugin)

	for _, want := range []string{
		`api.on("before_prompt_build"`,
		`api.on("llm_output"`,
		`finance-query`,
		`数据(出来|有了|有没有|情况|多少)`,
		`mustCallFinanceQuerySystemContext`,
		`financeQuerySystemFacts`,
		`latestFinanceQuestionFromMessages`,
		`latestUserTextFromMessages`,
		`financeQuestionForPromptEvent`,
		`最新财务问题`,
		`previous model attempt failed`,
		`contract_continuity_candidates`,
		`final_answer 是事实锚点，不是固定话术模板`,
		`可以重组表达顺序、表格和老板口吻`,
		`不要把 final_answer 的 YYYY-MM 或 YYYY-MM~YYYY-MM 期间改成相对时间或其他月份`,
		`来源和来源更新时间必须从 final_answer 逐字复制`,
		`指标和口径标签必须从 final_answer 逐字保留`,
		`same-project candidates/references`,
		`isFinanceQuestion`,
		`findFinanceQACwd`,
		`cwd: findFinanceQACwd(this.binaryPath)`,
		`api.on("before_message_write"`,
		`pendingFinanceFactAtomsBySession`,
		`patchAssistantTextsWithFinanceFactAtoms`,
		`financeFactAtomsFromToolResult`,
		`patchAssistantMessageWithFinanceFactAtoms`,
		`金额：${compact.total} 元`,
	} {
		if !strings.Contains(pluginText, want) {
			t.Fatalf("OpenClaw finance plugin should contain %q", want)
		}
	}
	for _, reject := range []string{
		`api.on("before_dispatch"`,
		`FINANCE_QUERY_FINAL_ANSWER_START`,
		`FINANCE_QUERY_PAYLOAD_START`,
		`FINANCE_BRIDGE_PATH`,
		`/root/.openclaw/extensions/openclaw-finance/server/finance_bridge.py`,
		`forcedAnswersBySessionKey`,
		`isBridgeFallbackPayload`,
		`默认必须逐字复制 final_answer`,
		`copy final_answer exactly`,
		`finance-query has already been executed`,
		`api.on("cleanup"`,
		`Latest finance question that MUST be sent to finance-query`,
		`Current authoritative finance-query result`,
		`Use these current facts`,
		`You may rephrase the final wording`,
	} {
		if strings.Contains(pluginText, reject) {
			t.Fatalf("OpenClaw finance plugin should not hard-intercept model answers; found %q", reject)
		}
	}
	if !strings.Contains(pluginText, "不要沿用历史对话") {
		t.Fatalf("prompt hook should forbid stale repeated answers from conversation history")
	}
	if !strings.Contains(pluginText, "process.env.FINANCEQA_BIN") {
		t.Fatalf("OpenClaw finance plugin should resolve the Go MCP binary path from FINANCEQA_BIN")
	}
	if strings.Contains(pluginText, "async register(api)") {
		t.Fatalf("OpenClaw finance plugin register hook must stay synchronous; async register is ignored by OpenClaw")
	}
	if strings.Contains(pluginText, "void getMCPClient().catch") ||
		strings.Contains(pluginText, "Failed to pre-start MCP client") ||
		strings.Contains(pluginText, "Try to pre-start MCP client") {
		t.Fatalf("OpenClaw finance plugin must not pre-start financeqa during plugin registration")
	}
	if !strings.Contains(pluginText, "register(api) {") {
		t.Fatalf("OpenClaw finance plugin should expose a synchronous register(api) hook")
	}
	if !strings.Contains(pluginText, `process.env.HOME || ""`) ||
		!strings.Contains(pluginText, `"finance_qa/bin/financeqa"`) {
		t.Fatalf("OpenClaw finance plugin should prefer the fixed server binary path under $HOME/finance_qa/bin/financeqa")
	}
	if !strings.Contains(pluginText, "existsSync(candidate)") {
		t.Fatalf("OpenClaw finance plugin should check binary candidates before selecting one")
	}
	for _, forbidden := range []string{
		`path.resolve(repoRoot, "financeqa")`,
		`path.resolve(process.cwd(), "financeqa")`,
		`"/usr/local/bin/financeqa"`,
		`"/usr/bin/financeqa"`,
	} {
		if strings.Contains(pluginText, forbidden) {
			t.Fatalf("OpenClaw finance plugin should not fall back to non-canonical financeqa binary path %q", forbidden)
		}
	}
	if !strings.Contains(pluginText, `spawn(this.binaryPath, ["serve"]`) {
		t.Fatalf("OpenClaw finance plugin should start financeqa serve as the Go MCP server")
	}
}

func TestOpenClawFinancePluginMetadataUsesCurrentMajorVersion(t *testing.T) {
	t.Parallel()

	var packageDoc map[string]any
	for _, path := range []string{
		filepath.Join("..", "..", "plugin", "openclaw-finance", "package.json"),
		filepath.Join("..", "..", "plugin", "openclaw-finance", "openclaw.plugin.json"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read plugin metadata %s: %v", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse plugin metadata %s: %v", path, err)
		}
		if got := doc["version"]; got != "2.2.25" {
			t.Fatalf("%s version = %v, want 2.2.25", path, got)
		}
		if strings.HasSuffix(path, "package.json") {
			packageDoc = doc
		}
	}
	openclawConfig, ok := packageDoc["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("plugin package.json must contain openclaw metadata")
	}
	extensions, ok := openclawConfig["extensions"].([]any)
	if !ok {
		t.Fatalf("plugin package.json openclaw.extensions must be present for OpenClaw discovery")
	}
	if len(extensions) != 1 || extensions[0] != "./dist/index.esm.js" {
		t.Fatalf("plugin package.json openclaw.extensions = %v, want [./dist/index.esm.js]", extensions)
	}
	if _, ok := openclawConfig["entry"]; ok {
		t.Fatalf("plugin package.json should use openclaw.extensions, not legacy openclaw.entry")
	}
}

func TestOpenClawFinancePluginExposesFinalAnswerJsonForFinanceQuery(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for OpenClaw plugin runtime test")
	}

	tmp := t.TempDir()
	stubPath := filepath.Join(tmp, "financeqa_stub.mjs")
	stubScript := `#!/usr/bin/env node
import fs from "node:fs";
import readline from "node:readline";

const rl = readline.createInterface({ input: process.stdin });
rl.on("line", (line) => {
  const request = JSON.parse(line);
  if (request.method === "initialize") {
    process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: request.id, result: { protocolVersion: "2024-11-05" } }) + "\n");
    return;
  }
  if (request.method === "tools/call") {
    if (process.env.FINANCEQA_CAPTURE_PATH) {
      fs.appendFileSync(process.env.FINANCEQA_CAPTURE_PATH, JSON.stringify(request.params?.arguments || {}) + "\n");
    }
    const payload = {
      success: true,
      final_answer: "FINAL:2026年3月应收账款\n来源：《测试表》",
      data: {
        internal_detail: "SHOULD_NOT_BE_VISIBLE_TO_OPENCLAW_MODEL",
        metric: "应付",
        metric_label: "项目应付（应付未付/未付款）",
        business_basis: "项目成本口径，应付未付金额",
        total: 1946918.51,
        source_note: "来源：《测试表》",
        source_update_note: "来源更新时间：2026-06-29 15:39:43",
        contract_summary: {
          invoice_unpaid_items: [
            {
              supplier_name: "测试供应商二",
              contract_content: "测试供应商项目二",
              invoice_amount: 100,
              paid_amount: 0,
              open_amount: 100
            }
          ]
        },
        detail_items: [
          {
            entity: "四川其妙科技有限公司",
            period_label: "2026年Q1",
            contract_content: "行业商品数据采购合同-A01",
            settlement_amount: 3677082.70,
            received_amount: 2072997.36,
            unpaid_amount: 1604085.34
          }
        ]
      }
    };
    process.stdout.write(JSON.stringify({
      jsonrpc: "2.0",
      id: request.id,
      result: { content: [{ type: "text", text: JSON.stringify(payload) }] }
    }) + "\n");
  }
});
`
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o755); err != nil {
		t.Fatalf("write MCP stub: %v", err)
	}

	runnerPath := filepath.Join(tmp, "run_plugin.mjs")
	pluginPath, err := filepath.Abs(filepath.Join("..", "..", "plugin", "openclaw-finance", "dist", "index.esm.js"))
	if err != nil {
		t.Fatalf("resolve plugin path: %v", err)
	}
	runnerScript := `import { pathToFileURL } from "node:url";

const plugin = (await import(pathToFileURL(process.argv[2]).href)).default;
const tools = new Map();
const hooks = new Map();
const api = {
  registerTool(tool, options) {
    tools.set(options?.name || tool.name, tool);
  },
  on(name, handler) {
    hooks.set(name, handler);
  }
};

plugin.register(api);
const tool = tools.get("finance-query");
if (!tool) {
  console.error("missing finance-query tool");
  process.exit(1);
}
const result = await tool.execute("test-call", { query: "2026年3月应收账款多少？" });
const text = result?.content?.[0]?.text || "";
let payload;
try {
  payload = JSON.parse(text);
} catch (err) {
  console.error("tool output should remain JSON:", text);
  process.exit(1);
}
if (payload.final_answer !== "FINAL:2026年3月应收账款\n来源：《测试表》") {
  console.error("unexpected final_answer:", JSON.stringify(payload.final_answer));
  process.exit(1);
}
if (payload.data?.internal_detail !== "SHOULD_NOT_BE_VISIBLE_TO_OPENCLAW_MODEL") {
  console.error("unexpected payload passthrough:", JSON.stringify(payload.data));
  process.exit(1);
}
const promptHook = hooks.get("before_prompt_build");
if (!promptHook) {
  console.error("missing before_prompt_build hook");
  process.exit(1);
}
const directHookResult = await promptHook({ prompt: "2026年3月应收账款多少（已开票未收款）？", messages: [] });
if (!directHookResult?.prependSystemContext?.includes("最新财务问题：2026年3月应收账款多少（已开票未收款）？")) {
  console.error("direct finance prompt should inject latest finance question context:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (Object.prototype.hasOwnProperty.call(directHookResult, "prependContext")) {
  console.error("direct finance prompt must not inject visible prependContext:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("FINAL:2026年3月应收账款")) {
  console.error("direct finance prompt should inject current finance-query facts into hidden system context:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("来源和来源更新时间必须从 final_answer 逐字复制")) {
  console.error("direct finance prompt should tell the model to preserve source lines verbatim:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("指标和口径标签必须从 final_answer 逐字保留")) {
  console.error("direct finance prompt should tell the model to preserve metric labels verbatim:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("标准指标标签：项目应付（应付未付/未付款）")) {
  console.error("direct finance prompt should expose metric_label as a protected fact:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("业务口径：项目成本口径，应付未付金额")) {
  console.error("direct finance prompt should expose business_basis as a protected fact:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("标准金额：1946918.51")) {
  console.error("direct finance prompt should expose total as a protected fact:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("\"金额：1946918.51 元\"")) {
  console.error("direct finance prompt should require exact yuan-denominated amount as a boss-visible fact atom:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("\"口径：项目应付（应付未付/未付款）\"")) {
  console.error("direct finance prompt should require metric label as a boss-visible fact atom:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("老板可见回复必须出现的精确片段")) {
  console.error("direct finance prompt should list exact boss-visible fact atoms:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("\"来源：《测试表》\"") ||
    !directHookResult?.prependSystemContext?.includes("\"来源更新时间：2026-06-29 15:39:43\"")) {
  console.error("direct finance prompt should require source note and update note as boss-visible fact atoms:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("final_answer 是事实锚点，不是固定话术模板")) {
  console.error("direct finance prompt should treat final_answer as factual anchor, not a fixed wording template:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("可以重组表达顺序、表格和老板口吻")) {
  console.error("direct finance prompt should allow flexible wording around protected facts:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("不要把 final_answer 的 YYYY-MM 或 YYYY-MM~YYYY-MM 期间改成相对时间或其他月份")) {
  console.error("direct finance prompt should forbid period drift without requiring verbatim final_answer copying:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("不要删掉 final_answer 中修饰指标的业务前缀，例如“项目成本口径”“项目口径”“应收未收”")) {
  console.error("direct finance prompt should protect business-basis prefixes such as project cost basis:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("不要提及“之前”“上次”“这次返回”“工具返回”“finance-query 返回”“我需要用”")) {
  console.error("direct finance prompt should forbid process/history repair wording in boss-visible answers:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!tool.description.includes("use final_answer as the factual source")) {
  console.error("finance-query tool description should preserve facts without forcing exact copying:", tool.description);
  process.exit(1);
}
if (tool.description.includes("copy final_answer exactly")) {
  console.error("finance-query tool description should not force exact final_answer copying:", tool.description);
  process.exit(1);
}
const sourceProtectedHookResult = await promptHook({
  prompt: "账上2026年3月应收和应付分别还挂着多少？哪头更重？",
  messages: []
});
if (!sourceProtectedHookResult?.prependSystemContext?.includes("最新财务问题：账上2026年3月应收和应付分别还挂着多少？哪头更重？")) {
  console.error("source-constrained finance prompt should retain raw user wording:", JSON.stringify(sourceProtectedHookResult));
  process.exit(1);
}
await tool.execute("rewritten-query-call", { query: "2026年3月应收和应付分别还挂着多少？哪头更重？" });
const capturedCalls = (await import("node:fs")).readFileSync(process.env.FINANCEQA_CAPTURE_PATH, "utf8")
  .trim()
  .split("\n")
  .filter(Boolean)
  .map((line) => JSON.parse(line));
const lastCapturedQuery = capturedCalls.at(-1)?.query;
if (lastCapturedQuery !== "账上2026年3月应收和应付分别还挂着多少？哪头更重？") {
  console.error("finance-query should preserve latest raw user wording over model-rewritten query:", JSON.stringify({ lastCapturedQuery, capturedCalls }));
  process.exit(1);
}
await tool.execute("second-call-without-new-prompt", { query: "2026年4月收入多少？" });
const afterConsumeCalls = (await import("node:fs")).readFileSync(process.env.FINANCEQA_CAPTURE_PATH, "utf8")
  .trim()
  .split("\n")
  .filter(Boolean)
  .map((line) => JSON.parse(line));
const queryAfterConsume = afterConsumeCalls.at(-1)?.query;
if (queryAfterConsume !== "2026年4月收入多少？") {
  console.error("raw finance wording override should be consumed after one finance-query call:", JSON.stringify({ queryAfterConsume, afterConsumeCalls }));
  process.exit(1);
}
await promptHook({ prompt: "今天天气怎么样？", messages: [] });
await tool.execute("after-non-finance-turn", { query: "2026年4月收入多少？" });
const afterClearCalls = (await import("node:fs")).readFileSync(process.env.FINANCEQA_CAPTURE_PATH, "utf8")
  .trim()
  .split("\n")
  .filter(Boolean)
  .map((line) => JSON.parse(line));
const queryAfterNonFinanceTurn = afterClearCalls.at(-1)?.query;
if (queryAfterNonFinanceTurn !== "2026年4月收入多少？") {
  console.error("non-finance prompt should clear captured raw finance wording:", JSON.stringify({ queryAfterNonFinanceTurn, afterClearCalls }));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("行业商品数据采购合同-A01") ||
    !directHookResult?.prependSystemContext?.includes("3677082.7") ||
    !directHookResult?.prependSystemContext?.includes("1604085.34")) {
  console.error("direct finance prompt should inject compound detail_items for detailed questions:", JSON.stringify(directHookResult));
  process.exit(1);
}
if (!directHookResult?.prependSystemContext?.includes("测试供应商项目二") ||
    !directHookResult?.prependSystemContext?.includes("测试供应商二") ||
    !directHookResult?.prependSystemContext?.includes("100")) {
  console.error("direct finance prompt should inject contract_summary open items for roster questions:", JSON.stringify(directHookResult));
  process.exit(1);
}
for (const forbidden of ["Current authoritative finance-query result", "Use these current facts", "You may rephrase the final wording", "Latest finance question that MUST"]) {
  if (directHookResult?.prependSystemContext?.includes(forbidden)) {
    console.error("direct finance prompt should not include leak-prone English context marker:", forbidden, JSON.stringify(directHookResult));
    process.exit(1);
  }
}
const messageOnlyHookResult = await promptHook({
  prompt: "",
  messages: [
    { role: "user", content: [{ type: "text", text: "[Fri 2026-05-08 11:57 GMT+8] 2026年3月应收账款多少（已开票未收款）？" }] }
  ]
});
if (!messageOnlyHookResult?.prependSystemContext?.includes("2026年3月应收账款多少（已开票未收款）？")) {
  console.error("message-only prompt event should recover latest finance question:", JSON.stringify(messageOnlyHookResult));
  process.exit(1);
}
const followupHookResult = await promptHook({
  prompt: "含未开票未付款多少",
  messages: [
    { role: "user", content: [{ type: "text", text: "从25年10月到今年4月底 客户未付款金额多少" }] },
    { role: "assistant", content: [{ type: "text", text: "已开票未回款 72801.07 元" }] },
    { role: "user", content: [{ type: "text", text: "含未开票未付款多少" }] }
  ]
});
if (!followupHookResult?.prependSystemContext?.includes("最新财务问题：从25年10月到今年4月底 客户未付款金额多少；含未开票未付款多少")) {
  console.error("context-dependent finance followup should be combined with previous finance question:", JSON.stringify(followupHookResult));
  process.exit(1);
}
const retryHookResult = await promptHook({
  prompt: "Continue where you left off. The previous model attempt failed or timed out.",
  messages: [
    { role: "assistant", content: [{ type: "text", text: "old stale answer" }] },
    { role: "user", content: [{ type: "text", text: "[Fri 2026-05-08 11:48 GMT+8] 2026年3月应收账款多少（已开票未收款）？" }] }
  ]
});
if (!retryHookResult?.prependSystemContext?.includes("2026年3月应收账款多少（已开票未收款）？")) {
  console.error("retry fallback prompt should recover latest finance question from messages:", JSON.stringify(retryHookResult));
  process.exit(1);
}
process.exit(0);
`
	if err := os.WriteFile(runnerPath, []byte(runnerScript), 0o644); err != nil {
		t.Fatalf("write plugin runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", runnerPath, pluginPath)
	capturePath := filepath.Join(tmp, "financeqa_calls.jsonl")
	cmd.Env = append(os.Environ(), "FINANCEQA_BIN="+stubPath, "FINANCEQA_CAPTURE_PATH="+capturePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenClaw finance-query should expose final_answer JSON to the model: %v\n%s", err, string(out))
	}
}

func TestSyncScriptPublishesOpenClawFinancePluginRuntime(t *testing.T) {
	t.Parallel()

	syncScriptPath := filepath.Join("..", "..", "tests", "scripts", "sync_openclaw_bridge_and_skill.sh")
	syncScript, err := os.ReadFile(syncScriptPath)
	if err != nil {
		t.Fatalf("read sync script: %v", err)
	}
	scriptText := string(syncScript)

	for _, want := range []string{
		`SERVER="${SERVER:-lzh}"`,
		`MODE="${MODE:-all}"`,
		`all|server|connector) ;;`,
		`deploy_server()`,
		`deploy_connector()`,
		`ssh_remote()`,
		`scp_remote()`,
		`if [[ -n "${KEY_PATH:-}" ]]; then`,
		`REMOTE_HOME="${REMOTE_HOME:-$(ssh_remote "$SERVER" 'printf %s "$HOME"')}"`,
		`LOCAL_PLUGIN_DIST="$ROOT_DIR/plugin/openclaw-finance/dist/index.esm.js"`,
		`LOCAL_PLUGIN_MANIFEST="$ROOT_DIR/plugin/openclaw-finance/openclaw.plugin.json"`,
		`LOCAL_CLAUDE_WRAPPER="$ROOT_DIR/tests/scripts/claude_finance_final_answer.sh"`,
		`LOCAL_ONLINE_CHECKER="$ROOT_DIR/tests/scripts/run_online_agent_final_answer_check.py"`,
		`LOCAL_MCP_SYSTEMD="$ROOT_DIR/deploy/systemd/financeqa-mcp.service"`,
		`scp_remote "$LOCAL_PLUGIN_DIST" "$SERVER:$REMOTE_REPO_DIR/plugin/openclaw-finance/dist/index.esm.js"`,
		`scp_remote "$LOCAL_PLUGIN_MANIFEST" "$SERVER:$REMOTE_REPO_DIR/plugin/openclaw-finance/openclaw.plugin.json"`,
		`scp_remote "$LOCAL_CLAUDE_WRAPPER" "$SERVER:$REMOTE_REPO_DIR/tests/scripts/claude_finance_final_answer.sh"`,
		`scp_remote "$LOCAL_ONLINE_CHECKER" "$SERVER:$REMOTE_REPO_DIR/tests/scripts/run_online_agent_final_answer_check.py"`,
		`if deploy_connector; then`,
		`if [ -L '$REMOTE_OPENCLAW_PLUGIN_DIR' ]; then rm -f '$REMOTE_OPENCLAW_PLUGIN_DIR'; fi;`,
		`cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/dist/index.esm.js' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'`,
		`cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/openclaw.plugin.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json'`,
		`cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/package.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'`,
		`REMOTE_FINANCEQA_BIN="${REMOTE_FINANCEQA_BIN:-$REMOTE_REPO_DIR/bin/financeqa}"`,
		`REMOTE_FINANCEQA_UPLOAD="${REMOTE_FINANCEQA_UPLOAD:-$REMOTE_FINANCEQA_BIN.upload.$$}"`,
		`REMOTE_MCP_READ_TOKEN_FILE="${REMOTE_MCP_READ_TOKEN_FILE:-$REMOTE_REPO_DIR/secrets/mcp_read_token}"`,
		`REMOTE_MCP_ADMIN_TOKEN_FILE="${REMOTE_MCP_ADMIN_TOKEN_FILE:-$REMOTE_REPO_DIR/secrets/mcp_admin_token}"`,
		`LOCAL_FINANCEQA_BIN="$(mktemp`,
		`if deploy_server; then`,
		`GOOS=linux GOARCH=amd64 go build -o "$LOCAL_FINANCEQA_BIN" ./cmd/financeqa/...`,
		`REMOTE_FINANCEQA_SERVE_PATTERN`,
		`pgrep -f '$REMOTE_FINANCEQA_SERVE_PATTERN'`,
		`scp_remote "$LOCAL_FINANCEQA_BIN" "$SERVER:$REMOTE_FINANCEQA_UPLOAD"`,
		`mv -f '$REMOTE_FINANCEQA_UPLOAD' '$REMOTE_FINANCEQA_BIN'`,
		`rm -f '$REMOTE_REPO_DIR/financeqa'`,
		`test -s \"\$token_file\"`,
		`stat -c '%a'`,
		`install -m 0644 /tmp/financeqa-mcp.service.$$ '$REMOTE_SYSTEMD_DIR/financeqa-mcp.service'`,
		`systemctl enable --now financeqa-mcp.service`,
		`systemctl restart financeqa-mcp.service`,
		`grep -n 'RemoteMCPClient' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'`,
		`pkg.openclaw?.extensions`,
		`verify OpenClaw config references the finance plugin and skill path`,
		`cfg.plugins?.entries?.['openclaw-finance']?.enabled === true`,
		`cfg.plugins?.entries?.['openclaw-finance']?.hooks?.allowPromptInjection === true`,
		`cfg.plugins.installs['openclaw-finance'].version = pluginVersion`,
		`cfg.plugins.installs['openclaw-finance'].installedAt = new Date().toISOString()`,
		`RESTART_OPENCLAW_GATEWAY="${RESTART_OPENCLAW_GATEWAY:-1}"`,
		`openclaw gateway restart`,
		`grep -q 'RPC probe: ok' /tmp/openclaw_gateway_status.txt`,
		`grep -q 'Runtime: running' /tmp/openclaw_gateway_status.txt`,
	} {
		if !strings.Contains(scriptText, want) {
			t.Fatalf("sync script should publish OpenClaw plugin runtime and prompt hook config; missing %q", want)
		}
	}
	if strings.Contains(scriptText, `cfg.plugins.entries['openclaw-finance'].enabled = true`) ||
		strings.Contains(scriptText, `cfg.plugins.entries['openclaw-finance'].hooks.allowPromptInjection = true`) {
		t.Fatalf("sync script should verify existing OpenClaw runtime config by default, not rewrite plugin entry settings")
	}
	if strings.Contains(scriptText, `$SERVER:$REMOTE_OPENCLAW_PLUGIN_DIR/index.ts`) {
		t.Fatalf("sync script should not publish plugin source entrypoint into the OpenClaw extension")
	}
	if strings.Contains(scriptText, `ln -sfn '$REMOTE_REPO_DIR/plugin/openclaw-finance' '$REMOTE_OPENCLAW_PLUGIN_DIR'`) ||
		strings.Contains(scriptText, `ln -sfn '$REMOTE_REPO_DIR' '$REMOTE_OPENCLAW_PLUGIN_DIR'`) {
		t.Fatalf("sync script should keep OpenClaw extension as copied runtime files, not a directory symlink")
	}
	if strings.Contains(scriptText, "LOCAL_PLUGIN_INDEX") ||
		strings.Contains(scriptText, "plugin/openclaw-finance/index.ts' '$REMOTE_OPENCLAW_PLUGIN_DIR/index.ts") {
		t.Fatalf("sync script should not publish plugin source entrypoint into the OpenClaw extension")
	}
	if strings.Contains(scriptText, `go build -o '$REMOTE_REPO_DIR/financeqa'`) {
		t.Fatalf("sync script should not build the canonical server binary at repo root")
	}
	if strings.Contains(scriptText, `cd '$REMOTE_REPO_DIR' && mkdir -p '$(dirname "$REMOTE_FINANCEQA_BIN")' && go build`) {
		t.Fatalf("sync script should build the server binary from local source before uploading")
	}
	if strings.Contains(scriptText, `ln -sfn '$REMOTE_FINANCEQA_BIN' '$REMOTE_REPO_DIR/financeqa'`) {
		t.Fatalf("sync script should not keep a second repo-root financeqa entrypoint")
	}
	if strings.Contains(scriptText, "finance_bridge.py") || strings.Contains(scriptText, "LOCAL_BRIDGE") {
		t.Fatalf("sync script should publish Go MCP only, got Python bridge references")
	}
}

func TestDeploymentDocsAndSyncScriptDoNotExposeProductionNetworkTargets(t *testing.T) {
	t.Parallel()

	checkedFiles := []string{
		filepath.Join("..", "..", "README.md"),
		filepath.Join("..", "..", "docs", "architecture", "03-deployment-runtime.md"),
		filepath.Join("..", "..", "plugin", "openclaw-finance", "server", "README.md"),
		filepath.Join("..", "..", "tests", "scripts", "deploy_openclaw.sh"),
		filepath.Join("..", "..", "tests", "scripts", "sync_openclaw_bridge_and_skill.sh"),
	}
	publicIPv4 := regexp.MustCompile(`\b(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-5])\.(?:\d{1,3})\.(?:\d{1,3})\.(?:\d{1,3})\b`)
	privateOrLocalIPv4 := regexp.MustCompile(`^(?:127\.|10\.|192\.168\.|172\.(?:1[6-9]|2\d|3[0-1])\.)`)

	for _, path := range checkedFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		for _, match := range publicIPv4.FindAllString(text, -1) {
			if privateOrLocalIPv4.MatchString(match) {
				continue
			}
			t.Fatalf("%s exposes public IPv4 target %q; use placeholders or env vars", path, match)
		}
		if strings.Contains(text, `KEY_PATH="${KEY_PATH:-`) || strings.Contains(text, `KEY="${HOME}`) {
			t.Fatalf("%s must not provide a default private key path", path)
		}
	}
}

func TestFinanceFinalAnswerWrapperDoesNotHardcodeServerBridgePath(t *testing.T) {
	t.Parallel()

	wrapperPath := filepath.Join("..", "..", "tests", "scripts", "claude_finance_final_answer.sh")
	wrapper, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatalf("read final answer wrapper: %v", err)
	}
	wrapperText := string(wrapper)
	if strings.Contains(wrapperText, "/root/.openclaw/extensions/openclaw-finance/server/finance_bridge.py") {
		t.Fatalf("final answer wrapper should not hardcode the server bridge path")
	}
	if strings.Contains(wrapperText, "FINANCE_BRIDGE_PATH") || strings.Contains(wrapperText, "BRIDGE_PATH") || strings.Contains(wrapperText, "finance_bridge.py") {
		t.Fatalf("final answer wrapper should not call the Python bridge")
	}
	if !strings.Contains(wrapperText, `FINANCEQA_BIN="${FINANCEQA_BIN:-$ROOT_DIR/bin/financeqa}"`) {
		t.Fatalf("final answer wrapper should default to the canonical repo bin/financeqa binary")
	}
	if !strings.Contains(wrapperText, `financeqa_bin, "serve"`) {
		t.Fatalf("final answer wrapper should call financeqa serve")
	}
}
