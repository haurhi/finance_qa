#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

async function main() {
  try {
    const args = parseArgs(process.argv.slice(2));
    const snapshot = readSnapshot(args.snapshot);
    const limit = Number.isFinite(Number(args.limit)) ? Math.max(1, Number(args.limit)) : 5;
    const templates = buildCaseVariables(snapshot, limit);
    const payload = {
      version: 1,
      source: "financeqa_snapshot_case_variables",
      generated_at: new Date().toISOString(),
      snapshot_metadata: {
        generated_at: stringValue(snapshot.metadata.generated_at),
        source_database: stringValue(snapshot.metadata.source_database),
        source_schema: stringValue(snapshot.metadata.source_schema)
      },
      templates
    };

    fs.mkdirSync(path.dirname(args.output), { recursive: true });
    fs.writeFileSync(args.output, `${JSON.stringify(payload, null, 2)}\n`, "utf8");
    console.log(`wrote FinanceQA case variables: ${args.output}`);
  } catch (err) {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  }
}

function buildCaseVariables(snapshot, limit) {
  const contracts = contractMap(snapshot);
  const fundRows = financeRows(snapshot, "fin_fund_income", "received_amount", contracts);
  const costRows = financeRows(snapshot, "fin_cost_settlements", "paid_amount", contracts);
  return {
    finance_customer_receivable_unpaid: {
      customer_name: topValues(fundRows, (row) => row.customer_name, limit)
    },
    finance_supplier_payable_unpaid: {
      supplier_name: topValues(costRows, (row) => row.customer_name, limit)
    },
    finance_contract_receivable_unpaid: {
      contract_name: topValues(fundRows, (row) => row.contract_content, limit)
    },
    finance_single_project_payable_unpaid: {
      project_name: topValues(costRows, (row) => row.contract_content, limit)
    }
  };
}

function financeRows(snapshot, tableName, movementField, contracts) {
  return arrayValue(snapshot.tables[tableName])
    .map((item) => asRecord(item) ?? {})
    .map((row) => {
      const contractId = stringValue(row.contract_id) ?? "";
      const contract = contracts.get(contractId) ?? {};
      const settlement = numericValue(row.settlement_amount);
      const movement = numericValue(row[movementField]);
      return {
        contract_id: contractId,
        customer_name: stringValue(row.customer_name) ?? stringValue(contract.customer_name) ?? "",
        contract_content: stringValue(contract.contract_content) ?? "",
        open_amount: Math.max(settlement - movement, 0)
      };
    })
    .filter((row) => row.open_amount > 0);
}

function topValues(rows, selector, limit) {
  const totals = new Map();
  for (const row of rows) {
    const value = selector(row).trim();
    if (!value) continue;
    totals.set(value, round2((totals.get(value) ?? 0) + row.open_amount));
  }
  return [...totals.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, limit)
    .map(([value]) => value);
}

function contractMap(snapshot) {
  return new Map(arrayValue(snapshot.tables.fin_contracts)
    .map((item) => asRecord(item) ?? {})
    .map((row) => [stringValue(row.contract_id) ?? "", {
      customer_name: stringValue(row.customer_name) ?? "",
      contract_content: stringValue(row.contract_content) ?? ""
    }])
    .filter(([contractId]) => contractId));
}

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const item = argv[index];
    if (!item.startsWith("--")) {
      throw new Error(`unexpected argument: ${item}`);
    }
    const key = item.slice(2).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      throw new Error(`missing value for ${item}`);
    }
    parsed[key] = value;
    index += 1;
  }
  if (!parsed.snapshot) throw new Error("missing --snapshot");
  if (!parsed.output) throw new Error("missing --output");
  return parsed;
}

function readSnapshot(snapshotPath) {
  const bytes = fs.readFileSync(snapshotPath);
  const text = snapshotPath.endsWith(".gz") ? zlib.gunzipSync(bytes).toString("utf8") : bytes.toString("utf8");
  const parsed = JSON.parse(text);
  return {
    metadata: asRecord(parsed.metadata) ?? {},
    tables: asRecord(parsed.tables) ?? {}
  };
}

function round2(value) {
  return Math.round((value + Number.EPSILON) * 100) / 100;
}

function numericValue(value) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value.replace(/,/g, ""));
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function stringValue(value) {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function arrayValue(value) {
  return Array.isArray(value) ? value : [];
}

function asRecord(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : undefined;
}

main();
