#!/usr/bin/env python3
"""Builds the gocpp introduction site (GitHub Pages) into site/.

    python3 scripts/site/build.py

Standard library only. Output: site/index.html, site/assets/{style.css,app.js,
favicon.svg}, site/.nojekyll. The Go snippets under snippets/ are complete
programs that were compiled against the module; keep them compiling when the
API changes. Every number on the page names its source next to it.
"""
import html
import pathlib
import re
import shutil
import sys

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parent.parent
OUT = ROOT / "site"
sys.path.insert(0, str(HERE))

import diagrams  # noqa: E402
import svg as svglib  # noqa: E402

REPO = "https://github.com/shiv3/gocpp"
DOCS = REPO + "/blob/main/docs/"
PKG = "https://pkg.go.dev/github.com/shiv3/gocpp"
COMPAT = REPO + "/blob/main/compat/"
REPLACE_019 = "replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.3"


def esc(s):
    return html.escape(str(s), quote=True)


def go_code(name):
    """Escape a Go snippet and mark comments and a few keywords."""
    src = (HERE / "snippets" / name).read_text().rstrip("\n")
    out = []
    for line in src.split("\n"):
        line = line.replace("\t", "    ")
        m = re.search(r"(^|\s)//", line)
        code, comment = (line[: m.start() + len(m.group(1))], line[m.start() + len(m.group(1)):]) if m else (line, "")
        code = esc(code)
        code = re.sub(r"\b(package|import|func|type|struct|return|if|go|defer|var)\b", r'<span class="k">\1</span>', code)
        if comment:
            code += '<span class="c">' + esc(comment) + "</span>"
        out.append(code)
    return "\n".join(out)


def table(head, rows, caption=None, cls="", num=()):
    """rows: lists of cells; a cell may be raw html. The first cell is a row header."""
    h = ['<div class="tablewrap %s"><table>' % cls]
    if caption:
        h.append("<caption>%s</caption>" % caption)
    h.append("<thead><tr>")
    for i, c in enumerate(head):
        h.append('<th scope="col"%s>%s</th>' % (' class="num"' if i in num else "", c))
    h.append("</tr></thead><tbody>")
    for r in rows:
        h.append("<tr>")
        for i, c in enumerate(r):
            if i == 0:
                h.append('<th scope="row">%s</th>' % c)
            else:
                h.append("<td%s>%s</td>" % (' class="num"' if i in num else "", c))
        h.append("</tr>")
    h.append("</tbody></table></div>")
    return "".join(h)


# --------------------------------------------------------------------------
# Charts: gocpp against ocpp-go, one small panel per measure (never two scales
# on one axis). Numbers: the 2026-11-22 and 2026-11-24 comparisons of gocpp and upstream
# ocpp-go v0.19.0 (same harness); upstream froze in the soak.
PANELS = [
    # title, unit, gocpp, ocpp-go, format, note
    ("4,000-charger soak, chargers connected", "median over 25 minutes, 40% mute chargers", 3989, 0, "{:,.0f}", "higher is better; ocpp-go froze after every start"),
    ("4,000-charger soak, memory", "max RSS over 25 minutes, MB", 163, 501, "{:,.0f} MB", "lower is better; ocpp-go's figure is a frozen process"),
    ("4,000-charger soak, goroutines", "maximum over 25 minutes", 5306, 22622, "{:,.0f}", "lower is better; ocpp-go's figure is a frozen process"),
    ("Max RSS at 30k msg/s", "MB, no CSMS calls", 124, 167, "{:,.0f} MB", "lower is better"),
    ("Heartbeat p99 at 30k msg/s", "ms, no CSMS calls", 1.4, 1.4, "{:.1f} ms", "same bucket"),
    ("Heartbeat p99 at 60k msg/s", "ms, no CSMS calls", 5.3, 5.3, "{:.1f} ms", "same bucket"),
    ("Server CPU at 30k msg/s", "cores (of 4)", 0.98, 1.01, "{:.2f} cores", "lower is better; within noise"),
    ("Allocation per request at 30k msg/s", "KB", 3.57, 2.92, "{:.2f} KB", "lower is better; ocpp-go is ahead"),
]
SERIES = [("gocpp", "gocpp (lenient schema validation)"), ("ocppgo", "ocpp-go v0.19.0 (upstream)")]


def bar_panel(i, title, unit, a, b, fmt, note):
    w, h = 300, 66
    x0, maxw = 62, 150
    top = max(a, b)
    parts = [
        '<svg class="ch" viewBox="0 0 %d %d" role="img" aria-label="%s">' % (
            w, h, esc("%s: gocpp %s, ocpp-go %s" % (title, fmt.format(a), fmt.format(b))))
    ]
    for j, ((key, name), v) in enumerate(zip(SERIES, (a, b))):
        y = 6 + j * 30
        bw = max(3.0, maxw * v / top)
        r = min(4.0, bw / 2)
        d = "M%.1f,%d h%.1f a%.1f,%.1f 0 0 1 %.1f,%.1f v%.1f a%.1f,%.1f 0 0 1 -%.1f,%.1f h-%.1f z" % (
            x0, y, bw - r, r, r, r, r, 20 - 2 * r, r, r, r, r, bw - r)
        parts.append('<text x="%d" y="%d" text-anchor="end">%s</text>' % (x0 - 8, y + 14, "gocpp" if key == "gocpp" else "ocpp-go"))
        parts.append(
            '<rect class="hit" x="0" y="%d" width="%d" height="28" tabindex="0" data-head="%s" data-series="%s" data-name="%s" data-value="%s"/>'
            % (y - 4, w, esc(title + " (" + unit + ")"), key, esc(name), esc(fmt.format(v))))
        parts.append('<path class="mark s-%s" d="%s"/>' % (key, d))
        parts.append('<text class="val" x="%.1f" y="%d">%s</text>' % (x0 + bw + 7, y + 14, esc(fmt.format(v).split(" ")[0])))
    parts.append('<line class="base" x1="%d" y1="2" x2="%d" y2="%d"/>' % (x0, x0, h - 6))
    parts.append("</svg>")
    return '<div class="panel"><h4>%s</h4><span class="unit">%s</span>%s<p class="verdict">%s</p></div>' % (
        esc(title), esc(unit), "".join(parts), esc(note))


def bars_panel(title, unit, rows, note="", label_w=62, w=300):
    """rows: (label, series key, series name, value, format). One scale per panel, bars from zero."""
    rows = [r for r in rows if r[3] is not None]  # not measured: no bar
    h = 6 + len(rows) * 30
    x0, maxw = label_w, w - label_w - 88
    top = max(r[3] for r in rows)
    aria = "%s: %s" % (title, ", ".join("%s %s" % (r[0], r[4].format(r[3])) for r in rows))
    parts = ['<svg class="ch" viewBox="0 0 %d %d" role="img" aria-label="%s">' % (w, h, esc(aria))]
    for j, (label, key, name, v, fmt) in enumerate(rows):
        y = 6 + j * 30
        bw = max(3.0, maxw * v / top)
        r = min(4.0, bw / 2)
        d = "M%.1f,%d h%.1f a%.1f,%.1f 0 0 1 %.1f,%.1f v%.1f a%.1f,%.1f 0 0 1 -%.1f,%.1f h-%.1f z" % (
            x0, y, bw - r, r, r, r, r, 20 - 2 * r, r, r, r, r, bw - r)
        parts.append('<text x="%d" y="%d" text-anchor="end">%s</text>' % (x0 - 8, y + 14, esc(label)))
        parts.append(
            '<rect class="hit" x="0" y="%d" width="%d" height="28" tabindex="0" data-head="%s" data-series="%s" data-name="%s" data-value="%s"/>'
            % (y - 4, w, esc(title + " (" + unit + ")"), key, esc(name), esc(fmt.format(v))))
        parts.append('<path class="mark s-%s" d="%s"/>' % (key, d))
        parts.append('<text class="val" x="%.1f" y="%d">%s</text>' % (x0 + bw + 7, y + 14, esc(fmt.format(v))))
    parts.append('<line class="base" x1="%d" y1="2" x2="%d" y2="%d"/>' % (x0, x0, h - 6))
    parts.append("</svg>")
    return '<div class="panel"><h4>%s</h4><span class="unit">%s</span>%s%s</div>' % (
        esc(title), esc(unit), "".join(parts), '<p class="verdict">%s</p>' % esc(note) if note else "")


