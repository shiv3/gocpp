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


def layers():
    s = Svg(940, 430, "Package layers",
            "Your application sits on generated per-version packages, which sit on csms and cp, then core/dispatcher, then ocppj, schema and transport. Auth, storage and observability are pluggable; addons are separate modules.", minw=780)
    s.box(12, 10, 640, 40, "user", "your application", ["typed handlers, typed calls, connect and disconnect callbacks"], ssize=10.5)
    s.group_frame(12, 62, 640, 96, "v16 / v201 / v21: generated by internal/codegen", "frame f-ok")
    for i, (t, sub) in enumerate((("handlers", "interfaces, Register"), ("client, calls", "typed send methods"), ("messages, profiles", "structs, Message[Req,Resp]"), ("schemas", "embedded JSON schemas"))):
        s.box(22 + i * 157, 86, 149, 60, "gen", t, [sub], tsize=11.5, ssize=9.5)
    s.box(12, 172, 314, 62, "api", "csms", ["Server, Conn, On, Call, Send, Lookup, Drain", "upgrade checks, admission, sessions"], ssize=10)
    s.box(338, 172, 314, 62, "api", "cp", ["Client, On, Call, Send", "Connect, Run (reconnect), offline queue"], ssize=10)
    s.box(12, 248, 640, 66, "disp", "core/dispatcher: the version-agnostic connection engine", ["Conn (reader, teardown), inbound scheduling, call slot, pending calls,", "Pipeline (schema, lenient repair, error mapping), keepalive timers, HandlerPool"], ssize=10)
    s.box(12, 328, 206, 60, "ok", "core/ocppj", ["framing, message ids,", "errors, signing (2.1)"], ssize=10)
    s.box(229, 328, 206, 60, "warn", "core/schema", ["registry, validator,", "lenient repair, time encoding"], ssize=10)
    s.box(446, 328, 206, 60, "plain", "core/transport", ["WebSocket interface", "(coder/websocket), fake for tests"], ssize=10)
    for x in (169, 495):
        s.line(x, 50, x, 61)
        s.line(x, 158, x, 171)
        s.line(x, 234, x, 247)
    for x in (115, 332, 549):
        s.line(x, 314, x, 327)
    s.text(332, 412, "A layer depends only on the layers below it. Adding OCPP 2.1 added no dispatcher code.", 11, "ts", maxw=630)
    s.group_frame(676, 10, 252, 224, "Pluggable (interface + default)", "frame")
    s.box(688, 34, 228, 56, "plain", "core/auth", ["None, BasicAuth, MTLSFromClientCert"], tsize=11.5, ssize=9.5)
    s.box(688, 98, 228, 60, "plain", "core/storage (+ memory)", ["ConnectionRegistry, SessionRegistry,", "TransactionStore, ConfigStore"], tsize=11.5, ssize=9.5)
    s.box(688, 166, 228, 58, "plain", "core/observability", ["Metrics (Prometheus, OTel), tracer,", "FrameObserver, tap"], tsize=11.5, ssize=9.5)
    s.path("M652,203 L675,203", start=True)
    s.group_frame(676, 248, 252, 140, "addons/ and gateway/ (experimental)", "frame")
    s.box(688, 272, 228, 50, "addon", "gateway, link-grpc, link-nats", ["connection tier and its links"], tsize=11, ssize=9.5)
    s.box(688, 330, 228, 50, "addon", "registry-redis, rollout, tenant, ...", ["each addon is its own Go module"], tsize=11, ssize=9.5)
    return s.render()


