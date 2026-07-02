#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA="${FINANCEQA_PG_SCHEMA:-}"
OUTPUT="${FINANCEQA_SNAPSHOT_OUTPUT:-tmp/reference-snapshots/financeqa-latest.json.gz}"
SQLITE_MIRROR_OUTPUT="${FINANCEQA_SQLITE_MIRROR_OUTPUT:-}"
CASE_VARIABLES_OUTPUT="${FINANCEQA_CASE_VARIABLES_OUTPUT:-${FINANCEQA_CASE_VARIABLES_FILE:-}}"

if [[ -z "$SCHEMA" ]]; then
  echo "FINANCEQA_PG_SCHEMA is required" >&2
  exit 2
fi
if [[ ! "$SCHEMA" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "invalid FINANCEQA_PG_SCHEMA=$SCHEMA" >&2
  exit 2
fi

mkdir -p "$(dirname "$OUTPUT")"
tmp="$(mktemp "${OUTPUT}.tmp.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

psql -X -v ON_ERROR_STOP=1 -q -tA <<SQL | gzip -c > "$tmp"
BEGIN READ ONLY;
SET LOCAL search_path TO "$SCHEMA", public;
WITH payload AS (
  SELECT jsonb_build_object(
    'metadata', jsonb_build_object(
      'generated_at', to_char(clock_timestamp(), 'YYYY-MM-DD"T"HH24:MI:SSOF'),
      'source_database', current_database(),
      'source_schema', current_schema(),
      'exporter', 'agent-patrol/examples/golden/export_financeqa_snapshot.sh'
    ),
    'tables', jsonb_build_object(
      'fin_contracts',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.contract_id), '[]'::jsonb) FROM fin_contracts t),
      'fin_fund_income',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.year_month, t.contract_id), '[]'::jsonb) FROM fin_fund_income t),
      'fin_fund_income_groups',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.year_month, t.id), '[]'::jsonb) FROM fin_fund_income_groups t),
      'fin_fund_income_group_members',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.group_id, t.contract_id, t.source_row_number), '[]'::jsonb) FROM fin_fund_income_group_members t),
      'fin_cost_settlements',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.year_month, t.contract_id), '[]'::jsonb) FROM fin_cost_settlements t),
      'fin_cost_settlement_groups',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.year_month, t.id), '[]'::jsonb) FROM fin_cost_settlement_groups t),
      'fin_cost_settlement_group_members',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.group_id, t.contract_id, t.source_row_number), '[]'::jsonb) FROM fin_cost_settlement_group_members t),
      'fin_bank_statement',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.transaction_date, t.counterparty_name, t.summary), '[]'::jsonb) FROM fin_bank_statement t),
      'fin_balance_detail',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.period, t.account_code, t.account_name), '[]'::jsonb) FROM fin_balance_detail t),
      'fin_balance_sheet',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.period, t.account_code, t.account_name), '[]'::jsonb) FROM fin_balance_sheet t),
      'fin_journal',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.period, t.voucher_date, t.voucher_no, t.account_code), '[]'::jsonb) FROM fin_journal t),
      'fin_file_mappings',
        (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.table_type, t.period, t.file_name), '[]'::jsonb) FROM fin_file_mappings t)
    )
  ) AS body
)
SELECT body::text FROM payload;
COMMIT;
SQL

mv "$tmp" "$OUTPUT"
trap - EXIT
echo "wrote FinanceQA snapshot: $OUTPUT"

if [[ -n "$CASE_VARIABLES_OUTPUT" ]]; then
  node "$SCRIPT_DIR/financeqa_snapshot_case_variables.mjs" \
    --snapshot "$OUTPUT" \
    --output "$CASE_VARIABLES_OUTPUT"
fi

if [[ -n "$SQLITE_MIRROR_OUTPUT" ]]; then
  node "$SCRIPT_DIR/financeqa_snapshot_to_sqlite.mjs" \
    --snapshot "$OUTPUT" \
    --output "$SQLITE_MIRROR_OUTPUT"
fi
