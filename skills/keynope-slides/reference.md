# keynope-slides reference

Recipes contributed by Josh Gonggryp, updated for native MCP authoring. Read only the part you need. All recipes use the built-in tools; no helper scripts are required.

Native equivalents: `text_measure` and `table_measure` for sizing; `slide_measure` with `panel_id` for reading-panel checks; `deck_objects` for IDs/table data; `diagram_layers` for ASCII layer masks; `image_pixelart` for editable coloured blocks; `animation_create` for transparent fireworks. These do not require a save/parse/reopen cycle or Python dependencies. Use explicit `move-object-layer` with `objectId` and `kind` to order objects.

## Recipes

**Boxed text with a tag (prompt/command boxes):** four elements, added in this order so layering works: content text (`top=T+3`, `left=L+3`, `width=W-6`), tag text (`top=T`, `left=L+2`, size 24, dark colour), tag shape (`top=T`, `left=L`, `height=2`, `width`=tag chars × 0.533 × 24 / 7.84 + 3, `shape-offset-y=-0.3`, filled accent, stroke 0), box shape (`top=T`, `left=L`, `width=W`, `height`=3 + lines × rows-per-line + 1, dark fill, 2 px accent stroke).

**Reading panel (calm text over an animated background):** keep the heading on the background and put one dark square behind the body. Preserve both the slide's effect and background settings when updating its appearance. Insert the shape, use its returned ID and `deck_objects` index to update it with `shape=square&top=11&left=8&width=229&height=H&fg=<slide bg>&shape-stroke=%232a3654&shape-stroke-width=2&modern-opacity=0.92&shape-offset-y=0.5`, then explicitly `move-object-layer` with `objectId=<panel-id>, kind=back`. Fit it to the content: top just under the heading, left/right about 4 columns outside the content, bottom about 1.2–1.8 rows below it. Check `slide_measure` with `panel_id` for content outside or behind the panel, then inspect a screenshot. Do not assume a new shape always has index 0 or is at the back.

**More or bigger fireworks (looping GIF overlay):** call `animation_create` with `path:"fireworks.gif"`, the current `expected_version`, and an `animation` object. Set width/height, seconds, rockets, burst_min/max, seed and an optional `avoid:{x,y,width,height}` rectangle in grid cells to keep bursts off a title. The result reports dimensions, frames and duration. `select-slide`, `asset_import` the generated GIF, then set its geometry and `image.alpha` to 100. Explicitly move it behind content with `move-object-layer`. Lower the opacity of overlaid cards (`paint.opacity` ~0.4) if bursts should show through. Leaving the built-in `fireworks` effect on adds sparks underneath.

**Pixel blocks / badges / cards:** use one filled or stroked square per block and explicit layer order to keep text above the fill. For a strip of blocks with dark text on a bright fill, use `shape-offset-y=-0.4` to centre a 3-row block on 28 px text.

**Pixel-art logo from an image:** call `image_pixelart` with a workspace `path`, `columns`, `mode` (alpha|dark|nonwhite), `threshold` and an optional single `colour`. Add the returned mono text element, then apply its runs with `set-scene-text`. Resize with `text.size`. Around 12–15 columns can suit simple icons; line drawings often need 30 or more. This recipe's `▆` glyph is not in bundled Go Mono and uses a system fallback: for font-independent output use native Retro image conversion with `glyph=blocks`. Test colour contrast: dark source ink may disappear on a dark slide, so choose a light single-colour tint where needed. Replace existing query keys rather than concatenating duplicates.

**ASCII diagrams:** draw on a char grid, one mono element per colour **and per build step**, all at the same top/left/size/`modern-line-height=1.15`. Call `diagram_layers` with `text` and named `layers`, each containing inclusive `rectangles:{row_start,row_end,column_start,column_end}`. Its returned text preserves alignment with NBSP and blank leading lines. Insert each layer separately and use its returned ID for styling and builds.

