#!/usr/bin/env node
import fs from "node:fs";

const DEFAULT_TIMEOUT_MS = 120_000;
const DEFAULT_MAX_TOKENS = 4096;

main().catch((error) => {
  console.error(error instanceof Error ? error.stack ?? error.message : String(error));
  process.exit(2);
});

async function main() {
  loadEnvFile(process.env.AGENT_PATROL_LLM_ENV_FILE);

  const prompt = await readStdin();
  if (!prompt.trim()) throw new Error("stdin prompt is required");

  const apiKey = envValue("AGENT_PATROL_LLM_API_KEY", "AGENT_PATROL_LLM_API_KEY_ENV")
    ?? process.env.OPENAI_API_KEY
    ?? process.env.DEEPSEEK_API_KEY;
  const baseUrl = envValue("AGENT_PATROL_LLM_BASE_URL", "AGENT_PATROL_LLM_BASE_URL_ENV")
    ?? process.env.OPENAI_BASE_URL
    ?? process.env.DEEPSEEK_BASE_URL;
  const model = envValue("AGENT_PATROL_LLM_MODEL", "AGENT_PATROL_LLM_MODEL_ENV")
    ?? process.env.OPENAI_MODEL
    ?? process.env.DEEPSEEK_MODEL
    ?? "deepseek-chat";
  const responseFormat = envValue("AGENT_PATROL_LLM_RESPONSE_FORMAT", "AGENT_PATROL_LLM_RESPONSE_FORMAT_ENV");

  if (!apiKey) throw new Error("AGENT_PATROL_LLM_API_KEY, OPENAI_API_KEY, or DEEPSEEK_API_KEY is required");
  if (!baseUrl) throw new Error("AGENT_PATROL_LLM_BASE_URL, OPENAI_BASE_URL, or DEEPSEEK_BASE_URL is required");

  const response = await callChatCompletions({
    url: chatCompletionsUrl(baseUrl),
    apiKey,
    model,
    prompt,
    responseFormat,
    timeoutMs: Number(process.env.AGENT_PATROL_LLM_TIMEOUT_MS) || DEFAULT_TIMEOUT_MS
  });

  process.stdout.write(response.trim() + "\n");
}

function loadEnvFile(filePath) {
  if (!filePath) return;
  if (!fs.existsSync(filePath)) throw new Error(`AGENT_PATROL_LLM_ENV_FILE not found: ${filePath}`);
  const lines = fs.readFileSync(filePath, "utf8").split(/\r?\n/);
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const match = trimmed.match(/^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$/);
    if (!match) continue;
    const key = match[1];
    if (process.env[key] !== undefined) continue;
    process.env[key] = unquoteEnvValue(match[2] ?? "");
  }
}

function unquoteEnvValue(value) {
  const trimmed = value.trim();
  if ((trimmed.startsWith("\"") && trimmed.endsWith("\"")) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function envValue(name, indirectionName) {
  if (process.env[name]) return process.env[name];
  const envName = process.env[indirectionName];
  if (envName && process.env[envName]) return process.env[envName];
  return undefined;
}

function readStdin() {
  return new Promise((resolve, reject) => {
    let input = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk) => {
      input += chunk;
    });
    process.stdin.on("error", reject);
    process.stdin.on("end", () => resolve(input));
  });
}

function chatCompletionsUrl(baseUrl) {
  const base = baseUrl.replace(/\/+$/, "");
  if (base.endsWith("/chat/completions")) return base;
  if (/\/v\d+$/i.test(base)) return `${base}/chat/completions`;
  return `${base}/v1/chat/completions`;
}

async function callChatCompletions(options) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs);
  try {
    const body = {
      model: options.model,
      messages: [{ role: "user", content: options.prompt }],
      temperature: Number(process.env.AGENT_PATROL_LLM_TEMPERATURE ?? "0.7"),
      max_tokens: Number(process.env.AGENT_PATROL_LLM_MAX_TOKENS ?? String(DEFAULT_MAX_TOKENS))
    };
    const responseFormat = parseResponseFormat(options.responseFormat);
    if (responseFormat) {
      body.response_format = responseFormat;
    }

    const response = await fetch(options.url, {
      method: "POST",
      headers: {
        "authorization": `Bearer ${options.apiKey}`,
        "content-type": "application/json"
      },
      body: JSON.stringify(body),
      signal: controller.signal
    });
    const text = await response.text();
    if (!response.ok) {
      throw new Error(`OpenAI-compatible LLM request failed ${response.status}: ${text.slice(0, 500)}`);
    }
    const payload = JSON.parse(text);
    const content = extractResponseContent(payload);
    if (typeof content !== "string" || !content.trim()) {
      throw new Error("OpenAI-compatible LLM response did not contain message content");
    }
    return content;
  } finally {
    clearTimeout(timer);
  }
}

function extractResponseContent(payload) {
  const choice = payload?.choices?.[0];
  const message = choice?.message;
  return stringifyContent(message?.content)
    ?? stringifyContent(message?.parsed)
    ?? stringifyContent(choice?.text)
    ?? stringifyContent(payload?.output_text);
}

function stringifyContent(value) {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    const text = value
      .map((item) => {
        if (typeof item === "string") return item;
        if (item && typeof item === "object") {
          if (typeof item.text === "string") return item.text;
          if (typeof item.content === "string") return item.content;
        }
        return undefined;
      })
      .filter(Boolean)
      .join("\n");
    return text || undefined;
  }
  if (value && typeof value === "object") return JSON.stringify(value);
  return undefined;
}

function parseResponseFormat(value) {
  const trimmed = typeof value === "string" ? value.trim() : "";
  if (!trimmed || trimmed === "none" || trimmed === "off") return undefined;
  if (trimmed === "json_object") return { type: "json_object" };
  if (trimmed.startsWith("{")) return JSON.parse(trimmed);
  return { type: trimmed };
}
