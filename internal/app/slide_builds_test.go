package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func buildEditorFixture(master bool) *nativeEditorSession {
	s := newNativeEditorSession("Builds.md", Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "slide-build-proof", PageNumber: pageNumberHide, Elements: []Element{
		{ID: "heading", Kind: "text", Text: "Tell the story", Query: "modern-font=sans&modern-size=70&top=4&left=10"},
		{ID: "first", Kind: "shape", Text: "[shape:circle]", Query: "shape=circle&top=18&left=15&width=50&height=20&fg=%2300aacc"},
		{ID: "second", Kind: "text", Text: "Then the next idea", Query: "modern-font=c64&modern-size=60&top=25&left=90"},
	}}}})
	if master {
		s.masterMode = true
		s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		s.deck.Masters.Base.Slide.ID = "build-master"
	}
	s.savedDeck = cloneDeck(s.deck)
	s.untitled = false
	s.dirtyOverride = false
	return s
}

func TestSlideBuildVisualFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_BUILD_EVIDENCE")
	if output == "" {
		t.Skip("set KEYNOPE_BUILD_EVIDENCE for owned build fixtures")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	s := buildEditorFixture(false)
	s.deck.Slides[0].BG = "48;2;20;28;40"
	s.deck.Slides[0].Elements[0].Query += "&fg=%23ffffff&width=225&height=9"
	s.deck.Slides[0].Elements[2].Query += "&fg=%23aaff55&width=140&height=10"
	md, err := serializeDeck("Builds.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "builds.md"), md, 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(buildAction(s, ownedBuild())); err != nil {
		t.Fatal(err)
	}
	html, err := exportHTMLDocument("Builds.md", s.deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "builds.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
	transitions := Deck{Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Builds: map[string]SlideBuild{}}, Slides: []Slide{
		{ID: "transition-first", BG: "48;2;192;64;48", PageNumber: pageNumberHide, Elements: []Element{{ID: "outgoing-title", Kind: "text", Text: "Outgoing slide", Query: "modern-font=sans&modern-size=100&top=12&left=25&width=210&height=12&text-box=1&fg=%23ffffff"}}},
		{ID: "transition-second", BG: "48;2;36;92;200", PageNumber: pageNumberHide, Elements: []Element{{ID: "incoming-title", Kind: "text", Text: "Incoming slide", Query: "modern-font=sans&modern-size=100&top=12&left=25&width=210&height=12&text-box=1&fg=%23ffffff"}}},
	}}
	transitions.Objects.Builds["transition-second"] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "push", Direction: "left", DurationMS: 1500}}
	for _, extension := range []string{"md", "html"} {
		var data []byte
		if extension == "md" {
			data, err = serializeDeck("Transitions.md", transitions)
		} else {
			var rendered string
			rendered, err = exportHTMLDocument("Transitions.md", transitions.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
			data = []byte(rendered)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "transitions."+extension), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestActivityBuildVisualFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_BUILD_EVIDENCE")
	if output == "" {
		t.Skip("set KEYNOPE_BUILD_EVIDENCE for owned fixtures")
	}
	s := buildEditorFixture(false)
	s.deck.Slides[0].Engagement = &EngagementDefinition{ID: "owned-pulse", Kind: "pulse", Prompt: "Choose your pace", Options: append([]string(nil), defaultPulseOptions...), Named: true, TimerSeconds: 120}
	s.deck.Slides[0].Elements[0].Query += "&fg=%23ffffff&width=225&height=9"
	build := ownedBuild()
	build.Steps[1].Advance = "click"
	build.Steps[1].DelayMS = 0
	if err := s.apply(buildAction(s, build)); err != nil {
		t.Fatal(err)
	}
	s.deck.Slides = append(s.deck.Slides, Slide{ID: "after-activity", Elements: []Element{{ID: "after-title", Kind: "text", Text: "Keep going", Query: "modern-font=sans&modern-size=100&top=12&left=25&width=210&height=12&text-box=1&fg=%23ffffff"}}})
	md, err := serializeDeck("ActivityBuilds.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "activity-builds.md"), md, 0644); err != nil {
		t.Fatal(err)
	}
	html, err := exportHTMLDocument("ActivityBuilds.md", s.deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "activity-builds.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
}
func buildAction(s *nativeEditorSession, build *SlideBuild) nativeEditorAction {
	revision := s.version
	index := s.current
	if s.masterMode {
		index = s.currentMaster
	}
	return nativeEditorAction{Action: "set-slide-build", SceneRevision: &revision, SceneMaster: s.masterMode, Slide: index, BuildData: build}
}
func ownedBuild() *SlideBuild {
	return &SlideBuild{Version: 1, Transition: SlideTransition{Effect: "fade", DurationMS: 400}, Steps: []BuildStep{{TargetIDs: []string{"first"}, Effect: "fade", DurationMS: 500, Advance: "click"}, {TargetIDs: []string{"second"}, Effect: "wipe", Direction: "left", DurationMS: 700, Advance: "after", DelayMS: 300}}}
}

func TestTVSwitchTransitionPersistence(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := buildEditorFixture(master)
		build := ownedBuild()
		build.Transition = SlideTransition{Effect: "tv-switch", DurationMS: 180}
		if err := s.apply(buildAction(s, build)); err != nil {
			t.Fatal(err)
		}
		target := slidesForBuildState(s, 0)
		data, err := serializeDeck("Switching.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Switching.md", data)
		if err != nil || loaded.Objects.Builds[target.ID].Transition != build.Transition {
			t.Fatal("TV switching lost on save/reopen", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || s.deck.Objects != nil && len(s.deck.Objects.Builds) != 0 {
			t.Fatal("TV switching was not undoable", err)
		}
	}
}

func TestSlideBuildAtomicHistoryAndPortability(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := buildEditorFixture(master)
		before := cloneDeck(s.deck)
		initialVersion := s.version
		build := ownedBuild()
		if err := s.apply(buildAction(s, build)); err != nil {
			t.Fatal(err)
		}
		if s.version != initialVersion+1 || len(s.undo) != 1 || !s.dirtyLocked() {
			t.Fatal("not one undoable change")
		}
		target := slidesForBuildState(s, 0)
		if !reflect.DeepEqual(target.Elements, slidesForBuildState(&nativeEditorSession{deck: before, masterMode: master}, 0).Elements) {
			t.Fatal("build edit changed element geometry/order/text")
		}
		build.Steps[0].TargetIDs[0] = "mutated"
		if s.deck.Objects.Builds[target.ID].Steps[0].TargetIDs[0] != "first" {
			t.Fatal("build targets not owned")
		}
		version := s.version
		if err := s.apply(buildAction(s, selectedSlideBuild(s.deck, target))); err != nil || s.version != version || len(s.undo) != 1 {
			t.Fatal("exact no-op changed history", err)
		}
		md, err := serializeDeck("Builds.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Builds.md", md)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded.Objects.Builds[target.ID], s.deck.Objects.Builds[target.ID]) {
			t.Fatal("build metadata lost on reopen")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("undo changed source", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || len(s.deck.Objects.Builds) != 1 {
			t.Fatal("redo lost timeline", err)
		}
		if err := s.apply(buildAction(s, &SlideBuild{Version: 1})); err != nil || len(s.deck.Objects.Builds) != 0 {
			t.Fatal("clear failed", err)
		}
	}
}

func TestSlideBuildRejectsStaleOrInvalidWithoutMutation(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, kind := range []string{"revision", "scope", "slide", "target", "duplicate", "locked", "hidden", "duration", "version"} {
			s := buildEditorFixture(master)
			a := buildAction(s, ownedBuild())
			target := &s.deck.Slides[0]
			if master {
				target = &s.deck.Masters.Base.Slide
			}
			switch kind {
			case "revision":
				*a.SceneRevision = 99
			case "scope":
				a.SceneMaster = !master
			case "slide":
				a.Slide = 3
			case "target":
				a.BuildData.Steps[1].TargetIDs = []string{"gone"}
			case "duplicate":
				a.BuildData.Steps[1].TargetIDs = []string{"first"}
			case "locked":
				target.Elements[1].Query += "&object-locked=1"
			case "hidden":
				target.Elements[1].Query += "&object-hidden=1"
			case "duration":
				a.BuildData.Steps[1].DurationMS = 60001
			case "version":
				a.BuildData.Version = 2
			}
			assertEditorFailureUnchanged(t, s, a)
		}
	}
}

func TestSlideBuildGroupExpansionAndDeleteReconciliation(t *testing.T) {
	s := buildEditorFixture(false)
	s.deck.Slides[0].Elements[1].Query += "&group=pair"
	s.deck.Slides[0].Elements[2].Query += "&group=pair"
	build := &SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"second"}, Effect: "appear", Advance: "click"}}}
	if err := s.apply(buildAction(s, build)); err != nil {
		t.Fatal(err)
	}
	stored := s.deck.Objects.Builds[s.deck.Slides[0].ID]
	if !reflect.DeepEqual(stored.Steps[0].TargetIDs, []string{"first", "second"}) {
		t.Fatal("group was not expanded deterministically", stored)
	}
	if err := s.apply(nativeEditorAction{Action: "delete-element", Element: 1}); err != nil {
		t.Fatal(err)
	}
	remaining := s.deck.Objects.Builds[s.deck.Slides[0].ID].Steps
	if len(remaining) != 1 || !reflect.DeepEqual(remaining[0].TargetIDs, []string{"second"}) {
		t.Fatal("deleted target not reconciled", remaining)
	}
	index := -1
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "second" {
			index = i
		}
	}
	if err := s.apply(nativeEditorAction{Action: "delete-element", Element: index}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Objects.Builds[s.deck.Slides[0].ID].Steps) != 0 {
		t.Fatal("empty step retained")
	}
}

