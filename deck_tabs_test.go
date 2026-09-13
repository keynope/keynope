package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeckTabsRoundTripAndUndo(t *testing.T) {
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Example"}}, Engagement: &EngagementDefinition{ID: "lobby", Kind: "onboarding", Code: "Test1234"}}}}
	path := filepath.Join(t.TempDir(), "deck.md")
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	session := newNativeEditorSession(path, deck)
	tabs := []DeckTab{{ID: "guide", Name: "Workshop guide", URL: "https://example.com/guide?a=1&b=2"}, {ID: "slide", Name: "Instructions", Page: 1}}
	if err := session.apply(nativeEditorAction{Action: "set-tabs", Tabs: tabs}); err != nil {
		t.Fatal(err)
	}
	if !session.state().Dirty || !session.state().HasActivities {
		t.Fatal("tabs did not dirty activity deck")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("tab edits wrote to disk before Save")
	}
	data, err := serializeDeck(path, session.deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened.Tabs, tabs) {
		t.Fatalf("round trip: %#v", opened.Tabs)
	}
	copy := cloneDeck(opened)
	copy.Tabs[0].Name = "Other"
	if opened.Tabs[0].Name == "Other" {
		t.Fatal("shallow clone")
	}
	safe, err := participantSlideMarkdown(opened, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(safe), "keynope-tabs") {
		t.Fatal("single page leaked other tab destinations")
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if len(session.state().Tabs) != 0 || session.state().Dirty {
		t.Fatal("undo did not restore saved state")
	}
	if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(session.state().Tabs, tabs) {
		t.Fatal("redo lost tabs")
	}
}

func TestDeckTabsValidation(t *testing.T) {
	for _, tab := range []DeckTab{
		{ID: "x", Name: "X", URL: "javascript:alert(1)"},
		{ID: "x", Name: "X", URL: "data:text/html,hi"},
		{ID: "x", Name: "X", URL: "https://user:password@example.com"},
		{ID: "x", Name: "X", URL: "https://example.com", Page: 1},
		{ID: "x", Name: "X"},
	} {
		if validateDeckTabs([]DeckTab{tab}) == nil {
			t.Fatalf("accepted %#v", tab)
		}
	}
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "test.md"), Deck{Slides: []Slide{{}}})
	if session.apply(nativeEditorAction{Action: "set-tabs", Tabs: []DeckTab{{ID: "x", Name: "X", Page: 1}}}) == nil {
		t.Fatal("allowed tabs without activities")
	}
}
