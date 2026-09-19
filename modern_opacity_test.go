package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestModernOpacityRoundTrip(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement = nil
	deck.Slides[0].EngagementResult = nil
	s := newNativeEditorSession("Opacity.md", deck)
	before := cloneDeck(s.deck)
	element := s.deck.Slides[0].Elements[0]
	element.Query = setQueryValue(element.Query, "modern-opacity", "0.25")
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &element}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Opacity.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		p := scenePaintFor(e, loaded.Slides[0], 1, 1)
		if p.Opacity != nil && *p.Opacity == .25 {
			found = true
		}
	}
	if !found {
		t.Fatal("opacity lost on save/reopen")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("opacity undo changed other fields")
	}
	for _, value := range []string{"-1", "2", "NaN", "Infinity"} {
		element.Query = setQueryValue(element.Query, "modern-opacity", value)
		if scenePaintFor(element, Slide{}, 1, 1).Opacity != nil {
			t.Fatalf("invalid opacity %s", value)
		}
	}
	element.Query = setQueryValue(element.Query, "modern-opacity", "0")
	p := scenePaintFor(element, Slide{}, 1, 1)
	if p.Opacity == nil || *p.Opacity != 0 {
		t.Fatal("zero opacity must not become opaque")
	}
}
