#!/usr/bin/env python3
"""Builds report.md from the run directories under results/<date>/ (summary.json + timeline.csv)."""
import csv, json, os, re, statistics as st, sys, glob

BARS = " ▁▂▃▄▅▆▇█"

def spark(vals, width=60):
    if not vals: return ""
    n = len(vals); out = []
    for i in range(width):
        a, b = i * n // width, max((i + 1) * n // width, i * n // width + 1)
        out.append(max(vals[a:b]))
    lo, hi = min(out), max(out)
    return "".join(BARS[0 if hi == lo else int((v - lo) / (hi - lo) * 8)] for v in out) + f"  (min {lo:.4g}, max {hi:.4g})"

def load(d):
    rows = []
    with open(os.path.join(d, "timeline.csv")) as f:
        for r in csv.DictReader(f):
            rows.append({k: (float(v) if re.match(r"^-?[\d.]+$", v) else v) for k, v in r.items()})
    merge_procs(d, rows)
    return rows


def merge_procs(d, rows):
    """Split topology: the timeline's server columns describe only the /control backend, so
    srv_rss_mb / srv_cpu_cores / srv_threads / srv_fds become the sum over all gateway and backend
    processes in procs.csv (sampled on the same clock), and gw_/be_ column pairs are added."""
    p = os.path.join(d, "procs.csv")
    if not os.path.exists(p):
        return
    by = {}
    with open(p) as f:
        for r in csv.DictReader(f):
            if not r["t_s"] or r["t_s"] == "t_s":
                continue
            t = float(r["t_s"]); a = by.setdefault(t, {})
            for role in (r["role"], "all"):
                v = a.setdefault(role, [0.0, 0.0, 0.0, 0.0])
                v[0] += float(r["rss_mb"]); v[1] += float(r["cpu_cores"]); v[2] += float(r["threads"]); v[3] += float(r["fds"])
    if not by:
        return
    ts = sorted(by)
    for row in rows:
        t = min(ts, key=lambda x: abs(x - row["t_s"]))
        if abs(t - row["t_s"]) > 6:
            continue
        a = by[t]
        for role in ("gateway", "backend"):
            v = a.get(role, [0, 0, 0, 0]); row[role[:2] + "_rss_mb"] = v[0]; row[role[:2] + "_cpu_cores"] = v[1]
        v = a["all"]; row["srv_rss_mb"], row["srv_cpu_cores"], row["srv_threads"], row["srv_fds"] = v

def slope(xs, ys):
    if len(xs) < 3: return 0.0
    mx, my = st.mean(xs), st.mean(ys)
    den = sum((x - mx) ** 2 for x in xs)
    return sum((x - mx) * (y - my) for x, y in zip(xs, ys)) / den if den else 0.0

def fmt(p, k): return f"{p[k]:.1f}" if p and k in p else "-"

def lat_row(name, s):
    out = [name]
    for a in ("BootNotification", "Heartbeat", "MeterValues"):
        p = s["latency_steady_ms"].get(a)
        out.append(f"{fmt(p,'P50')} / {fmt(p,'P95')} / {fmt(p,'P99')}")
    for a in ("BootNotification", "Heartbeat", "MeterValues"):
        p = s["latency_restart_windows_ms"].get(a)
        out.append(f"{fmt(p,'P50')} / {fmt(p,'P95')} / {fmt(p,'P99')}")
    return out

GC_RE = re.compile(r"gc \d+ @([\d.]+)s \d+%: [\d.]+\+([\d.]+)\+[\d.]+ ms clock, [\d.]+\+([\d.]+)/([\d.]+)/([\d.]+)\+[\d.]+ ms cpu, (\d+)->(\d+)->(\d+) MB.* (\d+) MB stacks")

def gc_lines(d):
    """GODEBUG=gctrace=1 lines of server.log: [t, mark_wall_ms, assist_ms, bg_ms, idle_ms, heap_before, heap_after, live, stacks]."""
    p = os.path.join(d, "server.log")
    out = []
    if os.path.exists(p):
        for line in open(p, errors="replace"):
            m = GC_RE.match(line)
            if m: out.append([float(x) for x in m.groups()])
    return out

