"""Inline SVG diagrams for the introduction site. Text widths are checked by svg.py."""
from svg import Svg


def codegen():
    s = Svg(920, 250, "Code generation pipeline",
            "Official JSON schemas and a profile file go into the code generator, which emits message structs, profiles, handlers, clients and embedded schemas for each OCPP version.", minw=700)
    s.box(16, 28, 220, 62, "ext", "Official OCA JSON schemas", ["1.6, 2.0.1, 2.1"], "schemas/<version>/*.json")
    s.box(16, 104, 220, 54, "ext", "Profile file", ["message names and directions"], "profiles/<version>.yaml")
    s.box(16, 172, 220, 54, "plain", "Schema overrides (optional)", ["JSON merge patches"], "schemas/overrides/")
    s.box(310, 84, 190, 94, "disp", "internal/codegen", ["deterministic output", "make codegen"], "loader, IR, renderer")
    for y in (59, 131, 199):
        s.path("M236,%d C275,%d 275,131 309,131" % (y, y))
    s.group_frame(574, 12, 330, 226, "Generated per version: v16, v201, v21", "frame f-ok")
    outs = [
        ("messages", "request, response, enum types"),
        ("profiles", "ocppj.Message[Req, Resp] values"),
        ("handlers", "typed interfaces, one-call register"),
        ("client, calls", "one typed method per message"),
        ("schemas", "embedded for run-time validation"),
    ]
    for i, (t, sub) in enumerate(outs):
        y = 38 + i * 39
        s.box(586, y, 306, 33, "gen", None, [], align="start")
        s.text(598, y + 21, t, 12, "tt", anchor="start", bold=True, mono=True)
        s.text(700, y + 21, sub, 10.5, "ts", anchor="start", maxw=188)
    s.path("M500,131 L573,131")
    s.chip(405, 208, "CI: regenerate, then git diff --exit-code", "ok", anchor="middle")
    return s.render()


def migration():
    s = Svg(920, 190, "Staged migration from ocpp-go",
            "Three stages: on ocpp-go, on gocpp with the compatibility preset, on gocpp with spec defaults, with the number of charger-visible differences at each stage.", minw=700)
    s.box(16, 40, 250, 96, "ext", "Today: ocpp-go", ["chargers are tuned to", "its replies"])
    s.box(335, 40, 250, 96, "warn", "Step 1: gocpp, compat preset", [("csms.WithOCPPGoCompat()",), "same codes, ids, timestamps", "and idle handling as ocpp-go"], ssize=10.5)
    s.box(654, 40, 250, 96, "ok", "Step 2: gocpp defaults", ["turn the six options back to the", "specification, one at a time"], ssize=10.5)
    s.path("M266,88 L334,88")
    s.path("M585,88 L653,88")
    s.label(300, 80, "swap library", size=10)
    s.label(619, 80, "per option", size=10)
    s.chip(460, 160, "13 of 67 cases differ from ocpp-go, all deliberate", "warn", anchor="middle")
    s.chip(779, 160, "24 of 67 cases differ from ocpp-go", "ok", anchor="middle")
    s.chip(141, 160, "reference behaviour", "plain", anchor="middle")
    return s.render()


def _conn(s, x, y, name):
    s.box(x, y, 150, 150, "plain", None, [])
    s.text(x + 75, y + 18, name, 11.5, "tt", bold=True)
    s.box(x + 10, y + 28, 130, 26, "api", None, ["reader goroutine"], ssize=10.5)
    s.box(x + 10, y + 58, 130, 26, "disp", None, ["call slot + FIFO queue"], ssize=10.5)
    s.box(x + 10, y + 88, 130, 26, "disp", None, ["pending calls, timers"], ssize=10.5)
    s.box(x + 10, y + 118, 130, 26, "disp", None, ["write lock (FIFO)"], ssize=10.5)


