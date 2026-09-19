package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func activityResultFixture() *EngagementResult {
	return &EngagementResult{Version: 1, ActivityID: "storm-1", Kind: "storm", State: map[string]json.RawMessage{
		"phase": json.RawMessage(`3`), "ideas": json.RawMessage(`["Keep this answer"]`),
		"attributions": json.RawMessage(`[{"displayName":"Private Name","idea":"Keep this answer"}]`),
		"participants": json.RawMessage(`1`),
		"resumeState":  json.RawMessage(`{"phase":1,"remainingMs":45000,"timed":true}`),
	}}
}

func TestActivityResultsSaveReloadResetAndPrivacy(t *testing.T) {
	deck := Deck{Slides: []Slide{{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "storm", Prompt: "Ideas?"}, Elements: []Element{{Kind: "text", Text: "Public slide"}}}}}
	path := filepath.Join(t.TempDir(), "training.md")
	s := newNativeEditorSession(path, deck)
	result := activityResultFixture()
	if err := s.apply(nativeEditorAction{Action: "set-engagement-result", Name: result.ActivityID, EngagementResult: result}); err != nil {
		t.Fatal(err)
	}
	if !s.state().Dirty || len(s.undo) != 0 {
		t.Fatal("results must dirty the document, not add an editing undo step")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("result arrival wrote to disk without Save")
	}
	encoded, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "keynope-engagement-results version=1") {
		t.Fatal("missing result metadata")
	}
	loaded, err := parseDeckData(path, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Slides[0].EngagementResult, result) {
		t.Fatal("results changed across Markdown round trip")
	}
	copy := cloneDeck(loaded)
	copy.Slides[0].EngagementResult.State["phase"][0] = '4'
	if string(loaded.Slides[0].EngagementResult.State["phase"]) != "3" {
		t.Fatal("cloned results alias the original")
	}
	safe, err := participantSlideMarkdown(loaded, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(safe), "engagement-results") || strings.Contains(string(safe), "Private Name") {
		t.Fatal("participant page leaked results")
	}
	rendered, err := participantRenderedDeck(loaded, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rendered)
	if strings.Contains(string(data), "Private Name") || strings.Contains(string(data), "Keep this answer") {
		t.Fatal("rendered participant page leaked results")
	}
	s = newNativeEditorSession(path, loaded)
	if err := s.apply(nativeEditorAction{Action: "set-engagement-result", Name: "storm-1"}); err != nil {
		t.Fatal(err)
	}
	encoded, err = serializeDeck(path, s.deck)
	if err != nil || strings.Contains(string(encoded), "engagement-results") {
		t.Fatal("reset retained archived metadata")
	}
	if !s.state().Dirty {
		t.Fatal("reset must enable Save")
	}
}

func TestActivityResultsFollowIdentityAndSurviveEdits(t *testing.T) {
	s := newNativeEditorSession("training.md", Deck{Slides: []Slide{
		{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "storm"}},
		{Elements: []Element{{Kind: "text", Text: "Other slide"}}},
	}})
	tabAction(t, s, nativeEditorAction{Action: "add-element", Kind: "text"})
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 1})
	tabAction(t, s, nativeEditorAction{Action: "set-engagement-result", Name: "storm-1", EngagementResult: activityResultFixture()})
	if s.current != 1 || s.deck.Slides[1].EngagementResult != nil {
		t.Fatal("result update navigated or targeted current slide")
	}
	tabAction(t, s, nativeEditorAction{Action: "undo"})
	if s.deck.Slides[0].EngagementResult == nil {
		t.Fatal("text undo lost workshop results")
	}
	tabAction(t, s, nativeEditorAction{Action: "redo"})
	if s.deck.Slides[0].EngagementResult == nil {
		t.Fatal("text redo lost workshop results")
	}
	tabAction(t, s, nativeEditorAction{Action: "select-slide", Slide: 0})
	tabAction(t, s, nativeEditorAction{Action: "update-slide", SlideData: &Slide{Background: "aurora"}})
	if s.deck.Slides[0].EngagementResult == nil {
		t.Fatal("style update lost results")
	}
	tabAction(t, s, nativeEditorAction{Action: "clone-slide"})
	if s.deck.Slides[1].EngagementResult != nil || s.deck.Slides[1].Engagement.ID == "storm-1" {
		t.Fatal("cloned activity reused completed session")
	}
}

func TestActivityResultsValidationAndSessionReset(t *testing.T) {
	result := activityResultFixture()
	result.State["signingKey"] = json.RawMessage(`"secret"`)
	if validateEngagementResult(result) == nil {
		t.Fatal("accepted connection credentials")
	}
	if _, err := decodeEngagementResult("<!-- keynope-engagement-results version=1 base64:AAAA -->"); err == nil {
		t.Fatal("accepted invalid gzip")
	}
	s := newNativeEditorSession("training.md", Deck{Slides: []Slide{
		{Engagement: &EngagementDefinition{ID: "lobby", Kind: "onboarding", Code: "AbCd1234"}},
		{Engagement: &EngagementDefinition{ID: "storm-1", Kind: "storm"}, EngagementResult: activityResultFixture()},
	}})
	tabAction(t, s, nativeEditorAction{Action: "set-engagement", EngagementData: &EngagementDefinition{ID: "lobby", Kind: "onboarding", Code: "NewC0de1"}})
	if s.deck.Slides[1].EngagementResult != nil {
		t.Fatal("new onboarding session retained old activity results")
	}
}
