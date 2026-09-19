package main

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceMasterUsesMixedScene(t *testing.T) {
	deck := themedSharedTextFixture(2)
	deck.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Slide: deck.Slides[0]}, Layouts: []MasterLayout{{ID: "layout", Slide: Slide{PageNumber: "hide"}}}}
	before, _ := json.Marshal(deck)
	for _, master := range []int{0, 1} {
		pages, err := editorWorkspacePages(deck, true, master, 245, 56)
		if err != nil || len(pages) != 1 || pages[0].Scene == nil {
			t.Fatalf("master %d: %v %+v", master, err, pages)
		}
		if len(pages[0].Scene.Objects) != 2 {
			t.Fatal("missing inherited master objects")
		}
		styles := map[string]bool{}
		for _, object := range pages[0].Scene.Objects {
			styles[object.Style] = true
		}
		if !styles["retro"] || !styles["modern"] {
			t.Fatal("lost mixed treatment")
		}
	}
	for _, master := range []int{-1, 2} {
		if _, err := editorWorkspacePages(deck, true, master, 245, 56); err == nil {
			t.Fatal("invalid master accepted")
		}
	}
	if _, err := editorWorkspacePages(deck, false, 0, 0, 56); err == nil {
		t.Fatal("invalid geometry accepted")
	}
	pages, err := editorWorkspacePages(deck, false, 0, 320, 90)
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].Scene.Width != 1920 || pages[0].Scene.Height != 1080 {
		t.Fatal("normal workspace did not use the shared scene geometry")
	}
	after, _ := json.Marshal(deck)
	if string(before) != string(after) {
		t.Fatal("workspace mutated source deck")
	}
}

func TestWorkspaceAnnotatesOnlyAuthoredObjectsForEditing(t *testing.T) {
	deck := sceneFixture(t)
	pages, err := editorWorkspacePages(deck, false, 0, 245, 56)
	if err != nil || len(pages) != 1 || pages[0].Scene == nil {
		t.Fatalf("workspace: %v %+v", err, pages)
	}
	editable := map[string]bool{}
	for _, object := range pages[0].Scene.Objects {
		editable[object.ID] = object.Editable
	}
	for _, id := range []string{"title", "body", "box", "circle", "image"} {
		if !editable[id] {
			t.Fatalf("authored workspace object %q is not editable", id)
		}
	}
	if editable["line"] {
		t.Fatal("connector unexpectedly exposed a text/media edit dialog")
	}

	presented := exportSlidePages(deck.slideRenderPreview(0, 245, 56), 0, 1, 245, 56)
	for _, object := range presented[0].Scene.Objects {
		if object.Editable {
			t.Fatalf("presentation object %q leaked editor-only editability", object.ID)
		}
	}
}
