package main

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestWorkspaceEntrySizeMatchesCompleteEncoding(t *testing.T) {
	corpus := powerupProofCorpus(t)
	var previous workspacePageEntry
	check := func(pages []exportPage, input Slide) {
		t.Helper()
		oldJSON, _ := json.Marshal(previous.pages)
		got, costs, err := workspaceEntryEncodedSize(pages, input, previous)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(struct {
			Pages []exportPage
			Input Slide
		}{pages, input})
		if err != nil || got != len(encoded) {
			t.Fatalf("size=%d complete=%d error=%v", got, len(encoded), err)
		}
		oldAfter, _ := json.Marshal(previous.pages)
		if string(oldJSON) != string(oldAfter) {
			t.Fatal("modified previous entry")
		}
		previous = workspacePageEntry{pages: pages, input: input, objectSizes: costs}
	}
	check(nil, Slide{})
	check([]exportPage{}, Slide{})
	check([]exportPage{{}}, Slide{})
	for _, objects := range [][]sceneObject{nil, {}, {{ID: "one", Kind: "text", Text: &sceneText{Runs: []sceneRun{{Text: "<&> ☃ \""}}}}}} {
		check([]exportPage{{Scene: &slideScene{Version: 1, Objects: objects}}}, Slide{})
	}
	for _, deck := range corpus {
		for i := range deck.Slides {
			pages := editorSlideWorkspacePages(deck, i, 245, 56)
			check(pages, workspaceSlideInput(deck.ResolveSlide(i, false)))
			// A repeated rendering must exercise reusable costs with exact output.
			check(pages, workspaceSlideInput(deck.ResolveSlide(i, false)))
		}
	}
}

func TestWorkspaceSizeEditsReorderAndFailure(t *testing.T) {
	before := []exportPage{{Scene: &slideScene{Version: 1, Objects: []sceneObject{{ID: "one", Kind: "text"}, {ID: "two", Kind: "shape"}}}}}
	_, costs, err := workspaceEntryEncodedSize(before, Slide{}, workspacePageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	previous := workspacePageEntry{pages: before, objectSizes: costs}
	oldCosts := append([]int(nil), costs[0]...)
	for _, objects := range [][]sceneObject{
		{before[0].Scene.Objects[1], before[0].Scene.Objects[0]},
		{{ID: "one", Kind: "text", Text: &sceneText{Runs: []sceneRun{{Text: "changed"}}}}, before[0].Scene.Objects[1]},
		{{ID: "duplicate"}, {ID: "duplicate"}, {ID: ""}},
		{}, nil,
	} {
		scene := *before[0].Scene
		scene.Objects = objects
		pages := []exportPage{{Scene: &scene}, {Page: 1}}
		got, _, err := workspaceEntryEncodedSize(pages, Slide{}, previous)
		want, _ := json.Marshal(struct {
			Pages []exportPage
			Input Slide
		}{pages, Slide{}})
		if err != nil || got != len(want) {
			t.Fatalf("got %d want %d: %v", got, len(want), err)
		}
	}
	if !reflect.DeepEqual(costs[0], oldCosts) {
		t.Fatal("mutated reusable costs")
	}
	bad := *before[0].Scene
	bad.Width = math.NaN()
	if _, _, err := workspaceEntryEncodedSize([]exportPage{{Scene: &bad}}, Slide{}, previous); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if workspaceSizeBookkeeping(costs) <= len(costs[0])*4 {
		t.Fatal("size index not charged to retention")
	}
}
