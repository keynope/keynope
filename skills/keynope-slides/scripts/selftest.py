#!/usr/bin/env python3
"""Check the keynope-slides skill against the installed Keynope.

Builds a throwaway deck in the automation workspace and checks the commands, workarounds
and helper scripts that SKILL.md documents. It never calls slide_screenshot.
  python3 scripts/selftest.py                                      # starts a private headless server (stdio)
  python3 scripts/selftest.py --url http://127.0.0.1:50987/mcp     # uses Keynope's in-app server instead
  python3 scripts/selftest.py --workspace DIR --keynope /path/to/Keynope.app/Contents/MacOS/Keynope --keep
--url needs Keynope open with Automation (MCP) > Enable MCP access ticked. It only runs while
that server's automation deck is a blank Untitled deck (--discard overrides this) and resets it
to a blank deck afterwards.
--workspace defaults to the Mac App Store build's automation folder when it exists.
Writes keynope-skill-selftest.* to the workspace and deletes them afterwards (--keep
leaves them); refuses to run if any of them already exist. The Pillow scripts are
checked only when this interpreter can import PIL.
Exit code 0 means everything the skill relies on still holds.
"""
import argparse
import base64
import json
import os
import queue
import re
import struct
import subprocess
import sys
import tempfile
import threading
import urllib.error
import urllib.request
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
SKILL = os.path.join(os.path.dirname(HERE), "SKILL.md")
REFERENCE = os.path.join(os.path.dirname(HERE), "reference.md")
APP_STORE_WS = os.path.expanduser("~/Library/Containers/sh.keynope.app/Data/Library/Application Support/Keynope/Automation")
NAME = "keynope-skill-selftest"
results = []


def check(label, ok, detail=""):
    ok = bool(ok)
    results.append(ok)
    print(("PASS " if ok else "FAIL ") + label + (f"  ({detail})" if detail else ""))
    return ok


def skip(label, why):
    print(f"SKIP {label}  ({why})")


class Server:
    def __init__(self, exe, workspace):
        self.p = subprocess.Popen([exe, "--mcp", "--workspace", workspace], stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        self.lines, self.n, self.version, self.sizes = queue.Queue(), 0, None, []
        threading.Thread(target=lambda: [self.lines.put(l) for l in self.p.stdout], daemon=True).start()
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "keynope-skill-selftest", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def send(self, msg):
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()

    def rpc(self, method, params):
        self.n += 1
        self.send({"jsonrpc": "2.0", "id": self.n, "method": method, "params": params})
        while True:
            try:
                msg = json.loads(self.lines.get(timeout=90))
            except queue.Empty:
                sys.exit("Keynope did not answer within 90 s. In the App Store build the workspace must be "
                         "the folder shown in Keynope's Automation (MCP) window.")
            if msg.get("id") == self.n:
                return msg

    def tool(self, name, **args):
        """Returns (data, error). Tracks expected_version from every result that carries one."""
        msg = self.rpc("tools/call", {"name": name, "arguments": args})
        res = msg.get("result") or {}
        text = "".join(c.get("text", "") for c in res.get("content", []) if c.get("type") == "text")
        if "error" in msg or res.get("isError"):
            return None, text or json.dumps(msg.get("error"))
        try:
            data = json.loads(text)
        except ValueError:
            data = text
        if isinstance(data, dict):
            version = data.get("version", data.get("deck", {}).get("version") if isinstance(data.get("deck"), dict) else None)
            if isinstance(version, int):
                self.version = version
        if name in ("editor_batch", "editor_command"):
            self.sizes.append(len(text))
        return data, None

    def inspect(self):
        data, err = self.tool("deck_inspect")
        if err:
            sys.exit("deck_inspect failed: " + err)
        self.version = data["version"]
        return data

    def batch(self, commands):
        return self.tool("editor_batch", expected_version=self.version, commands=commands)

    def close(self):
        self.p.terminate()
        try:
            self.p.wait(5)
        except subprocess.TimeoutExpired:
            self.p.kill()


class HttpServer(Server):
    """Keynope's in-app server: stateless streamable HTTP, answers as server-sent events."""

    def __init__(self, url):
        self.url, self.n, self.version, self.sizes = url, 0, None, []
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "keynope-skill-selftest", "version": "1"}})

    def rpc(self, method, params):
        self.n += 1
        body = json.dumps({"jsonrpc": "2.0", "id": self.n, "method": method, "params": params}).encode()
        request = urllib.request.Request(self.url, data=body, headers={
            "Content-Type": "application/json", "Accept": "application/json, text/event-stream"})
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                raw = response.read().decode("utf-8")
        except urllib.error.HTTPError as e:
            sys.exit(f"{self.url} answered {e.code}: {e.read().decode('utf-8', 'replace').strip()[:200]}")
        except urllib.error.URLError as e:
            sys.exit(f"Can't reach {self.url} ({e.reason}). Open Keynope and tick Automation (MCP) > Enable MCP access.")
        for line in raw.splitlines():
            if line.startswith("data: "):
                msg = json.loads(line[6:])
                if msg.get("id") == self.n:
                    return msg
        return json.loads(raw)

    def close(self):
        pass


