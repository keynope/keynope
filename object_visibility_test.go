package main

import (
	"reflect"
	"testing"
)

func TestObjectVisibilityRenderingPersistenceAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement = nil
	deck.Slides[0].EngagementResult = nil
	s := newNativeEditorSession("Visibility.md", deck)
	before := cloneDeck(s.deck)
	index := -1
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "box" {
			index = i
		}
	}
	e := s.deck.Slides[0].Elements[index]
	e.Query = setQueryValue(e.Query, "object-hidden", "1")
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: index, ElementData: &e}); err != nil {
		t.Fatal(err)
	}
	data, err := serializeDeck("Visibility.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Visibility.md", data)
	if err != nil {
		t.Fatal(err)
	}
	hidden := hiddenSlideObjects(loaded.Slides[0])
	if len(hidden) != 2 {
		t.Fatal("shape and its connector must be hidden", hidden)
	}
	for _, line := range layout(loaded.Slides[0], 245, 56) {
		if hidden[line.Element] {
			t.Fatal("hidden object leaked into Retro output")
		}
	}
	scene, err := buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		if object.ID == "box" || object.ID == "line" {
			t.Fatal("hidden object leaked into Modern output")
		}
	}
	if len(scene.Objects) == 0 {
		t.Fatal("visibility hid unrelated objects")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("visibility undo was not exact")
	}
}

func TestHiddenObjectPreservesFollowingFlow(t *testing.T) {
	slide := Slide{Elements: []Element{{ID: "first", Kind: "text", Text: "First"}, {ID: "second", Kind: "text", Text: "Second"}}}
	row := func(s Slide) int {
		for _, line := range layout(s, 245, 56) {
			if line.Element == 1 {
				return line.Row
			}
		}
		return -1
	}
	before := row(slide)
	slide.Elements[0].Query = "object-hidden=1"
	if after := row(slide); after != before || after < 0 {
		t.Fatalf("hiding changed flow: %d -> %d", before, after)
	}
	canonicalizeSlideElementOrder(&slide, 245, 56)
	if slide.Elements[0].ID != "first" {
		t.Fatal("hidden object moved in reading order")
	}
}
