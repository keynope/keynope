package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

type themeColors struct{ Background, Text, Heading string }
type pairedTheme struct{ Retro, Modern themeColors }

var deckThemes = map[string]pairedTheme{
	"studio-v1":   {themeColors{"#101828", "#e6f0ff", "#55ffff"}, themeColors{"#ffffff", "#263445", "#155e75"}},
	"midnight-v1": {themeColors{"#080820", "#eeeeff", "#ff55ff"}, themeColors{"#111827", "#e5e7eb", "#93c5fd"}},
	"warm-v1":     {themeColors{"#201008", "#fff0cc", "#ffaa00"}, themeColors{"#fffaf0", "#40342b", "#9a3412"}},
}

func (d Deck) themeID() string {
	if d.Appearance == nil {
		return ""
	}
	var id string
	_ = json.Unmarshal(d.Appearance.Extra["theme"], &id)
	return id
}

func (d Deck) withTheme(id string) (*DeckAppearance, error) {
	if _, ok := deckThemes[id]; id != "" && !ok {
		return nil, fmt.Errorf("unknown theme %q", id)
	}
	a := d.Appearance.Clone()
	if a == nil {
		if id == "" {
			return nil, nil
		}
		a = &DeckAppearance{Version: 3}
	}
	if a.Extra == nil {
		a.Extra = map[string]json.RawMessage{}
	}
	if id == "" {
		delete(a.Extra, "theme")
	} else {
		a.Extra["theme"], _ = json.Marshal(id)
	}
	if len(a.Extra) == 0 {
		a.Extra = nil
	}
	if _, err := encodeDeckAppearance(a); err != nil {
		return nil, err
	}
	return a, nil
}

// Resolved tokens are rendering data, not authored slide/master overrides.
func (d Deck) themeColors(mode string) *themeColors {
	theme, ok := deckThemes[d.themeID()]
	if !ok {
		return nil
	}
	colors := theme.Retro
	if mode == "modern" && !d.usesConcreteAppearance() {
		colors = theme.Modern
	}
	return &colors
}

func themeANSI(hex string, background bool) string {
	value, _ := strconv.ParseUint(hex[1:], 16, 24)
	code := 38
	if background {
		code = 48
	}
	return fmt.Sprintf("%d;2;%d;%d;%d", code, value>>16, (value>>8)&255, value&255)
}

// Theme typography is resolved for Modern only. Explicit element/master sizes
// and imported glyph sizing retain their existing optical-size interpretation.
// Resolve the active typography profile independently of the painter. Legacy
// Retro rasters still own their paint during migration, but their semantic
// payload must not describe a different font/size to the shared editor.
func (d Deck) sceneTextForStyle(element Element, style string) *sceneText {
	q, _ := url.ParseQuery(element.Query)
	return d.sceneTextForStyleFromValues(element, style, q)
}

func (d Deck) sceneTextForStyleFromValues(element Element, style string, q url.Values) *sceneText {
	if style != "retro" {
		return d.modernSceneTextFromValues(element, q)
	}
	text := sceneTextFromValues(element, q)
	text.FontID, text.Family = "c64", "KeynopeC64, monospace"
	text.Size = float64(trueTypeSizeWithValues(element, q))
	text.WidthScale = .4167 * trueTypeWidthWithValues(q) / 100
	text.EmojiWidthScale = 1 / .4167
	text.LineHeight = 1.2
	text.ParagraphBefore, text.ParagraphAfter = 0, 0
	return text
}

func (d Deck) modernSceneText(element Element) *sceneText {
	q, _ := url.ParseQuery(element.Query)
	return d.modernSceneTextFromValues(element, q)
}

func (d Deck) modernSceneTextFromValues(element Element, q url.Values) *sceneText {
	if d.usesConcreteAppearance() && !q.Has("modern-font") {
		cloned := make(url.Values, len(q)+1)
		for key, values := range q {
			cloned[key] = append([]string(nil), values...)
		}
		q = cloned
		q.Set("modern-font", "c64")
	}
	text := sceneTextFromValues(element, q)
	// An explicit Modern fitting adjustment is independent of Retro typography.
	defer func() {
		if size, err := strconv.ParseFloat(q.Get("modern-size"), 64); err == nil && size >= 1 && size <= 1024 {
			text.Size = size
		}
	}()
	if d.usesConcreteAppearance() {
		// Every font uses the same authored size. The old Modern profile used a
		// half-size scale, which made a font-only change unexpectedly resize text.
		text.Size *= 2
		if text.FontID == "c64" {
			// Authored 100% is the established Keynope C64 optical width, not
			// the raw square-cell width of the source face.
			text.WidthScale *= .4167
			text.EmojiWidthScale = 1 / .4167
			if !q.Has("modern-line-height") {
				text.LineHeight = 1.2
			}
		}
		return text
	}
	scale := d.appearanceTextScale("modern")
	text.Size *= scale / .5
	if _, ok := deckThemes[d.themeID()]; !ok {
		return text
	}
	if !q.Has("ttf-size") && !q.Has("text-size") && !q.Has("scale") {
		size := 36.0
		switch element.Kind {
		case "heading":
			size = 96
			if element.Level == 2 {
				size = 64
			}
		case "code":
			size = 30
		case "page-number":
			size = 24
		}
		text.Size = size * scale / .5
	}
	if !q.Has("modern-line-height") {
		text.LineHeight = 1.3
		if element.Kind == "heading" {
			text.LineHeight = 1.1
		}
		if element.Kind == "code" {
			text.LineHeight = 1.4
		}
	}
	return text
}
