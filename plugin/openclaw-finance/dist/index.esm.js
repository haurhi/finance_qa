import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";

const PLUGIN_ID = "openclaw-finance";

// Path to financeqa binary (auto-detect or from env)
const FINANCEQA_BIN = process.env.FINANCEQA_BIN || findFinanceQABinary();

function isRecord(value) {
  return value !== null && typeof value === "object";
}

function isAbortSignal(value) {
  return isRecord(value) && typeof value.aborted === "boolean" && typeof value.addEventListener === "function";
}

function getPath(obj, pathParts) {
  let current = obj;
  for (const key of pathParts) {
    if (!isRecord(current) || !(key in current)) return undefined;
    current = current[key];
  }
  return current;
}

function normalizePluginConfig(input) {
  if (!isRecord(input)) {
    return { transport: "stdio", timeout_ms: 60000 };
  }
  const mcpURL = typeof input.mcp_url === "string" ? input.mcp_url.trim() : "";
  const mcpToken = typeof input.mcp_token === "string" ? input.mcp_token.trim() : "";
  const mcpTokenFile = typeof input.mcp_token_file === "string" ? input.mcp_token_file.trim() : "";
  const transportValue = typeof input.transport === "string" ? input.transport.trim() : "";
  const transport = transportValue || (mcpURL || mcpToken || mcpTokenFile ? "remote" : "stdio");
  const timeout = Number(input.timeout_ms ?? 60000);
  const timeout_ms = Number.isFinite(timeout) && timeout > 0 ? timeout : 60000;
  if (transport !== "remote") {
    return { transport: "stdio", timeout_ms };
  }
  const resolvedTokenFile = mcpTokenFile ? resolveTokenFilePath(mcpTokenFile) : "";
  const token = resolvedTokenFile ? readTokenFile(resolvedTokenFile) : mcpToken;
  if (!mcpURL || !token) {
    throw new Error('Finance plugin remote config missing: expected "mcp_url" and either "mcp_token" or "mcp_token_file".');
  }
  return {
    transport: "remote",
    mcp_url: validateMcpURL(mcpURL),
    mcp_token: token,
    ...(resolvedTokenFile ? { mcp_token_file: resolvedTokenFile } : {}),
    timeout_ms
  };
}

function tryRuntimeConfig(runtime) {
  const direct = runtime.getPluginConfig?.(PLUGIN_ID);
  if (direct !== undefined) return normalizePluginConfig(direct);

  const configFromGetter = runtime.getConfig?.();
  const getterNested = getPath(configFromGetter, ["plugins", "entries", PLUGIN_ID, "config"]);
  if (getterNested !== undefined) return normalizePluginConfig(getterNested);

  if (isRecord(runtime.config)) {
    const loadedConfig = runtime.config.loadConfig?.();
    const loadedNested = getPath(loadedConfig, ["plugins", "entries", PLUGIN_ID, "config"]);
    if (loadedNested !== undefined) return normalizePluginConfig(loadedNested);
    const configGetNested = getPath(runtime.config.get?.(), ["plugins", "entries", PLUGIN_ID, "config"]);
    if (configGetNested !== undefined) return normalizePluginConfig(configGetNested);
  }

  for (const container of [runtime.config, runtime.settings, runtime.state?.config, runtime.plugins]) {
    const nested = getPath(container, ["plugins", "entries", PLUGIN_ID, "config"]);
    if (nested !== undefined) return normalizePluginConfig(nested);
  }
  return null;
}

function loadPluginConfig(runtime) {
  if (isRecord(runtime)) {
    const resolved = tryRuntimeConfig(runtime);
    if (resolved) return resolved;
  }
  return { transport: "stdio", timeout_ms: 60000 };
}

function validateMcpURL(value) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`Invalid mcp_url: ${value}`);
  }
  if (!/^https?:$/.test(parsed.protocol)) {
    throw new Error(`Invalid mcp_url protocol: ${parsed.protocol}`);
  }
  if (!parsed.pathname.endsWith("/mcp")) {
    throw new Error(`Invalid mcp_url endpoint: expected path ending with /mcp, got ${parsed.pathname || "/"}`);
  }
  return parsed.toString();
}

function resolveTokenFilePath(value) {
  const trimmed = String(value || "").trim();
  if (!trimmed) return "";
  if (trimmed === "~") return process.env.HOME || trimmed;
  if (trimmed.startsWith("~/")) {
    return path.join(process.env.HOME || "", trimmed.slice(2));
  }
  return path.resolve(trimmed);
}

function readTokenFile(filePath) {
  let token;
  try {
    token = readFileSync(filePath, "utf8").trim();
  } catch (error) {
    throw new Error(`Finance plugin remote token file could not be read: ${filePath}: ${error?.message || String(error)}`);
  }
  if (!token) {
    throw new Error(`Finance plugin remote token file is empty: ${filePath}`);
  }
  return token;
}

function findFinanceQABinary() {
  const pluginDir = path.dirname(fileURLToPath(import.meta.url));
  const repoRoot = path.resolve(pluginDir, "../../..");
  const fixedServerBin = path.join(process.env.HOME || "", "finance_qa/bin/financeqa");
  const candidates = [
    fixedServerBin,
    path.resolve(repoRoot, "bin/financeqa"),
    path.resolve(process.cwd(), "bin/financeqa")
  ];
  for (const candidate of candidates) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  return fixedServerBin;
}

function findFinanceQACwd(binaryPath) {
  const binDir = path.dirname(binaryPath);
  if (path.basename(binDir) === "bin") {
    return path.dirname(binDir);
  }
  return process.cwd();
}

const FINANCE_KEYWORDS = [
  "财务",
  "经营",
  "合同",
  "回款",
  "收款",
  "付款",
  "开票",
  "发票",
  "收入",
  "营收",
  "销售额",
  "成本",
  "费用",
  "利润",
  "应收",
  "应付",
  "到账",
  "支出",
  "流入",
  "流出",
  "现金",
  "银行",
  "余额",
  "销项",
  "进项",
  "税额",
  "供应商",
  "客户",
  "货币资金"
];

// MCP Client for communicating with financeqa serve
class MCPClient {
  constructor(binaryPath) {
    this.binaryPath = binaryPath;
    this.process = null;
    this.requestId = 0;
    this.pendingRequests = new Map();
    this.initialized = false;
  }

  async start() {
    if (this.process) return;

    return new Promise((resolve, reject) => {
      this.process = spawn(this.binaryPath, ["serve"], {
        stdio: ["pipe", "pipe", "pipe"],
        cwd: findFinanceQACwd(this.binaryPath),
        env: { ...process.env }
      });

      let buffer = "";
      this.process.stdout.on("data", (chunk) => {
        buffer += chunk.toString("utf8");
        const lines = buffer.split("\n");
        buffer = lines.pop(); // Keep incomplete line in buffer

        for (const line of lines) {
          if (line.trim()) {
            this.handleMessage(line.trim());
          }
        }
      });

      this.process.stderr.on("data", (chunk) => {
        console.error("[financeqa]", chunk.toString("utf8"));
      });

      this.process.on("error", (err) => {
        reject(new Error(`Failed to start financeqa: ${err.message}`));
      });

      this.process.on("close", (code) => {
        this.process = null;
        this.initialized = false;
        if (code !== 0 && code !== null) {
          console.error(`financeqa process exited with code ${code}`);
        }
      });

      // Wait a bit for process to start, then send initialize
      setTimeout(async () => {
        try {
          await this.sendRequest("initialize", {});
          this.initialized = true;
          resolve();
        } catch (err) {
          reject(err);
        }
      }, 500);
    });
  }

  stop() {
    if (this.process) {
      this.process.kill();
      this.process = null;
    }
    this.initialized = false;
  }

  handleMessage(line) {
    try {
      const msg = JSON.parse(line);
      if (msg.id !== undefined && this.pendingRequests.has(msg.id)) {
        const { resolve, reject } = this.pendingRequests.get(msg.id);
        this.pendingRequests.delete(msg.id);
        if (msg.error) {
          reject(new Error(msg.error.message || JSON.stringify(msg.error)));
        } else {
          resolve(msg.result);
        }
      }
    } catch (err) {
      console.error("[mcp] Failed to parse message:", line.slice(0, 200));
    }
  }

  sendRequest(method, params) {
    return new Promise((resolve, reject) => {
      if (!this.process) {
        reject(new Error("MCP client not started"));
        return;
      }

      this.requestId++;
      const id = this.requestId;
      const request = {
        jsonrpc: "2.0",
        id,
        method,
        params
      };

      this.pendingRequests.set(id, { resolve, reject });

      // Timeout after 60 seconds
      setTimeout(() => {
        if (this.pendingRequests.has(id)) {
          this.pendingRequests.delete(id);
          reject(new Error(`Request timeout: ${method}`));
        }
      }, 60000);

      this.process.stdin.write(JSON.stringify(request) + "\n");
    });
  }

  async callTool(name, args) {
    if (!this.initialized) {
      await this.start();
    }
    return this.sendRequest("tools/call", {
      name,
      arguments: args
    });
  }
}

class RemoteMCPClient {
  constructor({ url, token, timeoutMs = 60000 }) {
    this.url = validateMcpURL(url);
    this.token = String(token || "").trim();
    if (!this.token) {
      throw new Error("Remote FinanceQA MCP token is required");
    }
    this.timeoutMs = timeoutMs;
    this.requestId = 0;
    this.sessionId = "";
    this.initialized = false;
  }

  async start() {
    if (this.initialized) return;
    await this.sendRequest("initialize", {
      protocolVersion: "2025-03-26",
      capabilities: {},
      clientInfo: { name: "openclaw-finance", version: "1.0" }
    }, { skipStart: true });
    this.initialized = true;
  }

  async callTool(name, args) {
    if (!this.initialized) {
      await this.start();
    }
    return this.sendRequest("tools/call", {
      name,
      arguments: args || {}
    });
  }

