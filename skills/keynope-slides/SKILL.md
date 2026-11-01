---
name: keynope-slides
description: Build, design and edit Keynope decks with its MCP server. Read before the first edit. Includes the native authoring workflow, compact receipts, text and table measurement, layouts, artwork tools and visual review.
---

# Building slides with Keynope

Based on Josh Gonggryp's contributed skill (PR #16), now integrated with native
MCP authoring tools. No helper scripts, Python or Pillow installation is needed.

## 1. Workflow

1. Read `deck_inspect`. The automation deck is **separate from the user's open
   document**. File access stays inside the configured automation workspace.
2. Plan the story, slide order, palette and typography before editing. Use a
   consistent hierarchy, generous margins, short headings and readable body
   text. Use calm reading panels behind instructions on animated backgrounds.
3. Create/open the deck, preserving unsaved work. `discard_unsaved:true` and
   `overwrite:true` require user permission unless replacing your own scratch
   work. To edit a copy, save to the new name before changing content.
4. Batch up to **64 native commands**. Select the target slide before contextual
   edits. Every write requires `expected_version` from the last returned result.
   Do not calculate revisions: no-op commands need not increment them.
5. Mutations return **compact receipts**: version, selection IDs, created,
   changed and removed objects, affected slides. Use created IDs immediately.
   `editor_batch` also returns `applied_count`; on failure earlier changes stay
   applied and its error result includes the current revision and receipt.
   Never blindly replay a partially successful batch.
6. Use `deck_objects` for compact, paginated IDs and metadata, including masters
   with `include_masters:true`, and table/chart payloads with `include_data:true`.
   No save/parse/reopen cycle is needed. `response:"full"` on mutations restores
   the older full-deck result if explicitly needed.
7. Use `text_measure` and `table_measure` before positioning; `slide_measure`
   reports actual scene bounds, layer order, off-slide objects, text overflow
   and optional reading-panel containment. Overlap can be intentional.
8. Inspect **actual rendered screenshots**, not just geometry. Use
   `slide_screenshot` with width 800–1280. Review every slide in a final sweep.
   If a renderer is unavailable, export HTML and inspect in a browser; report
   the limitation rather than claiming a visual check.
9. `deck_validate`, explicitly `deck_save`, and export the requested formats.
   Stop only preview servers you started; never quit the user's Keynope.

The guide is also available through `deck_authoring_guide` (topics workflow,
typography, layouts, artwork, recipes, all), and MCP resources
`keynope://authoring/skill` and `keynope://authoring/recipes`. Client prefixes vary;
Claude Code may expose `mcp__keynope__editor_batch`, for example.

## 2. Commands and compact discovery

For one command's fields and example use
`deck_schema {"topic":"command","command_name":"set-layout"}`. The full
`topic:"action"` schema is intentionally available but large. Properties,
shapes, fonts, templates, activities, tables, charts and builds have separate
schema topics. Read only what the task needs.

Slides and element indices are **zero-based**. Native action geometry uses the
authored grid (normally 245 columns × 56 rows). Measured scene bounds are also
returned in physical pixels (normally 1920 × 1080). Do not mix these units.

```json
{"action":"select-slide","slide":2}
{"action":"add-slide"}
{"action":"clone-slide"}
{"action":"reorder-slide","slide":3,"value":1}
{"action":"update-slide-notes","slide":2,"notes":"Presenter notes"}
```

`add-slide` inserts after the current slide and selects it; `clone-slide` copies
the current slide. `delete-slide` deletes the **current** slide. `update-slide`
also modifies the current slide, not an arbitrary supplied index:

```json
{"action":"update-slide","slideData":{"bg":"#070a14","bgSet":true,"fg":"#ffffff","fgSet":true,"effect":"warp","effectSet":true,"background":"constellation","backgroundSet":true}}
```

Appearance updates are replacement-style: preserve existing settings when
changing just one. In particular, send both effect and background values.

Add text, then use its **returned ID** for rich runs, styling and builds:

```json
{"action":"add-element","kind":"text","elementData":{"kind":"text","text":"Hello","query":"render=truetype&modern-font=c64&modern-size=64&modern-width=120&fg=%23ffffff&top=12&left=18&width=210"}}
{"action":"set-scene-text","objectId":"<returned-id>","textRuns":[{"text":"Hello ","color":"#55ffff"},{"text":"world","bold":true}]}
{"action":"set-selection-property","objectIds":["<id>"],"property":"paint.glowRadius","propertyValue":12}
{"action":"set-selection-geometry","objectIds":["<id>"],"geometry":{"x":12,"y":25.5,"width":221,"height":14},"geometryResize":true}
```

For shape/code insertion, insert then update the selected element using its
index from `deck_objects`. Do not assume index 0 after sorting. Query placement
fields should be whole grid numbers; for fractional placement use
`set-selection-geometry`. That preserves native offset metadata correctly.

## 3. Layouts and layering

Layouts do have a native command. Its field is **kind**, containing the layout
ID (not its display name):

```json
{"action":"select-slide","slide":2}
{"action":"set-layout","kind":"blank"}
```

Batch these pairs for multiple slides. This keeps undo history and unsaved state
without editing saved Markdown or reopening the deck. Discover layout IDs
with `deck_inspect`. `toggle-master-mode` switches to master editing; master 0
is the base, and 1 onward are layouts. Switch back before selecting slides.

Layer ordering takes singular **objectId**:

```json
{"action":"move-object-layer","objectId":"<id>","kind":"front"}
{"action":"move-object-layer","objectId":"<id>","kind":"back"}
{"action":"move-object-layer","objectId":"<id>","kind":"before","name":"<target-id>"}
```

