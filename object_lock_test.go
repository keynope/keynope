package main

import (
	"reflect"
	"testing"
)

func TestObjectLocks(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("Locks.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "a", Kind: "text", Text: "Keep me", Query: "top=3"}}}}})
		if master {
			if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
				t.Fatal(err)
			}
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		}
		if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
			t.Fatal(err)
		}
		before := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "set-object-lock", Value: 1}); err != nil {
			t.Fatal(err)
		}
		locked := cloneDeck(s.deck)
		for _, action := range []nativeEditorAction{
			{Action: "delete-element", Element: 0},
			{Action: "update-element", Element: 0, ElementData: &Element{ID: "a", Kind: "text", Text: "Changed"}},
			{Action: "update-elements", ElementIndices: []int{0}, ElementsData: []Element{{ID: "a", Kind: "text", Text: "Changed"}}},
			{Action: "convert-text-kind", Element: 0, Kind: "heading", Level: 1},
			{Action: "move-object-layer", ObjectID: "a", Value: 1},
		} {
			if err := s.apply(action); err == nil {
				t.Fatalf("locked %s accepted (master=%v)", action.Action, master)
			}
			if !reflect.DeepEqual(locked, s.deck) {
				t.Fatal("rejected action mutated deck")
			}
		}
		data, err := serializeDeck("Locks.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Locks.md", data)
		if err != nil {
			t.Fatal(err)
		}
		target := loaded.Slides[0]
		if master {
			target = loaded.Masters.Base.Slide
		}
		if !objectLocked(target.Elements[0]) {
			t.Fatal("lock lost on reopen")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, s.deck) {
			t.Fatal("lock undo was not exact")
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(locked, s.deck) {
			t.Fatal("lock redo was not exact")
		}
		if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "set-object-lock", Value: 0}); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "delete-element", Element: 0}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroupLockPreflight(t *testing.T) {
	slide := Slide{Elements: []Element{{ID: "a", Query: "group=g"}, {ID: "b", Query: "group=g&object-locked=1"}}}
	action := nativeEditorAction{Action: "update-element", Element: 0}
	if checkObjectLocks(slide, action, nil, "") == nil {
		t.Fatal("collapsed group allowed mutation")
	}
	if err := checkObjectLocks(slide, action, nil, "g"); err != nil {
		t.Fatal("unlocked individual in entered group", err)
	}
	if checkObjectLocks(slide, nativeEditorAction{Action: "ungroup-elements"}, map[int]bool{0: true}, "g") == nil {
		t.Fatal("ungroup modified a locked unselected member")
	}
}
