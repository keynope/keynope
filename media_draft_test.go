package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMediaDraftDoesNotMutateSource(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	s := nativeEditorSession{deck: deck, version: 4}
	before := cloneDeck(deck)
	revision := int64(4)
	mask := "ellipse"
	action := nativeEditorAction{Slide: 0, ObjectID: "image", SceneRevision: &revision, Crop: &sceneCrop{Left: .2}, ModernMask: &mask}
	call := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(action)
		w := httptest.NewRecorder()
		s.handleMediaDraft(w, httptest.NewRequest("POST", "/api/editor/media-draft", bytes.NewReader(body)))
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
	if len(scene.Objects) != 1 || scene.Objects[0].ID != "image" || scene.Objects[0].RetroLines == nil || scene.Objects[0].Media.Mask != mask || scene.Objects[0].Media.Crop.Left != .2 {
		t.Fatal("draft did not preserve requested treatment/crop/mask")
	}
	if !reflect.DeepEqual(cloneDeck(s.deck), before) || s.version != 4 {
		t.Fatal("preview mutated deck or source assets")
	}
	revision = 3
	if call().Code != 409 {
		t.Fatal("stale draft accepted")
	}
	revision = 4
	mask = "invalid"
	if call().Code != 400 {
		t.Fatal("invalid mask accepted")
	}
}