  async sendRequest(method, params, options = {}) {
    if (!options.skipStart && !this.initialized) {
      await this.start();
    }

    this.requestId++;
    const request = {
      jsonrpc: "2.0",
      id: this.requestId,
      method,
      params
    };
    return this.postJSONRPC(request);
  }

  async postJSONRPC(request) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const headers = {
        "Authorization": `Bearer ${this.token}`,
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream"
      };
      if (this.sessionId) {
        headers["Mcp-Session-Id"] = this.sessionId;
      }

      let response;
      try {
        response = await fetch(this.url, {
          method: "POST",
          headers,
          body: JSON.stringify(request),
          signal: controller.signal
        });
      } catch (error) {
        if (error?.name === "AbortError") {
          throw new Error(`Remote FinanceQA MCP request timed out after ${this.timeoutMs}ms`);
        }
        throw new Error(`Remote FinanceQA MCP network error: ${error?.message || String(error)}`);
      }

      const nextSessionId = response.headers.get("Mcp-Session-Id");
      if (nextSessionId) {
        this.sessionId = nextSessionId;
      }
      const contentType = response.headers.get("content-type") || "";
      const rawBody = await response.text();
      if (response.status === 401 || response.status === 403) {
        throw new Error(`Remote FinanceQA MCP auth failed: HTTP ${response.status}`);
      }
      if (!response.ok) {
        throw new Error(`Remote FinanceQA MCP endpoint failed: HTTP ${response.status} ${rawBody.slice(0, 200)}`);
      }

      const payload = contentType.includes("text/event-stream")
        ? parseSsePayload(rawBody)
        : parseJSONPayload(rawBody);
      if (payload?.error) {
        throw new Error(payload.error.message || JSON.stringify(payload.error));
      }
      return payload?.result;
    } finally {
      clearTimeout(timer);
    }
  }
}

function parseJSONPayload(rawBody) {
  try {
    return JSON.parse(rawBody);
  } catch (error) {
    throw new Error(`Remote FinanceQA MCP returned invalid JSON: ${error.message}`);
  }
}

function parseSsePayload(rawBody) {
  const chunks = [];
  let current = [];
  for (const line of String(rawBody || "").split(/\r?\n/)) {
    if (line === "") {
      if (current.length) {
        chunks.push(current.join("\n"));
        current = [];
      }
      continue;
    }
    if (line.startsWith("data:")) {
      current.push(line.slice(5).trimStart());
    }
  }
  if (current.length) chunks.push(current.join("\n"));
  for (let i = chunks.length - 1; i >= 0; i--) {
    try {
      return JSON.parse(chunks[i]);
    } catch {
      continue;
    }
  }
  throw new Error("Remote FinanceQA MCP SSE response did not contain valid JSON");
}

// Global MCP client instance (lazy init)
let mcpClient = null;
let mcpClientKey = "";
let pluginRuntime = null;
const latestFinanceQuestionBySession = new Map();
const pendingFinanceFactAtomsBySession = new Map();
const pendingFinanceFactPayloadsBySession = new Map();
const financeRunKeysBySession = new Map();

async function getMCPClient() {
  const config = loadPluginConfig(pluginRuntime);
  const nextKey = config.transport === "remote"
    ? `remote\n${config.mcp_url}\n${config.mcp_token}`
    : `stdio\n${FINANCEQA_BIN}`;
  if (mcpClient && mcpClientKey === nextKey) {
    return mcpClient;
  }
  if (mcpClient?.stop) {
    mcpClient.stop();
  }
  mcpClientKey = nextKey;
  mcpClient = config.transport === "remote"
    ? new RemoteMCPClient({ url: config.mcp_url, token: config.mcp_token, timeoutMs: config.timeout_ms })
    : new MCPClient(FINANCEQA_BIN);
  await mcpClient.start();
  return mcpClient;
}

function textResult(payload) {
  return {
    content: [
      { type: "text", text: typeof payload === "string" ? payload : JSON.stringify(payload, null, 2) }
    ]
  };
}

function errorResult(error) {
  const message = error instanceof Error ? error.message : String(error);
  return textResult({ error: message });
}

function userVisibleText(value) {
  return String(value || "")
    .replace(/<relevant-memories\b[^>]*>[\s\S]*?<\/relevant-memories>\s*/gi, "")
    .replace(/(?:Conversation info|Sender) \(untrusted metadata\):\s*```(?:json)?[\s\S]*?```\s*/gi, "")
    .replace(/^\s*\[[^\]\n]*(?:UTC|GMT[^\]\n]*)\]\s*/i, "")
    .replace(/\[[^\]\n]*GMT[^\]\n]*\]\s*/g, "")
    .trim();
}

function financeOriginalQuestionFromWrapper(value) {
  const text = userVisibleText(value);
  if (!text) return "";
  const markers = ["[用户原问题]", "【用户原问题】", "用户原问题：", "用户原问题:"];
  let bestIndex = -1;
  let bestMarker = "";
  for (const marker of markers) {
    const index = text.lastIndexOf(marker);
    if (index > bestIndex) {
      bestIndex = index;
      bestMarker = marker;
    }
  }
  if (bestIndex < 0) return "";
  const candidate = userVisibleText(text.slice(bestIndex + bestMarker.length));
  return candidate || "";
}

function financeQuestionText(value) {
  return financeOriginalQuestionFromWrapper(value) || userVisibleText(value);
}

function messageContentText(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((item) => {
        if (typeof item === "string") return item;
        if (item && typeof item === "object") return item.text || item.content || "";
        return "";
      })
      .filter(Boolean)
      .join("\n");
  }
  if (content && typeof content === "object") return content.text || content.content || "";
  return "";
}

function isFinanceQuestion(rawText) {
  const text = financeQuestionText(rawText);
  if (!text || text.startsWith("/")) return false;
  if (/有限公司/.test(text) && /(20\d{2}年|\d{1,2}月|Q[1-4]|季度)/i.test(text)) return true;
  if (/数据(出来|有了|有没有|情况|多少)/.test(text)) return true;
  return FINANCE_KEYWORDS.some((keyword) => text.includes(keyword));
}

function latestFinanceQuestionFromMessages(messages, excludeText = "") {
  if (!Array.isArray(messages)) return "";
  const excluded = financeQuestionText(excludeText);
  for (let i = messages.length - 1; i >= 0 && i >= messages.length - 12; i--) {
    const entry = messages[i];
    const message = entry?.message && typeof entry.message === "object" ? entry.message : entry;
    if (message?.role !== "user") continue;
    const text = financeQuestionText(messageContentText(message.content));
    if (excluded && text === excluded) continue;
    if (isFinanceQuestion(text)) return text;
  }
  return "";
}

function latestUserTextFromMessages(messages) {
  if (!Array.isArray(messages)) return "";
  for (let i = messages.length - 1; i >= 0 && i >= messages.length - 6; i--) {
    const entry = messages[i];
    const message = entry?.message && typeof entry.message === "object" ? entry.message : entry;
    if (message?.role !== "user") continue;
    return financeQuestionText(messageContentText(message.content));
  }
  return "";
}

function isRetryOrContinuation(rawText) {
  return /(continue where you left off|previous model attempt failed|timed out|继续|接着)/i.test(String(rawText || ""));
}

function hasExplicitFinancePeriod(rawText) {
  const text = financeQuestionText(rawText);
  return /(20\d{2}年|\d{2}年|今年|本年|去年|上个月|下个月|本月|这个月|当月|[0-1]?\d月|[一二三四五六七八九十两]{1,3}月|Q\s*[1-4]|季度|全年|年度|上半年|下半年)/i.test(text);
}

function isContextDependentFinanceFollowup(rawText) {
  const text = financeQuestionText(rawText);
  if (!isFinanceQuestion(text) || hasExplicitFinancePeriod(text)) return false;
  return /^(那|其中|含|包括|包含|加上|还有)/.test(text) || /(含未开票|未开票未付款|未开票未回款)/.test(text);
}

function contextualFinanceQuestion(currentQuestion, messages) {
  const current = financeQuestionText(currentQuestion);
  if (!isContextDependentFinanceFollowup(current)) return current;
  const previous = latestFinanceQuestionFromMessages(messages, current);
  if (!previous) return current;
  return `${previous}；${current}`;
}

function financeQuestionForPromptEvent(event) {
  const prompt = financeQuestionText(event?.prompt || "");
  const latestUserText = latestUserTextFromMessages(event?.messages);
  if (isRetryOrContinuation(prompt) || isRetryOrContinuation(latestUserText)) {
    return latestFinanceQuestionFromMessages(event?.messages);
  }
  if (isFinanceQuestion(prompt)) return contextualFinanceQuestion(prompt, event?.messages);
  if (isFinanceQuestion(latestUserText)) return contextualFinanceQuestion(latestUserText, event?.messages);
  return "";
}

function requiresObservableFinanceToolCallText(rawText) {
  const text = userVisibleText(rawText);
  if (!text || !/finance-query/.test(text)) return false;
  return /(巡检要求|只读巡检请求|回答前必须先调用|必须先调用\s*`?finance-query`?|required tool|require-tool)/i.test(text);
}

function eventRequiresObservableFinanceToolCall(event) {
  if (requiresObservableFinanceToolCallText(event?.prompt || "")) return true;
  if (!Array.isArray(event?.messages)) return false;
  for (let i = event.messages.length - 1; i >= 0 && i >= event.messages.length - 6; i--) {
    const entry = event.messages[i];
    const message = entry?.message && typeof entry.message === "object" ? entry.message : entry;
    if (message?.role !== "user") continue;
    if (requiresObservableFinanceToolCallText(messageContentText(message.content))) return true;
  }
  return false;
}

