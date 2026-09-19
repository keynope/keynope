package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestShapeDraftIsIsolatedAndRevisionChecked(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	deck.Slides[0].Elements = []Element{{ID: "shape", Kind: "shape", Query: "shape=square&width=10&height=5&top=4&left=3&fg=%232463a8"}}
	s := nativeEditorSession{deck: deck, version: 7}
	before := cloneDeck(s.deck)
	revision := int64(7)
	action := nativeEditorAction{Slide: 0, ObjectID: "shape", SceneRevision: &revision, ShapeTextBounds: &sceneRect{Width: 350, Height: 180}}
	call := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(action)
		w := httptest.NewRecorder()
		s.handleShapeDraft(w, httptest.NewRequest("POST", "/api/editor/shape-draft", bytes.NewReader(data)))
		return w
	}
	w := call()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var scene slideScene
	if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 || scene.Objects[0].RetroLines == nil || scene.Objects[0].Bounds.X != 0 || scene.Objects[0].Bounds.Y != 0 || scene.Objects[0].Bounds.Width < 350 || scene.Objects[0].Bounds.Height < 180 {
		t.Fatal("incorrect isolated sampled body")
	}
	if !reflect.DeepEqual(cloneDeck(s.deck), before) || s.version != 7 {
		t.Fatal("preview mutated document")
	}
	revision = 6
	if w := call(); w.Code != 409 {
		t.Fatal("stale preview accepted", w.Code)
	}
	revision = 7
	action.ShapeTextBounds.Width = -1
	if w := call(); w.Code != 400 {
		t.Fatal("invalid bounds accepted", w.Code)
	}
}
