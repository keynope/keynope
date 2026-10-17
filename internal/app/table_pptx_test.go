package app

import (
	"bytes"
	"context"
	"math"
	"net/url"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestTablePPTXEditableRoundTrip(t *testing.T) {
	table, _ := newTableData(3, 3)
	table.Rows[1].Height = 2
	table.Columns[1].Width = 2
	table.Cells[0][0].Text = "Header"
	table.Cells[1][0] = TableCell{Text: "Bold red\nNext", Runs: []ObjectTextRun{{Text: "Bold ", Bold: true}, {Text: "red\nNext", Color: "#ff0055", Italic: true, Underline: true}}, RowSpan: 2, ColSpan: 2}
	for _, rc := range [][2]int{{1, 1}, {2, 0}, {2, 1}} {
		table.Cells[rc[0]][rc[1]] = TableCell{Hidden: true}
	}
	size, padding, border := 46.0, 12.0, 2.0
	table.Cells[1][0].Style = &TableCellStyle{FontID: "c64", FontSize: &size, Color: "#55aaff", Fill: "#112233", BorderColor: "#556677", BorderWidth: &border, Padding: &padding, Align: "right", Vertical: "bottom"}
	spacing := 1.6
	table.Cells[1][0].Style.LineHeight = &spacing
	table.Cells[2][2].Text = "Off"
	table.Cells[2][2].Style = &TableCellStyle{Fill: "none", BorderColor: "none", FontID: "sans"}
	d := Deck{Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table-data": table}}, Slides: []Slide{{Elements: []Element{{ID: "table", ObjectID: "table-data", Kind: "table", Query: "left=20&top=8&width=160&height=30&object-rotation=12"}}}}}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Object == "table" && w.Code != "table-border-segments-reader-dependent" {
			t.Fatalf("table export warning: %#v", w)
		}
	}
	// The merged right edge meets one inherited border and one explicit Off
	// cell. Both physical segments are retained in standard continuation-cell
	// borders; external-reader visual acceptance remains explicitly diagnosed.
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "table-border-segments-reader-dependent" {
		t.Fatal("missing merged border reader diagnostic", report)
	}
	p, readReport, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(readReport.Warnings) > 0 {
		t.Fatalf("read: %v %#v", err, readReport)
	}
	var raw *pptx.Table
	for _, o := range p.Slides[0].Objects {
		if o.Kind == "table" {
			raw = o.Table
		}
	}
	if raw == nil || raw.Cells[1][0].FontSize != 23 || !strings.HasPrefix(raw.Cells[1][0].FontFace, "Keynope C64") {
		t.Fatalf("native cells/font missing: %#v", raw)
	}
	imported, report, err := importPPTXDocument(data, "tables.pptx")
	if err != nil {
		t.Fatal(err)
	}
	var element Element
	for _, e := range imported.Slides[0].Elements {
		if e.Kind == "table" {
			element = e
		}
	}
	if element.ObjectID == "" || imported.Objects == nil {
		t.Fatal("semantic table lost")
	}
	got := imported.Objects.Tables[element.ObjectID]
	if got.Cells[1][0].Style.LineHeight == nil || *got.Cells[1][0].Style.LineHeight != spacing {
		t.Fatal("editable table line spacing lost on import")
	}
	if err := validateDeckObjectReferences(imported); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Cells[1][0].Text != table.Cells[1][0].Text || !got.Cells[2][1].Hidden || got.Cells[1][0].ColSpan != 2 || got.Cells[1][0].RowSpan != 2 {
		t.Fatal("cell text/spans changed", got.Cells)
	}
	s := got.Cells[1][0].Style
	if *s.FontSize != 46 || s.FontID != "c64" || s.Color != "#55AAFF" || s.Align != "right" || s.Vertical != "bottom" || *s.Padding != 12 || *s.BorderWidth != 2 {
		t.Fatalf("cell style lost: %#v", s)
	}
	if len(got.Cells[1][0].Runs) < 2 || !got.Cells[1][0].Runs[0].Bold || !got.Cells[1][0].Runs[1].Underline || got.Cells[1][0].Runs[1].Color != "#FF0055" {
		t.Fatal("rich text lost", got.Cells[1][0].Runs)
	}
	if got.Cells[2][2].Style.Fill != "none" || got.Cells[2][2].Style.BorderColor != "none" {
		t.Fatal("explicit Off lost")
	}
	q, _ := url.ParseQuery(element.Query)
	if q.Get("object-rotation") != "12" {
		t.Fatal("rotation lost", q)
	}
	if math.Abs(got.Rows[1].Height/got.Rows[0].Height-2) > .001 || math.Abs(got.Columns[1].Width/got.Columns[0].Width-2) > .001 {
		t.Fatal("track ratios lost")
	}
	beforeScene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	afterScene, err := buildSlideScene(imported, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var beforeBounds, afterBounds sceneRect
	for _, o := range beforeScene.Objects {
		if o.Kind == "table" {
			beforeBounds = o.Bounds
		}
	}
	for _, o := range afterScene.Objects {
		if o.Kind == "table" {
			afterBounds = o.Bounds
		}
	}
	for _, pair := range [][2]float64{{beforeBounds.X, afterBounds.X}, {beforeBounds.Y, afterBounds.Y}, {beforeBounds.Width, afterBounds.Width}, {beforeBounds.Height, afterBounds.Height}} {
		if math.Abs(pair[0]-pair[1]) > .001 {
			t.Fatal("PPTX table bounds drifted", beforeBounds, afterBounds)
		}
	}
	md, err := serializeDeck("tables.md", imported)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("tables.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Objects == nil || reopened.Objects.Tables[element.ObjectID].Cells[1][0].Text != table.Cells[1][0].Text {
		t.Fatal("imported table is not portable")
	}
}

func TestTablePPTXFontEmbeddingIncludesCellConsumers(t *testing.T) {
	table, _ := newTableData(1, 1)
	table.Cells[0][0].Text = "C64 in a cell"
	table.Cells[0][0].Style = &TableCellStyle{FontID: "c64"}
	width := 72.0
	table.Cells[0][0].Style.FontWidth = &width
	d := Deck{Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"data": table}}, Slides: []Slide{{Elements: []Element{{ID: "table", Kind: "table", ObjectID: "data", Query: "left=10&top=5&width=150&height=20"}}}}}
	p, _, err := deckToPPTX(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.EmbeddedFonts) != 1 || !strings.HasPrefix(p.EmbeddedFonts[0].Family, "Keynope C64") || len(p.EmbeddedFonts[0].Data) == 0 {
		t.Fatal("cell font was not collected")
	}
	if !strings.HasSuffix(p.EmbeddedFonts[0].Family, "W30.002") {
		t.Fatal("table width did not select its embedded font variant", p.EmbeddedFonts[0].Family)
	}
	opened := preparePPTXEditorModel(d)
	scene, err := buildDocumentSlideScene(opened, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, object := range scene.Objects {
		if object.Table != nil {
			matched = pptxC64FontFamily(object.Table.Cells[0].Text.WidthScale) == p.EmbeddedFonts[0].Family
		}
	}
	if !matched {
		t.Fatal("embedded table width differs from displayed editor cell")
	}
}

func TestTablePPTXImportReportsMetricAndFontSubstitution(t *testing.T) {
	table := &pptx.Table{Rows: []float64{100}, Columns: []float64{100}, Cells: [][]pptx.TableCell{{{Text: "Foreign", FontFace: "Foreign Font", FontSize: 1000, Padding: 500, BorderWidth: 200, Color: "#ffffff", Align: "start", Vertical: "middle"}}}}
	got, warnings, err := pptxTableToData(table, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 4 || *got.Cells[0][0].Style.FontSize != 1024 || *got.Cells[0][0].Style.Padding != 100 {
		t.Fatal("missing bounded conversion diagnostics", warnings)
	}
}