def viz(vid, title, sub, series, panels_html, table_html, note, cols=None):
    """A chart card: title, legend (two or more series), panels, a table view and a note."""
    tbl = table_html.replace('<div class="tablewrap "', '<div class="tablewrap" id="%s-table" hidden' % vid, 1)
    legend = ""
    if len(series) > 1:
        legend = '<ul class="vlegend" aria-label="Series">' + "".join(
            '<li><span class="key %s"></span>%s</li>' % (k, esc(n)) for k, n in series) + "</ul>"
    return (
        '<div class="viz" id="%s"><div class="vhead"><div><h3>%s</h3><p class="vsub">%s</p></div>'
        '<button class="btn" type="button" data-table="%s-table" aria-expanded="false">Show as table</button></div>%s'
        '<div class="panels%s">%s</div>%s<p class="vnote">%s</p></div>'
        % (vid, title, sub, vid, legend, " " + cols if cols else "", panels_html, tbl, note))


def chart():
    panels = "".join(bar_panel(i, *p) for i, p in enumerate(PANELS))
    rows = [[esc(t) + ' <span class="unit">(' + esc(u) + ")</span>", esc(f.format(a)), esc(f.format(b))] for t, u, a, b, f, _ in PANELS]
    tbl = table(["Measure", "gocpp", "ocpp-go v0.19.0"], rows, num=(1, 2)).replace(
        '<div class="tablewrap "', '<div class="tablewrap" id="cmp-table" hidden', 1)
    legend = "".join('<li><span class="key %s"></span>%s</li>' % (k, esc(n)) for k, n in SERIES)
    return (
        '<div class="viz" id="chart-cmp"><div class="vhead"><div><h3>The same fleet against gocpp and ocpp-go</h3>'
        '<p class="vsub">One server process pinned to 4 cores, OCPP 1.6J over loopback. Each panel has its own scale; bars start at zero.</p></div>'
        '<button class="btn" type="button" data-table="cmp-table" aria-expanded="false">Show as table</button></div>'
        '<ul class="vlegend" aria-label="Series">' + legend + "</ul>"
        '<div class="panels">' + panels + "</div>" + tbl +
        "<p class=\"vnote\">Single runs on one WSL2 machine (2026-11-22 and 2026-11-24), gocpp v0.3.x against upstream lorenzodonini/ocpp-go v0.19.0 with its defaults, same harness and fleet. The soak panels are the 25-minute run with 50 CSMS calls per second (2026-11-24); "
        "the other panels are the 4,000-charger runs without CSMS calls (2026-11-22), where ocpp-go did not freeze. Latency percentiles are histogram bucket upper bounds with 12% resolution, "
        "so differences of a bucket or two are noise. gocpp ran with lenient schema validation on. "
        "The upstream server stopped answering after each of the six starts in the soak (52 to 95 seconds in) and stayed stopped until the next restart; the cause is a deadlock between its dispatcher goroutine and its senders, written up in "
        '<a href="' + DOCS + 'ocpp-go-structural-issues.md">docs/ocpp-go-structural-issues.md</a>. Only 1.6 was measured.</p></div>'
    )


# --------------------------------------------------------------------------
def fig(svg, caption):
    return "<figure>%s<figcaption>%s</figcaption></figure>" % (svg, caption)


def section(sid, eyebrow, title, sub, body):
    return (
        '<section id="%s" aria-labelledby="%s-h"><div class="wrap"><p class="eyebrow">%s</p><h2 id="%s-h">%s</h2><p class="sub">%s</p>%s</div></section>'
        % (sid, sid, eyebrow, sid, title, sub, body)
    )


NAV = [
    ("why", "Why"), ("start", "Quick start"), ("typed", "Typed"), ("spec", "Spec"), ("safety", "Safety"),
    ("footprint", "Footprint"), ("testing", "Testing"), ("scale", "Scale-out"), ("observe", "Observability"),
    ("matrix", "Features"), ("dropin", "Drop-in"), ("compare", "vs ocpp-go"), ("docs", "Docs"), ("architecture.html", "Architecture"),
]

LOGO = (
    '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="2" y="2" width="20" height="20" rx="6" fill="var(--accent)"/>'
    '<path d="M13.2 4.8 7.4 13h3.9l-.9 6.2 6.2-8.6h-4z" fill="#fff"/></svg>'
)
FAVICON = (
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect x="2" y="2" width="20" height="20" rx="6" fill="#2a78d6"/>'
    '<path d="M13.2 4.8 7.4 13h3.9l-.9 6.2 6.2-8.6h-4z" fill="#fff"/></svg>\n'
)


PAGE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%(title)s</title>
<meta name="description" content="%(desc)s">
<meta property="og:title" content="gocpp: a typed OCPP library for Go">
<meta property="og:description" content="OCPP 1.6, 2.0.1 and 2.1 for CSMS backends and charge points. Generated from the official schemas, spec-correct by default, isolated per connection.">
<meta property="og:type" content="website">
<meta name="color-scheme" content="light dark">
<link rel="icon" href="assets/favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="assets/style.css">
<script>try{var t=localStorage.getItem('gocpp-theme');if(t==='dark'||t==='light')document.documentElement.setAttribute('data-theme',t)}catch(e){}</script>
</head>
<body>
<!-- Generated by scripts/site/build.py. Edit the generator, not this file. -->
<a class="skip" href="#main">Skip to content</a>
<div class="top"><div class="wrap">
<a class="brand" href="%(home)s" aria-label="%(homelabel)s">%(logo)s<span>gocpp</span></a>
<nav aria-label="Sections"><ul>%(nav)s</ul></nav>
<div class="tools"><button class="btn" id="themebtn" type="button">Theme</button><a class="btn gh" href="%(repo)s">GitHub</a></div>
</div></div>
<main id="main"><span id="top"></span>
%(body)s
</main>
<footer><div class="wrap">
<p><b>gocpp</b> is an open-source project under the MIT license. OCPP is a specification of the Open Charge Alliance; gocpp is an independent implementation and is not affiliated with or certified by it.</p>
<p>This page is static and self-contained: no trackers, no external fonts or scripts. <a href="%(repo)s">Source on GitHub</a>.</p>
</div></footer>
<div id="tip" role="status" aria-live="polite"></div>
<script src="assets/app.js"></script>
</body>
</html>
"""


def nav_html(items):
    return "".join('<li><a href="%s">%s</a></li>' % (k if "." in k else "#" + k, esc(v)) for k, v in items)


def render_page(title, desc, items, body, home="#top", homelabel="gocpp, back to top"):
    return PAGE % {"logo": LOGO, "nav": nav_html(items), "repo": REPO, "body": body, "title": esc(title),
                   "desc": esc(desc), "home": home, "homelabel": homelabel}


def build():
    d = diagrams
    parts = []

    # ---- hero ------------------------------------------------------------
    parts.append("""
