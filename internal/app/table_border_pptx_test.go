package app

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func testEdge(color string, width float64) TableBorderStyle {
	return TableBorderStyle{Color: color, Width: &width}
}

func TestTableFourEdgesProjectionCloneAndPortablePPTX(t *testing.T) {
	table, _ := newTableData(2, 2)
	table.Style.Cell.BorderColor = "#8899aa"
	table.Style.Cell.Borders = map[string]TableBorderStyle{"top": testEdge("#abcdef", 3)}
	table.Cells[0][0].Text = "Independent edges"
	table.Cells[0][0].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{
		"left": testEdge("none", 0), "right": testEdge("#ff0055", 4), "top": testEdge("#00ffaa", 2), "bottom": testEdge("#4455ff", 1),
	}}
	original := cloneTable(table)
	copy := cloneTable(table)
	*copy.Cells[0][0].Style.Borders["right"].Width = 7
	copy.Style.Cell.Borders["top"] = testEdge("#000000", 8)
	if !reflect.DeepEqual(table, original) {
		t.Fatal("per-edge styles alias across clones")
	}
	for _, mutate := range []func(*TableData){
		func(x *TableData) { x.Cells[0][0].Style.Borders["diagonal"] = testEdge("#ff0000", 1) },
		func(x *TableData) { x.Cells[0][0].Style.Borders["right"] = testEdge("#ff0000", math.NaN()) },
		func(x *TableData) { x.Cells[0][0].Style.Borders["right"] = testEdge("#ff0000", -1) },
		func(x *TableData) { x.Cells[0][0].Style.Borders["right"] = testEdge("red", 1) },
	} {
		x := cloneTable(table)
		mutate(&x)
		if x.Validate() == nil {
			t.Fatal("invalid per-side border accepted")
		}
	}
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	edges := tableBorderEdges(t, scene)
	for key, color := range map[string]string{"vertical:1:0": "#ff0055", "horizontal:0:0": "#00ffaa", "horizontal:1:0": "#4455ff"} {
		if edges[key].Color != color {
			t.Fatal("wrong per-side precedence", key, edges[key])
		}
	}
	if _, ok := edges["vertical:0:0"]; ok {
		t.Fatal("per-side Off lost")
	}
	deck := Deck{Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"edges": table}}, Slides: []Slide{{Elements: []Element{{Kind: "table", ID: "table", ObjectID: "edges", Query: "left=12&top=8&width=150&height=28"}}}}}
	md, err := serializeDeck("borders.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("borders.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Objects.Tables["edges"], original) {
		t.Fatal("Markdown lost edge style")
	}
	encoded, report, err := exportPPTXDocument(deck)
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal("distinct edges exported with warnings", err, report)
	}
	read, report, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal("native edge read", err, report)
	}
	var native *pptx.Table
	for _, object := range read.Slides[0].Objects {
		if object.Kind == "table" {
			if native != nil {
				t.Fatal("table duplicated")
			}
			native = object.Table
		}
	}
	if native == nil {
		t.Fatal("table flattened", read.Slides[0].Objects)
	}
	cell := native.Cells[0][0]
	if cell.Borders["left"].Color != "none" || cell.Borders["right"].Color != "#FF0055" || cell.Borders["right"].Width != 4 || cell.Borders["top"].Width != 2 {
		t.Fatal("native side paint lost", cell.Borders)
	}
	imported, report, err := importPPTXDocument(encoded, "edges.pptx")
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	var got TableData
	for _, element := range imported.Slides[0].Elements {
		if element.Kind == "table" {
			got = imported.Objects.Tables[element.ObjectID]
		}
	}
	projection, err := imported.projectTable(got, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	actual := tableBorderEdges(t, projection)
	if len(actual) != len(edges) {
		t.Fatal("border count changed", actual, edges)
	}
	for key, want := range edges {
		edge := actual[key]
		if !strings.EqualFold(want.Color, edge.Color) || math.Abs(want.Width-edge.Width) > .0002 {
			t.Fatal("PPTX edge drift", key, want, edge)
		}
	}
	// The sparse map is part of the known schema, never an opaque extension.
	data, _ := json.Marshal(got.Cells[0][0].Style)
	var decoded TableCellStyle
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Borders) != 4 || decoded.Extra != nil {
		t.Fatal("edge schema not understood", err, decoded)
	}
}

func TestTableMergedPerimeterExportPreservesSegments(t *testing.T) {
	table, _ := newTableData(2, 2)
	table.Cells[0][0].ColSpan = 2
	table.Cells[0][1] = TableCell{Hidden: true}
	table.Cells[1][0].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"top": testEdge("#ff0000", 2)}}
	table.Cells[1][1].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"top": testEdge("#00ff00", 3)}}
	scene, err := (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	object, warnings := sceneObjectToPPTX(sceneObject{Kind: "table", ID: "table", Table: scene, Bounds: sceneRect{Width: 600, Height: 300}})
	if object == nil || object.Kind != "table" || len(warnings) != 1 || warnings[0].Code != "table-border-segments-reader-dependent" {
		t.Fatal("missing truthful merged-edge diagnostic", object, warnings)
	}
	if border := object.Table.Cells[0][0].Borders["bottom"]; border.Color != "#ff0000" || border.Width != 2 {
		t.Fatal("first segment lost", border)
	}
	if border := object.Table.Cells[0][1].Borders["bottom"]; border.Color != "#00ff00" || border.Width != 3 {
		t.Fatal("continuation segment lost", border)
	}
	// Whole side paint is a uniform, exactly representable merged edge.
	table.Cells[0][0].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"bottom": testEdge("none", 0)}}
	scene, _ = (Deck{}).projectTable(table, sceneRect{Width: 600, Height: 300}, "#ffffff")
	_, warnings = sceneObjectToPPTX(sceneObject{Kind: "table", ID: "table", Table: scene, Bounds: sceneRect{Width: 600, Height: 300}})
	if len(warnings) != 0 {
		t.Fatal("uniform merged edge falsely diagnosed", warnings)
	}
}

