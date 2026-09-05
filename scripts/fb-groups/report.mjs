// Builds a readable log of what the runner did, from logs/YYYY-MM-DD.log.
//
//   node report.mjs                 today's log -> logs/report-YYYY-MM-DD.md
//   node report.mjs 2026-09-05      that day
//   node report.mjs 2026-09-05 --out ../../docs/social/groups-log-2026-09-05.md
//
// One row per group attempt (a group can appear twice if a run was
// restarted), in order, with the outcome the runner reported and the note
// it gave. Dry runs are listed separately and never counted as posts.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
const day = args.find((a) => /^\d{4}-\d{2}-\d{2}$/.test(a)) || new Date().toISOString().slice(0, 10);
const outIdx = args.indexOf("--out");
const out = outIdx >= 0 && args[outIdx + 1] ? path.resolve(here, args[outIdx + 1]) : path.join(here, "logs", `report-${day}.md`);
const src = path.join(here, "logs", `${day}.log`);
if (!fs.existsSync(src)) {
  console.error("no log for " + day + " at " + src);
  process.exit(1);
}

const lines = fs.readFileSync(src, "utf8").split(/\r?\n/);
const runs = [];
let run = null;
for (const line of lines) {
  const m = line.match(/^(\S+) (.*)$/);
  if (!m) continue;
  const [, ts, rest] = m;
  let x;
  if ((x = rest.match(/^rota (\S+): (\d+) of (\d+) due groups in this run( \(dry run\))?/))) {
    run = { started: ts, planned: +x[2], dueTotal: +x[3], dry: !!x[4], rows: [], stops: [] };
    runs.push(run);
  } else if (run && (x = rest.match(/^\[(\d+)\/(\d+)\] #(\d+) (.*?) \((\w+)\) (https?:\S+)/))) {
    run.rows.push({ at: ts, n: +x[1], id: +x[3], name: x[4], kind: x[5], url: x[6], outcome: "", note: "", recorded: "" });
  } else if (run && run.rows.length && (x = rest.match(/^\s+(posted|retry|blocked|failed|dry|signed-out|checkpoint)(?:: (.*))?$/))) {
    const r = run.rows[run.rows.length - 1];
    r.outcome = x[1];
    r.note = x[2] || "";
    r.at = ts;
  } else if (run && run.rows.length && (x = rest.match(/^\s+-> recorded (\w+) \((.*)\)/))) {
    run.rows[run.rows.length - 1].recorded = x[2];
  } else if (run && (x = rest.match(/^\s*!!\s*(.*)$/))) {
    run.stops.push({ at: ts, text: x[1] });
  } else if (run && /session is /.test(rest)) {
    run.session = rest.replace(/^session is /, "");
  }
}

const local = (iso) => new Date(iso).toLocaleTimeString("en-ZA", { hour: "2-digit", minute: "2-digit", timeZone: "Africa/Johannesburg" });
const esc = (s) => String(s).replace(/\|/g, "\\|");
let md = `# Facebook groups: runner log for ${day}\n\nBuilt by \`scripts/fb-groups/report.mjs\` from \`logs/${day}.log\`. Times are South African (SAST).\n\n`;
const totals = { posted: 0, retry: 0, blocked: 0, failed: 0 };
runs.forEach((r, i) => {
  md += `## Run ${i + 1}${r.dry ? " (dry run, nothing posted)" : ""}: started ${local(r.started)}, ${r.planned} of ${r.dueTotal} due groups\n\n`;
  if (r.session) md += `Session: ${r.session}\n\n`;
  md += `| # | Time | Group | Kind | Outcome | Note |\n|---|---|---|---|---|---|\n`;
  for (const row of r.rows) {
    md += `| ${row.n} | ${local(row.at)} | [${esc(row.name)}](${row.url}) | ${row.kind} | **${row.outcome || "(no result)"}** | ${esc(row.note)}${row.recorded ? ` · recorded: ${esc(row.recorded)}` : ""} |\n`;
    if (!r.dry && totals[row.outcome] !== undefined) totals[row.outcome]++;
  }
  const counts = {};
  for (const row of r.rows) counts[row.outcome || "none"] = (counts[row.outcome || "none"] || 0) + 1;
  md += `\n${Object.entries(counts).map(([k, v]) => `${k}: ${v}`).join(", ")}\n\n`;
  for (const s of r.stops) md += `- ${local(s.at)} stop: ${esc(s.text)}\n`;
  if (r.stops.length) md += "\n";
});
md += `## Day total (live runs)\n\nposted ${totals.posted}, retry ${totals.retry}, blocked ${totals.blocked}, failed ${totals.failed}.\n\n"posted" includes posts awaiting a group's admins. "retry" is booked again in three days, "failed" tomorrow, "blocked" is switched off on the console with the reason.\n`;
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, md);
console.log(`wrote ${out}: ${runs.length} run(s), ${runs.reduce((n, r) => n + r.rows.length, 0)} rows, posted ${totals.posted}`);