def isolation():
    s = Svg(940, 318, "Per-connection state compared with a shared dispatcher",
            "Left: in gocpp every connection has its own reader goroutine, call slot, pending-call table and write lock. Right: in a shared-dispatcher design all charge points pass through one goroutine and one lock for outbound requests.", minw=760)
    s.group_frame(10, 10, 500, 298, "gocpp: every connection owns its state", "frame f-api")
    for i, n in enumerate(("charger A", "charger B", "charger C")):
        _conn(s, 22 + i * 163, 36, n)
    s.chip(97, 206, "muted: never answers", "err", anchor="middle")
    s.chip(260, 206, "unaffected", "ok", anchor="middle")
    s.chip(423, 206, "unaffected", "ok", anchor="middle")
    s.text(260, 240, "A waits for its own call timeout, in its own slot.", 11.5, "ts", maxw=480)
    s.text(260, 258, "No lock, queue or goroutine is shared on the message path.", 11.5, "ts", maxw=480)
    s.text(260, 290, "core/dispatcher: conn.go, callslot.go, pending.go", 9.5, "tp", mono=True)

    s.group_frame(530, 10, 400, 298, "Shared dispatcher design", "frame")
    for i, n in enumerate(("charger A", "charger B", "charger C")):
        s.box(544 + i * 128, 40, 114, 34, "plain", n, [], tsize=11.5)
        s.path("M%d,74 L%d,107" % (601 + i * 128, 601 + i * 128 if i == 1 else (700 if i == 0 else 760)))
    s.box(600, 108, 260, 72, "res", "one dispatcher", ["one message-pump goroutine and one lock", "serve every charge point's requests"], ssize=10.5)
    s.box(600, 196, 260, 34, "plain", None, ["request queue per charge point"], ssize=10.5)
    s.path("M730,180 L730,195")
    s.text(730, 258, "Every outbound request, timeout and completion", 11.5, "ts", maxw=380)
    s.text(730, 276, "passes through the same goroutine and lock.", 11.5, "ts", maxw=380)
    s.text(730, 296, "e.g. ocpp-go: ocppj/dispatcher.go", 9.5, "tp", mono=True)
    return s.render()


def inbound():
    s = Svg(940, 178, "Inbound backpressure",
            "An inbound CALL goes from the socket to the reader, then to one of up to 16 handler slots; when they are busy it waits in a queue of 4; when that is full the reader stops reading.", minw=760)
    s.box(12, 46, 120, 62, "ext", "charger", ["WebSocket"])
    s.box(182, 46, 150, 62, "api", "reader", ["1 goroutine", "never runs a handler"], ssize=10.5)
    s.box(382, 20, 250, 50, "disp", "handler slots: 16 per connection", ["each CALL on its own goroutine"], tsize=11.5, ssize=10.5)
    s.box(382, 86, 250, 50, "warn", "bounded queue: 4", ["only fills while all slots are busy"], tsize=11.5, ssize=10.5)
    s.box(690, 46, 238, 62, "ok", "typed handler", ["panic recovered, 60 s deadline", "reply written under the write lock"], ssize=10.5)
    s.path("M132,77 L181,77")
    s.path("M332,66 L381,48")
    s.path("M332,88 L381,108")
    s.path("M632,45 L689,66")
    s.path("M507,86 L507,71")
    s.num(157, 60, 1)
    s.num(356, 42, 2)
    s.num(356, 112, 3)
    s.path("M382,150 L140,150 L140,109", "ar rej", dashed=True)
    s.num(152, 150, 4)
    s.label(390, 168, "queue full: the reader stops reading, so TCP slows this one charger", size=10.5, maxw=420)
    return s.render()


def parity():
    s = Svg(940, 214, "Differential testing against ocpp-go",
            "A charge-point simulator sends identical traffic to a CSMS built on gocpp and one built on ocpp-go; a runner diffs frames, close codes and server events.", minw=760)
    s.box(12, 62, 190, 90, "ext", "charge-point simulator", ["scenarios, raw frames,", "faults, disconnects"])
    s.box(300, 20, 240, 70, "api", "CSMS on gocpp", ["typed handlers, canned answers"], "interop/parity/cs-gocpp")
    s.box(300, 124, 240, 70, "plain", "CSMS on ocpp-go", ["identical canned answers"], "interop/parity/cs-ocppgo")
    s.path("M202,92 L299,58")
    s.path("M202,122 L299,156")
    s.label(246, 64, "same traffic", size=10)
    s.box(640, 54, 288, 106, "ok", "runner: diff per case", ["steps and replies the simulator saw", "every wire frame (ids, times normalised)", "close codes, server events, liveness"], ssize=10.5)
    s.path("M540,55 L639,92")
    s.path("M540,159 L639,122")
    s.chip(784, 184, "same, or a documented difference", "ok", anchor="middle")
    return s.render()


