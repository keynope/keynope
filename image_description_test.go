package main

import (
	"os"
	"reflect"
	"testing"
)

func TestImageDescriptionPersistence(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Description.md", deck)
	before := cloneDeck(s.deck)
	index := -1
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "image" {
			index = i
		}
	}
	e := s.deck.Slides[0].Elements[index]
	description := "Blue & violet sample — <not HTML>"
	e.Query = setQueryValue(e.Query, "alt-text", description)
	e.Query = setQueryValue(e.Query, "image-decorative", "1")
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: index, ElementData: &e}); err != nil {
		t.Fatal(err)
	}
	data, err := serializeDeck("Description.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Description.md", data)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "image" {
			found = true
			if object.Media == nil || object.Media.Alt != description || !object.Media.Decorative {
				t.Fatal("image description lost", object.Media)
			}
		}
	}
	if !found {
		t.Fatal("image missing")
	}
	loaded.Appearance = &DeckAppearance{Version: 1, Mode: "modern"}
	participant, err := participantRenderedDeck(loaded, 0)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, page := range participant.Pages {
		if page.Scene == nil {
			continue
		}
		for _, object := range page.Scene.Objects {
			if object.ID == "image" && object.Media != nil && object.Media.Alt == description && object.Media.Decorative {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("participant projection lost image accessibility metadata")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("description undo changed deck")
	}
	if path := os.Getenv("KEYNOPE_DESCRIPTION_FIXTURE"); path != "" {
		data, err = serializeDeck(path, s.deck)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
