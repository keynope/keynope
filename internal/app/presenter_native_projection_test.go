package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func nativeProjectionFixture(t *testing.T) (*nativeEditorSession, *presenterCompanion, string) {
	t.Helper()
	oldCols, oldRows := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = oldCols, oldRows })
	d := themeDependencyFixture("text", false)
	for i := 1; i < 5; i++ {
		s := cloneSlide(d.Slides[0])
		s.ID = newStableID("slide")
		s.Elements[0].ID = newStableID("element")
		d.Slides = append(d.Slides, s)
	}
	d.Slides[3].Elements[0].Query += "&theme-fg=accent-1"
	s := newNativeEditorSession("Fixture.md", d)
	p := &presenterCompanion{}
	path := filepath.Join(t.TempDir(), "Fixture.md")
	if err := p.Refresh(path, s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	s.companion = p
	s.deckPath = path
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	return s, p, path
}

func waitNativeProjection(t *testing.T, p *presenterCompanion) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		p.mu.RLock()
		pending := p.fullPending || p.slidePending || p.nativeProjection != nil && p.nativeProjection.pending
		p.mu.RUnlock()
		if !pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("native projection still pending")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertNativeProjectionMatchesCold(t *testing.T, s *nativeEditorSession, p *presenterCompanion, path string) {
	t.Helper()
	s.mu.RLock()
	d := cloneDeckForScene(s.deck)
	s.mu.RUnlock()
	_, want, err := projectPresenterDocument(path, d.ResolvedSlides(), 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !reflect.DeepEqual(p.pages, want) {
		t.Fatal("native incremental pages differ from cold production projection")
	}
}

func TestNativeProjectionRealCommandsCoalesceAndRetainPages(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	p.mu.RLock()
	untouched := p.pages[2][0].Scene
	base := p.state.DeckVersion
	p.mu.RUnlock()
	for _, index := range []int{0, 1} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: index, SceneRevision: &revision, ObjectIDs: []string{s.deck.Slides[index].Elements[0].ID}, Property: "paint.color", PropertyValue: json.RawMessage(`"#ff00aa"`)}); err != nil {
			t.Fatal(err)
		}
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	if p.nativeProjection.full || !reflect.DeepEqual(p.nativeProjection.projected, []int{0, 1}) || !reflect.DeepEqual(p.nativeProjection.considered, []int{0, 1}) || p.pages[2][0].Scene != untouched {
		t.Fatal("coalescing rendered unrelated pages")
	}
	p.mu.RUnlock()
	patch := p.pageUpdateSince(base)
	if !patch.Delta || !reflect.DeepEqual(patch.ReplaceSlides, []int{0, 1}) || len(patch.Pages) != 2 {
		t.Fatal("coherent multi-slide patch missing", patch.ReplaceSlides)
	}
	// Undo one command must update only its actual consumer, regardless of the
	// current editor/presenter slide and the command's original target.
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !reflect.DeepEqual(p.nativeProjection.projected, []int{1}) || !reflect.DeepEqual(p.nativeProjection.considered, []int{1}) || p.pages[2][0].Scene != untouched {
		t.Fatal("Undo rebuilt unrelated scenes")
	}
}

