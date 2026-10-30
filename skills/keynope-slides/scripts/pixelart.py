#!/usr/bin/env python3
"""Sample a logo image into a grid of U+2586 blocks for a Keynope mono text element.

Needs Pillow:  python3 -m venv <scratch>/venv && <scratch>/venv/bin/pip install pillow
  <scratch>/venv/bin/python -I pixelart.py NAME image.png COLS MODE THRESHOLD [#inkcolour]
MODE: alpha (transparent PNG), dark (black line art), nonwhite (logo on white).
THRESHOLD: share of a cell that must be ink (0.4 for solid logos, 0.12 for thin lines).
#inkcolour: one colour for every block (line art on a dark slide), else each block's average colour.
Writes NAME-grid.txt (hex per cell, '.' empty), NAME-runs.json (set-scene-text runs) and
NAME-plain.json (the plain text to add first), and prints a preview.
Element query: modern-font=mono&modern-line-height=0.76&modern-width=105&ttf-size=12..20
(blocks are 0.63 x 0.76 of the size; the grid uses that 0.82 aspect so the logo keeps its shape).
"""
import json
import sys

from PIL import Image

ASPECT = 0.82
NBSP, BLOCK = " ", "▆"


def is_ink(p, mode):
    r, g, b, a = p
    if mode == "alpha":
        return a > 128
    if mode == "dark":
        return a > 128 and r + g + b < 300
    return a > 128 and 765 - (r + g + b) > 90


def sample(path, cols, mode, thr, ink=None):
    im = Image.open(path).convert("RGBA")
    w, h = im.size
    px = im.load()
    step = max(1, max(w, h) // 400)
    pts = [(x, y) for y in range(0, h, step) for x in range(0, w, step) if is_ink(px[x, y], mode)]
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    im = im.crop((min(xs), min(ys), max(xs) + 1, max(ys) + 1))
    w, h = im.size
    rows = max(1, round(h / (w / cols / ASPECT)))
    small = im.resize((cols * 8, rows * 8), Image.LANCZOS).load()
    grid = []
    for r in range(rows):
        row = []
        for c in range(cols):
            cell = [small[x, y] for y in range(r * 8, r * 8 + 8) for x in range(c * 8, c * 8 + 8)]
            hit = [p for p in cell if is_ink(p, mode)]
            if len(hit) / 64 >= thr:
                row.append(ink or "#%02x%02x%02x" % tuple(sum(p[i] for p in hit) // len(hit) for i in range(3)))
            else:
                row.append(".")
        grid.append(row)
    return grid


def runs(grid):
    out = []
    for i, row in enumerate(grid):
        row = list(row)
        while row and row[-1] == ".":
            row.pop()
        for cell in row + (["\n"] if i < len(grid) - 1 else []):
            text, colour = (NBSP, None) if cell == "." else ("\n", None) if cell == "\n" else (BLOCK, cell)
            if out and out[-1].get("color") == colour:
                out[-1]["text"] += text
            else:
                out.append({"text": text, **({"color": colour} if colour else {})})
    return out


def main():
    if len(sys.argv) < 6:
        sys.exit(__doc__)
    name, path, cols, mode, thr = sys.argv[1:6]
    ink = sys.argv[6] if len(sys.argv) > 6 else None
    grid = sample(path, int(cols), mode, float(thr), ink)
    for row in grid:
        print("".join("#" if c != "." else "." for c in row))
    print(f"{len(grid[0])} x {len(grid)}")
    with open(f"{name}-grid.txt", "w") as f:
        f.write(f"# {name}: {len(grid[0])} x {len(grid)} blocks from {path.split('/')[-1]}; '.' = empty\n")
        f.writelines(" ".join(r) + "\n" for r in grid)
    r = runs(grid)
    json.dump(r, open(f"{name}-runs.json", "w"))
    json.dump("".join(x["text"] for x in r), open(f"{name}-plain.json", "w"))


if __name__ == "__main__":
    main()
