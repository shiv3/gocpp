"""Tiny SVG builder with text-fit checks. Output is static inline SVG."""
import html
import sys

WARN = []


def tw(s, size, mono=False):
    """Rough rendered width of s at font-size size."""
    w = 0.0
    for ch in s:
        o = ord(ch)
        if o >= 0x2E80:
            w += size
        elif mono:
            w += size * 0.61
        elif ch in "ilI.,:;|'!()[] ":
            w += size * 0.32
        elif ch.isupper() or ch in "mwMW@%":
            w += size * 0.70
        else:
            w += size * 0.56
    return w


def esc(s):
    return html.escape(str(s), quote=True)


class Svg:
    count = 0

    def __init__(self, w, h, title, desc="", minw=None, cls=""):
        Svg.count += 1
        self.uid = "m%d" % Svg.count
        self.w, self.h = w, h
        self.title, self.desc = title, desc
        self.minw = minw or min(w, 760)
        self.cls = cls
        self.out = []

    # --- primitives -------------------------------------------------------
    def raw(self, s):
        self.out.append(s)

    def text(self, x, y, s, size=12, cls="", anchor="middle", mono=False, bold=False, maxw=None, halo=False):
        c = " ".join(filter(None, ["t", cls, "mono" if mono else "", "b" if bold else "", "halo" if halo else ""]))
        if maxw is not None:
            w = tw(s, size, mono)
            if w > maxw:
                WARN.append(f"[{self.title}] text too wide ({w:.0f}>{maxw:.0f}): {s}")
        self.out.append(
            f'<text x="{x:.1f}" y="{y:.1f}" font-size="{size}" text-anchor="{anchor}" class="{c}">{esc(s)}</text>'
        )

    def box(self, x, y, w, h, kind, title=None, subs=(), path=None, align="middle", rx=8, tsize=12.5, ssize=11, extra=""):
        """Box with stacked centred lines: title (bold), subs, path caption (mono)."""
        self.out.append(f'<g class="bx k-{kind} {extra}"><rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}"/>')
        lines = []
        if title:
            lines.append((title, tsize, "tt", False, True))
        for s in subs:
            if isinstance(s, tuple):
                lines.append((s[0], ssize, "ts", True, False))
            else:
                lines.append((s, ssize, "ts", False, False))
        if path:
            lines.append((path, 9.5, "tp", True, False))
        total = sum(sz * 1.38 for _, sz, *_ in lines)
        cy = y + (h - total) / 2
        if total > h - 4:
            WARN.append(f"[{self.title}] box too short for lines: {title}")
        for s, sz, cls, mono, bold in lines:
            cy += sz * 1.38
            ty = cy - sz * 0.36
            if align == "middle":
                tx, mw = x + w / 2, w - 12
            else:
                tx, mw = x + 10, w - 16
            self.text(tx, ty, s, sz, cls, anchor=align, mono=mono, bold=bold, maxw=mw)
        self.out.append("</g>")

    def line(self, x1, y1, x2, y2, cls="ar", arrow=True, dashed=False, start=False):
        c = cls + (" dash" if dashed else "")
        m = ' marker-end="url(#%s)"' % self.uid if arrow else ""
        if start:
            m += ' marker-start="url(#%ss)"' % self.uid
        self.out.append(f'<line x1="{x1:.1f}" y1="{y1:.1f}" x2="{x2:.1f}" y2="{y2:.1f}" class="{c}"{m}/>')

    def path(self, d, cls="ar", arrow=True, dashed=False, start=False):
        c = cls + (" dash" if dashed else "")
        m = ' marker-end="url(#%s)"' % self.uid if arrow else ""
        if start:
            m += ' marker-start="url(#%ss)"' % self.uid
        self.out.append(f'<path d="{d}" class="{c}" fill="none"{m}/>')

    def label(self, x, y, s, size=10.5, anchor="middle", cls="tl", mono=False, maxw=None):
        self.text(x, y, s, size, cls, anchor=anchor, mono=mono, halo=True, maxw=maxw)

    def chip(self, x, y, s, kind="err", size=10.5, mono=False, anchor="start"):
        """Small pill; x,y = left-centre (or centre if anchor=middle). Returns width."""
        w = tw(s, size, mono) + 14
        x0 = x if anchor == "start" else x - w / 2
        self.out.append(
            f'<g class="chip c-{kind}"><rect x="{x0:.1f}" y="{y - 9:.1f}" width="{w:.1f}" height="18" rx="9"/>'
            f'<text x="{x0 + w / 2:.1f}" y="{y + 3.6:.1f}" font-size="{size}" text-anchor="middle" class="t{" mono" if mono else ""}">{esc(s)}</text></g>'
        )
        return w

    def num(self, x, y, n):
        self.out.append(
            f'<g class="num"><circle cx="{x}" cy="{y}" r="9.5"/><text x="{x}" y="{y + 3.7}" font-size="10.5" text-anchor="middle" class="t b">{n}</text></g>'
        )

    def group_frame(self, x, y, w, h, title, cls="frame", sub=None):
        self.out.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="10" class="{cls}"/>')
        self.text(x + 12, y + 17, title, 11.5, "tf", anchor="start", bold=True, maxw=w - 20)
        if sub:
            self.text(x + w - 12, y + 17, sub, 9.5, "tp", anchor="end", mono=True)

    # --- output -----------------------------------------------------------
    def render(self):
        defs = (
            '<defs><marker id="%s" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
            '<path d="M0,0.8 L9,5 L0,9.2 z" class="ahp"/></marker>'
            '<marker id="%ss" viewBox="0 0 10 10" refX="1.5" refY="5" markerWidth="7" markerHeight="7" orient="auto">'
            '<path d="M10,0.8 L1,5 L10,9.2 z" class="ahp"/></marker></defs>'
        ) % (self.uid, self.uid)
        return (
            f'<div class="figscroll"><svg class="dg {self.cls}" viewBox="0 0 {self.w} {self.h}" style="min-width:{self.minw}px" role="img" '
            f'aria-label="{esc(self.title)}"><title>{esc(self.title)}</title><desc>{esc(self.desc)}</desc>{defs}'
            + "".join(self.out)
            + "</svg></div>"
        )


def report():
    for w in WARN:
        print("WARN", w, file=sys.stderr)
    return len(WARN)
