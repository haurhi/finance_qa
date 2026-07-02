#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import zlib from "node:zlib";

const args = parseArgs(process.argv.slice(2));
const snapshotPath = args.snapshot;
const outputPath = args.output;

if (!snapshotPath || !outputPath) {
  console.error("usage: financeqa_snapshot_to_sqlite.mjs --snapshot <snapshot.json[.gz]> --output <financeqa.sqlite>");
  process.exit(2);
}

const snapshot = readSnapshot(snapshotPath);
const tables = snapshot.tables ?? {};
const resolvedOutput = path.resolve(outputPath);
const tmpPath = `${resolvedOutput}.tmp-${process.pid}-${Date.now()}`;

fs.mkdirSync(path.dirname(resolvedOutput), { recursive: true });
fs.rmSync(tmpPath, { force: true });

try {
  const sql = buildSql(snapshot, tables);
  const result = spawnSync("sqlite3", [tmpPath], {
    input: sql,
    encoding: "utf8",
    maxBuffer: 1024 * 1024 * 128
  });
  if (result.status !== 0) {
    throw new Error(`sqlite3 failed exit=${result.status}\n${result.stderr}`);
  }
  fs.renameSync(tmpPath, resolvedOutput);
  console.log(`wrote FinanceQA SQLite mirror: ${resolvedOutput}`);
} catch (error) {
  fs.rmSync(tmpPath, { force: true });
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i += 1) {
    const item = argv[i];
    if (item === "--snapshot") {
      out.snapshot = argv[++i];
    } else if (item === "--output") {
      out.output = argv[++i];
    } else if (item === "--help" || item === "-h") {
      out.help = true;
    } else {
      throw new Error(`unknown argument: ${item}`);
    }
  }
  return out;
}

function readSnapshot(filePath) {
  const raw = fs.readFileSync(filePath);
  const text = filePath.endsWith(".gz") ? zlib.gunzipSync(raw).toString("utf8") : raw.toString("utf8");
  return JSON.parse(text);
}