<header class="hero"><div class="wrap">
<p class="kicker">Open Charge Point Protocol for Go</p>
<h1>A typed OCPP 1.6, 2.0.1 and 2.1 library for charging backends and charge points.</h1>
<p class="lead">gocpp is one Go module for both ends of the WebSocket: the CSMS and the charge point. Messages are generated from the official OCA JSON schemas, the wire behaviour follows the specification by default, and one slow or broken charger cannot hold up the others.</p>
<div class="cta">
<span class="install"><code>go get github.com/shiv3/gocpp</code><button class="copy" type="button" data-copy="go get github.com/shiv3/gocpp">Copy</button></span>
<a class="btn primary" href="#start">Quick start</a>
<a class="btn" href="#dropin">ocpp-go drop-in</a>
<a class="btn" href="%(repo)s">GitHub</a>
<a class="btn" href="architecture.html">Architecture and performance</a>
<a class="btn" href="%(pkg)s">API reference</a>
</div>
<p class="teaser"><span class="pill ok">New</span> <b>Already on ocpp-go?</b> Move a server to the gocpp runtime with one <code>replace</code> line in <code>go.mod</code>; the public API is identical, so application code does not change. <a href="#dropin">Drop-in replacement for ocpp-go</a></p>
<ul class="badges" aria-label="Project facts"><li>OCPP 1.6 · 2.0.1 · 2.1</li><li>CSMS and charge point</li><li>Go 1.26+</li><li>MIT license</li><li>pre-v1: the API may still change before v1.0</li></ul>
<ul class="tiles" aria-label="Key figures">
<li class="tile"><div class="lab">Messages generated from the official schemas</div><div class="val">194</div><div class="cmp">39 in 1.6, 64 in 2.0.1, 91 in 2.1; none written by hand</div></li>
<li class="tile"><div class="lab">Resident goroutines per connection</div><div class="val">1</div><div class="cmp">the reader; writes, timers and teardown need none</div></li>
<li class="tile"><div class="lab">Memory per idle connection</div><div class="val">13.6<small>KB RSS</small></div><div class="cmp">7.8 KB heap + 4.1 KB stack live</div></li>
<li class="tile"><div class="lab">10,000 idle connections</div><div class="val">151<small>MB RSS</small></div><div class="cmp">one process, measured with raw OCPP WebSocket clients</div></li>
</ul>
<p class="fine">Measurements are single runs on one machine and are described under <a href="#footprint">Footprint and latency</a>. Sources: <code>docs/performance.md</code> and the load-test runs of 2026-11-21, 2026-11-22 and 2026-11-24.</p>
</div></header>
""" % {"repo": REPO, "pkg": PKG})

    # ---- why -------------------------------------------------------------
    cards = [
        ("typed", "Typed from the official schemas", "Requests, responses and handlers are generated from the OCA JSON schemas for all three versions, so the types cannot drift from the specification."),
        ("spec", "Spec-correct by default", "One outstanding CALL per connection, the error codes and message types of each version. Compatibility with chargers tuned against ocpp-go is an explicit preset, not the default."),
        ("safety", "One charger cannot hurt the others", "Each connection owns its state. A panic, a slow handler or a charger that never answers stays inside its own connection."),
        ("footprint", "Small, measured footprint", "One goroutine and about 13 KB per idle connection, with bounds that fail the build when they regress."),
        ("testing", "Behaviour is tested, not assumed", "Differential tests against ocpp-go with a charge-point simulator, conformance suites, fuzzing, chaos tests with real processes."),
        ("scale", "A path from one process to a fleet", "The same handlers run embedded in one server or behind a connection tier, with staged releases. The split mode is marked experimental."),
        ("observe", "You can see what is on the wire", "A frame observer, Prometheus and OpenTelemetry metrics, tracing, and lossy taps that never slow a connection."),
    ]
    body = '<div class="grid3">' + "".join(
        '<a class="card" href="#%s"><h4><span class="n">%02d</span>%s</h4><p>%s</p><span class="go">Read how &rarr;</span></a>' % (sid, i + 1, t, p)
        for i, (sid, t, p) in enumerate(cards)) + "</div>"
    body += """
<p>An OCPP backend is a long-lived connection server for hardware you do not control. Chargers stay in the field for a decade, their firmware interprets the specification loosely, and they reconnect all at once after every deploy. These seven commitments are the consequences we drew from that; each section below states the commitment, why it matters, and the mechanism in the code that keeps it.</p>
"""
    parts.append(section("why", "Design commitments", "What gocpp cares about", "Seven commitments shape the library. They are the reason it is built the way it is.", body))

    # ---- quick start -----------------------------------------------------
    tabs = [("csms16", "CSMS (1.6)", "csms16.go.txt"), ("cp16", "Charge point (1.6)", "cp16.go.txt"), ("csms201", "CSMS (2.0.1, validated)", "csms201.go.txt")]
    tb = "".join('<button type="button" role="tab" id="tab-%s" aria-controls="panel-%s" aria-selected="%s"%s>%s</button>' % (
        k, k, "true" if i == 0 else "false", "" if i == 0 else ' tabindex="-1"', esc(n)) for i, (k, n, _) in enumerate(tabs))
    panels = "".join('<div class="tabpanel" role="tabpanel" id="panel-%s" aria-labelledby="tab-%s" tabindex="0"%s><pre><code>%s</code></pre></div>' % (
        k, k, "" if i == 0 else " hidden", go_code(f)) for i, (k, _, f) in enumerate(tabs))
    body = """
<ol class="steps">
<li><h4>Install</h4><p><code>go get github.com/shiv3/gocpp</code> (Go 1.26 or later). Redis, NATS, gRPC and Temporal integrations are separate modules under <code>addons/</code>, so the core dependency tree stays small.</p></li>
<li><h4>Write typed handlers</h4><p>Embed the generated <code>Unimplemented</code> handler and override the messages you care about. Everything else answers <code>NotSupported</code>.</p></li>
<li><h4>Run both ends</h4><p>Start the CSMS, then the charge point. The charge point connects to <code>ws://localhost:8080/ocpp/CP_1</code> and sends a BootNotification.</p></li>
</ol>
<div class="codebox"><div class="bar"><div class="tabs" role="tablist" aria-label="Quick start programs">%s</div><button class="copy" type="button">Copy</button></div>%s</div>
<p class="src">Each tab is a complete <code>main.go</code>, compiled against the current API; the 1.6 pair was also run end to end. More: <a href="%sexamples/csms-minimal">examples/csms-minimal</a>, <a href="%sexamples/cp-minimal">examples/cp-minimal</a>, <a href="%sexamples/csms-full">examples/csms-full</a>, and the <a href="%susage.md">usage guide</a>.</p>
<div class="note info"><p><b>Defaults worth knowing.</b> Schema validation is off unless you choose a mode (<code>WithLenientSchema</code> is the one recommended for production). Outbound calls are serialized per connection. Transport keepalive is on: a ping every 54 s and a 60 s read-idle timeout.</p></div>
""" % (tb, panels, REPO + "/tree/main/", REPO + "/tree/main/", REPO + "/tree/main/", DOCS)
    parts.append(section("start", "Quick start", "A CSMS and a charge point in a few lines", "Install the module, write typed handlers, and connect a charge point.", body))

    # ---- typed -----------------------------------------------------------
    body = """
<p class="why"><b>Why.</b> OCPP 2.1 alone has 91 messages. Hand-written structs drift from the specification one field at a time, and the difference shows up in production as a charger that is rejected or, worse, silently misread.</p>
""" + fig(d.codegen(), "<b>Code generation.</b> The official JSON schemas and a small profile file (message names and directions) are the only inputs. CI regenerates everything and fails when the result differs from what is committed.") + """
<div class="grid2">
<div class="card"><h4>Generics carry the types</h4><p>A generated message value such as <code>v16profiles.BootNotification</code> is an <code>ocppj.Message[Req, Resp]</code>. <code>csms.On</code>, <code>csms.Call</code>, <code>cp.On</code> and <code>cp.Call</code> infer both types from it: handlers take a typed request and return a typed response, with no type assertions.</p></div>
<div class="card"><h4>Direction is checked</h4><p>Every message knows who may send it. Registering a handler or issuing a call in the wrong direction returns <code>ocppj.ErrInvalidDirection</code>; <code>DataTransfer</code> is allowed both ways, as in the specification.</p></div>
<div class="card"><h4>Numbers stay numbers</h4><p>Every JSON-Schema <code>integer</code> is an <code>int64</code>. Every <code>number</code> is a <code>types.Decimal</code> that is always written as an unquoted JSON number, without relying on process-global settings.</p></div>
<div class="card"><h4>Vendor data survives a round trip</h4><p>The 2.x <code>CustomDataType</code> keeps properties the schema does not define in <code>Extra</code> and writes them back unchanged. Untyped 2.x properties such as <code>DataTransfer.data</code> are <code>json.RawMessage</code>.</p></div>
</div>
<p>Three API levels are generated for each version, all typed: handler interfaces with one-call registration (<code>v16/handlers</code>), client wrappers with one method per message and an <code>Async</code> variant (<code>v16/client</code>), and free functions (<code>v16/calls</code>). The official schemas are embedded and also used at run time for validation, so the types and the validator can never disagree about what a message looks like.</p>
<p class="src">Mechanism: <code>internal/codegen</code>, <code>schemas/</code>, CI job <code>codegen-drift</code> (<code>make codegen &amp;&amp; git diff --exit-code</code>), <code>core/types</code>. See <a href="%sarchitecture.md">docs/architecture.md</a>.</p>
""" % DOCS
    parts.append(section("typed", "Commitment 1", "Typed messages, generated from the official schemas", "The OCA JSON schemas are the source of truth for 1.6, 2.0.1 and 2.1. Nothing about a message is written by hand.", body))

    # ---- spec ------------------------------------------------------------
    spec_rows = [
        ["Outbound CALLs", "One outstanding CALL per connection; later calls wait in a bounded FIFO (64 by default). OCPP-J forbids sending a new CALL while one is unanswered, and many chargers drop or mishandle concurrent requests.", "<code>WithConcurrentCalls()</code> (non-compliant)"],
        ["Error codes", "The specific code for each failure: <code>NotImplemented</code> for an unknown action, <code>TypeConstraintViolation</code>, <code>PropertyConstraintViolation</code>, and the 1.6 spelling <code>OccurenceConstraintViolation</code> on 1.6 connections.", "<code>WithErrorCodeMapper</code>"],
        ["Message types", "CALLRESULTERROR and SEND exist only on 2.1 connections. An unknown type is answered with <code>MessageTypeNotSupported</code> on 2.x and is not answered on 1.6, whose error table has no such code.", "<code>WithErrorCodeMapper</code>"],
        ["Message ids", "At most 36 characters, as OCPP-J says: rejected in strict mode, accepted and logged otherwise.", "<code>WithMessageIDPolicy</code>"],
        ["Timestamps", "RFC 3339 on the way out, always. Offset and fractional seconds are kept as given.", "<code>WithTimeEncoding</code> (UTC, fixed precision)"],
        ["Heartbeat", "Sent by the charge point, never by the CSMS: there is no server-side heartbeat option.", "<code>cp.WithHeartbeatInterval</code>"],
    ]
    lenient_rows = [
        ["Enum case", '<code>"status": "available"</code>', '<code>"Available"</code>'],
        ["Timestamp without a zone", '<code>"2026-01-01 10:00:00"</code>', '<code>"2026-01-01T10:00:00Z"</code> (UTC, or your configured location)'],
        ["Offset without a colon", '<code>"2026-01-01T10:00:00+0900"</code>', '<code>"2026-01-01T10:00:00+09:00"</code>'],
        ["Quoted number", '<code>"connectorId": "1"</code>', "<code>1</code>"],
        ["Number where a string is expected", '<code>"idTag": 12345</code>', '<code>"12345"</code>'],
        ["Null payload", '<code>[2,"id","Heartbeat",null]</code>', "read as <code>{}</code>"],
        ["Not repairable without loss", '<code>"1.5"</code> for an integer, <code>"abc"</code>, a missing required field', "left as sent and rejected"],
    ]
    body = """
