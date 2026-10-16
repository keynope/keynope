# Synthetic editor and output proof decks

These ordinary Markdown decks are generated entirely from project-owned test
data by `TestPowerupPublicProofCorpus`. They are distributed under the repository
license. Embedded PNG, GIF and SVG artwork is generated procedurally by that
test; no customer decks, participant identities, results or downloaded artwork
are included. Font names refer to Keynope's existing bundled fonts; their
upstream provenance/licenses remain with the bundled font assets, not here.

Regenerate intentionally, review the Markdown diff, then run without updating:

```sh
KEYNOPE_UPDATE_POWERUP_FIXTURES=1 go test -count=1 -run '^TestPowerupPublicProofCorpus$' .
go test -count=1 -run '^TestPowerupPublicProofCorpus$' .
```

The test checks deterministic source bytes, exact save/reopen stability and a
nonempty production scene for every page. The fixture manifest specifies the
default rendering context; it is not an approved screenshot baseline.

The browser corpus harness uses the production WASM editor and shared renderer.
After building it, run either installed Playwright engine (no desktop app opens):

```sh
sh tools/build_web_editor.sh /tmp/keynope-powerup-site/editor
TEST_SITE=/tmp/keynope-powerup-site node tools/test_powerup_corpus.cjs
TEST_SITE=/tmp/keynope-powerup-site TEST_BROWSER=webkit PROOF_OUTPUT=/tmp/keynope-corpus-webkit-proof node tools/test_powerup_corpus.cjs
```

Playwright must be available to Node; `PLAYWRIGHT_MODULE` can point to an existing
installation, and `CHROME_PATH` can select an installed Chromium executable.
Captures/reports remain outside these fixtures. Browser tests that await an
asynchronous page predicate should use `tools/wait_for_async.cjs`; passing an
async predicate directly to `waitForFunction` can stop at a Promise which later
resolves false.

For the consolidated Go/build/interaction/corpus batch, run
`node tools/test_editor_integration.cjs`. It creates a fresh temporary build and
independent fixture/capture folders for each browser, records source hashes and
individual exit statuses, and keeps visual approval explicitly separate. See
the main README for optional local-schema and background-native checks.

| Deck | Coverage |
|---|---|
| themes-and-overrides | Linked accents, explicit override, layout paint, C64/sans/mono |
| tables | Empty, 3×3, merged, rotated, header/bands, 20×20 |
| charts | Six native types, zero/negative values, long labels, multiple series |
| diagrams | Adjusted and rotated shapes, labels, group, routing obstacle, arrow |
| media | Alpha PNG, passive SVG, cropped two-frame GIF, masks, Retro image, adjusted background |
| builds | Click/after steps, grouped targets, fade transition |
| export-layout | Squeezed C64, mono text, intentionally clipped artwork |

Keep generated PDFs/PPTX/HTML/captures outside this directory. No screenshot is
an approved visual baseline until inspected and documented. For the same corpus
at widescreen, portrait and ultrawide sizes, generate the public output matrix:

```sh
DIR=$(mktemp -d)
KEYNOPE_SURFACE_EVIDENCE="$DIR" KEYNOPE_SURFACE_ALL_ASPECTS=1 \
  go test -count=1 -run '^TestPowerupPublicSurfaceSceneParity$' .
KEYNOPE_SURFACE_EVIDENCE="$DIR" KEYNOPE_SURFACE_EXPECTED_PAGES=60 \
  node tools/test_public_surface_parity.cjs
KEYNOPE_SURFACE_EVIDENCE="$DIR" KEYNOPE_SURFACE_EXPECTED_PAGES=60 TEST_BROWSER=webkit \
  node tools/test_public_surface_parity.cjs
```

On macOS, the actual OS WebView can check these sixty pages without opening
the editor. It compares standalone output with independently mounted public
scenes and rejects erased scenes, using exact backing-pixel comparisons:

```sh
swiftc -parse-as-library -warnings-as-errors -strict-concurrency=complete \
  -module-cache-path "${TMPDIR}keynope-swift-modules" \
  -target "$(uname -m)-apple-macosx14.0" -O \
  -framework Cocoa -framework WebKit -framework PDFKit \
  presenter/SlideExport.swift tools/test_native_surface_parity.swift \
  -o "$DIR/native-check"
"$DIR/native-check" "$DIR"
```

The native harness blocks external HTTP(S), leaves input fixtures untouched,
and asserts no activation or clipboard changes. It refuses to replace an
existing `native/` evidence directory. `KEYNOPE_NATIVE_SURFACE_FILTER` can select
one generated HTML file for diagnosis; only an unfiltered sixty-page success
sets `fullCorpus: true` in the report. Playback/navigation, foreground editing,
external reader and full cross-output acceptance are separate checks.
