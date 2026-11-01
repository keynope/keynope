#!/usr/bin/env python3
"""Turn an ASCII diagram into Keynope-ready text strings.

Keynope strips leading spaces, so every space becomes a non-breaking space
(U+00A0). Each layer keeps only the characters inside its rectangles, so all
layers can sit at the same left/top in the same mono font/size and line up
exactly; give each its own colour and build step.

Usage:
  ascii_layers.py diagram.txt                      # whole diagram, one string
  ascii_layers.py diagram.txt agent=7-9:1-27 rule=2-6:1-9 plugin=0-0:0-28+0-15:0-0+0-15:28-28+15-15:0-28

A spec is name=ROWS:COLS with inclusive 0-based ranges; join rectangles with +.
Output: one line per layer, `name<TAB>"json string"`, with \\u00a0 escapes,
ready to paste into an add-element "text" field.
"""
import json
import sys

NBSP = " "


def parse_spec(spec):
    name, _, rects = spec.partition("=")
    out = []
    for rect in rects.split("+"):
        rows, cols = rect.split(":")
        r0, r1 = (int(v) for v in rows.split("-"))
        c0, c1 = (int(v) for v in cols.split("-"))
        out.append((r0, r1, c0, c1))
    return name, out


def render(lines, rects=None):
    out = []
    for r, line in enumerate(lines):
        row = ""
        for c, ch in enumerate(line):
            inside = rects is None or any(r0 <= r <= r1 and c0 <= c <= c1 for r0, r1, c0, c1 in rects)
            row += ch if inside and ch != " " else NBSP
        out.append(row.rstrip(NBSP) or NBSP)
    while len(out) > 1 and out[-1] == NBSP:
        out.pop()
    return "\n".join(out)


def emit(name, text):
    print(name + "\t" + json.dumps(text, ensure_ascii=False).replace(NBSP, "\\u00a0"))


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    lines = open(sys.argv[1], encoding="utf-8").read().rstrip("\n").split("\n")
    specs = sys.argv[2:]
    if not specs:
        emit("all", render(lines))
    for spec in specs:
        name, rects = parse_spec(spec)
        emit(name, render(lines, rects))


if __name__ == "__main__":
    main()
