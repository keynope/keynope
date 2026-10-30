#!/usr/bin/env python3
"""Print element bounds per slide from a Keynope HTML export, in grid rows/cols.

The export embeds every slide's scene as JSON (<script id="keynope-data">), with the
real rendered bounds of each object. Use it to size frames/panels to their content and
to check layering without screenshots.
  python3 scene_bounds.py "<workspace>/preview.html" [slide ...]               # 0-based slides
  python3 scene_bounds.py "<workspace>/preview.html" --panel 0.92 [slide ...]
--panel OPACITY: find the shape with that opacity on each slide, report its rows/cols,
the lowest content below its top and the gap, and list slide objects that sit behind it
or reach outside it. Text boxes are measured at their wrap width, so a wide box can
"reach outside" while its glyphs don't.
Objects marked M are master objects (IDs repeated on several slides); --panel ignores them.
"""
import json
import sys
from collections import Counter

COL, ROW = 1920 / 245, 1080 / 56


def rect(o):
    b = o["bounds"]
    return b["y"] / ROW, (b["y"] + b["height"]) / ROW, b["x"] / COL, (b["x"] + b["width"]) / COL


def label(o):
    t = o.get("text")
    if isinstance(t, dict):
        return "".join(r.get("text", "") for r in t.get("runs", []))[:40].replace("\n", "|")
    return o.get("shape") or ""


def main():
    args = sys.argv[1:]
    if not args:
        sys.exit(__doc__)
    path, rest = args[0], args[1:]
    panel = None
    if "--panel" in rest:
        i = rest.index("--panel")
        panel = float(rest[i + 1])
        del rest[i:i + 2]
    want = [int(v) for v in rest]
    src = open(path, encoding="utf-8").read()
    tag = '<script id="keynope-data" type="application/json">'
    start = src.index(tag) + len(tag)
    pages = json.loads(src[start:src.index("</script>", start)])["pages"]
    seen = Counter(o["id"] for p in pages for o in {o["id"]: o for o in p["scene"]["objects"]}.values())
    master = {i for i, n in seen.items() if n > 1}
    for n in want or range(len(pages)):
        page, objs = pages[n], pages[n]["scene"]["objects"]
        print(f"== slide {n} (#{n + 1}) effect={page.get('effect') or '-'} background={page.get('background') or '-'}")
        if panel is None:
            for o in objs:
                top, bot, left, right = rect(o)
                p = o.get("paint", {})
                print("  %s %-6s rows %5.1f-%5.1f cols %5.1f-%5.1f op=%-4s fill=%s stroke=%s %s %r" % (
                    "M" if o["id"] in master else " ", o["kind"], top, bot, left, right, p.get("opacity", 1),
                    p.get("color"), p.get("stroke"), o["id"], label(o)))
            continue
        hits = [k for k, o in enumerate(objs) if o["kind"] == "shape" and o.get("paint", {}).get("opacity") == panel]
        if len(hits) != 1:
            print(f"  {len(hits)} shapes with opacity {panel}")
            continue
        k = hits[0]
        top, bot, left, right = rect(objs[k])
        own = [(j, o) for j, o in enumerate(objs) if j != k and o["id"] not in master and rect(o)[0] >= top - 0.01]
        if not own:
            print(f"  panel rows {top:.1f}-{bot:.1f} cols {left:.1f}-{right:.1f}; nothing below its top")
            continue
        low = max(rect(o)[1] for _, o in own)
        behind = [o["id"] for j, o in own if j < k]
        outside = [o["id"] for _, o in own if rect(o)[1] > bot + 0.01 or rect(o)[2] < left - 0.01 or rect(o)[3] > right + 0.01]
        print(f"  panel rows {top:.1f}-{bot:.1f} cols {left:.1f}-{right:.1f}; content bottom {low:.1f} (gap {bot - low:.1f})")
        print(f"  behind panel: {behind or 'none'}; outside: {outside or 'none'}")


if __name__ == "__main__":
    main()
