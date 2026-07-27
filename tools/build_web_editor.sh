#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output_dir=${1:-"$repo_dir/terraform/site/editor"}
temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT

mkdir -p "$output_dir"
cp "$repo_dir/app/Welcome.md" "$temporary_dir/Welcome.md"

(
  cd "$repo_dir"
  GOCACHE=${GOCACHE:-/tmp/keynope-go-cache} go build -o "$temporary_dir/keynope"
  GOOS=js GOARCH=wasm GOCACHE=${GOCACHE:-/tmp/keynope-go-cache} go build -o "$output_dir/keynope-editor.wasm" .
)

"$temporary_dir/keynope" --export "$temporary_dir/Welcome.md"
version=$(awk '/^## [0-9]/{print $2; exit}' "$repo_dir/CHANGELOG.md")

awk '
  /<title>Keynope Export<\/title>/ {
    print "<title>Keynope Web Editor</title>"
    print "<meta name=\"description\" content=\"Build expressive retro presentations in your browser with Keynope.\">"
    next
  }
  /<script id="keynope-data"/ {
    print "<link rel=\"manifest\" href=\"/editor/manifest.webmanifest\">"
    print "<link rel=\"stylesheet\" href=\"/editor/editor.css\">"
    print "<script src=\"/editor/editor.js\"></script>"
  }
  { print }
' "$temporary_dir/Welcome.html" > "$output_dir/index.html"

cp "$repo_dir/app/Welcome.md" "$output_dir/Welcome.md"
sed "s/__KEYNOPE_VERSION__/$version/g" "$repo_dir/web/editor/editor.js" > "$output_dir/editor.js"
cp "$repo_dir/web/editor/editor.css" "$output_dir/editor.css"
cp "$repo_dir/web/editor/service-worker.js" "$output_dir/service-worker.js"
cp "$repo_dir/web/editor/manifest.webmanifest" "$output_dir/manifest.webmanifest"
"$temporary_dir/keynope" --licenses > "$output_dir/licenses.txt"

goroot=$(go env GOROOT)
cp "$goroot/lib/wasm/wasm_exec.js" "$output_dir/wasm_exec.js"

printf '%s\n' "Built Keynope web editor in $output_dir"