<p class="why"><b>Why.</b> A default is what most deployments run. If the default bends the protocol, every fleet inherits the bend and chargers get tuned against it. gocpp makes the specification the default and makes every deviation a named option you can see in your configuration.</p>
""" + table(["Behaviour", "Default (what the specification says)", "Explicit opt-out"], spec_rows) + """
<h3>Lenient mode: strict about structure, forgiving about quirks</h3>
<p>Real firmware gets field formats wrong in a small number of well-known ways. Rejecting those messages loses meter values and transactions; accepting anything hands garbage to your handlers. <code>WithLenientSchema()</code> validates against the official schema, rejects structurally broken messages, and repairs the benign cases before the typed decode, only when the conversion is lossless. Each repair is logged and counted.</p>
""" + table(["Quirk", "The charger sends", "Your handler sees"], lenient_rows) + """
<p>The other modes never rewrite a payload: strict rejects, tolerant logs and passes, and off (the library default) skips validation.</p>
<h3>Migrating from ocpp-go: a preset, then one switch at a time</h3>
<p>This is the path to gocpp's own API. To keep ocpp-go's API and change only the runtime under it, see the <a href="#dropin">drop-in replacement</a>.</p>
<p>Chargers in the field may depend on ocpp-go's exact replies. <code>csms.WithOCPPGoCompat()</code> reproduces the charger-visible behaviour of ocpp-go (its error codes, silent drop of bad message ids, UTC whole-second timestamps, no server pings) so a migration can be two steps: swap the library with the preset on, then turn the individual behaviours back to the specification one at a time. The preset is shorthand for six ordinary options; later options win.</p>
""" + fig(d.migration(), "<b>Staged migration.</b> A differential harness replays 67 raw-frame, fault and connection cases against both libraries. gocpp by default differs from ocpp-go in 24 of them; with the preset 13 remain, all deliberate (for example: lenient mode accepts what ocpp-go rejects, and gocpp recovers a handler panic).") + """
<p class="src">Mechanism: <code>core/dispatcher/callslot.go</code>, <code>core/dispatcher/compat.go</code>, <code>core/schema/lenient.go</code>, <code>core/schema/coerce.go</code>. The full table of differences, row by row with the clause each default follows: <a href="%smigration-from-ocpp-go.md">docs/migration-from-ocpp-go.md</a>. API mapping: <a href="%smigration-from-lorenzodonini.md">docs/migration-from-lorenzodonini.md</a>.</p>
""" % (DOCS, DOCS)
    parts.append(section("spec", "Commitment 2", "Spec-correct by default, compatible on request", "The specification decides the default behaviour. Compatibility with existing fleets is an explicit, reversible choice.", body))

    # ---- safety ----------------------------------------------------------
    body = """
<p class="why"><b>Why.</b> In a fleet of thousands there is always a charger that is muted, flapping, or sending something no one anticipated. The server has to treat that as the normal case: the damage one connection can do must end at that connection.</p>
""" + fig(d.isolation(), "<b>Per-connection isolation.</b> In gocpp the state a call needs (its place in the queue, the write lock, the pending-call table, the timers) belongs to the connection. A design with one shared dispatcher puts every charge point's outbound requests through a single goroutine and a single lock; ocpp-go's default server dispatcher (<code>ocppj/dispatcher.go</code>) is built that way. Both are valid designs; they differ in what a stalled step can affect.") + """
<div class="grid2">
<div class="card"><h4>No global lock on the message path</h4><p>Reading, dispatching, writing and call tracking use per-connection state only. The one optional shared limit, <code>WithGlobalConcurrencyLimit</code>, is a semaphore you opt into to protect your own backends.</p></div>
<div class="card"><h4>Panics are contained</h4><p>A handler that panics is recovered and answered with an <code>InternalError</code> CALLERROR. The connection and the process keep running. Panics in observers and async callbacks are recovered too.</p></div>
<div class="card"><h4>Handlers outlive the socket, within a deadline</h4><p>A handler's context is not cancelled when the charger disconnects, so persisting a StopTransaction can finish. It carries a deadline instead (<code>WithHandlerTimeout</code>, 60 s by default) and a handler can still watch for the disconnect if it wants to.</p></div>
<div class="card"><h4>Bounded queues, backpressure instead of drops</h4><p>At most 16 handlers run per connection. When they are busy, inbound calls wait in a small queue (4), and when that is full the reader stops reading, which slows that one socket. No inbound call is dropped; a full outbound queue returns <code>ErrQueueFull</code> at once.</p></div>
<div class="card"><h4>An unresponsive charger costs one timeout</h4><p>The call timeout starts when a call takes the connection's slot, not when it was queued, and a stalled write is aborted by a write watchdog. Senders check the connection context, so a teardown never deadlocks a writer.</p></div>
<div class="card"><h4>Closes say why</h4><p>1000 for a normal or server-initiated close, 1008 when a newer connection replaced this one, 1009 for an oversized message, 1012 for a drain. Over capacity or while shutting down, upgrades get HTTP 503 with <code>Retry-After</code> before the handshake.</p></div>
</div>
""" + fig(d.inbound(), "<b>What happens when handlers are slow.</b> Pressure moves backwards one bounded step at a time and ends at the TCP window of the one charger that caused it.") + """
<h3>Deploys without a reconnect storm</h3>
<p>Restarting a process that holds thousands of sockets makes every charger reconnect at the same moment. <code>Server.Drain</code> refuses new upgrades, then closes sessions at a fixed pace, idle ones first, with close code 1012; sessions with a call in flight get a grace period. <code>WithAcceptRateLimit</code> bounds the handshake rate on the way back in. In a rolling restart of 4,000 simulated chargers, a 60-second drain kept about 96%% of the fleet connected throughout and held the reconnect peak at 90 attempts per second, against about 2,000 per second for a plain restart.</p>
<p class="src">Mechanism: <code>core/dispatcher/conn.go</code>, <code>inbound.go</code>, <code>pipeline.go</code>, <code>callslot.go</code>, <code>pending.go</code>; goroutine-leak checks with <code>goleak</code> and the race detector on every CI run. Details: <a href="%sarchitecture.md">architecture</a>, <a href="%sperformance.md">concurrency bounds</a>, <a href="%sdraining.md">draining</a> (measurement: 4,000 chargers, no TLS, one machine).</p>
""" % (DOCS, DOCS, DOCS)
    parts.append(section("safety", "Commitment 3", "One charger cannot hurt the others", "Isolation per connection, bounded queues, and failure handling that keeps the process and the rest of the fleet running.", body))

    # ---- footprint -------------------------------------------------------
    gogc_rows = [
        ["100 (Go default)", "30k", "1.7 ms", "151 MB", "1.16"],
        ["300", "30k", "1.0 ms", "264 MB", "1.10"],
        ["100 (Go default)", "60k", "7.4 ms", "151 MB", "2.11"],
        ["300", "60k", "4.2 ms", "270 MB", "2.01"],
    ]
    body = """
