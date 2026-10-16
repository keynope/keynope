package main

import (
	"fmt"
	"reflect"
	"testing"
)

func layerScopeSession(master bool) *nativeEditorSession {
	slide := Slide{Elements: []Element{
		{ID: "a", Kind: "text", Text: "A", Query: "top=2&left=10&render=truetype"},
		{ID: "b", Kind: "text", Text: "B", Query: "top=12&left=10&render=truetype"},
		{ID: "c", Kind: "text", Text: "C", Query: "top=22&left=10&render=truetype"},
	}}
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{cloneSlide(slide), cloneSlide(slide)}}
	deck.Masters.Base.Slide = cloneSlide(slide)
	s := newNativeEditorSession("Layers.md", deck)
	s.masterMode, s.selected, s.selection = master, 0, map[int]bool{0: true}
	return s
}

func TestLayerCommandScopeAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, drop := range []bool{false, true} {
			t.Run(fmt.Sprintf("master=%v/drop=%v", master, drop), func(t *testing.T) {
				s := layerScopeSession(master)
				before, version, mutation := cloneDeck(s.deck), s.version, s.mutationRevision
				action := nativeEditorAction{Action: "move-object-layer", ObjectID: "a", Value: 1, SceneMaster: master, SceneRevision: &version, MutationRevision: &mutation, NotesSessionID: s.notesSessionID}
				if drop {
					action.Name, action.Kind = "c", "after"
				}
				for _, fault := range []string{"revision", "mutation", "slide", "mode", "owner"} {
					bad := action
					switch fault {
					case "revision":
						stale := version - 1
						bad.SceneRevision = &stale
					case "mutation":
						stale := mutation + 1
						bad.MutationRevision = &stale
					case "slide":
						bad.Slide = 1
					case "mode":
						bad.SceneMaster = !master
					case "owner":
						bad.NotesSessionID = "another-document"
					}
					state := s.state()
					if err := s.apply(bad); err == nil || !reflect.DeepEqual(state, s.state()) || !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 || len(s.redo) != 0 || s.mutationRevision != mutation {
						t.Fatal("invalid layer scope changed state", fault, err)
					}
				}
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				if s.version != version+1 || s.mutationRevision != mutation+1 || len(s.undo) != 1 {
					t.Fatal("layer command did not create exactly one mutation")
				}
				after := cloneDeck(s.deck)
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("layer undo changed source", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
					t.Fatal("layer redo changed accepted order", err)
				}
				fresh := layerScopeSession(master) // Same IDs/version, different owner.
				state := fresh.state()
				if err := fresh.apply(action); err == nil || !reflect.DeepEqual(state, fresh.state()) || len(fresh.undo) != 0 {
					t.Fatal("layer gesture crossed document ownership", err)
				}
			})
		}
	}
}

func TestLayerBoundaryNoopsKeepRevisionAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, command := range []nativeEditorAction{
			{Action: "move-object-layer", ObjectID: "a", Value: -1},
			{Action: "move-object-layer", ObjectID: "c", Value: 1},
			{Action: "move-object-layer", ObjectID: "a", Name: "a", Kind: "after"},
		} {
			s := layerScopeSession(master)
			before, deck, mutation := s.state(), cloneDeck(s.deck), s.mutationRevision
			version := s.version
			command.SceneRevision, command.SceneMaster, command.NotesSessionID = &version, master, s.notesSessionID
			if err := s.apply(command); err != nil || !reflect.DeepEqual(before, s.state()) || !reflect.DeepEqual(deck, s.deck) || s.mutationRevision != mutation || len(s.undo) != 0 || len(s.redo) != 0 {
				t.Fatal("boundary reorder changed revision/history", master, command.ObjectID, err)
			}
		}
	}
}
