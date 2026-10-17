#!/bin/zsh
set -euo pipefail
repo="${0:A:h:h}"
fixtureRoot="$(mktemp -d /private/tmp/keynope-style-menu-fixture.XXXXXX)"
trap 'rm -rf "$fixtureRoot"' EXIT
cd "$repo"
if [[ "${KEYNOPE_NATIVE_STYLE_MENU_FOREGROUND:-0}" == "1" ]]; then
  print 'Foreground opt-in: this isolated test will activate its own Keynope window.'
  export KEYNOPE_NATIVE_BACKGROUND=0
fi
KEYNOPE_NATIVE_STYLE_MENU_EVIDENCE="$fixtureRoot" GOCACHE="${GOCACHE:-/tmp/keynope-go-cache}" \
  go test -run '^TestNativeStyleMenuFixture$' ./internal/app
KEYNOPE_NATIVE_STYLE_MENU_ACCEPTANCE=1 KEYNOPE_NATIVE_STYLE_MENU_FIXTURE="$fixtureRoot/original.md" \
  zsh "$repo/tools/test_native_pdf_export.sh"
