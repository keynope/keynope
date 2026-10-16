package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func denseNativePatchFixture(t *testing.T) (*nativeEditorSession, *presenterCompanion) {
	t.Helper()
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("Dense.md", powerupDenseEditingDeck(t))
	p := &presenterCompanion{}
	t.Cleanup(p.Close)
	s.deckPath = filepath.Join(t.TempDir(), "Dense.md")
	if err := p.Refresh(s.deckPath, s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	s.companion = p
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	return s, p
}

func TestNativePresenterObjectPatchesMixedPrimitives(t *testing.T) {
	s, p := denseNativePatchFixture(t)
	imageID, connectorID := "", ""
	for _, element := range s.deck.Slides[0].Elements {
		if element.Kind == "image" && imageID == "" {
			imageID = element.ID
		}
		if element.Kind == "connector" {
			connectorID = element.ID
		}
	}
	if imageID == "" || connectorID == "" {
		t.Fatal("dense fixture lost media or connectors")
	}
	table := cloneTable(s.deck.Objects.Tables["table-dense"])
	table.Cells[0][0].Text = "Native cell update"
	chart := cloneChart(s.deck.Objects.Charts["dense-chart"])
	chart.Series[0].Values[0] = 12
	steps := []struct {
		name, object string
		action       nativeEditorAction
	}{
		{"table", "dense-table", nativeEditorAction{Action: "set-table", ObjectID: "dense-table", TableData: &table}},
		{"chart", "dense-chart", nativeEditorAction{Action: "set-chart-data", ObjectID: "dense-chart", ChartData: &chart}},
		{"image crop", imageID, nativeEditorAction{Action: "set-scene-crop", ObjectID: imageID, Crop: &sceneCrop{Left: .1, Right: .1, Top: .1, Bottom: .1}}},
		{"line paint", connectorID, nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{connectorID}, Property: "paint.color", PropertyValue: json.RawMessage(`"#ff0055"`)}},
		{"timeline", "", nativeEditorAction{Action: "set-slide-build", BuildData: &SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"dense-text-0"}, Effect: "fade", DurationMS: 200, Advance: "click"}}}}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			base, previous := p.state.DeckVersion, p.slidePagesLocked()
			before := jsonForTest(t, previous)
			revision := s.version
			action := step.action
			action.SceneRevision = &revision
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
			cancel()
			update := p.pageUpdateSince(base, true)
			if len(update.PatchedPages) != 1 || len(update.Pages) != 0 {
				t.Fatal("supported native edit did not produce a scene patch")
			}
			patch := update.PatchedPages[0].ScenePatch
			if step.object != "" && (len(patch.Changed) != 1 || patch.Changed[0].ID != step.object) {
				t.Fatal("native edit invalidated unrelated object owners", patch.Order)
			}
			if step.object == "" && (len(patch.Changed) != 0 || patch.Metadata == nil || patch.Metadata.Build == nil) {
				t.Fatal("timeline change rebuilt objects or lost build metadata")
			}
			got := reconstructNativePatch(t, previous, update)
			cold := projectExportDeck(s.deckPath, s.deck.ResolvedSlides(), 245, 56, true).Pages
			if string(jsonForTest(t, got)) != string(jsonForTest(t, cold)) || string(before) != string(jsonForTest(t, previous)) {
				t.Fatal("native patch changed the previous snapshot or differs from cold output")
			}
		})
	}
}

func TestNativePresenterSceneSizingBoundsAndEntryOwnership(t *testing.T) {
	page := exportPage{Scene: &slideScene{Version: 1, SlideID: "size", Width: 1920, Height: 1080, Objects: []sceneObject{{ID: "image", Kind: "image", Media: &sceneMedia{Source: strings.Repeat("x", maxPresenterSceneBaselineBytes+1)}}}}}
	if entry := presenterPageSizeEntry([]exportPage{page}, workspacePageEntry{}, nil); entry.bytes != 0 || entry.pages != nil {
		t.Fatal("oversized source entered native baseline cache")
	}
	page.Scene.BackgroundMedia = &sceneBackgroundMedia{sceneMedia: *page.Scene.Objects[0].Media}
	page.Scene.Objects = nil
	if entry := presenterPageSizeEntry([]exportPage{page}, workspacePageEntry{}, nil); entry.bytes != 0 {
		t.Fatal("oversized background entered native baseline cache")
	}
	entries := map[int]workspacePageEntry{}
	for i := 0; i < 200; i++ {
		entries[i] = workspacePageEntry{bytes: 100}
	}
	retained := retainPresenterPageSizeEntries(entries, map[int]workspacePageEntry{199: {bytes: 100}}, []int{199})
	if len(retained) != maxPresenterSceneBaselinePages || retained[199].bytes != 100 {
		t.Fatal("native sizing cache is unbounded or evicted the current edit")
	}
	retained = retainPresenterPageSizeEntries(entries, nil, []int{199})
	if retained[199].bytes != 0 {
		t.Fatal("unsupported new page retained its stale sizing baseline")
	}
	p := &presenterCompanion{}
	baseline := map[int]workspacePageEntry{0: {bytes: 100}}
	p.recordPageChangeLocked(false, []int{0}, baseline)
	baseline[0] = workspacePageEntry{bytes: 200}
	if p.pageChanges[0].before[0].bytes != 100 {
		t.Fatal("publication journal borrowed a mutable baseline container")
	}
	p.invalidateNativeProjectionLocked()
	if p.pageChanges[0].before != nil || p.pageChanges[0].bytes != 0 {
		t.Fatal("compatibility invalidation retained obsolete baseline data")
	}
}

