// Parity runner: drives the same simulator traffic against cs-gocpp and
// cs-ocppgo and writes a difference report.
//
//   bun run interop/parity/runner/main.ts [--only <glob>[,<glob>]] [--runs N]
//        [--skip-slow] [--out DIR] [--concurrency N]
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { cases, type Case } from "./cases";
import { describeScript } from "./scenarios";
import { Ctx, type Observed } from "./ctx";
import { LIBS, PARITY_DIR, Server, SIM_DIR, stable, type Lib } from "./lib";

const args = process.argv.slice(2);
const opt = (n: string) => { const i = args.indexOf(n); return i >= 0 ? args[i + 1] : undefined; };
const only = opt("--only")?.split(",").map((g) => new RegExp("^" + g.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".") + "$"));
const runs = Number(opt("--runs") ?? 2);
const skipSlow = args.includes("--skip-slow");
const concurrency = Number(opt("--concurrency") ?? 6);
const date = new Date().toISOString().slice(0, 10);
const outDir = resolve(opt("--out") ?? join(PARITY_DIR, "results", date));
const rawDir = join(outDir, "raw");

const selected = cases.filter((c) => !only || only.some((r) => r.test(c.id)));
let portCounter = 21000;

interface UnitResult { observed: Observed; error?: string; versions?: string }

async function runUnit(c: Case, lib: Lib, run: number): Promise<UnitResult> {
  const dir = join(rawDir, `run${run}`, c.id, lib);
  mkdirSync(dir, { recursive: true });
  const port = portCounter++;
  const server = new Server(lib, port, dir);
  const ctx = new Ctx(lib, server, dir, c.id);
  let error: string | undefined;
  let version: string | undefined;
  try {
    await server.start();
    const h: any = await server.http("/control/health");
    version = h.json?.version;
    await server.http("/control/events/reset", {});
    await Promise.race([
      c.run(ctx),
      new Promise((_, rej) => setTimeout(() => rej(new Error("case timeout")), c.timeoutMs ?? 45000)),
    ]);
  } catch (e) {
    error = String((e as Error).message ?? e);
    ctx.rec("HARNESS_ERROR", error);
  }
  const observed = await ctx.finish();
  await server.stop();
  writeFileSync(join(dir, "obs.json"), JSON.stringify(observed.obs, null, 1));
  writeFileSync(join(dir, "side.json"), JSON.stringify({ side: observed.side, diag: observed.diag, exit: server.exited }, null, 1));
  for (const s of ctx.sims) writeFileSync(join(dir, `sim-${s.name}.stdout`), s.stdoutRaw);
  return { observed, error, versions: version };
}

async function pool<T>(items: (() => Promise<T>)[], n: number): Promise<T[]> {
  const out: T[] = new Array(items.length);
  let i = 0;
  await Promise.all(Array.from({ length: n }, async () => {
    while (i < items.length) { const k = i++; out[k] = await items[k](); }
  }));
  return out;
}

// -------------------------------------------------------------- comparison

const LAYERS = ["steps", "wire", "simEvents", "serverEvents", "serverAlive"] as const;
type Layer = (typeof LAYERS)[number];
const layerDiff = (a: Observed, b: Observed): Layer[] => LAYERS.filter((l) => stable(a.obs[l]) !== stable(b.obs[l]));

