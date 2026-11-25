#!/usr/bin/env python3
"""Summarise `go test -json` output of the interop suite as a matrix.

Usage: scripts/interop-matrix.py results.json [more.json ...] [--failures]

Leaf tests are named TestInterop_<Gocpp...>/<peer>/<ocpp version>/<scenario>.
A scenario that fails is FAIL; a skip whose reason starts with KNOWN[class] is a
recorded issue (class: peer, harness or gocpp); any other skip is a capability
the peer does not declare or a scenario that does not apply.
"""
import json
import re
import sys
from collections import defaultdict

ROLE = {"TestInterop_GocppCSMS": "gocpp CSMS <- peer station: messages",
        "TestInterop_GocppCP": "gocpp CP -> peer CSMS: messages",
        "TestInterop_GocppCSMS_Security": "gocpp CSMS <- peer station: profiles 1-3, compression",
        "TestInterop_GocppCP_Security": "gocpp CP -> peer CSMS: profiles 1-3, compression",
        "TestInterop_Gocpp_Send": "OCPP 2.1 SEND (both directions)"}


def load(paths):
    out = defaultdict(list)
    status = {}
    for p in paths:
        for line in open(p, errors="replace"):
            try:
                e = json.loads(line)
            except ValueError:
                continue
            t = e.get("Test")
            if not t:
                continue
            if e["Action"] == "output":
                out[t].append(e["Output"])
            elif e["Action"] in ("pass", "fail", "skip"):
                status[t] = e["Action"]
    return status, out


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    show_fail = "--failures" in sys.argv
    status, out = load(args)
    parents = {t.rsplit("/", 1)[0] for t in status if "/" in t}
    leaves = [t for t in status if t not in parents and t.count("/") >= 3]
    rows = defaultdict(lambda: defaultdict(int))
    fails, knowns = [], []
    for t in sorted(leaves):
        top, peer, ver = t.split("/")[:3]
        st = status[t]
        key = "pass" if st == "pass" else "fail" if st == "fail" else "skip"
        text = "".join(out[t])
        m = re.search(r"KNOWN\[(\w+)\]: (.*)", text)
        if st == "skip" and m:
            key = "known-" + m.group(1)
            knowns.append((t, m.group(1), m.group(2).strip()))
        if st == "fail":
            msg = [l.strip() for l in text.splitlines() if re.match(r"\s+\w+_test\.go:\d+:", l)]
            fails.append((t, (msg[0] if msg else "")[:220]))
        rows[(ROLE.get(top, top), peer, ver)][key] += 1
    print("| gocpp role | peer | OCPP | pass | fail | known peer | known harness | known gocpp | skipped |")
    print("|---|---|---|---:|---:|---:|---:|---:|---:|")
    for (role, peer, ver), c in sorted(rows.items()):
        print(f"| {role} | {peer} | {ver} | {c['pass']} | {c['fail']} | {c['known-peer']} | {c['known-harness']} | {c['known-gocpp']} | {c['skip']} |")
    if show_fail:
        print("\nFailures:")
        for t, m in fails:
            print(f"- {t}: {m}")
        print("\nKnown issues:")
        for t, c, m in knowns:
            print(f"- [{c}] {t}: {m}")


main()