**Tables:** `tableData` (column widths and row heights are relative weights; the real size comes from the geometry):
```json
{"version":1,"rows":[{"height":3},{"height":5}],"columns":[{"width":150},{"width":790}],
 "cells":[[{},{"text":"Header","runs":[{"text":"Header","bold":true,"color":"#ffcc00"}]}],
          [{"text":"Label"},{"text":"Mixed cell…","runs":[{"text":"Mixed cell","color":"#55ffff"},{"text":"…","fontId":"mono"}]}]],
 "style":{"header":true,"banded":false,"headerFill":"#1a2233","cell":{"fill":"#101828","color":"#ffffff","borderColor":"#334155","borderWidth":1,"fontId":"c64","fontSize":28,"fontWidth":160,"lineHeight":1.25,"align":"left","vertical":"middle","padding":12}}}
```
- `fontWidth` is the same percent as `modern-width`. Call `table_measure` with table data and `box_width` in grid cells to calculate minimum row heights using the actual font metrics, padding, wrapping and spans. Shadows don't apply in cells.
- To change an existing table, read it with `deck_objects` and `include_data:true`, edit and `set-table` it back. Use the measured row heights as row weights, and their total for the table height.

**QR code:** call `qr_code_create` with `qr_code:{"content":"https://example.com","correction":"M"}` and `expected_version`. Keynope generates and embeds the vector artwork locally, with a four-module quiet zone and a square physical aspect. Optional foreground/background colours must be contrasting dark-on-light hex colours. No Segno or external file is required. Use the returned image ID to reposition it. When resizing, preserve its physical aspect: on the default 245×56 grid and 1920×1080 canvas, height in rows = width in columns × (1920/245)/(1080/56). Do not use equal grid width/height. Decode the actual rendered QR to verify it.

**Screenshots of web pages:** `"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --screenshot=<file> --window-size=1600,1000 <url>` (writes nothing without an absolute path; there is no `timeout` command on macOS). Pages that animate in on scroll show only the hero: screenshot the page's own product image URL with `--window-size` equal to the image size instead.

## Animated GIFs

Import with `asset_import` and loop in the HTML player (the asset list shows `frames`); GIF transparency works, so lower layers and the slide background show through. Limits include **32 megapixels summed over all frames** (W × H × frames) and the image-import/embedded-storage byte budgets (currently 50 MiB). A 125-frame loop fits at 640×354, scaled up to the full slide; chunky pixel art survives scaling. The deck stores decoded PNG frames, so encoded GIF size alone does not predict deck size. With the OS "reduce motion" setting on, the HTML player shows the first frame. Snapshot exports deliberately capture a fixed frame; use live playback to test motion.

## Deeper checks

- Use `deck_schema` for supported properties and limits, and preserve native validation errors when reporting a failed command.
- Use `slide_inspect` for resolved object styling and `slide_measure` for actual bounds, rather than parsing the exported HTML.
- Use `text_measure.missing_glyphs` to check font coverage. System fallback can make unsupported characters look different across devices; inspect the rendered result as well.

## Full command list

Full command list: add-element, add-layout, add-slide, clone-slide, convert-selected-text-kind, convert-text-kind, copy-object-style, delete-element, delete-layout, delete-objects, delete-selection, delete-slide, detach-deck-theme, duplicate-element, enter-group, exit-group, group-elements, insert-chart, insert-qr-code, insert-table, move-element, move-object-layer, navigate-build, navigate-presentation, next-slide, paste-elements, paste-object-style, paste-objects, previous-slide, promote-image-background, redo, register-typeface, remove-engagement, rename-master, reorder-master, reorder-slide, reorder-slide-tab, replace-typefaces, reset-object-style, restore-background-image, select-element, select-elements, select-slide, set-activity-qr, set-chart-data, set-chart-options, set-chart-series-style, set-deck-theme, set-engagement, set-engagement-result, set-layout, set-modern-text-style, set-object-lock, set-page-size, set-scene-crop, set-scene-text, set-selection-geometry, set-selection-property, set-shape-adjustment, set-slide-build, set-table, set-tabs, set-text-size, set-text-width, set-theme, start-timer, stop-timer, toggle-master-mode, toggle-page-number, toggle-slide-tab, undo, ungroup-elements, update-element, update-elements, update-layout, update-slide, update-slide-notes. There is **no** `duplicate-slide`.
