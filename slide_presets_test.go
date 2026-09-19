package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestSlidePresets(t *testing.T) {
	for _, name := range []string{"title", "section", "body", "columns", "quote", "comparison"} {
		t.Run(name, func(t *testing.T) {
			preset, err := slidePreset(name, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			second, err := slidePreset(name, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			if len(preset.Elements) < 2 {
				t.Fatal("empty preset")
			}
			for i, e := range preset.Elements {
				q, _ := url.ParseQuery(e.Query)
				if e.ID == "" || e.ID == second.Elements[i].ID {
					t.Fatal("preset IDs must be fresh")
				}
				if q.Has("fg") || q.Has("header") || q.Has("ttf-size") {
					t.Fatal("preset overrides theme")
				}
			}
			s := newNativeEditorSession("Presets.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Existing"}}}}})
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "add-slide", Name: name}); err != nil {
				t.Fatal(err)
			}
			if s.current != 1 || len(s.deck.Slides) != 2 || len(s.undo) != 1 {
				t.Fatal("insertion transaction")
			}
			for _, mode := range []string{"retro", "modern"} {
				s.deck.Appearance, _ = s.deck.withTheme("studio-v1")
				s.deck.Appearance, err = s.deck.withDefaultStyle(mode)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := buildSlideScene(s.deck, 1, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				if len(scene.Objects) != len(preset.Elements) {
					t.Fatal("preset objects missing")
				}
				if mode == "retro" {
					for _, line := range layout(s.deck.ResolveSlide(1, false), 245, 56) {
						if line.Row < 0 || line.Row >= 56 {
							t.Fatalf("Retro preset overflow at row %d", line.Row)
						}
					}
				}
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck, before) {
				t.Fatal("undo must restore deck")
			}
		})
	}
	if _, err := slidePreset("missing", 245, 56); err == nil {
		t.Fatal("unknown preset accepted")
	}
}