def lifecycle():
    steps = [
        ("Shutting down or draining?", "503 + Retry-After"),
        ("Accept rate limit (optional token bucket)", "503 + Retry-After"),
        ("Charge point id from the path", "400"),
        ("Authenticate (Basic, mTLS, custom)", "401"),
        ("Duplicate id with RejectNew policy", "409"),
        ("Origin check", "403"),
        ("Connection cap: check and reserve atomically", "503 + Retry-After"),
        ("Per-tenant session limit (optional)", "503 + Retry-After"),
        ("Claim the session: registry assigns the epoch", "released if the upgrade fails"),
        ("WebSocket upgrade, subprotocol chosen", "close: no common subprotocol"),
    ]
    s = Svg(940, 404, "Connection lifecycle",
            "Ten checks run on the plain HTTP request before the WebSocket upgrade. Then inbound handling is held, the connection starts and is published, the connect callback runs, inbound is released, and the reader loop runs until one cancel tears everything down.", minw=780)
    s.text(16, 20, "Before the upgrade: every refusal is a plain HTTP answer", 12, "tf", anchor="start", bold=True)
    for i, (t, rej) in enumerate(steps):
        y = 32 + i * 36
        s.box(40, y, 300, 28, "plain", None, [t], ssize=10.5, align="start")
        s.num(24, y + 14, i + 1)
        if i < len(steps) - 1:
            s.line(190, y + 28, 190, y + 35)
        s.line(340, y + 14, 366, y + 14, cls="ar rej")
        s.chip(370, y + 14, rej, "ok" if i == 8 else "err", size=10)
    s.text(590, 20, "After the upgrade", 12, "tf", anchor="start", bold=True)
    after = [
        ("disp", "HoldInbound", "frames are read but handlers wait"),
        ("disp", "Start", "context, reader goroutine, timers"),
        ("api", "publish + OnConnect", "Get(id) works before any handler runs"),
        ("disp", "ReleaseInbound", "queued CALLs start, in arrival order"),
        ("ok", "reader loop", "the HTTP handler has already returned"),
        ("res", "teardown: one cancel(cause)", "pending calls fail, close frame, release"),
    ]
    for i, (k, t, sub) in enumerate(after):
        y = 32 + i * 50
        s.box(590, y, 338, 40, k, t, [sub], tsize=11.5, ssize=10)
        if i < len(after) - 1:
            s.line(759, y + 40, 759, y + 49)
    s.path("M190,384 L190,396 L574,396 L574,52 L589,52")
    s.text(759, 344, "close codes: 1000 normal or by the server, 1008 replaced,", 10, "ts", maxw=340)
    s.text(759, 358, "1009 message too big, 1012 drain", 10, "ts", maxw=340)
    s.text(759, 384, "csms/server.go: serveWS, core/dispatcher/conn.go", 9.5, "tp", mono=True)
    return s.render()


def goroutines():
    s = Svg(940, 250, "What a connection consists of",
            "One resident reader goroutine per connection. Writes run on the calling goroutine under a FIFO lock. Read watchdog, ping and write timeout are timers. Teardown is a context callback. Handlers are short-lived goroutines or pool workers.", minw=780)
    s.group_frame(12, 10, 916, 150, "one connection, idle: 1 goroutine, 7.8 KB heap + 4.1 KB stack", "frame f-api")
    s.box(26, 38, 190, 108, "api", "reader goroutine", ["the only resident goroutine", "reads, parses, routes frames;", "never runs a handler"], ssize=10)
    s.box(230, 38, 216, 50, "warn", "timers (time.AfterFunc)", ["read-idle watchdog, WebSocket ping"], tsize=11.5, ssize=10)
    s.box(230, 96, 216, 50, "warn", "write timeout", ["one lazily armed timer"], tsize=11.5, ssize=10)
    s.box(460, 38, 216, 50, "disp", "write lock (FIFO)", ["the producer of a frame writes it"], tsize=11.5, ssize=10)
    s.box(460, 96, 216, 50, "disp", "call slot, pending table", ["plain data, no goroutine"], tsize=11.5, ssize=10)
    s.box(690, 38, 224, 108, "res", "teardown", ["context.AfterFunc callback:", "runs once, whichever side", "ends the connection"], ssize=10)
    s.group_frame(12, 172, 916, 68, "only while a message is being handled", "frame")
    s.box(26, 194, 280, 36, "user", None, ["handler goroutine per inbound CALL (short-lived)"], ssize=10)
    s.box(320, 194, 290, 36, "user", None, ["or a warm HandlerPool worker, if one is idle"], ssize=10)
    s.box(624, 194, 290, 36, "user", None, ["ping: a goroutine only while awaiting the pong"], ssize=10)
    return s.render()


def inpath():
    s = Svg(940, 300, "Inbound path of a CALL",
            "A frame is read, parsed by a scanner, routed by message type, scheduled into a handler slot, validated against the schema, decoded, handled with a deadline, and the reply is validated and written under the write lock.", minw=800)
    row1 = [
        ("api", "read", ["1 MiB limit", "else close 1009"]),
        ("ok", "ocppj.Parse", ["scanner fast path,", "reflective fallback"]),
        ("disp", "route by type", ["per OCPP version;", "results go to pending"]),
        ("disp", "schedule", ["16 slots, queue of 4,", "then stop reading"]),
    ]
    for i, (k, t, sub) in enumerate(row1):
        x = 12 + i * 234
        s.box(x, 28, 214, 70, k, t, sub, ssize=10)
        s.num(x + 14, 28, i + 1)
        if i < 3:
            s.line(x + 214, 63, x + 233, 63)
    row2 = [
        ("warn", "schema mode", ["strict: reject. lenient: repair.", "tolerant: log. off: skip"]),
        ("gen", "typed decode", ["into the generated", "request struct"]),
        ("user", "your handler", ["ctx detached from the socket,", "60 s deadline, panic recovered"]),
        ("disp", "reply", ["validate, encode times,", "write under the FIFO lock"]),
    ]
    for i, (k, t, sub) in enumerate(row2):
        x = 12 + i * 234
        s.box(x, 150, 214, 70, k, t, sub, ssize=10)
        s.num(x + 14, 150, i + 5)
        if i < 3:
            s.line(x + 214, 185, x + 233, 185)
    s.path("M821,98 L821,124 L119,124 L119,149")
    s.chip(12, 250, "malformed frame, unknown action or type: CALLERROR with the code of that OCPP version", "err")
    s.chip(12, 276, "handler error: your CallError as is; deadline exceeded or panic: InternalError", "warn")
    s.text(928, 254, "steps 5 to 8 are dispatcher.Pipeline, shared with the", 10, "ts", anchor="end")
    s.text(928, 270, "backend of a split deployment: identical error codes", 10, "ts", anchor="end")
    return s.render()


