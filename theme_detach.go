package main

import (
	"encoding/json"
	"fmt"
)

// Construct an owned candidate without changing the source or flattening master
// relationships. The command boundary owns final save-budget/concurrency checks.
func concreteThemeDeck(source Deck) (Deck, error) {
	result := cloneDeck(source)
	if source.Theme == nil {
		return result, nil
	}
	if err := source.Theme.Validate(); err != nil {
		return Deck{}, err
	}
	convert := func(slide *Slide) error {
		slots := source.masterSlotMap(slide.LayoutID)
		for i, element := range slide.Elements {
			// Bound instances render with their placeholder's kind/level. Use
			// that role when unlinking an explicit theme font, but retain the
			// authored instance and its inheritance link in the stored deck.
			effective := element
			if slot, ok := slots[element.MasterSlotID]; ok {
				effective.Kind, effective.Level = slot.Kind, slot.Level
			}
			converted, err := concreteThemeSourceElement(source, effective)
			if err != nil {
				return err
			}
			converted.Kind, converted.Level = element.Kind, element.Level
			slide.Elements[i] = converted
		}
		if slide.BackgroundMedia != nil {
			q := mustQuery(slide.BackgroundMedia.Query)
			if !q.Has("default-color") {
				q.Set("default-color", source.Theme.Colors.Text)
			}
			slide.BackgroundMedia.Query = q.Encode()
		}
		// An explicit clear still used the theme's canvas colour before removal.
		if slide.BGSet && slide.BG == "" {
			slide.BG, _ = ansiBG(source.Theme.Colors.Background)
		}
		return nil
	}
	if err := convert(&result.Masters.Base.Slide); err != nil {
		return Deck{}, err
	}
	if deckHasMasterData(result.Masters) && result.Masters.Base.Slide.BG == "" {
		result.Masters.Base.Slide.BG, _ = ansiBG(source.Theme.Colors.Background)
		result.Masters.Base.Slide.BGSet = true
	}
	for i := range result.Masters.Layouts {
		if err := convert(&result.Masters.Layouts[i].Slide); err != nil {
			return Deck{}, err
		}
	}
	for i := range result.Slides {
		slide := &result.Slides[i]
		if err := convert(slide); err != nil {
			return Deck{}, err
		}
		_, layout := result.Masters.Layout(slide.LayoutID)
		if (slide.LayoutID == "" || !layout) && slide.BG == "" {
			slide.BG, _ = ansiBG(source.Theme.Colors.Background)
			slide.BGSet = true
		}
	}
	if result.Objects != nil {
		for id, table := range result.Objects.Tables {
			converted, err := concreteThemeTable(source, table)
			if err != nil {
				return Deck{}, err
			}
			result.Objects.Tables[id] = converted
		}
		for id, chart := range result.Objects.Charts {
			converted, _, err := concreteThemeChart(source, Element{Kind: "chart"}, chart)
			if err != nil {
				return Deck{}, err
			}
			result.Objects.Charts[id] = converted
		}
		if err := result.Objects.Validate(); err != nil {
			return Deck{}, err
		}
	}
	if err := archiveDetachedThemeExtensions(&result, source.Theme); err != nil {
		return Deck{}, err
	}
	extra := map[string]json.RawMessage(nil)
	if result.Appearance != nil {
		extra = result.Appearance.Clone().Extra
	}
	result.Appearance = &DeckAppearance{Version: 3, Extra: extra}
	result.Theme = nil
	if err := validateDeckPlacementQueries(result); err != nil {
		return Deck{}, err
	}
	if err := validateDeckObjectReferences(result); err != nil {
		return Deck{}, err
	}
	return result, nil
}

// Unknown compatible theme data may refer to assets/objects. Preserve only
// opaque extensions as inert document metadata; never retain a live hidden
// theme or overwrite a pre-existing extension namespace.
func archiveDetachedThemeExtensions(deck *Deck, theme *DeckTheme) error {
	fields := map[string]*jsonExtensions{
		"theme": theme.Extra, "colors": theme.Colors.Extra, "fonts": theme.Fonts.Extra,
		"heading": theme.Fonts.Heading.Extra, "body": theme.Fonts.Body.Extra, "mono": theme.Fonts.Mono.Extra, "defaults": theme.Defaults.Extra,
	}
	for key, extra := range fields {
		if extra == nil || len(*extra) == 0 {
			delete(fields, key)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if deck.Extra == nil {
		deck.Extra = &jsonExtensions{}
	}
	key := "detached-theme-extensions"
	for i := 2; ; i++ {
		if _, exists := (*deck.Extra)[key]; !exists {
			break
		}
		key = fmt.Sprintf("detached-theme-extensions-%d", i)
	}
	(*deck.Extra)[key] = data
	return nil
}
