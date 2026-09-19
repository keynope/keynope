package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tabTestSession(t *testing.T) *nativeEditorSession {
	t.Helper()
	d := Deck{}
	for _, name := range []string{"First", "Reference", "Third", "Resources", "Last"} {
		d.Slides = append(d.Slides, Slide{Elements: []Element{{Kind: "text", Text: name}}})
	}
	return newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), d)
}

func tabAction(t *testing.T, s *nativeEditorSession, a nativeEditorAction) {
	t.Helper()
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
}

func TestSlideTabToggleOrderRoundTripAndRestore(t *testing.T) {
	s := tabTestSession(t)
	original := cloneDeck(s.deck)
	for _, i := range []int{1, 3} {
		tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: i})
		tabAction(t, s, nativeEditorAction{Action: "toggle-slide-tab"})
	}
	if len(s.deck.Tabs) != 2 || s.deck.Tabs[0].Name != "Reference" || s.deck.Tabs[1].Name != "Resources" {
		t.Fatalf("tabs: %+v", s.deck.Tabs)
	}
	tabAction(t, s, nativeEditorAction{Action: "reorder-slide-tab", Slide: 3, Value: 0})
	if s.deck.Tabs[0].Page != 4 || s.deck.Tabs[1].Page != 2 {
		t.Fatalf("tab order: %+v", s.deck.Tabs)
	}
	for i, slide := range s.deck.Slides {
		if slide.Elements[0].Text != original.Slides[i].Elements[0].Text {
			t.Fatal("tab reorder moved physical slide")
		}
	}
	data, err := serializeDeck(s.deckPath, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData(s.deckPath, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened.Tabs, s.deck.Tabs) || opened.Slides[1].TabID != s.deck.Slides[1].TabID {
		t.Fatal("lost tab metadata on reload")
	}
	if _, err := os.Stat(s.deckPath); !os.IsNotExist(err) {
		t.Fatal("mutation wrote to disk")
	}
	// Off after reopening restores the original slot even after reordering tabs.
	s = newNativeEditorSession(s.deckPath, opened)
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	tabAction(t, s, nativeEditorAction{Action: "toggle-slide-tab"})
	if s.deck.Slides[1].TabID != "" || s.deck.Slides[1].Elements[0].Text != "Reference" || len(s.deck.Tabs) != 1 {
		t.Fatal("restore lost original position")
	}
	if !s.state().Dirty {
		t.Fatal("toggle did not dirty deck")
	}
	tabAction(t, s, nativeEditorAction{Action: "undo"})
	if s.state().Dirty || s.deck.Slides[1].TabID == "" {
		t.Fatal("undo lost saved tab state")
	}
	tabAction(t, s, nativeEditorAction{Action: "redo"})
	if s.deck.Slides[1].TabID != "" {
		t.Fatal("redo did not restore slide")
	}
}

