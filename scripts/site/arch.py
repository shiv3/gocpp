"""Second page: architecture and performance. body(h) gets the build module for its helpers."""
import diagrams as d

NAV = [
    ("index.html", "Introduction"), ("layers", "Layers"), ("lifecycle", "Lifecycle"), ("goroutines", "Goroutines"),
    ("paths", "Message paths"), ("split", "Gateway split"), ("perf-footprint", "Footprint"), ("perf-latency", "Latency"),
    ("perf-soak", "Soak"), ("perf-drain", "Drain"), ("perf-capacity", "Capacity"), ("perf-gateway", "Gateway cost"), ("tuning", "Tuning"),
]

G = ("gocpp", "gocpp")
O = ("ocppgo", "ocpp-go v0.19.0")
B = ("before", "gocpp before the footprint work")


def _rows3(before, now, other, fmt):
    return [("before", "before", B[1], before, fmt), ("gocpp", "gocpp", "gocpp now", now, fmt), ("ocpp-go", "ocppgo", O[1], other, fmt)]


def body(h):
    DOCS, table, fig, section = h.DOCS, h.table, h.fig, h.section
    parts = []
    toc = "".join('<li><a href="#%s">%s</a></li>' % (k, v) for k, v in NAV[1:])
    parts.append("""
<header class="hero small"><div class="wrap">
<p class="kicker">gocpp in depth</p>
<h1>Architecture and performance</h1>
<p class="lead">How a connection is built, what happens to a message in each direction, how the gateway split divides the work, and the measurements behind the numbers on the <a href="index.html">introduction page</a>.</p>
<ul class="toclist" aria-label="On this page">%s</ul>
<p class="fine">Everything here describes the code on the main line of the repository and names the file or report it comes from. Performance figures are single runs on one shared machine over loopback, without TLS: read them as orders of magnitude.</p>
</div></header>
""" % toc)

    # ---- layers ----------------------------------------------------------
    body_ = fig(d.layers(), "<b>Layers.</b> The generated per-version packages depend only on <code>core/ocppj</code> (message values) and <code>core/schema</code> (embedded schemas). The dispatcher knows nothing about any OCPP version.") + """
<div class="grid3">
<div class="card"><h4>API layer: csms and cp</h4><p>The generic <code>On</code> / <code>Call</code> / <code>Send</code> functions, options, the HTTP upgrade with its admission checks, session registration, drain. Version-agnostic.</p></div>
<div class="card"><h4>Engine: core/dispatcher</h4><p>One <code>Conn</code> per socket: the reader, inbound scheduling, the call slot, pending calls, keepalive timers and teardown. <code>Pipeline</code> is the payload policy (schema, lenient repair, error mapping).</p></div>
<div class="card"><h4>Wire: ocppj, schema, transport</h4><p>OCPP-J framing and error codes, the JSON-Schema registry and validator, and a small WebSocket interface with an in-memory fake that the tests and benchmarks use.</p></div>
</div>
<p>Heavy integrations (Redis, NATS, gRPC, Temporal) are separate Go modules under <code>addons/</code>, so importing gocpp does not pull them into your dependency tree. The reference commands <code>gocpp-gateway</code> and <code>gocppctl</code> are separate modules too.</p>
<p class="src">Source: <a href="%sarchitecture.md">docs/architecture.md</a>, the package table in the README.</p>
""" % DOCS
    parts.append(section("layers", "Architecture", "Package layers", "Four layers, each depending only on the ones below, with generated code on top and pluggable interfaces on the side.", body_))

    # ---- lifecycle -------------------------------------------------------
    body_ = fig(d.lifecycle(), "<b>From HTTP request to teardown.</b> Everything that can refuse a charger does so before the upgrade, so a refused charger sees an HTTP status it can back off from instead of a socket that opens and closes. On a gateway, step 4 is a question to the processing tier (<code>Upstream.Connect</code>).") + """
<div class="grid2">
<div class="card"><h4>Claim before upgrade</h4><p>With a session registry, the registry assigns the next epoch for the charge point before the WebSocket is accepted, so epoch order follows the order in which sockets were established. A failed upgrade releases the claim.</p></div>
<div class="card"><h4>No handler before OnConnect</h4><p>Inbound handling is held until the connection is published and the connect callback has returned. A handler can rely on <code>Get(id)</code> finding its connection and on <code>OnConnect</code> having run; frames that arrive meanwhile wait in arrival order.</p></div>
<div class="card"><h4>The HTTP handler returns</h4><p>Once the reader runs, the upgrade handler returns and releases its goroutine and request. The connection hangs off the server context, not the request.</p></div>
<div class="card"><h4>One way down</h4><p>Teardown flows from a single <code>cancel(cause)</code>. Senders check the connection context before writing and while waiting, so there is no write deadlock and no channel is closed under a sender. The disconnect callback fires once, with the cause.</p></div>
</div>
<p class="src">Source: <code>csms/server.go</code> (<code>serveWS</code>), <code>core/dispatcher/conn.go</code>, <a href="%sclustering.md">docs/clustering.md</a> (claim, epochs, reconcile), <a href="%sdraining.md">docs/draining.md</a>.</p>
""" % (DOCS, DOCS)
    parts.append(section("lifecycle", "Architecture", "Connection lifecycle", "Admission happens on the plain HTTP request; after the upgrade a connection has exactly one path to its end.", body_))

    # ---- goroutines ------------------------------------------------------
    rows = [
        ["Resident goroutines per idle connection", "8", "1", "2"],
        ["Heap + stack per idle connection", "34 + 27 KB", "7.8 + 4.1 KB", "not measured"],
        ["RSS per idle connection", "61 KB", "13.6 KB", "not measured (35 to 38 KB per connected charger with traffic, capacity ramp)"],
        ["Goroutines per connection under load", "8.3", "1.3", "2.0 (capacity ramp)"],
    ]
    body_ = fig(d.goroutines(), "<b>Anatomy of a connection.</b> Under load the count is about 1.0 to 1.5 goroutines per connection: the reader plus the handlers and call timers in flight.") + """
<p>An earlier design used a goroutine for each role: reader, writer, dispatcher, closer, watchdogs. Each one costs a stack and scheduler work even when the connection is idle, and idle is what a charger connection is most of the time. The redesign removed them one by one:</p>
<ul>
<li><b>No writer goroutine or queue.</b> The goroutine that produced a frame (a handler, a caller) writes it under a per-connection FIFO lock. One lazily armed timer enforces the write timeout and aborts the transport when a write stalls.</li>
<li><b>No dispatcher goroutine.</b> The reader starts a handler goroutine itself when a slot is free, and otherwise parks the frame in the bounded queue that finishing handlers drain.</li>
<li><b>No closer or watchdog goroutines.</b> Teardown is a <code>context.AfterFunc</code> callback; the read-idle watchdog and the WebSocket ping are <code>time.AfterFunc</code> timers.</li>
<li><b>Optional handler pool.</b> At tens of thousands of messages per second, growing the stack of a fresh goroutine per message becomes visible (<code>runtime.copystack</code> was 20% of server CPU samples at 60k messages/s). <code>WithHandlerPool</code> keeps warm workers; it is a hand-off, never a queue, so a slow handler cannot delay another connection. Off by default.</li>
</ul>
""" + table(["", "Before the redesign", "Now", "ocpp-go v0.19.0"], rows) + """
<p class="src">Source: <a href="%sperformance.md">docs/performance.md</a> ("Goroutine model", "Per-connection footprint"), <code>core/dispatcher/conn.go</code>, <code>inbound.go</code>, <code>keepalive.go</code>, <code>handler_pool.go</code>; soak ratios from the 2026-11-20 and 2026-11-21 load-test reports.</p>
""" % DOCS
    parts.append(section("goroutines", "Architecture", "The one-goroutine connection", "A connection is one reader goroutine plus data. Everything else is a timer, a callback, or a goroutine that exists only while a message is handled.", body_))

    # ---- paths -----------------------------------------------------------
    body_ = "<h3>Inbound: a CALL from the charger</h3>" + fig(d.inpath(), "<b>Inbound path.</b> The reader does steps 1 to 4 and never runs a handler. Steps 5 to 8 run on the handler goroutine.") + """
<ul>
<li><b>Parse.</b> A scanner recognises well-formed frames and returns sub-slices of the read buffer instead of copies. Anything else, including every malformed frame, goes to the reflective parser, and a fuzz test checks that both agree on arbitrary input.</li>
<li><b>Routing by type and version.</b> CALLRESULT and CALLERROR resolve a pending call. CALLRESULTERROR and SEND are accepted only on OCPP 2.1 connections. An unknown type is answered with <code>MessageTypeNotSupported</code> on 2.x and left unanswered on 1.6.</li>
<li><b>Scheduling.</b> A CALL takes one of 16 handler slots per connection (and a server-wide slot, if <code>WithGlobalConcurrencyLimit</code> is set). With all slots busy it waits in a queue of 4; with the queue full the reader stops reading. CALLRESULTs keep flowing until that point.</li>
<li><b>Handler context.</b> It keeps the connection's values (charge point id, message id, trace) but is not cancelled by a disconnect; it carries the handler deadline instead. A reply that is ready after the connection is gone is dropped silently.</li>
</ul>
<h3>Outbound: a call from the CSMS</h3>
""" + fig(d.outpath(), "<b>Outbound path.</b> Synchronous <code>Call</code> and callback-style <code>CallAsync</code> share the same per-connection FIFO, so callbacks fire in submission order.") + """
<ul>
<li><b>One call slot per connection.</b> OCPP-J allows one outstanding CALL per direction. Waiting calls queue per connection, so a charger that never answers delays only calls to itself, by one timeout each.</li>
<li><b>Typed errors.</b> A CALLERROR from the charger is a <code>*ocppj.CallError</code>; timeouts, a closed connection and a full queue are sentinels for <code>errors.Is</code>. Through a gateway or a cluster router the same types come back.</li>
<li><b>Measured overhead.</b> In process, a serialized call costs about 7 microseconds and 55 allocations on one connection, within noise of the unserialized mode (<code>core/dispatcher/call_bench_test.go</code>).</li>
</ul>
<p class="src">Source: <code>core/ocppj/parse.go</code>, <code>core/dispatcher/inbound.go</code>, <code>pipeline.go</code>, <code>callslot.go</code>, <code>pending.go</code>; <a href="%susage.md">docs/usage.md</a> ("Handler context and limits", "Async calls"), <a href="%sperformance.md">docs/performance.md</a> ("Concurrency bounds").</p>
""" % (DOCS, DOCS)
    parts.append(section("paths", "Architecture", "Message paths", "What happens to a message in each direction, and where each bound and error comes from.", body_))

    # ---- split -----------------------------------------------------------
    body_ = """
<div class="note"><p><span class="pill">Experimental</span> Gateway mode, its links, the session registry and the tools around them may change in minor releases before v1.0. See the <a href="index.html#scale">introduction</a> for what is stable.</p></div>
<p>The seam of the split is <code>dispatcher.Pipeline</code>, the payload policy of a connection. An embedded server runs it inside the connection; in a split deployment the backend runs it. The gateway keeps everything that belongs to the socket (TLS, framing, call correlation, the call slot, timeouts, keepalive, frame observer) and never reads a payload, which is why it rarely needs a deploy and why error codes are byte-identical in both modes.</p>
""" + fig(d.linkseq(), "<b>The link contract.</b> Solid arrows are requests, dashed arrows answers. Backends dial gateways. The same messages travel over gRPC streams, NATS subjects or an in-process hub.") + """
<div class="grid2">
<div class="card"><h4>Handshake with negotiation</h4><p>The first frame is <code>Hello</code> (protocol range, capabilities, pool, version); <code>HelloAck</code> carries the negotiated protocol and the capability intersection. A disjoint range is rejected loudly, and a frame a peer does not know is answered <code>Unsupported</code> without breaking the stream, so adjacent releases can run mixed.</p></div>
<div class="card"><h4>Epoch fencing</h4><p>The registry's <code>Claim</code> is atomic and assigns the epoch; <code>Release</code> is a compare-and-delete on it. Every request carries the session epoch, and a gateway rejects a call for a session it no longer holds (<code>ErrSessionStale</code>). Each process keeps a lease, so a crashed one invalidates all of its sessions at once.</p></div>
<div class="card"><h4>Deadlines, not clocks</h4><p>Deadlines are never compared across hosts. An inbound message carries the gateway's receive time and deadline and the backend uses the difference as the handler budget; a CSMS call carries the time left.</p></div>
<div class="card"><h4>Backpressure and drain</h4><p>On gRPC, HTTP/2 flow control plus a per-stream in-flight cap is the backpressure. A stopping backend sends <code>Drain</code>, the gateway stops choosing it and acknowledges once its in-flight requests are answered.</p></div>
</div>
<p class="src">Source: <a href="%sgateway.md">docs/gateway.md</a>, <a href="%sdesign/gateway.md">docs/design/gateway.md</a>, <code>addons/link-grpc/proto/gocpp/link/v1/link.proto</code>, <a href="%sclustering.md">docs/clustering.md</a>.</p>
""" % (DOCS, DOCS, DOCS)
    parts.append(section("split", "Architecture", "The gateway split", "Which half of a connection stays at the edge, what crosses the link, and how a session is fenced.", body_))

    # ---- footprint -------------------------------------------------------
    fp = [
        ("Resident goroutines", "per idle connection", _rows3(8, 1, 2, "{:.0f}")),
        ("RSS per idle connection", "KB", _rows3(61, 13.6, None, "{:.1f} KB")),
        ("10,000 idle connections", "process RSS, MB (before: about 610)", _rows3(610, 151, None, "{:,.0f} MB")),
        ("4,000-charger soak, memory", "max RSS, MB (ocpp-go: a frozen process)", _rows3(501, 154, 501, "{:,.0f} MB")),
        ("4,000-charger soak, goroutines", "maximum (ocpp-go: a frozen process)", _rows3(33326, 5323, 22622, "{:,.0f}")),
        ("Allocation per request", "KB at 30k msg/s, lenient validation", _rows3(7.6, 3.5, 2.9, "{:.1f} KB")),
    ]
    panels = "".join(h.bars_panel(t, u, r) for t, u, r in fp)
    tbl = table(["Measure", "Before", "gocpp now", "ocpp-go v0.19.0"],
                [[h.esc(t) + ' <span class="unit">(' + h.esc(u) + ")</span>"] + [h.esc(x[4].format(x[3]) if x[3] is not None else "not measured") for x in r] for t, u, r in fp], num=(1, 2, 3))
    body_ = h.viz("chart-fp", "Per-connection cost: before the redesign, now, and ocpp-go",
                  "Same harness and fleet for all three. Each panel has its own scale; bars start at zero; lower is better.",
                  [("before", B[1] + " (2026-11-20)"), ("gocpp", "gocpp now (2026-11-21)"), O], panels, tbl,
                  "Single runs on one WSL2 machine. Idle figures: 10,000 raw-TCP OCPP WebSocket connections and <code>BenchmarkIdleFootprint</code> (2,000 connections). "
                  "Soak: 4,000 chargers, 40%% mute, 50 CSMS calls/s, 25 minutes, 5 restarts. The upstream ocpp-go v0.19.0 server (2026-11-24 run) froze after each of its six starts, so its soak figures describe a frozen process that keeps accepting sockets; bars are omitted where upstream was not measured. "
                  "ocpp-go allocates less per request than gocpp with lenient validation on (measured in a run without CSMS calls); with validation off gocpp allocates 1.4 KB. "
                  'Source: <a href="%sperformance.md">docs/performance.md</a>.' % DOCS, cols="c3") + """
<h3>RSS is not live memory</h3>
<p>Heap plus stack per connection is the live cost: about 12 KB for an embedded server and 11 to 14 KB for a gateway session. The RSS of a process that has just taken 10,000 real clients through a 2-minute connect ramp is about 23 KB per session, for the embedded server and the gateway alike: the difference is garbage from the connect storm that the next collection has not freed and heap spans not yet returned to the system (about 1.7 times live at the default <code>GOGC=100</code>). The 13.6 KB figure was taken with idle connections that sent nothing.</p>
"""
    parts.append(section("perf-footprint", "Performance", "Footprint", "What a connection costs, how that changed with the one-goroutine redesign, and how it compares.", body_))

    # ---- latency ---------------------------------------------------------
    def lat(a, b, c, dd):
        f = "{:.1f} ms"
        return [("lenient", "gocpp", "gocpp, lenient validation, GOGC=100", a, f), ("validation off", "gocpp", "gocpp, validation off, GOGC=100", b, f),
                ("lenient, GOGC=300", "gocpp", "gocpp, lenient validation, GOGC=300", c, f), ("ocpp-go", "ocppgo", "ocpp-go v0.19.0, GOGC=100", dd, f)]
    lp = [("Heartbeat p99 at 30,000 messages/s", "ms", lat(1.7, 0.8, 1.0, 1.4)), ("Heartbeat p99 at 60,000 messages/s", "ms", lat(7.4, 5.3, 4.2, 5.3))]
    panels = "".join(h.bars_panel(t, u, r, label_w=142, w=380) for t, u, r in lp)
    tbl = table(["Configuration", "p99 at 30k msg/s", "p99 at 60k msg/s"],
                [[h.esc(a[2]), h.esc(a[4].format(a[3])), h.esc(b[4].format(b[3]))] for a, b in zip(lp[0][2], lp[1][2])], num=(1, 2))
    gc_rows = [
        ["gocpp, lenient", "30k", "104 MB/s", "2.3", "2.3%", "1.7 ms", "151 MB"],
        ["gocpp, lenient, GOGC=300", "30k", "104 MB/s", "0.7", "0.8%", "1.0 ms", "264 MB"],
        ["gocpp, validation off", "30k", "41 MB/s", "0.9", "0.8%", "0.8 ms", "144 MB"],
        ["ocpp-go v0.19.0", "30k", "2.92 KB per request", "1.8", "1.1%", "1.4 ms", "167 MB"],
        ["gocpp, lenient", "60k", "202 MB/s", "4.7", "5.2%", "7.4 ms", "151 MB"],
        ["gocpp, lenient, GOGC=300", "60k", "204 MB/s", "1.4", "1.7%", "4.2 ms", "270 MB"],
        ["gocpp, validation off", "60k", "80 MB/s", "1.8", "1.8%", "5.3 ms", "146 MB"],
        ["ocpp-go v0.19.0", "60k", "2.89 KB per request", "3.5", "2.2%", "5.3 ms", "168 MB"],
    ]
    body_ = h.viz("chart-lat", "Heartbeat p99 against message rate",
                  "4,000 chargers with timers sped up to reach the rate; server pinned to 4 cores. One panel per rate, each with its own scale; lower is better.",
                  [G, O], panels, tbl,
                  "Single runs; percentiles are histogram bucket upper bounds with 12%% resolution, so one or two buckets of difference are noise (at 30k, 0.8 to 1.0 ms are neighbouring buckets). "
                  "Neither server is CPU-saturated at these rates (1 to 2 of 4 cores). p50 is 0.1 ms in every row. "
                  "The ocpp-go v0.19.0 bar and table rows come from the 2026-11-22 run without CSMS calls, in which gocpp v0.3.0 also measured 1.4 and 5.3 ms; with 50 CSMS calls per second the upstream server froze before the 30k step. "
                  "The gocpp bars are from the 2026-11-21 run. GOGC=300 was not measured for ocpp-go.", cols="c2") + """
<h3>Why the tail grows: time inside GC mark phases</h3>
<p>At these rates the servers have CPU to spare, so the tail is not queueing for CPU. It follows the share of wall time the process spends inside garbage-collector mark phases, when goroutines are asked to assist and scheduling gets less regular. That share is driven by the allocation rate and by how often a cycle runs:</p>
""" + table(["Server", "Messages/s", "Allocation", "GC cycles/s", "Time in mark", "Heartbeat p99", "Max RSS"], gc_rows, num=(1, 2, 3, 4, 5, 6),
            caption="From GODEBUG=gctrace=1. The ocpp-go rows are from the 2026-11-22 run, the gocpp rows from 2026-11-21 (allocation per request: gocpp 3.5 KB). Rule of thumb from docs/performance.md: in-mark share is about alloc MB/s x 0.3 ms / (GOGC/100) per second; keep it under 1%.") + """
<ul>
<li><b>Allocate less.</b> The per-message work (scanner parse, frames that reference the read buffer, a reflection-free lenient decoder, an empty-object fast path) brought lenient mode from 7.6 to 3.5 KB per request, and the p99 at 30k messages/s from 18 ms to 1.7 ms in the same harness.</li>
<li><b>Collect less often.</b> <code>GOGC=300</code> cuts the cycles to a third and the p99 with them, for about 110 MB more RSS at 4,000 connections. Compare libraries at the same setting.</li>
<li><b>Skip what you do not need.</b> <code>WithOutboundValidation(false)</code> removes the response-side validation pass, about half of the lenient cost per request, once your handler output is covered by tests.</li>
</ul>
<p>The price of validation is CPU: at 30k messages/s gocpp with lenient validation used 1.16 cores and 0.81 with validation off (in the 2026-11-22 run gocpp used 0.98 cores and upstream ocpp-go 1.01); the handler pool (64 workers) brought lenient down to 0.91.</p>
"""
    parts.append(section("perf-latency", "Performance", "Latency under load", "Heartbeat round-trip tail at 30,000 and 60,000 messages per second, and what drives it.", body_))

    # ---- soak ------------------------------------------------------------
    soak_rows = [
        ["gocpp, default (lenient)", "154 MB", "5,323", "0.10", "0.2 / 0.5 / 0.8 ms", "6.0 to 6.8 s"],
        ["gocpp, ocpp-go compat preset", "149 MB", "5,318", "0.10", "0.2 / 0.5 / 0.8 ms", "6.0 to 6.8 s"],
        ["gocpp, handler pool 64", "158 MB", "5,373", "0.10", "0.2 / 0.5 / 0.7 ms", "6.0 to 6.8 s"],
        ["ocpp-go v0.19.0 (froze after each of 6 starts)", "501 MB", "22,622", "0.11", "0.2 / 0.7 / 1.0 ms, only in the windows in which it answered", "never reached (0 chargers connected before each restart)"],
    ]
    body_ = """
<p>The soak is the unglamorous case a CSMS actually lives in: 4,000 OCPP 1.6J chargers for 25 minutes, 40% of them mute (connected, sending their own traffic, never answering a CSMS call), 3 flapping, 20 fighting over duplicate ids, 15% running transactions, 50 CSMS calls per second of which about 40% land on mute chargers and time out after 30 seconds, and five SIGTERM restarts of the server.</p>
""" + table(["Server", "Max RSS", "Max goroutines", "Mean CPU (cores)", "Heartbeat p50 / p95 / p99", "99% reconnected after a restart"], soak_rows, num=(1, 2, 3)) + """
<ul>
<li><b>gocpp: no freeze, crash or leak in any run.</b> RSS slope stayed within 3 MB per minute in both directions, goroutines per connection were flat, and file descriptors did not drift.</li>
<li><b>Upstream ocpp-go v0.19.0 froze.</b> It stopped answering every charger after each of the six starts and stayed stopped until the next restart: 0 chargers connected at the end, 307,154 BootNotification requests closed, RSS up to 501 MB, 22,622 goroutines. It also froze with 5 CSMS calls per second, and in a capacity ramp with no calls at about 31,700 chargers; the two 4,000-charger runs without calls did not freeze. The goroutine dumps show the dispatcher's message pump and its senders blocked on the dispatcher's own channels, described in <a href="%socpp-go-structural-issues.md">docs/ocpp-go-structural-issues.md</a>.</li>
<li><b>Mute chargers stall nothing in gocpp.</b> About 26,500 call timeouts per run; the call-outcome mix of gocpp, the compatibility preset and the handler-pool run was the same to within 0.1%% (59.3%% answered, 35.6%% timed out).</li>
<li><b>Restart recovery is the clients' doing.</b> For gocpp, 99%% of the fleet was back 6 to 7 seconds after the server was healthy: the time is reconnect jitter and backoff, not server work.</li>
<li><b>A hot spot hits a bound, not the heap.</b> In an earlier run that aimed 300 calls per second at 100 mute chargers, gocpp answered the excess with <code>ErrQueueFull</code> because the per-connection wait queue is bounded.</li>
</ul>
<p class="src">Source: the load-test runs of 2026-11-20, 2026-11-21 and 2026-11-24 (harness <code>interop/loadtest</code>). Single runs, one machine, loopback, no TLS.</p>
""" % DOCS
    parts.append(section("perf-soak", "Performance", "A 4,000-charger soak", "Mute chargers, flapping connections, duplicate ids and server restarts, for 25 minutes.", body_))

    # ---- drain -----------------------------------------------------------
    f = "{:,.0f}/s"
    dr = [("plain restart", "gocpp", "SIGTERM, exit at once", 2000, f), ("accept limit", "gocpp", "SIGTERM + WithAcceptRateLimit(200, 400)", 984, f),
          ("drain 30 s", "gocpp", "Drain, window 30 s", 161, f), ("drain 60 s", "gocpp", "Drain, window 60 s", 90, f)]
    drain_rows = [
        ["SIGTERM, exit at once", "about 2,000/s (all 4,000 within 3 s)", "0.3 to 0.4", "3 s, with every charger offline until then"],
        ["SIGTERM + <code>WithAcceptRateLimit(200, 400)</code> on the new process", "984 attempts/s, 594 upgrades/s", "0.24", "33 s"],
        ["<code>Drain</code>, window 30 s (target 148/s)", "161/s", "0.12", "29 s"],
        ["<code>Drain</code>, window 60 s (target 74/s)", "90/s", "0.12", "56 s, with about 3,850 of 4,000 connected throughout"],
    ]
    tbl = table(["Procedure", "Peak connect attempts at the new process", "Peak server CPU (cores, 250 ms)", "Fleet back to 99%"], drain_rows)
    body_ = h.viz("chart-drain", "Peak connect attempts per second at the new process",
                  "Rolling restart of 4,000 simulated 1.6J chargers with jittered backoff. One series; lower means a flatter reconnect ramp.",
                  [G], h.bars_panel("Reconnect peak", "connect attempts per second", dr, label_w=92, w=380), tbl,
                  "Single runs, one machine, no TLS and no authentication. The plain-restart value is approximate (\"about 2,000 per second\"). "
                  'Source: <a href="%sdraining.md">docs/draining.md</a>, "Measured"; harness on branch <code>test/loadtest-drain</code>.' % DOCS, cols="c2") + """
<p>The harness records a table of peaks, not a time series, so there is no curve to draw; the "Show as table" view has the CPU and recovery columns. The reconnect peak follows the configured pace (within 25% of the target; the excess is the chargers' own retry jitter), and the fleet stays about 96% connected through a 60-second drain instead of 0% for the restart. The bare server handles 4,000 reconnects in 3 seconds without trouble, so the saving in absolute CPU is small here; the pace matters where each connect costs more: TLS handshakes, authentication against a database, per-connect application work.</p>
"""
    parts.append(section("perf-drain", "Performance", "Drain against a plain restart", "Turning a reconnect storm into a ramp with Server.Drain and the accept rate limit.", body_))

    # ---- capacity --------------------------------------------------------
    fc, fk, fm, fs = "{:,.0f}", "{:.1f} KB", "{:,.0f}/s", "{:.1f} s"
    cap = [
        ("Chargers held with Heartbeat p99 at or under 200 ms", "1 core / 2 GB, last stable ramp step", [
            ("lenient", "gocpp", "gocpp, lenient schema, GOMEMLIMIT 80%", 93700, fc),
            ("schema off", "gocpp", "gocpp, schema off, GOMEMLIMIT 80%", 100000, fc),
            ("no GOMEMLIMIT", "gocpp", "gocpp, lenient schema, no GOMEMLIMIT", 78000, fc),
            ("ocpp-go", "ocppgo", "ocpp-go v0.19.0, GOMEMLIMIT 80% (froze at about 28,000)", 27774, fc)]),
        ("Server memory per connection", "KB of anonymous memory at that step", [
            ("lenient", "gocpp", "gocpp, lenient schema, GOMEMLIMIT 80%", 16.8, fk),
            ("schema off", "gocpp", "gocpp, schema off, GOMEMLIMIT 80%", 15.3, fk),
            ("no GOMEMLIMIT", "gocpp", "gocpp, lenient schema, no GOMEMLIMIT", 23.6, fk),
            ("ocpp-go", "ocppgo", "ocpp-go v0.19.0, GOMEMLIMIT 80% (RSS per connected charger)", 37.0, fk)]),
        ("Throughput at CPU saturation", "messages per second, 5,000 chargers, timers sped up", [
            ("1 core", "gocpp", "gocpp, lenient schema, 1 core / 2 GB", 47700, fm),
            ("2 cores", "gocpp", "gocpp, lenient schema, 2 cores / 4 GB", 97600, fm),
            ]),
    ]
    panels = "".join(h.bars_panel(t, u, r, label_w=116, w=350) for t, u, r in cap)
    cap_rows = [
        ["1 core / 2 GB", "gocpp, lenient", "80%", "93,700 (p99 57 ms)", "96,000: Heartbeat p99 1.9 s, heap at the memory limit", "16.8 KB", "47.7k msg/s (p99 100 ms)"],
        ["1 core / 2 GB", "gocpp, schema off", "80%", "about 100,000", "117,400: memory above 95%, CPU at 1.00 core", "15.3 KB", "not run"],
        ["1 core / 2 GB", "gocpp, lenient", "none", "78,000 (p99 46 ms)", "78,000: memory above 95%, CPU 0.24 core", "23.6 KB", "not run"],
        ["1 core / 2 GB", "gocpp, lenient, Heartbeat 60 s", "80%", "about 100,000", "115,100: memory above 95%, CPU at 1.00 core", "15.8 KB", "not run"],
        ["1 core / 2 GB", "ocpp-go v0.19.0", "80%", "26,000 to 31,000 (Heartbeat p99 10 to 29 ms until the freeze)", "freeze at about 28,000 to 32,000 chargers, then memory above 95%", "37 KB (RSS)", "not run"],
        ["2 cores / 4 GB", "gocpp, lenient", "80%", "at least 112,000 (p99 2.4 ms)", "112,400: the test machine ran out of memory; server at 2.8 of 4 GB, 0.33 core", "24.0 KB", "97.6k msg/s (p99 41 ms)"],
        ["2 cores / 4 GB", "gocpp, schema off", "80%", "at least 111,700 (p99 10 ms)", "111,700: test machine memory", "24.8 KB", "not run"],
    ]
    tbl = table(["Server size", "Server", "GOMEMLIMIT", "Chargers held, Heartbeat p99 at or under 200 ms", "Where the ramp stopped", "Memory per connection", "Saturation throughput, 5,000 chargers"], cap_rows)
    storm = [
        ("1 core / 2 GB, 90,000 chargers", "seconds from the new process being ready to 99% of the fleet connected", [
            ("plain restart", "gocpp", "SIGTERM, new process", 12.3, fs),
            ("drain 15 s", "gocpp", "-drain-window 15s", 62.4, fs),
            ("accept 3,000/s", "gocpp", "-accept-rate 3000", 68.9, fs),
            ("drain + accept", "gocpp", "-drain-window 15s -accept-rate 3000", 68.7, fs)]),
        ("2 cores / 4 GB, 100,000 chargers", "seconds from the new process being ready to 99% of the fleet connected", [
            ("plain restart", "gocpp", "SIGTERM, new process", 6.8, fs),
            ("drain + accept", "gocpp", "-drain-window 15s -accept-rate 6000", 71.5, fs)]),
    ]
    spanels = "".join(h.bars_panel(t, u, r, label_w=112, w=380) for t, u, r in storm)
    storm_rows = [
        ["1 core / 2 GB", "90,000", "none", "0.6 s", "12.3 s", "16.4 s", "2.7 s"],
        ["1 core / 2 GB", "90,000", "<code>-drain-window 15s</code>", "15.6 s", "62.4 s", "80.5 s", "13.7 s"],
        ["1 core / 2 GB", "90,000", "<code>-accept-rate 3000</code>", "0.6 s", "68.9 s", "73.3 s", "14.0 s"],
        ["1 core / 2 GB", "90,000", "<code>-drain-window 15s -accept-rate 3000</code>", "15.5 s", "68.7 s", "86.7 s", "9.4 s"],
        ["2 cores / 4 GB", "100,000", "none", "0.6 s", "6.8 s", "10.8 s", "1.4 s"],
        ["2 cores / 4 GB", "100,000", "<code>-drain-window 15s -accept-rate 6000</code>", "15.7 s", "71.5 s", "89.9 s", "9.4 s"],
    ]
    stbl = table(["Server size", "Fleet", "Server options", "Exit after SIGTERM", "99% back, from ready", "99% back, from SIGTERM", "Heartbeat p99 in the worst 10 s"], storm_rows, num=(1, 3, 4, 5, 6))
    body_ = """
<p>The measurements above use 4,000 chargers and a server with four cores. This one asks the sizing question directly: how many chargers does one embedded gocpp server hold when its cgroup gives it one core and 2 GB, or two cores and 4 GB? The fleet behaves like a quiet production fleet: Heartbeat every 300 seconds, 20% of the chargers charging and sending MeterValues every 60 seconds, 40% mute, and CSMS calls at one per second per 1,000 chargers. The ramp adds 2,000 chargers every 30 seconds until memory, latency or connect errors cross a limit.</p>
<ul class="tiles" aria-label="Recommended sizing">
<li class="tile"><div class="lab">Chargers on 1 core / 2 GB, with headroom</div><div class="val">60,000</div><div class="cmp">about 1.0 GB and 0.2 to 0.3 core; tested ceiling 93,700 to 117,400</div></li>
<li class="tile"><div class="lab">Chargers on 2 cores / 4 GB, with headroom</div><div class="val">100,000</div><div class="cmp">112,000 reached at 2.8 GB and 0.33 core; the test machine gave out first</div></li>
<li class="tile"><div class="lab">Saturation throughput, 1 core</div><div class="val">45,000<small>msg/s</small></div><div class="cmp">rounded down from 47,700 measured; about 95,000 on 2 cores</div></li>
<li class="tile"><div class="lab">Server memory per connection</div><div class="val">16<small>KB</small></div><div class="cmp">when GOMEMLIMIT squeezes the heap; about 24 KB with memory to spare</div></li>
</ul>
<p class="fine">The two sizing figures are recommendations derived from the runs below, not measured limits. Plain <code>ws://</code> over loopback, single runs, trivial handlers.</p>
""" + h.viz("chart-cap", "One server on 1 core and 2 GB: connections, memory, throughput",
            "Server limited by cgroups (CPU quota, memory limit, no swap) and pinned; the load generator runs on other cores. Each panel has its own scale; bars start at zero.",
            [("gocpp", "gocpp"), O], panels, tbl,
            "Single runs on one shared WSL2 machine over loopback, without TLS. Chargers held: the last 30-second ramp step with the whole fleet connected and Heartbeat p99 at or under 200 ms (histogram buckets, 12%% resolution); "
            "\"about\" values are where p99 first crossed 200 ms. More chargers is better in the first panel, less memory in the second, more messages in the third. "
            "The upstream ocpp-go v0.19.0 server (2026-11-22 and 2026-11-24 runs) stopped at a dispatcher freeze, not at a CPU or memory limit, so its figures say nothing about its capacity above about 30,000 chargers; its throughput at saturation was not measured. "
            "Throughput is a closed loop with 5,000 chargers and the generator close to its own limit; gocpp ran with lenient schema validation on. "
            "Source: capacity run of 2026-11-22 (harness <code>interop/loadtest</code>).", cols="c3") + """
<h3>What limits a small server</h3>
<ul>
<li><b>Memory, then the collector, then CPU.</b> On 1 core / 2 GB with lenient validation the Heartbeat p99 went from 57 ms at 93,700 chargers to 1.9 s at 96,000 while CPU stayed under 0.8 core: the heap had reached <code>GOMEMLIMIT</code> and the collector ran continuously. With schema validation off a connection costs less (15.3 against 16.8 KB) and the server got to about 100,000 before p99 crossed 200 ms, now with CPU at the one-core quota.</li>
<li><b>Set <code>GOMEMLIMIT</code>.</b> Without it the same server stopped at 78,000 chargers at 0.24 core, because the heap was allowed to grow to 23.6 KB per connection and hit the cgroup limit. At 80% of the limit the collector works harder and holds 16.8 KB.</li>
<li><b>Two cores and 4 GB were not exhausted.</b> At 112,000 chargers the server used 2.8 GB and 0.33 core with a Heartbeat p99 of 2.4 ms; the ramp ended because the test machine ran out of memory for the load generator. The figure is a lower bound. Memory arithmetic puts the real ceiling at roughly 170,000 to 250,000; that was not measured.</li>
<li><b>A fleet this quiet barely uses the CPU.</b> 90,000 chargers held for five minutes cost 0.27 core on average (Heartbeat p99 at most 51 ms). A five times higher heartbeat rate (every 60 seconds) brought the p99 over 200 ms at 100,000 chargers and the server to a full core at 115,000.</li>
<li><b>Throughput scales with cores.</b> With 5,000 chargers and compressed timers, one core saturated at 47,700 messages per second (p99 100 ms; under 37,000 per second the p99 stayed below 64 ms) and two cores at 97,600 (p99 41 ms). Throughput at saturation was not measured for ocpp-go; gocpp ran with lenient schema validation on and was not measured with it off in this experiment.</li>
</ul>
<h3>TLS changes the arithmetic</h3>
<p>The harness speaks plain <code>ws://</code>. A separate micro-benchmark of Go's <code>crypto/tls</code> (TLS 1.3, ECDSA P-256, no client certificates, 20,000 idle connections, one core) measured about 136 us of server CPU per full handshake, or about 7,400 handshakes per second per core, and about 15 KB more memory per idle TLS connection. As an estimate, not a measurement: terminating TLS in the server roughly doubles the memory per connection, so 1 core / 2 GB holds about 50,000 to 60,000 chargers, and reconnecting 90,000 chargers costs about 12 seconds of one core in handshakes alone. Terminating TLS on a proxy or load balancer keeps the numbers above.</p>
<h3>A reconnect storm after a restart</h3>
<p>The fleet is held for five minutes, then the server gets one SIGTERM and a new process starts two seconds later. Chargers reconnect with jittered exponential backoff (base 1 s, cap 30 s).</p>
""" + h.viz("chart-storm", "Time until 99% of the fleet is connected again",
            "One server restarted in place, gocpp with lenient validation. Seconds from the new process being ready; shorter is better. One panel per server size, each with its own scale.",
            [G], spanels, stbl,
            "Single runs, loopback, no TLS and no authentication. Heartbeat p99 is the worst 10-second window and is only an order of magnitude. "
            'Source: capacity run of 2026-11-22, "Reconnect storm".', cols="c2") + """
<ul>
<li><b>A plain restart of a single server is fast.</b> 99%% of 90,000 chargers were back 12 seconds after the new process was ready on one core, and 99%% of 100,000 after 7 seconds on two. Server CPU peaked at 0.65 and 0.55 core without throttling; memory peaked at 91%% of the 2 GB limit.</li>
<li><b>Drain and the accept-rate limit made the single-server restart slower</b>: 62 to 72 seconds to 99%%. The likely reason, not verified with a trace: a server that refuses upgrades during its drain window, or paces them at 3,000 per second, sends the chargers into longer backoffs, and with only one server there is nowhere else for them to go.</li>
<li><b>They pay off when another instance absorbs the drained sessions.</b> That is the rolling-restart case measured <a href="#perf-drain">above</a>, where the fleet stays connected while one process drains. They also protect a server whose connects are expensive (TLS, authentication against a database).</li>
</ul>
<h3>Caveats</h3>
<ul>
<li>Loopback, no TLS, no network latency or loss; the generator and the server share one machine (separate cores, shared memory bandwidth and cache).</li>
<li>Every number is a single run. Throughput varies by 5 to 10%% between runs, and the point where a latency criterion trips varies more, because p99 sits near a cliff when the heap is close to <code>GOMEMLIMIT</code>.</li>
<li>The two-core figures are lower bounds set by the test machine's memory.</li>
<li>Upstream ocpp-go v0.19.0 froze at about 28,000 to 32,000 chargers in this profile (about 31,700 with no CSMS calls), so there is no ocpp-go number for the larger fleets.</li>
<li>The servers keep no event history and the handlers are trivial. A server that stores events or talks to a database needs more memory and CPU per message.</li>
</ul>
<p class="src">Source: the capacity run of 2026-11-22 and the 2026-11-24 repeat for the drop-in module (harness <code>interop/loadtest</code>).</p>
"""
    parts.append(section("perf-capacity", "Performance", "Sizing and capacity", "How many chargers one server holds on 1 core / 2 GB and on 2 cores / 4 GB, and what a restart of that fleet looks like.", body_))

    # ---- gateway cost ----------------------------------------------------
    hop_rows = [
        ["Added latency, 7.7k msg/s", "Heartbeat p50 0.20 to 0.35 ms (+0.15), p99 0.49 to 0.96 ms (+0.5)"],
        ["Added latency, steady state at about 300 msg/s", "p50 / p99 0.5 / 1.1 ms split against 0.3 / 0.5 ms embedded"],
        ["Gateway throughput", "22,700 msg/s per core at 36k msg/s"],
        ["CPU per message, whole deployment", "76.2 us split against 59.1 us embedded at 35k msg/s (1.29x); other sessions on the same machine measured 1.4x to 1.6x"],
        ["The gRPC hop itself", "about 25 us of CPU per message over both ends"],
        ["In-process link (CI gate)", "+13 us mean, +31 us p99; 31 allocations / 2.4 KB per round trip against 17 / 1.8 KB embedded"],
        ["gRPC hop, loopback (CI gate)", "p50 0.10 ms, p99 0.3 ms at 5,000 msg/s open loop"],
    ]
    linger_rows = [
        ["embedded server, for scale", "52 to 60", "0.2 / 1.1"],
        ["split, linger off (the default)", "87", "0.6 to 0.7 / 29 to 32"],
        ["linger 200 us on both ends", "76 (-12%)", "1.1 to 1.2 / 21"],
        ["linger 500 us on both ends", "60 (-31%)", "2.4 / 23 to 26"],
    ]
    bench_rows = [
        ["1,000", "278 / 293 / 332 (not armed)", "0.17 / 0.19 / 0.22"],
        ["5,000", "118 / 114 (not armed) / 85", "0.13 / 0.13 / 2.5"],
        ["20,000", "96 / 50 / 37", "0.20 / 1.5 / 2.8"],
        ["40,000", "66 / 38 / 31", "0.23 / 0.87 / 2.5"],
    ]
    body_ = """
<div class="note"><p><span class="pill">Experimental</span> These figures describe the experimental gateway mode with the gRPC link.</p></div>
<p>Splitting a server adds a hop to every message. The design set targets for that hop before it was built; this is what was measured against them, with 4,000 chargers over loopback, one or two gateways and two or three backends.</p>
""" + table(["", "Measured"], hop_rows) + """
<p>The gateway's profile is dominated by system calls: 27% of its CPU in <code>write(2)</code>, split about evenly between the WebSocket reply and the link stream. When a gateway is the bottleneck, p99 grows to tens of milliseconds; that is queueing, not hop cost. Size gateways below about 70% CPU.</p>
<h3>Adaptive write linger: CPU against latency</h3>
<p>Writes on a link stream are always coalesced: frames queued while a write is in progress leave together, with no timer, so an idle stream adds no latency. The optional write linger goes further: on a busy stream, a frame that would be written alone waits up to <code>d</code> for the frames about to follow. It is adaptive per stream (armed only while frames follow each other at an average interval of at most <code>d/2</code>) and off by default.</p>
""" + table(["Gateway on 2 CPUs, 36,000 msg/s", "CPU per message (us)", "Heartbeat p50 / p99 (ms)"], linger_rows, num=(1, 2),
            caption="Whole deployment (gateway plus two backends), two passes each on a shared machine: compare rows with each other only. The p99 column is the harness near its limit in every split row.") + table(
            ["Messages/s on one stream", "CPU us per message: off / 200 us / 500 us", "Round-trip p50 ms: off / 200 us / 500 us"], bench_rows,
            caption="BenchmarkGRPCHopRate: one stream over loopback, 1,000 open-loop senders, 4 cores.") + """
<ul>
<li><b>Below its threshold the linger changes nothing</b>, because it is not armed: about 10,000 frames per second per stream for 200 us, 4,000 for 500 us. With ten backends behind a gateway the same traffic never reaches it.</li>
<li><b>Above it, it trades latency for CPU</b>: roughly 0.6 ms more per round trip at 200 us and 1.8 ms at 500 us, for 12%% to 31%% less CPU in the fleet measurement and up to half on a single saturated stream. The saving is fewer system calls and thread wake-ups on both ends.</li>
<li><b>The worst case is just above the threshold</b>: 5,000 messages per second on one stream with a 500 us linger took a round trip from 0.13 to 2.5 ms.</li>
<li><b>A fixed delay was measured and dropped</b>: at 260 messages per second a fixed 100 us raised the Heartbeat p50 from 0.8 to 2.1 ms and total CPU by a quarter.</li>
</ul>
<p>Guidance from the documentation: leave it off unless one gateway-backend stream carries more than about 10,000 messages per second and a millisecond of latency is a fair price for the CPU; then start with 200 us on both ends.</p>
<p class="src">Source: <a href="%sgateway.md">docs/gateway.md</a> ("Performance", "Write linger"), the gateway load-test reports of 2026-11-21, gates <code>TestGateHopOverhead</code> and <code>TestGateGRPCHopLatency</code>. Not measured: TLS handshake capacity, gateway crash recovery, behaviour behind a real L4 load balancer.</p>
""" % DOCS
    parts.append(section("perf-gateway", "Performance", "What the gateway hop costs", "Added latency, CPU per message, and the opt-in write linger that trades one for the other.", body_))

    # ---- tuning ----------------------------------------------------------
    tune_rows = [
        ["<code>GOMAXPROCS</code>", "Leave unset", "Go 1.25 and later follows the cgroup CPU limit. Give the process at least 2 CPUs so GC workers do not time-slice with the reader and handlers."],
        ["<code>GOMEMLIMIT</code>", "Always set, 80 to 90% of the container limit", "Turns an out-of-memory kill into a more aggressive collection, and is what makes a high GOGC safe. 4,000 connections need about 150 MB at GOGC=100."],
        ["<code>GOGC</code>", "100 below about 10k requests/s; 300 above", "Moves mark phases out of the way of requests; RSS grows roughly in proportion. Never <code>GOGC=off</code> without <code>GOMEMLIMIT</code>; no heap ballast."],
        ["Schema mode", "<code>WithLenientSchema()</code> in production", "Protects handlers and repairs firmware quirks. Off is for trusted fleets or a validating proxy in front."],
        ["<code>WithOutboundValidation(false)</code>", "Once handler output is tested", "Removes about half of the lenient cost per request."],
        ["<code>WithHandlerPool</code>", "Off; try 64 workers at high rates", "CPU -22% at 30k and -18% at 60k msg/s in the load test; no effect at a few hundred msg/s; larger pools get slower."],
        ["<code>WithGlobalConcurrencyLimit</code>", "Set when handlers hit a shared backend", "Caps handlers in flight process-wide; excess waits (backpressure), it is not rejected."],
        ["<code>WithMaxConnections</code>, <code>WithAcceptRateLimit</code>", "Set for production", "Bounds sockets and the handshake rate; refusals are HTTP 503 with Retry-After before the upgrade."],
        ["permessage-deflate", "Keep off", "Compression with context takeover costs far more per connection than everything else on this page saves."],
    ]
    body_ = table(["Knob", "Recommendation", "Why"], tune_rows) + """
<h3>What to watch</h3>
<ul>
<li><code>go_goroutines</code> per connection: about 1.0 to 1.5. A rising ratio means handlers or calls are piling up.</li>
<li>Scheduler latency (<code>/sched/latencies:seconds</code>): a p99 above a few milliseconds means CPU starvation or GC.</li>
<li>GC assist CPU: sustained non-zero assist means the allocation rate outruns the collector.</li>
<li><code>Conn.Stats()</code> for per-connection pending and queued calls; <code>gocpp_schema_soft_violations_total</code> for what lenient mode is repairing.</li>
</ul>
<p>Measure your own handlers before tuning: the numbers on this page use trivial handlers, so they show the library's share of the cost. Run load tests without the race detector and with pprof on.</p>
<p class="src">Source: <a href="%sperformance.md">docs/performance.md</a> ("Runtime settings", "Schema validation cost", "Handler pool sizing", "Tuning summary").</p>
""" % DOCS
    parts.append(section("tuning", "Performance", "Runtime and tuning guidance", "The settings that matter for a server full of long-lived connections, and the defaults that are already right.", body_))
    return "\n".join(parts)
