#!/usr/bin/env python3
"""Report for a rollout run directory (see rollout/run.sh).

usage: rollout_report.py RUNDIR [RUNDIR...]   -> prints markdown

Phases are the intervals between hook_start events of events.log (the policy
steps); the first phase is the baseline before the first hook. Sections:
routing correctness (from the backends' handled-*.log), errors, latency per pool
(gateway upstream_seconds), harness latency, gateway CPU per phase.
"""
import csv, glob, json, math, os, re, sys, statistics as st
from collections import defaultdict


def read_events(d):
    ev = []
    for line in open(os.path.join(d, "events.log"), errors="replace"):
        f = line.rstrip("\n").split("\t")
        if len(f) >= 4:
            ev.append((float(f[1]), f[2], f[3]))
    return ev


def short(cmd):
    m = re.search(r"route apply \S*/(policy-\d+)\.yaml", cmd)
    if m:
        return m.group(1).replace("policy-", "policy ") + ("%" if not m.group(1).endswith("-0") else " (none)")
    if "rollout run" in cmd:
        return "rollout run"
    return cmd[:30]


def phases(d, dur):
    ev = read_events(d)
    s0 = next(t for t, k, m in ev if k == "start")
    unix0 = int(re.search(r"unix_ms=(\d+)", next(m for t, k, m in ev if k == "start")).group(1)) / 1000
    hooks = [(t - s0, short(m)) for t, k, m in ev if k == "hook_start"]
    ends = {m.split(":")[0]: t - s0 for t, k, m in ev if k == "hook_end"}
    ph = []
    prev, name = 90.0, "baseline"
    for t, h in hooks:
        ph.append((name, prev, t))
        prev, name = t, h
    ph.append((name, prev, dur))
    return ph, unix0, hooks, ends


def decisions(d, i, unix0):
    """Controller decisions of hook i: [(t_s, action, stage, percent, reason)]."""
    import datetime
    out = []
    p = os.path.join(d, f"hook-{i}.log")
    if not os.path.exists(p):
        return out
    for line in open(p, errors="replace"):
        if "rollout decision" not in line:
            continue
        m = re.search(r'time=(\S+)', line)
        if not m:
            continue
        t = datetime.datetime.fromisoformat(m.group(1)).timestamp() - unix0
        g = lambda k: (re.search(k + r'=("([^"]*)"|\S+)', line) or [None, "", ""])
        out.append((t, g("action")[2] or g("action")[1], g("stage")[1], g("percent")[1], g("reason")[2] or g("reason")[1]))
    return out


def expand_auto(d, ph, hooks, unix0, dur):
    """Replace the phase of a 'rollout run' hook by one phase per applied stage."""
    out = []
    for name, a, b in ph:
        if name != "rollout run":
            out.append((name, a, b))
            continue
        idx = [i for i, (t, h) in enumerate(hooks) if h == "rollout run" and abs(t - a) < 1][0]
        ds = decisions(d, idx, unix0)
        starts = [(t, f"stage {stg} {pc}%") for t, act, stg, pc, _ in ds if act == "apply"]
        endt = next((t for t, act, *_ in reversed(ds) if act in ("rollback", "done")), b)
        end_name = next((("rolled back" if act == "rollback" else "done") for t, act, *_ in reversed(ds) if act in ("rollback", "done")), "run ended")
        if not starts:
            out.append((name, a, b))
            continue
        if starts[0][0] - a >= 2:
            out.append(("controller start", a, starts[0][0]))
        for k, (t, n) in enumerate(starts):
            nxt = starts[k + 1][0] if k + 1 < len(starts) else endt
            out.append((n, a if k == 0 and starts[0][0] - a < 2 else t, min(nxt, b)))
        if endt < b:
            out.append((end_name, endt, b))
    return out


def pct_from_buckets(b, q):
    """b: {le: cumulative count} (le float, inf allowed)."""
    les = sorted(b)
    total = b[les[-1]]
    if total <= 0:
        return None
    target, lo, prevc = q * total, 0.0, 0.0
    for le in les:
        c = b[le]
        if c >= target:
            if math.isinf(le):
                return lo
            frac = 0 if c == prevc else (target - prevc) / (c - prevc)
            return lo + (le - lo) * frac
        lo, prevc = le, c
    return lo


def label(s, k):
    m = re.search(k + r"='([^']*)'", s)
    return m.group(1) if m else ""


