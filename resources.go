// Package keynope provides the bundled resources shared by the native app,
// browser editor and offline site generator. Application code lives in
// internal/app; this file stays beside the resources so go:embed can include
// them without copying assets or depending on the build's working directory.
package keynope

import "embed"

// Resources contains only the assets used by the application, never test
// fixtures, private infrastructure or development tools.
//
//go:embed LICENSE.txt app/Welcome.md
//go:embed assets/emoji/NOTICE.txt assets/emoji/NOTO-REGION-FLAGS-LICENSE.txt assets/emoji/OFL.txt assets/emoji/UNICODE-LICENSE.txt
//go:embed assets/emoji/emoji-test.txt
//go:embed assets/fonts/GO-FONT-LICENSE.txt assets/keynope-c64.ttf.base64
//go:embed assets/licenses/GO-LIBRARIES-LICENSE.txt assets/licenses/SVG-RENDERER-LICENSE.txt assets/licenses/UNISEG-LICENSE.txt
//go:embed assets/licenses/CRAFT-EFFECTS-LICENSE.txt
//go:embed assets/licenses/MCP-SDK-LICENSE.txt assets/licenses/JSONSCHEMA-LICENSE.txt
//go:embed assets/licenses/MCP-SUPPORT-LICENSE.txt
//go:embed assets/templates/*.md assets/templates/previews.json
//go:embed web/activity-design.js web/activity-games.js web/builds.js web/charts.js web/conversion-report.js web/fonts.js
//go:embed web/interface.css web/participant-transfer.js web/presentation-tools.js web/video.js web/print.js web/scene.js web/tables.js web/templates.js
//go:embed web/text-layout.js web/text-styles.js web/truetype.js web/workspace.css web/workspace.js
var Resources embed.FS

// Keep the large archives as direct byte embeds. Reading them through embed.FS
// would allocate retained copies on startup, unlike the original byte embeds.
//
//go:embed assets/emoji/keynope-emoji-fonts.zip
var EmojiFontArchive []byte

//go:embed assets/emoji/keynope-emoji-glyphs.bin.gz
var EmojiGlyphArchive []byte