function observableFinanceToolCallContext() {
  return [
    "巡检强制工具调用：本轮没有预置 finance-query 结果。",
    "回答前必须实际调用 OpenClaw 工具 `finance-query` 获取最新事实；禁止根据本提示、记忆、历史会话、缓存摘要或旧工具结果直接作答。",
    "实际调用后，金额、期间、口径、来源和来源更新时间必须来自该次 finance-query 结果。"
  ].join("\n");
}

function mustCallFinanceQuerySystemContext(latestQuestion, currentFacts) {
  const lines = [
    "财务问答系统规则：",
    latestQuestion ? `最新财务问题：${latestQuestion}` : "",
    "核心边界：FinanceQA 决定事实，OpenClaw 决定表达，bridge 负责校验表达没有改坏事实。",
    "若本次结果含 finance_facts 结构化事实包，金额、期间、口径、来源、来源更新时间必须来自 finance_facts；final_answer 只作为兼容参考，不是事实优先级最高的来源。",
    "不得把 finance_facts.resolved_period 改成其他月份，不得把 finance_facts.basis 改成其他口径，不得自行重算 finance_facts.metrics。",
    "回答财务、经营、合同、回款、开票、收入、成本、利润、现金、银行、税务、应收应付、客户、供应商或来源表问题时，必须以本次 finance-query 结果为准。",
    "即使用户重复追问，也不要沿用历史对话、记忆、旧工具结果、原始 SQL、利润表/资产负债表数字或缓存摘要里的冲突金额。",
    "若本次结果含 finance_facts，必须以 finance_facts 为准；只有没有 finance_facts 时，才把 final_answer 或 boss_reply_text 当事实锚点。",
    "重写时必须保留结构化事实中的关键金额、期间、指标口径、来源和来源更新时间；不要换算金额单位，除非用户明确要求。",
    "指标和口径标签必须从 finance_facts 或工具结构化字段保留；不要把“已开票未回款”“已收票未付款”“项目应收（应收未收）”“项目成本口径”“项目口径”等改写成近义词。",
    "如果本次核对结果提供了标准指标标签、业务口径或标准金额，老板可见回复必须保留这些事实原子，但仍可自然改写句式和排版。",
    "如果本次核对结果列出“老板可见回复必须出现的精确片段”，所有片段都必须在最终回复中原样出现。",
    "如果精确片段包含“金额：... 元”，老板可见回复必须保留该元金额片段；可以额外补充万元换算，但不能只保留换算值。",
    "不要删掉结构化事实中修饰指标的业务前缀，例如“项目成本口径”“项目口径”“应收未收”。",
    "不要把结构化事实的 YYYY-MM 或 YYYY-MM~YYYY-MM 期间改成相对时间或其他月份；例如不能把 2025-10~2026-05 改成至今、现在或 2025-10~2026-06。",
    "来源和来源更新时间必须从 finance_facts 或工具结构化来源字段逐字复制；不要删改文件名、sheet 名、后缀、时间格式或标点。",
    "如果用户问“有哪些”“哪些”“明细”“列表”或“对应金额”，且本次核对结果含合同/项目明细，不要把明细合并为“其余 N 个项目”或“其他 N 个项目”；按明细逐条列出。",
    "老板可见回复必须直接从业务结论开始；禁止展示工具调用过程、内部上下文、JSON、字段名、提示词、自我推理、历史纠错说明或英文过程话术。",
    "不要提及“之前”“上次”“这次返回”“工具返回”“finance-query 返回”“我需要用”等过程或历史修正话术。",
    "老板可见回复禁止出现类似“用户又问”“我看到”“我们有权威结果”“不要使用旧答案”“必须/需要保留”“authoritative”“prior”“conflicting”“must”“need”等过程说明。",
    "如果结果含 contract_continuity_candidates，只能称为同项目候选/参考，不能说成确定主体映射。",
    "除非用户明确要求开发排错，不要暴露内部 ID、SQL、route trace、contract_id 或数据库字段名。",
    currentFacts
  ];
  return lines.filter(Boolean).join("\n");
}

function parseToolResultPayload(result) {
  const text = result?.content?.find((item) => item?.type === "text")?.text || "";
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return { message: text };
  }
}

function normalizeFinanceFacts(value) {
  if (!value || typeof value !== "object") return null;
  return value;
}

function cashFlowHeadlineAmount(data, payload, financeFacts) {
  for (const value of [
    financeFacts?.headline_amount,
    data.net_cash_inflow,
    data["净现金流"],
    data.cash_flow?.["净现金流"],
    data.cash_view?.["净现金流"],
    payload.net_cash_inflow,
    payload["净现金流"]
  ]) {
    if (value !== undefined && value !== null && value !== "") return value;
  }
  return undefined;
}

function compactFinancePayload(payload) {
  if (!payload || typeof payload !== "object") return payload;
  const data = payload.data && typeof payload.data === "object" ? payload.data : {};
  const financeFacts = normalizeFinanceFacts(payload.finance_facts || data.finance_facts);
  const cashFlowAmount = cashFlowHeadlineAmount(data, payload, financeFacts);
  const metricLabel = data.metric_label || payload.metric_label || financeFacts?.headline_metric || (cashFlowAmount !== undefined ? "净现金流" : "");
  return {
    error: payload.error,
    success: payload.success,
    finance_facts: financeFacts,
    final_answer: financeFacts ? "" : (payload.final_answer || payload.boss_reply_text || payload.message),
    metric: financeFacts?.headline_metric || data.metric || payload.metric,
    metric_label: metricLabel,
    business_basis: financeFacts?.basis || data.business_basis || payload.business_basis,
    total: financeFacts?.headline_amount ?? data.total ?? payload.total ?? cashFlowAmount,
    source_note: financeFacts?.source_note || data.source_note || payload.source_note,
    source_update_note: financeFacts?.source_update_note || data.source_update_note || payload.source_update_note,
    period: financeFacts?.resolved_period || data.period || payload.period,
    requested_period: financeFacts?.requested_period || data.requested_period || payload.requested_period,
    source_priority: data.source_priority || payload.source_priority,
    requested_metrics: data.requested_metrics || payload.requested_metrics,
    role: data.role || payload.role,
    account_view: data.account_view || payload.account_view,
    cash_view: data.cash_view || payload.cash_view,
    contract_summary: data.contract_summary || payload.contract_summary,
    customer_summary: data.customer_summary || payload.customer_summary,
    supplier_summary: data.supplier_summary || payload.supplier_summary,
    tax_summary: data.tax_summary || payload.tax_summary,
    source_documents: financeFacts?.source_files || data.source_documents || payload.source_documents,
    source_tables: financeFacts?.source_tables || data.source_tables || payload.source_tables,
    metrics: financeFacts?.metrics || data.metrics || payload.metrics || data.cash_flow || data.cash_view,
    warnings: financeFacts?.warnings || data.warnings || payload.warnings,
    explanation_hints: financeFacts?.explanation_hints || data.explanation_hints || payload.explanation_hints,
    items: data.items || payload.items,
    detail_items: data.detail_items || payload.detail_items,
    source_cell_notes: data.source_cell_notes || payload.source_cell_notes,
    remarks: data.remarks || payload.remarks,
    contract_continuity_candidates: data.contract_continuity_candidates || payload.contract_continuity_candidates
  };
}

function compactFinanceRows(rows, maxRows = 20) {
  if (!Array.isArray(rows)) return [];
  return rows.slice(0, maxRows).map((row) => {
    if (!row || typeof row !== "object") return row;
    const out = {};
    for (const [key, label] of [
      ["supplier_name", "主体"],
      ["customer_name", "主体"],
      ["entity", "主体"],
      ["period_label", "期间"],
      ["contract_content", "合同/项目"],
      ["settlement_amount", "结算金额"],
      ["received_amount", "已回款"],
      ["paid_amount", "已付款"],
      ["invoice_amount", "开票金额"],
      ["open_amount", "未结金额"],
      ["unpaid_amount", "未付款"],
      ["unreceived_amount", "未回款"],
      ["coverage_status", "覆盖状态"]
    ]) {
      if (row[key] !== undefined && row[key] !== null && row[key] !== "") {
        out[label] = row[key];
      }
    }
    return Object.keys(out).length ? out : row;
  });
}

function contractSummaryDetailRows(contractSummary) {
  if (!contractSummary || typeof contractSummary !== "object") return [];
  const rows = [];
  for (const key of [
    "invoice_unpaid_items",
    "payable_open_items",
    "invoice_open_items",
    "receivable_open_items",
    "cost_items",
    "revenue_items"
  ]) {
    if (Array.isArray(contractSummary[key])) rows.push(...contractSummary[key]);
  }
  return rows;
}

function requiredBossVisibleAtoms(payload) {
  const atoms = [];
  const compact = compactFinancePayload(payload);
  const required = compact?.finance_facts?.required_atoms;
  if (Array.isArray(required)) {
    for (const atom of required) {
      const line = String(atom || "").trim();
      if (line && !atoms.includes(line)) atoms.push(line);
    }
  }
  for (const atom of financeFactAtomsFromPayload(payload)) {
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    if (line && !atoms.includes(line)) atoms.push(line);
  }
  return atoms;
}

function normalizedSourceUpdateNote(value) {
  const note = String(value || "").trim();
  if (!note) return "";
  return note.startsWith("来源更新时间") ? note : `来源更新时间：${note}`;
}

function sourceAtomsFromText(text) {
  const rawText = String(text || "");
  const atoms = [];
  const updateMatch = rawText.match(/来源更新时间[：:]\s*\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}/);
  const sourceMatch = rawText.match(/来源[：:][^\n]*(?:\.xlsx|\.xls|\.csv)[^\n]*?(?=\s*来源更新时间[：:]|\n|$)/i);
  for (const atom of [sourceMatch?.[0], updateMatch?.[0]]) {
    const value = String(atom || "").trim();
    if (value && !atoms.includes(value)) atoms.push(value);
  }
  return atoms;
}