func TestTabSlidesExcludedFromNavigationButRenderAsParticipantTabs(t *testing.T) {
	s := tabTestSession(t)
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	tabAction(t, s, nativeEditorAction{Action: "toggle-slide-tab"})
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 0})
	tabAction(t, s, nativeEditorAction{Action: "next-slide"})
	if s.current != 2 {
		t.Fatal("next entered tab slide")
	}
	tabAction(t, s, nativeEditorAction{Action: "previous-slide"})
	if s.current != 0 {
		t.Fatal("previous entered tab slide")
	}
	if err := s.apply(nativeEditorAction{Action: "navigate-presentation", Slide: 1}); err != nil {
		t.Fatalf("explicit cheat-sheet selection failed: %v", err)
	}
	slide := s.deck.ResolvedSlides()[1]
	pages := exportSlidePages(slide, 1, 5, 245, 56)
	for _, page := range pages {
		if !page.TabOnly {
			t.Fatal("tab-only flag lost in rendering")
		}
	}
	safe, err := participantRenderedDeck(s.deck, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(safe.Pages) == 0 || safe.Pages[0].TabOnly {
		t.Fatal("participant tab cannot render")
	}
	text, err := participantSlideMarkdown(s.deck, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "keynope-tab") {
		t.Fatal("participant tab leaked deck metadata")
	}
	p := &presenterCompanion{pages: map[int][]exportPage{1: pages}}
	p.Update(1, 0, true, nil)
	if !p.state.Presenting {
		t.Fatal("explicitly selected cheat sheet was hidden from presentation")
	}
	p.helper, p.target = true, "main"
	if !p.presentationEnabled() {
		t.Fatal("visiting tab cancelled presentation mode")
	}
	p.Update(2, 0, p.presentationEnabled(), nil)
	if !p.state.Presenting {
		t.Fatal("returning to slide did not resume presentation")
	}
	p.paused = true
	p.Update(2, 0, p.presentationEnabled(), nil)
	if p.state.Presenting {
		t.Fatal("returning to slide resumed paused presentation")
	}
	// Slide appearance edits must preserve its identity.
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	tabAction(t, s, nativeEditorAction{Action: "update-slide", SlideData: &Slide{Background: "aurora"}})
	if s.deck.Slides[1].TabID == "" {
		t.Fatal("appearance edit cleared tab marker")
	}
	tabAction(t, s, nativeEditorAction{Action: "toggle-master-mode"})
	if s.apply(nativeEditorAction{Action: "toggle-slide-tab"}) == nil {
		t.Fatal("master marked as participant tab")
	}
}

func TestCheatSheetSelectionFollowsExternalPresentation(t *testing.T) {
	s := tabTestSession(t)
	s.deck.Slides[1].TabID = "reference"
	s.companion = &presenterCompanion{helper: true, target: "external", pages: map[int][]exportPage{1: {{Slide: 1, TabOnly: true}}}}
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	if s.companion.state.Slide != 1 || !s.companion.state.Presenting {
		t.Fatal("selected cheat sheet did not follow to external presentation")
	}
	s.companion.paused = true
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	if s.companion.state.Presenting {
		t.Fatal("cheat-sheet selection resumed a paused presentation")
	}
	s.companion.paused, s.companion.target = false, "none"
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	if s.companion.state.Presenting {
		t.Fatal("cheat-sheet selection started a stopped presentation")
	}
}

func TestSlideTabCloneDeleteAndManualPageReferences(t *testing.T) {
	s := tabTestSession(t)
	s.deck.Tabs = []DeckTab{{ID: "manual", Name: "Manual", Page: 5}}
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	tabAction(t, s, nativeEditorAction{Action: "toggle-slide-tab"})
	tabAction(t, s, nativeEditorAction{Action: "clone-slide"})
	if len(s.deck.Tabs) != 3 || s.deck.Slides[1].TabID == s.deck.Slides[2].TabID || s.deck.Tabs[0].Page != 6 {
		t.Fatalf("clone tabs: %+v", s.deck.Tabs)
	}
	tabAction(t, s, nativeEditorAction{Action: "delete-slide"})
	if len(s.deck.Tabs) != 2 || s.deck.Tabs[0].Page != 5 {
		t.Fatal("delete didn't remap tabs")
	}
	tabAction(t, s, nativeEditorAction{Action: "reorder-slide", Slide: 4, Value: 0})
	if s.deck.Tabs[0].Page != 1 || s.deck.Tabs[1].Page != 3 {
		t.Fatalf("reorder tabs: %+v", s.deck.Tabs)
	}
	tabAction(t, s, nativeEditorAction{Action: "delete-slide"})
	if len(s.deck.Tabs) != 1 || s.deck.Tabs[0].Page != 2 {
		t.Fatal("deleted manual tab not removed")
	}
}

func TestBlankSlideTabPersists(t *testing.T) {
	d := Deck{Slides: []Slide{{TabID: "blank-tab"}}}
	data, err := serializeDeck("test.md", d)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Slides) != 1 || opened.Slides[0].TabID != "blank-tab" || len(opened.Tabs) != 1 {
		t.Fatal("blank tab disappeared")
	}
	if nextPresentationSlide(opened.Slides, 0, 1) != 0 {
		t.Fatal("all-tab deck navigated out of range")
	}
}
