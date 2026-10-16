package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nativeCandidateChanges(before, after Deck, candidates []int) []int {
	var changed []int
	for _, page := range candidates {
		if !sceneProjectionInputsEqual(before, after, sceneResponseKey{index: page, cols: 245, rows: 56}) {
			changed = append(changed, page)
		}
	}
	return changed
}

func nativeDependencyFixture() Deck {
	d := workspaceConsumerFixture()
	d.Objects.Charts = structuredFixture().Objects.Charts
	d.Slides[1].Elements = append(d.Slides[1].Elements, Element{ID: "chart", Kind: "chart", ObjectID: "chart-1"})
	return d
}

func TestNativeProjectionDependencyCandidatesMatchExactOracle(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	before := nativeDependencyFixture()
	dependencies := captureNativeProjectionDependencies(before)
	for _, tc := range []struct {
		name string
		edit func(*Deck)
		want []int // Conservative candidate set, not necessarily changed artwork.
	}{
		{"local paint", func(d *Deck) { d.Slides[2].Elements[0].Query += "&fg=%23ff0055" }, []int{2}},
		{"local shape", func(d *Deck) {
			d.Slides[4].Elements = append(d.Slides[4].Elements, Element{ID: "shape", Kind: "shape", Query: "shape=circle&top=5&left=2"})
		}, []int{4}},
		{"private notes", func(d *Deck) { d.Slides[2].Notes = "PRIVATE NOTES" }, nil},
		{"private result", func(d *Deck) { d.Slides[2].EngagementResult = &EngagementResult{ActivityID: "private"} }, nil},
		{"shared source", func(d *Deck) { d.Assets["asset-a"] = workspaceAsset(color.RGBA{R: 255, A: 255}) }, []int{0, 1}},
		{"removed source", func(d *Deck) { delete(d.Assets, "asset-b") }, []int{2}},
		{"unused source", func(d *Deck) { d.Assets["unused"] = workspaceAsset(color.RGBA{G: 255, A: 255}) }, nil},
		{"inherited background", func(d *Deck) { d.Assets["backdrop"] = workspaceAsset(color.RGBA{B: 255, A: 255}) }, []int{0}},
		{"table", func(d *Deck) { d.Objects.Tables["shared-table"].Cells[0][0].Text = "Changed" }, []int{0, 2}},
		{"chart", func(d *Deck) { d.Objects.Charts["chart-1"].Series[0].Values[0]++ }, []int{1}},
		{"build", func(d *Deck) {
			d.Objects.Builds["slide-4"] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "fade", DurationMS: 200}}
		}, []int{4}},
		{"master layout", func(d *Deck) { d.Masters.Layouts[0].Slide.BG = "#ffaa22" }, []int{0, 3}},
		{"unused master", func(d *Deck) { d.Masters.Layouts[2].Slide.BG = "#ffaa22" }, nil},
		{"base", func(d *Deck) { d.Masters.Base.Slide.Elements[0].Text = "Changed base" }, []int{0, 1, 2, 3, 4}},
		{"theme", func(d *Deck) { d.Theme.Colors.Accent[5] = "#ff0055" }, []int{0, 1, 2, 3, 4}},
		{"page size", func(d *Deck) { d.PageSize = &DeckPageSize{Version: 1, Width: 1080, Height: 1920} }, []int{0, 1, 2, 3, 4}},
		{"appearance", func(d *Deck) { d.Appearance.Extra = map[string]json.RawMessage{"future": json.RawMessage(`true`)} }, []int{0, 1, 2, 3, 4}},
		{"QR", func(d *Deck) { d.HideActivityQR = true }, []int{0, 1, 2, 3, 4}},
		{"root extension", func(d *Deck) { d.Extra = extensionFixture("future", `true`) }, []int{0, 1, 2, 3, 4}},
		{"registry extension", func(d *Deck) { d.Objects.Extra = extensionFixture("future", `true`) }, []int{0, 1, 2, 3, 4}},
		{"master extension", func(d *Deck) { d.Masters.Extra = extensionFixture("future", `true`) }, []int{0, 1, 2, 3, 4}},
		{"duplicate layout IDs", func(d *Deck) { d.Masters.Layouts = append(d.Masters.Layouts, d.Masters.Layouts[0]) }, []int{0, 1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := cloneDeck(before)
			tc.edit(&after)
			owned := cloneDeck(after)
			got := nativeProjectionCandidateSlides(before, after, dependencies)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatal("wrong native candidate set", got, "want", tc.want)
			}
			oracle := []int{0, 1, 2, 3, 4}
			if !reflect.DeepEqual(nativeCandidateChanges(before, after, got), nativeCandidateChanges(before, after, oracle)) {
				t.Fatal("index missed an exact scene dependency")
			}
			if !reflect.DeepEqual(after, owned) {
				t.Fatal("index changed authored input")
			}
		})
	}
	// A compatible future adapter has conservative registry consumers even if
	// no currently implemented object can interpret that payload namespace.
	unknown := cloneDeck(before)
	unknown.Slides[4].Elements = append(unknown.Slides[4].Elements, Element{Kind: "future", ObjectID: "unknown"})
	index := captureNativeProjectionDependencies(unknown)
	after := cloneDeck(unknown)
	after.Assets["unused"] = workspaceAsset(color.RGBA{R: 255, A: 255})
	if got := nativeProjectionCandidateSlides(unknown, after, index); !reflect.DeepEqual(got, []int{4}) {
		t.Fatal("future adapter did not conservatively consider registry changes", got)
	}
}