function sourceAtomsFromPayload(payload) {
  const compact = compactFinancePayload(payload);
  if (!compact || typeof compact !== "object") return [];
  const atoms = [];
  const sourceNote = String(compact.source_note || "").trim();
  const sourceUpdateNote = normalizedSourceUpdateNote(compact.source_update_note);
  for (const atom of [sourceNote, sourceUpdateNote, ...sourceAtomsFromText(compact.final_answer)]) {
    if (atom && !atoms.includes(atom)) atoms.push(atom);
  }
  return atoms;
}

function pushFinanceFactAtom(atoms, value, line) {
  const key = String(value || "").trim();
  const text = String(line || key).trim();
  if (!key || !text) return;
  if (atoms.some((atom) => atom.value === key || atom.line === text)) return;
  atoms.push({ value: key, line: text });
}

function financeFactAtomValueFromLine(line) {
  const text = String(line || "").trim();
  let match = text.match(/^期间[：:]\s*(.+)$/);
  if (match) return match[1].trim();
  match = text.match(/^口径[：:]\s*(.+)$/);
  if (match) return match[1].trim();
  match = text.match(/^金额[：:]\s*([0-9][0-9,]*(?:\.\d+)?)/);
  if (match) return match[1].replace(/,/g, "").trim();
  return text;
}

function financeFactAtomsFromPayload(payload) {
  const compact = compactFinancePayload(payload);
  if (!compact || typeof compact !== "object") return [];
  const atoms = [];
  const required = compact.finance_facts?.required_atoms;
  if (Array.isArray(required)) {
    for (const line of required) {
      const text = String(line || "").trim();
      pushFinanceFactAtom(atoms, financeFactAtomValueFromLine(text), text);
    }
  }
  pushFinanceFactAtom(atoms, compact.period, compact.period ? `期间：${compact.period}` : "");
  pushFinanceFactAtom(atoms, compact.metric_label, compact.metric_label ? `口径：${compact.metric_label}` : "");
  if (compact.business_basis) pushFinanceFactAtom(atoms, compact.business_basis, `口径：${compact.business_basis}`);
  if (compact.total !== undefined && compact.total !== null && compact.total !== "") {
    pushFinanceFactAtom(atoms, compact.total, `金额：${compact.total} 元`);
  }
  for (const atom of sourceAtomsFromPayload(compact)) {
    pushFinanceFactAtom(atoms, atom, atom);
  }
  return atoms;
}

function financeFactAtomsFromToolResult(message) {
  if (!message || message.role !== "toolResult") return [];
  const messageToolName = message.toolName || message.name;
  if (messageToolName && messageToolName !== "finance-query") return [];
  const text = message.content?.find?.((item) => item?.type === "text")?.text || "";
  if (!text) return [];
  const payload = parseToolResultPayload({ content: [{ type: "text", text }] });
  if (!payload || typeof payload !== "object") return [];
  const payloadToolName = payload.bridge_meta?.tool_name || payload.tool_name;
  if (messageToolName !== "finance-query" && payloadToolName !== "finance-query") return [];
  return financeFactAtomsFromPayload(payload);
}

function firstNonEmptyString(...values) {
  for (const value of values) {
    const text = String(value || "").trim();
    if (text) return text;
  }
  return "";
}

function financeExecutionContext(toolCtx, runtimeCtx) {
  const factory = isRecord(toolCtx) ? toolCtx : {};
  const runtime = isRecord(runtimeCtx) && !isAbortSignal(runtimeCtx) ? runtimeCtx : {};
  return { ...factory, ...runtime };
}

function financeScope(event, ctx) {
  const runId = firstNonEmptyString(ctx?.runId, event?.runId, event?.message?.runId);
  const sessionKey = firstNonEmptyString(ctx?.sessionKey, event?.sessionKey, event?.message?.sessionKey);
  return {
    runId,
    sessionKey,
    runKey: runId ? `run:${runId}` : "",
    sessionStateKey: sessionKey ? `session:${sessionKey}` : "__default__",
    sessionIndexKey: sessionKey || "__default__",
    primaryKey: runId ? `run:${runId}` : (sessionKey ? `session:${sessionKey}` : "__default__")
  };
}

function registerFinanceRunForSession(scope) {
  if (!scope?.runKey) return;
  const sessionKey = scope.sessionIndexKey || "__default__";
  const runKeys = financeRunKeysBySession.get(sessionKey) || new Set();
  runKeys.add(scope.runKey);
  financeRunKeysBySession.set(sessionKey, runKeys);
}

function unregisterFinanceRunForSession(scope) {
  if (!scope?.runKey) return;
  const sessionKey = scope.sessionIndexKey || "__default__";
  const runKeys = financeRunKeysBySession.get(sessionKey);
  if (!runKeys) return;
  runKeys.delete(scope.runKey);
  if (runKeys.size) {
    financeRunKeysBySession.set(sessionKey, runKeys);
  } else {
    financeRunKeysBySession.delete(sessionKey);
  }
}

function activeFinanceRunKeysForSession(scope) {
  const runKeys = financeRunKeysBySession.get(scope?.sessionIndexKey || "__default__");
  if (!runKeys?.size) return [];
  return [...runKeys].filter((key) => (
    pendingFinanceFactAtomsBySession.has(key) ||
    pendingFinanceFactPayloadsBySession.has(key) ||
    latestFinanceQuestionBySession.has(key)
  ));
}

function rememberPendingFinanceFacts(event, ctx, atoms, payload) {
  const scope = financeScope(event, ctx);
  if (!Array.isArray(atoms) || !atoms.length) {
    clearPendingFinanceFacts(event, ctx);
    return;
  }
  pendingFinanceFactAtomsBySession.set(scope.primaryKey, atoms);
  pendingFinanceFactPayloadsBySession.set(scope.primaryKey, payload);
  if (scope.runKey) {
    registerFinanceRunForSession(scope);
    pendingFinanceFactAtomsBySession.delete(scope.sessionStateKey);
    pendingFinanceFactPayloadsBySession.delete(scope.sessionStateKey);
  }
}

function clearPendingFinanceFacts(event, ctx) {
  const scope = financeScope(event, ctx);
  pendingFinanceFactAtomsBySession.delete(scope.primaryKey);
  pendingFinanceFactPayloadsBySession.delete(scope.primaryKey);
  if (scope.runKey && !latestFinanceQuestionBySession.has(scope.primaryKey)) {
    unregisterFinanceRunForSession(scope);
  }
}

function pendingFinanceFactsForScope(event, ctx) {
  const scope = financeScope(event, ctx);
  const directAtoms = pendingFinanceFactAtomsBySession.get(scope.primaryKey);
  if (directAtoms?.length) {
    return {
      atoms: directAtoms,
      payload: pendingFinanceFactPayloadsBySession.get(scope.primaryKey),
      key: scope.primaryKey,
      scope
    };
  }
  if (!scope.runKey) {
    const runKeys = activeFinanceRunKeysForSession(scope);
    if (runKeys.length === 1) {
      const key = runKeys[0];
      return {
        atoms: pendingFinanceFactAtomsBySession.get(key),
        payload: pendingFinanceFactPayloadsBySession.get(key),
        key,
        scope: { ...scope, runKey: key }
      };
    }
    if (runKeys.length > 1) return null;
  }
  const sessionAtoms = pendingFinanceFactAtomsBySession.get(scope.sessionStateKey);
  if (sessionAtoms?.length) {
    return {
      atoms: sessionAtoms,
      payload: pendingFinanceFactPayloadsBySession.get(scope.sessionStateKey),
      key: scope.sessionStateKey,
      scope
    };
  }
  return null;
}

function setLatestFinanceQuestionForToolScope(event, ctx, latestQuestion) {
  const scope = financeScope(event, ctx);
  const question = String(latestQuestion || "").trim();
  if (question) {
    latestFinanceQuestionBySession.set(scope.primaryKey, question);
    if (scope.runKey) {
      registerFinanceRunForSession(scope);
      latestFinanceQuestionBySession.delete(scope.sessionStateKey);
    }
  } else {
    latestFinanceQuestionBySession.delete(scope.primaryKey);
    if (scope.runKey) unregisterFinanceRunForSession(scope);
  }
}

function takeLatestFinanceQuestionForTool(ctx) {
  const scope = financeScope(undefined, ctx);
  if (scope.runKey) {
    if (!latestFinanceQuestionBySession.has(scope.runKey)) return "";
    const question = latestFinanceQuestionBySession.get(scope.runKey) || "";
    latestFinanceQuestionBySession.delete(scope.runKey);
    return question;
  }
  const runKeys = activeFinanceRunKeysForSession(scope);
  if (runKeys.length > 1) return "";
  if (runKeys.length === 1) {
    const key = runKeys[0];
    if (latestFinanceQuestionBySession.has(key)) {
      const question = latestFinanceQuestionBySession.get(key) || "";
      latestFinanceQuestionBySession.delete(key);
      return question;
    }
    return "";
  }
  if (latestFinanceQuestionBySession.has(scope.sessionStateKey)) {
    const question = latestFinanceQuestionBySession.get(scope.sessionStateKey) || "";
    latestFinanceQuestionBySession.delete(scope.sessionStateKey);
    return question;
  }
  const pending = [...latestFinanceQuestionBySession.entries()];
  if (pending.length !== 1 || !pending[0][0].startsWith("session:")) return "";
  const [key, question] = pending[0];
  latestFinanceQuestionBySession.delete(key);
  return question || "";
}

function amountAtomValue(atom) {
  const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
  const value = String(typeof atom === "string" ? atom : atom?.value ?? "").trim();
  const match = line.match(/^金额[：:]\s*(-?[0-9][0-9,]*(?:\.\d+)?)/);
  return String(match?.[1] || value).replace(/,/g, "").trim();
}

function hasValidGroupedNumber(raw) {
  const [integerPart] = String(raw || "").split(".");
  if (!integerPart?.includes(",")) return true;
  const groups = integerPart.split(",");
  return /^[0-9]{1,3}$/.test(groups[0] || "") && groups.slice(1).every((group) => /^[0-9]{3}$/.test(group));
}

