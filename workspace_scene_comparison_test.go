package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func workspaceComparisonFixture(t testing.TB) (workspacePageEntry, []exportPage) {
	d := powerupDenseEditingDeck(t)
	before, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "fg", "#ff0055")
	after, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	pages := []exportPage{{Slide: 0, Scene: &before}}
	_, sizes, err := workspaceEntryEncodedSize(pages, Slide{}, workspacePageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	return workspacePageEntry{pages: pages, objectSizes: sizes}, []exportPage{{Slide: 0, Scene: &after}}
}

func TestWorkspaceStructuredComparisonReuseIsExactAndBounded(t *testing.T) {
	previous, pages := workspaceComparisonFixture(t)
	var comparisons workspaceSceneComparisons
	size, _, err := workspaceEntryEncodedSize(pages, Slide{}, previous, &comparisons)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparisons.entries) == 0 || comparisons.hits != 0 {
		t.Fatal("accounting did not capture structured comparisons")
	}
	wire := workspaceWirePages(pages, map[int]workspacePageEntry{0: previous}, true, &comparisons)
	plain := workspaceWirePages(pages, map[int]workspacePageEntry{0: previous}, true)
	if comparisons.hits < 2 || !reflect.DeepEqual(wire, plain) {
		t.Fatal("patch construction missed reuse or changed output", comparisons.hits)
	}
	encoded, err := json.Marshal(struct {
		Pages []exportPage
		Input Slide
	}{pages, Slide{}})
	if err != nil || len(encoded) != size {
		t.Fatal("comparison reuse changed exact size", size, len(encoded), err)
	}
	var bounded workspaceSceneComparisons
	for range 150 {
		a, b := sceneObject{Table: &sceneTable{}}, sceneObject{Table: &sceneTable{}}
		if !bounded.equal(&a, &b) {
			t.Fatal("equal structured objects changed")
		}
	}
	if len(bounded.entries) != 128 {
		t.Fatal("unbounded request memo", len(bounded.entries))
	}
	// A later request must not reuse an earlier result for modified snapshots.
	a, b := sceneObject{Table: &sceneTable{Rows: []float64{1}}}, sceneObject{Table: &sceneTable{Rows: []float64{1}}}
	var first workspaceSceneComparisons
	if !first.equal(&a, &b) {
		t.Fatal("first request mismatch")
	}
	b.Table.Rows[0] = 2
	var next workspaceSceneComparisons
	if next.equal(&a, &b) {
		t.Fatal("comparison escaped request lifetime")
	}
}

func BenchmarkWorkspaceStructuredComparisonReuse(b *testing.B) {
	previous, pages := workspaceComparisonFixture(b)
	for _, memo := range []bool{false, true} {
		name := "separate"
		if memo {
			name = "request-reuse"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var owned workspaceSceneComparisons
				var comparisons *workspaceSceneComparisons
				if memo {
					comparisons = &owned
				}
				if _, _, err := workspaceEntryEncodedSize(pages, Slide{}, previous, comparisons); err != nil {
					b.Fatal(err)
				}
				if scenePatchFor(previous.pages[0].Scene, pages[0].Scene, comparisons) == nil {
					b.Fatal("missing patch")
				}
			}
		})
	}
}
