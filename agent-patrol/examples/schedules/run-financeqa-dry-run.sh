#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$DEFAULT_ROOT"

ENV_FILE="${AGENT_PATROL_ENV_FILE:-examples/schedules/financeqa-daily.env}"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

if [[ -n "${AGENT_PATROL_FINANCEQA_ENV_FILE:-}" && -r "${AGENT_PATROL_FINANCEQA_ENV_FILE:-}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$AGENT_PATROL_FINANCEQA_ENV_FILE"
  set +a
fi

ROOT="${AGENT_PATROL_ROOT:-$DEFAULT_ROOT}"
cd "$ROOT"

if [[ "${AGENT_PATROL_LIVE:-}" != "1" ]]; then
  echo "Refusing dry-run: set AGENT_PATROL_LIVE=1 after manual validation." >&2
  exit 2
fi

: "${OPENCLAW_AGENT_CMD:?OPENCLAW_AGENT_CMD is required}"
: "${FINANCEQA_MCP_URL:?FINANCEQA_MCP_URL is required}"

require_production_safety() {
  if [[ "${AGENT_PATROL_ENV:-}" != "production" ]]; then
    return 0
  fi

  case "${AGENT_PATROL_PREPARE_CMD:-}" in
    *prepare-financeqa-snapshot-mirror.sh*)
      echo "refusing production dry-run: AGENT_PATROL_PREPARE_CMD must not use prepare-financeqa-snapshot-mirror.sh" >&2
      return 2
      ;;
  esac
}

require_production_safety

if [[ -z "${FINANCEQA_MCP_READ_TOKEN:-}" && -n "${FINANCEQA_MCP_READ_TOKEN_FILE:-}" && -r "$FINANCEQA_MCP_READ_TOKEN_FILE" ]]; then
  FINANCEQA_MCP_READ_TOKEN="$(<"$FINANCEQA_MCP_READ_TOKEN_FILE")"
  export FINANCEQA_MCP_READ_TOKEN
fi

: "${FINANCEQA_MCP_READ_TOKEN:?FINANCEQA_MCP_READ_TOKEN or FINANCEQA_MCP_READ_TOKEN_FILE is required}"

generate_seed() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen
    return
  fi
  if [[ -r /proc/sys/kernel/random/uuid ]]; then
    cat /proc/sys/kernel/random/uuid
    return
  fi
  date +%s%N
}

run_cleanup_command() {
  local cleanup_cmd="${AGENT_PATROL_CLEANUP_CMD:-}"

  if [[ "${AGENT_PATROL_CLEANUP_SESSIONS:-1}" != "1" ]]; then
    echo "$(date -Is) skip agent session cleanup: disabled"
    return 0
  fi
  if [[ -z "$cleanup_cmd" ]]; then
    echo "$(date -Is) skip agent session cleanup: no AGENT_PATROL_CLEANUP_CMD"
    return 0
  fi
  if [[ -x "$cleanup_cmd" ]]; then
    "$cleanup_cmd"
    return
  fi
  if [[ -f "$cleanup_cmd" ]]; then
    bash "$cleanup_cmd"
    return
  fi

  echo "$(date -Is) skip agent session cleanup: missing cleanup_cmd=$cleanup_cmd"
}

run_reference_export_command() {
  local export_cmd="${AGENT_PATROL_REFERENCE_EXPORT_CMD:-}"

  if [[ -z "$export_cmd" ]]; then
    echo "$(date -Is) skip FinanceQA reference export: no AGENT_PATROL_REFERENCE_EXPORT_CMD"
    return 0
  fi
  if [[ -x "$export_cmd" ]]; then
    "$export_cmd"
    return
  fi
  if [[ -f "$export_cmd" ]]; then
    bash "$export_cmd"
    return
  fi

  echo "$(date -Is) missing FinanceQA reference export command=$export_cmd" >&2
  return 2
}

run_prepare_command() {
  local prepare_cmd="${AGENT_PATROL_PREPARE_CMD:-}"

  if [[ -z "$prepare_cmd" ]]; then
    echo "$(date -Is) skip FinanceQA dry-run prepare: no AGENT_PATROL_PREPARE_CMD"
    return 0
  fi
  if [[ -x "$prepare_cmd" ]]; then
    "$prepare_cmd"
    return
  fi
  if [[ -f "$prepare_cmd" ]]; then
    bash "$prepare_cmd"
    return
  fi

  echo "$(date -Is) missing FinanceQA dry-run prepare command=$prepare_cmd" >&2
  return 2
}

run_doctor_command() {
  if [[ "${AGENT_PATROL_RUN_DOCTOR:-0}" != "1" ]]; then
    echo "$(date -Is) skip patrol doctor: disabled"
    return 0
  fi

  local args=(run start -- doctor --config "$CONFIG")
  if [[ "${AGENT_PATROL_REQUIRE_GOLDEN_REFERENCE:-0}" == "1" ]]; then
    args+=(--require-golden-reference)
  fi
  if [[ "${AGENT_PATROL_REQUIRE_RESOLVED_ENV:-0}" == "1" ]]; then
    args+=(--require-resolved-env)
  fi
  npm "${args[@]}"
}

