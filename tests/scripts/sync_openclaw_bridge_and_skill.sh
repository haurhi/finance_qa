#!/usr/bin/env bash
set -euo pipefail

# Sync Go MCP binary + SKILL.md + appendix + OpenClaw plugin runtime to server.
# SERVER defaults to the local SSH config alias. KEY_PATH is optional; set it
# only when the SSH config alias is not enough.

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SERVER="${SERVER:-lzh}"
MODE="${MODE:-all}"
LOCAL_VERSION_PREFLIGHT="$ROOT_DIR/tests/scripts/check_version_preflight.sh"
VERSION_PREFLIGHT_ENABLED="${VERSION_PREFLIGHT_ENABLED:-1}"

case "$MODE" in
  all|server|connector) ;;
  *)
    echo "invalid MODE=$MODE; expected all, server, or connector" >&2
    exit 1
    ;;
esac

deploy_server() {
  [[ "$MODE" == "all" || "$MODE" == "server" ]]
}

deploy_connector() {
  [[ "$MODE" == "all" || "$MODE" == "connector" ]]
}

if [[ "$VERSION_PREFLIGHT_ENABLED" == "1" ]]; then
  if [[ ! -x "$LOCAL_VERSION_PREFLIGHT" ]]; then
    echo "missing executable version preflight: $LOCAL_VERSION_PREFLIGHT" >&2
    exit 1
  fi
  echo "[0/9] run version preflight"
  "$LOCAL_VERSION_PREFLIGHT"
fi

ssh_remote() {
  if [[ -n "${KEY_PATH:-}" ]]; then
    ssh -i "$KEY_PATH" "$@"
    return
  fi
  ssh "$@"
}

scp_remote() {
  if [[ -n "${KEY_PATH:-}" ]]; then
    scp -i "$KEY_PATH" "$@"
    return
  fi
  scp "$@"
}

REMOTE_HOME="${REMOTE_HOME:-$(ssh_remote "$SERVER" 'printf %s "$HOME"')}"

LOCAL_SKILL="$ROOT_DIR/SKILL.md"
LOCAL_APPENDIX="$ROOT_DIR/docs/SKILL_APPENDIX_FULL.md"
LOCAL_PLUGIN_DIST="$ROOT_DIR/plugin/openclaw-finance/dist/index.esm.js"
LOCAL_PLUGIN_MANIFEST="$ROOT_DIR/plugin/openclaw-finance/openclaw.plugin.json"
LOCAL_PLUGIN_PACKAGE="$ROOT_DIR/plugin/openclaw-finance/package.json"
LOCAL_CLAUDE_WRAPPER="$ROOT_DIR/tests/scripts/claude_finance_final_answer.sh"
LOCAL_ONLINE_CHECKER="$ROOT_DIR/tests/scripts/run_online_agent_final_answer_check.py"
LOCAL_MCP_SYSTEMD="$ROOT_DIR/deploy/systemd/financeqa-mcp.service"
LOCAL_RULES_CONFIG="$ROOT_DIR/config/rules.json"
LOCAL_FINANCEQA_BIN="$(mktemp "${TMPDIR:-/tmp}/financeqa-linux-amd64.XXXXXX")"
trap 'rm -f "$LOCAL_FINANCEQA_BIN"' EXIT

