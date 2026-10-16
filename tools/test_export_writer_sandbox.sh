#!/bin/zsh
set -euo pipefail
repo="${0:A:h:h}"
root="$(mktemp -d /private/tmp/keynope-writer-sandbox.XXXXXX)"
app="$root/Writer.app"
identifier="sh.keynope.writer-acceptance.$(basename "$root" | tr '[:upper:]' '[:lower:]')"
trap 'rm -rf "$root"' EXIT
cd "$repo"
mkdir -p "$app/Contents/MacOS"
swiftc -warnings-as-errors -strict-concurrency=complete \
  -module-cache-path "${SWIFT_MODULECACHE_PATH:-/tmp/keynope-swift-modules}" \
  -target arm64-apple-macosx14.0 -O presenter/SlideExportWriter.swift \
  tools/test_export_writer.swift -o "$app/Contents/MacOS/Keynope"
cp app/Info.plist "$app/Contents/Info.plist"
plutil -replace CFBundleIdentifier -string "$identifier" "$app/Contents/Info.plist"
codesign --force --options runtime --entitlements app/Keynope.entitlements --sign - "$app"
codesign -d --entitlements :- "$app" 2>/dev/null
# No pathname grant is synthesized. Foundation resolves Application Support
# inside this test app's container. External user-selected access is a separate
# manual Save-panel acceptance, not proven by this helper.
"$app/Contents/MacOS/Keynope"