function replaceMalformedAmountAtom(text, atom) {
  const expectedAmount = amountAtomValue(atom);
  if (!expectedAmount || !/^-?[0-9]+(?:\.\d+)?$/.test(expectedAmount)) return text;
  if (text.includes(expectedAmount)) return text;
  const lines = String(text || "").split("\n");
  const moneyPattern = /-?[0-9][0-9,]*(?:\.\d+)?(?=\s*(?:万元|万|元))/g;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] || "";
    if (!/(合计|总额|金额|项目|应收|应付|未回款|未付款)/.test(line)) continue;
    let replaced = false;
    const nextLine = line.replace(moneyPattern, (raw) => {
      if (replaced || hasValidGroupedNumber(raw)) return raw;
      replaced = true;
      return expectedAmount;
    });
    if (!replaced) continue;
    lines[i] = nextLine;
    return lines.join("\n");
  }
  return text;
}

function replaceMalformedFinanceAmountAtoms(text, atoms) {
  let nextText = String(text || "");
  for (const atom of atoms || []) {
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    if (!line.startsWith("金额：") && !line.startsWith("金额:")) continue;
    nextText = replaceMalformedAmountAtom(nextText, atom);
  }
  return nextText;
}

function formatFinanceMoney(value) {
  const num = Number(value);
  if (!Number.isFinite(num)) return String(value ?? "").trim();
  return num.toFixed(2);
}

function sameFinanceAmount(a, b) {
  const left = Number(String(a || "").replace(/,/g, ""));
  const right = Number(String(b || "").replace(/,/g, ""));
  return Number.isFinite(left) && Number.isFinite(right) && Math.abs(left - right) < 0.005;
}

function displayedFinanceAmountInYuan(rawNumber, rawUnit) {
  const amount = Number(String(rawNumber || "").replace(/,/g, ""));
  if (!Number.isFinite(amount)) return NaN;
  const unit = String(rawUnit || "").trim();
  return unit.startsWith("万") ? amount * 10000 : amount;
}

function sourceUpdateAtomLine(atoms) {
  for (const atom of atoms || []) {
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    if (line.startsWith("来源更新时间：") || line.startsWith("来源更新时间:")) return line;
  }
  return "";
}

function sourceUpdateTimestamp(value) {
  const match = String(value || "").match(/\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}/);
  return match?.[0] || "";
}

function replaceConflictingSourceUpdate(text, atoms) {
  const expectedLine = sourceUpdateAtomLine(atoms);
  const expectedTimestamp = sourceUpdateTimestamp(expectedLine);
  if (!expectedLine || !expectedTimestamp) return text;
  return String(text || "").replace(/(?:来源)?更新时间[：:]\s*\d{4}-\d{2}-\d{2}(?:\s+\d{2}:\d{2}:\d{2})?/g, (raw) => {
    const current = String(raw || "").trim();
    if (current === expectedLine && sourceUpdateTimestamp(current) === expectedTimestamp) return raw;
    return expectedLine;
  });
}

function hasConflictingFinanceFactAtoms(text, atoms) {
  const rawText = String(text || "");
  const expectedAmount = amountAtomValue((atoms || []).find((atom) => {
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    return line.startsWith("金额：") || line.startsWith("金额:");
  }));
  if (expectedAmount) {
    for (const match of rawText.matchAll(/金额[：:]\s*([0-9][0-9,]*(?:\.\d+)?)/g)) {
      if (!sameFinanceAmount(match[1], expectedAmount)) return true;
    }
  }
  const expectedSourceUpdate = sourceUpdateAtomLine(atoms);
  if (expectedSourceUpdate) {
    for (const match of rawText.matchAll(/(?:来源)?更新时间[：:]\s*\d{4}-\d{2}-\d{2}(?:\s+\d{2}:\d{2}:\d{2})?/g)) {
      if (match[0].trim() !== expectedSourceUpdate) return true;
    }
  }
  return false;
}

function hasSuccessfulFinanceFactPayload(payload) {
  const compact = compactFinancePayload(payload);
  if (!compact || typeof compact !== "object") return false;
  if (payload && typeof payload === "object" && payload.success === false) return false;
  if (compact.total !== undefined && compact.total !== null && compact.total !== "") {
    const total = Number(String(compact.total).replace(/,/g, ""));
    if (Number.isFinite(total)) return true;
  }
  const metrics = compact.metrics && typeof compact.metrics === "object" ? compact.metrics : null;
  if (metrics) {
    for (const value of Object.values(metrics)) {
      const amount = Number(String(value).replace(/,/g, ""));
      if (Number.isFinite(amount)) return true;
    }
  }
  return false;
}

function hasFinanceFactContradictingDenial(text, payload) {
  if (!hasSuccessfulFinanceFactPayload(payload)) return false;
  const rawText = String(text || "");
  if (!rawText.trim()) return false;
  return [
    /返回不了(?:具体)?金额/,
    /没有识别到(?:对应的)?(?:合同|项目|客户|供应商|主体)/,
    /未识别到(?:对应的)?(?:合同|项目|客户|供应商|主体)/,
    /无法(?:直接)?(?:查询|回答|返回)/,
    /不能(?:直接)?回答/,
    /系统(?:暂时|目前)?不支持/,
    /需要(?:先)?(?:导入|同步).*?(?:余额表|收入表|成本表|序时账|流水|数据)/,
    /(?:期间|当前|该月|这个月|6月|六月).{0,16}没有匹配(?:到)?(?:记录|数据)/,
    /(?:数据|系统).{0,12}(?:目前|当前)?还没有/,
    /查不到.{0,16}(?:记录|数据|金额|结果)/
  ].some((pattern) => pattern.test(rawText));
}

function financePeriodTextVariants(period) {
  const raw = String(period || "").trim();
  if (!raw) return [];
  const variants = new Set([raw]);
  let match = raw.match(/^(20\d{2})-(\d{2})$/);
  if (match) {
    variants.add(`${match[1]}年${Number(match[2])}月`);
    return [...variants];
  }
  match = raw.match(/^(20\d{2})-(\d{2})~(20\d{2})-(\d{2})$/);
  if (match) {
    variants.add(`${match[1]}年${Number(match[2])}月~${match[3]}年${Number(match[4])}月`);
    variants.add(`${match[1]}年${Number(match[2])}月至${match[3]}年${Number(match[4])}月`);
  }
  return [...variants];
}

function financeMonthKeysFromText(text) {
  const keys = new Set();
  for (const match of String(text || "").matchAll(/(20\d{2})-(\d{1,2})/g)) {
    keys.add(`${match[1]}-${String(Number(match[2])).padStart(2, "0")}`);
  }
  for (const match of String(text || "").matchAll(/(20\d{2})年\s*(\d{1,2})月/g)) {
    keys.add(`${match[1]}-${String(Number(match[2])).padStart(2, "0")}`);
  }
  return keys;
}

function hasFinanceFactPeriodConflict(text, payload) {
  const compact = compactFinancePayload(payload);
  const expectedPeriod = String(compact?.period || "").trim();
  if (!expectedPeriod) return false;
  const rawText = String(text || "")
    .split("\n")
    .filter((line) => !/来源|更新时间/.test(line))
    .join("\n");
  if (!rawText.trim()) return false;
  if (financePeriodTextVariants(expectedPeriod).some((variant) => rawText.includes(variant))) return false;
  const mentioned = financeMonthKeysFromText(rawText);
  if (!mentioned.size) return false;
  const expectedMonths = new Set(String(expectedPeriod).split("~"));
  for (const month of mentioned) {
    if (!expectedMonths.has(month)) return true;
  }
  return false;
}

function metricNameForConflictScan(metric) {
  return String(metric || "")
    .replace(/[（(].*?[）)]/g, "")
    .replace(/名义/g, "")
    .trim();
}

function metricAliasesForConflictScan(metric) {
  const name = metricNameForConflictScan(metric);
  const aliases = new Set();
  if (name) aliases.add(name);
  const isDifferenceMetric = /差异|差额|相差|账上净利润-银行净流入/.test(name);
  if (isDifferenceMetric) {
    ["差异金额", "名义差额", "差异", "差额", "相差", "差了"].forEach((alias) => aliases.add(alias));
  }
  if (!isDifferenceMetric && name.includes("银行净流入")) {
    aliases.add("净流入");
  }
  if (!isDifferenceMetric && name.includes("账上净利润")) {
    aliases.add("账上利润");
    aliases.add("净利润");
  }
  return [...aliases].filter((alias) => alias.length >= 2);
}

function financeMetricAmountsOnLine(line, aliases) {
  const amounts = [];
  const prefixPattern = /^(?:\s|[：:]|为|是|约|共|合计|人民币|\*{1,2}){0,16}(-?[0-9][0-9,]*(?:\.\d+)?)\s*(万元|万|元)/;
  for (const alias of aliases) {
    let fromIndex = 0;
    while (fromIndex < line.length) {
      const aliasIndex = line.indexOf(alias, fromIndex);
      if (aliasIndex < 0) break;
      const match = line.slice(aliasIndex + alias.length).match(prefixPattern);
      if (match) amounts.push(displayedFinanceAmountInYuan(match[1], match[2]));
      fromIndex = aliasIndex + alias.length;
    }
  }
  return amounts;
}

function hasFinanceFactMetricAmountConflict(text, payload) {
  const compact = compactFinancePayload(payload);
  const metrics = compact?.metrics && typeof compact.metrics === "object" ? compact.metrics : {};
  const rawText = String(text || "");
  if (!rawText.trim()) return false;
  for (const [rawMetric, rawExpected] of Object.entries(metrics)) {
    const aliases = metricAliasesForConflictScan(rawMetric);
    if (!aliases.length) continue;
    const expected = Number(String(rawExpected).replace(/,/g, ""));
    if (!Number.isFinite(expected)) continue;
    for (const line of rawText.split("\n")) {
      if (!aliases.some((alias) => line.includes(alias))) continue;
      const amounts = financeMetricAmountsOnLine(line, aliases);
      if (amounts.some((amount) => !sameFinanceAmount(amount, expected))) return true;
    }
  }
  return false;
}

