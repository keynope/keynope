package main

import (
	_ "embed"
	"encoding/base64"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The standard Retro text face is derived from our C64 bitmap font. Both
// windowed editors and HTML exports use the shared TrueType geometry.
//
//go:embed assets/keynope-c64.ttf.base64
var trueTypeFontBase64 string

//go:embed web/truetype.js
var trueTypeJS string

const trueTypeDefaultSize = 97
const trueTypeMaxSize = 512

// Only the obsolete envelope remains recognised; its contents are discarded.
var keynopeFontsEnvelopeRE = regexp.MustCompile(`(?m)^<!--\s*keynope-fonts\b[^\r\n]*(?:\r?\n|$)`)
var trueTypeEnvelopeRE = regexp.MustCompile(`(?m)^<!--\s*truetype-text\b[^\r\n]*(?:\r?\n|$)`)
var trueTypePayloadRE = regexp.MustCompile(`\bbase64:([A-Za-z0-9+/=]*)`)

func recoverTrueTypeEnvelopes(text string) (string, bool) {
	malformed := false
	recovered := trueTypeEnvelopeRE.ReplaceAllStringFunc(text, func(envelope string) string {
		match := trueTypeTextRE.FindStringSubmatch(strings.TrimSpace(envelope))
		if match == nil {
			malformed = true
			if payload := trueTypePayloadRE.FindStringSubmatch(envelope); payload != nil {
				if _, err := base64.StdEncoding.DecodeString(payload[1]); err == nil {
					return "<!-- truetype-text=base64:" + payload[1] + " kind=text -->\n"
				}
			}
			return ""
		}
		if _, err := base64.StdEncoding.DecodeString(match[1]); err != nil {
			malformed = true
			return ""
		}
		return envelope
	})
	return recovered, malformed
}

func deckUsesRetiredTextMetadata(deck Deck) bool {
	retired := false
	visit := func(slide Slide) {
		for _, element := range slide.Elements {
			switch element.Kind {
			case "text", "heading", "bullet", "code", "text-image", "page-number":
			default:
				continue
			}
			q, _ := url.ParseQuery(element.Query)
			if element.Kind == "text-image" || q.Get("render") == "text-image" || q.Has("font") || q.Has("glyph") || q.Has("source") || q.Has("scale") || q.Has("text-size") {
				retired = true
				return
			}
		}
	}
	for _, slide := range deck.Slides {
		visit(slide)
	}
	visit(deck.Masters.Base.Slide)
	for _, layout := range deck.Masters.Layouts {
		visit(layout.Slide)
	}
	return retired
}

type exportTrueType struct {
	RichRuns   []sceneRun        `json:"richRuns,omitempty"`
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

// Upgrade text at document/editor boundaries. Custom bitmap/FIGlet faces and
// sampled text are retired; media and protected QR artwork remain independent.
func standardTextElement(element Element) Element {
	switch element.Kind {
	case "shape":
		return standardShapeLabel(element)
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
	// An authored TrueType size takes precedence over retired sampling metadata.
	// Only infer an optical size when the newer field is absent.
	if !q.Has("ttf-size") && q.Get("render") == "text-image" && strings.TrimSpace(element.Text) != "" {
		onlyEmoji := true
		for _, token := range splitEmojiText(element.Text) {
			if token.assetKey == "" && strings.TrimSpace(token.text) != "" {
				onlyEmoji = false
				break
			}
		}
		if onlyEmoji {
			q.Set("ttf-size", strconv.Itoa(legacyBitmapTrueTypeSize(textImageScale(element))))
		}
	}
	// Explicit bitmap scaling is authored typography, not an inherited default.
	// Match its old eight-pixel cap height in the C64 font's .8-em cap. Keep
	// implicit heading/body sizes inherited, and never replace a newer TTF size.
	if !q.Has("ttf-size") && q.Get("render") == "text-image" {
		if scale, err := strconv.ParseFloat(q.Get("scale"), 64); err == nil && scale > 0 && !math.IsInf(scale, 0) && !math.IsNaN(scale) {
			q.Set("ttf-size", strconv.Itoa(legacyBitmapTrueTypeSize(scale)))
		}
	}
	q.Del("font")
	q.Del("glyph")
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

func legacyBitmapTrueTypeSize(scale float64) int {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	_, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	// Saturate in floating point before converting the potentially enormous
	// legacy pixel count to an integer (including emoji-only legacy elements).
	pixels := math.Max(1, math.Round(8*scale))
	height := math.Ceil(pixels / 2)
	size := math.Round(height * 1080 / float64(rows) / .8)
	return int(math.Max(1, math.Min(trueTypeMaxSize, size)))
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
	return trueTypeSizeWithValues(element, values)
}

func trueTypeSizeWithValues(element Element, values url.Values) int {
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
	return trueTypeWidthWithValues(values)
}

func trueTypeWidthWithValues(values url.Values) float64 {
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
	// This projection mutates only Query strings. Keep unrelated metadata
	// read-only instead of deep-copying potentially large activity archives.
	slide.Elements = append([]Element(nil), slide.Elements...)
	for i, element := range slide.Elements {
		if !isTrueType(element) {
			continue
		}
		q, _ := url.ParseQuery(element.Query)
		if !q.Has("ttf-size") && slide.TTFSize > 0 {
			size := int(math.Round(float64(trueTypeSizeWithValues(element, q)) * float64(slide.TTFSize) / 97))
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
	return trueTypeBoundsFromValues(element, cols, rows, values)
}

func trueTypeBoundsFromValues(element Element, cols, rows int, values url.Values) (int, int) {
	// Authored boxes already define wrapping space. Measuring their contents
	// cannot affect either dimension, and needlessly scans emoji/text on every
	// scene projection. Invalid or incomplete bounds still use content sizing.
	if width, ok := authoredObjectDimension(values, "width", cols); ok {
		if height, ok := authoredObjectDimension(values, "height", rows); ok {
			return int(math.Ceil(width)), int(math.Ceil(height))
		}
	}
	size := float64(trueTypeSizeWithValues(element, values))
	widthPercent := trueTypeWidthWithValues(values)
	longest := 0.0
	for _, line := range strings.Split(element.Text, "\n") {
		width := 0.0
		for _, token := range splitEmojiText(line) {
			if token.assetKey != "" {
				width += size*.8*widthPercent/100 + 2*math.Max(1, size/40)
			} else {
				width += float64(len([]rune(token.text))) * size * .8 * .4167 * widthPercent / 100
			}
		}
		longest = math.Max(longest, width)
	}
	w := int(math.Ceil(longest * float64(cols) / 1920))
	h := int(math.Ceil(float64(strings.Count(element.Text, "\n")+1) * size * 1.2 * float64(rows) / 1080))
	if width, ok := authoredObjectDimension(values, "width", cols); ok {
		w = int(math.Ceil(width))
	}
	if height, ok := authoredObjectDimension(values, "height", rows); ok {
		h = int(math.Ceil(height))
	}
	return max(1, min(cols, w)), max(1, min(rows, h))
}

func trueTypeLayoutRows(element Element, cols, rows int) []string {
	values, _ := url.ParseQuery(element.Query)
	return trueTypeLayoutRowsFromValues(element, cols, rows, values)
}

func trueTypeLayoutRowsFromValues(element Element, cols, rows int, values url.Values) []string {
	w, h := trueTypeBoundsFromValues(element, cols, rows, values)
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
