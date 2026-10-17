#!/bin/zsh
set -euo pipefail
repo="${0:A:h:h}"
fixtureRoot="$(mktemp -d /private/tmp/keynope-appkit-gesture-fixture.XXXXXX)"
trap 'rm -rf "$fixtureRoot"' EXIT
cd "$repo"
KEYNOPE_DENSE_EDITING_FIXTURE="$fixtureRoot/dense.md" GOCACHE="${GOCACHE:-/tmp/keynope-go-cache}" \
  go test -count=1 -run '^TestPowerupDenseEditingFixture$' ./internal/app
KEYNOPE_NATIVE_GESTURE_ACCEPTANCE=1 KEYNOPE_NATIVE_GESTURE_FIXTURE="$fixtureRoot/dense.md" \
  KEYNOPE_NATIVE_BACKGROUND=0 zsh tools/test_native_pdf_export.sh
