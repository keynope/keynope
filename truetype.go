package main

import (
	_ "embed"
	"math"
	"net/url"
	"strings"
)

// The experimental face is derived from our C64 bitmap font. Keep the original
// glyph renderer as the default and store only opt-in object metadata in decks.
//
//go:embed assets/keynope-c64.ttf.base64
var trueTypeFontBase64 string

//go:embed web/truetype.js
var trueTypeJS string

const trueTypeDefaultSize = 72
const trueTypeMaxSize = 512

type exportTrueType struct {
	Kind   string `json:"kind,omitempty"`
	Text   string `json:"text"`
	Query  string `json:"query"`
	Size   int    `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func isTrueType(element Element) bool { return textRenderMode(element.Query) == "truetype" }

func trueTypeSize(element Element) int {
	values, _ := url.ParseQuery(element.Query)
	return max(1, min(trueTypeMaxSize, intQueryDefault(values, "ttf-size", trueTypeDefaultSize)))
}

// Explicit bounds keep layout, sorting, hit testing and terminal fallback stable.
// Font sizes are in reference pixels on a 1920x1080 slide, independent of screen.
func trueTypeBounds(element Element, cols, rows int) (int, int) {
	values, _ := url.ParseQuery(element.Query)
	size := float64(trueTypeSize(element))
	longest := 1
	for _, line := range strings.Split(element.Text, "\n") {
		longest = max(longest, len([]rune(line)))
	}
	w := int(math.Ceil(float64(longest) * size * .8 * float64(cols) / 1920))
	h := int(math.Ceil(float64(len(strings.Split(element.Text, "\n"))) * size * 1.2 * float64(rows) / 1080))
	return max(1, min(cols, intQueryDefault(values, "width", w))), max(1, min(rows, intQueryDefault(values, "height", h)))
}

func trueTypeLayoutRows(element Element, cols, rows int) []string {
	w, h := trueTypeBounds(element, cols, rows)
	out := make([]string, h)
	text := strings.Split(element.Text, "\n")
	for i := range out {
		out[i] = strings.Repeat(" ", w)
		// A readable, bounded fallback for terminal clients; no TTF installation needed.
		if i < len(text) {
			out[i] = padRight(crop(text[i], w), w)
		}
	}
	return out
}

func trueTypeCSS() string {
	return `@font-face{font-family:KeynopeC64;src:url(data:font/ttf;base64,` + strings.TrimSpace(trueTypeFontBase64) + `) format('truetype');font-weight:400;font-display:block}
body :is([class*="keynope-editor"],[class*="keynope-app"],[class*="keynope-engagement"],[class*="keynope-font"],[class*="keynope-modal"],[class*="keynope-slide"],[class*="keynope-colour"],[class*="keynope-speaker"],[class*="keynope-text-effect"]),
body :is(.keynope-editor-shell,.keynope-editor-topbar,.keynope-app-toolbar,.keynope-engagement-board,.keynope-modal-blocker,.keynope-font-editor) :is(button,input,textarea,select,label,h1,h2,h3,p,span,div){font-family:KeynopeC64,ui-monospace,monospace!important}
.keynope-ttf-size{width:64px;min-width:64px;height:30px;box-sizing:border-box;background:#20262c;color:#fff;border:1px solid #59616a;border-radius:4px}
`
}
