package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEditorAlwaysUsesUnifiedAppearance(t *testing.T) {
	for _, mode := range []string{"", "retro", "modern"} {
		t.Run(mode, func(t *testing.T) {
			deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Plain deck"}}}}}
			wantFont := "c64"
			if mode != "" {
				if mode == "modern" {
					wantFont = "sans"
				}
				deck.Appearance = &DeckAppearance{Version: 1, Mode: mode, Extra: map[string]json.RawMessage{"future": json.RawMessage(`{"preserved":true}`)}}
			}
			before, err := json.Marshal(deck)
			if err != nil {
				t.Fatal(err)
			}
			session := newNativeEditorSession("Plain.md", deck)
			after, err := json.Marshal(deck)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("opening mutated caller's deck")
			}
			if !session.deck.usesConcreteAppearance() || session.deck.Appearance.Version != 3 || session.state().Dirty {
				t.Fatal("opening must normalize to a clean concrete-style session")
			}
			if mode != "" && !reflect.DeepEqual(session.deck.Appearance.Extra, deck.Appearance.Extra) {
				t.Fatal("unknown appearance data lost")
			}
			scene := session.deck.slideRenderPreview(0, 245, 56).ModernScene
			if scene == nil || len(scene.Objects) == 0 || scene.Objects[0].Style != "modern" || scene.Objects[0].Text == nil || scene.Objects[0].Text.FontID != wantFont || scene.Objects[0].RetroLines != nil {
				t.Fatal("default text did not enter shared scene")
			}
			if session.deck.masterRenderPreview(0, 245, 56).ModernScene == nil {
				t.Fatal("master did not enter shared scene")
			}
			data, err := serializeDeck("Plain.md", session.deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Plain.md", data)
			if err != nil {
				t.Fatal(err)
			}
			if !reopened.usesConcreteAppearance() || reopened.Appearance.Version != 3 {
				t.Fatal("saved migration did not persist")
			}
		})
	}
}
