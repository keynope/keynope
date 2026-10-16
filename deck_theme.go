package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type DeckTheme struct {
	Version  int             `json:"version"`
	Name     string          `json:"name,omitempty"`
	Colors   ThemeColors     `json:"colors"`
	Fonts    ThemeFonts      `json:"fonts"`
	Defaults ThemeDefaults   `json:"defaults,omitempty"`
	Extra    *jsonExtensions `json:"-"`
}
type ThemeColors struct {
	Background string          `json:"background"`
	Surface    string          `json:"surface"`
	Text       string          `json:"text"`
	Heading    string          `json:"heading"`
	Muted      string          `json:"muted"`
	Line       string          `json:"line"`
	Accent     []string        `json:"accent"`
	Extra      *jsonExtensions `json:"-"`
}
type ThemeFonts struct {
	Heading ThemeFont       `json:"heading"`
	Body    ThemeFont       `json:"body"`
	Mono    ThemeFont       `json:"mono"`
	Extra   *jsonExtensions `json:"-"`
}
type ThemeFont struct {
	FontID     string          `json:"fontId"`
	Size       float64         `json:"size"`
	Width      float64         `json:"width"`
	LineHeight float64         `json:"lineHeight"`
	Extra      *jsonExtensions `json:"-"`
}
type ThemeDefaults struct {
	ShapeFill string          `json:"shapeFill,omitempty"`
	ShapeLine string          `json:"shapeLine,omitempty"`
	LineWidth float64         `json:"lineWidth,omitempty"`
	Extra     *jsonExtensions `json:"-"`
}

const maxThemeMetadataBytes = 256 << 10

var deckThemeRE = regexp.MustCompile(`(?m)^<!--\s*keynope-theme\s+version=([0-9]+)\s+base64:([A-Za-z0-9+/=]+)\s*-->[\t ]*\r?\n?`)
var deckThemeEnvelopeRE = metadataNamespaceEnvelopeRE("keynope-theme")

