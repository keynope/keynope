package app

import (
	"reflect"
	"testing"
)

func TestEditorSingleElementNoOpPreservesRevisionHistoryAndRedo(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			s := atomicEditorFixture(master)
			owner := func() *Slide {
				if master {
					return &s.deck.Masters.Base.Slide
				}
				return &s.deck.Slides[0]
			}
			owner().Elements[0].Extra = extensionFixture("future-property", `{"owned":true}`)
			s.savedDeck = cloneDeck(s.deck)
			changed := cloneElement(owner().Elements[0])
			changed.Text += " changed"
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &changed}); err != nil {
				t.Fatal(err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			before := cloneDeck(s.deck)
			version, mutation, undo, redo := s.version, s.mutationRevision, len(s.undo), len(s.redo)
			if redo != 1 {
				t.Fatal("fixture did not retain redo")
			}
			copy := cloneElement(owner().Elements[0])
			copy.Extra = nil // Missing future fields preserve the original extension.
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &copy}); err != nil {
				t.Fatal(err)
			}
			if s.version != version || s.mutationRevision != mutation || len(s.undo) != undo || len(s.redo) != redo || !reflect.DeepEqual(s.deck, before) || s.state().Dirty {
				t.Fatal("equal update dirtied the document, changed revision or destroyed history")
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || owner().Elements[0].Text != changed.Text {
				t.Fatal("redo was no longer usable", err)
			}
		})
	}
}
