package app

import (
	"math"
	"reflect"
	"testing"
)

func TestPageSizeSceneProjectsPhysicalBoundsWithoutStretchingFonts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := structuredFixture()
	d.Appearance = &DeckAppearance{Version: 3}
	d.Slides[0].Elements = append(d.Slides[0].Elements,
		Element{ID: "body", Kind: "text", Text: "Same physical font size", Query: "render=truetype&modern-font=sans&modern-size=48&modern-width=85&text-box=1&top=2&left=8&width=100&height=10"},
		Element{ID: "shape", Kind: "shape", Query: "shape=square&top=10&left=120&width=40&height=15"})
	base, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	before := cloneDeck(d)
	for _, size := range []DeckPageSize{{1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}, {1, 1920, 1080}} {
		d.PageSize = size.Clone()
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if scene.Width != float64(size.Width) || scene.Height != float64(size.Height) || len(scene.Objects) != len(base.Objects) {
			t.Fatal("wrong page dimensions")
		}
		for i, original := range base.Objects {
			object := scene.Objects[i]
			for name, pair := range map[string][2]float64{"x": {object.Bounds.X, original.Bounds.X * scene.Width / 1920}, "y": {object.Bounds.Y, original.Bounds.Y * scene.Height / 1080}, "width": {object.Bounds.Width, original.Bounds.Width * scene.Width / 1920}, "height": {object.Bounds.Height, original.Bounds.Height * scene.Height / 1080}} {
				if math.Abs(pair[0]-pair[1]) > 1e-6 {
					t.Fatalf("%s %s is not physically projected: %v", object.Kind, name, pair)
				}
			}
			if !reflect.DeepEqual(object.Text, original.Text) {
				t.Fatal("page aspect stretched font typography", object.ID)
			}
			if object.Table != nil && (math.Abs(object.Table.Columns[0]-original.Table.Columns[0]*scene.Width/1920) > 1e-6 || math.Abs(object.Table.Rows[0]-original.Table.Rows[0]*scene.Height/1080) > 1e-6) {
				t.Fatal("table geometry was not recomputed")
			}
		}
	}
	d.PageSize = nil
	if !reflect.DeepEqual(before, d) {
		t.Fatal("projection changed authored data")
	}
}

func TestPageSizeInvalidatesWorkspaceAndScopedSceneInputs(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := structuredFixture()
	d.Slides = append(d.Slides, Slide{ID: "second", Elements: []Element{{ID: "text", Kind: "text", Text: "Second"}}})
	d.EnsureDefaultMasters()
	var cache workspacePageCache
	if _, err := cache.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	previous := cloneDeck(d)
	d.PageSize = &DeckPageSize{1, 1080, 1920}
	before := cache.misses
	pages, err := cache.render(d, false, 0, 245, 56)
	if err != nil || cache.misses-before != 2 {
		t.Fatal("page change reused stale workspace", err, cache.misses-before)
	}
	for _, page := range pages {
		if page.Scene == nil || page.Scene.Width != 1080 || page.Scene.Height != 1920 {
			t.Fatal("stale workspace page dimensions")
		}
	}
	for _, master := range []bool{false, true} {
		if sceneProjectionInputsEqual(previous, d, sceneResponseKey{index: 0, master: master, cols: 245, rows: 56}) {
			t.Fatal("scene dependency ignored page dimensions")
		}
		scope, err := captureSlideResolutionScope(d, 0, master)
		if err != nil || !reflect.DeepEqual(scope.deck.PageSize, d.PageSize) {
			t.Fatal("resolution scope lost dimensions", err)
		}
		snapshot, _, err := captureMutationPreviewDeck(d, 0, master, nil)
		if err != nil || !reflect.DeepEqual(snapshot.PageSize, d.PageSize) {
			t.Fatal("mutation preview lost dimensions", err)
		}
		snapshot.PageSize.Width++
		if d.PageSize.Width != 1080 {
			t.Fatal("preview aliases source dimensions")
		}
	}
}

func TestPageSizeRefreshesEntireNativePresentation(t *testing.T) {
	before := Deck{Slides: []Slide{{ID: "first"}, {ID: "second"}}}
	after := cloneDeck(before)
	after.PageSize = &DeckPageSize{1, 1080, 1920}
	for _, action := range []string{"set-page-size", "undo", "redo"} {
		if scope, _ := nativeEditorRefreshPlan(action, before, after, 0, 0); scope != "deck" {
			t.Fatal("page size failed to refresh every presentation page", action, scope)
		}
	}
}
