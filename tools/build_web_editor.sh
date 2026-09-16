#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output_dir=${1:-"$repo_dir/terraform/site/editor"}
temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT
welcome_source="$repo_dir/web/editor/Welcome.md"

mkdir -p "$output_dir"
mkdir -p "$output_dir/../join"
cp "$welcome_source" "$temporary_dir/Welcome.md"

(
  cd "$repo_dir"
  GOCACHE=${GOCACHE:-/tmp/keynope-go-cache} go run -tags keynope_sitegen . "$temporary_dir/Welcome.md" "$temporary_dir"
  GOOS=js GOARCH=wasm GOCACHE=${GOCACHE:-/tmp/keynope-go-cache} go build -o "$output_dir/keynope-editor.wasm" .
)

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

cp "$temporary_dir/Welcome.md" "$output_dir/Welcome.md"
sed "s/__KEYNOPE_VERSION__/$version/g" "$repo_dir/web/editor/editor.js" > "$output_dir/editor.js"
cp "$repo_dir/web/editor/editor.css" "$output_dir/editor.css"
cp "$repo_dir/web/activity-games.js" "$output_dir/../activity-games.js"
cp "$repo_dir/web/activity-design.js" "$output_dir/../activity-design.js"
cp "$repo_dir/web/ui-font.css" "$output_dir/../ui-font.css"
mkdir -p "$output_dir/../fonts"
base64 -d < "$repo_dir/assets/keynope-c64.ttf.base64" > "$output_dir/../fonts/keynope-c64.ttf"
cp "$repo_dir/web/participant-transfer.js" "$output_dir/../participant-transfer.js"
node "$repo_dir/tools/build_participant_renderer.cjs" "$temporary_dir/Welcome.html" "$output_dir/../join/slide-renderer.js"
cp "$repo_dir/web/editor/service-worker.js" "$output_dir/service-worker.js"
cp "$repo_dir/web/editor/manifest.webmanifest" "$output_dir/manifest.webmanifest"
cp "$temporary_dir/licenses.txt" "$output_dir/licenses.txt"

goroot=$(go env GOROOT)
cp "$goroot/lib/wasm/wasm_exec.js" "$output_dir/wasm_exec.js"

# Fingerprint the complete editor, not its release number. Development deploys
# often share a version, but must never keep an obsolete parser or renderer.
build=$(cd "$output_dir" && shasum -a 256 index.html editor.js editor.css Welcome.md licenses.txt wasm_exec.js keynope-editor.wasm service-worker.js | shasum -a 256 | cut -c1-20)
node - "$output_dir" "$build" <<'NODE'
const fs = require('node:fs');
const [dir, build] = process.argv.slice(2);
const html = fs.readFileSync(dir + '/index.html', 'utf8')
  .replace(/\/editor\/(editor\.(?:js|css))/g, '/editor/$1?build=' + build);
fs.writeFileSync(dir + '/index.html', html);
const worker = fs.readFileSync(dir + '/service-worker.js', 'utf8').replaceAll('__KEYNOPE_BUILD__', build);
fs.writeFileSync(dir + '/service-worker.js', worker);
NODE

printf '%s\n' "Built Keynope web editor in $output_dir"
