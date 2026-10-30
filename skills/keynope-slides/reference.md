# keynope-slides reference

Recipes and details that SKILL.md points to. Read only the part you need; section numbers refer to SKILL.md.

## Recipes

**Boxed text with a tag (prompt/command boxes):** four elements, added in this order so layering works: content text (`top=T+3`, `left=L+3`, `width=W-6`), tag text (`top=T`, `left=L+2`, size 24, dark colour), tag shape (`top=T`, `left=L`, `height=2`, `width`=tag chars × 0.533 × 24 / 7.84 + 3, `shape-offset-y=-0.3`, filled accent, stroke 0), box shape (`top=T`, `left=L`, `width=W`, `height`=3 + lines × rows-per-line + 1, dark fill, 2 px accent stroke).

**Reading panel (calm text over an animated background):** keep the heading on the background and put one dark square behind everything below it. In the batch: `select-slide`, `update-slide` with `"effect":"none"` **and** the slide's `background` (both with `*Set`), `add-element` shape, then `update-element 0` (needs nothing else at top 0) with `shape=square&top=11&left=8&width=229&height=H&fg=<slide bg>&shape-stroke=<dim tint, e.g. #2a3654 on navy>&shape-stroke-width=2&modern-opacity=0.92&shape-offset-y=0.5`. Fit it to the content: top just under the heading or its rule (here row 11.5), left/right 4 cols outside the content (content 12–233 → 8–237), bottom = lowest content + 1.2–1.8 rows. New shapes go behind everything, so the panel lands behind all content; it hides the background except around the edges. Measure first and check after with `scene_bounds.py` (SKILL.md section 9).

**More or bigger fireworks (looping GIF overlay):** `<venv>/bin/python -I scripts/fireworks_gif.py <workspace>/fireworks.gif --avoid C0,R0,C1,R1 --sheet <scratch>/sheet.png` (needs Pillow). The defaults are 36 rockets per 10 s, bursts 2–3× the built-in size, 640×354 within the GIF limit, a transparent background and a seamless loop; `--avoid` keeps most bursts off a title, and the sheet shows 9 frames. `select-slide`, `asset_import` it, then apply the geometry the script prints (full width, y ≈ 0.9) plus `image.alpha` 100. It lands behind all content: lower the opacity of any card over the slide (`paint.opacity` ~0.4) so the bursts show. Leaving the built-in `fireworks` effect on adds sparks underneath. The same approach works for any custom looping animation.

**Pixel blocks / badges / cards:** texts first, then one filled or stroked square per block. For a strip of blocks with dark text on a bright fill, use `shape-offset-y=-0.4` to centre a 3-row block on 28 px text.

**Pixel-art logo from an image:** `<venv>/bin/python -I scripts/pixelart.py name image.png COLS MODE THRESHOLD [#inkcolour]` (needs Pillow; MODE alpha|dark|nonwhite; render SVGs to PNG first, e.g. with headless Chrome). It writes `name-grid.txt` and `name-runs.json` and prints a preview. Add a mono text element with the plain blocks (`modern-line-height=0.76&modern-width=105`, NBSP leads), then `set-scene-text` the runs. Single-colour art needs no runs: use the plain text with `fg`. Resize later with `text.size`. 12–15 columns keeps a simple icon recognisable; line drawings need ~30.

**ASCII diagrams:** draw on a char grid, one mono element per colour **and per build step**, all at the same top/left/size/`modern-line-height=1.15`. Each layer is just its own characters: blank leading lines as `\n`, one NBSP to start each indented line. `scripts/ascii_layers.py` makes layers from rectangles of a drawn diagram.