def gateway():
    s = Svg(940, 372, "Gateway mode topology",
            "Chargers connect to gateways in the connection tier. Gateways forward messages over a link to stateless backends in the processing tier, grouped in pools. A session registry holds epoch-numbered session records.", minw=780)
    s.box(12, 96, 118, 130, "ext", "chargers", ["WebSocket", "OCPP 1.6,", "2.0.1, 2.1"], ssize=10)
    s.group_frame(176, 40, 250, 250, "Connection tier", "frame f-api", "rarely deployed")
    s.box(190, 68, 222, 96, "api", "gateway", ["TLS, WebSocket, framing", "call slot, timeouts, keepalive", "never reads a payload"], ssize=10.5)
    s.box(190, 176, 222, 96, "api", "gateway", ["drain with close code 1012", "soft session cap, tenant limits", "frame observer and taps"], ssize=10.5)
    s.path("M130,140 L189,118")
    s.path("M130,182 L189,222")
    s.group_frame(560, 12, 368, 278, "Processing tier (stateless)", "frame f-ok", "deployed often")
    s.group_frame(574, 40, 340, 142, "pool: default", "frame")
    s.box(586, 64, 152, 106, "ok", "backend", ["schema validation", "lenient repair", "your typed handlers"], ssize=10.5)
    s.box(750, 64, 152, 106, "ok", "backend", ["csms.On / csms.Call", "unchanged API"], ssize=10.5)
    s.group_frame(574, 194, 340, 84, "pool: canary", "frame")
    s.box(586, 216, 316, 52, "warn", "backend, new version", ["gets work only when the routing policy says so"], ssize=10.5)
    s.path("M427,116 L559,116", start=True)
    s.path("M427,224 L559,224", start=True)
    s.label(493, 104, "link", size=11)
    s.label(493, 140, "gRPC (default)", size=10)
    s.label(493, 154, "NATS", size=10)
    s.label(493, 168, "in-process", size=10)
    s.label(493, 246, "backends dial", size=10)
    s.label(493, 260, "gateways", size=10)
    s.box(176, 310, 520, 50, "disp", "session registry (Redis addon or in-memory)", ["claim before upgrade, epoch per connection, leases, routing policy with compare-and-set"], tsize=11.5, ssize=10)
    s.path("M301,291 L301,309", dashed=True)
    s.path("M640,291 L640,309", dashed=True)
    s.box(744, 310, 184, 50, "plain", "gocppctl", ["route, gateway, rollout"], tsize=11.5, ssize=10)
    s.path("M743,335 L697,335", dashed=True)
    return s.render()


def canary():
    s = Svg(940, 226, "Backend canary by routing policy",
            "The gateway router sends a stable-hash percentage of chargers to the canary pool and the rest to the default pool. Steps of 1, 10, 50 and 100 percent are separate policy revisions; rollback returns every session to the default pool.", minw=760)
    s.box(12, 62, 150, 86, "api", "gateway router", ["first matching rule", "decides the pool"], ssize=10.5)
    s.box(250, 22, 250, 60, "ok", "pool: default (stable version)", ["everything no rule matches"], tsize=11.5, ssize=10.5)
    s.box(250, 128, 250, 60, "warn", "pool: canary (new version)", ["rule: percent, ids, attributes, actions"], tsize=11.5, ssize=10.5)
    s.path("M162,92 L249,56")
    s.path("M162,120 L249,156")
    s.label(204, 60, "the rest", size=10)
    s.label(204, 152, "N %", size=10)
    s.text(730, 26, "One policy revision per step", 12, "tt", bold=True)
    x = 560
    for i, (p, w) in enumerate((("1%", 4), ("10%", 34), ("50%", 170), ("100%", 340))):
        y = 42 + i * 30
        s.raw('<rect x="%d" y="%d" width="340" height="18" rx="4" fill="var(--k-ok-bg)" stroke="var(--k-ok)" stroke-width="1"/>' % (x, y))
        s.raw('<rect x="%d" y="%d" width="%d" height="18" rx="%d" fill="var(--k-warn)"/>' % (x, y, w, 2 if w < 8 else 4))
        s.text(x - 8, y + 13, p, 11, "ts", anchor="end")
    s.text(730, 176, "Stable hash of the charger id: raising the share only adds chargers.", 10.5, "ts", maxw=400)
    s.text(730, 194, "Rollback: one new revision, every session back on default.", 10.5, "ts", maxw=400)
    s.text(730, 212, "Guard: a failing canary rule is suspended by the gateway.", 10.5, "ts", maxw=400)
    return s.render()
