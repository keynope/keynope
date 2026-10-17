package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkspaceScenePatchOwnsEnvelopeAndPreservesCompleteState(t *testing.T) {
	before := slideScene{Version: 1, SlideID: "slide", Width: 1920, Height: 1080, Background: "#fff", Objects: []sceneObject{{ID: "keep", Kind: "shape", Shape: "square"}, {ID: "edit", Kind: "text", Text: &sceneText{Runs: []sceneRun{{Text: "Before"}}}}, {ID: "remove", Kind: "shape"}}}
	old, _ := json.Marshal(before)
	next := before
	next.Background = "#111"
	next.Objects = []sceneObject{before.Objects[1], before.Objects[0], {ID: "new", Kind: "shape"}}
	next.Objects[0].Text = &sceneText{Runs: []sceneRun{{Text: "After"}}}
	patch := scenePatchFor(&before, &next)
	if patch == nil || len(patch.Changed) != 2 || !reflect.DeepEqual(patch.Removed, []string{"remove"}) || patch.Metadata == nil {
		t.Fatal(patch)
	}
	objects := map[string]sceneObject{}
	for _, o := range before.Objects {
		objects[o.ID] = o
	}
	for _, id := range patch.Removed {
		delete(objects, id)
	}
	for _, o := range patch.Changed {
		objects[o.ID] = o
	}
	rebuilt := *patch.Metadata
	rebuilt.Objects = nil
	for _, id := range patch.Order {
		rebuilt.Objects = append(rebuilt.Objects, objects[id])
	}
	a, _ := json.Marshal(rebuilt)
	b, _ := json.Marshal(next)
	after, _ := json.Marshal(before)
	if !bytes.Equal(a, b) || !bytes.Equal(old, after) {
		t.Fatal("patch differs from cold scene or mutates previous snapshot")
	}
	page := exportPage{Slide: 0, Page: 0, Scene: &next}
	wire := workspaceWirePages([]exportPage{page}, map[int]workspacePageEntry{0: {pages: []exportPage{{Slide: 0, Page: 0, Scene: &before}}}}, true)
	if len(wire) != 1 || wire[0].Scene != nil || wire[0].ScenePatch == nil || page.Scene != &next {
		t.Fatal("wire page mutates or sends complete scene")
	}
}

func TestWorkspaceScenePatchConservativeFallback(t *testing.T) {
	original := slideScene{Version: 1, SlideID: "slide", Width: 1920, Height: 1080, Objects: []sceneObject{{ID: "one", Kind: "shape"}, {ID: "two", Kind: "shape"}}}
	for _, change := range []func(*slideScene){
		func(s *slideScene) { s.SlideID = "replacement" }, func(s *slideScene) { s.Version = 2 }, func(s *slideScene) { s.Objects = []sceneObject{{ID: "one"}, {ID: "one"}} }, func(s *slideScene) { s.Objects = []sceneObject{{ID: ""}} }, func(s *slideScene) { s.Objects = []sceneObject{{ID: "one", Kind: "text"}, {ID: "two", Kind: "text"}} },
	} {
		next := original
		change(&next)
		if scenePatchFor(&original, &next) != nil {
			t.Fatal("unsafe patch instead of full fallback")
		}
	}
}
