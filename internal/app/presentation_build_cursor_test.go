package app

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func cursorCompanionFixture() *presenterCompanion {
	first := ownedBuild()
	first.Steps[1].Advance = "click"
	p := &presenterCompanion{pages: map[int][]exportPage{
		0: {{Slide: 0, Page: 0, Scene: &slideScene{SlideID: "first-slide", Build: first}}},
		1: {{Slide: 1, Page: 0, TabOnly: true, Scene: &slideScene{SlideID: "tab"}}},
		2: {{Slide: 2, Page: 0, Scene: &slideScene{SlideID: "last-slide", Build: &SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"third"}, Effect: "appear"}}}}}},
	}}
	p.Update(0, 0, true, nil)
	return p
}
func navigateCursor(t *testing.T, p *presenterCompanion, name string) {
	t.Helper()
	c := p.BuildSnapshot()
	p.mu.Lock()
	_, err := p.navigateBuildLocked(name, &c.Generation)
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}
func awaitCursorStep(t *testing.T, p *presenterCompanion, step int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c := p.BuildSnapshot()
		if c != nil && c.Step == step {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("cursor did not reach step", step, p.BuildSnapshot())
}

func TestPresentationBuildCursorNavigationAndSessions(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	initial := p.BuildSnapshot()
	if initial.Step != 0 || initial.SlideID != "first-slide" || initial.Session == "" {
		t.Fatal(initial)
	}
	navigateCursor(t, p, "next")
	if p.BuildSnapshot().Step != 1 || p.state.Slide != 0 {
		t.Fatal("skipped first build")
	}
	navigateCursor(t, p, "next")
	if p.BuildSnapshot().Step != 2 {
		t.Fatal("skipped last build")
	}
	navigateCursor(t, p, "next")
	if p.state.Slide != 2 || p.BuildSnapshot().Step != 0 {
		t.Fatal("did not skip participant tab")
	}
	navigateCursor(t, p, "previous")
	if p.state.Slide != 0 || p.BuildSnapshot().Step != 2 {
		t.Fatal("backward entry must use final state")
	}
	navigateCursor(t, p, "previous")
	if p.BuildSnapshot().Step != 1 {
		t.Fatal("backward build navigation failed")
	}
	navigateCursor(t, p, "first")
	if p.BuildSnapshot().Step != 0 || p.state.Slide != 0 {
		t.Fatal("Home did not restart")
	}
	navigateCursor(t, p, "last")
	if p.state.Slide != 2 || p.BuildSnapshot().Step != 1 {
		t.Fatal("End did not use final state")
	}
	stale := initial.Generation
	before := p.BuildSnapshot()
	p.mu.Lock()
	_, err := p.navigateBuildLocked("next", &stale)
	p.mu.Unlock()
	if err == nil || !reflect.DeepEqual(before, p.BuildSnapshot()) {
		t.Fatal("stale input changed cursor")
	}
	p.Update(2, 0, false, nil)
	if p.BuildSnapshot() != nil {
		t.Fatal("stop retained active cursor")
	}
	p.Update(0, 0, true, nil)
	if p.BuildSnapshot().Session == initial.Session {
		t.Fatal("restart reused stopped session")
	}
}

func setCursorStatus(t *testing.T, p *presenterCompanion, paused bool, mode string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"mode": mode, "paused": paused})
	r := httptest.NewRequest("POST", "/api/presenter-status", strings.NewReader(string(data)))
	w := httptest.NewRecorder()
	p.handlePresenterStatus(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPresentationBuildCursorAutomaticPauseRefreshAndCancellation(t *testing.T) {
	oldMode := nativeAppModeActive
	nativeAppModeActive = true
	defer func() { nativeAppModeActive = oldMode }()
	p := cursorCompanionFixture()
	defer p.Close()
	p.mu.Lock()
	page := p.pages[0][0]
	build := cloneBuild(*page.Scene.Build)
	build.Transition.DurationMS = 0
	build.Steps[0].Advance = "after"
	build.Steps[0].DelayMS = 100
	build.Steps[0].DurationMS = 0
	page.Scene = &slideScene{SlideID: "first-slide", Build: &build}
	p.pages[0] = []exportPage{page}
	p.buildDefinition = nil
	p.maintainBuildCursorLocked()
	p.cursorLocked(0, "enter", false)
	p.scheduleNextBuildLocked(0)
	p.mu.Unlock()
	setCursorStatus(t, p, true, "main")
	paused := p.BuildSnapshot()
	time.Sleep(150 * time.Millisecond)
	if !paused.Paused || p.BuildSnapshot().Step != 0 {
		t.Fatal("auto callback fired while paused")
	}
	setCursorStatus(t, p, false, "main")
	awaitCursorStep(t, p, 1)
	navigateCursor(t, p, "previous")
	time.Sleep(150 * time.Millisecond)
	if p.BuildSnapshot().Step != 0 {
		t.Fatal("backward restarted automatic builds")
	}
	// Re-enter while an automatic callback is pending: stopping must make it inert.
	navigateCursor(t, p, "first")
	setCursorStatus(t, p, false, "none")
	time.Sleep(150 * time.Millisecond)
	if p.BuildSnapshot() != nil {
		t.Fatal("stopped session resurrected")
	}
	setCursorStatus(t, p, false, "main")
	p.mu.Lock()
	p.cursorLocked(0, "enter", false)
	p.scheduleNextBuildLocked(0)
	p.mu.Unlock()
	seq := p.beginFullRefresh("proof.md")
	frozen := p.BuildSnapshot()
	time.Sleep(150 * time.Millisecond)
	if p.BuildSnapshot().Step != 0 {
		t.Fatal("pending refresh advanced old source")
	}
	p.mu.RLock()
	pages := p.pages
	p.mu.RUnlock()
	if !p.publishFullRefresh(seq, "fixture", pages) {
		t.Fatal("refresh not published")
	}
	awaitCursorStep(t, p, 1)
	if p.BuildSnapshot().Generation <= frozen.Generation {
		t.Fatal("new source retained stale callback generation")
	}
}

func TestPresentationBuildNavigationDoesNotDirtyOrCreateHistory(t *testing.T) {
	s := buildEditorFixture(false)
	s.companion = cursorCompanionFixture()
	defer s.companion.Close()
	before, version := cloneDeck(s.deck), s.version
	c := s.companion.BuildSnapshot()
	if err := s.apply(nativeEditorAction{Action: "navigate-build", Name: "next", BuildGeneration: &c.Generation}); err != nil {
		t.Fatal(err)
	}
	if s.version != version || len(s.undo) != 0 || s.dirtyLocked() || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("runtime step changed authored state")
	}
	state := s.state()
	if state.PresentationCursor == nil || state.PresentationCursor.Step != 1 {
		t.Fatal("editor did not expose shared cursor")
	}
	state.PresentationCursor.Step = 99
	if s.companion.BuildSnapshot().Step != 1 {
		t.Fatal("cursor snapshot aliases runtime")
	}
}

