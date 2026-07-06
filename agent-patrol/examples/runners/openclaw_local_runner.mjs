#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import fs from "node:fs";

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2) {
  args.set(process.argv[i], process.argv[i + 1]);
}

if (process.env.AGENT_PATROL_LIVE !== "1") {
  console.error("Refusing live OpenClaw run: set AGENT_PATROL_LIVE=1 explicitly.");
  process.exit(2);
}

const questionFile = args.get("--question-file");
const sessionId = args.get("--session-id");
const agent = args.get("--agent");
const thinking = args.get("--thinking") ?? "off";
const timeout = args.get("--timeout") ?? "180";
const requiredTool = args.get("--require-tool");

if (!questionFile || !sessionId) {
  console.error("usage: openclaw_local_runner.mjs --question-file <path> --session-id <id> [--agent <id>] [--require-tool <tool>]");
  process.exit(2);
}

const question = buildQuestion(fs.readFileSync(questionFile, "utf8"), requiredTool);
const openclawArgs = ["agent", "--json", "--message", question, "--session-id", sessionId, "--thinking", thinking, "--timeout", timeout];
if (agent) {
  openclawArgs.splice(1, 0, "--agent", agent);
}

const result = spawnSync("openclaw", openclawArgs, {
  encoding: "utf8",
  maxBuffer: 20 * 1024 * 1024
});

if (result.stdout) process.stdout.write(result.stdout);
if (result.stderr) process.stderr.write(result.stderr);
if (result.error) {
  console.error(result.error.stack ?? result.error.message);
  process.exit(1);
}
process.exit(result.status ?? 1);

function buildQuestion(question, requiredTool) {
  if (!requiredTool) return question;
  return [
    "[巡检要求]",
    `这是一条只读巡检请求。回答前必须先调用 \`${requiredTool}\` 获取最新事实；不要使用记忆、历史会话、已有上下文或猜测直接作答。`,
    "调用工具后，用自然语言回答下面的用户原问题；不要暴露工具原始 JSON 或内部 final_answer。",
    "",
    "[用户原问题]",
    question
  ].join("\n");
}