<p class="why"><b>Why.</b> A CSMS is mostly idle connections. What a connection costs while nothing happens decides how many chargers fit in a process, and garbage-collector work decides the latency tail when traffic arrives. Both are designed for and both are measured.</p>
<div class="grid3">
<div class="card"><h4>One resident goroutine</h4><p>The reader. Writes happen on the goroutine that produced the frame, under a per-connection FIFO lock. Teardown is a <code>context.AfterFunc</code>; the read watchdog and WebSocket ping are timers.</p></div>
<div class="card"><h4>Small buffers, shared configuration</h4><p>1 KB read and write buffers per connection instead of the 4 KB default, a configuration shared between connections, and timers armed only when needed.</p></div>
<div class="card"><h4>Allocation work for the tail</h4><p>Frames are parsed by a scanner that references the read buffer, lenient validation uses a reflection-free decoder, and an optional handler pool keeps warm stacks for high message rates.</p></div>
</div>
""" + chart() + """
<h3>What the numbers do and do not say</h3>
<ul>
<li>For the same 4,000-charger fleet without CSMS calls, gocpp used 26% less memory than upstream ocpp-go v0.19.0 at 30k messages per second (124 against 167 MB); Heartbeat p99 and server CPU were the same to within a histogram bucket.</li>
<li>With lenient validation on, gocpp allocated about 22% more per request than ocpp-go (3.57 against 2.92 KB). With validation off gocpp allocated 1.4 KB, 53% less.</li>
<li>In the 25-minute soak (40% mute chargers, 50 CSMS calls per second, five restarts), gocpp did not freeze, crash or leak. Upstream ocpp-go v0.19.0 froze after each of its six starts and served no messages for 69% of the run; it also froze with 5 calls per second, and in a capacity ramp with no calls at all at about 31,700 chargers. Not every setup triggers it: it did not freeze in the 4,000-charger runs without calls.</li>
<li>These are single runs on one shared machine over loopback, without TLS. Treat them as orders of magnitude and measure your own workload; the harness and reports are in the repository.</li>
</ul>
<h3>Sizing: what one small server holds</h3>
<p>A capacity run limited one embedded gocpp server with cgroups and ramped a quiet fleet against it (Heartbeat every 300 seconds, 20% charging with MeterValues every 60 seconds, 40% mute). On <b>1 core and 2 GB</b> it held about 93,700 chargers with a Heartbeat p99 at or under 200 ms (about 100,000 with schema validation off), at about 16 KB per connection; upstream ocpp-go v0.19.0 froze at about 26,000 to 31,000 chargers (about 37 KB per connection), before reaching a memory or CPU limit. On <b>2 cores and 4 GB</b> it held at least 112,000, where the test machine, not the server, ran out of memory. As a sizing rule with headroom: about 60,000 chargers on 1 core / 2 GB and about 100,000 on 2 cores / 4 GB, before TLS. Loopback, single runs. Charts, the reconnect-storm results and the caveats are under <a href="architecture.html#perf-capacity">Sizing and capacity</a>.</p>
<h3>The footprint is guarded in CI</h3>
<p><code>TestIdleFootprintBounds</code> opens 2,000 connections through the real server and fails if heap, stack or goroutines per connection exceed fixed bounds (9 KB heap and 6 KB stack today). The bounds are ratcheted down as the footprint shrinks, so a regression of a few hundred bytes per connection fails the build. <code>TestGatewayIdleFootprintBounds</code> does the same for the connection tier of a split deployment.</p>
<h3>Tuning the garbage collector</h3>
<p>Above roughly 10k requests per second, a larger <code>GOGC</code> trades memory for a shorter tail. Always set <code>GOMEMLIMIT</code>.</p>
""" + table(["GOGC", "Messages/s", "Heartbeat p99", "Max RSS", "Server CPU (cores)"], gogc_rows, caption="4,000 chargers, server pinned to 4 cores, lenient schema, one run per row.", num=(1, 2, 3, 4)) + """
<p>The connection model, the message paths and the full set of measurements (soak, drain, gateway hop) are on the <a href="architecture.html">architecture and performance page</a>.</p>
<p class="src">Sources: <a href="%sperformance.md">docs/performance.md</a> (cost model, runtime settings, handler pool sizing, every tuning knob), <code>benchmarks/footprint_test.go</code>, and the load-test runs of 2026-11-21 and 2026-11-22 (harness <code>interop/loadtest</code>).</p>
""" % DOCS
    parts.append(section("footprint", "Commitment 4", "Footprint and latency", "One goroutine and about 13 KB per idle connection, and a latency tail that stays short at tens of thousands of messages per second.", body))

    # ---- testing ---------------------------------------------------------
    test_rows = [
        ["Differential tests against ocpp-go", "Two minimal CSMS servers with identical canned behaviour, one on each library, receive the same traffic from a charge-point simulator; frames, close codes and server events are diffed.", "157 cases in the full run against upstream v0.19.0; all 68 simulator scenarios are identical and 23 raw-frame, fault and connection cases differ, each explained"],
        ["Conformance suites", "Per-message validation tables and direction enforcement for every generated message, ported from ocpp-go's per-message tests.", "194 messages across 1.6, 2.0.1 and 2.1; the suites run embedded, split over the in-process link, and split over NATS"],
        ["Fuzzing", "Differential fuzz targets: the fast paths (frame scanner, frame encoder, reflection-free decoder, lenient repair) must agree with the reference implementations on arbitrary bytes.", "4 fuzz targets in <code>core/ocppj</code> and <code>core/schema</code>"],
        ["Chaos tests with real processes", "Gateway, backends and registry run as real processes that are killed, partitioned and restarted while chargers stay connected.", "24 scenarios, run over the gRPC link and again over NATS; a mixed-version run against the previous release is wired up"],
        ["Race and leak detection", "The whole suite runs under the race detector; goroutine-leak checks (<code>goleak</code>) and deterministic-time tests (<code>testing/synctest</code>) cover timers and teardown.", "every pull request"],
        ["Performance gates", "Tests that fail the build: per-connection footprint bounds, the added latency of the gateway hop (at most 0.5 ms mean and 2 ms p99 in process), gRPC hop latency, edge and tenant-limit overhead.", "every pull request; benchmarks against a baseline weekly and on release tags, plus a soak"],
        ["Generated-code drift", "CI regenerates all message code from the schemas and fails on any difference.", "every pull request"],
    ]
    body = """
<p class="why"><b>Why.</b> For a protocol library, "it works with our simulator" is not evidence. The questions that matter are what chargers see on the wire, what happens when a process dies mid-message, and whether last month's performance is still there.</p>
""" + table(["Layer", "What it checks", "Scale"], test_rows) + fig(d.parity(), "<b>Differential testing.</b> The same simulator traffic goes to both servers; anything a charger could observe is compared. Differences are either fixed, or documented as deliberate and covered by the compatibility preset.") + """
<p class="src">Where: the differential harness <code>interop/parity</code>, <code>internal/conformance</code>, <code>internal/e2e</code>, <code>cmd/gocpp-gateway/chaos</code>, <code>.github/workflows/ci.yml</code>, <code>.github/workflows/bench.yml</code>. The differential reports cover OCPP 1.6, the version ocpp-go and the simulator scenarios share.</p>
"""
    parts.append(section("testing", "Commitment 5", "Behaviour is tested, not assumed", "Claims about wire behaviour, failure handling and performance are backed by tests that run in CI.", body))

    # ---- scale -----------------------------------------------------------
    where_rows = [
        ["TLS, WebSocket, subprotocol, origin check", '<span class="yes">yes</span>', ""],
        ["OCPP-J framing, message ids, size limit, malformed-frame errors", '<span class="yes">yes</span>', ""],
        ["Call correlation, one outstanding call, timeouts, keepalive", '<span class="yes">yes</span>', "supplies the deadline"],
        ["Credential decision", "asks the backend", '<span class="yes">decides</span>'],
        ["Schema validation, lenient repair, time encoding, signed messages", "", '<span class="yes">yes</span>'],
        ["Your handlers and error mapping", "", '<span class="yes">yes</span>'],
    ]
    perf_rows = [
        ["Gateway throughput", "22,700 messages/s per core at 36k messages/s"],
        ["Added latency of the hop", "+0.15 ms p50, +0.5 ms p99 at 7.7k messages/s"],
        ["Gateway memory", "11 KB heap + stack per idle session (design target 16 KB)"],
        ["Backend deploys", "5 backend restarts (3 SIGTERM, 2 SIGKILL) under load: chargers up never fell below 3,987 of 4,000"],
        ["Gateway drain", "one of two gateways drained over 20 s: back to 99% within 1.8 s of the replacement being ready"],
        ["Cost", "total CPU about 1.3 to 1.4 times the embedded server at 35k to 37k messages/s"],
    ]
    body = """