func TestPresentationBuildCursorSourceChangeClampsWithoutReplay(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	navigateCursor(t, p, "next")
	navigateCursor(t, p, "next")
	before := p.BuildSnapshot()
	seq := p.beginFullRefresh("proof.md")
	p.mu.RLock()
	pages := make(map[int][]exportPage, len(p.pages))
	for index, source := range p.pages {
		pages[index] = append([]exportPage(nil), source...)
	}
	p.mu.RUnlock()
	build := cloneBuild(*pages[0][0].Scene.Build)
	build.Steps = build.Steps[:1]
	build.Steps[0].Advance = "after"
	build.Steps[0].DelayMS = 1
	pages[0][0].Scene = &slideScene{SlideID: "first-slide", Build: &build}
	if !p.publishFullRefresh(seq, "fixture", pages) {
		t.Fatal("source update rejected")
	}
	after := p.BuildSnapshot()
	if after.Session != before.Session || after.Step != 1 || after.Motion != "seek" || after.Generation <= before.Generation {
		t.Fatal("edited timeline replayed instead of preserving/clamping cursor", after)
	}
	time.Sleep(40 * time.Millisecond)
	if !reflect.DeepEqual(after, p.BuildSnapshot()) {
		t.Fatal("rebase restarted old automatic callbacks")
	}
	p.Close()
	if p.BuildSnapshot() != nil {
		t.Fatal("closed companion retained playback")
	}
}
