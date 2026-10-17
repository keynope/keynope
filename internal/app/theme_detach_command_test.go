package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"testing"
)

func detachAction(s *nativeEditorSession) nativeEditorAction {
	return nativeEditorAction{Action: "detach-deck-theme", ThemeDraft: &themeDraftContext{Owner: s.notesSessionID, Base: s.deck.resolvedDeckTheme()}}
}

func themeDetachFixture() Deck {
	deck := themedSharedTextFixture(2)
	deck.Theme = builtinDeckTheme("clean-light")
	deck.Masters = defaultMasterDeck()
	deck.Masters.Base.Slide.Elements = []Element{{ID: "master-text", Kind: "text", Text: "Master text", Query: "render=truetype"}}
	deck.Slides = append(deck.Slides, Slide{ID: "second", Elements: []Element{{ID: "second-text", Kind: "text", Text: "Second slide", Query: "render=truetype"}}})
	return deck
}

func TestThemeDetachCommandHistoryAndSelection(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			s := newNativeEditorSession("theme.md", themeDetachFixture())
			s.masterMode, s.currentMaster = master, 0
			s.selected, s.selection = 0, map[int]bool{0: true}
			before, version, mutation := cloneDeck(s.deck), s.version, s.mutationRevision
			if err := s.apply(detachAction(s)); err != nil {
				t.Fatal(err)
			}
			if s.deck.Theme != nil || len(s.undo) != 1 || len(s.redo) != 0 || s.version != version+1 || s.mutationRevision != mutation+1 || s.masterMode != master || s.selected != 0 || !s.selection[0] {
				t.Fatal("detachment failed history/revision/selection contract")
			}
			after := cloneDeck(s.deck)
			if _, err := serializeDeck("detached.md", after); err != nil {
				t.Fatal("accepted deck cannot be saved", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("undo is not an exact restore", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
				t.Fatal("redo is not an exact restore", err)
			}
			version = s.version
			if err := s.apply(detachAction(s)); err != nil || s.version != version || len(s.undo) != 1 {
				t.Fatal("detaching an already detached deck is not a no-op", err)
			}
		})
	}
}

func TestThemeDetachPreparedConflictAndCancellation(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, reason := range []string{"edit", "document", "theme", "cancel", "missing-context"} {
		t.Run(reason, func(t *testing.T) {
			s := newNativeEditorSession("theme.md", themeDetachFixture())
			action := ownEditorObjectInput(detachAction(s))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			draft, err := s.prepareThemeDetach(ctx, action)
			if err != nil || draft == nil {
				t.Fatal("preparation failed", err)
			}
			action.preparedThemeDetach = draft
			switch reason {
			case "edit":
				version := s.version
				if err := s.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 0, SceneRevision: &version, NotesSessionID: s.notesSessionID, NotesOwnerID: s.deck.Slides[0].ID, NotesBeforeHash: notesHash(s.deck.Slides[0].Notes), Notes: "Concurrent notes"}); err != nil {
					t.Fatal(err)
				}
			case "document":
				s = newNativeEditorSession("theme.md", themeDetachFixture())
			case "theme":
				if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: builtinDeckTheme("bold")}); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "missing-context":
				action.ThemeDraft = nil
			}
			before, version, undo, redo := cloneDeck(s.deck), s.version, len(s.undo), len(s.redo)
			err = s.apply(action)
			if err == nil || reason == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("unsafe prepared detachment accepted", err)
			}
			if !reflect.DeepEqual(before, s.deck) || version != s.version || undo != len(s.undo) || redo != len(s.redo) {
				t.Fatal("rejected detachment changed document/history")
			}
		})
	}
}

func TestThemeDetachPreparedNavigationAndInputOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("theme.md", themeDetachFixture())
	action := detachAction(s)
	owned := ownEditorObjectInput(action)
	action.ThemeDraft.Owner = "Changed by caller"
	action.ThemeDraft.Base.Name = "Changed by caller"
	if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 1}); err != nil {
		t.Fatal(err)
	}
	draft, err := s.prepareThemeDetach(context.Background(), owned)
	if err != nil {
		t.Fatal("retained caller pointers or navigation invalidated the dialog", err)
	}
	owned.preparedThemeDetach = draft
	if err := s.apply(owned); err != nil || s.current != 1 || s.deck.Theme != nil {
		t.Fatal("unrelated navigation invalidated detachment or was overwritten", err)
	}
}

func TestThemeDetachLimitsAndEarlyCancellation(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := powerupProofCorpus(t)["tables.md"]
	deck.Theme = builtinDeckTheme("clean-light")
	for _, late := range []bool{false, true} {
		s := newNativeEditorSession("tables.md", deck)
		action := detachAction(s)
		if late {
			draft, err := s.prepareThemeDetach(context.Background(), action)
			if err != nil {
				t.Fatal(err)
			}
			action.preparedThemeDetach = draft
		}
		s.structuredLimits = defaultStructuredBudgetLimits
		s.structuredLimits.EncodedBytes = 1
		before, version := cloneDeck(s.deck), s.version
		err := s.apply(action)
		var failure *editorCommandError
		if !errors.As(err, &failure) || failure.code != "payload-limit" || !reflect.DeepEqual(before, s.deck) || version != s.version || len(s.undo) != 0 {
			t.Fatal("budget failure mutated deck/history or lacked typed error", late, err)
		}
	}
	s := newNativeEditorSession("theme.md", themeDetachFixture())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	action := detachAction(s)
	action.commandContext = ctx
	before, version := cloneDeck(s.deck), s.version
	if err := s.apply(action); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, s.deck) || version != s.version || len(s.undo) != 0 {
		t.Fatal("cancelled preparation changed deck/history", err)
	}
}

func TestThemeDetachHTTPCancellation(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("theme.md", themeDetachFixture())
	data, err := json.Marshal(detachAction(s))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("POST", "/api/editor/action", bytes.NewReader(data)).WithContext(ctx)
	response := httptest.NewRecorder()
	before, version := cloneDeck(s.deck), s.version
	s.handleAction(response, request)
	if response.Code == 200 || version != s.version || !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 {
		t.Fatal("HTTP cancellation was not passed to detachment", response.Code, response.Body.String())
	}
}

func TestThemeDetachHTTPSuccess(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("theme.md", themeDetachFixture())
	data, err := json.Marshal(detachAction(s))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.handleAction(response, httptest.NewRequest("POST", "/api/editor/action", bytes.NewReader(data)))
	if response.Code != 200 || s.deck.Theme != nil || len(s.undo) != 1 {
		t.Fatal("HTTP Detach rejected or did not commit", response.Code, response.Body.String())
	}
	var state nativeEditorState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.HasDeckTheme {
		t.Fatal("HTTP response retained live theme state", err)
	}
}
