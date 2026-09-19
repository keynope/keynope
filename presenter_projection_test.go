package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPresenterProjectionHTMLMatchesTransport(t *testing.T) {
	deck := themedSharedTextFixture(4)
	mixed := deck.ResolvedSlides()
	legacy := []Slide{{Elements: []Element{{Kind: "text", Text: "First"}, {Kind: "text", Text: "Overflow", Query: "top=100"}}}, {Elements: []Element{{Kind: "text", Text: "Last"}}}}
	for _, fixture := range []struct {
		name       string
		slides     []Slide
		cols, rows int
	}{
		{"mixed", mixed, 245, 56},
		{"overflow", legacy, 80, 25},
		{"minimum-geometry", legacy, 1, 1},
		{"empty", nil, 245, 56},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			before, _ := json.Marshal(fixture.slides)
			html, pages, err := projectPresenterDocument(filepath.Join(t.TempDir(), "Deck.md"), fixture.slides, fixture.cols, fixture.rows)
			if err != nil {
				t.Fatal(err)
			}
			_, tail, ok := strings.Cut(html, "<script id=\"keynope-data\" type=\"application/json\">")
			if !ok {
				t.Fatal("missing deck payload")
			}
			payload, _, ok := strings.Cut(tail, "</script>")
			if !ok {
				t.Fatal("unterminated deck payload")
			}
			var embedded exportDeck
			if err := json.Unmarshal([]byte(payload), &embedded); err != nil {
				t.Fatal(err)
			}
			if embedded.Cols != max(20, fixture.cols) || embedded.Rows != max(10, fixture.rows) {
				t.Fatal("geometry not normalized")
			}
			var ordered []exportPage
			for i, slide := range fixture.slides {
				want := exportSlidePages(slide, i, len(fixture.slides), embedded.Cols, embedded.Rows)
				if !reflect.DeepEqual(pages[i], want) {
					t.Fatalf("slide %d differs from fresh projection", i)
				}
				ordered = append(ordered, pages[i]...)
			}
			encoded, _ := json.Marshal(ordered)
			embeddedPages, _ := json.Marshal(embedded.Pages)
			if string(encoded) != string(embeddedPages) {
				t.Fatal("HTML and transport pages differ")
			}
			after, _ := json.Marshal(fixture.slides)
			if string(before) != string(after) {
				t.Fatal("source mutated")
			}
			if fixture.name == "overflow" && len(pages[0]) < 2 {
				t.Fatal("fixture did not exercise subpages")
			}
		})
	}
}

func TestPresenterRefreshGenerationOrdering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Deck.md")
	slides := []Slide{{Elements: []Element{{Kind: "text", Text: "Latest local edit"}}}, {Elements: []Element{{Kind: "text", Text: "Changed master content"}}}}
	p := &presenterCompanion{}
	old := p.beginFullRefresh(path)
	p.RefreshActiveSlideAsync(slides, 80, 25)
	if p.publishFullRefresh(old, "STALE", nil) {
		t.Fatal("older full refresh overwrote the newer local edit")
	}
	// First-use font/render setup is substantially slower under the race
	// detector. This is an ordering test, not a latency acceptance test.
	deadline := time.Now().Add(15 * time.Second)
	for {
		p.mu.RLock()
		pending := p.fullPending
		p.mu.RUnlock()
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("promoted refresh did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	wantHTML, wantPages, err := projectPresenterDocument(path, slides, 80, 25)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	if p.html != wantHTML || !reflect.DeepEqual(p.pages, wantPages) || p.state.DeckSlide != -1 {
		t.Error("local edit lost pending deck-wide changes")
	}
	p.mu.RUnlock()

	// A newer complete refresh cancels a queued/debounced single-slide job.
	p.RefreshActiveSlideAsync([]Slide{{Elements: []Element{{Kind: "text", Text: "STALE local"}}}}, 80, 25)
	if err := p.Refresh(path, slides, 80, 25); err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	version := p.state.DeckVersion
	p.mu.RUnlock()
	time.Sleep(150 * time.Millisecond)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.DeckVersion != version || p.html != wantHTML || !reflect.DeepEqual(p.pages, wantPages) {
		t.Fatal("stale local refresh replaced newer full refresh")
	}
}

