package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLegacyDocumentUsesSharedSceneAtEveryRenderEntry(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, appearance := range []*DeckAppearance{nil, {Version: 1, Mode: "retro"}, {Version: 1, Mode: "modern"}} {
		deck := Deck{Appearance: appearance, Masters: defaultMasterDeck(), Slides: []Slide{{Elements: []Element{
			{ID: "inherited", Kind: "text", Text: "Inherited", Query: "render=truetype&top=2"},
			{ID: "override", Kind: "text", Text: "Modern", Query: "render=truetype&top=12&element-style=modern"},
		}}}}
		before, _ := json.Marshal(deck)
		preview := deck.slideRenderPreview(0, 245, 56)
		if preview.ModernScene == nil {
			t.Fatal("legacy entry bypassed shared scene")
		}
		want := "retro"
		if appearance != nil && appearance.Mode == "modern" {
			want = "modern"
		}
		for _, object := range preview.ModernScene.Objects {
			if object.ID == "inherited" && object.Style != want {
				t.Fatalf("inherited style %s, want %s", object.Style, want)
			}
			if object.ID == "override" && object.Style != "modern" {
				t.Fatal("element override ignored")
			}
		}
		participant, err := participantRenderedDeck(deck, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(participant.Pages[0].Scene, preview.ModernScene) {
			t.Fatal("participant/presenter scene mismatch")
		}
		if deck.masterRenderPreview(0, 245, 56).ModernScene == nil {
			t.Fatal("master bypassed shared scene")
		}
		after, _ := json.Marshal(deck)
		if string(before) != string(after) {
			t.Fatal("render entry changed authored metadata")
		}
	}
}