def gwmetrics(d):
    """{(t, gateway, metric, labels): value}"""
    rows = []
    p = os.path.join(d, "gwmetrics.csv")
    if not os.path.exists(p):
        return rows
    for r in csv.DictReader(open(p)):
        rows.append((float(r["t_s"]), r["gateway"], r["metric"], r["labels"], float(r["value"])))
    return rows


def snapshot_at(rows, t):
    """Latest sample (at or before t) of every series: {(gw, metric, labels): value}."""
    best = {}
    ts = sorted({r[0] for r in rows})
    tt = max([x for x in ts if x <= t], default=None)
    if tt is None:
        return {}
    return {(r[1], r[2], r[3]): r[4] for r in rows if r[0] == tt}


def delta(a, b):
    out = {}
    for k, v in b.items():
        out[k] = v - a.get(k, 0.0) if v >= a.get(k, 0.0) else v
    return out


def routing(d, unix0, ph, dur):
    """Per charger, the ordered pools that served its heartbeats."""
    seq = defaultdict(list)  # cp -> [(t, pool)]
    for f in glob.glob(os.path.join(d, "handled-*.log")):
        name = os.path.basename(f)[len("handled-"):-4]  # be-<pool>-<k>
        pool = name.split("-")[1]
        for line in open(f, errors="replace"):
            p = line.split()
            if len(p) == 3 and p[2] == "Heartbeat" and p[1].startswith("LT-"):
                seq[p[1]].append((int(p[0]) / 1000 - unix0, pool))
    for v in seq.values():
        v.sort()
    return seq


def routing_section(d, unix0, ph, dur):
    seq = routing(d, unix0, ph, dur)
    out = ["### Routing correctness", ""]
    if not seq:
        return out + ["(no handled logs)", ""]
    n = len(seq)
    out.append(f"{n} chargers with heartbeats in the handled logs.")
    out.append("")
    out.append("| phase | window | chargers seen | on canary | share | nominal | within 3 sigma |")
    out.append("|---|---|---:|---:|---:|---:|---|")
    nominal = {"policy 1%": 1, "policy 10%": 10, "policy 50%": 50, "policy 100%": 100, "policy 0 (none)": 0, "baseline": 0,
               "controller start": 0, "rolled back": 0}
    for name, _, _ in ph:
        m = re.match(r"stage \S+ (\d+)%", name)
        if m:
            nominal[name] = int(m.group(1))
    # after a finished rollout (done) every charger is on the canary
    nominal["done"] = 100
    for name, a, b in ph:
        lo = a + 25 if name != "baseline" and b - a >= 45 else a  # give the revision time to apply and each charger a heartbeat
        seen = on = 0
        for cp, v in seq.items():
            last = None
            for t, pool in v:
                if lo <= t < b:
                    last = pool
            if last is not None:
                seen += 1
                on += last == "canary"
        nom = nominal.get(name)
        if nom is None:
            out.append(f"| {name} | {a:.0f}-{b:.0f}s | {seen} | {on} | {100*on/max(seen,1):.1f}% | - | - |")
            continue
        p = nom / 100
        sig = math.sqrt(max(p * (1 - p), 1e-9) / max(seen, 1)) * 100
        ok = abs(100 * on / max(seen, 1) - nom) <= 3 * sig + 0.01
        out.append(f"| {name} | {a:.0f}-{b:.0f}s | {seen} | {on} | {100*on/max(seen,1):.1f}% | {nom}% | {'yes' if ok else 'NO'} |")
    out.append("")
    # sticky / flip analysis
    cut = next((a for name, a, b in ph if name in ("policy 0 (none)", "rolled back")), dur + 1)
    changes = defaultdict(int)
    before = defaultdict(int)  # changes before the rollback
    up_after_down = 0   # leaves the canary then comes back (a flip) before the rollback
    for cp, v in seq.items():
        prev = None
        left = False
        for t, pool in v:
            if prev is not None and pool != prev:
                changes[cp] += 1
                if t < cut:
                    before[cp] += 1
                    if prev == "canary":
                        left = True
                if t < cut and prev != "canary" and left:
                    up_after_down += 1
            prev = pool
    hist = defaultdict(int)
    for cp in seq:
        hist[min(changes[cp], 3)] += 1
    out.append("Pool changes per charger over the whole run (every change is a heartbeat served by a different pool than the one before): "
               + ", ".join(f"{k if k < 3 else '3+'}: {hist[k]}" for k in sorted(hist)) + ".")
    multi = sum(1 for cp in seq if before[cp] > 1)
    out.append(f"Chargers with more than one pool change before the rollback (not sticky): {multi}; "
               f"chargers that left the canary and came back before the rollback (flip): {up_after_down}.")
    return out + [""]