<div class="note"><p><span class="pill">Experimental</span> Gateway mode and everything built on it (the <code>gateway</code> package, <code>csms.NewGatewayServer</code>, <code>WithGatewayLink</code>, <code>Server.Drain</code>, <code>storage.SessionRegistry</code>, the addons <code>link-grpc</code>, <code>link-nats</code>, <code>registry-redis</code> and <code>rollout</code>, the commands <code>gocpp-gateway</code> and <code>gocppctl</code>) is experimental until v1.0: the API may change in minor releases, and each such symbol says so in its doc comment. The embedded <code>csms.Server</code>, the generated packages and the core packages are the stable surface and are unchanged by it.</p></div>
<p class="why"><b>Why.</b> Deploying a process that owns the sockets disconnects every charger it holds. The code that changes most often is the business logic; the code that must not restart is the connection. Separating the two makes an application deploy invisible to chargers.</p>
""" + fig(d.gateway(), "<b>Gateway mode.</b> The gateway owns the WebSocket sessions and forwards each inbound CALL, payload untouched, to a backend. Backends hold no per-charger state, so deploying one disconnects nobody. Backends dial gateways; the link is gRPC by default, NATS where it already runs, or in-process for a single binary and for tests.") + """
<div class="grid2">
<div class="card"><h4>The typed API does not change</h4><p><code>csms.On</code>, <code>csms.Call</code>, the generated handlers and every schema option mean the same in both modes. Because the backend decides every payload-level error, the CALLERROR on the wire is byte-identical to embedded mode, and a test asserts it.</p></div>
<div class="card"><h4>Epoch-fenced sessions</h4><p>A session registry assigns an epoch to each connection of a charge point before the upgrade is accepted. Calls carry the epoch and a gateway rejects one that does not match the session it holds, so a delayed write or a stale route cannot reach the wrong connection.</p></div>
<div class="card"><h4>At most once after dispatch</h4><p>If a backend takes a message and then fails, the charger gets <code>InternalError</code> and the gateway never retries elsewhere. A backend that is draining answers <code>Nack</code> and the message is redispatched within the same deadline.</p></div>
<div class="card"><h4>Callers learn what was delivered</h4><p>A failed CSMS call reports whether the frame was not sent (safe to retry), sent without a response (the charger may have executed it), or answered.</p></div>
</div>
""" + table(["Concern", "Gateway (connection tier)", "Backend (processing tier)"], where_rows, caption="What runs where.") + """
<h3>Staged releases</h3>
<p>A backend canary is a routing decision at the gateway. Backends attach with a pool name, and a declarative policy decides which pool serves each charger: by id, by attribute, by OCPP version, or by a stable-hash percentage. No socket is touched, so chargers never notice a step or a rollback. Without a policy only the default pool gets work, so starting a canary changes nothing by itself.</p>
""" + fig(d.canary(), "<b>Backend canary.</b> A rollout is a sequence of policy revisions written with compare-and-set (<code>gocppctl route apply</code>). Raising the percentage only adds chargers; a rollback puts every session back on the default pool from its next message. Promotion is always external: a gateway only ever suspends a rule when its local guard trips.") + """
<ul>
<li><b>Gateway canary.</b> The connection tier has its own procedure: a canary gateway with a soft session cap, filled by a paced partial drain of the others, then rebalanced (<code>gocppctl gateway</code>).</li>
<li><b>Automatic promotion</b> is an optional addon, off by default: <code>addons/rollout</code> steps through a plan, evaluates gates (error ratio, CALLERROR mix, p99 latency) and rolls back in one revision. The documentation recommends operating canaries by hand first.</li>
<li><b>Entry criteria are yours.</b> Backends share your stores, so two versions must read what the other writes. The runbook lists what to check before starting.</li>
</ul>
<h3>Measured</h3>
""" + table(["", "Result"], perf_rows, caption="4,000 chargers over loopback, gRPC link; single runs on one machine, no TLS.") + """
<p>In the rollout run (4,000 chargers, 2 gateways, pools of 3 and 2 backends), manual steps of 1%%, 10%%, 50%% and 100%% moved 0.9%%, 9.4%%, 48.7%% and 100%% of chargers with no disconnects and no charger-visible errors, and each revision reached every gateway in 14 to 140 ms. A canary made to answer 30%% of requests with <code>InternalError</code> was rolled back by the automatic controller 20 seconds after its 1%% stage was applied.</p>
<p>Running several full CSMS instances behind a load balancer, with calls forwarded to the instance that holds the socket, is also supported (<a href="%sclustering.md">clustering</a>); its message-router API is deprecated in favour of the gateway link.</p>
<p class="src">Guides: <a href="%sgateway.md">gateway mode</a>, <a href="%sstaged-releases.md">staged releases</a>, <a href="%srollout.md">operator runbook</a>, <a href="%sdraining.md">draining</a>, <a href="%sdesign/gateway.md">design document</a>. Measurements: the gateway and rollout load-test runs of 2026-11-21. Gateway crash recovery with TLS handshakes was not measured.</p>
""" % (DOCS, DOCS, DOCS, DOCS, DOCS, DOCS)
    parts.append(section("scale", "Commitment 6", "A path from one process to a fleet", "Start with one embedded server. When deploys must not disconnect chargers, split the connection tier from the processing tier without rewriting handlers.", body))

    # ---- observability ---------------------------------------------------
    body = """
