package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
)

// Appearance v3 deliberately has no Retro/Modern deck mode. Typography is a
// font choice, images own an image-style, and shapes/connectors use the shared
// vector model. Versions 1 and 2 remain readable so older decks can be opened
// without changing their presentation before an editor session is created.
func (d Deck) usesConcreteAppearance() bool {
	return d.Theme != nil || d.Appearance != nil && d.Appearance.Version >= 3
}

func concreteTextFont(kind, style string, q url.Values) string {
	if font := q.Get("modern-font"); validTextFont(font) || font == "theme" {
		return font
	}
	if style == "modern" {
		if kind == "code" {
			return "mono"
		}
		return "sans"
	}
	return "c64"
}

func migrateConcreteElement(element *Element, inheritedStyle string) {
	if element == nil {
		return
	}
	if !standardElementKind(element.Kind) {
		return
	}
	q, _ := url.ParseQuery(element.Query)
	style := q.Get("element-style")
	if !validElementStyle(style) {
		style = inheritedStyle
	}
	q.Del("element-style")
	switch element.Kind {
	case "heading", "text", "bullet", "code", "text-image", "page-number":
		if q.Has("modern-font") || element.MasterSlotID == "" && !validConcreteDefault("default-font", q.Get("default-font")) {
			q.Set("modern-font", concreteTextFont(element.Kind, style, q))
		}
	case "image":
		if !validElementStyle(q.Get("image-style")) {
			q.Set("image-style", style)
		}
	case "shape":
		if encoded := q.Get("shape-label"); encoded != "" {
			if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(raw) <= 65536 {
				var label shapeLabelData
				if json.Unmarshal(raw, &label) == nil {
					lq, _ := url.ParseQuery(label.Query)
					labelStyle := lq.Get("element-style")
					if !validElementStyle(labelStyle) {
						labelStyle = style
					}
					lq.Del("element-style")
					if lq.Has("modern-font") || !validConcreteDefault("default-font", lq.Get("default-font")) {
						lq.Set("modern-font", concreteTextFont("text", labelStyle, lq))
					}
					label.Query = lq.Encode()
					if next, err := json.Marshal(label); err == nil {
						q.Set("shape-label", base64.StdEncoding.EncodeToString(next))
					}
				}
			}
		}
	}
	element.Query = q.Encode()
}

func migrateConcreteSlide(slide *Slide, inheritedStyle string) {
	if slide == nil {
		return
	}
	style := slide.DefaultStyle
	if !validElementStyle(style) {
		style = inheritedStyle
	}
	for index := range slide.Elements {
		migrateConcreteElement(&slide.Elements[index], style)
	}
	slide.DefaultStyle = ""
}

// canonicalizeConcreteAppearance runs at the editor boundary. It is not a
// user edit and therefore becomes part of the session baseline rather than
// making a just-opened deck dirty.
func canonicalizeConcreteAppearance(deck *Deck) {
	if deck == nil {
		return
	}
	// Portable theme documents retain optional font association. Opening one
	// must not replace its inherited families with the legacy C64 default.
	if deck.Theme != nil {
		markThemeFonts := func(slide *Slide) {
			for i := range slide.Elements {
				e := &slide.Elements[i]
				q, _ := url.ParseQuery(e.Query)
				switch e.Kind {
				case "text", "heading", "bullet", "code", "text-image", "page-number":
					if !q.Has("modern-font") && e.MasterSlotID == "" && !validConcreteDefault("default-font", q.Get("default-font")) {
						q.Set("modern-font", "theme")
						e.Query = q.Encode()
					}
				case "shape":
					*e = mapShapeLabelQuery(*e, func(label shapeLabelData, lq url.Values) url.Values {
						if !lq.Has("modern-font") && !validConcreteDefault("default-font", lq.Get("default-font")) {
							lq.Set("modern-font", "theme")
						}
						return lq
					})
				}
			}
		}
		markThemeFonts(&deck.Masters.Base.Slide)
		for i := range deck.Masters.Layouts {
			markThemeFonts(&deck.Masters.Layouts[i].Slide)
		}
		for i := range deck.Slides {
			markThemeFonts(&deck.Slides[i])
		}
	}
	deckStyle := deck.AppearanceMode()
	baseStyle := deck.Masters.Base.Slide.DefaultStyle
	if !validElementStyle(baseStyle) {
		baseStyle = deckStyle
	}
	layoutStyles := make(map[string]string, len(deck.Masters.Layouts))
	for index := range deck.Masters.Layouts {
		style := deck.Masters.Layouts[index].Slide.DefaultStyle
		if !validElementStyle(style) {
			style = baseStyle
		}
		layoutStyles[deck.Masters.Layouts[index].ID] = style
		migrateConcreteSlide(&deck.Masters.Layouts[index].Slide, style)
	}
	migrateConcreteSlide(&deck.Masters.Base.Slide, deckStyle)
	for index := range deck.Slides {
		style := baseStyle
		if layoutStyle, ok := layoutStyles[deck.Slides[index].LayoutID]; ok {
			style = layoutStyle
		}
		migrateConcreteSlide(&deck.Slides[index], style)
	}
	extra := map[string]json.RawMessage(nil)
	if deck.Appearance != nil && deck.Appearance.Extra != nil {
		extra = deck.Appearance.Clone().Extra
	}
	deck.Appearance = &DeckAppearance{Version: 3, Extra: extra}
}