// Independent wire reconstruction oracle. The production client uses the
// validated shared JavaScript decoder, not this test helper.
func reconstructNativePatch(t *testing.T, previous []exportPage, update presenterPageUpdate) []exportPage {
	t.Helper()
	if !update.Delta {
		return update.Pages
	}
	pages := append([]exportPage(nil), update.Pages...)
	for _, wire := range update.PatchedPages {
		var old *slideScene
		for _, page := range previous {
			if page.Slide == wire.Slide && page.Page == wire.Page {
				old = page.Scene
			}
		}
		patch := wire.ScenePatch
		if old == nil || patch == nil || old.SlideID != patch.SlideID {
			t.Fatal("invalid native test patch baseline")
		}
		objects := map[string]sceneObject{}
		for _, object := range old.Objects {
			objects[object.ID] = object
		}
		for _, id := range patch.Removed {
			delete(objects, id)
		}
		for _, object := range patch.Changed {
			objects[object.ID] = object
		}
		scene := *old
		if patch.Metadata != nil {
			scene = *patch.Metadata
		}
		scene.Objects = make([]sceneObject, 0, len(patch.Order))
		for _, id := range patch.Order {
			object, exists := objects[id]
			if !exists {
				t.Fatal("incomplete native scene patch", id)
			}
			scene.Objects = append(scene.Objects, object)
		}
		page := wire.exportPage
		page.Scene = &scene
		pages = append(pages, page)
	}
	for _, page := range previous {
		replaced := false
		for _, slide := range update.ReplaceSlides {
			replaced = replaced || slide == page.Slide
		}
		if !replaced {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Slide < pages[j].Slide || pages[i].Slide == pages[j].Slide && pages[i].Page < pages[j].Page
	})
	return pages
}

