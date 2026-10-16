package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEditorProblemNegotiationAndOwnership(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, kind := range []string{"stale", "locked", "table", "payload", "unsupported", "value", "missing", "media"} {
			t.Run(fmt.Sprintf("master=%v/%s", master, kind), func(t *testing.T) {
				s := structuredBudgetSession(t, master)
				revision := s.version
				data := cloneTable(s.deck.Objects.Tables["table-data"])
				action := nativeEditorAction{Action: "set-table", ObjectID: "table-a", Slide: 0, SceneMaster: master, SceneRevision: &revision, TableData: &data}
				wantCode, wantField, wantObject := "", "", "table-a"
				switch kind {
				case "stale":
					revision--
					wantCode = "stale-target"
				case "locked":
					slide := &s.deck.Slides[0]
					if master {
						slide = &s.deck.Masters.Base.Slide
					}
					slide.Elements[0].Query += "&object-locked=1"
					wantCode = "locked-target"
				case "table":
					data.Cells = nil
					wantCode, wantField = "invalid-table", "tableData"
				case "payload":
					s.structuredLimits.Cells = 4
					data, _ = newTableData(3, 2)
					wantCode = "payload-limit"
				case "unsupported", "value", "missing":
					action.Action, action.ObjectID, action.ObjectIDs, action.TableData = "set-selection-property", "", []string{"table-a"}, nil
					action.Property, action.PropertyValue = "paint.opacity", json.RawMessage(`99`)
					wantCode, wantField = "invalid-value", "paint.opacity"
					if kind == "unsupported" {
						action.Property = "text.font"
						action.PropertyValue = json.RawMessage(`"sans"`)
						wantCode, wantField = "unsupported-property", "text.font"
					}
					if kind == "missing" {
						action.ObjectIDs = []string{"gone"}
						wantCode, wantField, wantObject = "missing-reference", "", "gone"
					}
				case "media":
					action.Action, action.ObjectID, action.TableData = "add-element", "", nil
					action.ElementData = &Element{Kind: "image", AssetID: "bad"}
					action.AssetData = &DeckAsset{MIME: "image/png", Width: 2, Height: 2, Data: []byte("broken")}
					wantCode, wantField, wantObject = "invalid-media", "assetData", ""
				}
				before, version, undo, redo := cloneDeck(s.deck), s.version, len(s.undo), len(s.redo)
				encoded, err := json.Marshal(action)
				if err != nil {
					t.Fatal(err)
				}
				message := ""
				for _, accept := range []string{"application/problem+json", ""} {
					r := httptest.NewRequest(http.MethodPost, "/api/editor/action", strings.NewReader(string(encoded)))
					r.Header.Set("Accept", accept)
					w := httptest.NewRecorder()
					s.handleAction(w, r)
					if w.Code != http.StatusBadRequest || w.Header().Get("X-Keynope-Error-Code") != wantCode || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("wrong failure contract", w.Code, w.Header(), w.Body.String())
					}
					if accept != "" {
						var problem editorProblem
						if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
							t.Fatal(err)
						}
						if problem.Version != 1 || problem.Code != wantCode || problem.Field != wantField || problem.ObjectID != wantObject || problem.Status != w.Code || problem.Message == "" {
							t.Fatal("wrong structured details", problem)
						}
						message = problem.Message
					} else if strings.TrimSpace(w.Body.String()) != message || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
						t.Fatal("legacy readable body changed", w.Body.String())
					}
				}
				if !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != undo || len(s.redo) != redo {
					t.Fatal("failed transport command mutated state")
				}
			})
		}
	}
}

func TestEditorProblemSafeFieldsAcceptAndCauses(t *testing.T) {
	for _, accept := range []string{"application/problem+json", "text/plain, application/problem+json;q=0.5"} {
		if !acceptsEditorProblem(accept) {
			t.Fatal("valid negotiation refused", accept)
		}
	}
	for _, accept := range []string{"", "*/*", "application/json", "application/problem+json;q=0", "application/problem+json;q=NaN", strings.Repeat("x", 513)} {
		if acceptsEditorProblem(accept) {
			t.Fatal("invalid negotiation accepted", accept)
		}
	}
	wrapped := editorFailure("cancelled", "", "", context.Canceled)
	if !errors.Is(wrapped, context.Canceled) {
		t.Fatal("error cause lost")
	}
	problem := editorProblemFor(nativeEditorAction{Action: "set-selection-property", Property: "field\nunsafe", ObjectID: "../private"}, fmt.Errorf("readable failure"), 400)
	if problem.Field != "" || problem.ObjectID != "" || problem.Message != "readable failure" {
		t.Fatal("unsafe target details exposed", problem)
	}
	for _, accept := range []string{"application/problem+json", ""} {
		r := httptest.NewRequest(http.MethodPost, "/api/editor/action", strings.NewReader(`{"private":"not reflected",`))
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		newNativeEditorSession("invalid.md", Deck{}).handleAction(w, r)
		if w.Header().Get("X-Keynope-Error-Code") != "invalid-command" || strings.Contains(w.Body.String(), "not reflected") {
			t.Fatal("malformed action leaked request", w.Body.String())
		}
	}
}