function buildSql(snapshot, tables) {
  const lines = [
    ".bail on",
    "PRAGMA foreign_keys=OFF;",
    "BEGIN;",
    ...schemaSql(),
    ...insertRows("fin_contracts", [
      "contract_id",
      "customer_name",
      "contract_content",
      "contract_start_date",
      "contract_end_date",
      "settlement_cycle",
      "created_at",
      "updated_at"
    ], tables.fin_contracts),
    ...insertRows("fin_fund_income", [
      "id",
      "contract_id",
      "year_month",
      "source_report_type",
      "source_sheet_name",
      "quantity",
      "settlement_amount",
      "received_amount",
      "is_invoiced",
      "invoice_amount",
      "remarks",
      "invoice_open_offset_amount",
      "invoice_open_offset_reason",
      "contract_start_date",
      "contract_end_date",
      "settlement_cycle",
      "settlement_unit_price",
      "source_cell_notes",
      "created_at",
      "updated_at"
    ], tables.fin_fund_income),
    ...insertRows("fin_fund_income_groups", [
      "id",
      "customer_name",
      "year_month",
      "source_report_type",
      "source_sheet_name",
      "source_start_row",
      "source_end_row",
      "merge_range",
      "quantity",
      "settlement_amount",
      "received_amount",
      "is_invoiced",
      "invoice_amount",
      "remarks",
      "invoice_open_offset_amount",
      "invoice_open_offset_reason",
      "contract_start_date",
      "contract_end_date",
      "settlement_cycle",
      "settlement_unit_price",
      "source_cell_notes",
      "created_at",
      "updated_at"
    ], tables.fin_fund_income_groups),
    ...insertRows("fin_fund_income_group_members", [
      "id",
      "group_id",
      "contract_id",
      "source_row_number",
      "created_at",
      "updated_at"
    ], tables.fin_fund_income_group_members),
    ...insertRows("fin_cost_settlements", [
      "id",
      "contract_id",
      "year_month",
      "source_report_type",
      "source_sheet_name",
      "quantity",
      "settlement_amount",
      "is_invoiced",
      "invoice_amount",
      "paid_amount",
      "invoice_open_offset_amount",
      "invoice_open_offset_reason",
      "account_code",
      "contract_start_date",
      "contract_end_date",
      "settlement_cycle",
      "settlement_unit_price",
      "source_cell_notes",
      "created_at",
      "updated_at"
    ], tables.fin_cost_settlements),
    ...insertRows("fin_cost_settlement_groups", [
      "id",
      "customer_name",
      "year_month",
      "source_report_type",
      "source_sheet_name",
      "source_start_row",
      "source_end_row",
      "merge_range",
      "quantity",
      "settlement_amount",
      "is_invoiced",
      "invoice_amount",
      "paid_amount",
      "invoice_open_offset_amount",
      "invoice_open_offset_reason",
      "account_code",
      "contract_start_date",
      "contract_end_date",
      "settlement_cycle",
      "settlement_unit_price",
      "source_cell_notes",
      "created_at",
      "updated_at"
    ], tables.fin_cost_settlement_groups),
    ...insertRows("fin_cost_settlement_group_members", [
      "id",
      "group_id",
      "contract_id",
      "source_row_number",
      "created_at",
      "updated_at"
    ], tables.fin_cost_settlement_group_members),
    ...insertRows("fin_file_mappings", [
      "id",
      "table_type",
      "period",
      "company",
      "storage_key",
      "file_name",
      "description",
      "file_size",
      "source_file_hash",
      "source_version_id",
      "created_at",
      "updated_at"
    ], tables.fin_file_mappings),
    ...insertRows("bank_statement", [
      "company",
      "transaction_date",
      "credit_amount",
      "debit_amount",
      "counterparty_name",
      "summary"
    ], tables.fin_bank_statement),
    ...insertRows("journal", [
      "company",
      "period",
      "voucher_date",
      "voucher_no",
      "account_code",
      "account_name",
      "summary",
      "direction",
      "amount",
      "debit_amount",
      "credit_amount",
      "counterparty"
    ], tables.fin_journal),
    ...insertRows("balance_sheet", [
      "company",
      "period",
      "account_code",
      "account_name",
      "opening_balance",
      "closing_balance"
    ], tables.fin_balance_sheet),
    ...insertRows("balance_detail", [
      "company",
      "year",
      "period",
      "account_code",
      "account_name",
      "opening_debit",
      "opening_credit",
      "current_debit",
      "current_credit",
      "closing_debit",
      "closing_credit"
    ], tables.fin_balance_detail),
    ...insertRows("agent_patrol_snapshot_metadata", [
      "generated_at",
      "source_database",
      "source_schema",
      "exporter"
    ], [snapshot.metadata ?? {}]),
    "COMMIT;"
  ];
  return `${lines.join("\n")}\n`;
}