func TestPresenterSynchronousRefreshSupersedesPendingFull(t *testing.T) {
	p := &presenterCompanion{}
	path := filepath.Join(t.TempDir(), "Deck.md")
	old := p.beginFullRefresh(path)
	if err := p.Refresh(path, nil, 80, 25); err != nil {
		t.Fatal(err)
	}
	if p.publishFullRefresh(old, "STALE", nil) {
		t.Fatal("stale full refresh replaced synchronous document")
	}
	if p.fullPending {
		t.Fatal("successful refresh remained pending")
	}
}

func TestPresenterLocalRefreshPathsCoalesceTogether(t *testing.T) {
	for _, firstLegacy := range []bool{false, true} {
		for _, lastLegacy := range []bool{false, true} {
			for _, sameSlide := range []bool{false, true} {
				t.Run(fmt.Sprintf("legacy-%t-to-%t/same-%t", firstLegacy, lastLegacy, sameSlide), func(t *testing.T) {
					deck := themedSharedTextFixture(1)
					deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
					path := filepath.Join(t.TempDir(), "Deck.md")
					p := &presenterCompanion{}
					if err := p.Refresh(path, deck.ResolvedSlides(), 245, 56); err != nil {
						t.Fatal(err)
					}
					untouched := p.pages[1][0].Scene
					refresh := func(legacy bool, index int) {
						if legacy {
							p.mu.Lock()
							p.state.Slide = index
							p.mu.Unlock()
							p.RefreshActiveSlideAsync(deck.ResolvedSlides(), 245, 56)
						} else {
							p.RefreshDeckSlideAsync(path, cloneDeckForRender(deck), index, 245, 56)
						}
					}
					deck.Slides[0].Elements[0].Text = "First pending change"
					refresh(firstLegacy, 0)
					p.mu.RLock()
					pending := p.slidePending && p.pendingSlide == 0
					p.mu.RUnlock()
					if !pending {
						t.Fatal("local work is not registered before asynchronous projection")
					}
					last := 1
					if sameSlide {
						last = 0
					}
					deck.Slides[last].Elements[0].Text = "Latest pending change"
					refresh(lastLegacy, last)
					deadline := time.Now().Add(15 * time.Second)
					for {
						p.mu.RLock()
						pending = p.fullPending || p.slidePending
						p.mu.RUnlock()
						if !pending {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("refresh did not finish")
						}
						time.Sleep(time.Millisecond)
					}
					_, want, err := projectPresenterDocument(path, deck.ResolvedSlides(), 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					p.mu.RLock()
					defer p.mu.RUnlock()
					if !reflect.DeepEqual(p.pages, want) {
						t.Fatal("coalescing lost a pending slide update")
					}
					if sameSlide && (p.pages[1][0].Scene != untouched || p.state.DeckSlide != 0) {
						t.Fatal("same-slide coalescing rebuilt unrelated content")
					}
					if !sameSlide && p.state.DeckSlide != -1 {
						t.Fatal("cross-slide update did not publish complete refresh")
					}
				})
			}
		}
	}
}

func TestPresenterLocalProjectionAndCrossSlideCoalescing(t *testing.T) {
	deck := themedSharedTextFixture(2)
	deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
	path := filepath.Join(t.TempDir(), "Deck.md")
	p := &presenterCompanion{}
	if err := p.Refresh(path, deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	unchanged := p.pages[0][0].Scene
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			p.mu.RLock()
			pending := p.fullPending || p.slidePending
			p.mu.RUnlock()
			if !pending {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("refresh did not finish")
			}
			time.Sleep(time.Millisecond)
		}
	}
	// Presenter position deliberately differs from the edited slide.
	deck.Slides[1].Elements[0].Text = "Edited second slide"
	p.RefreshDeckSlideAsync(path, cloneDeckForRender(deck), 1, 245, 56)
	wait()
	p.mu.RLock()
	if p.pages[0][0].Scene != unchanged || p.state.DeckSlide != 1 {
		t.Error("local edit replaced unrelated scene or wrong slide")
	}
	want := exportSlidePages(deck.slideRenderPreview(1, 245, 56), 1, 2, 245, 56)
	if !reflect.DeepEqual(p.pages[1], want) {
		t.Error("local scene differs from full projection")
	}
	p.mu.RUnlock()
	// Rapid edits on different slides must preserve both, not cancel the first.
	deck.Slides[0].Elements[0].Text = "First edit"
	p.RefreshDeckSlideAsync(path, cloneDeckForRender(deck), 0, 245, 56)
	deck.Slides[1].Elements[0].Text = "Second edit"
	p.RefreshDeckSlideAsync(path, cloneDeckForRender(deck), 1, 245, 56)
	wait()
	_, pages, err := projectPresenterDocument(path, deck.ResolvedSlides(), 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !reflect.DeepEqual(p.pages, pages) {
		t.Fatal("coalescing lost an edit on another slide")
	}
}

func TestPresenterModernRefreshScopes(t *testing.T) {
	for _, action := range []string{"set-text-size", "set-text-width", "set-modern-text-style", "set-scene-text", "set-scene-crop"} {
		if nativeEditorRefreshScope(action) != "slide" {
			t.Fatalf("%s refreshes whole deck", action)
		}
	}
	for _, action := range []string{"set-theme", "undo", "redo"} {
		if nativeEditorRefreshScope(action) != "deck" {
			t.Fatalf("%s lost full invalidation", action)
		}
	}
	for _, action := range []string{"set-appearance-mode", "set-appearance-profile", "set-style-default", "set-element-style"} {
		if nativeEditorRefreshScope(action) != "" {
			t.Fatalf("retired action %s still schedules a presenter rebuild", action)
		}
	}
}

func TestPresenterClientRejectsStaleSnapshotsAndSerializesPolling(t *testing.T) {
	html, err := exportHTMLDocument("Reliable.md", []Slide{{Elements: []Element{{Kind: "text", Text: "Current"}}}}, 245, 56, preservedExportHead{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"async function refreshPresenterSlide(slideIndex, expectedVersion)",
		"snapshot.version < expectedVersion",
		"snapshot.version < (state.deckVersion || 0)",
		"let presenterStateSync = null;",
		"if (presenterStateSync) return presenterStateSync;",
		"const publicationCurrent = () => {",
		"onboardingSessionDefinition()?.code !== owner.code",
		"if(!publicationCurrent())return;",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("presenter reliability guard missing %q", marker)
		}
	}
}

func TestPresenterHTMLSnapshotUsesCurrentPages(t *testing.T) {
	old := exportDeck{Cols: 245, Rows: 56, Source: "Deck.md", Pages: []exportPage{{Slide: 0, FG: "outdated"}}}
	html, err := exportProjectedHTML(old, preservedExportHead{}, true)
	if err != nil {
		t.Fatal(err)
	}
	deck := themedSharedTextFixture(2)
	pages := exportSlidePages(deck.slideRenderPreview(0, 245, 56), 0, 1, 245, 56)
	pages[0].FG = "</script><script>alert('not markup')</script>"
	p := &presenterCompanion{html: html, pages: map[int][]exportPage{0: pages, 1: {{Slide: 1, Page: 0}, {Slide: 1, Page: 1}}}}
	snapshot, err := p.htmlSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	_, tail, _ := strings.Cut(snapshot, "<script id=\"keynope-data\" type=\"application/json\">")
	payload, _, _ := strings.Cut(tail, "</script>")
	var decoded exportDeck
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatal(err)
	}
	current, _ := p.slideSnapshot()
	want, _ := json.Marshal(exportDeck{Cols: 245, Rows: 56, Source: "Deck.md", Pages: current})
	got, _ := json.Marshal(decoded)
	if string(want) != string(got) {
		t.Fatal("bootstrap diverged from current transport")
	}
	if strings.Contains(snapshot, pages[0].FG) {
		t.Fatal("unsafe script content was not escaped")
	}
	if strings.Count(snapshot, "<style data-keynope-modern-fonts>") != 1 {
		t.Fatal("new mixed scene missing font stylesheet")
	}
	if p.html != html {
		t.Fatal("snapshot mutated cached shell")
	}
	p.html = snapshot
	again, err := p.htmlSnapshot()
	if err != nil || again != snapshot {
		t.Fatal("snapshot rewrite not idempotent")
	}
	for _, invalid := range []string{"", "<script id=\"keynope-data\" type=\"application/json\">", "<script id=\"keynope-data\" type=\"application/json\">bad</script>"} {
		p.html = invalid
		if _, err := p.htmlSnapshot(); err == nil {
			t.Fatal("invalid bootstrap accepted")
		}
	}
}