**Tables:** `tableData` (column widths and row heights are relative weights; the real size comes from the geometry):
```json
{"version":1,"rows":[{"height":3},{"height":5}],"columns":[{"width":150},{"width":790}],
 "cells":[[{},{"text":"Header","runs":[{"text":"Header","bold":true,"color":"#ffcc00"}]}],
          [{"text":"Label"},{"text":"Mixed cell…","runs":[{"text":"Mixed cell","color":"#55ffff"},{"text":"…","fontId":"mono"}]}]],
 "style":{"header":true,"banded":false,"headerFill":"#1a2233","cell":{"fill":"#101828","color":"#ffffff","borderColor":"#334155","borderWidth":1,"fontId":"c64","fontSize":28,"fontWidth":160,"lineHeight":1.25,"align":"left","vertical":"middle","padding":12}}}
```
- `fontWidth` is the same percent as `modern-width`. Row height in rows = (lines × size × 1.25 + 2 × padding) / 19.29; `fit.py rows` computes it per row from column weights. Shadows don't apply in cells.
- To change an existing table, read its tableData from the saved deck (`list_ids.py --tables`), edit and `set-table` it back.

**QR code:** `python3 -m venv <scratch>/venv && <scratch>/venv/bin/pip install segno`, then `segno.make(url, error='m').save(path, kind='svg', scale=10, border=4, dark='#000000', light='#ffffff')` into the workspace, and `asset_import` it.

**Screenshots of web pages:** `"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --screenshot=<file> --window-size=1600,1000 <url>` (writes nothing without an absolute path; there is no `timeout` command on macOS). Pages that animate in on scroll show only the hero: screenshot the page's own product image URL with `--window-size` equal to the image size instead.

## Animated GIFs

import with `asset_import` and loop in the HTML player (the asset list shows `frames`); GIF transparency works, so lower layers and the slide background show through. Limits: **32 megapixels summed over all frames** (W × H × frames; "animation exceeds the decoded frame limit") and 64 MiB encoded. A 125-frame loop fits at 640×354, scaled up to the full slide; chunky pixel art survives the scaling. The deck stores decoded PNG frames: a 0.6 MB GIF added ~2.2 MB to the `.md` and ~0.9 MB to the HTML. With the OS "reduce motion" setting on, the HTML player shows the first frame.

## Deeper checks

- How an effect or limit really works: the player's JavaScript is inside the exported HTML (search it, e.g. `function makeBursts` for fireworks; the player ticks every 70 ms). Engine error messages and limits are strings in `/Applications/Keynope.app/Contents/Helpers/keynope-engine`; search the bytes with Python, since `strings` returns one huge blob.
- Per-element scale without screenshots: in the exported HTML, regex `"fontId":"(\w+)","widthScale":([\d.]+),"runs":\[\{"text":"(.{0,14})`.
- Glyph coverage: in the page's JavaScript, draw on a canvas with `ctx.font='100px KeynopeC64'` vs a fallback font; a char whose width equals the fallback's is missing (C64 glyphs are all 80 at 100 px).

## Full command list

Full command list: add-element, add-layout, add-slide, clone-slide, convert-selected-text-kind, convert-text-kind, copy-object-style, delete-element, delete-layout, delete-objects, delete-selection, delete-slide, detach-deck-theme, duplicate-element, enter-group, exit-group, group-elements, insert-chart, insert-table, move-element, move-object-layer, navigate-build, navigate-presentation, next-slide, paste-elements, paste-object-style, paste-objects, previous-slide, promote-image-background, redo, register-typeface, remove-engagement, rename-master, reorder-master, reorder-slide, reorder-slide-tab, replace-typefaces, reset-object-style, restore-background-image, select-element, select-elements, select-slide, set-activity-qr, set-chart-data, set-chart-options, set-chart-series-style, set-deck-theme, set-engagement, set-engagement-result, set-layout, set-modern-text-style, set-object-lock, set-page-size, set-scene-crop, set-scene-text, set-selection-geometry, set-selection-property, set-shape-adjustment, set-slide-build, set-table, set-tabs, set-text-size, set-text-width, set-theme, start-timer, stop-timer, toggle-master-mode, toggle-page-number, toggle-slide-tab, undo, ungroup-elements, update-element, update-elements, update-layout, update-slide, update-slide-notes. There is **no** `duplicate-slide`.
