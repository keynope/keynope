package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyAppearanceModeRoundTripCompatibility(t *testing.T) {
	deck := cloneDeck(sceneFixture(t))
	deck.Slides[0].EngagementResult.ActivityID = "private"
	deck.Slides[0].EngagementResult.Kind = "impostor"
	deck.Slides[0].EngagementResult.State["phase"] = json.RawMessage(`3`)
	before := cloneDeck(deck)
	session := newNativeEditorSession("Modes.md", deck)
	sessionBefore := cloneDeck(session.deck)
	if err := session.apply(nativeEditorAction{Action: "set-appearance-mode", Name: "modern"}); err != errInvalidEditorAction || !reflect.DeepEqual(session.deck, sessionBefore) {
		t.Fatal("retired editor mode action remains callable or changed the deck")
	}
	appearance, err := deck.withAppearanceMode("modern")
	if err != nil {
		t.Fatal(err)
	}
	deck.Appearance = appearance
	content := cloneDeck(deck)
	content.Appearance = before.Appearance
	if !reflect.DeepEqual(content, before) {
		t.Fatal("legacy appearance migration mutated content, geometry, identities, sources or workshop results")
	}
	path := filepath.Join(t.TempDir(), "Modes.md")
	data, err := serializeDeck(path, deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil || loaded.AppearanceMode() != "modern" || loaded.ResolvedSlides()[0].ModernScene == nil {
		t.Fatalf("saved Modern deck failed to reopen: %v", err)
	}
	if !reflect.DeepEqual(loaded.Slides[0].EngagementResult, deck.Slides[0].EngagementResult) {
		t.Fatal("saved legacy appearance lost private activity results")
	}
	if _, err := deck.withAppearanceMode("invalid"); err == nil || !reflect.DeepEqual(content, before) {
		t.Fatal("invalid legacy appearance was accepted or changed content")
	}
}

func TestAppearancePreviewReportsWholeDeckWithoutPrivateData(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides = append(deck.Slides, Slide{TabID: "tab", Elements: []Element{
		{ID: "one", Kind: "text", Text: "PRIVATE TEXT", Query: "top=2&glyph=braille"},
		{ID: "two", Kind: "text", Text: "PRIVATE TEXT", Query: "top=6&glyph=ascii"},
	}})
	s := newNativeEditorSession("Report.md", deck)
	before := cloneDeck(s.deck)
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0&report=deck", nil))
	var scene slideScene
	if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	if scene.Conversion == nil || scene.Conversion.Slides != len(deck.Slides) {
		t.Fatal("missing deck report")
	}
	found := false
	for _, issue := range scene.Conversion.Issues {
		if issue.Slide == len(deck.Slides) && issue.Code == "art-treatment" && issue.Count == 2 {
			found = true
		}
	}
	if found {
		t.Fatal("retired text treatments should migrate before conversion reporting")
	}
	data, _ := json.Marshal(scene.Conversion)
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "data:image") {
		t.Fatal("report contains private/source content")
	}
	if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
		t.Fatal("report mutated deck")
	}
}
