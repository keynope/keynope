package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func themeDraftAction(s *nativeEditorSession, name string) nativeEditorAction {
	theme := s.deck.resolvedDeckTheme()
	theme.Name = name
	return nativeEditorAction{Action: "set-deck-theme", ThemeData: theme, ThemeDraft: &themeDraftContext{Owner: s.notesSessionID, Base: s.deck.resolvedDeckTheme()}}
}

func TestThemeDraftOwnershipAndUnrelatedEdits(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("theme.md", themedSharedTextFixture(2))
		s.masterMode, s.currentMaster = master, 0
		action := themeDraftAction(s, "Owned draft")
		// Navigation/selection increments revision but does not stale a theme.
		if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 0}); err != nil {
			t.Fatal(err)
		}
		before, revision := cloneDeck(s.deck), s.version
		if err := s.apply(action); err != nil || s.deck.Theme.Name != "Owned draft" || s.version <= revision {
			t.Fatal("unrelated view change invalidated theme draft", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("theme draft undo changed content", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || s.deck.Theme.Name != "Owned draft" {
			t.Fatal("theme draft redo failed", err)
		}
	}
}

func TestThemeDraftRejectsNewerThemeAndReplacementDocument(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, reason := range []string{"theme", "document", "missing-owner", "missing-base", "invalid-base"} {
			t.Run(reason+map[bool]string{false: "/slide", true: "/master"}[master], func(t *testing.T) {
				s := newNativeEditorSession("same.md", themedSharedTextFixture(2))
				s.masterMode, s.currentMaster = master, 0
				action := themeDraftAction(s, "Stale draft")
				switch reason {
				case "theme":
					if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: builtinDeckTheme("bold")}); err != nil {
						t.Fatal(err)
					}
				case "document":
					// Same path, data and initial revision; only the document owner differs.
					s = newNativeEditorSession("same.md", themedSharedTextFixture(2))
					s.masterMode, s.currentMaster = master, 0
				case "missing-owner":
					action.ThemeDraft.Owner = ""
				case "missing-base":
					action.ThemeDraft.Base = nil
				case "invalid-base":
					action.ThemeDraft.Base.Version = 9
				}
				before, version, undo, redo := cloneDeck(s.deck), s.version, len(s.undo), len(s.redo)
				err := s.apply(action)
				var failure *editorCommandError
				if !errors.As(err, &failure) || failure.field != "theme" || reason == "theme" && failure.code != "stale-target" || reason == "document" && failure.code != "stale-target" {
					t.Fatal("missing typed theme conflict", err)
				}
				if !reflect.DeepEqual(s.deck, before) || s.version != version || len(s.undo) != undo || len(s.redo) != redo {
					t.Fatal("rejected theme draft mutated document/history")
				}
			})
		}
	}
}

func TestThemeDraftInputCopiesAndHTTPConflict(t *testing.T) {
	s := newNativeEditorSession("theme.md", themedSharedTextFixture(2))
	action := themeDraftAction(s, "Captured")
	owned := ownEditorObjectInput(action)
	action.ThemeData.Name = "Caller changed"
	action.ThemeDraft.Base.Colors.Accent[0] = "#010203"
	action.ThemeDraft.Owner = "Caller changed owner"
	if owned.ThemeData.Name != "Captured" || owned.ThemeDraft.Owner != s.notesSessionID || owned.ThemeDraft.Base.Colors.Accent[0] == "#010203" {
		t.Fatal("theme input retained caller-owned pointers")
	}
	if err := s.apply(owned); err != nil {
		t.Fatal(err)
	}
	before, version := cloneDeck(s.deck), s.version
	data, err := json.Marshal(owned) // Its base predates the successful apply.
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleAction(w, httptest.NewRequest(http.MethodPost, "/api/editor/action", strings.NewReader(string(data))))
	if w.Code != http.StatusBadRequest || w.Header().Get("X-Keynope-Error-Code") != "stale-target" || !strings.Contains(w.Body.String(), "theme changed") || !reflect.DeepEqual(s.deck, before) || s.version != version {
		t.Fatal("HTTP theme conflict overwrote state or lost typed response", w.Code, w.Body.String())
	}
}