<p class="why"><b>Why.</b> When a charger misbehaves, the first question is always "what exactly was on the wire?" The answer should not require a packet capture or a rebuild.</p>
<div class="grid2">
<div class="card"><h4>Frame observer</h4><p><code>WithFrameObserver</code> receives every inbound frame, including unparsable ones, and every outbound frame after the write attempt: charge point id, version, direction, raw bytes, type, message id, action, time and write error. <code>observability.SlogFrameObserver</code> is a ready-made logger.</p></div>
<div class="card"><h4>Metrics</h4><p>Prometheus and OpenTelemetry adapters behind one <code>Metrics</code> interface: connections, calls and call duration, pending calls, schema failures and lenient repairs, ping latency, close codes with causes, accepts by result, session duration.</p></div>
<div class="card"><h4>Tracing and message ids</h4><p><code>WithTracerProvider</code> emits OpenTelemetry spans. Handlers read the frame's message id from the context, and outbound calls can report the id they were sent with, so application logs join up with the wire.</p></div>
<div class="card"><h4>Frame and session taps</h4><p><code>core/observability/tap</code> exports frames and session open/close events to a sink (JSON Lines, NATS, your own) for audit, analytics and replay. It samples, filters and drops under pressure: nothing on the connection ever waits for the sink, and losses are visible as sequence gaps.</p></div>
</div>
<p>Per connection, <code>Conn.Stats()</code> reports when it connected, when it last received and sent, and how many calls are pending or queued. A gateway adds routing, pool and tenant metrics for comparing a canary with stable.</p>
<p class="src">Mechanism: <code>core/observability</code> (<code>frame.go</code>, <code>metrics/prom</code>, <code>metrics/otel</code>, <code>tracer.go</code>, <code>tap</code>). Taps are part of the experimental gateway work. See <a href="%sgateway.md">docs/gateway.md</a>, "Frame and session taps", and <a href="%sperformance.md">docs/performance.md</a>, "What to export".</p>
""" % (DOCS, DOCS)
    parts.append(section("observe", "Commitment 7", "You can see what is on the wire", "Raw frames, metrics, traces and session events are available without touching the hot path.", body))

    # ---- matrix ----------------------------------------------------------
    Y = '<span class="yes">yes</span>'
    N = '<span class="no">not in this version</span>'
    mrows = [
        ["Generated messages", "39", "64", "91"],
        ["CSMS and charge-point roles", Y, Y, Y],
        ["Typed handlers, client wrappers, call functions", Y, Y, Y],
        ["Embedded official JSON schemas; strict, tolerant and lenient validation", Y, Y, Y],
        ["Typed enum constants", Y, "string fields, checked by the schema", "string fields, checked by the schema"],
        ["<code>CustomData</code> with unknown properties preserved", "not in 1.6", Y, Y],
        ["CALL, CALLRESULT, CALLERROR", Y, Y, Y],
        ["CALLRESULTERROR and SEND message types", N, N, Y],
        ["Signed messages (<code>WithSigner</code>, <code>WithVerifier</code>)", N, N, Y],
        ["Error-code spelling per version", "<code>OccurenceConstraintViolation</code>, <code>FormationViolation</code>", "<code>Occurrence…</code>, <code>FormatViolation</code>", "<code>Occurrence…</code>, <code>FormatViolation</code>"],
        ["ocpp-go compatibility preset", Y + " (verified by differential tests)", "options apply; not differential-tested", "options apply; ocpp-go has no 2.1"],
        ["ocpp-go <a href=\"#dropin\">drop-in module</a> (ocpp-go's API on the gocpp runtime)", Y + " (ocpp-go's 1.6 suite passes)", Y + " (ocpp-go's 2.0.1 suite passes)", "ocpp-go has no 2.1"],
        ["Conformance suite", Y, Y, Y],
        ["Gateway mode <span class=\"pill\">Experimental</span>", Y, Y, Y],
    ]
    common = """
<h3>In every version</h3>
<div class="grid3">
<div class="card"><h4>Connection</h4><ul><li>Basic auth and mutual TLS authenticators (Security Profiles 1 to 3)</li><li>Custom charge-point id extraction from the path</li><li>Duplicate-connection policy: close the old one or reject the new one</li><li>Per-charge-point timeouts</li></ul></div>
<div class="card"><h4>Charge-point client</h4><ul><li>Reconnect with exponential backoff; handlers persist</li><li>Offline queue with optional retry of in-flight calls</li><li>Automatic Heartbeat</li><li>Basic auth, custom headers, TLS configuration</li></ul></div>
<div class="card"><h4>Tools and addons</h4><ul><li><code>gocpp-validate</code>: check a message against the official schema</li><li><code>gocpp-sim</code>: drive a simulated charge point from a YAML scenario</li><li><code>addons/statefsm</code>: 1.6 connector state machine</li><li><code>addons/tenant</code>: multi-tenant partitioning of stores</li></ul></div>
</div>
<p>gocpp is a protocol library: it gives you typed messages, connections and their guarantees. Business logic such as authorization rules, transaction storage and smart-charging decisions lives in your handlers; the storage interfaces it ships are extension points with in-memory defaults.</p>
"""
    body = table(["", "OCPP 1.6", "OCPP 2.0.1", "OCPP 2.1"], mrows) + common
    parts.append(section("matrix", "Feature matrix", "What is supported, by OCPP version", "One module and one connection engine for all three versions; the engine is version-agnostic and the per-version code is generated.", body))


    # ---- drop-in ---------------------------------------------------------
    def codeline(label, text):
        return ('<div class="codebox oneline"><div class="bar"><span class="label">%s</span><button class="copy" type="button" aria-label="Copy the replace line for %s">Copy</button></div>'
                '<pre><code>%s</code></pre></div>' % (label, esc(label), esc(text)))

    body = """
<p class="why"><b>Why.</b> Rewriting a working backend against a new API is a large step. The compatibility module takes a smaller one: it has the public API of <a href="https://github.com/lorenzodonini/ocpp-go">ocpp-go</a> v0.19.0 and serves connections on the gocpp runtime, so an existing application switches with one line in <code>go.mod</code>, usually with no change to its code. The exceptions are a few OCPP 2.0.1 (and 1.6) types corrected to the OCA schemas, such as <code>CompositeSchedule</code>; see the compat README.</p>
<h3>One line in <code>go.mod</code></h3>
<p>If your code compiles against ocpp-go v0.19.0, add this line to <code>go.mod</code>, then run <code>go mod tidy</code> and build as before. The import paths in your code stay <code>github.com/lorenzodonini/ocpp-go/...</code>.</p>
""" + codeline("go.mod, for ocpp-go v0.19.0", REPLACE_019) + """
<p class="src">Module <a href="%(c)socppgo/README.md">compat/ocppgo</a>, tag <code>v0.19.0-gocpp.3</code>, built on gocpp v0.4.0. <code>go mod tidy</code> adds gocpp and may raise some <code>golang.org/x</code> versions; the minimum Go version is the module's.</p>
<div class="grid2">
<div class="card"><h4>What changes (server side)</h4><ul>
<li>Each connection is served by gocpp's per-connection dispatcher. There is no dispatcher goroutine or lock shared by all charge points, so a stalled or silent charger does not hold up the others.</li>
<li>A panic in an application handler is answered with <code>InternalError</code>; the process keeps running.</li>
<li>Close codes are sent as WebSocket close frames; an idle connection is closed with 1000.</li>
<li>Outbound requests are ordered per charge point, not globally.</li>
<li>New upgrades get HTTP 503 once <code>Stop</code> has begun.</li>
<li>One extra package, <code>compat</code>, adds what ocpp-go's API has no place for: admission control, readiness, paced drain, metrics and tracing. Using it is optional.</li>
</ul></div>
<div class="card"><h4>What stays</h4><ul>
<li>The public API: import paths, types and signatures are identical. CI runs <code>apidiff</code> against the pinned v0.19.0 tag for all 42 packages and fails on any difference.</li>
<li>Payload validation, error codes and error descriptions are ocpp-go's own code.</li>
<li>The client side (<code>ws.Client</code>, <code>ocppj.Client</code>, the charge-point types) is ocpp-go's unchanged code.</li>
<li>A server that injects its own <code>ServerDispatcher</code> keeps ocpp-go's original code path.</li>
<li>A way back without a rebuild: <code>OCPPGO_COMPAT_RUNTIME=legacy</code> selects ocpp-go's original code path everywhere.</li>
</ul></div>
</div>
<h3>How it was checked</h3>
<ul>
<li><b>API.</b> <code>apidiff</code> in CI: 42 of 42 packages identical to ocpp-go v0.19.0.</li>
<li><b>ocpp-go's own tests.</b> ocpp-go's OCPP 1.6 and 2.0.1 test suites run against the module on the gocpp runtime in CI, and pass.</li>
<li><b>Soak.</b> The module held 4,000 simulated chargers for 25 minutes (40%% of them not answering server calls, 50 server calls per second, five server restarts) with no stall and no crash: 154 MB maximum RSS, 5,988 goroutines (about 1.5 per connection), Heartbeat p50 / p95 / p99 of 0.3 / 0.6 / 0.9 ms. Upstream ocpp-go v0.19.0 under the same load froze after each of its six starts. In a capacity ramp on 1 core and 2 GB the module held 80,571 chargers at 22 to 23 KB per connection and stopped at a Heartbeat p99 of 1.2 s; upstream stopped at 27,774 chargers (35 to 38 KB per connection) after a freeze.</li>
</ul>
<div class="note"><p><b>Read these numbers with care.</b> Single runs over loopback on one shared machine, with a synthetic fleet and no TLS, measured on 2026-11-24 on this module and on upstream ocpp-go v0.19.0 with the same harness. Treat them as an indication and measure with your own traffic before a rollout.</p></div>
<p>For production, switch in stages: build and run your tests with the replace line, try both runtimes (<code>OCPPGO_COMPAT_RUNTIME=gocpp</code> and <code>=legacy</code>), run a canary, and keep the <code>legacy</code> switch as the fallback. The drop-in keeps ocpp-go's API; to move to gocpp's typed API later, see <a href="%(d)smigration-from-ocpp-go.md">Migrating from ocpp-go</a>.</p>
<p class="src">Sources: <a href="%(c)socppgo/README.md">compat/ocppgo/README.md</a> (runtime choice, the <code>compat</code> package, known differences); CI jobs <code>ocppgo-compat</code> and <code>ocppgo-apidiff</code>; the load-test runs of 2026-11-24.</p>
""" % {"c": COMPAT, "d": DOCS}
    parts.append(section("dropin", "Already on ocpp-go", "Drop-in replacement for ocpp-go", "Keep your code and ocpp-go's API; change the server runtime with one line in go.mod.", body))

    # ---- compare ---------------------------------------------------------
    crows = [
        ["OCPP versions", "1.6, 2.0.1, 2.1", "1.6, 2.0.1"],
        ["Message types", "generated from the official JSON schemas", "hand-written structs with validation tags"],
        ["Handler API", "typed generic handlers per message, or generated interfaces with <code>Unimplemented</code> defaults", "one handler interface per feature profile; every method of a profile must be implemented"],
        ["Validation", "JSON Schema (strict, tolerant, lenient with repair), off by default", "struct-tag validation, on by default"],
        ["Outbound calls", "serialized per connection; state owned by the connection", "serialized per client; one dispatcher goroutine and lock shared by all clients"],
        ["Handler panic", "recovered, answered with <code>InternalError</code>", "crashes the process"],
        ["Idle connection", "server pings every 54 s; closes after 60 s of silence with close code 1000", "no server pings; read deadline 60 s, connection dropped (1006)"],
        ["After shutdown starts", "new upgrades get HTTP 503", "keeps accepting"],
        ["Logging", "<code>log/slog</code>", "logrus"],
        ["Idle footprint (measured)", "1 goroutine, 13.6 KB RSS per connection", "2 goroutines per connection; 35 to 38 KB RSS per connected charger under the capacity ramp (idle RSS not measured)"],
        ["CPU with validation on (measured, 30k msg/s, no CSMS calls)", "0.98 cores", "1.01 cores"],
        ["Mute chargers plus CSMS calls (measured, 4,000 chargers, 50 calls/s, 25 min)", "no freeze, 0 crashes", "froze after each of 6 starts; the dispatcher deadlocks (<a href=\"%socpp-go-structural-issues.md\">analysis</a>)" % DOCS],
        ["Migration path", "native API: compatibility preset and an API mapping; or keep ocpp-go's API with the <a href=\"#dropin\">drop-in module</a>", "n/a"],
        ["Maturity", "pre-v1; API may change", "established, widely used"],
    ]
    body = """
