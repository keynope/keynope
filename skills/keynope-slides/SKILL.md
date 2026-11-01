---
name: keynope-slides
description: Build, design or edit slide decks with Keynope's MCP server (deck_*, editor_*, asset_import tools). Load before the first Keynope tool call. Gives the exact command JSON, query keys, font widths, glyph coverage, effect/transition/build names, a low-token workflow, helper scripts and known gotchas, so you don't need deck_schema, slide_inspect or trial and error.
---

# Building slides with the Keynope MCP

This skill covers how to drive Keynope's MCP server cheaply. Everything below was checked with Keynope 1.8.8 (Mac App Store build) against the saved Markdown and the HTML export; `scripts/selftest.py` re-checks it after a Keynope update. Script paths are relative to this skill's folder.

**Before you start**
- **Workspace:** the server can only read and write its automation workspace (the folder shown in Keynope's Automation (MCP) window). Copy images in before `asset_import`; finished decks stay there unless you copy them out.
- **Screenshots:** in the Mac App Store build (1.8.6–1.8.8), `slide_screenshot` fails with "render slide: signal: trace/BPT trap" and macOS shows a "Keynope quit unexpectedly" dialog. Preview the HTML export instead (section 9). If it works in your build, use it at a small width.
- **The deck in the editor window is never shared** with the server; the server has its own automation deck.
- **Tool names:** in Claude Code the tools are `mcp__keynope__<tool>` (e.g. `mcp__keynope__editor_batch`) when the server is registered as `keynope`. If your client loads them on demand, search for those full names; `deck_inspect` alone finds nothing.
- **`expected_version` on every write:** every tool that changes or saves the deck (`editor_batch`, `editor_command`, `deck_save`, `deck_export`, `deck_open`, `deck_new`, `asset_import`, and the font and video imports) needs `expected_version`, the `version` from the last result (start with `deck_inspect`). Without it the call fails with `missing properties: ["expected_version"]`.
- **Select the slide first:** commands that target objects by ID (`set-table`, `set-scene-text`, `set-selection-property`, `set-selection-geometry`, `set-slide-build`) only find objects on the current slide. Start each slide's part of a batch with `{"action":"select-slide","slide":N}`; otherwise the batch stops with "selected object no longer exists".
- **Scripts:** every helper script except `selftest.py` (which runs the test) prints its usage when run without arguments. Run them; there's no need to read their source.

## 1. Token budget (read first)

In one 34-slide rebuild, about 45 Keynope calls returned ~9k each (~400k tokens of results) before any screenshots. Budget **tool calls**, not commands.

| What | Cost | Do instead |
|---|---|---|
| **Every** editor_batch / editor_command / deck_save / deck_export / deck_open result | ~1.5k + ~220 chars per slide + the base master's elements verbatim. A run-coloured logo on the master adds ~2.5k to every result. 34 slides ≈ 9k per call | Max 64 commands per batch, as few calls as possible: aim for ~1 call per 2–4 slides of content plus one pass for runs/builds. Never `editor_command` for a single command when it can ride in a batch. |
| Result with an object selected | +6–8k | End **every** batch with `{"action":"select-slide","slide":N}`. add-element, update-element, insert-table, select-elements leave a selection. |
| `asset_import` | ~9k each (selection blob, unavoidable) | Import each image once; set its geometry in the next content batch. |
| `deck_schema` topic `action` | ~30k | Never. Field names are below. `commands` (~1.5k) lists names only; `slide` (~2k) lists slideData fields. |
| `slide_inspect` | 6–9k | `deck_save` + `list_ids.py` (section 2). |
| A failed command | ~50 tokens (short error, batch stops, earlier commands stay applied) | Probing an unknown field is cheap; a probe that *succeeds* returns the full summary. |
| Browser screenshots of the HTML export | ~1k each at 800×450 | One pass per 4–6 slides; read text from the page instead when you only need the words. |
| Generating command JSON with a script and pasting it back | input + output | Hand-write plain text commands. Generate only pixel art, ASCII diagrams and large tables. |
| Interactive probing of missing features (layouts, layer order) | 5–10 calls | Use the workarounds in section 3. |

**Plan briefly before the first write:** slide order, colours, roughly where each element goes, and which elements get runs and builds. Don't work out every coordinate in your head: check text that may wrap with `fit.py`, place everything, then measure with `scene_bounds.py` (section 9) and fix in one batch. Then: structure batch → content batches → one save + `list_ids.py` → one or two run/build/table batches → one preview sweep → fixes → finish.

**Versions and resets:** start with `deck_inspect`; on "stale deck revision" inspect again. Every command bumps the version by 1, and a failed batch keeps the commands before the failure (the error says how many applied), so recompute `expected_version` (old + applied) instead of re-inspecting. A failed `asset_import` leaves the version unchanged. A server started by the client (stdio) begins at version 1 with an empty Untitled deck each time the client restarts it; the in-app server keeps its deck while Keynope stays open.

## 2. Workflow

1. `deck_inspect`, then `deck_open` (`discard_unsaved: true` only for an Untitled scratch deck). To work on a copy, `deck_save` it to the new name first; the original file is never touched after that.
2. Old element IDs: `deck_save`, then `python3 scripts/list_ids.py "<workspace>/deck.md" [slide ...]` (0-based slides; prints id, top/left, width, size, font, first 40 chars).
3. **Structure batch:** add/reorder/delete slides, `update-slide` bg/fg/effect/background for every slide, master elements (section 3).
4. **Content batches (2–4 slides each):** `delete-objects` the old elements, add texts with their plain text, then shapes, `insert-table` placeholders, `update-slide-notes`. Rebuilding a slide this way is usually cheaper and cleaner than patching element by element. Keep each slide's title text the **first element by top**: a frame or shape above it, even a full-slide border at row 1–2, becomes the slide's name in Keynope's slide list ("[shape:rounded]"). See Navigator titles (section 7).
5. **One** `deck_save` + `list_ids.py` for all slides to get the new IDs.
6. **Pass B batches:** `set-scene-text` runs (colour/bold/mono glyphs), `set-table` + geometry, `set-slide-build`, `asset_import` + geometry.
7. Export, preview in one sweep (section 9), fix, then finish (section 8).

IDs can't be chosen in advance (`elementData.id`, `element-id=` are ignored). A single add returns its ID in `selection.targetIds[0]` but with the selection blob.

## 3. Command cookbook (verified JSON)

The full command list is in `reference.md`; the commands you need are below. There is **no** `duplicate-slide`.

**Slides**
- `{"action":"add-slide"}`: blank slide after the current one, made current (layout "", so no master elements).
- `{"action":"clone-slide"}`: copies the **current** slide (elements, runs, notes, bg, effect, layout) right after it and selects the copy. Cheapest way to make a slide that looks like an existing one (e.g. a closing slide from the title slide): clone, `reorder-slide`, then change texts by their new IDs.
- `{"action":"reorder-slide","slide":30,"value":2}`: moves slide 30 to index 2.
- `delete-slide` deletes the **current** slide (`slide` is ignored): `select-slide` first.
- `{"action":"update-slide","slide":2,"slideData":{"bg":"#070a14","bgSet":true,"fg":"#ffffff","fgSet":true,"effect":"warp","effectSet":true,"background":"constellation","backgroundSet":true}}`. It ignores `elements` and `layoutId`, and it also ignores `slide`: it changes the **current** slide. Put `{"action":"select-slide","slide":N}` before every update-slide (two commands per slide, so 32 slides per batch). Without it, every update lands on the same slide. Sending `effect` without `background` **clears the background**: whenever you change either, send both, each with its `*Set` flag.
- `{"action":"update-slide-notes","slide":3,"notes":"…"}` (plain text, `\n\n` for paragraphs, `""` clears).

**Master and layouts**
- `{"action":"toggle-master-mode"}`, then add-element / set-scene-text work on the base master, then toggle back. Master elements don't appear in `list_ids.py`; their IDs are in every result under `masters.base.slide.elements`.
- Master elements render **only on slides whose layout is `blank`**. A new deck's first slide already has it; `add-slide` gives layout "". No MCP command sets a layout: `set-layout` rejects every field tried (`value` as a string or a number, `name`, `layout`, `layoutId`, `id`, `index`) and `update-slide` ignores `layoutId`. Workaround: `deck_save`, run `python3 scripts/set_layout.py "<workspace>/deck.md" blank --except 0 33` (sets or removes the `<!-- layout=blank -->` line of each slide), then `deck_open` the file again (undo history resets). Slides left without it don't show the master (useful for a title slide).
- The master's text is echoed in every result, so keep it to what must repeat (e.g. one logo).

**Elements**
- Text (`kind` at top level **and** in elementData): `{"action":"add-element","kind":"text","elementData":{"kind":"text","text":"Hello","query":"render=truetype&modern-font=c64&modern-width=160&ttf-size=40&fg=#ffffff&top=16&left=18&width=214"}}`
- `kind:"heading"` ignores elementData and creates a default "# Title" at top 0. Don't use headings; use text.
- Shape: add, then `update-element 0` (the new shape sits at top 0, index 0 sorted by top; only safe when nothing else is at top 0):
  `{"action":"add-element","kind":"shape","name":"square"}`
  `{"action":"update-element","element":0,"elementData":{"kind":"shape","text":"[shape:square]","query":"shape=square&top=25&left=15&width=214&height=13&fg=#202833&shape-stroke=#8b9ab5&shape-stroke-width=2"}}`
- Code box: `{"action":"add-element","kind":"code"}` then `update-element 0` with `{"kind":"code","text":"…","query":"render=truetype&modern-font=mono&ttf-size=40&fg=#ffffff&top=27&left=19&width=206"}`. For full control of font and padding, prefer the boxed-text recipe in `reference.md`.
- Delete: `{"action":"delete-objects","slide":1,"objectIds":["text-…","shape-…"]}`.
- Replace text by ID (plain or styled): `{"action":"set-scene-text","objectId":"text-…","textRuns":[{"text":"run "},{"text":"npm test","color":"#55ffff"},{"text":" now","bold":true},{"text":" →","fontId":"mono"}]}`. The concatenation **replaces** the text; one plain run just changes the text. Runs can contain `\n`. Markdown and `[color=…]` markup in add-element text stay literal; runs can't be set at creation, and set-scene-text without `objectId` fails ("invalid editor action") instead of using the selection, so new elements need a save + `list_ids.py` before their runs.
- Properties: `{"action":"set-selection-property","objectIds":["a","b","c"],"property":"paint.shadow","propertyValue":"none"}`. Many IDs per call work, but **all must be on the current slide** ("selected object no longer exists" otherwise). Verified keys: text.size, text.align (`"center"`), text.lineHeight, paint.color, paint.fill (`"none"` makes a shape hollow), paint.opacity, paint.strokeColor, paint.strokeWidth, paint.shadow (none/soft/solid), paint.shadowColor, paint.shadowX/Y, paint.gradientStart/End/Direction, paint.glowRadius, paint.glowColour, image.alpha (imports default to 96; set 100).
- Geometry (grid units, fractions OK): `{"action":"set-selection-geometry","objectIds":["…"],"geometry":{"x":12,"y":25.5,"width":221,"height":28.5},"geometryResize":true}`. **One objectId per call.** All four fields are required; without `geometryResize` only x/y are used. For text, geometryResize sets the wrap width. The box must stay on the slide (x + width ≤ 245, y + height ≤ 56), else the batch stops with "keep the object box within the slide".
- Copy elements to another slide (keeps runs): `{"action":"select-elements","objectIds":["text-…"]}` on the source slide, call the `selection_copy` tool (returns a compact bundle), then in a batch `select-slide` target and `{"action":"paste-objects","clipboard":<bundle>}`. Same position.
- Layer order: new shapes **and imported images** go behind everything already on the slide; texts go on top. `move-object-layer` field names are unknown (objectIds+name/kind → invalid). So add texts first, then shapes; and add a filled frame **after** an image, or give it `paint.fill` `"none"`, or it hides the image. By the tie rule in section 7, a new shape or image left at y 0 comes first in array order and can take the slide's navigator title from a top-0 text: move it down (even y 0.9 is enough).

**Tables**
- `{"action":"insert-table","cols":3,"rows":5}` lands at x24 y10; get its ID from `list_ids.py`. Then, after `select-slide` for that slide, `{"action":"set-table","objectId":"table-…","tableData":{…}}` (`reference.md`) and geometry with `geometryResize`.
- Builds can only target whole tables. Row-by-row reveal: one 1-row table per row, stacked (header as its own table). Column reveal: side-by-side tables with identical row weights and the same total height.

**Builds and transitions**
- `select-slide` that slide **first** ("the slide changed; reopen Builds before applying"), then `{"action":"set-slide-build","slide":5,"buildData":{"version":1,"transition":{"effect":"cut"},"steps":[{"targetIds":["shape-…","text-…"],"effect":"wipe","direction":"right","durationMs":500},{"targetIds":["text-…"],"effect":"fade","direction":"up","durationMs":400,"advance":"after","delayMs":200}]}}`.
- No `advance` = waits for a click; `"advance":"after"` + `delayMs` runs automatically (on the first step: right after the slide appears). Effects: appear, fade, wipe, zoom; directions left/right/up/down. Clear builds: `"steps":[]`.

**Images**
- Copy the file into the workspace, `select-slide`, then `asset_import {"path":"x.png"}`; the ID is in `selection.targetIds[0]`. The asset list shows a 384-px preview, but the full-resolution source is kept and the HTML export is sharp. SVGs are rasterised to 384×384, so import PNGs for photos and screenshots.
- Then geometry with the image's aspect ratio (height rows = width cols × 7.84 / aspect / 19.29) and `image.alpha` 100.
- Animated GIFs (limits, transparency, file size): see `reference.md`.

## 4. Query keys (set everything at creation)

- Text: `render=truetype`, `modern-font=c64|sans|mono`, `ttf-size=<px>`, `modern-width=<percent>`, `fg=#hex`, `top`, `left` (integers), `width` (wraps; cols), `align=center` (works with `width`; if a big title still isn't centred, set `text.align` afterwards), `modern-line-height=1.15`, `modern-opacity=0.8`.
- `gradient-start=#…&gradient-end=#…&gradient-dir=horizontal|vertical|diagonal|radial` (works on c64 text).
- `shadow=solid&shadow-color=#000000&shadow-x=2&shadow-y=1`: offsets are **grid cells** (x 2 = 15.7 px, y 1 = 19.3 px). Fine on 100 px titles; on 30–44 px body text it reads as doubled text, so keep shadows on titles only.
- `fx-glow-radius=18&fx-glow-colour=%23hex` (`#` as `%23` in fx keys only).
- Shape: `shape=square|rounded|pill|circle|triangle|diamond|hexagon|arrow-right|chevron|star|burst|cloud|database|document|callout|speech-bubble|brace|label|…`, `top,left,width,height`, `fg=#fill`, `shape-fill=none`, `shape-stroke=#hex`, `shape-stroke-width=2` (0 = none), `modern-opacity`, `shape-offset-y=-0.3`. Keep `top`/`left`/`width`/`height` in a query whole numbers: a fractional `top` is accepted, but then `deck_save` fails with `invalid numeric placement field "top"` until the element is fixed. For half rows use `shape-offset-y` or `set-selection-geometry` (fractions are fine there).

## 5. Names

- Slide `effect`: matrix, stars, plasma, glitch, digital-snow, radar, neural, circuit, data-storm, flame, warp, scanline, fireworks, explosion.
  - How they look in the HTML player (all drawn on the character grid): calm enough behind body text: circuit, neural, warp, scanline, plasma, radar (a sweeping line), matrix (rain near the top). Noisy over text: data-storm (floating code snippets), digital-snow and glitch (rows of random characters). flame fills roughly the bottom half with tall flames, great on a title-only slide with the text kept above row ~26, unreadable behind text. fireworks and explosion are single-character sparks and barely show.
  - Effects have **no settings** (frequency, size, speed, intensity): the player reads only the name and the slide index (as a random seed). fireworks is ~20 rockets per ~17 s loop with bursts ~14 cols wide. For more and bigger fireworks, overlay a GIF (`reference.md`).
  - Effects and backgrounds draw under every element, so any semi-opaque card or panel hides them. On slides people read for minutes (instructions, exercises), even the calm effects distract: use the reading panel (`reference.md`).
- Slide `background`: soft-plasma, aurora, topography, waves, mesh, constellation, ribbons, diagonal-flow, blueprint. Effects and backgrounds combine. `deck_validate` reports `background-counterpart` on animated backgrounds; it's informational.
- Transitions: cut, fade, push, wipe, dip-black, dip-white, tv-switch.
- Fonts: `c64` (KeynopeC64), `sans` (Go, KeynopeModern), `mono` (Go Mono, KeynopeModernMono).

## 6. Geometry, widths and type

- Canvas 1920×1080 = grid 245×56. **1 col = 7.84 px, 1 row = 19.29 px.** `ttf-size` = px.
- Line box = size × lineHeight (text 1.25, code 1.3). Rows per line = size × lh / 19.29 (40 px → 2.6 rows, 32 px → 2.07, 28 px → 1.81).
- **Width scaling:** the theme's `fonts.body.width` scales **every** text element (c64, sans **and mono**); keep theme widths at 100 and set `modern-width` per element.
  - c64 char width = **0.333 × size** (width 100), **0.40 × size** (120), **0.533 × size** (160).
  - mono/sans scale = width/100; mono char = 0.6 × size. Mono/sans runs inside a c64 element are squeezed by that element's scale (arrows → come out thin).
- Chars per line = width_cols × 7.84 / char_px. `fit.py` does this with word wrap; budget one extra line for URLs. Usage: `printf '40|214|160|A title\n30|214|-100|A sans line\n' | python3 scripts/fit.py` (size|width in cols|modern-width, -100 for sans or mono|text) prints the lines and rows for each text.
- c64 metrics: ascent 0.8, descent 0.2; with line height 1.25 the caps start 0.125 em below the top.
- **C64 glyph coverage:** full printable ASCII. **Missing:** • · € → … ⋯ ✓ ’ “ ” é — – « » ■ □ ▸ ✗ × ². Put those in runs with `fontId:"mono"`, or use a separate sans element. Use straight quotes.
- sans/mono have ▸ ✓ √ ■ □ ● → ← ↓ ▲ ▼ ░▒▓█ ▆ € and single/double box drawing (┌─┐│└┘├┤┬┴┼ ═║╔╗╚╝╠╣╦╩). Avoid ▶ (colour emoji), heavy and dashed box chars.
- Go Mono █ is 0.6 × 1.156 em; **▆ (U+2586) is 0.6 × 0.765 em**: with `modern-line-height=0.76` rows touch exactly (block aspect 0.82 w/h at `modern-width=105`).
- **Leading spaces are stripped per line.** Start each indented line with one NBSP (` `); the spaces after it can be regular spaces. Interior spaces are kept.
- Bullets: one sans "•" element per bullet at the same top as its text. A single "•\n\n•" element only lines up if every text top is a whole number of line pitches apart, which integer tops rarely are.

## 7. Recipes

**Navigator titles:** the slide title = the **first element by top** (ties follow array order). A shape first shows "[shape:square]"; a run-coloured element shows `[color=…]` markup. Keep a run-free text element alone at the smallest top. When the visible title has runs or sits below a frame, add a run-free copy in the background colour (size 20) at top 0.

More recipes are in `reference.md`; read the one you need: boxed text with a tag (prompt/command boxes), the reading panel (calm text over an animated background), more or bigger fireworks (GIF overlay), pixel blocks/badges/cards, pixel-art logos, ASCII diagrams, tables (`tableData`), QR codes and screenshots of web pages.

## 8. Finish

1. `deck_validate` (the background-counterpart notes are informational).
2. `deck_save` to the final name (`overwrite:true` only for your own file or with the user's OK).
3. `deck_export` HTML (numeric `expected_version`); copy the `.md` and `.html` out of the workspace if the user wants them elsewhere, and delete your own scratch files. If the client blocks the copy (Claude Code only touches files inside the project and folders added to it), don't try other commands: tell the user where the files are and that they can allow the workspace folder (`--add-dir`, or `permissions.additionalDirectories` in Claude Code's settings).
4. Stop any preview server you started.
5. Don't quit Keynope: the user may have decks open in the editor, and the in-app server stops with the app. A stdio server ends when the client disconnects.

## 9. Preview and measuring

- `deck_export` HTML to the workspace and open it in a browser. If the browser can't open files in that folder, serve it: `python3 -m http.server 8765 --bind 127.0.0.1 --directory "<workspace>"`, then http://localhost:8765/preview.html.
- With a browser tool: a 1280×720 viewport, then one batch per 4–6 slides: Right, wait, screenshot at half size.
- In the player, Right first works through a slide's builds: one press per click step, and a press during an automatic build completes it. Presses per slide = 1 + click steps; wait 3–6 s on slides with automatic builds before the screenshot.
- Real bounds without screenshots: `python3 scripts/scene_bounds.py "<workspace>/preview.html" [slide ...]` (0-based) prints every object's rows/cols, opacity, fill and stroke from the export's embedded scene JSON (`<script id="keynope-data">`, `pages[i].scene.objects[]`, bounds in px). `--panel 0.92` checks a reading panel: content bottom, gap, and anything behind it or outside it.
- Effect internals, per-element text scale and glyph coverage checks: see `reference.md`.