def script(name, *args, stdin=None):
    r = subprocess.run([sys.executable, "-I", os.path.join(HERE, name), *args], input=stdin,
                       capture_output=True, text=True, timeout=120)
    return r.returncode, r.stdout + r.stderr


def png(path, w=64, h=32, rgb=(85, 255, 255)):
    rows = b"".join(b"\0" + bytes(rgb) * w for _ in range(h))
    chunk = lambda kind, data: struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))


def ids(md_path, slide):
    """{text and kind: (id, top, left, width)} for one slide, from list_ids.py (first match wins)."""
    code, out = script("list_ids.py", md_path, str(slide))
    found = {}
    for line in out.splitlines():
        m = re.match(r"s\d+ ([\w-]+) t(\S+) l(\S+) w(\S+) \S+ \S+ \| ?(.*)", line)
        if m:
            row = (m.group(1), m.group(2), m.group(3), m.group(4))
            found.setdefault(m.group(5), row)
            found.setdefault(m.group(1).split("-")[0], row)
    return code, found


def pages(html_path):
    src = open(html_path, encoding="utf-8").read()
    tag = '<script id="keynope-data" type="application/json">'
    start = src.index(tag) + len(tag)
    return json.loads(src[start:src.index("</script>", start)])["pages"]


def run(server, ws, keep):
    md, html, img = (os.path.join(ws, f"{NAME}.{ext}") for ext in ("md", "html", "png"))

    # Tools and the command list the skill documents
    names = {t["name"] for t in server.rpc("tools/list", {})["result"]["tools"]}
    needed = {"deck_inspect", "deck_open", "deck_save", "deck_export", "deck_validate", "editor_batch",
              "editor_command", "asset_import", "selection_copy", "deck_schema"}
    check("tools the skill uses exist", needed <= names, ", ".join(sorted(needed - names)) or f"{len(names)} tools")
    schema, err = server.tool("deck_schema", topic="commands")
    live = set(schema["schema"]["commands"]) if not err else set()
    docs = "".join(open(p, encoding="utf-8").read() for p in (SKILL, REFERENCE) if os.path.exists(p))
    m = re.search(r"Full command list: (.*?)\. There is", docs)
    documented = set(m.group(1).split(", ")) if m else set()
    check("documented command list matches deck_schema", live == documented and bool(live),
          f"new: {sorted(live - documented)}, gone: {sorted(documented - live)}" if live != documented else f"{len(live)} commands")

    deck = server.inspect()
    check("the automation deck starts with one slide", len(deck["slides"]) == 1, f"{len(deck['slides'])} slides")

    # Structure batch: slides, update-slide quirks, text + shape at creation, notes
    v0 = server.version
    text_q = "render=truetype&modern-font=c64&modern-width=160&ttf-size=40&fg=#ffffff&top=16&left=18&width=214"
    shape_q = "shape=square&top=25&left=15&width=214&height=13&fg=#202833&shape-stroke=#8b9ab5&shape-stroke-width=2"
    slide = lambda n: {"action": "select-slide", "slide": n}
    cmds = [{"action": "add-slide"}, {"action": "add-slide"}, {"action": "add-slide"},
            slide(1), {"action": "update-slide", "slide": 1, "slideData": {"bg": "#070a14", "bgSet": True, "fg": "#ffffff", "fgSet": True,
                                                                          "effect": "warp", "effectSet": True, "background": "constellation", "backgroundSet": True}},
            slide(2), {"action": "update-slide", "slide": 1, "slideData": {"effect": "radar", "effectSet": True, "background": "aurora", "backgroundSet": True}},
            slide(3), {"action": "update-slide", "slide": 3, "slideData": {"effect": "circuit", "effectSet": True, "background": "waves", "backgroundSet": True}},
            {"action": "update-slide", "slide": 3, "slideData": {"effect": "scanline", "effectSet": True}},
            slide(1), {"action": "add-element", "kind": "text", "elementData": {"kind": "text", "text": "Hello selftest", "query": text_q}},
            {"action": "add-element", "kind": "shape", "name": "square"},
            {"action": "update-element", "element": 0, "elementData": {"kind": "shape", "text": "[shape:square]", "query": shape_q}},
            {"action": "update-slide-notes", "slide": 1, "notes": "Selftest notes"}, slide(1)]
    _, err = server.batch(cmds)
    check("structure batch applies", not err, err or "")
    check("every command bumps the version by 1", server.version == v0 + len(cmds), f"{v0} + {len(cmds)} -> {server.version}")

    _, err = server.tool("deck_save", expected_version=server.version, path=f"{NAME}.md", overwrite=False)
    check("deck_save to a new file", not err, err or "")
    blocks = re.split(r"\n\n---\n", open(md, encoding="utf-8").read())
    layouts = ["<!-- layout=blank -->" in b for b in blocks]
    check("a new deck's first slide has layout blank, add-slide gives none", layouts == [True, False, False, False], str(layouts))
    code, found = ids(md, 1)
    text, shape = found.get("Hello selftest"), found.get("shape")
    check("list_ids.py finds the text with its query position", code == 0 and text and text[1:] == ("16", "18", "214"), str(text))
    check("update-element 0 targets the new shape at top 0", bool(shape) and shape[1:3] == ("25", "15"), str(shape))
    if not (text and shape):
        return
    text_id, shape_id = text[0], shape[0]

    # A failed batch keeps the commands before the failure; set-layout is still unusable
    v = server.version
    _, err = server.batch([slide(0), {"action": "set-layout", "value": 0}])
    server.inspect()
    check("set-layout is still rejected (keep set_layout.py)", bool(err) and "invalid editor action" in err, (err or "it worked")[:120])
    check("a failed batch keeps the commands before the failure", server.version == v + (1 if err else 2), f"{v} -> {server.version}")
    for label, command, expect in [
        ("set-scene-text without objectId fails", {"action": "set-scene-text", "textRuns": [{"text": "x"}]}, "invalid editor action"),
        ("geometry outside the slide is refused", {"action": "set-selection-geometry", "objectIds": [shape_id],
                                                   "geometry": {"x": 240, "y": 50, "width": 20, "height": 10}, "geometryResize": True}, "within the slide"),
        ("move-object-layer field names are still unknown", {"action": "move-object-layer", "objectIds": [shape_id], "name": "front"}, "invalid")]:
        server.tool("editor_command", expected_version=server.version, command=slide(1))
        _, err = server.tool("editor_command", expected_version=server.version, command=command)
        check(label, bool(err) and expect in err, (err or "it worked")[:120])
        if not err:
            server.inspect()

    # Pass B: runs, properties, fractional geometry, builds, clone/reorder, table, master
    cmds = [slide(1),
            {"action": "set-scene-text", "objectId": text_id, "textRuns": [{"text": "Hello "}, {"text": "runs", "color": "#55ffff"}, {"text": " →", "fontId": "mono"}]},
            {"action": "set-selection-property", "objectIds": [shape_id], "property": "paint.shadow", "propertyValue": "none"},
            {"action": "set-selection-geometry", "objectIds": [shape_id], "geometry": {"x": 15, "y": 25.5, "width": 214, "height": 12.5}, "geometryResize": True},
            {"action": "set-slide-build", "slide": 1, "buildData": {"version": 1, "transition": {"effect": "fade"}, "steps": [
                {"targetIds": [shape_id], "effect": "wipe", "direction": "right", "durationMs": 500},
                {"targetIds": [text_id], "effect": "fade", "direction": "up", "durationMs": 400, "advance": "after", "delayMs": 200}]}},
            {"action": "clone-slide"}, {"action": "reorder-slide", "slide": 2, "value": 4},
            slide(0), {"action": "insert-table", "cols": 2, "rows": 2},
            {"action": "toggle-master-mode"},
            {"action": "add-element", "kind": "text", "elementData": {"kind": "text", "text": "MASTER LOGO",
                                                                      "query": "render=truetype&modern-font=c64&ttf-size=24&fg=#ffcc00&top=1&left=200&width=40"}},
            {"action": "toggle-master-mode"}, slide(0)]
    _, err = server.batch(cmds)
    check("pass-B batch (runs, property, geometry, build, clone, reorder, table, master)", not err, (err or "")[:200])
    _, err = server.tool("deck_save", expected_version=server.version, path=f"{NAME}.md", overwrite=True)
    code, found = ids(md, 0)
    table = found.get("table")
    check("insert-table lands at x24 y10", bool(table) and table[1:3] == ("10", "24"), str(table))
    if table:
        table_data = {"version": 1, "rows": [{"height": 3}, {"height": 5}], "columns": [{"width": 150}, {"width": 790}],
                      "cells": [[{}, {"text": "Header", "runs": [{"text": "Header", "bold": True, "color": "#ffcc00"}]}],
                                [{"text": "Label"}, {"text": "Mixed cell", "runs": [{"text": "Mixed cell", "color": "#55ffff"}]}]],
                      "style": {"header": True, "banded": False, "headerFill": "#1a2233", "cell": {
                          "fill": "#101828", "color": "#ffffff", "borderColor": "#334155", "borderWidth": 1, "fontId": "c64",
                          "fontSize": 28, "fontWidth": 160, "lineHeight": 1.25, "align": "left", "vertical": "middle", "padding": 12}}}
        _, err = server.batch([slide(0), {"action": "set-table", "objectId": table[0], "tableData": table_data},
                               {"action": "set-selection-geometry", "objectIds": [table[0]],
                                "geometry": {"x": 24, "y": 12, "width": 150, "height": 10}, "geometryResize": True}, slide(0)])
        check("set-table with the SKILL.md tableData example", not err, (err or "")[:200])

    # Images
    png(img)
    server.tool("editor_command", expected_version=server.version, command=slide(3))
    data, err = server.tool("asset_import", expected_version=server.version, path=f"{NAME}.png")
    image_id = ((data or {}).get("selection") or {}).get("targetIds", [None])[0] if isinstance(data, dict) else None
    check("asset_import returns the image ID in selection.targetIds[0]", bool(image_id), err or str(image_id))
    if image_id:
        _, err = server.batch([{"action": "set-selection-property", "objectIds": [image_id], "property": "image.alpha", "propertyValue": 100},
                               {"action": "set-selection-geometry", "objectIds": [image_id],
                                "geometry": {"x": 20, "y": 20, "width": 40, "height": 8}, "geometryResize": True}, slide(3)])
        check("image.alpha and geometry on an imported image", not err, (err or "")[:200])

    # Layout workaround: save, set_layout.py, reopen, save again
    _, err = server.tool("deck_save", expected_version=server.version, path=f"{NAME}.md", overwrite=True)
    code, out = script("set_layout.py", md, "blank", "--except", "0")
    check("set_layout.py marks slides", code == 0 and open(md, encoding="utf-8").read().count("<!-- layout=blank -->") == 4, out.strip()[:120])
    _, err = server.tool("deck_open", expected_version=server.version, path=f"{NAME}.md")
    check("deck_open the edited file", not err, (err or "")[:200])
    server.inspect()
    _, err = server.tool("deck_save", expected_version=server.version, path=f"{NAME}.md", overwrite=True)
    check("deck_save after reopening", not err, (err or "")[:200])

    # Export: check the rendered result
    data, err = server.tool("deck_export", expected_version=server.version, format="html", path=f"{NAME}.html", overwrite=True)
    check("deck_export html", not err, (err or "")[:200])
    if err:
        return
    pg = pages(html)
    eff = [(p.get("effect") or "-", p.get("background") or "-") for p in pg]
    check("update-slide sets effect and background", eff[1] == ("warp", "constellation"), str(eff[1]))
    check("update-slide ignores 'slide' and changes the current slide", eff[2] == ("radar", "aurora"), str(eff))
    check("effect without background clears the background", eff[3] == ("scanline", "-"), str(eff[3]))
    texts = lambda p: [o for o in p["scene"]["objects"] if o["kind"] == "text"]
    joined = lambda o: "".join(r.get("text", "") for r in (o.get("text") or {}).get("runs", [])) if isinstance(o.get("text"), dict) else ""
    check("clone-slide + reorder-slide copy the slide to the end", any(joined(o) == "Hello runs →" for o in texts(pg[4])), str(eff[4]))
    runs = [r for o in texts(pg[1]) for r in (o.get("text") or {}).get("runs", [])]
    check("set-scene-text runs keep colour and font", any(r.get("text") == "runs" and str(r.get("color", "")).lower() == "#55ffff" for r in runs)
          and any(r.get("fontId") == "mono" for r in runs), str(runs)[:160])
    masters = [any(joined(o) == "MASTER LOGO" for o in texts(p)) for p in pg]
    check("master elements show only on layout=blank slides", masters == [False, True, True, True, True], str(masters))
    check("builds reach the export", "wipe" in json.dumps(pg[1]), "")
    check("tables and images reach the export", any(o["kind"] == "table" for o in pg[0]["scene"]["objects"])
          and any(o["kind"] == "image" for o in pg[3]["scene"]["objects"]), "")
    code, out = script("scene_bounds.py", html, "1")
    check("scene_bounds.py reads the export", code == 0 and "effect=warp background=constellation" in out, out.splitlines()[0] if out else "")

    data, err = server.tool("deck_validate")
    issues = data.get("issues", data.get("diagnostics", [])) if isinstance(data, dict) else []
    errors = [i for i in issues if isinstance(i, dict) and i.get("severity") == "error"]
    check("deck_validate reports no errors", not err and not errors, (err or json.dumps(errors))[:200])
    # Last, because it leaves the throwaway deck unsaveable
    _, err = server.batch([slide(2), {"action": "add-element", "kind": "shape", "name": "square"},
                           {"action": "update-element", "element": 0, "elementData": {"kind": "shape", "text": "[shape:square]",
                                                                                      "query": "shape=square&top=30.5&left=15&width=20&height=3"}}, slide(2)])
    _, save_err = server.tool("deck_save", expected_version=server.version, path=f"{NAME}.md", overwrite=True)
    check("a fractional top in a shape query is accepted but breaks deck_save", not err and bool(save_err)
          and "invalid numeric placement field" in save_err, (err or save_err or "saved fine")[:120])
    size = max(server.sizes) if server.sizes else 0
    print(f"INFO largest editor result on this 5-slide deck: {size} chars (the token table in SKILL.md assumes ~1.5k + ~220 per slide)")