def outpath():
    s = Svg(940, 208, "Outbound path of a CSMS call",
            "A call is validated, waits for the connection's single call slot in a bounded FIFO, is written under the write lock, registered as pending, and resolved by the reader when the charger answers or the call timeout fires.", minw=800)
    row = [
        ("user", "csms.Call", ["typed request,", "your ctx"]),
        ("warn", "validate", ["outbound schema check,", "time encoding"]),
        ("disp", "call slot", ["one outstanding CALL;", "FIFO of 64 waiters"]),
        ("disp", "write", ["under the FIFO lock;", "10 s write timeout"]),
        ("ok", "pending", ["the reader resolves it", "on CALLRESULT or error"]),
    ]
    for i, (k, t, sub) in enumerate(row):
        x = 12 + i * 186
        s.box(x, 26, 170, 70, k, t, sub, ssize=10)
        s.num(x + 14, 26, i + 1)
        if i < 4:
            s.line(x + 170, 61, x + 185, 61)
    s.chip(384, 122, "queue full: ErrQueueFull at once", "err", size=10)
    s.chip(384, 146, "ctx cancelled while queued: dropped, never sent", "warn", size=10)
    s.chip(12, 122, "not connected: ErrNotConnected", "err", size=10)
    s.chip(690, 122, "no answer in 30 s: ErrCallTimeout", "err", size=10)
    s.chip(690, 146, "connection ends: ErrConnClosed", "err", size=10)
    s.text(470, 186, "The call timeout starts at step 3, when the call owns the slot. Replies to inbound requests and 2.1 SEND bypass the slot.", 10.5, "ts", maxw=900)
    return s.render()


def linkseq():
    s = Svg(940, 380, "Messages on the gateway link",
            "Sequence between charger, gateway, backend and session registry: Hello and HelloAck, the connect decision and session claim, a forwarded inbound CALL with its reply, a CSMS call fenced by the session epoch, and Drain with DrainAck.", minw=800)
    cols = [("charger", 90, "ext"), ("gateway", 340, "api"), ("backend", 640, "ok"), ("registry", 850, "disp")]
    for name, x, k in cols:
        s.box(x - 70, 8, 140, 30, k, name, [], tsize=12)
        s.raw('<line x1="%d" y1="38" x2="%d" y2="372" stroke="var(--axis)" stroke-width="1"/>' % (x, x))

    def msg(y, a, b, text, dashed=False):
        s.line(a + (6 if b > a else -6), y, b + (-6 if b > a else 6), y, dashed=dashed)
        s.label((a + b) / 2, y - 6, text, size=10.5)

    def phase(y, text):
        s.text(12, y, text, 10.5, "tp", anchor="start", bold=True)

    phase(62, "attach")
    msg(74, 640, 340, "Hello: protocol range, capabilities, pool, version")
    msg(100, 340, 640, "HelloAck: negotiated protocol, shared capabilities", dashed=True)
    phase(130, "connect")
    msg(142, 90, 340, "HTTP upgrade")
    msg(142, 340, 640, "Connect: credentials, headers")
    msg(168, 640, 340, "accept or refuse, attributes, timeouts", dashed=True)
    msg(194, 340, 850, "Claim: next epoch for this charge point")
    phase(224, "inbound")
    msg(236, 90, 340, "CALL")
    msg(236, 340, 640, "envelope: raw payload, epoch, deadline")
    msg(262, 640, 340, "result, CALLERROR, or Nack (redispatch)", dashed=True)
    msg(262, 340, 90, "reply", dashed=True)
    phase(292, "CSMS call")
    msg(304, 640, 340, "Call: epoch, time budget, call id")
    msg(304, 340, 90, "CALL")
    msg(330, 340, 640, "result + Sent: NotSent / SentNoResponse / Answered", dashed=True)
    phase(356, "deploy")
    msg(366, 640, 340, "Drain ... DrainAck: in-flight work answered first")
    return s.render()