REMOTE_REPO_DIR="${REMOTE_REPO_DIR:-$REMOTE_HOME/finance_qa}"
REMOTE_RULES_CONFIG="${REMOTE_RULES_CONFIG:-$REMOTE_REPO_DIR/config/rules.json}"
REMOTE_RULES_UPLOAD="${REMOTE_RULES_UPLOAD:-$REMOTE_RULES_CONFIG.upload.$$}"
REMOTE_FINANCEQA_BIN="${REMOTE_FINANCEQA_BIN:-$REMOTE_REPO_DIR/bin/financeqa}"
REMOTE_FINANCEQA_UPLOAD="${REMOTE_FINANCEQA_UPLOAD:-$REMOTE_FINANCEQA_BIN.upload.$$}"
REMOTE_FINANCEQA_BIN_DIR="$(dirname "$REMOTE_FINANCEQA_BIN")"
REMOTE_FINANCEQA_SERVE_PATTERN="${REMOTE_FINANCEQA_SERVE_PATTERN:-$(printf '%s' "$REMOTE_FINANCEQA_BIN" | sed 's#/#[/]#g') serve}"
REMOTE_OPENCLAW_PLUGIN_DIR="${REMOTE_OPENCLAW_PLUGIN_DIR:-$REMOTE_HOME/.openclaw/extensions/openclaw-finance}"
REMOTE_OPENCLAW_SKILL_DIR="${REMOTE_OPENCLAW_SKILL_DIR:-$REMOTE_HOME/.openclaw/skills/finance}"
REMOTE_OPENCLAW_EXT_SKILL_DIR="${REMOTE_OPENCLAW_EXT_SKILL_DIR:-$REMOTE_HOME/.openclaw/extensions/openclaw-finance/skills/finance}"
REMOTE_CLAUDE_SKILL_DIR="${REMOTE_CLAUDE_SKILL_DIR:-$REMOTE_HOME/.claude/skills/finance}"
REMOTE_OPENCLAW_SKILL_PARENT="$(dirname "$REMOTE_OPENCLAW_SKILL_DIR")"
REMOTE_CLAUDE_SKILL_PARENT="$(dirname "$REMOTE_CLAUDE_SKILL_DIR")"
REMOTE_OPENCLAW_SESSION_STORE="${REMOTE_OPENCLAW_SESSION_STORE:-$REMOTE_HOME/.openclaw/agents/main/sessions/sessions.json}"
REMOTE_OPENCLAW_CONFIG_PATH="${REMOTE_OPENCLAW_CONFIG_PATH:-$REMOTE_HOME/.openclaw/openclaw.json}"
REMOTE_SYSTEMD_DIR="${REMOTE_SYSTEMD_DIR:-/etc/systemd/system}"
REMOTE_MCP_READ_TOKEN_FILE="${REMOTE_MCP_READ_TOKEN_FILE:-$REMOTE_REPO_DIR/secrets/mcp_read_token}"
REMOTE_MCP_ADMIN_TOKEN_FILE="${REMOTE_MCP_ADMIN_TOKEN_FILE:-$REMOTE_REPO_DIR/secrets/mcp_admin_token}"
RESTART_OPENCLAW_GATEWAY="${RESTART_OPENCLAW_GATEWAY:-1}"
RESTART_FINANCEQA_MCP="${RESTART_FINANCEQA_MCP:-1}"

if [[ ! -f "$LOCAL_SKILL" ]]; then
  echo "missing local SKILL.md: $LOCAL_SKILL" >&2
  exit 1
fi
if [[ ! -f "$LOCAL_APPENDIX" ]]; then
  echo "missing local appendix: $LOCAL_APPENDIX" >&2
  exit 1
fi
if deploy_connector && [[ ! -f "$LOCAL_PLUGIN_DIST" ]]; then
  echo "missing local OpenClaw plugin runtime: $LOCAL_PLUGIN_DIST" >&2
  exit 1
fi
if deploy_connector && [[ ! -f "$LOCAL_PLUGIN_MANIFEST" ]]; then
  echo "missing local OpenClaw plugin manifest: $LOCAL_PLUGIN_MANIFEST" >&2
  exit 1
fi
if deploy_connector && [[ ! -f "$LOCAL_PLUGIN_PACKAGE" ]]; then
  echo "missing local OpenClaw plugin package: $LOCAL_PLUGIN_PACKAGE" >&2
  exit 1
fi
if deploy_connector && [[ ! -f "$LOCAL_CLAUDE_WRAPPER" ]]; then
  echo "missing local Claude finance wrapper: $LOCAL_CLAUDE_WRAPPER" >&2
  exit 1
fi
if deploy_connector && [[ ! -f "$LOCAL_ONLINE_CHECKER" ]]; then
  echo "missing local online agent checker: $LOCAL_ONLINE_CHECKER" >&2
  exit 1