def offline_scripts(ws):
    code, out = script("fit.py", stdin="32|212|160|Some long bullet text that wraps onto a second line for sure\n")
    check("fit.py", code == 0 and "lines" in out and "rows" in out, out.strip()[:80])
    with tempfile.TemporaryDirectory() as tmp:
        diagram = os.path.join(tmp, "d.txt")
        open(diagram, "w", encoding="utf-8").write("+--+\n|ab|\n+--+\n")
        code, out = script("ascii_layers.py", diagram, "box=0-2:0-3")
        check("ascii_layers.py", code == 0 and out.startswith("box\t"), out.strip()[:80])
        try:
            import PIL  # noqa: F401
        except ImportError:
            skip("pixelart.py and fireworks_gif.py", "Pillow not installed for this interpreter")
            return
        logo = os.path.join(tmp, "logo.png")
        png(logo, 40, 40, (255, 85, 85))
        code, out = script("pixelart.py", os.path.join(tmp, "logo"), logo, "12", "nonwhite", "0.4")
        check("pixelart.py", code == 0 and os.path.exists(os.path.join(tmp, "logo-runs.json")), out.strip()[-80:])
        gif = os.path.join(tmp, "fw.gif")
        code, out = script("fireworks_gif.py", gif, "--size", "160x90", "--seconds", "2", "--rockets", "4")
        check("fireworks_gif.py", code == 0 and os.path.getsize(gif) > 0, out.strip()[-80:])


