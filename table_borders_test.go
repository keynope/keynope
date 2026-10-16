package main

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func tableBorderEdges(t *testing.T, scene *sceneTable) map[string]sceneTableBorder {
	t.Helper()
	edges := map[string]sceneTableBorder{}
	for _, border := range scene.Borders {
		if border.Width <= 0 || border.Color == "none" || border.From >= border.To {
			t.Fatal("invalid visible border", border)
		}
		for i := border.From; i < border.To; i++ {
			key := fmt.Sprintf("%s:%d:%d", border.Axis, border.Track, i)
			if _, exists := edges[key]; exists {
				t.Fatal("shared seam painted twice", key)
			}
			edges[key] = border
		}
	}
	return edges
}

func TestTableSharedBordersCoalesceAndSkipMergedInterior(t *testing.T) {
	table, _ := newTableData(2, 2)
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if scene.Borders == nil || len(scene.Borders) != 6 || len(tableBorderEdges(t, scene)) != 12 {
		t.Fatal("uniform grid does not resolve seams once", scene.Borders)
	}
	table.Cells[0][0].ColSpan = 2
	table.Cells[0][1].Hidden = true
	scene, err = (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	edges := tableBorderEdges(t, scene)
	if len(edges) != 11 {
		t.Fatal("merged grid has wrong edge count", edges)
	}
	if _, exists := edges["vertical:1:0"]; exists {
		t.Fatal("merged interior has a border")
	}
	if _, exists := edges["vertical:1:1"]; !exists {
		t.Fatal("neighbour boundary was removed")
	}
}

func TestTableSharedBorderPrecedenceOffAndStableTies(t *testing.T) {
	table, _ := newTableData(2, 2)
	base := 5.0
	thin := 2.0
	wide := 3.0
	table.Style.Cell.BorderColor = "#0000ff"
	table.Style.Cell.BorderWidth = &base
	table.Cells[0][0].Style = &TableCellStyle{BorderColor: "#ff0000", BorderWidth: &thin}
	table.Cells[0][1].Style = &TableCellStyle{BorderColor: "#00ff00", BorderWidth: &wide}
	table.Cells[1][1].Style = &TableCellStyle{BorderColor: "none"}
	original := cloneTable(table)
	scene, err := (Deck{Theme: builtinDeckTheme("clean-light")}).projectTable(table, sceneRect{Width: 600, Height: 300}, "")
	if err != nil {
		t.Fatal(err)
	}
	edges := tableBorderEdges(t, scene)
	if e := edges["horizontal:1:0"]; e.Color != "#ff0000" || e.Width != 2 {
		t.Fatal("cell border did not beat wider table default", e)
	}
	if e := edges["vertical:1:0"]; e.Color != "#00ff00" || e.Width != 3 {
		t.Fatal("equal-rank wider border did not win", e)
	}
	if _, ok := edges["horizontal:1:1"]; ok {
		t.Fatal("explicit Off did not suppress equal-priority neighbour")
	}
	if _, ok := edges["vertical:1:1"]; ok {
		t.Fatal("explicit Off did not suppress table border")
	}
	if !reflect.DeepEqual(table, original) {
		t.Fatal("resolution mutated authored cells")
	}
	table.Cells[0][1].Style.BorderWidth = &thin
	scene, _ = (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if e := tableBorderEdges(t, scene)["vertical:1:0"]; e.Color != "#ff0000" {
		t.Fatal("top/left tie break is not stable", e)
	}
	// Logical edges don't bake frame size, so all surfaces and local gestures
	// can reframe the same seams without recomputing cell formatting.
	reframed, _ := (Deck{}).projectTable(table, sceneRect{Width: 1700, Height: 450}, "#ffffff")
	if !reflect.DeepEqual(scene.Borders, reframed.Borders) {
		t.Fatal("track resizing changed border decisions")
	}
}

func TestTableNoBordersUsesPresentEmptyList(t *testing.T) {
	table, _ := newTableData(1, 1)
	table.Style.Cell.BorderColor = "none"
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 400, Height: 100}, "#ffffff")
	if err != nil || scene.Borders == nil || len(scene.Borders) != 0 {
		t.Fatal("Off is ambiguous with legacy cell borders", scene, err)
	}
}

func TestTableSharedBordersDenseMergedGridAndOrderIndependence(t *testing.T) {
	table, _ := newTableData(20, 20)
	for r := 0; r < 20; r += 4 {
		for c := 0; c < 20; c += 4 {
			table.Cells[r][c].RowSpan, table.Cells[r][c].ColSpan = 2, 2
			for _, offset := range [][2]int{{0, 1}, {1, 0}, {1, 1}} {
				table.Cells[r+offset[0]][c+offset[1]].Hidden = true
			}
		}
	}
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 1600, Height: 900}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	edges := tableBorderEdges(t, scene)
	// A 20x20 grid has 840 elementary boundaries; each of 25 disjoint
	// 2x2 merged owners removes its four interior elementary boundaries.
	if len(edges) != 840-25*4 {
		t.Fatal("dense merge removed/duplicated a perimeter segment", len(edges))
	}
	original := append([]sceneTableBorder(nil), scene.Borders...)
	random := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		random.Shuffle(len(scene.Cells), func(i, j int) { scene.Cells[i], scene.Cells[j] = scene.Cells[j], scene.Cells[i] })
		if !reflect.DeepEqual(resolveTableBorders(table, scene), original) {
			t.Fatal("paint order affected shared seams")
		}
	}
}

func BenchmarkTableBorderResolution20x20(b *testing.B) {
	table, _ := newTableData(20, 20)
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 1600, Height: 900}, "#ffffff")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(resolveTableBorders(table, scene)) != 42 {
			b.Fatal("uniform edges were not coalesced")
		}
	}
}