fi
if deploy_server && [[ ! -f "$LOCAL_MCP_SYSTEMD" ]]; then
  echo "missing local FinanceQA MCP systemd unit: $LOCAL_MCP_SYSTEMD" >&2
  exit 1
fi
if deploy_server && [[ ! -f "$LOCAL_RULES_CONFIG" ]]; then
  echo "missing local FinanceQA rules config: $LOCAL_RULES_CONFIG" >&2
  exit 1
fi

ssh_remote "$SERVER" "mkdir -p '$REMOTE_REPO_DIR'"

echo "[1/9] upload SKILL.md to ${SERVER}:${REMOTE_REPO_DIR}/SKILL.md"
scp_remote "$LOCAL_SKILL" "$SERVER:$REMOTE_REPO_DIR/SKILL.md"

echo "[2/9] upload appendix to ${SERVER}:${REMOTE_REPO_DIR}/docs/SKILL_APPENDIX_FULL.md"
ssh_remote "$SERVER" "mkdir -p '$REMOTE_REPO_DIR/docs'"
scp_remote "$LOCAL_APPENDIX" "$SERVER:$REMOTE_REPO_DIR/docs/SKILL_APPENDIX_FULL.md"

if deploy_connector; then
  echo "[3/9] upload OpenClaw plugin runtime into repo"
  ssh_remote "$SERVER" "mkdir -p '$REMOTE_REPO_DIR/plugin/openclaw-finance/dist'"
  scp_remote "$LOCAL_PLUGIN_DIST" "$SERVER:$REMOTE_REPO_DIR/plugin/openclaw-finance/dist/index.esm.js"
  scp_remote "$LOCAL_PLUGIN_MANIFEST" "$SERVER:$REMOTE_REPO_DIR/plugin/openclaw-finance/openclaw.plugin.json"
  scp_remote "$LOCAL_PLUGIN_PACKAGE" "$SERVER:$REMOTE_REPO_DIR/plugin/openclaw-finance/package.json"

  echo "[4/9] upload Claude finance final_answer wrapper"
  ssh_remote "$SERVER" "mkdir -p '$REMOTE_REPO_DIR/tests/scripts'"
  scp_remote "$LOCAL_CLAUDE_WRAPPER" "$SERVER:$REMOTE_REPO_DIR/tests/scripts/claude_finance_final_answer.sh"
  scp_remote "$LOCAL_ONLINE_CHECKER" "$SERVER:$REMOTE_REPO_DIR/tests/scripts/run_online_agent_final_answer_check.py"
  ssh_remote "$SERVER" "chmod 755 '$REMOTE_REPO_DIR/tests/scripts/claude_finance_final_answer.sh' '$REMOTE_REPO_DIR/tests/scripts/run_online_agent_final_answer_check.py'"
else
  echo "[3/9] skip OpenClaw plugin runtime upload (MODE=$MODE)"
  echo "[4/9] skip Claude wrapper upload (MODE=$MODE)"
fi

if deploy_server; then
  echo "[5/9] upload rules config, build local Linux financeqa Go MCP binary, and upload"
  ssh_remote "$SERVER" "mkdir -p '$(dirname "$REMOTE_RULES_CONFIG")' && rm -f '$REMOTE_RULES_UPLOAD'"
  scp_remote "$LOCAL_RULES_CONFIG" "$SERVER:$REMOTE_RULES_UPLOAD"
  ssh_remote "$SERVER" "chmod 644 '$REMOTE_RULES_UPLOAD' && mv -f '$REMOTE_RULES_UPLOAD' '$REMOTE_RULES_CONFIG'"
  (
    cd "$ROOT_DIR"
    GOOS=linux GOARCH=amd64 go build -o "$LOCAL_FINANCEQA_BIN" ./cmd/financeqa/...
  )
  ssh_remote "$SERVER" "set -e; \
    if command -v pgrep >/dev/null 2>&1; then \
      pgrep -f '$REMOTE_FINANCEQA_SERVE_PATTERN' | xargs -r kill; \
    fi; \
    mkdir -p '$REMOTE_FINANCEQA_BIN_DIR' && rm -f '$REMOTE_REPO_DIR/financeqa' '$REMOTE_FINANCEQA_UPLOAD'"
  scp_remote "$LOCAL_FINANCEQA_BIN" "$SERVER:$REMOTE_FINANCEQA_UPLOAD"
  ssh_remote "$SERVER" "chmod 755 '$REMOTE_FINANCEQA_UPLOAD' && mv -f '$REMOTE_FINANCEQA_UPLOAD' '$REMOTE_FINANCEQA_BIN'"