prune_report_dirs() {
  local retention_days="${AGENT_PATROL_REPORT_RETENTION_DAYS:-}"
  if [[ -z "$retention_days" ]]; then
    echo "$(date -Is) skip report retention pruning: no AGENT_PATROL_REPORT_RETENTION_DAYS"
    return 0
  fi
  if [[ ! "$retention_days" =~ ^[0-9]+$ ]]; then
    echo "$(date -Is) skip report retention pruning: invalid retention_days=$retention_days" >&2
    return 2
  fi
  if [[ ! -d "$OUT_BASE" ]]; then
    echo "$(date -Is) skip report retention pruning: missing output_dir=$OUT_BASE"
    return 0
  fi

  local deleted=0
  while IFS= read -r report_dir; do
    rm -rf "$report_dir"
    deleted=$((deleted + 1))
  done < <(find "$OUT_BASE" -mindepth 1 -maxdepth 1 -type d -name '20[0-9][0-9]*' -mtime +"$retention_days" -print)
  echo "$(date -Is) pruned old report directories deleted=$deleted retention_days=$retention_days output_dir=$OUT_BASE"
}

read_business_status() {
  local summary_file="$1"
  node -e '
const fs = require("fs");
const file = process.argv[1];
try {
  const summary = JSON.parse(fs.readFileSync(file, "utf8"));
  const runnerHealthPassed = summary?.aggregate?.runnerHealthPassed;
  if (runnerHealthPassed === false) {
    console.log("runner_invalid");
    process.exit(0);
  }
  const validBusinessThresholdPassed = summary?.aggregate?.validBusinessThresholdPassed;
  if (validBusinessThresholdPassed === true) {
    console.log("valid_business_threshold_passed");
    process.exit(0);
  }
  if (validBusinessThresholdPassed === false) {
    console.log("valid_business_threshold_failed");
    process.exit(0);
  }
  const thresholdPassed = summary?.aggregate?.thresholdPassed;
  if (thresholdPassed === true) {
    console.log("threshold_passed");
    process.exit(0);
  }
  if (thresholdPassed === false) {
    console.log("threshold_failed");
    process.exit(0);
  }
  console.error("summary missing aggregate.thresholdPassed");
  process.exit(3);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(3);
}
' "$summary_file"
}

OUT_BASE="${AGENT_PATROL_OUTPUT_DIR:-tmp/financeqa-dry-run}"
CONFIG="${AGENT_PATROL_CONFIG:-presets/financeqa.yaml}"
SUITE="${AGENT_PATROL_SUITE:-smoke}"
SEED="${AGENT_PATROL_SEED:-$(generate_seed)}"
RUN_ID="${AGENT_PATROL_RUN_ID:-$(date +%Y%m%dT%H%M%S)}"
LOG_FILE="${AGENT_PATROL_LOG_FILE:-$OUT_BASE/dry-run.log}"
LOCK_FILE="${AGENT_PATROL_LOCK_FILE:-$OUT_BASE/.financeqa-dry-run.lock}"
FAIL_ON_THRESHOLD="${AGENT_PATROL_FAIL_ON_THRESHOLD:-0}"

mkdir -p "$OUT_BASE"

if command -v flock >/dev/null 2>&1; then
  exec 9>"$LOCK_FILE"
  if ! flock -n 9; then
    echo "$(date -Is) another FinanceQA dry-run is already running; skip" >> "$LOG_FILE"
    exit 0
  fi
fi

{
  echo "$(date -Is) start FinanceQA dry-run config=$CONFIG suite=$SUITE seed=$SEED"
  run_reference_export_command
  run_doctor_command
  run_prepare_command
  set +e
  npm run start -- run \
    --config "$CONFIG" \
    --suite "${AGENT_PATROL_SUITE:-smoke}" \
    --seed "$SEED" \
    --out "$OUT_BASE/$RUN_ID"
  npm_rc=$?
  set -e
  cleanup_rc=0
  run_cleanup_command || cleanup_rc=$?
  if [[ "$cleanup_rc" -ne 0 ]]; then
    echo "$(date -Is) agent session cleanup failed exit=$cleanup_rc"
  fi
  retention_rc=0
  prune_report_dirs || retention_rc=$?
  if [[ "$retention_rc" -ne 0 ]]; then
    echo "$(date -Is) report retention pruning failed exit=$retention_rc"
  fi
  summary_file="$OUT_BASE/$RUN_ID/summary.json"
  report_status="missing"
  business_status="unknown"
  service_rc="$npm_rc"
  if [[ -f "$summary_file" ]]; then
    report_status="generated"
    if business_status="$(read_business_status "$summary_file")"; then
      if [[ "$npm_rc" -eq 1 && ( "$business_status" == "threshold_failed" || "$business_status" == "valid_business_threshold_failed" ) && "$FAIL_ON_THRESHOLD" != "1" ]]; then
        service_rc=0
      fi
    else
      business_status="summary_parse_failed"
      service_rc=2
    fi
  elif [[ "$npm_rc" -eq 0 ]]; then
    service_rc=2
  fi
  if [[ "$service_rc" -eq 0 && "$retention_rc" -ne 0 ]]; then
    service_rc="$retention_rc"
  fi
  echo "$(date -Is) finish FinanceQA dry-run config=$CONFIG suite=$SUITE seed=$SEED out=$OUT_BASE/$RUN_ID cli_exit=$npm_rc report_status=$report_status business_status=$business_status service_exit=$service_rc"
  exit "$service_rc"
} >> "$LOG_FILE" 2>&1
