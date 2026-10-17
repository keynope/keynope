package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGestureMutationRevisionAcrossScopes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		t.Run(fmt.Sprint(master), func(t *testing.T) {
			s := structuredBudgetSession(t, master)
			if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
				t.Fatal(err)
			}
			captured := s.mutationRevision
			if captured != 0 {
				t.Fatal("selection changed mutation revision")
			}
			source := cloneSlide(s.deck.Slides[0])
			if master {
				source = cloneSlide(s.deck.Masters.Base.Slide)
			}
			if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: builtinDeckTheme("clean-light")}); err != nil {
				t.Fatal(err)
			}
			if s.mutationRevision != captured+1 {
				t.Fatal("theme edit did not advance mutation revision")
			}
			live := s.deck.Slides[0]
			if master {
				live = s.deck.Masters.Base.Slide
			}
			if !reflect.DeepEqual(source.Elements, live.Elements) {
				t.Fatal("theme fixture modified authored target")
			}
			changed := cloneElement(source.Elements[0])
			changed.Query = setQueryValue(changed.Query, "width", "105.25")
			revision := s.version
			action := nativeEditorAction{Action: "update-elements", SceneMaster: master, Slide: 0, SceneRevision: &revision, MutationRevision: &captured, ElementIndices: []int{0}, ElementsData: []Element{changed}}
			before, mr, undo, redo := cloneDeck(s.deck), s.mutationRevision, len(s.undo), len(s.redo)
			err := s.apply(action)
			if err == nil || !strings.Contains(err.Error(), "changed during the gesture") || !reflect.DeepEqual(before, s.deck) || s.version != revision || s.mutationRevision != mr || len(s.undo) != undo || len(s.redo) != redo {
				t.Fatal("stale inherited edit was not rejected atomically", err)
			}
			encoded, _ := json.Marshal(action)
			request := httptest.NewRequest("POST", "/api/editor/action", bytes.NewReader(encoded))
			request.Header.Set("Accept", "application/problem+json")
			response := httptest.NewRecorder()
			s.handleAction(response, request)
			var problem editorProblem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || response.Code != 400 || problem.Code != "stale-target" || s.mutationRevision != mr || s.version != revision || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("wire mutation token lost", err, response.Code, response.Body.String())
			}
			fresh := s.mutationRevision
			action.MutationRevision = &fresh
			if err := s.apply(action); err != nil {
				t.Fatal("fresh gesture failed", err)
			}
			if s.mutationRevision != fresh+1 {
				t.Fatal("accepted geometry did not advance mutation revision once")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if s.mutationRevision != fresh+2 {
				t.Fatal("undo restored an old mutation revision")
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			if s.mutationRevision != fresh+3 {
				t.Fatal("redo did not advance mutation revision")
			}
			data, err := serializeDeck("Gesture.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "mutationRevision") {
				t.Fatal("runtime gesture token persisted")
			}
		})
	}
}

func TestGestureMutationRevisionIgnoresViewSaveAndPrivateResults(t *testing.T) {
	s := structuredBudgetSession(t, false)
	for _, action := range []nativeEditorAction{{Action: "select-element", Element: 0}, {Action: "select-elements", ElementIndices: []int{0, 1}, ElementsData: cloneSlide(s.deck.Slides[0]).Elements}, {Action: "start-timer", Value: 60}, {Action: "stop-timer"}} {
		if err := s.apply(action); err != nil {
			t.Fatal(action.Action, err)
		}
		if s.mutationRevision != 0 {
			t.Fatal(action.Action, "invalidated geometry")
		}
	}
	s.deck.Slides[0].Engagement = &EngagementDefinition{ID: "result", Kind: "storm", Prompt: "Ideas"}
	result := &EngagementResult{Version: 1, ActivityID: "result", Kind: "storm", State: map[string]json.RawMessage{"phase": json.RawMessage(`3`)}}
	if err := s.apply(nativeEditorAction{Action: "set-engagement-result", Name: "result", EngagementResult: result}); err != nil {
		t.Fatal(err)
	}
	if !s.confirmSaved("Gesture.md", s.version) || s.mutationRevision != 0 {
		t.Fatal("private results or save invalidated geometry")
	}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"table-a"}, Property: "paint.opacity", PropertyValue: json.RawMessage(`-1`)}); err == nil || s.mutationRevision != 0 {
		t.Fatal("rejected edit advanced mutation revision", err)
	}
}