else
  echo "[5/9] skip Go MCP binary upload (MODE=$MODE)"
fi

if deploy_server; then
  echo "[6/9] deploy FinanceQA MCP systemd unit"
  ssh_remote "$SERVER" "set -e; \
    for token_file in '$REMOTE_MCP_READ_TOKEN_FILE' '$REMOTE_MCP_ADMIN_TOKEN_FILE'; do \
      test -s \"\$token_file\" || { echo \"missing or empty token file: \$token_file\" >&2; exit 1; }; \
      perms=\$(stat -c '%a' \"\$token_file\"); \
      test \"\$perms\" = '600' || { echo \"token file must be chmod 600: \$token_file has \$perms\" >&2; exit 1; }; \
    done"
  scp_remote "$LOCAL_MCP_SYSTEMD" "$SERVER:/tmp/financeqa-mcp.service.$$"
  ssh_remote "$SERVER" "set -e; \
    install -m 0644 /tmp/financeqa-mcp.service.$$ '$REMOTE_SYSTEMD_DIR/financeqa-mcp.service'; \
    rm -f /tmp/financeqa-mcp.service.$$; \
    systemctl daemon-reload; \
    if [ '$RESTART_FINANCEQA_MCP' = '1' ]; then \
      systemctl enable --now financeqa-mcp.service; \
      systemctl restart financeqa-mcp.service; \
    fi"
else
  echo "[6/9] skip FinanceQA MCP systemd unit (MODE=$MODE)"
fi