def main():
    ap = argparse.ArgumentParser(usage=__doc__)
    ap.add_argument("--workspace", default=APP_STORE_WS if os.path.isdir(APP_STORE_WS) else None)
    ap.add_argument("--keynope", default="/Applications/Keynope.app/Contents/MacOS/Keynope")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--url")
    ap.add_argument("--discard", action="store_true")
    a = ap.parse_args()
    if not a.workspace:
        sys.exit("Pass --workspace: the folder shown in Keynope's Automation (MCP) window.")
    paths = [os.path.join(a.workspace, f"{NAME}.{ext}") for ext in ("md", "html", "png")]
    if any(os.path.exists(p) for p in paths):
        sys.exit(f"{NAME}.* already exists in the workspace; move it away first.")
    server = HttpServer(a.url) if a.url else Server(a.keynope, a.workspace)
    started = False
    try:
        deck = server.inspect()
        blank = (len(deck["slides"]) == 1 and deck["slides"][0].get("elements") == 0
                 and os.path.basename(deck.get("path") or "") == "Untitled.md"
                 and not (((deck.get("masters") or {}).get("base") or {}).get("slide") or {}).get("elements"))
        if a.url and not blank:
            if not a.discard:
                sys.exit(f"The in-app automation deck isn't blank ({os.path.basename(deck.get('path') or '')}, "
                         f"{len(deck['slides'])} slides). Save anything you need from it, then run again with --discard.")
        started = True
        if a.url:
            print(f"Testing the in-app server at {a.url}")
            if a.discard:
                server.tool("deck_new", expected_version=server.version, template="blank", discard_unsaved=True)
        run(server, a.workspace, a.keep)
    finally:
        if a.url and started:
            # Leave the in-app server with a blank deck, not this unsaveable throwaway one
            server.inspect()
            server.tool("deck_new", expected_version=server.version, template="blank", discard_unsaved=True)
        server.close()
        if not a.keep:
            for p in paths:
                if os.path.exists(p):
                    os.remove(p)
    offline_scripts(a.workspace)
    failed = results.count(False)
    print(f"\n{len(results) - failed}/{len(results)} checks passed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
