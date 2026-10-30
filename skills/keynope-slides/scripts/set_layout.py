#!/usr/bin/env python3
"""Give slides a layout in a saved Keynope deck, so base-master elements show on them.

The MCP has no working command for this. deck_save first, run this, then deck_open the file again.
  python3 set_layout.py "<workspace>/deck.md" blank                 # every slide
  python3 set_layout.py "<workspace>/deck.md" blank --except 0 33   # 0-based slides to leave without a layout
"""
import sys


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    path, layout = sys.argv[1], sys.argv[2]
    skip = {int(v) for v in sys.argv[3:] if v.isdigit()}
    tag = f"<!-- layout={layout} -->"
    lines = open(path, encoding="utf-8").read().split("\n")
    # Slide 0 starts at the top of the file; every "---" after a blank line starts the next slide.
    seps = [i for i, line in enumerate(lines) if line == "---" and i and lines[i - 1] == ""]
    drop, after, replace, missing = set(), set(), {}, []
    for n, (a, b) in enumerate(zip([0] + seps, seps + [len(lines)])):
        want = n not in skip
        sid = next((i for i in range(a, b) if lines[i].startswith("<!-- keynope-slide-id=")), None)
        if sid is not None:
            if sid + 1 < b and lines[sid + 1].startswith("<!-- layout="):
                drop.add(sid + 1)   # re-added below if wanted
            if want:
                after.add(sid)
            continue
        # A new deck's first slide has no id line; its layout line stands alone before the first "---".
        own = [i for i in range(a, b) if lines[i].startswith("<!-- layout=")]
        if own:
            if want:
                replace[own[-1]] = tag
            else:
                drop.add(own[-1])
        elif want:
            missing.append(n)
    out = []
    for i, line in enumerate(lines):
        if i not in drop:
            out.append(replace.get(i, line))
        if i in after:
            out.append(tag)
    open(path, "w", encoding="utf-8").write("\n".join(out))
    print(f"{len(seps) + 1} slides; layout '{layout}' on all except {sorted(skip)}")
    if missing:
        print(f"Not changed (no id or layout line to anchor to): slides {missing}. Edit and save them once, then run this again.")


if __name__ == "__main__":
    main()
