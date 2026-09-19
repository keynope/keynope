package main

import (
	"strings"
	"testing"
)

func TestLegacyDuplicateActivityIDsAreRepaired(t *testing.T) {
	result := activityResultFixture()
	deck := Deck{Slides: []Slide{
		{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "chosen", ChosenCount: 1}},
		{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "storm"}, EngagementResult: result},
		{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "storm"}},
	}}
	md, err := serializeDeck("test.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("test.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if !parsedDeckElementOrderChanged {
		t.Fatal("ID repair must mark deck dirty")
	}
	seen := map[string]bool{}
	for _, slide := range loaded.Slides {
		id := slide.Engagement.ID
		if seen[id] || id == "" {
			t.Fatal("duplicate or missing ID")
		}
		seen[id] = true
	}
	if loaded.Slides[0].Engagement.ID != "storm-1" {
		t.Fatal("original activity ID changed")
	}
	if loaded.Slides[1].EngagementResult.ActivityID != loaded.Slides[1].Engagement.ID {
		t.Fatal("existing results did not follow their slide")
	}
	session := newNativeEditorSession("test.md", loaded)
	result = cloneEngagementResult(result)
	result.ActivityID = loaded.Slides[2].Engagement.ID
	if err := session.apply(nativeEditorAction{Action: "set-engagement-result", Name: result.ActivityID, EngagementResult: result}); err != nil {
		t.Fatal(err)
	}
	if session.deck.Slides[0].EngagementResult != nil {
		t.Fatal("Storm result written onto Chosen")
	}
	saved, err := serializeDeck("test.md", session.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("test.md", saved)
	if err != nil {
		t.Fatal(err)
	}
	for i, slide := range reopened.Slides {
		if slide.Engagement.ID != loaded.Slides[i].Engagement.ID {
			t.Fatal("IDs not stable across save/reopen")
		}
	}
	if !strings.Contains(string(saved), "keynope-engagement-results") {
		t.Fatal("missing result metadata")
	}
}

func TestActivityConfigurationCannotReuseAnotherSlidesID(t *testing.T) {
	s := newNativeEditorSession("test.md", Deck{Slides: []Slide{{Engagement: &EngagementDefinition{ID: "same", Kind: "chosen"}}, {}}})
	s.current = 1
	if err := s.apply(nativeEditorAction{Action: "set-engagement", EngagementData: &EngagementDefinition{ID: "same", Kind: "storm"}}); err != nil {
		t.Fatal(err)
	}
	if s.deck.Slides[0].Engagement.ID == s.deck.Slides[1].Engagement.ID {
		t.Fatal("reused activity ID")
	}
}
