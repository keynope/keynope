package main

import (
	_ "embed"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// The standard text face is derived from our C64 bitmap font. Raster treatments
// use this same face and geometry in both windowed editors and HTML exports.
//
//go:embed assets/keynope-c64.ttf.base64
var trueTypeFontBase64 string

//go:embed web/truetype.js
var trueTypeJS string

const trueTypeDefaultSize = 97
const trueTypeMaxSize = 512

type exportTrueType struct {
	Kind       string            `json:"kind,omitempty"`
	Text       string            `json:"text"`
	Query      string            `json:"query"`
	Size       int               `json:"size"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Emojis     []exportEmojiRun  `json:"emojis,omitempty"`
	EmojiFonts map[string]string `json:"emojiFonts,omitempty"`
}

func isTrueType(element Element) bool { return textRenderMode(element.Query) == "truetype" }

// Upgrade text at document/editor boundaries. The bitmap engine remains useful
// for user-created glyph fonts and non-text artwork, but is no longer the default
// text object. Legacy truetype metadata is simply ordinary text metadata now.
func standardTextElement(element Element) Element {
	switch element.Kind {
	case "heading", "text", "text-image", "bullet", "code":
	default:
		return element
	}
	q, _ := url.ParseQuery(element.Query)
	if q.Get("participant-kind") == "page-number" || element.PlaceholderRole == "activity-qr" || q.Get("qr") == "1" {
		return element
	}
	// Preserve the optical size of existing picker artwork when moving it to
	// the colour font. Font glyphs use a square .8-em cap, without text squeeze.
	if q.Get("render") == "text-image" && strings.TrimSpace(element.Text) != "" {
		onlyEmoji := true
		for _, token := range splitEmojiText(element.Text) {
			if token.assetKey == "" && strings.TrimSpace(token.text) != "" {
				onlyEmoji = false
				break
			}
		}
		if onlyEmoji {
			_, rows := authoredRenderSize(245, 56)
			size := int(math.Round(float64(bitmapTextRowHeight(textImageScale(element))) * 1080 / float64(rows) / .8))
			q.Set("ttf-size", strconv.Itoa(max(1, min(trueTypeMaxSize, size))))
		}
	}
	if q.Get("font") != "" {
		return element
	}
	q.Set("render", "truetype")
	q.Del("source")
	q.Del("scale")
	q.Del("text-size")
	if element.Kind == "text-image" {
		element.Kind = "text"
	}
	element.Query = q.Encode()
	return element
}

func standardizeDeckText(deck *Deck) {
	convert := func(slide *Slide) {
		for i := range slide.Elements {
			slide.Elements[i] = standardTextElement(slide.Elements[i])
		}
	}
	for i := range deck.Slides {
		convert(&deck.Slides[i])
	}
	convert(&deck.Masters.Base.Slide)
	for i := range deck.Masters.Layouts {
		convert(&deck.Masters.Layouts[i].Slide)
	}
}

func trueTypeSize(element Element) int {
	values, _ := url.ParseQuery(element.Query)
	defaultSize := trueTypeDefaultSize
	if element.Kind == "heading" {
		defaultSize = 386
		if element.Level == 2 {
			defaultSize = 193
		}
	}
	return max(1, min(trueTypeMaxSize, intQueryDefault(values, "ttf-size", defaultSize)))
}

func trueTypeWidthPercent(element Element) float64 {
	values, _ := url.ParseQuery(element.Query)
	width, err := strconv.ParseFloat(values.Get("ttf-width"), 64)
	if err != nil || math.IsNaN(width) || math.IsInf(width, 0) {
		return 100
	}
	return math.Max(1, math.Min(200, width))
}

// Defaults are applied to a render-only copy, never to authored element metadata.
func withTrueTypeDefaults(slide Slide) Slide {
	if slide.TTFSize <= 0 && slide.TTFWidth <= 0 {
		return slide
	}
	slide = cloneSlide(slide)
	for i, element := range slide.Elements {
		if !isTrueType(element) {
			continue
		}
		q, _ := url.ParseQuery(element.Query)
		if !q.Has("ttf-size") && slide.TTFSize > 0 {
			size := int(math.Round(float64(trueTypeSize(element)) * float64(slide.TTFSize) / 97))
			q.Set("ttf-size", strconv.Itoa(max(1, min(512, size))))
		}
		if !q.Has("ttf-width") && slide.TTFWidth > 0 {
			q.Set("ttf-width", strconv.FormatFloat(slide.TTFWidth, 'f', -1, 64))
		}
		slide.Elements[i].Query = q.Encode()
	}
	return slide
}

// Explicit bounds keep layout, sorting, hit testing and terminal fallback stable.
// Font sizes are in reference pixels on a 1920x1080 slide, independent of screen.
func trueTypeBounds(element Element, cols, rows int) (int, int) {
	values, _ := url.ParseQuery(element.Query)
	size := float64(trueTypeSize(element))
	longest := 0.0
	for _, line := range strings.Split(element.Text, "\n") {
		width := 0.0
		for _, token := range splitEmojiText(line) {
			if token.assetKey != "" {
				width += size*.8*trueTypeWidthPercent(element)/100 + 2*math.Max(1, size/40)
			} else {
				width += float64(len([]rune(token.text))) * size * .8 * .4167 * trueTypeWidthPercent(element) / 100
			}
		}
		longest = math.Max(longest, width)
	}
	w := int(math.Ceil(longest * float64(cols) / 1920))
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
.keynope-ttf-size,.keynope-ttf-width{width:64px;min-width:64px;height:30px;box-sizing:border-box;background:#20262c;color:#fff;border:1px solid #59616a;border-radius:4px}
.keynope-ttf-width-control{display:inline-flex;flex-direction:column;align-items:flex-start;gap:2px;white-space:nowrap;color:#d8dde3;font-size:10px;line-height:12px}
`
}
