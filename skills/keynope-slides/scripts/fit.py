#!/usr/bin/env python3
"""Line counts and heights for Keynope text, before you place it.

Text lines, one per input line on stdin:  size|width_cols|modern_width|text
  echo '32|212|160|Some long bullet text…' | python3 fit.py
  (modern_width: 160/120/100 for c64; use -100 for mono or sans at width 100)
Prints: lines, chars per line, height in grid rows.

Table rows:  python3 fit.py rows SIZE TABLE_WIDTH_COLS W1,W2,… [PADDING]
  then stdin lines with cells separated by |  -> lines and row height (rows)
"""
import sys

COL, ROW = 7.84, 19.29


def char_px(size, mw):
    if mw < 0:                      # mono/sans
        return 0.6 * size * (-mw / 100)
    return 0.8 * 0.4167 * mw / 100 * size   # c64


def wrap(text, cpl):
    n = 0
    for para in text.split("\\n"):
        cur, lines = 0, 1
        for w in para.split(" "):
            if cur == 0:
                cur = len(w)
            elif cur + 1 + len(w) <= cpl:
                cur += 1 + len(w)
            else:
                lines, cur = lines + 1, len(w)
            while cur > cpl:
                lines, cur = lines + 1, cur - cpl
        n += lines
    return n


def main():
    a = sys.argv[1:]
    if a and a[0] == "rows":
        size, width = float(a[1]), float(a[2])
        weights = [float(x) for x in a[3].split(",")]
        pad = float(a[4]) if len(a) > 4 else 10
        px = [w / sum(weights) * width * COL for w in weights]
        cw = char_px(size, 160)
        for raw in sys.stdin.read().splitlines():
            if not raw.strip():
                continue
            cells = raw.split("|")
            n = max(wrap(t, int((p - 2 * pad) // cw)) for t, p in zip(cells, px))
            print(f"{n} lines, {(n * size * 1.25 + 2 * pad) / ROW:.1f} rows | {cells[0][:30]}")
        return
    lines = [] if sys.stdin.isatty() else [raw for raw in sys.stdin.read().splitlines() if raw.strip()]
    if not lines:
        sys.exit(__doc__)
    for raw in lines:
        size, wc, mw, text = raw.split("|", 3)
        size, wc, mw = float(size), float(wc), float(mw)
        cpl = int(wc * COL // char_px(size, mw))
        n = wrap(text, cpl)
        print(f"{n} lines ({cpl} cpl) {n * size * 1.25 / ROW:.1f} rows | {text[:40]}")


if __name__ == "__main__":
    main()