Other directions: forward, backward, after. Group rules remain the same as in
the editor. Use explicit ordering rather than relying on insertion order.

Keep the visible title first by top position where practical: navigator labels
come from slide content. A shape before all text may otherwise become the
navigator label. Avoid hidden duplicate headings unless truly necessary.

## 4. Typography

Use `modern-font=c64|sans|mono` or an imported font ID, `modern-size` in pixels,
and `modern-width` as a percentage. `ttf-size` remains supported. A grid cell is
1920/245 pixels horizontally and 1080/56 vertically on the default canvas;
different page sizes change these ratios.

Do not estimate proportional-font width from character counts. Example:

```json
{"element":{"kind":"text","text":"A heading that might wrap","query":"render=truetype&modern-font=sans&modern-size=64"},"box_width":210,"box_height":8,"slide":0}
```

Call `text_measure`; it returns natural width, wrapped height, resolved font
metrics and whether the proposed height fits. It uses the same native font
metrics, mixed runs, Unicode line breaking and inherited style as the app.
For mixed-font text pass `text_runs` with the same run objects used by
`set-scene-text`; their concatenated text replaces the proposed element's text.
`table_measure` accepts `table` or `object_id` plus `box_width`, and returns
minimum row heights including padding and spanning cells. Apply these as row
weights and use the returned total for the table's height.

Prefer `text-align` for text alignment inside its box; `align` positions the
element. Gradient keys: gradient-start/end/dir. Shadows use grid-cell offsets,
so keep large shadows off small body text. Query strings should be URL encoded.
Gradient directions are `horizontal`, `vertical`, `diagonal`, `radial`,
`reflected`, `diamond`, and `angle` (the UI calls the last one “Angular”).
When changing an existing query, replace its key with `URLSearchParams.set`
rather than appending a duplicate key; duplicate placement/style fields fail
save validation.

C64 supports printable ASCII but lacks many punctuation/Unicode symbols. Put
arrows, accented characters, typographic quotes and bullets into appropriate
sans/mono runs instead of assuming glyph coverage. Native measurement is more
reliable than hard-coded width ratios. Use explicit line breaks deliberately.
Check `text_measure.missing_glyphs`: even Go Mono lacks some symbols, notably
`✓` and the partial block `▆`. Browser/system fallback may display these locally but is not guaranteed
to look the same elsewhere. Use a font that contains the symbol, a Keynope
emoji, or a portable ASCII alternative such as `[x]` for reliable output.

## 5. Artwork

- `qr_code_create`: insert a sharp, embedded vector QR code. Pass
  `qr_code:{"content":"https://example.com","correction":"M"}` and the current
  `expected_version`. Optional `foreground`/`background` use `#RRGGBB`;
  dark-on-light contrast and the four-module quiet border are enforced.
  Correction is L/M/Q/H (M by default). It inserts a centred, physically square
  image on the current slide or master and returns its ID. Move/resize it with
  normal geometry commands, keeping its square aspect and quiet zone. No Segno,
  Python, network generator or temporary SVG is needed. Scan the rendered QR
  at its final display size. The equivalent editor command is
  `{"action":"insert-qr-code","qrCode":{"content":"https://example.com"}}`.
- `asset_import`: native image/GIF/SVG import; created image ID is in the receipt.
  Set geometry and `image.alpha` as needed. Existing Retro conversion, cropping,
  tinting and object effects remain accessible through selection properties.
- `diagram_layers`: pass `text` and optional named layers with inclusive
  row/column rectangles. It returns NBSP-preserving aligned text for separately
  styled or animated layers. No temporary text files are needed.
- `image_pixelart`: pass workspace `path`, `columns`, `mode` (alpha, dark,
  nonwhite), optional `threshold` and `colour`. Returns a colour grid, a text
  element and `text_runs`; add it and apply runs by the returned ID. Uses native
  decoding and resampling. No Pillow required. Dark/nonwhite modes help with
  logos on opaque backgrounds; alpha mode is for transparent artwork.
  The contributor's `▆` text recipe uses a system fallback font; choose native
  Retro image conversion with `glyph=blocks` for font-independent portability.
- `animation_create`: generates a transparent fireworks GIF. Pass `path`,
  `expected_version`, optional `animation` with width/height, seconds, delay_ms,
  rockets, salvo_every, burst_min/max, colours, seed and avoid={x,y,width,height}
  in grid cells. Import the generated GIF normally. A seed makes output
  repeatable; native animation limits apply. Reduce motion can freeze playback.

Slide effects: matrix, stars, plasma, glitch, digital-snow, radar, neural,
circuit, data-storm, flame, warp, scanline, fireworks, explosion. Backgrounds:
soft-plasma, aurora, topography, waves, mesh, constellation, ribbons,
diagonal-flow, blueprint. Transitions: cut, fade, push, wipe, dip-black,
dip-white, tv-switch. Busy effects should not compete with text.

Build example (select the slide first):

```json
{"action":"set-slide-build","buildData":{"version":1,"transition":{"effect":"fade"},"steps":[{"targetIds":["<id>"],"effect":"wipe","direction":"right","durationMs":500},{"targetIds":["<other-id>"],"effect":"fade","advance":"after","delayMs":200,"durationMs":400}]}}
```

Build effects: appear, fade, wipe, zoom. No advance means click; after means
automatic. Screenshot/HTML review must account for builds before judging
missing elements. Detailed panel, table and artwork recipes are in reference.md.