function cell(o: Observed): string {
  const parts: string[] = [];
  for (const [k, v] of o.obs.steps) parts.push(`${k}=${k === "report" ? reportCell(v) : compact(v)}`);
  for (const [n, ev] of Object.entries(o.obs.simEvents)) {
    const d = ev.filter((e) => e.startsWith("disconnected"));
    if (d.length) parts.push(`sim ${n}: ${d.join(",")}`);
  }
  const sv = o.obs.serverEvents.filter((e: any) => e.kind === "disconnect").map((e: any) => `${e.cpId}:${e.cause}`);
  if (sv.length) parts.push(`srv disc ${sv.join(",")}`);
  if (!o.obs.serverAlive) parts.push("**SERVER DEAD**");
  return parts.join("; ");
}
function reportCell(v: any): string {
  if (!v || typeof v !== "object" || !("executionState" in v)) return compact(v);
  const a = (v.assertions ?? []).map((x: any) => x.status).join("/");
  return `${v.executionState}/${v.verdict}${a ? ` [${a}]` : ""} errs=${v.errorCount} nodes=${v.executedNodes?.length ?? "?"} csms-exchange=${v.transcript?.length ?? "?"}`;
}
function compact(v: unknown): string {
  let s = typeof v === "string" ? v : JSON.stringify(v);
  if (s === undefined) s = "undefined";
  s = s.replace(/"/g, "'");
  return s.length > 150 ? s.slice(0, 147) + "..." : s;
}
const esc = (s: string) => s.replace(/\|/g, "\\|").replace(/\n/g, " ");

function flat(o: Observed, l: Layer): string[] {
  const v: any = o.obs[l];
  if (l === "steps") return v.map(([k, x]: [string, unknown]) => `${k}: ${stable(x)}`);
  if (l === "wire" || l === "simEvents") return Object.entries(v).flatMap(([n, xs]) => (xs as string[]).map((x) => `${n}: ${x}`));
  if (l === "serverEvents") return v.map(stable);
  return [`serverAlive=${v}`];
}
function minus(a: string[], b: string[]): string[] {
  const rest = [...b];
  const out: string[] = [];
  for (const x of a) { const i = rest.indexOf(x); if (i >= 0) rest.splice(i, 1); else out.push(x); }
  return out;
}
const clip = (s: string) => (s.length > 600 ? s.slice(0, 597) + "..." : s);

// -------------------------------------------------------------------- main

function gitHead(): string {
  try { return Bun.spawnSync(["git", "rev-parse", "--short", "HEAD"], { cwd: PARITY_DIR }).stdout.toString().trim(); } catch { return "?"; }
}

async function main() {
  rmSync(rawDir, { recursive: true, force: true });
  mkdirSync(outDir, { recursive: true });
  const runnable = selected.filter((c) => !c.skip && !(skipSlow && c.slow));
  const skipped = selected.filter((c) => c.skip || (skipSlow && c.slow));
  console.log(`cases: ${runnable.length} runnable, ${skipped.length} skipped; runs=${runs}; sim=${SIM_DIR}`);
  const results: Record<string, Record<Lib, UnitResult>>[] = [];
  const versions: Partial<Record<Lib, string>> = {};
  for (let run = 1; run <= runs; run++) {
    const t0 = Date.now();
    const units: (() => Promise<void>)[] = [];
    const res: Record<string, Record<Lib, UnitResult>> = {};
    // Slow cases first so they overlap with the rest.
    const order = [...runnable].sort((a, b) => Number(!!b.slow) - Number(!!a.slow));
    for (const c of order) {
      res[c.id] = {} as Record<Lib, UnitResult>;
      for (const lib of LIBS) {
        units.push(async () => {
          const r = await runUnit(c, lib, run);
          res[c.id][lib] = r;
          if (r.versions) versions[lib] = r.versions;
          process.stdout.write(".");
        });
      }
    }
    await pool(units, concurrency);
    results.push(res);
    console.log(`\nrun ${run} done in ${((Date.now() - t0) / 1000).toFixed(0)}s`);
  }
  await writeReport(runnable, skipped, results, versions);
}

/** Short outcome of one group-E run. */
function eOutcome(o: Observed): { kind: "pass" | "fail" | "bounded"; text: string } {
  const st = new Map(o.obs.steps as [string, any][]);
  const rep: any = st.get("report");
  const calls = o.obs.steps.filter(([k]) => /^call\d+:/.test(k)).map(([k, v]: [string, any]) => {
    const a = k.split(":")[1];
    return `${a}${v?.callError ? ` CallError(${v.callError.code})` : v?.result?.status ? `=${v.result.status}` : v?.ok ? "=ok" : "=?"}`;
  });
  const asserts = (rep?.assertions ?? []).map((a: any) => `${a.id}:${a.status}`);
  const unfinished = st.get("unfinished") as any;
  const missed = o.obs.steps.find(([k]) => k.startsWith("missed:"));
  const expectProblems: string[] = Array.isArray(st.get("expect")) ? (st.get("expect") as string[]) : [];
  const bits = [rep ? `${rep.executionState}/${rep.verdict}` : "no report"];
  if (asserts.length) bits.push(`assertions ${asserts.join(", ")}`);
  if (calls.length) bits.push(`CSMS: ${calls.join("; ")}`);
  if (unfinished) bits.push(`stopped by runner at budget (node ${unfinished.node ?? "?"}, ${unfinished.state ?? "?"})`);
  if (st.has("expect")) bits.push(expectProblems.length ? `EXPECTED PATH NOT TAKEN (${expectProblems.join(" | ")})` : "expected path ok");
  if (missed) bits.push(`never reached ${missed[0].slice(7)}`);
  if (!o.obs.serverAlive) bits.push("SERVER DEAD");
  const kind = unfinished ? "bounded" : (!rep || missed || rep.executionState !== "completed" || rep.verdict === "FAIL" || expectProblems.length || !o.obs.serverAlive) ? "fail" : "pass";
  return { kind, text: bits.join("; ") };
}

async function eScripts(es: Case[]): Promise<string[]> {
  const rows: string[] = [];
  for (const c of es) rows.push(`| ${c.id.slice(2)} | ${esc(await describeScript(c.id.slice(2)))} |`);
  return ["### CSMS side per scenario", "", "What the runner sends (through `/control/call`) and when; \"on waiting for X\" = `scenario_status` shows state `waiting` with that expectation.", "", "| scenario | CSMS actions |", "| --- | --- |", ...rows, ""];
}

function eSection(runnable: Case[], r1: Record<string, Record<Lib, UnitResult>>): string[] {
  const es = runnable.filter((c) => c.group === "E");
  if (!es.length) return [];
  const rows: string[] = [];
  const bucket: Record<string, string[]> = { "pass both": [], "fail both": [], "bounded both": [], "differing": [] };
  for (const c of es) {
    const g = r1[c.id]["gocpp"].observed, o = r1[c.id]["ocpp-go"].observed;
    const og = eOutcome(g), oo = eOutcome(o);
    const layers = layerDiff(g, o);
    rows.push(`| ${c.id.slice(2)} | ${esc(og.text)} | ${esc(oo.text)} | ${layers.length ? `no (${layers.join(", ")})` : "yes"} |`);
    if (layers.length) bucket["differing"].push(c.id.slice(2));
    else if (og.kind === oo.kind) bucket[`${og.kind} both`].push(c.id.slice(2));
    else bucket["differing"].push(c.id.slice(2));
  }
  return [
    "## Group E: simulator scenarios",
    "",
    `${es.length} scenarios run. "same?" compares all layers (scenario report incl. the CSMS-initiated exchange it saw, control-call results, the full CP-side wire trace, server events, server alive).`,
    "",
    ...Object.entries(bucket).map(([k, v]) => `- ${k}: ${v.length}${v.length ? " (" + v.join(", ") + ")" : ""}`),
    "",
    "| scenario | gocpp result | ocpp-go result | same? |",
    "| --- | --- | --- | --- |",
    ...rows,
    "",
  ];
}

async function writeReport(runnable: Case[], skipped: Case[], results: Record<string, Record<Lib, UnitResult>>[], versions: Partial<Record<Lib, string>>) {
  const r1 = results[0];
  const flaky = new Map<string, Lib[]>();
  for (const c of runnable) {
    for (const lib of LIBS) {
      const base = stable(r1[c.id][lib].observed.obs);
      if (results.slice(1).some((r) => stable(r[c.id][lib].observed.obs) !== base)) {
        flaky.set(c.id, [...(flaky.get(c.id) ?? []), lib]);
      }
    }
  }
  const rows: string[] = [];
  const detail: string[] = [];
  let same = 0, diff = 0;
  for (const c of runnable) {
    const g = r1[c.id]["gocpp"], o = r1[c.id]["ocpp-go"];
    const layers = layerDiff(g.observed, o.observed);
    const fl = flaky.get(c.id);
    const verdict = (layers.length ? `DIFF (${layers.join(", ")})` : "same") + (fl ? ` FLAKY(${fl.join(",")})` : "");
    layers.length ? diff++ : same++;
    rows.push(`| ${c.id} | ${esc(cell(g.observed))} | ${esc(cell(o.observed))} | ${verdict} |`);
    if (layers.length || fl) {
      detail.push(`### ${c.id}\n\n${c.desc}\n`);
      for (const l of layers) {
        detail.push(`Layer \`${l}\`:\n`);
        const a = flat(g.observed, l), b = flat(o.observed, l);
        const onlyA = minus(a, b), onlyB = minus(b, a);
        for (const x of onlyA) detail.push(`- only gocpp: \`${clip(x)}\``);
        for (const x of onlyB) detail.push(`- only ocpp-go: \`${clip(x)}\``);
        if (!onlyA.length && !onlyB.length) detail.push("- same items, different order");
        detail.push("");
      }
      for (const lib of fl ?? []) {
        detail.push(`Flaky in ${lib}; run 2 differed in: ${LAYERS.filter((l) => results.slice(1).some((r) => stable(r[c.id][lib].observed.obs[l]) !== stable(r1[c.id][lib].observed.obs[l]))).join(", ")}\n`);
      }
      for (const lib of LIBS) {
        const side = r1[c.id][lib].observed.side;
        const t = (side.texts as string[] | undefined) ?? [];
        const extra = Object.entries(side).filter(([k]) => k !== "texts");
        if (t.length || extra.length) detail.push(`Side (${lib}): \`${compact({ ...Object.fromEntries(extra), texts: t.slice(0, 8) })}\`\n`);
      }
      const dg = stable(g.observed.diag) !== stable(o.observed.diag);
      if (dg) detail.push(`Diagnostics (log/error events, not compared): gocpp ${g.observed.diag.length}, ocpp-go ${o.observed.diag.length}\n`);
    }
  }
  const md = [
    `# Parity report ${date}`,
    "",
    `Same simulator traffic against \`cs-gocpp\` (this checkout, ${gitHead()}) and \`cs-ocppgo\` (${versions["ocpp-go"] ?? "?"}).`,
    `Simulator: \`${SIM_DIR}\` (JSON Lines mode, \`--reconnect-max-attempts 0\`). Runs: ${results.length}.`,
    "",
    "Re-run: `bun run interop/parity/runner/main.ts [--only <glob>] [--runs N] [--skip-slow]` (needs `make` in `interop/parity` first).",
    "",
    `Totals: ${runnable.length} cases run, ${same} same, ${diff} differ, ${flaky.size} flaky, ${skipped.length} skipped.`,
    "",
    "Compared layers: `steps` (what the simulator observed for each command or frame), `wire` (every frame in the simulator's trace, from the CP side, ids/timestamps/descriptions normalised), `simEvents` (connected/disconnected with close code), `serverEvents` (connect/disconnect cause/request/response_sent/panic), `serverAlive`. Library log/error events are not compared (they are library specific); free-text such as CallError descriptions is kept in `raw/run1/<case>/<lib>/side.json`.",
    "",
    "## Skipped",
    "",
    ...(skipped.length ? skipped.map((c) => `- ${c.id}: ${c.skip ?? "skipped by --skip-slow"}`) : ["none"]),
    "",
    "## Flaky",
    "",
    ...(flaky.size ? [...flaky].map(([id, l]) => `- ${id} (${l.join(", ")})`) : ["none"]),
    "",
    ...eSection(runnable, r1),
    ...(await eScripts(runnable.filter((c) => c.group === "E"))),
    "## Table",
    "",
    "| case | gocpp | ocpp-go | same? |",
    "| --- | --- | --- | --- |",
    ...rows,
    "",
    "## Differences in detail",
    "",
    ...(detail.length ? detail : ["none"]),
    "",
  ].join("\n");
  // Hand-written analysis kept next to the generated report is appended.
  const extra = existsSync(join(outDir, "analysis.md")) ? "\n" + readFileSync(join(outDir, "analysis.md"), "utf8") : "";
  writeFileSync(join(outDir, "report.md"), md + extra);
  console.log(`report: ${join(outDir, "report.md")}  same=${same} diff=${diff} flaky=${flaky.size}`);
}

await main();
process.exit(0);
