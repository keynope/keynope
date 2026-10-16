package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMutationPreviewScopeAdmission(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, kind := range []string{"accepted", "stale", "wrong-mode", "wrong-slide", "legacy"} {
			t.Run(fmt.Sprintf("master=%v/%s", master, kind), func(t *testing.T) {
				s := structuredBudgetSession(t, master)
				revision := s.version
				element := s.deck.Slides[0].Elements
				if master {
					element = s.deck.Masters.Base.Slide.Elements
				}
				draft := element[0]
				draft.Query = "left=5&top=5&width=110.25&height=21.5"
				action := nativeEditorAction{Element: 0, ElementData: &draft, SceneRevision: &revision, SceneMaster: master, Slide: 0, Cols: 245, Rows: 56}
				switch kind {
				case "stale":
					revision--
				case "wrong-mode":
					action.SceneMaster = !master
				case "wrong-slide":
					action.Slide = 1
				case "legacy":
					action.SceneRevision = nil
				}
				before, version, undo, redo := cloneDeck(s.deck), s.version, len(s.undo), len(s.redo)
				encoded, err := json.Marshal(action)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodPost, "/api/editor/preview", strings.NewReader(string(encoded)))
				r.Header.Set("Accept", "application/problem+json")
				w := httptest.NewRecorder()
				s.handlePreview(w, r)
				if kind == "accepted" || kind == "legacy" {
					if w.Code != 200 || !strings.Contains(w.Body.String(), "table-a") {
						t.Fatal("valid draft failed", w.Code, w.Body.String())
					}
				} else {
					var problem editorProblem
					if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || w.Code != 409 || problem.Code != "stale-target" || problem.ObjectID != "table-a" {
						t.Fatal("stale preview not rejected", err, w.Code, w.Body.String())
					}
				}
				if !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != undo || len(s.redo) != redo {
					t.Fatal("preview mutated authored state or history")
				}
			})
		}
	}
}
