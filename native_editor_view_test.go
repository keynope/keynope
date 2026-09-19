package main

import (
	"fmt"
	"reflect"
	"testing"
)

func viewCommandFixture(count int, master bool) *nativeEditorSession {
	deck := Deck{Slides: make([]Slide, count)}
	for i := range deck.Slides {
		deck.Slides[i].Elements = []Element{{ID: fmt.Sprint("a", i), Kind: "text", Text: "First", Query: "group=pair"}, {ID: fmt.Sprint("b", i), Kind: "text", Text: "Second", Query: "group=pair"}}
	}
	s := newNativeEditorSession("View.md", deck)
	s.deck.Masters.Base.Slide.Elements = cloneSlide(s.deck.Slides[0]).Elements
	s.masterMode = master
	return s
}

func TestViewCommandsPreserveDeckAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := viewCommandFixture(3, master)
		before := cloneDeck(s.deck)
		commands := []nativeEditorAction{
			{Action: "select-element", Element: 0},
			{Action: "select-elements", ElementIndices: []int{0, 1}, ElementsData: s.deck.Slides[0].Elements},
			{Action: "enter-group", Element: 0},
			{Action: "exit-group"},
			{Action: "select-slide", Slide: 0},
			{Action: "next-slide"}, {Action: "previous-slide"},
		}
		if !master {
			commands = append(commands, nativeEditorAction{Action: "navigate-presentation", Slide: 1}, nativeEditorAction{Action: "start-timer", Value: 60}, nativeEditorAction{Action: "stop-timer"})
		}
		for _, command := range commands {
			if !nativeEditorViewAction(command.Action) {
				t.Fatalf("unclassified %s", command.Action)
			}
			version := s.version
			if err := s.apply(command); err != nil {
				t.Fatalf("master=%t %s: %v", master, command.Action, err)
			}
			if s.version <= version || !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 || len(s.redo) != 0 {
				t.Fatalf("master=%t %s mutated document/history or failed to publish view state", master, command.Action)
			}
		}
	}
	if nativeEditorViewAction("new-future-mutation") {
		t.Fatal("unknown commands must retain history snapshots")
	}
}

func TestSelectionAllocationsIndependentOfUnrelatedSlides(t *testing.T) {
	for _, master := range []bool{false, true} {
		allocs := func(count int) float64 {
			s := viewCommandFixture(count, master)
			command := nativeEditorAction{Action: "select-elements", ElementIndices: []int{0, 1}, ElementsData: s.deck.Slides[0].Elements}
			return testing.AllocsPerRun(20, func() {
				if err := s.apply(command); err != nil {
					t.Fatal(err)
				}
			})
		}
		small, large := allocs(1), allocs(1000)
		t.Logf("master=%t selection allocations: 1 slide %.0f, 1000 slides %.0f", master, small, large)
		if large > small+5 {
			t.Fatalf("master=%t selection allocates by deck size: small %.0f, large %.0f", master, small, large)
		}
	}
}
