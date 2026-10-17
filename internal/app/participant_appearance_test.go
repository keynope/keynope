package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestParticipantAppearanceRenderingFieldsAndPrivacy(t *testing.T) {
	for _, source := range []*DeckAppearance{nil,
		{Version: 1, Mode: "modern"},
		{Version: 2, DefaultStyle: "modern"},
		{Version: 3},
	} {
		if source != nil {
			source.Extra = map[string]json.RawMessage{
				"private":  json.RawMessage(`"PRIVATE_APPEARANCE"`),
				"profiles": json.RawMessage(`{"modern":{"textScale":0.75,"private":"PRIVATE_PROFILE"},"retro":{"textScale":1.2},"future":{"private":"PRIVATE_SCOPE"}}`),
			}
		}
		before := source.Clone()
		got := participantAppearance(source)
		wire, err := json.Marshal(got)
		if err != nil || bytes.Contains(wire, []byte("PRIVATE")) || !reflect.DeepEqual(source, before) {
			t.Fatal("participant appearance leaked extras or changed source", err)
		}
		if source == nil {
			if got != nil {
				t.Fatal("implicit legacy policy was changed")
			}
			continue
		}
		if got.Version != source.Version || got.Mode != source.Mode || got.DefaultStyle != source.DefaultStyle {
			t.Fatal("rendering policy changed", source, got)
		}
		if source.Version < 3 {
			for _, mode := range []string{"retro", "modern"} {
				if (Deck{Appearance: source}).appearanceTextScale(mode) != (Deck{Appearance: got}).appearanceTextScale(mode) {
					t.Fatal("known profile text scale changed", mode)
				}
			}
		} else if got.Extra != nil {
			t.Fatal("v3 exposed unused profiles")
		}
		// Returned raw fields are owned, never aliases into source metadata.
		if got.Extra != nil {
			got.Extra["profiles"][0] = '['
			if !reflect.DeepEqual(source, before) {
				t.Fatal("public profile bytes alias the source")
			}
		}
	}
}

func TestParticipantMarkdownConcreteCanvasAndTypography(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []*DeckPageSize{nil, {Version: 1, Width: 1080, Height: 1920}, {Version: 1, Width: 2560, Height: 1080}} {
		for _, font := range []string{"c64", "mono", "sans"} {
			d := Deck{PageSize: size.Clone(), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "public", PageNumber: pageNumberHide, Elements: []Element{{ID: "type", Kind: "text", Text: "Public page", Query: "render=truetype&modern-font=" + font + "&modern-size=64&modern-width=80&text-align=right&text-valign=bottom&text-box=1&width=150&height=20&left=10&top=3"}}}}}
			d.Appearance.Extra = map[string]json.RawMessage{"private": json.RawMessage(`"PRIVATE_PAGE_POLICY"`)}
			before := cloneDeck(d)
			wire, err := participantSlideMarkdown(d, 0)
			if err != nil || bytes.Contains(wire, []byte("PRIVATE")) {
				t.Fatal("public page serialization", err)
			}
			loaded, err := parseDeckData("Presentation.md", wire)
			if err != nil || !reflect.DeepEqual(loaded.PageSize, size) || !loaded.usesConcreteAppearance() {
				t.Fatal("public canvas/policy changed", err)
			}
			pages := participantMarkdownExportPages(loaded, 5, 20, 245, 56)
			if len(pages) != 1 || pages[0].Scene == nil || pages[0].Slide != 5 || pages[0].SlideCount != 20 {
				t.Fatal("participant projection/position missing")
			}
			scene := pages[0].Scene
			w, h := d.pageDimensions()
			if scene.Width != w || scene.Height != h || len(scene.Objects) != 1 || scene.Objects[0].Text == nil {
				t.Fatal("participant canvas/text changed")
			}
			text := scene.Objects[0].Text
			if text.FontID != font || text.Size != 64 || text.Align != "end" || text.Vertical != "bottom" {
				t.Fatal("participant concrete typography changed", text)
			}
			if !reflect.DeepEqual(before, d) {
				t.Fatal("participant projection changed the source")
			}
		}
	}
}