func TestNativePresenterObjectPatchesCatchUpAndHistory(t *testing.T) {
	s, p := denseNativePatchFixture(t)
	base, original := p.state.DeckVersion, p.slidePagesLocked()
	before := jsonForTest(t, original)
	nativePatchColour(t, s, p, "#ff0055")
	firstVersion := p.state.DeckVersion
	nativePatchColour(t, s, p, "#55aaff")
	for _, since := range []int64{base, firstVersion} {
		old := original
		if since == firstVersion {
			// Rebuild the first publication from its independently authored colour.
			copy := cloneDeckForScene(s.deck)
			for i, element := range copy.Slides[0].Elements {
				if element.ID == "dense-text-0" {
					copy.Slides[0].Elements[i].Query = setQueryValue(element.Query, "fg", "#ff0055")
				}
			}
			old = exportSlidePages(copy.slideRenderPreview(0, 245, 56), 0, 1, 245, 56)
		}
		update := p.pageUpdateSince(since, true)
		if len(update.PatchedPages) != 1 || len(update.Pages) != 0 {
			t.Fatal("missed-version scene baseline not retained")
		}
		if !reflect.DeepEqual(reconstructNativePatch(t, old, update), p.slidePagesLocked()) {
			t.Fatal("missed-version scene patch differs from published complete pages")
		}
	}
	if string(before) != string(jsonForTest(t, original)) {
		t.Fatal("subsequent native publications mutated earlier pages")
	}
	for _, action := range []string{"undo", "redo"} {
		previous := p.slidePagesLocked()
		since := p.state.DeckVersion
		if err := s.apply(nativeEditorAction{Action: action}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
		cancel()
		update := p.pageUpdateSince(since, true)
		if len(update.PatchedPages) != 1 || !reflect.DeepEqual(reconstructNativePatch(t, previous, update), p.slidePagesLocked()) {
			t.Fatal("native history patch differs from complete pages", action)
		}
	}
	plain := p.pageUpdateSince(base)
	if len(plain.PatchedPages) != 0 || len(plain.Pages) != 1 {
		t.Fatal("client without patch negotiation lost compatibility")
	}
}

func TestNativePresenterScenePatchMixedLineageAndBudget(t *testing.T) {
	page := func(slide int, colour string) exportPage {
		return exportPage{Slide: slide, Scene: &slideScene{Version: 1, SlideID: fmt.Sprintf("s-%d", slide), Width: 1920, Height: 1080, Objects: []sceneObject{{ID: "keep", Kind: "shape"}, {ID: "edit", Kind: "shape", Paint: scenePaint{Color: colour}}}}}
	}
	original := []exportPage{page(0, "old"), page(1, "old"), page(2, "old")}
	p := &presenterCompanion{pages: map[int][]exportPage{0: {original[0]}, 1: {original[1]}, 2: {original[2]}}}
	for _, index := range []int{0, 1, 0} {
		old := p.pages[index]
		next := map[int][]exportPage{index: {page(index, fmt.Sprint(p.state.DeckVersion+1))}}
		before, _ := preparePresenterSceneBaselines(map[int]workspacePageEntry{index: {pages: old}}, next)
		p.pages[index] = next[index]
		p.state.DeckVersion++
		p.recordPageChangeLocked(false, []int{index}, before)
	}
	patch := p.pageUpdateSince(0, true)
	if len(patch.PatchedPages) != 2 || !reflect.DeepEqual(patch.ReplaceSlides, []int{0, 1}) || !reflect.DeepEqual(reconstructNativePatch(t, original, patch), p.slidePagesLocked()) {
		t.Fatal("coherent multi-slide object catch-up differs")
	}
	// Losing the first baseline must not accidentally patch from a later one.
	p.pageChanges[0].before = nil
	patch = p.pageUpdateSince(0, true)
	if len(patch.PatchedPages) != 1 || patch.PatchedPages[0].Slide != 1 || len(patch.Pages) != 1 || patch.Pages[0].Slide != 0 || !reflect.DeepEqual(reconstructNativePatch(t, original, patch), p.slidePagesLocked()) {
		t.Fatal("missing earliest baseline used an incorrect later scene")
	}
	// Journal cost is conservatively bounded, while ordinary page lineage stays.
	for i := 0; i < 70; i++ {
		p.state.DeckVersion++
		p.recordPageChangeLocked(false, []int{0}, map[int]workspacePageEntry{0: {pages: p.pages[0], bytes: maxPresenterSceneBaselineBytes / 2}})
	}
	total, retained := 0, 0
	for _, entry := range p.pageChanges {
		total += entry.bytes
		if entry.before != nil {
			retained++
		}
	}
	if total > maxPresenterSceneBaselineBytes || retained != 2 || len(p.pageChanges) != 64 || p.pageUpdateSince(0, true).Delta {
		t.Fatal("native baseline journal is unbounded or lost full recovery", total, retained)
	}
	p.state.DeckVersion++
	p.recordPageChangeLocked(true, nil)
	for _, entry := range p.pageChanges {
		if entry.before != nil || entry.bytes != 0 {
			t.Fatal("structural refresh retained obsolete scene baselines")
		}
	}
}

func nativePatchColour(t *testing.T, s *nativeEditorSession, p *presenterCompanion, colour string) {
	t.Helper()
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"dense-text-0"}, Property: "paint.color", PropertyValue: json.RawMessage(fmt.Sprintf("%q", colour))}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
	if ctx.Err() != nil {
		t.Fatal("native page publication did not settle", ctx.Err())
	}
}

func TestNativePresenterObjectPatchHTTPContract(t *testing.T) {
	s, p := denseNativePatchFixture(t)
	base := p.state.DeckVersion
	nativePatchColour(t, s, p, "#ff0055")
	w := httptest.NewRecorder()
	p.handlePageUpdate(w, httptest.NewRequest("GET", fmt.Sprintf("/slides?since=%d&scenePatches=1", base), nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var reply struct {
		Version       int64               `json:"version"`
		Base          int64               `json:"baseVersion"`
		Delta         bool                `json:"delta"`
		Pages         []exportPage        `json:"pages"`
		PatchedPages  []workspaceWirePage `json:"patchedPages"`
		ReplaceSlides []int               `json:"replaceSlides"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Delta || reply.Base != base || reply.Version <= base || len(reply.Pages) != 0 || len(reply.PatchedPages) != 1 {
		t.Fatalf("native paint edit sent a full scene rather than one object patch: full=%d patched=%d bytes=%d", len(reply.Pages), len(reply.PatchedPages), w.Body.Len())
	}
	patch := reply.PatchedPages[0].ScenePatch
	if patch == nil || len(patch.Changed) != 1 || patch.Changed[0].ID != "dense-text-0" || len(patch.Removed) != 0 {
		t.Fatal("native paint patch invalidated unrelated objects")
	}
	if w.Body.Len() > 10000 {
		t.Fatal("native paint patch still transfers dense table/media", w.Body.Len())
	}
	full := httptest.NewRecorder()
	p.handlePageUpdate(full, httptest.NewRequest("GET", fmt.Sprintf("/slides?since=%d", base), nil))
	if full.Code != 200 || w.Body.Len()*4 >= full.Body.Len() {
		t.Fatal("negotiated patch did not reduce the complete-page transfer", w.Body.Len(), full.Body.Len())
	}
	t.Logf("native dense paint response: %d bytes patched, %d bytes full", w.Body.Len(), full.Body.Len())
}