func TestNativeProjectionThemeConsumersAndStructuralFallback(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	start := p.state.DeckVersion
	theme := s.deck.Theme.Clone()
	theme.Colors.Accent[5] = "#ff00aa"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	same := p.state.DeckVersion == start && len(p.nativeProjection.projected) == 0
	p.mu.RUnlock()
	if !same {
		t.Fatal("unused theme role republished/reprojected deck")
	}
	theme = s.deck.Theme.Clone()
	theme.Colors.Accent[0] = "#aa00ff"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	only := reflect.DeepEqual(p.nativeProjection.projected, []int{3}) && !p.nativeProjection.full
	p.mu.RUnlock()
	if !only {
		t.Fatal("theme role rebuilt non-consuming native pages")
	}
	if err := s.apply(nativeEditorAction{Action: "add-slide"}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	full := p.nativeProjection.full && len(p.nativeProjection.projected) == len(s.deck.Slides)
	p.mu.RUnlock()
	if !full || p.pageUpdateSince(start).Delta {
		t.Fatal("structural edit did not force complete coherent snapshot")
	}
}

func TestNativeProjectionOutOfOrderAndCompatibilityCancellation(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	old := cloneDeckForScene(s.deck)
	latest := cloneDeckForScene(s.deck)
	old.Slides[0].Elements[0].Text = "Older accepted edit"
	latest.Slides[0].Elements[0].Text = old.Slides[0].Elements[0].Text
	latest.Slides[1].Elements[0].Text = "Latest accepted edit"
	p.RefreshEditorDeckAsync(s, path, latest, s.version+2, 245, 56)
	p.RefreshEditorDeckAsync(s, path, old, s.version+1, 245, 56)
	waitNativeProjection(t, p)
	_, want, err := projectPresenterDocument(path, latest.ResolvedSlides(), 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	match := reflect.DeepEqual(p.pages, want) && reflect.DeepEqual(p.nativeProjection.projected, []int{0, 1})
	p.mu.RUnlock()
	if !match {
		t.Fatal("late dispatch lost earlier unrendered change or overwrote latest")
	}
	// Full legacy publication cancels a native job that has not projected yet.
	p.RefreshEditorDeckAsync(s, path, old, s.version+3, 245, 56)
	if err := p.Refresh(path, latest.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	p.mu.RLock()
	match = reflect.DeepEqual(p.pages, want) && !p.nativeProjection.pending
	p.mu.RUnlock()
	if !match {
		t.Fatal("cancelled native job overwrote complete refresh")
	}
	// After an untracked compatibility refresh, don't reuse an assumed baseline.
	p.RefreshEditorDeckAsync(s, path, latest, s.version+4, 245, 56)
	waitNativeProjection(t, p)
	p.mu.RLock()
	full := p.nativeProjection.full
	p.mu.RUnlock()
	if !full {
		t.Fatal("untracked baseline did not rebuild fully")
	}
}

func TestNativeProjectionMasterConsumersAndSessionBinding(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	// Give two pages a real shared layout; the remaining pages do not consume it.
	s.deck.EnsureDefaultMasters()
	s.deck.Slides[0].LayoutID = "blank"
	s.deck.Slides[1].LayoutID = "blank"
	if err := p.Refresh(path, s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	unchanged := p.pages[2][0].Scene
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 1}); err != nil {
		t.Fatal(err)
	}
	layout := cloneSlide(s.deck.Masters.Layouts[0].Slide)
	layout.BG = "#ffaa55"
	layout.BGSet = true
	if err := s.apply(nativeEditorAction{Action: "update-slide", SlideData: &layout}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	ok := !p.nativeProjection.full && reflect.DeepEqual(p.nativeProjection.projected, []int{0, 1}) && p.pages[2][0].Scene == unchanged
	p.mu.RUnlock()
	if !ok {
		t.Fatal("master update rebuilt non-consuming native pages")
	}
	// A detached session cannot replace the attached editor's document.
	other := newNativeEditorSession("Other.md", s.deck)
	before := p.state.DeckVersion
	p.RefreshEditorDeckAsync(other, path, cloneDeckForScene(other.deck), 100, 245, 56)
	time.Sleep(100 * time.Millisecond)
	if p.state.DeckVersion != before {
		t.Fatal("unbound session published")
	}
}

func TestNativeProjectionPreservesBuildActivityAndOverlay(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	defer p.Close()
	s.deck.Objects = &DeckObjects{Version: 1, Builds: map[string]SlideBuild{s.deck.Slides[0].ID: {Version: 1, Steps: []BuildStep{{TargetIDs: []string{s.deck.Slides[0].Elements[0].ID}, Effect: "appear", Advance: "click", DurationMS: 200}}}}}
	if err := p.Refresh(path, s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	p.Update(0, 0, true, nil)
	navigateCursor(t, p, "next")
	cursor := p.BuildSnapshot()
	overlay := p.overlaySnapshot()
	overlay.Mode = "ink"
	overlay.Strokes = []PresentationInk{{ID: "ink", Color: "#ffffff", Width: 6, Points: []PresentationPoint{{.2, .3}, {.4, .5}}}}
	if w := postPresentationOverlay(t, p, 1, overlay); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	beforeOverlay := p.overlaySnapshot()
	activity := &EngagementRuntimeState{Definition: EngagementDefinition{ID: "running", Kind: "pulse", Prompt: "Check in"}, Phase: 1}
	p.mu.Lock()
	p.state.Engagement = activity
	p.mu.Unlock()
	theme := s.deck.Theme.Clone()
	theme.Colors.Accent[0] = "#aa00ff"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	waitNativeProjection(t, p)
	after := p.BuildSnapshot()
	if after.Session != cursor.Session || after.SlideID != cursor.SlideID || after.Step != cursor.Step {
		t.Fatal("scoped native refresh reset playback", cursor, after)
	}
	if !reflect.DeepEqual(beforeOverlay, p.overlaySnapshot()) {
		t.Fatal("scoped refresh discarded presentation ink")
	}
	p.mu.RLock()
	same := p.state.Engagement == activity
	p.mu.RUnlock()
	if !same {
		t.Fatal("scoped refresh interrupted running activity")
	}
}

func TestNativeProjectionCloseCancelsQueuedPublisher(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	deck := cloneDeckForScene(s.deck)
	deck.Slides[0].Elements[0].Text = "Do not publish after Close"
	original := p.pages[0][0].Scene
	version := p.state.DeckVersion
	p.RefreshEditorDeckAsync(s, path, deck, s.version+1, 245, 56)
	p.Close()
	p.RefreshEditorDeckAsync(s, path, deck, s.version+2, 245, 56)
	time.Sleep(120 * time.Millisecond)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.DeckVersion != version || p.pages[0][0].Scene != original || p.nativeProjection.pending || p.fullPending {
		t.Fatal("closed companion accepted or published stale work")
	}
}

func TestPresenterPageChangeLineageBoundedAndOwned(t *testing.T) {
	p := &presenterCompanion{pages: map[int][]exportPage{0: {{Slide: 0}}, 1: {{Slide: 1}}, 2: {{Slide: 2}}}}
	for _, index := range []int{0, 1, 0} {
		p.state.DeckVersion++
		indices := []int{index}
		p.recordPageChangeLocked(false, indices)
		indices[0] = 2
	}
	patch := p.pageUpdateSince(0)
	if !patch.Delta || patch.BaseVersion != 0 || !reflect.DeepEqual(patch.ReplaceSlides, []int{0, 1}) || len(patch.Pages) != 2 {
		t.Fatal("missed-version union/ownership failed")
	}
	patch.ReplaceSlides[0] = 2
	if p.pageUpdateSince(0).ReplaceSlides[0] != 0 {
		t.Fatal("response aliases journal")
	}
	if !p.pageUpdateSince(3).Delta || len(p.pageUpdateSince(3).Pages) != 0 {
		t.Fatal("settled delta not empty")
	}
	if p.pageUpdateSince(-1).Delta || p.pageUpdateSince(100).Delta {
		t.Fatal("invalid lineage did not recover fully")
	}
	p.state.DeckVersion++
	p.recordPageChangeLocked(true, nil)
	if p.pageUpdateSince(3).Delta {
		t.Fatal("structural history returned partial deck")
	}
	for i := 0; i < 70; i++ {
		p.state.DeckVersion++
		p.recordPageChangeLocked(false, []int{i % 3})
	}
	if len(p.pageChanges) != 64 || p.pageUpdateSince(0).Delta || !p.pageUpdateSince(p.state.DeckVersion-2).Delta {
		t.Fatal("history bound/recovery failed")
	}
	// An unrecorded version must never pretend to provide complete lineage.
	p.state.DeckVersion++
	if p.pageUpdateSince(p.state.DeckVersion - 1).Delta {
		t.Fatal("gap returned partial data")
	}
}

// Loopback-only owned fixture for a headless test of the actual native HTTP
// transport and generated client. It never opens or activates the Mac app.
func TestNativeProjectionBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_NATIVE_PROJECTION_EVIDENCE")
	if root == "" {
		t.Skip("browser fixture not requested")
	}
	s, p, _ := nativeProjectionFixture(t)
	p.Update(0, 0, true, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := p.htmlSnapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/state", p.handleState)
	mux.HandleFunc("/slides", p.handlePageUpdate)
	mux.HandleFunc("/api/editor/action", s.handleAction)
	mux.HandleFunc("/api/editor/state", s.handleState)
	mux.HandleFunc("/proof", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p.mu.RLock()
		defer p.mu.RUnlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"pending": p.nativeProjection.pending, "projected": p.nativeProjection.projected, "full": p.nativeProjection.full, "version": p.state.DeckVersion, "requestedRevision": p.nativeProjection.revision, "cols": authoredTerminalWidth, "rows": authoredTerminalHeight})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]any{"url": server.URL})
	if err := os.WriteFile(filepath.Join(root, "connection.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("browser fixture timed out")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
