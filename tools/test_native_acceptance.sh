#!/bin/zsh
# Explicit opt-in cases using the shared isolated Mac acceptance harness.
set -euo pipefail
repo="${0:A:h:h}"
cases=(activity-builds builds image-first-paint pptx-timing presentation-tools shapes stroke-routes svg system-fonts table-tokens theme-colours transitions)
if [[ "$#" == 1 && ( "$1" == --list || "$1" == --help ) ]]; then
  print -l -- "Usage: zsh tools/test_native_acceptance.sh CASE [--dry-run]" "Cases:" "${cases[@]}"
  exit 0
fi
if [[ "$#" -lt 1 || "$#" -gt 2 || ( "$#" == 2 && "$2" != --dry-run ) ]]; then
  print -u2 -- 'Usage: zsh tools/test_native_acceptance.sh CASE [--dry-run]'
  exit 2
fi
case "$1" in
  activity-builds) flag=KEYNOPE_NATIVE_ACTIVITY_BUILD_ACCEPTANCE ;;
  builds) flag=KEYNOPE_NATIVE_BUILD_ACCEPTANCE ;;
  image-first-paint) flag=KEYNOPE_NATIVE_IMAGE_FIRST_PAINT_ACCEPTANCE ;;
  pptx-timing) flag=KEYNOPE_NATIVE_PPTX_TIMING_ACCEPTANCE ;;
  presentation-tools) flag=KEYNOPE_NATIVE_PRESENTATION_TOOLS_ACCEPTANCE ;;
  shapes) flag=KEYNOPE_NATIVE_SHAPE_ACCEPTANCE ;;
  stroke-routes) flag=KEYNOPE_NATIVE_STROKE_ROUTE_ACCEPTANCE ;;
  svg) flag=KEYNOPE_NATIVE_SVG_ACCEPTANCE ;;
  system-fonts) flag=KEYNOPE_NATIVE_SYSTEM_FONTS_ACCEPTANCE ;;
  table-tokens) flag=KEYNOPE_NATIVE_TABLE_TOKEN_ACCEPTANCE ;;
  theme-colours) flag=KEYNOPE_NATIVE_THEME_COLOUR_ACCEPTANCE ;;
  transitions) flag=KEYNOPE_NATIVE_TRANSITION_ACCEPTANCE ;;
  *) print -u2 -- "Unknown acceptance case: $1"; exit 2 ;;
esac
if [[ "${2:-}" == --dry-run ]]; then
  if [[ "$1" == svg ]]; then
    print -r -- 'KEYNOPE_SVG_EVIDENCE=/tmp/keynope-svg-proof go test -run TestSVG .'
  fi
  print -r -- "$flag=1 zsh tools/test_native_pdf_export.sh"
  exit 0
fi
if [[ "$1" == svg ]]; then
  KEYNOPE_SVG_EVIDENCE=/tmp/keynope-svg-proof GOCACHE=${GOCACHE:-/tmp/keynope-go-cache} go -C "$repo" test -run TestSVG .
fi
export "$flag=1"
exec zsh "$repo/tools/test_native_pdf_export.sh"