def errors_section(d, ph, dur):
    out = ["### Errors and connections", ""]
    errs = defaultdict(lambda: defaultdict(int))
    p = os.path.join(d, "errors.csv")
    if os.path.exists(p):
        for r in csv.DictReader(open(p)):
            t = float(r["window_start_s"])
            for name, a, b in ph:
                if a <= t < b:
                    errs[name][r["kind"]] += int(r["n"])
    rows = list(csv.DictReader(open(os.path.join(d, "timeline.csv"))))
    out.append("| phase | window | charger request errors | chargers up (min) | server_connected (min) |")
    out.append("|---|---|---|---:|---:|")
    for name, a, b in ph:
        w = [r for r in rows if a <= float(r["t_s"]) < min(b, dur - 10)]  # the last samples are the teardown
        up = min((float(r["chargers_up"]) for r in w), default=0)
        sc = min((float(r["server_connected"]) for r in w), default=0)
        e = errs[name]
        es = ", ".join(f"{k}: {v}" for k, v in sorted(e.items())) or "0"
        out.append(f"| {name} | {a:.0f}-{b:.0f}s | {es} | {up:.0f} | {sc:.0f} |")
    s = json.load(open(os.path.join(d, "summary.json")))
    out.append("")
    out.append(f"Connect errors (whole run): {s.get('connect_errors') or 'none'}; freezes: {s.get('freezes')}; "
               f"final chargers up: {s.get('final_up')}.")
    return out + [""]


def pool_latency_section(d, ph, rows):
    out = ["### Upstream latency and outcomes per pool (gateway view)", ""]
    if not rows:
        return out + ["(no gateway metrics)", ""]
    out.append("| phase | pool | requests | failed | callerror InternalError | p50 ms | p99 ms |")
    out.append("|---|---|---:|---:|---:|---:|---:|")
    for name, a, b in ph:
        s0, s1 = snapshot_at(rows, a), snapshot_at(rows, b)
        dl = delta(s0, s1)
        per = defaultdict(lambda: {"req": 0.0, "fail": 0.0, "ie": 0.0, "b": defaultdict(float)})
        for (gw, metric, labels), v in dl.items():
            pool = label(labels, "pool") or "default"
            if metric == "gocpp_gateway_upstream_requests_total":
                per[pool]["req"] += v
                oc = label(labels, "outcome")
                if oc in ("timeout", "stream_broken", "callerror_InternalError"):
                    per[pool]["fail"] += v
                if oc == "callerror_InternalError":
                    per[pool]["ie"] += v
            elif metric == "gocpp_gateway_upstream_seconds_bucket":
                le = label(labels, "le")
                per[pool]["b"][float("inf") if le == "+Inf" else float(le)] += v
        for pool in sorted(per):
            x = per[pool]
            p50, p99 = pct_from_buckets(x["b"], .5), pct_from_buckets(x["b"], .99)
            f = lambda v: "-" if v is None else f"{v*1000:.2f}"
            out.append(f"| {name} | {pool} | {x['req']:.0f} | {x['fail']:.0f} | {x['ie']:.0f} | {f(p50)} | {f(p99)} |")
    return out + [""]


def harness_latency(d, ph):
    out = ["### Charger-side latency (harness, Heartbeat)", "", "| phase | requests | p50 ms (n-weighted) | median window p99 ms | max window p99 ms |", "|---|---:|---:|---:|---:|"]
    rows = [r for r in csv.DictReader(open(os.path.join(d, "latency.csv"))) if r["action"] == "Heartbeat"]
    for name, a, b in ph:
        w = [r for r in rows if a <= float(r["window_start_s"]) < b]
        n = sum(int(r["n"]) for r in w)
        if not n:
            out.append(f"| {name} | 0 | - | - | - |")
            continue
        p50 = sum(float(r["p50_ms"]) * int(r["n"]) for r in w) / n
        p99s = [float(r["p99_ms"]) for r in w]
        out.append(f"| {name} | {n} | {p50:.2f} | {st.median(p99s):.1f} | {max(p99s):.1f} |")
    return out + [""]