const FINANCE_BASIS_CONFLICT_GROUPS = {
  project: ["项目口径", "项目经营口径", "项目成本口径", "项目结算", "项目营收", "项目收入", "项目应收", "项目应付", "合同口径"],
  journal: ["序时账", "账上", "会计账", "财务账", "账上净利润", "账上利润"],
  bank: ["银行流水", "银行卡", "银行净流入", "净现金流", "现金流", "现金口径"],
  balance: ["官方余额表", "余额表", "科目余额", "资产负债表", "应收账款", "应付账款", "其他应付款"]
};

function financeBasisGroupsFromText(...values) {
  const text = values.map((value) => String(value || "")).join("\n");
  const groups = new Set();
  for (const [group, aliases] of Object.entries(FINANCE_BASIS_CONFLICT_GROUPS)) {
    if (aliases.some((alias) => text.includes(alias))) groups.add(group);
  }
  if (/fin_(?:fund_income|cost_settlements|contracts)|contract_/i.test(text)) groups.add("project");
  if (/fin_journal|journal/i.test(text)) groups.add("journal");
  if (/bank_statement|fin_bank|bank/i.test(text)) groups.add("bank");
  if (/balance|fin_balance|arap/i.test(text)) groups.add("balance");
  return groups;
}

function hasFinanceFactBasisConflict(text, payload) {
  const compact = compactFinancePayload(payload);
  if (!compact || typeof compact !== "object") return false;
  const expectedGroups = financeBasisGroupsFromText(
    compact.business_basis,
    compact.metric_label,
    compact.metric,
    ...(Array.isArray(compact.source_tables) ? compact.source_tables : [])
  );
  if (!expectedGroups.size) return false;
  const answerGroups = financeBasisGroupsFromText(text);
  if (!answerGroups.size) return false;
  for (const group of answerGroups) {
    if (!expectedGroups.has(group)) return true;
  }
  return false;
}

function sourceFileNamesFromText(text) {
  const names = [];
  for (const match of String(text || "").matchAll(/《([^》]+?\.(?:xlsx|xls|csv))[^》]*》/gi)) {
    const name = String(match[1] || "").trim();
    if (name && !names.includes(name)) names.push(name);
  }
  return names;
}

function hasFinanceFactSourceConflict(text, payload) {
  const compact = compactFinancePayload(payload);
  if (!compact?.source_note && !compact?.source_documents?.length) return false;
  const rawText = String(text || "");
  if (/来源[：:]\s*(?:未记录|无|暂无|没有)/.test(rawText)) return true;
  const expectedSources = sourceFileNamesFromText([
    compact.source_note,
    ...(Array.isArray(compact.source_documents) ? compact.source_documents : [])
  ].join("\n"));
  const actualSources = sourceFileNamesFromText(rawText);
  if (!expectedSources.length || !actualSources.length) return false;
  return !actualSources.some((source) => expectedSources.includes(source));
}

function hasFinanceFactConflict(text, atoms, payload) {
  return hasFinanceFactContradictingDenial(text, payload) ||
    hasConflictingFinanceFactAtoms(text, atoms) ||
    hasFinanceFactPeriodConflict(text, payload) ||
    hasFinanceFactMetricAmountConflict(text, payload) ||
    hasFinanceFactBasisConflict(text, payload) ||
    hasFinanceFactSourceConflict(text, payload);
}

function financeDetailLine(row) {
  if (!row || typeof row !== "object") return "";
  const entity = String(row.supplier_name || row.customer_name || row.entity || "").trim();
  const content = String(row.contract_content || row.project_name || "").trim();
  const label = [entity, content].filter(Boolean).join("-");
  if (!label) return "";
  if (row.unpaid_amount !== undefined && row.unpaid_amount !== null) {
    const parts = [];
    if (row.settlement_amount !== undefined && row.settlement_amount !== null) parts.push(`项目成本 ${formatFinanceMoney(row.settlement_amount)} 元`);
    if (row.paid_amount !== undefined && row.paid_amount !== null) parts.push(`已付款 ${formatFinanceMoney(row.paid_amount)} 元`);
    parts.push(`未付款 ${formatFinanceMoney(row.unpaid_amount)} 元`);
    return `${label} ${parts.join("、")}`;
  }
  if (row.open_amount !== undefined && row.open_amount !== null) {
    const parts = [];
    if (row.invoice_amount !== undefined && row.invoice_amount !== null) parts.push(`已收票 ${formatFinanceMoney(row.invoice_amount)} 元`);
    if (row.paid_amount !== undefined && row.paid_amount !== null) parts.push(`已付款 ${formatFinanceMoney(row.paid_amount)} 元`);
    parts.push(`未付款 ${formatFinanceMoney(row.open_amount)} 元`);
    return `${label} ${parts.join("、")}`;
  }
  if (row.unreceived_amount !== undefined && row.unreceived_amount !== null) {
    const parts = [];
    if (row.settlement_amount !== undefined && row.settlement_amount !== null) parts.push(`结算 ${formatFinanceMoney(row.settlement_amount)} 元`);
    if (row.received_amount !== undefined && row.received_amount !== null) parts.push(`已回款 ${formatFinanceMoney(row.received_amount)} 元`);
    parts.push(`未回款 ${formatFinanceMoney(row.unreceived_amount)} 元`);
    return `${label} ${parts.join("、")}`;
  }
  return "";
}

function canonicalFinanceAnswerFromPayload(payload) {
  const compact = compactFinancePayload(payload);
  if (!compact || typeof compact !== "object") return "";
  const requiredAtoms = compact.finance_facts?.required_atoms;
  if (Array.isArray(requiredAtoms)) {
    const lines = [...new Set(requiredAtoms.map((atom) => String(atom || "").trim()).filter(Boolean))];
    if (lines.length) return lines.join("\n");
  }
  const lines = [];
  if (compact.period) lines.push(`期间：${compact.period}`);
  if (compact.metric_label) lines.push(`口径：${compact.metric_label}`);
  if (compact.total !== undefined && compact.total !== null && compact.total !== "") {
    lines.push(`金额：${formatFinanceMoney(compact.total)} 元`);
  }
  const summary = compact.contract_summary && typeof compact.contract_summary === "object" ? compact.contract_summary : {};
  if (summary.cost_settlement !== undefined && summary.cost_paid !== undefined) {
    lines.push(`补充：项目成本 ${formatFinanceMoney(summary.cost_settlement)} 元、已付款 ${formatFinanceMoney(summary.cost_paid)} 元。`);
  } else if (summary.settlement_amount !== undefined && summary.received_amount !== undefined) {
    lines.push(`补充：项目结算 ${formatFinanceMoney(summary.settlement_amount)} 元、已回款 ${formatFinanceMoney(summary.received_amount)} 元。`);
  }
  const detailLines = contractSummaryDetailRows(summary).map(financeDetailLine).filter(Boolean);
  if (detailLines.length) {
    lines.push("明细：");
    detailLines.forEach((line, index) => lines.push(`${index + 1}. ${line}`));
  }
  const sourceNote = String(compact.source_note || "").trim();
  const sourceUpdateNote = normalizedSourceUpdateNote(compact.source_update_note);
  if (sourceNote) lines.push(sourceNote);
  if (sourceUpdateNote) lines.push(sourceUpdateNote);
  return lines.filter(Boolean).join("\n");
}

function hasAssistantToolCalls(message) {
  if (!message || message.role !== "assistant") return false;
  if (Array.isArray(message.toolCalls) && message.toolCalls.length) return true;
  if (Array.isArray(message.tool_calls) && message.tool_calls.length) return true;
  if (Array.isArray(message.content)) {
    return message.content.some((item) => item?.type === "tool_use" || item?.type === "tool_call");
  }
  return false;
}

function appendMissingFinanceFactAtoms(text, atoms, payload) {
  let current = replaceConflictingSourceUpdate(
    replaceConflictingHeadlineAmount(
      replaceMalformedFinanceAmountAtoms(
        replaceSingleConflictingPeriodToken(
          replaceSingleConflictingPeriodRange(String(text || ""), atoms),
          atoms
        ),
        atoms
      ),
      atoms,
      payload
    ),
    atoms
  );
  if (payload && hasFinanceFactConflict(current, atoms, payload)) {
    current = canonicalFinanceAnswerFromPayload(payload) || current;
  }
  const missing = atoms
    .map((atom) => typeof atom === "string" ? { value: atom, line: atom } : atom)
    .filter((atom) => atom?.value && atom?.line && shouldAppendMissingFinanceFactAtom(current, atom))
    .map((atom) => atom.line);
  if (!missing.length) return current;
  const base = current.trimEnd();
  const separator = base ? "\n\n" : "";
  return `${base}${separator}${missing.join("\n")}`;
}

function shouldAppendMissingFinanceFactAtom(text, atom) {
  const current = String(text || "");
  const line = String(atom?.line || "").trim();
  if (!line) return false;
  if (/^(期间|口径|金额|来源更新时间)[：:]/.test(line)) {
    return !current.includes(line);
  }
  const value = String(atom?.value || "").trim();
  return value ? !current.includes(value) && !current.includes(line) : !current.includes(line);
}

function periodAtomValue(atoms) {
  if (!Array.isArray(atoms)) return "";
  for (const atom of atoms) {
    const value = String(typeof atom === "string" ? atom : atom?.value || "").trim();
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    const candidate = line.startsWith("期间：") ? line.slice("期间：".length).trim() : value;
    if (/^20\d{2}-\d{2}$/.test(candidate)) return candidate;
    if (/^20\d{2}-\d{2}~20\d{2}-\d{2}$/.test(candidate)) return candidate;
  }
  return "";
}