function schemaSql() {
  return [
    `CREATE TABLE fin_contracts (
      contract_id TEXT PRIMARY KEY,
      customer_name TEXT,
      contract_content TEXT,
      contract_start_date TEXT,
      contract_end_date TEXT,
      settlement_cycle TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_contracts_name ON fin_contracts(customer_name, contract_content);",
    `CREATE TABLE fin_fund_income (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      contract_id TEXT NOT NULL,
      year_month TEXT NOT NULL,
      source_report_type TEXT,
      source_sheet_name TEXT,
      quantity TEXT,
      settlement_amount REAL,
      received_amount REAL,
      is_invoiced TEXT,
      invoice_amount REAL,
      remarks TEXT,
      invoice_open_offset_amount REAL,
      invoice_open_offset_reason TEXT,
      contract_start_date TEXT,
      contract_end_date TEXT,
      settlement_cycle TEXT,
      settlement_unit_price TEXT,
      source_cell_notes TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_fund_income_contract_period ON fin_fund_income(contract_id, year_month);",
    `CREATE TABLE fin_fund_income_groups (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      customer_name TEXT NOT NULL,
      year_month TEXT NOT NULL,
      source_report_type TEXT,
      source_sheet_name TEXT,
      source_start_row INTEGER,
      source_end_row INTEGER,
      merge_range TEXT,
      quantity TEXT,
      settlement_amount REAL,
      received_amount REAL,
      is_invoiced TEXT,
      invoice_amount REAL,
      remarks TEXT,
      invoice_open_offset_amount REAL,
      invoice_open_offset_reason TEXT,
      contract_start_date TEXT,
      contract_end_date TEXT,
      settlement_cycle TEXT,
      settlement_unit_price TEXT,
      source_cell_notes TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_fund_income_groups_period ON fin_fund_income_groups(customer_name, year_month);",
    `CREATE TABLE fin_fund_income_group_members (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      group_id INTEGER NOT NULL,
      contract_id TEXT NOT NULL,
      source_row_number INTEGER,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_fund_income_group_members_group ON fin_fund_income_group_members(group_id);",
    "CREATE INDEX idx_fin_fund_income_group_members_contract ON fin_fund_income_group_members(contract_id);",
    `CREATE TABLE fin_cost_settlements (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      contract_id TEXT NOT NULL,
      year_month TEXT NOT NULL,
      source_report_type TEXT,
      source_sheet_name TEXT,
      quantity TEXT,
      settlement_amount REAL NOT NULL,
      is_invoiced TEXT,
      invoice_amount REAL,
      paid_amount REAL,
      invoice_open_offset_amount REAL,
      invoice_open_offset_reason TEXT,
      account_code TEXT,
      contract_start_date TEXT,
      contract_end_date TEXT,
      settlement_cycle TEXT,
      settlement_unit_price TEXT,
      source_cell_notes TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_cost_settlements_contract_period ON fin_cost_settlements(contract_id, year_month);",
    `CREATE TABLE fin_cost_settlement_groups (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      customer_name TEXT NOT NULL,
      year_month TEXT NOT NULL,
      source_report_type TEXT,
      source_sheet_name TEXT,
      source_start_row INTEGER,
      source_end_row INTEGER,
      merge_range TEXT,
      quantity TEXT,
      settlement_amount REAL,
      is_invoiced TEXT,
      invoice_amount REAL,
      paid_amount REAL,
      invoice_open_offset_amount REAL,
      invoice_open_offset_reason TEXT,
      account_code TEXT,
      contract_start_date TEXT,
      contract_end_date TEXT,
      settlement_cycle TEXT,
      settlement_unit_price TEXT,
      source_cell_notes TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_cost_settlement_groups_period ON fin_cost_settlement_groups(customer_name, year_month);",
    `CREATE TABLE fin_cost_settlement_group_members (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      group_id INTEGER NOT NULL,
      contract_id TEXT NOT NULL,
      source_row_number INTEGER,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_cost_settlement_group_members_group ON fin_cost_settlement_group_members(group_id);",
    "CREATE INDEX idx_fin_cost_settlement_group_members_contract ON fin_cost_settlement_group_members(contract_id);",
    `CREATE TABLE fin_file_mappings (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      table_type TEXT NOT NULL,
      period TEXT NOT NULL,
      company TEXT,
      storage_key TEXT NOT NULL,
      file_name TEXT NOT NULL,
      description TEXT,
      file_size INTEGER,
      source_file_hash TEXT,
      source_version_id TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_fin_files_table_type ON fin_file_mappings(table_type);",
    "CREATE INDEX idx_fin_files_period ON fin_file_mappings(period);",
    `CREATE TABLE agent_patrol_snapshot_metadata (
      generated_at TEXT,
      source_database TEXT,
      source_schema TEXT,
      exporter TEXT
    );`,
    ...compatibilitySchemaSql()
  ];
}

function compatibilitySchemaSql() {
  return [
    "CREATE TABLE income_statement (company TEXT, period TEXT, item_name TEXT, current_amount REAL, cumulative_amount REAL);",
    "CREATE TABLE bank_statement (company TEXT, transaction_date TEXT, credit_amount REAL, debit_amount REAL, counterparty_name TEXT, summary TEXT);",
    "CREATE TABLE journal (company TEXT, period TEXT, voucher_date TEXT, voucher_no TEXT, account_code TEXT, account_name TEXT, summary TEXT, direction TEXT, amount REAL, debit_amount REAL, credit_amount REAL, counterparty TEXT);",
    "CREATE TABLE balance_sheet (company TEXT, period TEXT, account_code TEXT, account_name TEXT, opening_balance REAL, closing_balance REAL);",
    "CREATE TABLE balance_detail (company TEXT, year INTEGER, period TEXT, account_code TEXT, account_name TEXT, opening_debit REAL, opening_credit REAL, current_debit REAL, current_credit REAL, closing_debit REAL, closing_credit REAL);",
    `CREATE TABLE dimensions (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      code TEXT UNIQUE NOT NULL,
      name TEXT NOT NULL,
      type TEXT,
      description TEXT,
      is_hierarchical BOOLEAN DEFAULT 0,
      is_active BOOLEAN DEFAULT 1,
      metadata TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    `CREATE TABLE dimension_members (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      dimension_id INTEGER NOT NULL,
      code TEXT NOT NULL,
      name TEXT NOT NULL,
      parent_id INTEGER,
      level INTEGER DEFAULT 0,
      path TEXT,
      is_active BOOLEAN DEFAULT 1,
      sort_order INTEGER DEFAULT 0,
      metadata TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      UNIQUE(dimension_id, code)
    );`,
    `CREATE TABLE mapping_rules (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      company TEXT NOT NULL,
      rule_name TEXT NOT NULL,
      priority INTEGER DEFAULT 100,
      account_code_pattern TEXT,
      account_name_pattern TEXT,
      summary_pattern TEXT,
      counterparty_pattern TEXT,
      dimension_code TEXT NOT NULL,
      member_code TEXT NOT NULL,
      allocation_ratio REAL DEFAULT 1.0,
      valid_from TEXT,
      valid_to TEXT,
      is_active BOOLEAN DEFAULT 1,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`,
    "CREATE INDEX idx_dimensions_code ON dimensions(code);",
    "CREATE INDEX idx_dimension_members_lookup ON dimension_members(dimension_id, code);",
    "CREATE INDEX idx_dimension_members_parent ON dimension_members(dimension_id, parent_id);",
    "CREATE INDEX idx_mapping_rules_company ON mapping_rules(company, is_active);",
    "CREATE INDEX idx_mapping_rules_priority ON mapping_rules(company, priority);",
    "CREATE TABLE meta_table_comments (table_name TEXT PRIMARY KEY, comment TEXT, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);",
    "CREATE TABLE meta_column_comments (table_name TEXT, column_name TEXT, comment TEXT, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(table_name, column_name));"
  ];
}

function insertRows(tableName, columns, rows) {
  if (!Array.isArray(rows) || rows.length === 0) {
    return [];
  }
  return rows.map((row) => {
    const values = columns.map((column) => sqlValue(row[column]));
    return `INSERT INTO ${tableName}(${columns.join(", ")}) VALUES (${values.join(", ")});`;
  });
}

function sqlValue(value) {
  if (value === null || value === undefined) {
    return "NULL";
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      return "NULL";
    }
    return String(value);
  }
  if (typeof value === "boolean") {
    return value ? "1" : "0";
  }
  if (typeof value === "object") {
    return quoteSql(JSON.stringify(value));
  }
  return quoteSql(String(value));
}

function quoteSql(value) {
  return `'${value.replaceAll("'", "''")}'`;
}
