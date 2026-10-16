#!/bin/zsh
set -euo pipefail
repo="${0:A:h:h}"
root="$(mktemp -d /private/tmp/keynope-pdf-panels.XXXXXX)"
app="$root/KeynopePDFAcceptance.app"
identifier="sh.keynope.pdf-acceptance.$(basename "$root" | tr '[:upper:]' '[:lower:]')"
reportRoot="$root"
fixture="$root/original.md"
external=""
if [[ "${SANDBOX:-0}" == "1" ]]; then
  # macOS forbids programmatic Save confirmation. This variant requires the
  # tester to press Export on three preconfigured native panels (60s each).
  print 'Manual sandbox check: press Export on the Quality, Exact and external PDF panels.'
  external="$root/user-selected.pdf"
  reportRoot="$HOME/Library/Containers/$identifier/Data/Library/Application Support/PDFAcceptance"
  fixture="$app/Contents/Resources/pdf-acceptance.md"
fi
cleanup() {
  if [[ "${KEEP_OUTPUT:-0}" == "1" ]]; then
    print "Native PDF artifacts: $root"
  else
    rm -rf "$root"
  fi
}
trap cleanup EXIT
cd "$repo"
if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  GOCACHE="${GOCACHE:-/private/tmp/keynope-go-cache}" \
  CLANG_MODULE_CACHE_PATH="${CLANG_MODULE_CACHE_PATH:-/private/tmp/keynope-clang-cache}" \
  make app
fi
swiftc -D KEYNOPE_NATIVE_ACCEPTANCE -warnings-as-errors -strict-concurrency=complete \
  -module-cache-path "${SWIFT_MODULECACHE_PATH:-/tmp/keynope-swift-modules}" -target arm64-apple-macosx14.0 -O \
  -framework Cocoa -framework WebKit -framework AVFoundation -framework ScreenCaptureKit -framework PDFKit \
  presenter/KeynopePresenter.swift presenter/ScreenShare.swift presenter/EmbeddedIcon.swift \
  presenter/SlideExport.swift presenter/SlideExportWriter.swift -o "$root/Keynope"
ditto bin/Keynope.app "$app"
cp "$root/Keynope" "$app/Contents/MacOS/Keynope"
plutil -replace CFBundleIdentifier -string "$identifier" "$app/Contents/Info.plist"
print '<!-- keynope width=245 height=56 -->\n\n<!-- top=2 left=10 width=220 height=10 render=truetype modern-font=sans modern-size=72 -->\n# Native Presentation Acceptance\n\n<!-- top=16 left=10 width=220 height=8 render=truetype modern-font=sans modern-size=48 -->\nNative PDF workflow proof.\n\n---\n<!-- keynope-tab=private-tab -->\n\n# PRIVATE TAB\n\n---\n\n<!-- top=2 left=10 width=220 height=10 render=truetype modern-font=c64 modern-size=100 -->\n# Final Slide\n' > "$root/original.md"
if [[ "${KEYNOPE_NATIVE_TRANSITION_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-build-proof/transitions.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_PPTX_TIMING_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-timing-proof/timing-imported.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_ACTIVITY_BUILD_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-build-proof/activity-builds.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_STROKE_ROUTE_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-stroke-route-proof/routes.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_THEME_COLOUR_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-theme-colour-proof/colours.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_TABLE_TOKEN_ACCEPTANCE:-0}" == "1" ]]; then
  cp /tmp/keynope-table-token-proof/tokens.md "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_STYLE_MENU_ACCEPTANCE:-0}" == "1" ]]; then
  cp "${KEYNOPE_NATIVE_STYLE_MENU_FIXTURE:?Missing owned style-menu fixture}" "$root/original.md"
fi
if [[ "${KEYNOPE_NATIVE_GESTURE_ACCEPTANCE:-0}" == "1" ]]; then
  cp "${KEYNOPE_NATIVE_GESTURE_FIXTURE:?Missing owned gesture fixture}" "$root/original.md"
fi
if [[ "${SANDBOX:-0}" == "1" ]]; then
  cp "$root/original.md" "$fixture"
  codesign --force --options runtime --identifier "$identifier" --entitlements app/Keynope.entitlements --sign - "$app" >/dev/null
else
  # The production helper inherits its parent's sandbox. An unsandboxed test
  # parent instead needs an ordinary helper signature (no inherited sandbox).
  codesign --force --options runtime --sign - "$app/Contents/Helpers/keynope-engine" >/dev/null
  codesign --force --options runtime --identifier "$identifier" --sign - "$app" >/dev/null
fi
KEYNOPE_NATIVE_BACKGROUND="${KEYNOPE_NATIVE_BACKGROUND:-1}" KEYNOPE_NATIVE_PDF_ACCEPTANCE=1 KEYNOPE_NATIVE_ACCEPTANCE_ROOT="$reportRoot" \
  KEYNOPE_NATIVE_PDF_SANDBOX="${SANDBOX:-0}" \
  KEYNOPE_NATIVE_PDF_EXTERNAL_DEST="$external" \
  "$app/Contents/MacOS/Keynope" --app "$fixture"
if [[ ! -f "$reportRoot/report.json" ]]; then KEEP_OUTPUT=1;print -u2 "No native PDF report: $reportRoot";exit 1;fi
plutil -p "$reportRoot/report.json"
if [[ "$(plutil -extract passed raw -o - "$reportRoot/report.json")" != "true" ]]; then KEEP_OUTPUT=1;exit 1;fi
if [[ "${SANDBOX:-0}" == "1" ]]; then
  swiftc -warnings-as-errors -framework PDFKit tools/test_sandbox_pdf_readback.swift -o "$root/verify-sandbox-pdf"
  "$root/verify-sandbox-pdf" "$root" "$reportRoot" > "$root/sandbox-readback.json"
  plutil -p "$root/sandbox-readback.json"
  ditto "$reportRoot" "$root/container-proof"
  codesign -d --entitlements :- "$app" 2>/dev/null
fi