function normalizePeriodRangeToken(raw) {
  const text = String(raw || "").trim();
  let match = text.match(/(20\d{2})-(\d{1,2})\s*~\s*(20\d{2})-(\d{1,2})/);
  if (!match) {
    match = text.match(/(20\d{2})年\s*(\d{1,2})月\s*[~至到-]\s*(20\d{2})年\s*(\d{1,2})月/);
  }
  if (!match) return "";
  const [, fromYear, fromMonth, toYear, toMonth] = match;
  return `${fromYear}-${fromMonth.padStart(2, "0")}~${toYear}-${toMonth.padStart(2, "0")}`;
}

function chinesePeriodRange(period) {
  const match = String(period || "").match(/^(20\d{2})-(\d{2})~(20\d{2})-(\d{2})$/);
  if (!match) return period;
  const [, fromYear, fromMonth, toYear, toMonth] = match;
  return `${fromYear}年${Number(fromMonth)}月~${toYear}年${Number(toMonth)}月`;
}

function periodRangesFromText(text) {
  const ranges = [];
  for (const pattern of [
    /20\d{2}-\d{2}\s*~\s*20\d{2}-\d{2}/g,
    /20\d{2}年\s*\d{1,2}月\s*[~至到-]\s*20\d{2}年\s*\d{1,2}月/g
  ]) {
    for (const match of String(text || "").matchAll(pattern)) {
      ranges.push(match[0]);
    }
  }
  return ranges;
}

function replaceSingleConflictingPeriodRange(text, atoms) {
  const currentPeriod = periodAtomValue(atoms);
  if (!currentPeriod) return text;
  const ranges = periodRangesFromText(text);
  if (!ranges.length) return text;
  const uniquePeriods = [...new Set(ranges.map(normalizePeriodRangeToken).filter(Boolean))];
  if (uniquePeriods.length !== 1 || uniquePeriods[0] === currentPeriod) return text;
  let nextText = text;
  for (const range of ranges) {
    const replacement = range.includes("年") ? chinesePeriodRange(currentPeriod) : currentPeriod;
    nextText = nextText.split(range).join(replacement);
  }
  return nextText;
}

function replaceSingleConflictingPeriodToken(text, atoms) {
  const currentPeriod = periodAtomValue(atoms);
  if (!/^20\d{2}-\d{2}$/.test(currentPeriod)) return text;
  const matches = [...String(text || "").matchAll(/20\d{2}-\d{2}/g)].map((match) => match[0]);
  const uniquePeriods = [...new Set(matches)];
  const conflicting = uniquePeriods.filter((period) => period !== currentPeriod);
  if (conflicting.length !== 1 || uniquePeriods.includes(currentPeriod)) return text;
  return String(text || "").split(conflicting[0]).join(currentPeriod);
}

function replaceConflictingHeadlineAmount(text, atoms, payload) {
  const compact = compactFinancePayload(payload);
  const expectedAmount = amountAtomValue((atoms || []).find((atom) => {
    const line = String(typeof atom === "string" ? atom : atom?.line || "").trim();
    return line.startsWith("金额：") || line.startsWith("金额:");
  }));
  const label = String(compact?.metric || compact?.metric_label || "").trim();
  if (!expectedAmount || !label) return text;
  const lines = String(text || "").split("\n");
  const moneyPattern = /(-?[0-9][0-9,]*(?:\.\d+)?)(\s*)(万元|万|元)/g;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] || "";
    if (!line.includes(label)) continue;
    const matches = [...line.matchAll(moneyPattern)];
    if (matches.length !== 1) continue;
    const displayAmount = displayedFinanceAmountInYuan(matches[0][1], matches[0][3]);
    if (sameFinanceAmount(displayAmount, expectedAmount)) continue;
    const start = matches[0].index ?? -1;
    if (start < 0) continue;
    const end = start + matches[0][0].length;
    const trailingAlreadyHasYuan = matches[0][3] !== "元" && /^\*{0,2}\s*元/.test(line.slice(end));
    const replacement = trailingAlreadyHasYuan ? expectedAmount : `${expectedAmount} 元`;
    lines[i] = line.slice(0, start) + replacement + line.slice(end);
    return lines.join("\n");
  }
  return text;
}

function patchAssistantMessageWithFinanceFactAtoms(message, atoms, payload) {
  if (!message || message.role !== "assistant" || hasAssistantToolCalls(message)) return null;
  if (!Array.isArray(atoms) || !atoms.length) return null;
  if (typeof message.content === "string") {
    const nextText = appendMissingFinanceFactAtoms(message.content, atoms, payload);
    return nextText === message.content ? message : { ...message, content: nextText };
  }
  if (!Array.isArray(message.content)) return null;
  const textIndex = message.content.map((item) => item?.type).lastIndexOf("text");
  if (textIndex < 0) return null;
  const currentText = String(message.content[textIndex]?.text || "");
  const nextText = appendMissingFinanceFactAtoms(currentText, atoms, payload);
  if (nextText === currentText) return message;
  const nextContent = message.content.slice();
  nextContent[textIndex] = { ...nextContent[textIndex], text: nextText };
  return { ...message, content: nextContent };
}

function hasAssistantMessageText(message) {
  if (!message || message.role !== "assistant" || hasAssistantToolCalls(message)) return false;
  if (typeof message.content === "string") return Boolean(message.content.trim());
  if (!Array.isArray(message.content)) return false;
  return message.content.some((item) => item?.type === "text" && typeof item.text === "string" && item.text.trim());
}

function patchFinanceTextValue(value, atoms, payload) {
  if (typeof value !== "string" || !value.trim()) return { text: value, changed: false };
  const nextText = appendMissingFinanceFactAtoms(value, atoms, payload);
  return { text: nextText, changed: nextText !== value };
}

function patchLastStringArrayItem(values, atoms, payload) {
  if (!Array.isArray(values) || !Array.isArray(atoms) || !atoms.length) return false;
  for (let i = values.length - 1; i >= 0; i--) {
    const patched = patchFinanceTextValue(values[i], atoms, payload);
    if (!patched.text?.trim()) continue;
    if (!patched.changed) return false;
    values[i] = patched.text;
    return true;
  }
  return false;
}

function patchLastTextObject(items, atoms, payload) {
  if (!Array.isArray(items) || !Array.isArray(atoms) || !atoms.length) return false;
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (!item || typeof item !== "object" || typeof item.text !== "string") continue;
    const patched = patchFinanceTextValue(item.text, atoms, payload);
    if (!patched.text?.trim()) continue;
    if (!patched.changed) return false;
    item.text = patched.text;
    return true;
  }
  return false;
}

function hasPatchableFinanceTextTarget(target) {
  if (Array.isArray(target)) return target.some((value) => typeof value === "string" && value.trim());
  if (!target || typeof target !== "object") return false;
  if (hasAssistantMessageText(target.message)) return true;
  if (Array.isArray(target.assistantTexts) && target.assistantTexts.some((value) => typeof value === "string" && value.trim())) return true;
  if (Array.isArray(target.payloads) && target.payloads.some((item) => item && typeof item === "object" && typeof item.text === "string" && item.text.trim())) return true;
  if (Array.isArray(target.content) && target.content.some((item) => item && typeof item === "object" && typeof item.text === "string" && item.text.trim())) return true;
  if (target.result && typeof target.result === "object" && hasPatchableFinanceTextTarget(target.result)) return true;
  if (typeof target.text === "string" && target.text.trim()) return true;
  if (typeof target.content === "string" && target.content.trim()) return true;
  return false;
}

function patchAssistantTextsWithFinanceFactAtoms(target, atoms, payload) {
  if (!Array.isArray(atoms) || !atoms.length) return false;
  if (Array.isArray(target)) return patchLastStringArrayItem(target, atoms, payload);
  if (!target || typeof target !== "object") return false;
  if (target.message && typeof target.message === "object") {
    const patched = patchAssistantMessageWithFinanceFactAtoms(target.message, atoms, payload);
    if (patched && patched !== target.message) {
      target.message = patched;
      return true;
    }
  }
  if (Array.isArray(target.assistantTexts) && patchLastStringArrayItem(target.assistantTexts, atoms, payload)) return true;
  if (Array.isArray(target.payloads) && patchLastTextObject(target.payloads, atoms, payload)) return true;
  if (Array.isArray(target.content) && patchLastTextObject(target.content, atoms, payload)) return true;
  if (target.result && typeof target.result === "object" && patchAssistantTextsWithFinanceFactAtoms(target.result, atoms, payload)) return true;
  if (typeof target.text === "string") {
    const patched = patchFinanceTextValue(target.text, atoms, payload);
    if (patched.changed) {
      target.text = patched.text;
      return true;
    }
  }
  if (typeof target.content === "string") {
    const patched = patchFinanceTextValue(target.content, atoms, payload);
    if (patched.changed) {
      target.content = patched.text;
      return true;
    }
  }
  return false;
}

async function financeQuerySystemFacts(question) {
  const bundle = await financeQuerySystemFactBundle(question);
  return bundle.text;
}