def cost_section(d, ph, rows):
    out = ["### Gateway CPU per phase (added cost of policy evaluation, end to end)", "",
           "| phase | gateway cores (sum of both) | requests/s | gateway core-us per request | route decisions/s |", "|---|---:|---:|---:|---:|"]
    procs = [r for r in csv.DictReader(open(os.path.join(d, "procs.csv"))) if r["role"] == "gateway"]
    tl = list(csv.DictReader(open(os.path.join(d, "timeline.csv"))))
    for name, a, b in ph:
        lo = a + 10
        byt = defaultdict(float)
        for r in procs:
            t = float(r["t_s"])
            if lo <= t < b:
                byt[t] += float(r["cpu_cores"])
        cores = st.mean(byt.values()) if byt else 0
        mps = st.mean([float(r["msgs_per_s"]) for r in tl if lo <= float(r["t_s"]) < b] or [0])
        s0, s1 = snapshot_at(rows, lo), snapshot_at(rows, b)
        dl = delta(s0, s1)
        dec = sum(v for (gw, m, l), v in dl.items() if m == "gocpp_gateway_route_decisions_total")
        dur = max(b - lo, 1)
        out.append(f"| {name} | {cores:.3f} | {mps:.0f} | {1e6*cores/max(mps,1):.1f} | {dec/dur:.0f} |")
    return out + [""]


def calls_section(d, ph):
    out = ["### CSMS-initiated calls (issued from the control backend in the default pool to chargers in either pool)", "",
           "| phase | ok | ok p50 ms | ok p99 ms (max of windows) | timeout (muted chargers) | not_connected |", "|---|---:|---:|---:|---:|---:|"]
    rows = list(csv.DictReader(open(os.path.join(d, "calls.csv"))))
    for name, a, b in ph:
        w = [r for r in rows if a <= float(r["window_start_s"]) < b]
        n = defaultdict(int)
        for r in w:
            n[r["outcome"]] += int(r["n"])
        ok = [r for r in w if r["outcome"] == "ok"]
        okn = sum(int(r["n"]) for r in ok)
        p50 = sum(float(r["p50_ms"]) * int(r["n"]) for r in ok) / okn if okn else 0
        p99 = max((float(r["p99_ms"]) for r in ok), default=0)
        out.append(f"| {name} | {n['ok']} | {p50:.2f} | {p99:.1f} | {n['timeout']} | {n['not_connected']} |")
    s = json.load(open(os.path.join(d, "summary.json")))
    out.append("")
    out.append(f"Whole run: calls {s.get('calls')}, delivered {s.get('csms_calls_delivered')} (a timeout is a call to a muted charger, which drops it by design; not_connected is the "
               f"harness' own pre-check: the control backend only knows the chargers whose session events it received, a third to a half of the fleet in split mode, so it is not a CSMS failure).")
    return out + [""]


def hooks_section(d, hooks, ends):
    out = ["### Hooks", ""]
    for i, (t, name) in enumerate(hooks):
        log = os.path.join(d, f"hook-{i}.log")
        body = open(log, errors="replace").read().strip() if os.path.exists(log) else ""
        out.append(f"**{name}** at {t:.0f}s")
        ts = [float(x) for x in body.splitlines() if re.fullmatch(r"\d+\.\d+", x.strip())]
        if name.startswith("policy") and len(ts) >= 2:
            out.append(f"(`route apply --wait` took {1000*(ts[-1]-ts[0]):.0f} ms until every gateway reported the revision)")
        out.append("```")
        out.append(body[-3500:])
        out.append("```")
    return out + [""]


def main():
    for d in sys.argv[1:]:
        s = json.load(open(os.path.join(d, "summary.json")))
        ph, unix0, hooks, ends = phases(d, s["duration_s"])
        ph = expand_auto(d, ph, hooks, unix0, s["duration_s"])
        rows = gwmetrics(d)
        print(f"## {s.get('label', os.path.basename(d))}\n")
        print("Phases: " + ", ".join(f"{n} {a:.0f}-{b:.0f}s" for n, a, b in ph) + "\n")
        for part in (routing_section(d, unix0, ph, s["duration_s"]), errors_section(d, ph, s["duration_s"]), pool_latency_section(d, ph, rows),
                     harness_latency(d, ph), calls_section(d, ph), cost_section(d, ph, rows), hooks_section(d, hooks, ends)):
            print("\n".join(part))


if __name__ == "__main__":
    main()
