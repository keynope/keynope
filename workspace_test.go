package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestResponsiveWorkspaceDrawerAndStateCuesAreEmbedded(t *testing.T) {
	for _, marker := range []string{
		"keynope-inspector-scrim",
		"Close Format inspector",
		"root.dataset.keynopeInspectorDrawer",
		"root.dataset.keynopeInspectorOpen",
		"closeInspector(true)",
		"event.stopImmediatePropagation();closeInspector(true)",
		"snap.dataset.state=snapping?'ON':'OFF'",
		"rulers.dataset.state=rulersOn?'ON':'OFF'",
	} {
		if !strings.Contains(workspaceJS, marker) {
			t.Fatalf("workspace script is missing responsive/state contract %q", marker)
		}
	}
	for _, marker := range []string{
		".keynope-inspector-scrim:not([hidden])",
		".keynope-inspector-close { display:block; }",
		"button[data-state]::after",
		"button[aria-pressed=\"true\"]::after",
		".keynope-slide-item[aria-current=\"page\"]::after",
		".keynope-ribbon-tab[aria-selected=\"true\"]",
	} {
		if !strings.Contains(workspaceCSS, marker) {
			t.Fatalf("workspace CSS is missing non-colour/responsive cue %q", marker)
		}
	}
}

func TestMasterThumbnailDoesNotNavigateOrMutate(t *testing.T) {
	s := newNativeEditorSession("Masters.md", sceneFixture(t))
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	before := cloneDeck(s.deck)
	revision, current := s.version, s.currentMaster
	request := func(master string, version int64) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleWorkspace(w, httptest.NewRequest("GET", fmt.Sprintf("/api/editor/workspace?master=%s&revision=%d&cols=245&rows=56", master, version), nil))
		return w
	}
	w := request("2", revision)
	if w.Code != 200 {
		t.Fatalf("preview failed: %s", w.Body.String())
	}
	var pages []exportPage
	if err := json.Unmarshal(w.Body.Bytes(), &pages); err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 || pages[0].Slide != 2 {
		t.Fatal("preview returned current master rather than requested one")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("master previews must not be cached across edits")
	}
	for _, master := range []string{"-1", "999", "invalid"} {
		if request(master, revision).Code != 400 {
			t.Fatalf("invalid master %s accepted", master)
		}
	}
	if request("2", revision+1).Code != 409 {
		t.Fatal("stale thumbnail version accepted")
	}
	if s.currentMaster != current || s.version != revision || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("thumbnail request changed editor state")
	}
}

func TestModernMasterWorkspaceAndOpacity(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 1, Mode: "modern"}
	s := newNativeEditorSession("Masters.md", deck)
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	// Use the title master, not a normal slide with a coincident index.
	s.currentMaster = 2
	target := masterSlideAt(&s.deck, s.currentMaster)
	target.Elements = []Element{{ID: "master-opacity", Kind: "text", Text: "Master only", Query: "top=2&left=2"}}
	before := cloneDeck(s.deck)
	element := target.Elements[0]
	element.Query = setQueryValue(element.Query, "modern-opacity", "0.25")
	if err := s.apply(nativeEditorAction{Action: "update-elements", ElementIndices: []int{0}, ElementsData: []Element{element}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleWorkspace(w, httptest.NewRequest("GET", "/api/editor/workspace?cols=245&rows=56", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var pages []exportPage
	if err := json.Unmarshal(w.Body.Bytes(), &pages); err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Scene == nil {
		t.Fatal("master workspace did not use Modern scene")
	}
	found := false
	for _, object := range pages[0].Scene.Objects {
		if object.ID == element.ID {
			found = object.Paint.Opacity != nil && *object.Paint.Opacity == .25
		}
	}
	if !found {
		t.Fatal("master opacity not projected")
	}
	if !reflect.DeepEqual(s.deck.Slides, before.Slides) {
		t.Fatal("master edit changed normal slides")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("master opacity undo was not exact")
	}
}

func TestModernMasterShapedEditScopeAndHistory(t *testing.T) {
	s := newNativeEditorSession("Masters.md", sceneFixture(t))
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	s.currentMaster = 1
	s.deck.Masters.Layouts[0].Slide.Elements = []Element{{Kind: "text", ID: "master-text", Text: "Original", Query: "top=4&left=5"}}
	before := cloneDeck(s.deck)
	revision := s.version
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var scene slideScene
	if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	if !scene.Master || scene.Revision == nil || *scene.Revision != revision {
		t.Fatal("scene lost its master scope/revision")
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "master-text" {
			found = object.Editable
		} else if object.Editable {
			t.Fatal("inherited object exposed for editing")
		}
	}
	if !found {
		t.Fatal("authored master text is not editable")
	}
	action := nativeEditorAction{Action: "set-scene-text", SceneMaster: true, SceneRevision: &revision, Slide: 1, ObjectID: "master-text", TextRuns: []sceneRun{{Text: "Changed", Bold: true}}}
	wrong := action
	wrong.SceneMaster = false
	if s.apply(wrong) == nil {
		t.Fatal("normal-slide command accepted in master mode")
	}
	wrong = action
	wrong.Slide = 0
	if s.apply(wrong) == nil {
		t.Fatal("wrong master accepted")
	}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Slides, s.deck.Slides) {
		t.Fatal("master text edit changed normal slides")
	}
	if s.deck.Masters.Layouts[0].Slide.Elements[0].Text != "Changed" {
		t.Fatal("text was not committed")
	}
	after := cloneDeck(s.deck)
	if s.apply(action) == nil {
		t.Fatal("stale master edit accepted")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("master undo changed source")
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, s.deck) {
		t.Fatal("master redo changed source")
	}
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	latest := s.version
	action.SceneRevision = &latest
	if s.apply(action) == nil {
		t.Fatal("master command accepted in normal mode")
	}
}

func TestModernMasterCropPreservesSources(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement = nil
	deck.Slides[0].EngagementResult = nil
	image := deck.Slides[0].Elements[len(deck.Slides[0].Elements)-1]
	s := newNativeEditorSession("Master image.md", deck)
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	s.deck.Masters.Base.Slide.Elements = []Element{image}
	before := cloneDeck(s.deck)
	revision, mask := s.version, "ellipse"
	action := nativeEditorAction{Action: "set-scene-crop", SceneMaster: true, SceneRevision: &revision, Slide: 0, ObjectID: image.ID, Crop: &sceneCrop{Left: .1, Right: .2}, ModernMask: &mask}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck.Assets, before.Assets) || !reflect.DeepEqual(s.deck.Slides, before.Slides) {
		t.Fatal("master crop mutated image source or normal slides")
	}
	data, err := serializeDeck("Master image.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Master image.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Masters.Base.Slide.Elements[0].Query != s.deck.Masters.Base.Slide.Elements[0].Query {
		t.Fatal("master crop not preserved on reopen")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("master crop undo was not exact")
	}
}