<p><a href="https://github.com/lorenzodonini/ocpp-go">ocpp-go</a> is the established OCPP library for Go, and gocpp owes it a great deal: the conformance suite is ported from its per-message tests, and its behaviour is the reference for the compatibility preset. gocpp's own API is a deliberate rewrite with different trade-offs; the table below compares that API with ocpp-go. Where ocpp-go follows the specification, gocpp does too. If you want to keep ocpp-go's API, the <a href="#dropin">drop-in module</a> above runs it on the gocpp runtime.</p>
""" + table(["", "gocpp", "ocpp-go (v0.19.0)"], crows) + """
<p>The design-level problems of ocpp-go v0.19.0 that can be reproduced with small programs, and what gocpp changes and does not change about each, are written up in <a href="%socpp-go-structural-issues.md">docs/ocpp-go-structural-issues.md</a>.</p>
<div class="grid2">
<div class="card"><h4>Choose gocpp when</h4><ul><li>you need OCPP 2.1, or all three versions in one module</li><li>you want generated, typed messages and handlers</li><li>you run large fleets and care about per-connection cost and isolation</li><li>you want deploys that do not disconnect chargers (experimental gateway mode)</li></ul></div>
<div class="card"><h4>Stay with ocpp-go when</h4><ul><li>you need a stable v1-style API today; gocpp is pre-v1 (the <a href="#dropin">drop-in module</a> keeps ocpp-go's API and changes the server runtime only)</li><li>your fleet works and you have no reason to change the wire behaviour</li><li>you depend on its feature-profile structure or its ecosystem</li></ul></div>
</div>
<p class="src">Sources: the differential reports (<code>interop/parity/results</code>), <a href="%smigration-from-ocpp-go.md">docs/migration-from-ocpp-go.md</a>, the load-test runs cited above (upstream ocpp-go v0.19.0), <a href="%socpp-go-structural-issues.md">docs/ocpp-go-structural-issues.md</a>, and ocpp-go's source (<code>ocppj/dispatcher.go</code>, <code>go.mod</code>). Measured rows are single runs on one machine.</p>
""" % (DOCS, DOCS, DOCS)
    parts.append(section("compare", "Comparison", "gocpp and ocpp-go", "A factual comparison for engineers choosing between them, including where ocpp-go is the better choice.", body))

    # ---- docs ------------------------------------------------------------
    links = [
        (DOCS + "usage.md", "Usage guide", "Handlers, calls, options, validation"),
        (DOCS + "architecture.md", "Architecture", "Layers, connection lifecycle, code generation"),
        (DOCS + "performance.md", "Performance and scaling", "Cost model, runtime settings, tuning knobs"),
        (COMPAT + "ocppgo/README.md", "Drop-in replacement for ocpp-go", "One replace line: runtime choice, compat package, known differences"),
        (DOCS + "ocpp-go-structural-issues.md", "Structural issues in ocpp-go v0.19.0", "Reproducible design issues, and what gocpp does about each"),
        (DOCS + "migration-from-ocpp-go.md", "Migrating from ocpp-go", "The compatibility preset, row by row"),
        (DOCS + "migration-from-lorenzodonini.md", "API mapping from ocpp-go", "Before and after, call by call"),
        (DOCS + "draining.md", "Draining", "Deploys and reconnect-storm control"),
        (DOCS + "clustering.md", "Clustering", "Several CSMS instances"),
        (DOCS + "gateway.md", "Gateway mode", "Connection tier and processing tier (experimental)"),
        (DOCS + "staged-releases.md", "Staged releases", "Backend canary and routing policy"),
        (DOCS + "rollout.md", "Operator runbook", "Staged releases on Kubernetes"),
        (PKG, "API reference", "pkg.go.dev"),
        (REPO + "/releases", "Releases", "Tags and release notes"),
        (REPO + "/blob/main/CHANGELOG.md", "Changelog", "Every change, with upgrade notes"),
        (REPO + "/tree/main/examples", "Examples", "Runnable programs"),
        (REPO + "/blob/main/CONTRIBUTING.md", "Contributing", "How to build, test and send changes"),
    ]
    body = '<ul class="linklist">' + "".join('<li><a href="%s"><b>%s</b><span>%s</span></a></li>' % (u, esc(t), esc(s)) for u, t, s in links) + "</ul>"
    body += '<p>gocpp is released under the <a href="%s/blob/main/LICENSE">MIT license</a>. Security reports: see <a href="%s/blob/main/SECURITY.md">SECURITY.md</a>.</p>' % (REPO, REPO)
    parts.append(section("docs", "Documentation", "Where to go next", "Guides, reference and the measurements behind this page.", body))

    page = render_page(
        "gocpp: a typed OCPP 1.6, 2.0.1 and 2.1 library for Go",
        "gocpp is a Go library for OCPP 1.6, 2.0.1 and 2.1, for CSMS backends and charge points: messages generated from the official schemas, spec-correct by default, isolated per connection, and measured.",
        NAV, "\n".join(parts))
    import arch
    arch_page = render_page(
        "gocpp architecture and performance",
        "How gocpp is built and what it costs: package layers, connection lifecycle, the one-goroutine connection model, inbound and outbound paths, the gateway split, and the measurements behind the numbers.",
        arch.NAV, arch.body(sys.modules[__name__]), home="index.html", homelabel="gocpp, introduction")
    (OUT / "assets").mkdir(parents=True, exist_ok=True)
    (OUT / "index.html").write_text(page)
    (OUT / "architecture.html").write_text(arch_page)
    shutil.copyfile(HERE / "style.css", OUT / "assets" / "style.css")
    shutil.copyfile(HERE / "app.js", OUT / "assets" / "app.js")
    (OUT / "assets" / "favicon.svg").write_text(FAVICON)
    (OUT / ".nojekyll").write_text("")
    n = svglib.report()
    print("wrote %s (%d + %d bytes), %d diagram warnings" % (OUT, len(page), len(arch_page), n))
    return 1 if n else 0


if __name__ == "__main__":
    sys.exit(build())