func TestTableMergedPerimeterExportAllSidesRoundTrip(t *testing.T) {
	for _, name := range []string{"colours", "off", "theme", "sparse"} {
		t.Run(name, func(t *testing.T) {
			d := fragmentDeck(t)
			d.Theme = builtinDeckTheme("clean-light")
			data := d.Objects.Tables["data"]
			switch name {
			case "off":
				data.Cells[0][0].BorderFragments["top"][1] = testEdge("none", 0)
				data.Cells[0][0].BorderFragments["right"][1] = testEdge("none", 0)
			case "theme":
				data.Cells[0][0].BorderFragments["bottom"][1].Color = "theme:accent-6"
			case "sparse":
				data.Cells[0][0].BorderFragments["left"][1].Width = nil
				data.Cells[0][0].BorderFragments["right"][1].Color = ""
			}
			d.Objects.Tables["data"] = data
			before := cloneDeck(d)
			scene, err := d.projectTable(data, sceneRect{Width: 600, Height: 300}, "#223344")
			if err != nil {
				t.Fatal(err)
			}
			encoded, report, err := exportPPTXDocument(d)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Warnings) != 1 || report.Warnings[0].Code != "table-border-segments-reader-dependent" {
				t.Fatal("wrong export boundary diagnostic", report)
			}
			read, report, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			var native *pptx.Table
			if len(read.Slides[0].Objects) != 1 || !strings.EqualFold(read.Slides[0].Background, d.Theme.Colors.Background) {
				t.Fatal("expected native background property and one editable table", read.Slides[0])
			}
			for _, o := range read.Slides[0].Objects {
				if o.Kind == "table" {
					if native != nil {
						t.Fatal("extra native table")
					}
					native = o.Table
				} else {
					t.Fatal("export added overlay objects", o)
				}
			}
			if native == nil || native.Cells[0][0].RowSpan != 2 || native.Cells[0][0].ColSpan != 2 {
				t.Fatal("lost editable spans")
			}
			if name == "off" && (native.Cells[0][1].Borders["top"].Color != "none" || native.Cells[1][1].Borders["right"].Color != "none") {
				t.Fatal("explicit Off segment lost")
			}
			imported, report, err := importPPTXDocument(encoded, "Fragments.pptx")
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			var recovered TableData
			for _, e := range imported.Slides[0].Elements {
				if e.Kind == "table" {
					recovered = imported.Objects.Tables[e.ObjectID]
				}
			}
			after, err := imported.projectTable(recovered, sceneRect{Width: 600, Height: 300}, "#223344")
			if err != nil {
				t.Fatal(err)
			}
			want, got := tableBorderEdges(t, scene), tableBorderEdges(t, after)
			if len(want) != len(got) {
				t.Fatal("segment count drift", want, got)
			}
			for key, e := range want {
				other := got[key]
				if !strings.EqualFold(e.Color, other.Color) || math.Abs(e.Width-other.Width) > .0002 {
					t.Fatal("segment paint drift", key, e, other)
				}
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("export mutated deck")
			}
			if root := os.Getenv("KEYNOPE_FRAGMENT_EVIDENCE"); root != "" {
				if err := os.MkdirAll(root, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "perimeter-"+name+".pptx"), encoded, 0644); err != nil {
					t.Fatal(err)
				}
				if err := saveDeck(filepath.Join(root, "perimeter-"+name+"-imported.md"), imported); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTablePerimeterPPTX(b *testing.B) {
	data, _ := newTableData(100, 100)
	for r := range data.Cells {
		for c := range data.Cells[r] {
			data.Cells[r][c].Text = "Cell"
		}
	}
	scene, err := (Deck{}).projectTable(data, sceneRect{Width: 1920, Height: 1080}, "#ffffff")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table := sceneTableToPPTX(scene)
		if len(table.Cells) != 100 {
			b.Fatal("bad conversion")
		}
	}
}

func TestTableBorderExtensionsRemainOwned(t *testing.T) {
	var style TableCellStyle
	if err := json.Unmarshal([]byte(`{"borders":{"right":{"color":"#ff0000","width":2,"future":{"values":[1,2]}}}}`), &style); err != nil {
		t.Fatal(err)
	}
	copy := cloneCellStyle(style)
	border := copy.Borders["right"]
	(*border.Extra)["future"] = json.RawMessage(`"changed"`)
	if string((*style.Borders["right"].Extra)["future"]) != `{"values":[1,2]}` {
		t.Fatal("border extension clone shares storage")
	}
	encoded, err := json.Marshal(style)
	if err != nil || !bytes.Contains(encoded, []byte(`"future":{"values":[1,2]}`)) {
		t.Fatal("unknown compatible edge metadata lost", err, string(encoded))
	}
}