if deploy_connector; then
  echo "[7/9] publish OpenClaw extension runtime files and skill symlinks"
  ssh_remote "$SERVER" "set -e; \
  if [ -L '$REMOTE_OPENCLAW_PLUGIN_DIR' ]; then rm -f '$REMOTE_OPENCLAW_PLUGIN_DIR'; fi; \
  mkdir -p '$REMOTE_OPENCLAW_SKILL_PARENT' '$REMOTE_CLAUDE_SKILL_PARENT'; \
  mkdir -p '$REMOTE_OPENCLAW_PLUGIN_DIR/dist'; \
  rm -rf '$REMOTE_OPENCLAW_SKILL_DIR' '$REMOTE_CLAUDE_SKILL_DIR' '$REMOTE_OPENCLAW_EXT_SKILL_DIR'; \
  rm -f '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js' '$REMOTE_OPENCLAW_PLUGIN_DIR/index.ts' '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'; \
  ln -sfn '$REMOTE_REPO_DIR' '$REMOTE_OPENCLAW_SKILL_DIR'; \
  ln -sfn '$REMOTE_REPO_DIR' '$REMOTE_CLAUDE_SKILL_DIR'; \
  cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/dist/index.esm.js' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/openclaw.plugin.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json'; \
  cp '$REMOTE_REPO_DIR/plugin/openclaw-finance/package.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'; \
  chmod 444 '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js' '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'; \
  node -e \"const fs=require('fs'); const financeSkillDir = process.argv[2]; const configPath = process.argv[3]; const packagePath = process.argv[4]; const cfg=JSON.parse(fs.readFileSync(configPath,'utf8')); const pkg=JSON.parse(fs.readFileSync(packagePath,'utf8')); const pluginVersion = pkg.version; const packageExtensions = Array.isArray(pkg.openclaw?.extensions) ? pkg.openclaw.extensions : []; const extraDirs = Array.isArray(cfg.skills?.load?.extraDirs) ? cfg.skills.load.extraDirs : []; const pluginEnabled = cfg.plugins?.entries?.['openclaw-finance']?.enabled === true; const promptHookEnabled = cfg.plugins?.entries?.['openclaw-finance']?.hooks?.allowPromptInjection === true; const install = cfg.plugins?.installs?.['openclaw-finance']; const missing = []; if (!packageExtensions.includes('./dist/index.esm.js')) missing.push('package.json openclaw.extensions must include ./dist/index.esm.js'); if (!extraDirs.includes(financeSkillDir)) missing.push('skills.load.extraDirs must include ' + financeSkillDir); if (!pluginEnabled) missing.push('plugins.entries.openclaw-finance.enabled must be true'); if (!promptHookEnabled) missing.push('plugins.entries.openclaw-finance.hooks.allowPromptInjection must be true'); if (!install || typeof install !== 'object') missing.push('plugins.installs.openclaw-finance must exist'); if (missing.length) { console.error('OpenClaw config is not ready for finance runtime:'); for (const item of missing) console.error('- ' + item); process.exit(1); } cfg.plugins.installs['openclaw-finance'].version = pluginVersion; cfg.plugins.installs['openclaw-finance'].installedAt = new Date().toISOString(); const tmp = configPath + '.tmp-' + process.pid; fs.writeFileSync(tmp, JSON.stringify(cfg, null, 2) + String.fromCharCode(10)); fs.renameSync(tmp, configPath); console.log('verify OpenClaw config references the finance plugin and skill path'); console.log('updated OpenClaw install metadata for openclaw-finance to ' + pluginVersion);\" _ '$REMOTE_OPENCLAW_SKILL_DIR' '$REMOTE_OPENCLAW_CONFIG_PATH' '$REMOTE_REPO_DIR/plugin/openclaw-finance/package.json'; \
  node -e \"const fs=require('fs'); const sessionStorePath=process.argv[2]; if (fs.existsSync(sessionStorePath)) { const sessions=JSON.parse(fs.readFileSync(sessionStorePath,'utf8')); let cleared=0; for (const entry of Object.values(sessions)) { if (entry && typeof entry === 'object' && entry.skillsSnapshot) { delete entry.skillsSnapshot; cleared += 1; } } if (cleared > 0) { const tmp = sessionStorePath + '.tmp-' + process.pid; fs.writeFileSync(tmp, JSON.stringify(sessions, null, 2) + String.fromCharCode(10)); fs.renameSync(tmp, sessionStorePath); } console.log('cleared skillsSnapshot caches: ' + cleared); }\" _ '$REMOTE_OPENCLAW_SESSION_STORE'; \
  chmod 444 '$REMOTE_REPO_DIR/SKILL.md' '$REMOTE_REPO_DIR/docs/SKILL_APPENDIX_FULL.md' '$REMOTE_REPO_DIR/plugin/openclaw-finance/dist/index.esm.js' '$REMOTE_REPO_DIR/plugin/openclaw-finance/openclaw.plugin.json' '$REMOTE_REPO_DIR/plugin/openclaw-finance/package.json'"

  echo "[7/9] verify skill path and plugin runtime on connector host"
  ssh_remote "$SERVER" "set -e; \
  ls -ld '$REMOTE_OPENCLAW_SKILL_DIR' '$REMOTE_CLAUDE_SKILL_DIR'; \
  ls -l '$REMOTE_REPO_DIR/SKILL.md' '$REMOTE_REPO_DIR/docs/SKILL_APPENDIX_FULL.md' '$REMOTE_OPENCLAW_SKILL_DIR/SKILL.md' '$REMOTE_OPENCLAW_SKILL_DIR/docs/SKILL_APPENDIX_FULL.md' '$REMOTE_CLAUDE_SKILL_DIR/SKILL.md' '$REMOTE_CLAUDE_SKILL_DIR/docs/SKILL_APPENDIX_FULL.md' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js' '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json' '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json' '$REMOTE_REPO_DIR/tests/scripts/claude_finance_final_answer.sh' '$REMOTE_REPO_DIR/tests/scripts/run_online_agent_final_answer_check.py'; \
  test ! -e '$REMOTE_REPO_DIR/financeqa'; \
  test ! -e '$REMOTE_OPENCLAW_EXT_SKILL_DIR'; \
  test ! -e '$REMOTE_OPENCLAW_PLUGIN_DIR/index.ts'; \
  test -L '$REMOTE_OPENCLAW_SKILL_DIR'; \
  test -L '$REMOTE_CLAUDE_SKILL_DIR'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_SKILL_DIR')\" = '$REMOTE_REPO_DIR'; \
  test \"\$(readlink -f '$REMOTE_CLAUDE_SKILL_DIR')\" = '$REMOTE_REPO_DIR'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_SKILL_DIR/SKILL.md')\" = '$REMOTE_REPO_DIR/SKILL.md'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_SKILL_DIR/docs/SKILL_APPENDIX_FULL.md')\" = '$REMOTE_REPO_DIR/docs/SKILL_APPENDIX_FULL.md'; \
  test \"\$(readlink -f '$REMOTE_CLAUDE_SKILL_DIR/SKILL.md')\" = '$REMOTE_REPO_DIR/SKILL.md'; \
  test \"\$(readlink -f '$REMOTE_CLAUDE_SKILL_DIR/docs/SKILL_APPENDIX_FULL.md')\" = '$REMOTE_REPO_DIR/docs/SKILL_APPENDIX_FULL.md'; \
  test ! -L '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  test ! -L '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json'; \
  test ! -L '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js')\" = '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json')\" = '$REMOTE_OPENCLAW_PLUGIN_DIR/openclaw.plugin.json'; \
  test \"\$(readlink -f '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json')\" = '$REMOTE_OPENCLAW_PLUGIN_DIR/package.json'; \
  grep -n 'SKILL_APPENDIX_FULL.md' '$REMOTE_OPENCLAW_SKILL_DIR/SKILL.md'; \
  grep -n 'SKILL_APPENDIX_FULL.md' '$REMOTE_CLAUDE_SKILL_DIR/SKILL.md'; \
  grep -n 'before_prompt_build' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  grep -n 'mustCallFinanceQuerySystemContext' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  grep -n 'RemoteMCPClient' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  grep -n 'FINANCEQA_BIN' '$REMOTE_OPENCLAW_PLUGIN_DIR/dist/index.esm.js'; \
  grep -n 'final_answer' '$REMOTE_REPO_DIR/tests/scripts/claude_finance_final_answer.sh'; \
  grep -n 'claude_finance_final_answer.sh' '$REMOTE_REPO_DIR/tests/scripts/run_online_agent_final_answer_check.py'; \
  if command -v openclaw >/dev/null 2>&1; then openclaw skills list --json 2>&1 | grep -q '\"name\": \"finance\"'; fi"