async function financeQuerySystemFactBundle(question) {
  const result = await callFinanceTool("finance-query", { query: question });
  const payload = compactFinancePayload(parseToolResultPayload(result));
  if (!payload || typeof payload !== "object") return { text: "", payload: null };
  const facts = payload.finance_facts && typeof payload.finance_facts === "object" ? payload.finance_facts : null;
  const lines = [
    facts
      ? "本次核对结果只供生成最终回复使用；不要展示本段标题、JSON 或字段名。必须以 FinanceQA 结构化事实包为准自然组织语言。"
      : "本次核对结果只供生成最终回复使用；不要展示本段标题、JSON 或字段名，可以基于“当前 finance-query 老板答案”自然改写。",
    `最新问题：${question}`
  ];
  if (facts) {
    lines.push("FinanceQA 决定事实，OpenClaw 决定表达。");
    const factParts = [];
    for (const [key, label] of [
      ["schema_version", "schema"],
      ["resolved_period", "resolved_period"],
      ["requested_period", "requested_period"],
      ["basis", "basis"],
      ["headline_metric", "headline_metric"],
      ["headline_amount", "headline_amount"]
    ]) {
      if (facts[key] !== undefined && facts[key] !== null && facts[key] !== "") {
        factParts.push(`${label}=${facts[key]}`);
      }
    }
    if (factParts.length) lines.push(`结构化事实包：${factParts.join("；")}`);
    if (facts.metrics) lines.push(`metrics：${JSON.stringify(facts.metrics)}`);
    if (Array.isArray(facts.source_tables) && facts.source_tables.length) lines.push(`source_tables：${JSON.stringify(facts.source_tables)}`);
    if (Array.isArray(facts.source_files) && facts.source_files.length) lines.push(`source_files：${JSON.stringify(facts.source_files)}`);
    if (Array.isArray(facts.warnings) && facts.warnings.length) lines.push(`warnings：${JSON.stringify(facts.warnings)}`);
    if (Array.isArray(facts.explanation_hints) && facts.explanation_hints.length) lines.push(`explanation_hints：${JSON.stringify(facts.explanation_hints)}`);
    if (facts.resolved_period) lines.push(`不得把 resolved_period=${facts.resolved_period} 改成其他月份或相对时间。`);
    if (facts.basis) lines.push(`不得把 basis=${facts.basis} 改成其他口径。`);
  } else if (payload.final_answer) {
    lines.push(`当前 finance-query 老板答案：${payload.final_answer}`);
  }
  if (payload.metric_label) lines.push(`标准指标标签：${payload.metric_label}`);
  if (payload.business_basis) lines.push(`业务口径：${payload.business_basis}`);
  if (payload.metric) lines.push(`标准指标：${payload.metric}`);
  if (payload.total !== undefined && payload.total !== null && payload.total !== "") lines.push(`标准金额：${payload.total}`);
  const requiredAtoms = requiredBossVisibleAtoms(payload);
  if (requiredAtoms.length) lines.push(`老板可见回复必须出现的精确片段：${JSON.stringify(requiredAtoms)}`);
  if (payload.source_note) lines.push(`来源说明：${payload.source_note}`);
  if (payload.source_update_note) {
    const updateNote = String(payload.source_update_note).trim();
    lines.push(updateNote.startsWith("来源更新时间") ? updateNote : `来源更新时间：${updateNote}`);
  }
  if (payload.period) lines.push(`期间：${payload.period}`);
  if (payload.requested_metrics) lines.push(`指标：${JSON.stringify(payload.requested_metrics)}`);
  const itemRows = compactFinanceRows(payload.items);
  if (itemRows.length) lines.push(`分主体/期间汇总：${JSON.stringify(itemRows)}`);
  const detailRows = compactFinanceRows([
    ...(Array.isArray(payload.detail_items) ? payload.detail_items : []),
    ...contractSummaryDetailRows(payload.contract_summary)
  ]);
  if (detailRows.length) lines.push(`合同/项目明细：${JSON.stringify(detailRows)}`);
  if (!payload.final_answer) lines.push(`结果摘要：${payload.message || payload.error || "finance-query 未返回老板答案"}`);
  return { text: lines.join("\n"), payload };
}

async function callFinanceTool(name, rawParams) {
  try {
    const client = await getMCPClient();
    const result = await client.callTool(name, rawParams || {});

    // Convert MCP tool result to OpenClaw format
    if (result.content && result.content[0] && result.content[0].type === "text") {
      try {
        // Parse the JSON result from the text
        const parsed = JSON.parse(result.content[0].text);
        return textResult(parsed);
      } catch {
        // Return as-is if not JSON
        return result;
      }
    }
    return result;
  } catch (error) {
    return errorResult(error);
  }
}

function createFinanceTool(name, description, parameters, toolCtx) {
  return {
    name,
    label: name,
    description,
    parameters,
    async execute(_toolCallId, rawParams, runtimeCtx) {
      const executionCtx = financeExecutionContext(toolCtx, runtimeCtx);
      const protectedQuestion = name === "finance-query" ? takeLatestFinanceQuestionForTool(executionCtx) : "";
      const rawParamsObject = isRecord(rawParams) ? rawParams : {};
      const { raw_user_query: _discardModelRawQuery, ...forwardedParams } = rawParamsObject;
      const modelQuery = name === "finance-query" ? financeQuestionText(rawParamsObject.query || "") : "";
      const params = name === "finance-query"
        ? (modelQuery
          ? {
            ...forwardedParams,
            query: modelQuery,
            ...(protectedQuestion ? { raw_user_query: protectedQuestion } : {})
          }
          : forwardedParams)
        : rawParams;
      const result = await callFinanceTool(name, params);
      if (name === "finance-query") {
        const payload = compactFinancePayload(parseToolResultPayload(result));
        const atoms = financeFactAtomsFromPayload(payload);
        if (atoms.length) {
          rememberPendingFinanceFacts(undefined, executionCtx, atoms, payload);
        }
      }
      return result;
    }
  };
}

function createFinanceToolRegistration(name, description, parameters) {
  const fallbackTool = createFinanceTool(name, description, parameters);
  const factory = (ctx) => createFinanceTool(name, description, parameters, ctx);
  factory.label = fallbackTool.label;
  factory.description = fallbackTool.description;
  factory.parameters = fallbackTool.parameters;
  factory.execute = fallbackTool.execute;
  return factory;
}

const plugin = {
  id: PLUGIN_ID,
  name: "Finance",
  description: "Finance MCP plugin (native, no Python bridge)",
  register(api) {
    pluginRuntime = api;

    api.registerTool(createFinanceToolRegistration(
      "finance-query",
      "Boss finance QA. Call this first for finance questions. When the returned JSON has finance_facts, use finance_facts as the factual source; otherwise use final_answer or boss_reply_text as the compatibility factual anchor. You may rephrase for clarity, but preserve exact amounts, period, business basis, uncertainty, source notes, and source update time. When it has contract_continuity_candidates, describe them as same-project candidates/references, not a confirmed counterparty mapping.",
      {
        type: "object",
        properties: {
          query: {
            type: "string",
            description: "The latest natural-language finance question from the user"
          },
          raw_user_query: {
            type: "string",
            description: "Original user finance question before agent rewrite; used only to protect intent, period, and source basis"
          }
        },
        required: ["query"]
      }
    ), { name: "finance-query" });

    api.registerTool(createFinanceToolRegistration(
      "finance-upload",
      "Upload and import Excel files (bank statements, journals, balance sheets, contract ledgers, etc.)",
      {
        type: "object",
        properties: {
          file: {
            type: "string",
            description: "Path to the Excel file to upload"
          }
        },
        required: ["file"]
      }
    ), { name: "finance-upload" });

    api.registerTool(createFinanceToolRegistration(
      "finance-sync",
      "Synchronize a directory of financial Excel files",
      {
        type: "object",
        properties: {
          directory: {
            type: "string",
            description: "Directory path containing Excel files"
          },
          incremental: {
            type: "boolean",
            description: "Incremental sync (don't clear existing data)"
          }
        },
        required: ["directory"]
      }
    ), { name: "finance-sync" });

    api.on("before_prompt_build", async (event, ctx) => {
      const latestQuestion = financeQuestionForPromptEvent(event);
      setLatestFinanceQuestionForToolScope(event, ctx, latestQuestion || "");
      if (!latestQuestion) {
        clearPendingFinanceFacts(event, ctx);
        return undefined;
      }
      if (eventRequiresObservableFinanceToolCall(event)) {
        clearPendingFinanceFacts(event, ctx);
        return {
          prependSystemContext: mustCallFinanceQuerySystemContext(latestQuestion, observableFinanceToolCallContext())
        };
      }
      const financeFactBundle = await financeQuerySystemFactBundle(latestQuestion);
      const atoms = financeFactAtomsFromPayload(financeFactBundle.payload);
      if (atoms.length) {
        rememberPendingFinanceFacts(event, ctx, atoms, financeFactBundle.payload);
      } else {
        clearPendingFinanceFacts(event, ctx);
      }
      return {
        prependSystemContext: mustCallFinanceQuerySystemContext(latestQuestion, financeFactBundle.text)
      };
    });

    api.on("llm_output", (event, ctx) => {
      const bundle = pendingFinanceFactsForScope(event, ctx);
      const atoms = bundle?.atoms;
      const payload = bundle?.payload;
      if (!atoms?.length) return undefined;
      const hasPatchableText = hasPatchableFinanceTextTarget(event);
      const patched = patchAssistantTextsWithFinanceFactAtoms(event, atoms, payload);
      if (!patched && !hasPatchableText) return undefined;
      pendingFinanceFactAtomsBySession.delete(bundle.key);
      pendingFinanceFactPayloadsBySession.delete(bundle.key);
      unregisterFinanceRunForSession(bundle.scope);
      return undefined;
    });

    api.on("before_message_write", (event, ctx) => {
      const message = event?.message;
      if (message?.role === "toolResult") {
        const atoms = financeFactAtomsFromToolResult(message);
        if (atoms.length) {
          rememberPendingFinanceFacts(event, ctx, atoms, compactFinancePayload(parseToolResultPayload(message)));
        } else if (message.toolName === "finance-query") {
          clearPendingFinanceFacts(event, ctx);
        }
        return undefined;
      }
      if (message?.role !== "assistant") return undefined;
      const bundle = pendingFinanceFactsForScope(event, ctx);
      const atoms = bundle?.atoms;
      const payload = bundle?.payload;
      if (!atoms?.length) return undefined;
      const patched = patchAssistantMessageWithFinanceFactAtoms(message, atoms, payload);
      if (!patched) return undefined;
      return { message: patched };
    });
  }
};

export { plugin as default, normalizePluginConfig, RemoteMCPClient };
