package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestConnectorDraftRoutesTemporaryShapesWithoutMutation(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0] = connectorFixture()
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	s := nativeEditorSession{deck: deck, version: 7}
	before := cloneDeck(s.deck)
	request := connectorDraftRequest{Revision: 7, Bounds: map[string]sceneRect{"a": {X: 30.25, Y: 6.5, Width: 35.5, Height: 14}}}
	call := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(body)))
		return w
	}
	w := call()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Revision int64
		Routes   []shapeConnector
		Lines    map[string][]exportLine
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cols, rows := authoredRenderSize(245, 56)
	original := slideShapeConnectors(deck.Slides[0], displayLines(deck.Slides[0], cols, rows, 0), cols, rows)
	if result.Revision != 7 || len(result.Routes) != 1 || reflect.DeepEqual(original, result.Routes) {
		t.Fatal("missing changed draft route", result)
	}
	expected := cloneDeck(deck)
	q, _ := url.ParseQuery(expected.Slides[0].Elements[0].Query)
	for key, value := range map[string]string{"left": "30", "top": "6", "width": "35.5", "height": "14", "object-offset-x": "0.25", "object-offset-y": "0.5"} {
		q.Set(key, value)
	}
	expected.Slides[0].Elements[0].Query = q.Encode()
	projected := expected.ResolveSlide(0, false)
	projectedLines := displayLines(projected, cols, rows, 0)
	want := retroObjectLines(expected, projected, shapeConnectorLines(projected, projectedLines, cols, rows), 2, cols, rows)
	if len(want) == 0 || !reflect.DeepEqual(result.Lines["line"], want) {
		t.Fatal("draft raster differs from committed connector painter")
	}
	if result.Routes[0].Points[len(result.Routes[0].Points)-1] != original[0].Points[len(original[0].Points)-1] {
		t.Fatal("stationary destination changed")
	}
	if !reflect.DeepEqual(cloneDeck(s.deck), before) || s.version != 7 {
		t.Fatal("draft mutated source")
	}
	request.Bounds["b"] = sceneRect{X: 100, Y: 20, Width: 40, Height: 20}
	request.Routes = map[string][]connectorPoint{"line": {{60, 12}, {75, 12}, {75, 30}, {100, 30}}}
	// Manual routing is only meaningful for elbow connectors.
	s.deck.Slides[0].Elements[2].Query += "&connector-mode=elbow"
	manualBefore := cloneDeck(s.deck)
	w = call()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Routes[0].Points[1].X != 75 || result.Routes[0].Points[2].X != 75 {
		t.Fatal("draft lost manual track", result.Routes)
	}
	if !reflect.DeepEqual(cloneDeck(s.deck), manualBefore) {
		t.Fatal("manual route persisted during draft")
	}
	delete(request.Bounds, "b")
	if call().Code != 400 {
		t.Fatal("accepted route without both draft endpoints")
	}
	request.Routes = nil
	request.Revision = 6
	if call().Code != 409 {
		t.Fatal("accepted stale revision")
	}
	request.Revision = 7
	request.Master = true
	if call().Code != 409 {
		t.Fatal("accepted wrong workspace")
	}
	request.Master = false
	request.Bounds["a"] = sceneRect{Width: -1, Height: 10}
	if call().Code != 400 {
		t.Fatal("accepted invalid bounds")
	}
	request.Bounds = map[string]sceneRect{"missing": {Width: 1, Height: 1}}
	if call().Code != 404 {
		t.Fatal("accepted unknown target")
	}
	request.Bounds = map[string]sceneRect{"line": {Width: 1, Height: 1}}
	if call().Code != 400 {
		t.Fatal("accepted nonshape")
	}
}
