package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestObjectRotationProfilesAndMarkdown(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := sceneFixture(t)
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		deck.Slides[0].Engagement = nil
		deck.Slides[0].EngagementResult = nil
		before, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for i, e := range deck.Slides[0].Elements {
			if e.Kind != "connector" {
				deck.Slides[0].Elements[i].Query = setQueryValue(e.Query, "object-rotation", "37.5")
			}
		}
		after, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for i, o := range after.Objects {
			if o.Kind == "connector" {
				continue
			}
			if o.Rotation != 37.5 {
				t.Fatalf("%s/%s angle %g", style, o.ID, o.Rotation)
			}
			o.Rotation = before.Objects[i].Rotation
			if !reflect.DeepEqual(o, before.Objects[i]) {
				t.Fatalf("%s/%s changed local payload", style, o.ID)
			}
		}
		path := filepath.Join(t.TempDir(), "Angles.md")
		data, err := serializeDeck(path, deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		scene, err := buildMixedSlideScene(loaded, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range scene.Objects {
			if o.Kind != "connector" && o.Rotation != 37.5 {
				t.Fatalf("%s/%s lost angle after reload", style, o.ID)
			}
		}
	}
}