func (theme *DeckTheme) Validate() error {
	if theme == nil {
		return nil
	}
	if theme.Version != 1 || len(theme.Name) > 100 {
		return fmt.Errorf("unsupported or invalid deck theme")
	}
	c := theme.Colors
	for _, color := range append([]string{c.Background, c.Surface, c.Text, c.Heading, c.Muted, c.Line}, c.Accent...) {
		if len(color) != 7 || !validObjectColor(color) {
			return fmt.Errorf("theme colors must be six-digit hex colors")
		}
	}
	if len(c.Accent) != 6 {
		return fmt.Errorf("theme needs six accent colors")
	}
	for _, font := range []ThemeFont{theme.Fonts.Heading, theme.Fonts.Body, theme.Fonts.Mono} {
		if font.FontID != "c64" && font.FontID != "sans" && font.FontID != "mono" {
			return fmt.Errorf("theme font must be a bundled Keynope font")
		}
		if !finitePositive(font.Size) || font.Size > 1024 || !finitePositive(font.Width) || font.Width > 200 || !finitePositive(font.LineHeight) || font.LineHeight < .5 || font.LineHeight > 4 {
			return fmt.Errorf("invalid theme typography")
		}
	}
	if !validObjectColor(theme.Defaults.ShapeFill) || !validObjectColor(theme.Defaults.ShapeLine) || math.IsNaN(theme.Defaults.LineWidth) || math.IsInf(theme.Defaults.LineWidth, 0) || theme.Defaults.LineWidth < 0 || theme.Defaults.LineWidth > 20 {
		return fmt.Errorf("invalid theme shape defaults")
	}
	return nil
}
func (theme *DeckTheme) Clone() *DeckTheme {
	if theme == nil {
		return nil
	}
	copy := *theme
	copy.Extra = cloneJSONExtensions(theme.Extra)
	copy.Colors.Extra = cloneJSONExtensions(theme.Colors.Extra)
	copy.Colors.Accent = append([]string(nil), theme.Colors.Accent...)
	copy.Fonts.Extra = cloneJSONExtensions(theme.Fonts.Extra)
	copy.Fonts.Heading.Extra = cloneJSONExtensions(theme.Fonts.Heading.Extra)
	copy.Fonts.Body.Extra = cloneJSONExtensions(theme.Fonts.Body.Extra)
	copy.Fonts.Mono.Extra = cloneJSONExtensions(theme.Fonts.Mono.Extra)
	copy.Defaults.Extra = cloneJSONExtensions(theme.Defaults.Extra)
	return &copy
}
func encodeDeckTheme(theme *DeckTheme) (string, error) {
	if theme == nil {
		return "", nil
	}
	if err := theme.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(theme)
	if err != nil || len(data) > maxThemeMetadataBytes {
		return "", fmt.Errorf("theme metadata is invalid or too large")
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-theme version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->", nil
}
func decodeDeckTheme(text string) (*DeckTheme, string, error) {
	matches := findDocumentMetadata(text, deckThemeRE, 2)
	if len(matches) == 0 {
		if firstDocumentMetadata(text, deckThemeEnvelopeRE) != nil {
			return nil, text, fmt.Errorf("malformed deck theme metadata")
		}
		return nil, text, nil
	}
	if len(matches) != 1 || len(findDocumentMetadataIndices(text, deckThemeEnvelopeRE, 2)) != 1 || matches[0][1] != "1" || len(matches[0][2]) > maxThemeMetadataBytes*2 {
		return nil, text, fmt.Errorf("ambiguous, unsupported or oversized deck theme")
	}
	compressed, err := base64.StdEncoding.DecodeString(matches[0][2])
	if err != nil {
		return nil, text, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, text, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxThemeMetadataBytes+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(data) > maxThemeMetadataBytes {
		return nil, text, fmt.Errorf("invalid or oversized deck theme")
	}
	var theme DeckTheme
	if err = json.Unmarshal(data, &theme); err != nil {
		return nil, text, err
	}
	if err = theme.Validate(); err != nil {
		return nil, text, err
	}
	return &theme, rewriteDocumentMetadata(text, deckThemeRE, nil), nil
}

// Resolve before geometry is calculated so flow, bounds and the painter use
// exactly the same font metrics. Explicit master/element fields win.
func (deck Deck) withThemeTypography(slide Slide) Slide {
	if deck.Theme == nil && !slideHasConcreteTextDefaults(slide) {
		return slide
	}
	slide.Elements = append([]Element(nil), slide.Elements...)
	for i, e := range slide.Elements {
		outer, _ := url.ParseQuery(e.Query)
		if !outer.Has("fg") && !outer.Has("header") {
			if color := deck.themeTokenColor(outer.Get("theme-fg")); color != "" {
				outer.Set("fg", color)
				e.Query = outer.Encode()
			}
		}
		slide.Elements[i] = e
		if e.Kind == "shape" {
			slide.Elements[i] = mapShapeLabelQuery(e, func(label shapeLabelData, q url.Values) url.Values {
				q = shapeLabelStyleValues(e, q)
				return deck.resolveThemeTextValues(Element{Kind: "text", Text: label.Text}, q)
			})
			continue
		}
		switch e.Kind {
		case "heading", "text", "bullet", "code", "text-image", "page-number":
		default:
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		slide.Elements[i].Query = deck.resolveThemeTextValues(e, q).Encode()
	}
	return slide
}

func (deck Deck) themeTokenColor(token string) string {
	if deck.Theme == nil {
		if token == "" {
			return ""
		}
		deck.Theme = deck.resolvedDeckTheme()
	}
	c := deck.Theme.Colors
	switch token {
	case "background":
		return c.Background
	case "surface":
		return c.Surface
	case "text":
		return c.Text
	case "heading":
		return c.Heading
	case "muted":
		return c.Muted
	case "line":
		return c.Line
	}
	for i, color := range c.Accent {
		if token == "accent-"+strconv.Itoa(i+1) {
			return color
		}
	}
	return ""
}

func validThemeColorToken(token string) bool {
	switch token {
	case "background", "surface", "text", "heading", "muted", "line", "accent-1", "accent-2", "accent-3", "accent-4", "accent-5", "accent-6":
		return true
	}
	return false
}

func (deck Deck) applyThemeObjectPaint(e Element, slide Slide, q url.Values, paint scenePaint) scenePaint {
	defaults, hasDefaults := deck.shapePaintDefaults(q)
	if e.Kind == "shape" {
		paint.NoFill = q.Get("shape-fill") == "none"
		if !q.Has("shape-fill") && !q.Has("fg") && !q.Has("theme-fg") && slide.FG == "" && paint.GradientStart == "" && hasDefaults {
			paint.NoFill = defaults.ShapeFill == "none"
		}
	}
	if e.Kind == "shape" && strings.HasPrefix(q.Get("shape-stroke"), "theme:") {
		if color := deck.themeTokenColor(strings.TrimPrefix(q.Get("shape-stroke"), "theme:")); color != "" {
			q = cloneQueryValues(q)
			q.Set("shape-stroke", color)
		}
	}
	if !q.Has("fg") && !q.Has("header") {
		if color := deck.themeTokenColor(q.Get("theme-fg")); color != "" {
			paint.Color = color
		}
	}
	if !hasDefaults {
		if e.Kind == "shape" {
			return explicitShapeStroke(q, paint)
		}
		return paint
	}
	if e.Kind == "shape" {
		if !q.Has("fg") && !q.Has("theme-fg") && slide.FG == "" && defaults.ShapeFill != "" && defaults.ShapeFill != "none" {
			paint.Color = defaults.ShapeFill
		}
		if !q.Has("outline") && !q.Has("shape-stroke") && defaults.ShapeLine != "" && defaults.ShapeLine != "none" {
			paint.Stroke, paint.StrokeWidth = defaults.ShapeLine, defaults.LineWidth
		}
		if !q.Has("outline") {
			paint = explicitShapeStroke(q, paint, defaults.LineWidth)
		} else {
			paint = explicitShapeStroke(q, paint)
		}
	} else if e.Kind == "connector" && deck.Theme != nil && !validConcreteDefault("default-color", q.Get("default-color")) && !q.Has("fg") && !q.Has("theme-fg") && slide.FG == "" {
		paint.Color = deck.Theme.Colors.Line
	}
	return paint
}

// Resolve each property independently. Never write these values to authored
// metadata: explicit family choice does not detach colour/size inheritance.
func (deck Deck) resolveThemeTextValues(e Element, q url.Values) url.Values {
	q = concreteTextDefaults(q)
	if deck.Theme == nil {
		if q.Get("modern-font") != "theme" {
			return q
		}
		q = cloneQueryValues(q)
		q.Set("modern-font", "c64")
		return q
	}
	q = cloneQueryValues(q)
	font := deck.Theme.Fonts.Body
	if e.Kind == "heading" {
		font = deck.Theme.Fonts.Heading
	} else if e.Kind == "code" {
		font = deck.Theme.Fonts.Mono
	}
	if q.Get("modern-font") == "" || q.Get("modern-font") == "theme" {
		q.Set("modern-font", font.FontID)
	}
	if !q.Has("ttf-size") && !q.Has("modern-size") {
		size := font.Size
		if e.Kind == "heading" && e.Level == 2 {
			size /= 2
		}
		q.Set("modern-size", strconv.FormatFloat(size, 'f', -1, 64))
		// Legacy layout uses a TTF occupancy envelope; match it to the same
		// resolved size instead of placing a 64px text in a 97px envelope.
		q.Set("ttf-size", strconv.Itoa(int(math.Round(size))))
	}
	if !q.Has("ttf-width") && !q.Has("modern-width") {
		q.Set("modern-width", strconv.FormatFloat(font.Width, 'f', -1, 64))
		q.Set("ttf-width", strconv.FormatFloat(font.Width, 'f', -1, 64))
	}
	if !q.Has("modern-line-height") {
		q.Set("modern-line-height", strconv.FormatFloat(font.LineHeight, 'f', -1, 64))
	}
	return q
}

func cloneQueryValues(q url.Values) url.Values {
	copy := make(url.Values, len(q))
	for key, values := range q {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

// Edit nested values without materializing default label paint/alignment or
// dropping future compatible label fields.
func mapShapeLabelQuery(e Element, update func(shapeLabelData, url.Values) url.Values) Element {
	outer, _ := url.ParseQuery(e.Query)
	raw, err := base64.StdEncoding.DecodeString(outer.Get("shape-label"))
	var data shapeLabelData
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &data) != nil {
		return e
	}
	q, _ := url.ParseQuery(data.Query)
	data.Query = update(data, q).Encode()
	next, err := json.Marshal(data)
	if err == nil {
		outer.Set("shape-label", base64.StdEncoding.EncodeToString(next))
		e.Query = outer.Encode()
	}
	return e
}
func (deck Deck) themeCacheKey() string {
	if deck.Theme == nil {
		return deck.themeID()
	}
	data, _ := json.Marshal(deck.Theme)
	sum := sha256.Sum256(data)
	return "embedded:" + hex.EncodeToString(sum[:])
}
func (deck Deck) resolvedDeckTheme() *DeckTheme {
	if deck.Theme != nil {
		return deck.Theme.Clone()
	}
	theme := builtinDeckTheme("c64")
	if colors := deck.themeColors("modern"); colors != nil {
		theme.Colors.Background, theme.Colors.Text, theme.Colors.Heading = colors.Background, colors.Text, colors.Heading
		theme.Name = "Current deck"
	}
	return theme
}
func builtinDeckTheme(id string) *DeckTheme {
	theme := &DeckTheme{Version: 1, Name: "Clean light",
		Colors:   ThemeColors{Background: "#ffffff", Surface: "#eef2f6", Text: "#263445", Heading: "#155e75", Muted: "#64748b", Line: "#94a3b8", Accent: []string{"#0e7490", "#7c3aed", "#ea580c", "#059669", "#dc2626", "#ca8a04"}},
		Fonts:    ThemeFonts{Heading: ThemeFont{FontID: "sans", Size: 160, Width: 100, LineHeight: 1.1}, Body: ThemeFont{FontID: "sans", Size: 64, Width: 100, LineHeight: 1.25}, Mono: ThemeFont{FontID: "mono", Size: 54, Width: 100, LineHeight: 1.3}},
		Defaults: ThemeDefaults{ShapeFill: "#eef2f6", ShapeLine: "#94a3b8", LineWidth: 1},
	}
	switch id {
	case "clean-light":
	case "clean-dark":
		theme.Name = "Clean dark"
		theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading, theme.Colors.Muted, theme.Colors.Line = "#111827", "#1f2937", "#e5e7eb", "#93c5fd", "#9ca3af", "#64748b"
	case "editorial":
		theme.Name = "Editorial"
		theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading = "#fffaf0", "#f5ead9", "#40342b", "#9a3412"
	case "bold":
		theme.Name = "Bold colour"
		theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading = "#312e81", "#4338ca", "#f5f3ff", "#facc15"
		theme.Colors.Muted = "#c4b5fd"
	case "workshop":
		theme.Name = "Workshop"
		theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading = "#f8fafc", "#e0f2fe", "#0f172a", "#0369a1"
	case "c64":
		theme.Name = "C64-inspired"
		theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading, theme.Colors.Muted, theme.Colors.Line = "#101828", "#19243a", "#e6f0ff", "#55ffff", "#a0aec0", "#55aaff"
		theme.Fonts = ThemeFonts{Heading: ThemeFont{FontID: "c64", Size: 386, Width: 100, LineHeight: 1.2}, Body: ThemeFont{FontID: "c64", Size: 97, Width: 100, LineHeight: 1.2}, Mono: ThemeFont{FontID: "c64", Size: 97, Width: 100, LineHeight: 1.2}}
	default:
		return nil
	}
	theme.Defaults.ShapeFill, theme.Defaults.ShapeLine = theme.Colors.Surface, theme.Colors.Line
	return theme
}

func builtinDeckThemes() []*DeckTheme {
	themes := make([]*DeckTheme, 0, 6)
	for _, id := range []string{"clean-light", "clean-dark", "editorial", "bold", "workshop", "c64"} {
		themes = append(themes, builtinDeckTheme(id))
	}
	return themes
}

func (value DeckTheme) MarshalJSON() ([]byte, error) {
	type stored DeckTheme
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"version", "name", "colors", "fonts", "defaults"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *DeckTheme) UnmarshalJSON(data []byte) error {
	type stored DeckTheme
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "name", "colors", "fonts", "defaults")
	if err != nil {
		return err
	}
	*value = DeckTheme(decoded)
	value.Extra = extra
	return nil
}

func (value ThemeColors) MarshalJSON() ([]byte, error) {
	type stored ThemeColors
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"background", "surface", "text", "heading", "muted", "line", "accent"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ThemeColors) UnmarshalJSON(data []byte) error {
	type stored ThemeColors
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "background", "surface", "text", "heading", "muted", "line", "accent")
	if err != nil {
		return err
	}
	*value = ThemeColors(decoded)
	value.Extra = extra
	return nil
}

func (value ThemeFonts) MarshalJSON() ([]byte, error) {
	type stored ThemeFonts
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"heading", "body", "mono"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ThemeFonts) UnmarshalJSON(data []byte) error {
	type stored ThemeFonts
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "heading", "body", "mono")
	if err != nil {
		return err
	}
	*value = ThemeFonts(decoded)
	value.Extra = extra
	return nil
}

func (value ThemeFont) MarshalJSON() ([]byte, error) {
	type stored ThemeFont
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"fontId", "size", "width", "lineHeight"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ThemeFont) UnmarshalJSON(data []byte) error {
	type stored ThemeFont
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "fontId", "size", "width", "lineHeight")
	if err != nil {
		return err
	}
	*value = ThemeFont(decoded)
	value.Extra = extra
	return nil
}

func (value ThemeDefaults) MarshalJSON() ([]byte, error) {
	type stored ThemeDefaults
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"shapeFill", "shapeLine", "lineWidth"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ThemeDefaults) UnmarshalJSON(data []byte) error {
	type stored ThemeDefaults
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "shapeFill", "shapeLine", "lineWidth")
	if err != nil {
		return err
	}
	*value = ThemeDefaults(decoded)
	value.Extra = extra
	return nil
}