def highrate_rows(d, s):
    """One row per step: allocation, GC and scheduler columns for the second half of the step."""
    rows = s["_rows"]
    if not rows or "alloc_kb_req" not in rows[0]: return []
    gcs = gc_lines(d)
    out = []
    for i, sp in enumerate(s["config"]["Steps"]):
        pts = [r for r in rows if r["step"] == i and r["srv_rss_mb"] > 0 and r["alloc_kb_req"] >= 0]
        tail = pts[len(pts) // 2:]
        if len(tail) < 2: continue
        t0, t1 = tail[0]["t_s"], tail[-1]["t_s"]
        g = [x for x in gcs if t0 <= x[0] <= t1]
        mark = f"{100 * sum(x[1] for x in g) / max(t1 - t0, 1) / 1000:.1f}%" if len(g) > 1 else "-"
        lat = s["latency_by_step_steady_ms"].get(str(i), {}).get("Heartbeat")
        m = lambda k: st.mean(r[k] for r in tail)
        out.append([sp["N"], sp.get("Speed", 1), f"{m('msgs_per_s'):.0f}", f"{m('srv_cpu_cores'):.2f}", f"{max(r['srv_rss_mb'] for r in pts):.0f}",
            f"{m('alloc_mb_s'):.0f}", f"{m('alloc_kb_req'):.2f}", f"{m('gc_cycles_s'):.2f}", mark, f"{m('gc_cpu_cores'):.2f}", f"{m('gc_assist_cores'):.2f}",
            f"{max(r['sched_p99_ms'] for r in tail):.2f}", f"{m('heap_obj_mb'):.0f} + {m('stacks_mb'):.0f}",
            f"{fmt(lat,'P50')} / {fmt(lat,'P95')} / {fmt(lat,'P99')}"])
    return out

HIGHRATE_HEAD = ["N", "msg speed x", "msgs/s", "CPU cores", "max RSS MB", "alloc MB/s", "alloc KB/req", "GC cycles/s", "in-mark share (gctrace)",
                 "GC CPU cores", "GC assist cores", "sched latency p99 ms (max window)", "heap objects + stacks MB", "Heartbeat p50 / p95 / p99 ms"]

def table(head, rows):
    o = ["| " + " | ".join(head) + " |", "|" + "|".join("---" for _ in head) + "|"]
    o += ["| " + " | ".join(str(c) for c in r) + " |" for r in rows]
    return "\n".join(o)

def analyse(d):
    s = json.load(open(os.path.join(d, "summary.json")))
    s["restarts"] = s["restarts"] or []
    s["freezes"] = s["freezes"] or []
    rows = load(d)
    s["_rows"] = rows
    ramp = s["config"]["Ramp"] / 1e9 if "Ramp" in s["config"] else 60
    kills = [r["kill_at_s"] for r in s["restarts"]]
    # steady segments: from end of recovery (+60s) to next kill (or end)
    ends = sorted(kills) + [rows[-1]["t_s"]]
    starts = [ramp + 60] + [k + 90 for k in sorted(kills)]
    segs = []
    for a, b in zip(starts, ends):
        pts = [r for r in rows if a <= r["t_s"] <= b - 5 and r["srv_rss_mb"] > 0 and r["step"] == 0]
        if len(pts) < 4: continue
        xs = [r["t_s"] / 60 for r in pts]
        segs.append({
            "from": a, "to": b, "n": len(pts),
            "rss_slope": slope(xs, [r["srv_rss_mb"] for r in pts]),
            "gor_slope": slope(xs, [r["srv_goroutines"] for r in pts if r["srv_goroutines"] > 0] or [0] * len(xs)) if all(r["srv_goroutines"] > 0 for r in pts) else float("nan"),
            "gor_per_conn": st.mean(r["srv_goroutines"] / max(r["server_connected"], 1) for r in pts if r["srv_goroutines"] > 0) if any(r["srv_goroutines"] > 0 for r in pts) else float("nan"),
            "fd_slope": slope(xs, [r["srv_fds"] for r in pts]),
            "rss0": pts[0]["srv_rss_mb"], "rss1": pts[-1]["srv_rss_mb"],
            "conn_mean": st.mean(r["server_connected"] for r in pts),
        })
    s["_segs"] = segs
    return s

def errs_from_log(d):
    c = {}
    p = os.path.join(d, "server.log")
    if not os.path.exists(p): return c
    for line in open(p, errors="replace"):
        for pat in ("panic", "fatal error", "deadlock", "too many open", "concurrent map", "out of memory", "goroutine "):
            if pat in line:
                c[pat] = c.get(pat, 0) + 1
    return c

def section(name, d, s):
    rows = s["_rows"]; cfg = s["config"]
    steady = [r for r in rows if r["step"] == 0 and r["t_s"] > 120 and r["srv_rss_mb"] > 0]
    up = [r["chargers_up"] for r in rows]
    o = [f"### {name}\n"]
    fr = s["freezes"] or []
    o.append(f"- Freeze detected: **{'YES (%d)' % len(fr) if fr else 'no'}**; server crashes: {s['server_crashes']}; aborted: {s['aborted']} {s['abort_reason']}")
    o.append(f"- Duration {s['duration_s']/60:.1f} min; chargers up (client side, after ramp) median {st.median([r['chargers_up'] for r in steady]) if steady else 0:.0f}")
    if steady:
        cpu = [r["srv_cpu_cores"] for r in steady]
        o.append(f"- Server RSS MB: min {min(r['srv_rss_mb'] for r in steady):.0f}, max {max(r['srv_rss_mb'] for r in rows):.0f}; CPU cores mean {st.mean(cpu):.2f}, max {max(cpu):.2f}; threads max {max(r['srv_threads'] for r in rows):.0f}; fds max {max(r['srv_fds'] for r in rows):.0f}; goroutines max {max(r['srv_goroutines'] for r in rows):.0f}; generator RSS max {max(r['gen_rss_mb'] for r in rows):.0f} MB")
        o.append(f"- Canary heartbeat max RTT ms (steady): {max(r['canary_max_rtt_ms'] for r in steady):.1f}; /control/health ms max (steady): {max(r['health_ms'] for r in steady):.1f}")
    o.append(f"- CSMS calls: " + ", ".join(f"{k}={v}" for k, v in sorted(s["calls"].items())))
    tot = sum(s["calls"].values()) or 1
    o.append("  - shares: " + ", ".join(f"{k} {100*v/tot:.1f}%" for k, v in sorted(s["calls"].items())))
    cl = s["call_latency_ms"]
    o.append("  - call latency ms (p50/p99): " + ", ".join(f"{k} {fmt(v,'P50')}/{fmt(v,'P99')}" for k, v in sorted(cl.items())))
    o.append(f"- Charger request errors: {s['request_errors']}; connect errors: {s['connect_errors']}")
    o.append(f"- Server log pattern counts: {errs_from_log(d) or 'none'}")
    o.append("\nTimeline (client side chargers up / server RSS MB / server goroutines / CPU cores):\n```")
    o.append("up         " + spark(up))
    o.append("rss MB     " + spark([r["srv_rss_mb"] for r in rows]))
    o.append("goroutines " + spark([max(r["srv_goroutines"], 0) for r in rows]))
    o.append("cpu cores  " + spark([r["srv_cpu_cores"] for r in rows]))
    o.append("```")
    if s["restarts"]:
        o.append("\nRestarts (kill -TERM, 2 s gap, start; times from server healthy):\n")
        o.append(table(["#", "at s", "pre-up", "exit ms", "ready after kill s", "t90 s", "t99 s", "t99 from kill s", "all back s", "reached 99%"],
            [[r["k"], f"{r['kill_at_s']:.0f}", r["pre_up"], r["exit_ms"], f"{r['ready_after_kill_s']:.1f}", f"{r['t90_from_ready_s']:.1f}", f"{r['t99_from_ready_s']:.1f}", f"{r['t99_from_kill_s']:.1f}", f"{r['t100_from_ready_s']:.1f}", r["reached_99"]] for r in s["restarts"]]))
    if s["_segs"]:
        o.append("\nSteady segments between restarts (leak check: slopes via least squares):\n")
        o.append(table(["from s", "to s", "samples", "RSS start", "RSS end", "RSS MB/min", "goroutines/min", "goroutines per conn", "fds/min"],
            [[f"{g['from']:.0f}", f"{g['to']:.0f}", g["n"], f"{g['rss0']:.0f}", f"{g['rss1']:.0f}", f"{g['rss_slope']:+.1f}", f"{g['gor_slope']:+.0f}", f"{g['gor_per_conn']:.2f}", f"{g['fd_slope']:+.0f}"] for g in s["_segs"]]))
    if fr:
        o.append("\nFreezes:\n")
        o.append(table(["at s", "why", "recovered", "lasted s", "killed by harness"], [[f["at_s"] and f"{f['at_s']:.0f}", f["why"], f["recovered"], f"{f['lasted_s']:.0f}", f["killed_by_harness"]] for f in fr]))
    return "\n".join(o)

def main():
    root = sys.argv[1]
    soak = [d for d in sorted(glob.glob(os.path.join(root, "*"))) if os.path.exists(os.path.join(d, "summary.json")) and not os.path.basename(d).startswith(("steps-", "hot-"))]
    stp = [d for d in sorted(glob.glob(os.path.join(root, "steps-*"))) if os.path.exists(os.path.join(d, "summary.json"))]
    hot = [d for d in sorted(glob.glob(os.path.join(root, "hot-*"))) if os.path.exists(os.path.join(d, "summary.json"))]
    out = ["# gocpp vs ocpp-go large-fleet load/soak report\n"]
    out.append(open(os.path.join(root, "preamble.md")).read() if os.path.exists(os.path.join(root, "preamble.md")) else "")
    S = {}
    for d in soak: S[os.path.basename(d)] = analyse(d)
    if S:
        out.append("## Side-by-side: 4,000 chargers, 40% mute, 25 min, 5 restarts\n")
        out.append("Latency columns are p50 / p95 / p99 in ms (histogram bucket upper bounds, 12% resolution). Steady = windows that are not within a restart recovery; restart = 10 s windows overlapping a restart recovery.\n")
        out.append(table(["server", "Boot steady", "Heartbeat steady", "MeterValues steady", "Boot restart", "Heartbeat restart", "MeterValues restart"], [lat_row(k, v) for k, v in S.items()]))
        rr = []
        for k, v in S.items():
            steady = [r for r in v["_rows"] if r["step"] == 0 and r["t_s"] > 120 and r["srv_rss_mb"] > 0]
            t99 = [r["t99_from_ready_s"] for r in v["restarts"]]
            rr.append([k, "YES" if v["freezes"] else "no", v["server_crashes"],
                f"{st.median(r['chargers_up'] for r in steady):.0f}" if steady else "-",
                f"{max(r['srv_rss_mb'] for r in v['_rows']):.0f}", f"{max(r['srv_goroutines'] for r in v['_rows']):.0f}",
                f"{st.mean(r['srv_cpu_cores'] for r in steady):.2f}" if steady else "-",
                ", ".join(f"{x:.1f}" for x in t99), ", ".join(f"{k2}:{n}" for k2, n in sorted(v["calls"].items()))])
        out.append("\n" + table(["server", "freeze", "crashes", "median up", "max RSS MB", "max goroutines", "mean CPU cores", "t99 per restart (s from healthy)", "CSMS call outcomes"], rr))
        out.append("")
        for k, v in S.items(): out.append(section(k, os.path.join(root, k), v)); out.append("")
    if hot:
        out.append("## Hot-spot test: most CSMS calls aimed at 100 mute chargers (serialized per connection)\n")
        for d in hot:
            v = analyse(d)
            out.append(section(os.path.basename(d), d, v)); out.append("")
    for d in stp:
        v = analyse(d); k = os.path.basename(d)
        out.append(f"## Step test: {k}\n")
        rows = v["_rows"]; steps = v["config"]["Steps"]
        tr = []
        for i, sp in enumerate(steps):
            pts = [r for r in rows if r["step"] == i and r["srv_rss_mb"] > 0]
            if not pts: continue
            tail = pts[len(pts) // 2:]
            lat = v["latency_by_step_steady_ms"].get(str(i), {})
            tr.append([sp["N"], sp["Rate"], sp.get("Speed", 1), f"{st.median(r['chargers_up'] for r in tail):.0f}", f"{max(r['srv_rss_mb'] for r in pts):.0f}",
                f"{st.mean(r['srv_cpu_cores'] for r in tail):.2f}", f"{max(r['srv_goroutines'] for r in pts):.0f}",
                f"{max(r['canary_max_rtt_ms'] for r in tail):.0f}", f"{max(r['health_ms'] for r in tail):.0f}",
                f"{fmt(lat.get('Heartbeat'),'P50')} / {fmt(lat.get('Heartbeat'),'P99')}", f"{fmt(lat.get('MeterValues'),'P50')} / {fmt(lat.get('MeterValues'),'P99')}",
                f"{statistics_mean([r['msgs_per_s'] for r in tail]):.0f}"])
        out.append(table(["N", "call rate/s", "msg speed x", "median up (2nd half)", "max RSS MB", "mean CPU cores", "max goroutines", "canary max RTT ms", "health max ms", "Heartbeat p50/p99 ms", "MeterValues p50/p99 ms", "msgs/s"], tr))
        out.append(f"\nFreeze: {'YES' if v['freezes'] else 'no'}; aborted: {v['aborted']} {v['abort_reason']}; calls: {v['calls']}\n")
        out.append("```\nup         " + spark([r["chargers_up"] for r in rows]) + "\nrss MB     " + spark([r["srv_rss_mb"] for r in rows]) + "\ncpu cores  " + spark([r["srv_cpu_cores"] for r in rows]) + "\ncanary ms  " + spark([r["canary_max_rtt_ms"] for r in rows]) + "\n```\n")
    hr = []
    for d in sorted(glob.glob(os.path.join(root, "*"))):
        if not os.path.exists(os.path.join(d, "summary.json")): continue
        for r in highrate_rows(d, analyse(d)): hr.append([os.path.basename(d)] + r)
    if hr:
        out.append("## High-rate columns (second half of every step)\n")
        out.append("alloc KB/req divides the server's heap allocation by completed charger requests (each is one CALL and one CALLRESULT). The in-mark share is the wall time inside GC mark phases from `GODEBUG=gctrace=1` (run the harness with `-gctrace` or `-knee`). Scheduler latency is the p99 bucket of `/sched/latencies:seconds` per 5 s sample.\n")
        out.append(table(["run"] + HIGHRATE_HEAD, hr))
        out.append("")
    open(os.path.join(root, "report.md"), "w").write("\n".join(out))

def statistics_mean(x): return st.mean(x) if x else 0

main()
