# Developer guide

Keynope builds a windowed Mac app and a self-contained browser editor. The Mac
app includes a private Go engine; there is no standalone terminal editor or
presenter helper to install.

For the document model, validation limits and export boundaries, see
[Format and export details](formats.md). For contribution guidelines, see
[CONTRIBUTION.md](../CONTRIBUTION.md).

## Build and install

Requirements for the Mac app:

- Go (the required version is declared in `go.mod`).
- macOS 14 or later, with `swiftc` available through Xcode Command Line Tools.

```sh
make build
open bin/Keynope.app
```

The bundle includes its private engine and resources. To install it:

```sh
make install
# Or install for the current user only:
make install APPDIR="$HOME/Applications"
```

Generated app bundles live in `bin/` and are not committed.

Build the browser editor into a temporary site directory:

```sh
sh tools/build_web_editor.sh /tmp/keynope-site/editor
```

The participant viewer source is `web/participant-presentation.js`. The web
build packages it as `join/presentation.js` alongside the generated
`join/slide-renderer.js`. Tests do not require the website infrastructure
directory.

## Tests

Run the Go suite:

```sh
go test ./...
```

With Node and Playwright's Chromium/WebKit engines installed, run the
headless integration batch:

```sh
node tools/test_editor_integration.cjs
```

The batch builds a fresh browser editor, runs the Go suite, generates synthetic
fixtures and checks editing, history, save/reopen and output in both browsers.
Coverage includes themes, tables, charts, shapes, SVG/media, formatting,
selection, builds, dense editing, master thumbnails and document replacement.
It also exports Retro-font and image-effect artwork to PowerPoint and checks
the resulting package's pixels, animation timing and editable diagrams.

Each browser uses isolated fixtures and required output files; missing proof
artifacts fail the batch. The report records source hashes and individual exit
statuses, and detects source changes during the run. Logs, captures and
`report.json` go into a new temporary directory. The default batch neither
deploys anything nor opens the working Mac app.

Useful options:

```sh
node tools/test_editor_integration.cjs --list
node tools/test_editor_integration.cjs --browser=webkit
node tools/test_editor_integration.cjs --case=tables,charts
```

`PLAYWRIGHT_MODULE` can point to an existing Playwright installation, and
`CHROME_PATH` can select a Chromium executable. Dependencies are not installed
automatically.

Optional checks:

- `--schemas=/path/to/transitional-xsd` validates generated packages using
  local official ECMA-376 schemas and `xmllint --nonet`.
- On macOS, `--native` builds the app and runs an isolated background WebView
  acceptance app.

Screenshot approval, foreground Save panels, PDF reader comparisons and external
Office acceptance are separate from this automated batch. Runtime cache
diagnostics are opt-in local counters, not saved deck metadata or heap
measurements.

### Focused checks

The integration batch is the maintained end-to-end entry point. Additional
`tools/test_*.cjs` scripts cover focused regressions, including activity state,
selection ownership, text editing, renderer resources and import/export pixels.
They are development checks, not dependencies of a normal app build. Browser
scripts generally use `TEST_SITE` for a freshly built local editor; fixture-based
scripts describe their generator or environment variables in their headers.

Native feature checks share one dispatcher instead of separate per-feature
launch scripts:

```sh
zsh tools/test_native_acceptance.sh --list
zsh tools/test_native_acceptance.sh shapes --dry-run
zsh tools/test_native_acceptance.sh shapes
```

This is opt-in: a real case builds and runs the isolated acceptance app. `--list`
and `--dry-run` do neither. Cases that consume proof decks require the matching
fixtures first, as documented in `tools/test_native_pdf_export.sh`; the SVG case
generates its own fixture. Foreground gestures and sandboxed Save panels remain
explicit checks, using `tools/test_native_appkit_gestures.sh` and the sandbox
options of `tools/test_native_pdf_export.sh`.

Keep scripts that exercise current behaviour and distinct regressions. Archive
retired-interface probes and superseded launch wrappers locally rather than
making them part of the release source. Never commit generated screenshots,
reports, temporary packages or private customer fixtures as test tools.

The fixture provenance and regeneration instructions are in
[testdata/powerup](../testdata/powerup/README.md) and
[testdata/pptx](../testdata/pptx/README.md). Keep generated reports, exported
packages and screenshots outside the committed fixtures.

## Emoji font generation

Normal builds consume `assets/emoji/keynope-emoji-fonts.zip` and do not require
Python or FontTools. To regenerate the colour fonts from canonical block
artwork, install `fonttools[woff]` in a Python environment and run:

```sh
python tools/build_emoji_fonts.py
```

Uncompressed `.ttf` files are additionally written to `output/emoji-fonts/`.
These are per-artwork faces used by Keynope's sequence mapping, not a single
installable system emoji font. The derivatives retain the bundled OFL and
attribution notices. Exported text includes only the faces it uses; Markdown
stores Unicode text and styling, not a second copy of the built-in fonts.

## Source layout

| Location | Responsibility |
| --- | --- |
| `main.go`, `native_editor.go`, `main_wasm.go` | Shared engine and editor entry points |
| `masters.go`, `master_ui.go` | Master/layout model, persistence and editing |
| `scene*.go`, `workspace*.go`, `web/scene.js`, `web/workspace.js` | Scene projection and visual editing |
| `table*.go`, `chart*.go`, `web/tables.js`, `web/charts.js` | Structured objects and editors |
| `internal/pptx/`, `pptx*.go` | PowerPoint package conversion |
| `presenter/` | Native Mac app, display handling and export capture |
| `app/`, `assets/` | App metadata, starter decks, templates, fonts and licenses |
| `web/`, `tools/` | Shared browser components, build scripts and tests |
| `testdata/` | Committed synthetic regression fixtures |
| `Makefile` | Build, sign, test, install and clean targets |