func TestSlideBuildAudienceSceneStripsExtensionsAndStaticVisibility(t *testing.T) {
	s := buildEditorFixture(false)
	if err := s.apply(buildAction(s, ownedBuild())); err != nil {
		t.Fatal(err)
	}
	b := s.deck.Objects.Builds[s.deck.Slides[0].ID]
	extra := jsonExtensions{"private": json.RawMessage(`"SECRET"`)}
	b.Extra = &extra
	b.Transition.Extra = &extra
	b.Steps[0].Extra = &extra
	s.deck.Objects.Builds[s.deck.Slides[0].ID] = b
	scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(scene)
	if scene.Build == nil || len(scene.Build.Steps) != 2 || scene.SlideID != "slide-build-proof" || bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("unsafe/missing scene build")
	}
	if len(scene.Objects) != 3 {
		t.Fatal("authoring/static projection hid objects")
	}
	manifest, _, err := projectPDFExport("Builds.md", s.deck, 245, 56)
	if err != nil || len(manifest.Pages[0].Page.Scene.Objects) != 3 {
		t.Fatal("PDF does not use final visible state", err)
	}
	data, report, err := exportPPTXDocument(s.deck)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code == "timeline-omitted" {
			t.Fatal("PPTX discarded supported builds")
		}
	}
	if bytes.Contains(data, []byte("SECRET")) {
		t.Fatal("private build extensions leaked")
	}
	imported, _, err := importPPTXDocument(data, "Builds.pptx")
	if err != nil || imported.Objects == nil || len(imported.Objects.Builds[imported.Slides[0].ID].Steps) != 2 {
		t.Fatal("PPTX did not retain supported builds", err)
	}
}

func TestSlideBuildMasterSceneAndProtectedPublicTargets(t *testing.T) {
	s := buildEditorFixture(true)
	if err := s.apply(buildAction(s, ownedBuild())); err != nil {
		t.Fatal(err)
	}
	scene, err := buildStyledSlideScene(s.deck, masterViewPreview(s.deck.Masters, 0), 0, 245, 56, true)
	if err != nil || scene.Build == nil || len(scene.Build.Steps) != 2 {
		t.Fatal("master preview lost authored builds", err)
	}
	scene.Objects = append(scene.Objects, sceneObject{ID: "private-qr", Kind: "image"})
	scene.Build.Steps = append(scene.Build.Steps, BuildStep{TargetIDs: []string{"private-qr"}, Effect: "appear"})
	slide := standaloneExportSlide(Slide{ModernScene: &scene, Elements: []Element{{ID: "private-qr", Kind: "image", PlaceholderRole: activityQRCodeRole}}}, 0)
	for _, step := range slide.ModernScene.Build.Steps {
		for _, id := range step.TargetIDs {
			if id == "private-qr" {
				t.Fatal("public timeline retained private QR target")
			}
		}
	}
}