func TestNativeProjectionDependencyRetentionBoundsAndOwnership(t *testing.T) {
	refs := map[int]workspaceReferences{}
	for page := 0; page < maxNativeDependencyPages+5; page++ {
		refs[page] = workspaceReferences{assets: map[string]bool{"asset": true}, payloads: map[workspacePayloadReference]bool{{"table", "shared"}: true}}
	}
	old := retainNativeProjectionDependencies(nil, refs)
	if len(old.refs) != maxNativeDependencyPages || old.bytes <= 0 || old.bytes > maxNativeDependencyBytes {
		t.Fatal("native dependency page/byte bounds failed")
	}
	delete(refs[0].assets, "asset")
	if !old.refs[0].assets["asset"] || !old.index.assets["asset"][0] {
		t.Fatal("native dependency snapshot borrowed caller containers")
	}
	next := retainNativeProjectionDependencies(old, map[int]workspaceReferences{maxNativeDependencyPages + 3: {assets: map[string]bool{"new-source": true}}})
	if _, exists := next.refs[maxNativeDependencyPages+3]; !exists || len(next.refs) != maxNativeDependencyPages || next.index.assets["new-source"] == nil || old.index.assets["new-source"] != nil {
		t.Fatal("changed-page priority or immutable previous index failed")
	}
	oversized := workspaceReferences{assets: map[string]bool{strings.Repeat("x", maxNativeDependencyBytes): true}}
	bounded := retainNativeProjectionDependencies(old, map[int]workspaceReferences{0: oversized})
	if _, exists := bounded.refs[0]; exists || len(bounded.refs) != maxNativeDependencyPages-1 || bounded.bytes > maxNativeDependencyBytes {
		t.Fatal("over-budget replacement retained an obsolete reference")
	}
	before := nativeDependencyFixture()
	after := cloneDeck(before)
	partial := retainNativeProjectionDependencies(nil, map[int]workspaceReferences{0: workspacePageReferences(before.Slides[0], before.ResolveSlide(0, false))})
	if got := nativeProjectionCandidateSlides(before, after, partial); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatal("unindexed pages did not fall back to full exact checks", got)
	}
	if got := nativeProjectionCandidateSlides(before, after, nil); len(got) != len(after.Slides) {
		t.Fatal("missing index must use full exact checks")
	}
	// A changed owner rewrites its asset consumers, without mutating old leases.
	old = captureNativeProjectionDependencies(before)
	after.Slides[0].Elements[1].AssetID = "asset-b"
	next = retainNativeProjectionDependencies(old, map[int]workspaceReferences{0: workspacePageReferences(after.Slides[0], after.ResolveSlide(0, false))})
	if old.index.assets["asset-b"][0] || !next.index.assets["asset-b"][0] || next.index.assets["asset-a"][0] {
		t.Fatal("retargeted source did not replace dependency consumers")
	}
}