else
  echo "[7/9] skip OpenClaw extension publish and connector verification (MODE=$MODE)"
fi

if deploy_connector && [[ "$RESTART_OPENCLAW_GATEWAY" == "1" ]]; then
  echo "[8/9] restart OpenClaw gateway so the updated extension runtime is loaded"
  ssh_remote "$SERVER" "set -e; \
    if command -v openclaw >/dev/null 2>&1; then \
      openclaw gateway restart; \
      sleep 3; \
      openclaw gateway status >/tmp/openclaw_gateway_status.txt; \
      grep -q 'RPC probe: ok' /tmp/openclaw_gateway_status.txt; \
      grep -q 'Runtime: running' /tmp/openclaw_gateway_status.txt; \
    fi"
else
  echo "[8/9] skip OpenClaw gateway restart (MODE=$MODE RESTART_OPENCLAW_GATEWAY=$RESTART_OPENCLAW_GATEWAY)"
fi

if deploy_server; then
  echo "[9/9] verify FinanceQA MCP service status"
  ssh_remote "$SERVER" "set -e; \
    '$REMOTE_FINANCEQA_BIN' version; \
    if systemctl is-enabled financeqa-mcp.service >/dev/null 2>&1; then \
      systemctl is-active --quiet financeqa-mcp.service; \
    fi"
else
  echo "[9/9] skip FinanceQA MCP service verification (MODE=$MODE)"
fi

echo "done."
