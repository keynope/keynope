package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityQRSettingRoundTripAndUndo(t *testing.T) {
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Join us"}}, Engagement: &EngagementDefinition{ID: "lobby", Kind: "onboarding", Code: "AbCd1234"}}}}
	path := filepath.Join(t.TempDir(), "deck.md")
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	session := newNativeEditorSession(path, deck)
	if session.state().HideActivityQR {
		t.Fatal("QR codes must default to on")
	}
	if err := session.apply(nativeEditorAction{Action: "set-activity-qr", Value: 0}); err != nil {
		t.Fatal(err)
	}
	if !session.state().HideActivityQR || !session.state().Dirty {
		t.Fatal("setting not applied or dirty")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("setting wrote to disk before save")
	}
	encoded, err := serializeDeck(path, session.deck)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "<!-- keynope-activity-qr=off -->") {
		t.Fatal("missing deck setting")
	}
	opened, err := parseDeckData(path, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !opened.HideActivityQR || len(opened.Slides) != 1 {
		t.Fatal("setting lost or created an extra slide")
	}
	if !cloneDeck(opened).HideActivityQR {
		t.Fatal("clone lost setting")
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if session.state().HideActivityQR || session.state().Dirty {
		t.Fatal("undo failed")
	}
	if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !session.state().HideActivityQR {
		t.Fatal("redo failed")
	}
	runtime := EngagementRuntimeState{HideActivityQR: true, SessionCode: "AbCd1234"}
	payload, _ := json.Marshal(runtime)
	var received EngagementRuntimeState
	if err := json.Unmarshal(payload, &received); err != nil {
		t.Fatal(err)
	}
	if !received.HideActivityQR || received.SessionCode != runtime.SessionCode {
		t.Fatal("presentation sync lost setting/code")
	}
}

func TestActivityQRSettingUnavailableWithoutActivities(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "No activities"}}}}})
	if err := session.apply(nativeEditorAction{Action: "set-activity-qr", Value: 0}); err == nil {
		t.Fatal("QR setting should require activities")
	}
}

func TestOnboardingQRPreferenceReachesPresentationPages(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Layouts = append(masters.Layouts, defaultActivityMaster())
	for _, layout := range []string{"", "missing", activityLayoutID} {
		deck := Deck{HideActivityQR: true, Masters: masters, Slides: []Slide{{LayoutID: layout,
			Engagement: &EngagementDefinition{ID: "lobby", Kind: "onboarding", Code: "AbCd1234"}}}}
		resolved := deck.ResolveSlide(0, false)
		if !resolved.HideActivityQR {
			t.Fatalf("layout %q lost the deck's QR preference", layout)
		}
		for _, page := range exportSlidePagesFrozen(resolved, 0, 1, 245, 56) {
			if !page.HideActivityQR {
				t.Fatalf("layout %q: presentation/export page lost the preference", layout)
			}
		}
		if layout == activityLayoutID {
			found := false
			for _, element := range resolved.Elements {
				if element.PlaceholderRole != activityQRCodeRole {
					continue
				}
				found = true
				q, _ := url.ParseQuery(element.Query)
				if element.Kind != "text" || q.Has("qr") || !strings.Contains(element.Text, "Code: AbCd1234") {
					t.Fatalf("legacy onboarding QR was not replaced: %+v", element)
				}
			}
			if !found {
				t.Fatal("missing join text")
			}
		}
		deck.HideActivityQR = false
		shown := deck.ResolveSlide(0, false)
		if shown.HideActivityQR {
			t.Fatal("re-enabling QR retained hidden state")
		}
		for _, element := range shown.Elements {
			if element.PlaceholderRole == activityQRCodeRole && element.Kind != "code" {
				t.Fatal("toggling off mutated the master QR element")
			}
		}
	}
}
