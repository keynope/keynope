package main

import "testing"

func TestHistorySelectionFollowsIdentity(t *testing.T) {
	for _, master := range []bool{false, true} {
		a := Element{ID: "a", Kind: "text", Text: "A", Query: "group=g"}
		b := Element{ID: "b", Kind: "text", Text: "B", Query: "group=g"}
		s := newNativeEditorSession("History.md", Deck{Slides: []Slide{{Elements: []Element{b, a}}}})
		s.masterMode = master
		if master {
			s.currentMaster = 0
			s.deck.Masters.Base.Slide.Elements = []Element{b, a}
		}
		previous := cloneDeck(s.deck)
		if master {
			previous.Masters.Base.Slide.Elements = []Element{a, b}
		} else {
			previous.Slides[0].Elements = []Element{a, b}
		}
		s.undo = []Deck{previous}
		s.selected = 1
		s.selection = map[int]bool{0: true, 1: true}
		s.editingGroup = "g"
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if s.selected != 0 || len(s.selection) != 2 || s.editingGroup != "g" {
			t.Fatalf("master=%v undo lost selection: %d %v %s", master, s.selected, s.selection, s.editingGroup)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		if s.selected != 1 || len(s.selection) != 2 || s.editingGroup != "g" {
			t.Fatalf("master=%v redo lost selection", master)
		}
	}
}

func TestHistorySelectionDropsMissingOrAmbiguousObjects(t *testing.T) {
	a := Element{ID: "a", Kind: "text", Query: "group=g"}
	b := Element{ID: "b", Kind: "text"}
	for _, restored := range [][]Element{nil, {b}, {a, a}, {{ID: "a", Inherited: true}}} {
		s := &nativeEditorSession{selected: 0, selection: map[int]bool{0: true}, editingGroup: "g"}
		s.retainHistorySelection([]Element{a}, restored)
		if s.selected != -1 || len(s.selection) != 0 {
			t.Fatal("selected a missing/ambiguous/inherited target")
		}
	}
	s := &nativeEditorSession{selected: 0, selection: map[int]bool{0: true, 1: true}, editingGroup: "g"}
	s.retainHistorySelection([]Element{a, b}, []Element{b})
	if s.selected != 0 || len(s.selection) != 1 || s.editingGroup != "" {
		t.Fatal("remaining member not selected or stale group retained")
	}
}