func TestNativeProjectionDependencyPublishedLifecycle(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := nativeDependencyFixture()
	s := newNativeEditorSession("Dependencies.md", d)
	p := &presenterCompanion{}
	defer p.Close()
	path := filepath.Join(t.TempDir(), "Dependencies.md")
	if err := p.Refresh(path, d.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	p.bindNativeProjection(s, cloneDeckForScene(d), s.version, 245, 56)
	revision := s.version
	for _, tc := range []struct {
		name       string
		edit       func(*Deck)
		considered []int
	}{
		{"retarget image", func(d *Deck) { d.Slides[0].Elements[1].AssetID = "asset-b" }, []int{0}},
		{"old source now page one only", func(d *Deck) { d.Assets["asset-a"] = workspaceAsset(color.RGBA{G: 255, A: 255}) }, []int{1}},
		{"new source shared", func(d *Deck) { d.Assets["asset-b"] = workspaceAsset(color.RGBA{R: 255, A: 255}) }, []int{0, 2}},
		{"unused source", func(d *Deck) { d.Assets["unused"] = workspaceAsset(color.RGBA{B: 255, A: 255}) }, nil},
		{"private notes", func(d *Deck) { d.Slides[2].Notes = "Private notes" }, nil},
		{"inherited source removed", func(d *Deck) { delete(d.Assets, "backdrop") }, []int{0}},
		{"shared payload", func(d *Deck) { d.Objects.Tables["shared-table"].Cells[1][1].Text = "Accepted shared data" }, []int{0, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := cloneDeck(d)
			tc.edit(&d)
			var all []int
			for page := range d.Slides {
				all = append(all, page)
			}
			want := nativeCandidateChanges(before, d, all)
			revision++
			p.RefreshEditorDeckAsync(s, path, cloneDeckForScene(d), revision, 245, 56)
			p.awaitEditorProjection(t.Context(), s.notesSessionID, revision)
			_, cold, err := projectPresenterDocument(path, d.ResolvedSlides(), 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			p.mu.RLock()
			defer p.mu.RUnlock()
			if !reflect.DeepEqual(p.pages, cold) || !reflect.DeepEqual(p.nativeProjection.projected, want) || !reflect.DeepEqual(p.nativeProjection.considered, tc.considered) {
				t.Fatal("native indexed publication diverged", p.nativeProjection.projected, "want", want, "candidates", p.nativeProjection.considered, "want", tc.considered)
			}
		})
	}
	// Compatibility reset drops the dependency lease along with its old deck.
	p.mu.Lock()
	p.invalidateNativeProjectionLocked()
	retained := p.nativeProjection.dependencies != nil
	p.mu.Unlock()
	if retained {
		t.Fatal("invalidated native snapshot retained its source index")
	}
}

func TestNativeProjectionDependencyDeckFieldReview(t *testing.T) {
	// New document fields need an explicit candidate-filter decision. Scene
	// comparison presently excludes tab configuration and editor diagnostics;
	// authored tab membership itself remains part of the local Slide input.
	want := []string{"PageSize", "Theme", "Typefaces", "Objects", "Appearance", "Diagnostics", "HideActivityQR", "Tabs", "Slides", "Masters", "Assets", "Extra"}
	typeOf := reflect.TypeOf(Deck{})
	var actual []string
	for index := 0; index < typeOf.NumField(); index++ {
		actual = append(actual, typeOf.Field(index).Name)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("review native source candidates for document model changes", actual)
	}
}

func TestNativeProjectionPreparedBootstrapCatchUp(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := nativeDependencyFixture()
	s := newNativeEditorSession("Bootstrap.md", d)
	initial, version := cloneDeckForScene(s.deck), s.version
	dependencies := captureNativeProjectionDependencies(initial)
	p := &presenterCompanion{}
	defer p.Close()
	path := filepath.Join(t.TempDir(), "Bootstrap.md")
	if err := p.Refresh(path, initial.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	// The native editor can accept input while its first HTML is being built.
	// Bind to the actual published snapshot, then catch up, never label old
	// pages with the newer accepted document's source dependencies.
	for _, page := range []int{0, 2} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: page, SceneRevision: &revision, ObjectIDs: []string{fmt.Sprintf("text-%d", page)}, Property: "paint.color", PropertyValue: json.RawMessage(`"#ff0055"`)}); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.deckPath, s.companion = path, p
	p.bindPreparedNativeProjection(s, initial, version, 245, 56, dependencies)
	p.RefreshEditorDeckAsync(s, path, cloneDeckForScene(s.deck), s.version, 245, 56)
	s.mu.Unlock()
	p.awaitEditorProjection(t.Context(), s.notesSessionID, s.version)
	assertNativeProjectionMatchesCold(t, s, p, path)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !reflect.DeepEqual(p.nativeProjection.considered, []int{0, 2}) || !reflect.DeepEqual(p.nativeProjection.projected, []int{0, 2}) || p.nativeProjection.pending {
		t.Fatal("native bootstrap lost an accepted edit", p.nativeProjection.considered, p.nativeProjection.projected)
	}
}

func BenchmarkNativeProjectionDependencySelection(b *testing.B) {
	before := themedSharedTextFixture(20)
	for len(before.Slides) < 100 {
		before.Slides = append(before.Slides, cloneSlide(before.Slides[0]))
	}
	for index := range before.Slides {
		before.Slides[index].ID = fmt.Sprintf("dependency-slide-%d", index)
	}
	after := cloneDeck(before)
	after.Slides[50].Elements[0].Text = "Accepted local edit"
	dependencies := captureNativeProjectionDependencies(before)
	changedRef := workspacePageReferences(after.Slides[50], after.ResolveSlide(50, false))
	for _, indexed := range []bool{false, true} {
		b.Run(fmt.Sprintf("indexed=%t", indexed), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var candidates []int
				if indexed {
					candidates = nativeProjectionCandidateSlides(before, after, dependencies)
				} else {
					for page := range after.Slides {
						candidates = append(candidates, page)
					}
				}
				changed := nativeCandidateChanges(before, after, candidates)
				if !reflect.DeepEqual(changed, []int{50}) {
					b.Fatal("candidate selection changed accepted pages", changed)
				}
				if indexed {
					// Include index publication overhead, not just the fast lookup.
					next := retainNativeProjectionDependencies(dependencies, map[int]workspaceReferences{50: changedRef})
					if len(next.refs) != len(after.Slides) {
						b.Fatal("incomplete next native index")
					}
				}
			}
		})
	}
}
