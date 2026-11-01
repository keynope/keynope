#!/usr/bin/env python3
"""List element IDs per slide from a saved Keynope deck (.md).

Cheaper than slide_inspect: deck_save the deck, then
  python3 list_ids.py "<workspace>/deck.md" [slide ...]      # 0-based slides
  python3 list_ids.py "<workspace>/deck.md" --tables [slide ...]  # also print tableData JSON
Prints: slide, element-id, top/left, width, size, font, first 40 chars of text.
Master elements are not listed (their IDs are in every MCP result).
"""
import base64
import gzip
import json
import re
import sys


def main():
    args = sys.argv[1:]
    if not args:
        sys.exit(__doc__)
    path, rest = args[0], args[1:]
    tables = "--tables" in rest
    want = {int(v) for v in rest if v.lstrip("-").isdigit()}
    src = open(path, encoding="utf-8").read()
    table_data = {}
    m = re.search(r"keynope-objects version=1 base64:(\S+)", src)
    if m:
        table_data = json.loads(gzip.decompress(base64.b64decode(m.group(1)))).get("tables", {})
    lines = src.split("\n")
    # Slide 0 starts at the top of the file (a new deck's first slide has no id line);
    # every "---" after a blank line starts the next slide.
    slide = 0
    for i, line in enumerate(lines):
        if line == "---" and i and lines[i - 1] == "":
            slide += 1
            continue
        m = re.search(r"element-id=([\w-]+)", line)
        if not m or (want and slide not in want):
            continue
        q = lambda k: (re.search(r"(?<![\w-])" + k + r"=([^ &]+)", line) or [None, "-"])[1]
        text = lines[i + 1] if i + 1 < len(lines) else ""
        b64 = re.search(r"truetype-text=base64:(\S+)", text)
        if b64:
            text = base64.b64decode(b64.group(1)).decode("utf-8", "replace")
        text = text.replace("\n", "⏎").replace(" ", " ")[:40]
        print(f"s{slide} {m.group(1)} t{q('top')} l{q('left')} w{q('width')} "
              f"sz{q('ttf-size')} {q('modern-font')} | {text}")
        tid = re.search(r"object-id=(table-data-\w+)", line)
        if tables and tid and tid.group(1) in table_data:
            print("   tableData:", json.dumps(table_data[tid.group(1)], ensure_ascii=False))


if __name__ == "__main__":
    main()
