package main

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

func nativeFragmentTable() *pptx.Table {
	t := &pptx.Table{Rows: []float64{120, 180}, Columns: []float64{200, 300, 100}, Cells: make([][]pptx.TableCell, 2)}
	for r := range t.Cells {
		t.Cells[r] = make([]pptx.TableCell, 3)
		for c := range t.Cells[r] {
			t.Cells[r][c] = pptx.TableCell{Text: "Cell", FontFace: "Arial", FontSize: 14, Color: "#223344", Fill: "#eeeedd", BorderColor: "#8899aa", BorderWidth: 1, Padding: 8}
		}
	}
	t.Cells[0][0].Text = "Merged cell"
	t.Cells[0][0].RowSpan, t.Cells[0][0].ColSpan = 2, 2
	t.Cells[0][0].Borders = map[string]pptx.TableBorder{"left": {Color: "#AA0000", Width: 2}, "top": {Color: "#AA0000", Width: 2}, "right": {Color: "#AA0000", Width: 2}, "bottom": {Color: "#AA0000", Width: 2}}
	t.Cells[0][1] = pptx.TableCell{Hidden: true, Borders: map[string]pptx.TableBorder{"top": {Color: "#00AA00", Width: 3}, "right": {Color: "#AA0000", Width: 2}, "bottom": {Color: "#ffffff", Width: 20}}}
	t.Cells[1][0] = pptx.TableCell{Hidden: true, Borders: map[string]pptx.TableBorder{"left": {Color: "#0000AA", Width: 4}, "bottom": {Color: "#0000AA", Width: 1.25}}}
	t.Cells[1][1] = pptx.TableCell{Hidden: true, Borders: map[string]pptx.TableBorder{"right": {Color: "#00AAAA", Width: 6}, "bottom": {Color: "#AA00AA", Width: 5.5}}}
	return t
}

func fragmentDeck(t *testing.T) Deck {
	t.Helper()
	data, warnings, err := pptxTableToData(nativeFragmentTable(), 1)
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"data": data}}, Slides: []Slide{{ID: "fragment-slide", PageNumber: pageNumberHide, Elements: []Element{{ID: "table", Kind: "table", ObjectID: "data", Query: "left=15&top=8&width=180&height=35&fg=%23223344"}}}}}
}

func TestImportedMergedPerimeterFragmentsSceneAndPersistence(t *testing.T) {
	d := fragmentDeck(t)
	data := d.Objects.Tables["data"]
	if !data.Cells[1][1].Hidden || data.Cells[0][0].BorderFragments["bottom"][1].Color != "#AA00AA" || data.Cells[0][1].Style != nil {
		t.Fatal("hidden perimeter or interior filtering wrong", data.Cells)
	}
	s, err := d.projectTable(data, sceneRect{Width: 600, Height: 300}, "#223344")
	if err != nil {
		t.Fatal(err)
	}
	find := func(axis string, track, offset int) sceneTableBorder {
		for _, e := range s.Borders {
			if e.Axis == axis && e.Track == track && e.From <= offset && e.To > offset {
				return e
			}
		}
		return sceneTableBorder{}
	}
	for _, check := range []struct {
		axis          string
		track, offset int
		color         string
		width         float64
	}{
		{"horizontal", 0, 0, "#AA0000", 2}, {"horizontal", 0, 1, "#00AA00", 3},
		{"horizontal", 2, 0, "#0000AA", 1.25}, {"horizontal", 2, 1, "#AA00AA", 5.5},
		{"vertical", 0, 0, "#AA0000", 2}, {"vertical", 0, 1, "#0000AA", 4},
		{"vertical", 2, 0, "#AA0000", 2}, {"vertical", 2, 1, "#00AAAA", 6},
	} {
		got := find(check.axis, check.track, check.offset)
		if !strings.EqualFold(got.Color, check.color) || got.Width != check.width {
			t.Fatal("fragment lost", check, got)
		}
	}
	if e := find("horizontal", 1, 1); e.Color != "" {
		t.Fatal("merged interior painted", e)
	}
	if e := find("vertical", 1, 1); e.Color != "" {
		t.Fatal("merged interior painted", e)
	}
	md, err := serializeDeck("Fragments.md", d)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Fragments.md", md)
	if err != nil || !reflect.DeepEqual(data, reopened.Objects.Tables["data"]) {
		t.Fatal("Markdown fragment loss", err)
	}
	// The native writer/reader retains continuation paint before the editor
	// adapter; this does not prove external Office rendering.
	var encoded bytes.Buffer
	_, err = pptx.Write(context.Background(), &encoded, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{{ID: "table", Kind: "table", Width: 600, Height: 300, Table: nativeFragmentTable()}}}}})
	if err != nil {
		t.Fatal(err)
	}
	read, report, err := pptx.Read(context.Background(), bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), pptx.Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	parsed, warnings, err := pptxTableToData(read.Slides[0].Objects[0].Table, 1)
	if err != nil || len(warnings) > 0 || parsed.Cells[0][0].BorderFragments["bottom"][1].Color != "#AA00AA" {
		t.Fatal("package continuation loss", err, warnings)
	}
}

func TestMergedFragmentPropertiesHistoryAndValidation(t *testing.T) {
	s := newNativeEditorSession("Fragments.md", fragmentDeck(t))
	original := cloneDeck(s.deck)
	range0 := &tableCellRange{}
	a := nativeEditorAction{Action: "set-selection-property", Slide: 0, ObjectID: "table", ObjectIDs: []string{"table"}, StyleContext: "table-cells", CellRange: range0, Property: "cell.borders.bottom.color", PropertyValue: json.RawMessage(`"#123456"`)}
	caps, err := tableCellStyleCapabilities(s.deck, a, "", s.version)
	if err != nil {
		t.Fatal(err)
	}
	if caps.Properties[a.Property].State != "mixed" || caps.Properties["cell.borders.bottom.width"].State != "mixed" {
		t.Fatal("fragment mixed properties hidden", caps.Properties)
	}
	version := s.version
	a.SceneRevision = &version
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
	edited := s.deck.Objects.Tables[s.deck.Slides[0].Elements[0].ObjectID]
	for _, edge := range edited.Cells[0][0].BorderFragments["bottom"] {
		if edge.Color != "" || edge.Width == nil {
			t.Fatal("colour edit discarded width or retained fragment colour", edge)
		}
	}
	if len(s.undo) != 1 {
		t.Fatal("not one history entry")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, original) {
		t.Fatal("undo lost fragments", err)
	}
	delta := 1.0
	a.Property, a.PropertyValue, a.PropertyDelta = "cell.borders.bottom.width", nil, &delta
	version = s.version
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
	edited = s.deck.Objects.Tables[s.deck.Slides[0].Elements[0].ObjectID]
	if *edited.Cells[0][0].BorderFragments["bottom"][0].Width != 2.25 || *edited.Cells[0][0].BorderFragments["bottom"][1].Width != 6.5 {
		t.Fatal("step flattened fragment widths")
	}
	style, err := captureTableCellStyle(original, original.Objects.Tables["data"], range0)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cell.borders.bottom.color", "cell.borders.bottom.width", "cell.borderColor", "cell.borderWidth"} {
		if _, ok := style.Fields[key]; ok {
			t.Fatal("mixed fragment falsely copied as uniform", key)
		}
	}
	reset, _, err := applyTableCellStyleData(original, original.Objects.Tables["data"], range0, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(reset.Cells[0][0].BorderFragments) != 0 || reset.Cells[1][1].Style != nil || reset.Cells[1][0].Style != nil {
		t.Fatal("reset left hidden overrides")
	}
	// Fully overridden/clamped sides must not materialize an unused owner
	// width merely to create a history entry. Independent in-range steps also
	// keep that sparse fallback unchanged.
	if edited.Cells[0][0].Style.Borders["bottom"].Width == nil || *edited.Cells[0][0].Style.Borders["bottom"].Width != 2 {
		t.Fatal("step authored an unused fallback")
	}
	clamped := cloneTable(original.Objects.Tables["data"])
	for i := range clamped.Cells[0][0].BorderFragments["bottom"] {
		v := 100.0
		clamped.Cells[0][0].BorderFragments["bottom"][i].Width = &v
	}
	a.PropertyDelta = &delta
	_, changed, err := incrementTableCellProperty(original, clamped, a)
	if err != nil || changed {
		t.Fatal("clamped fragment step was not a no-op", changed, err)
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		bad := cloneTable(original.Objects.Tables["data"])
		edge := bad.Cells[0][0].BorderFragments["bottom"][1]
		edge.Width = &value
		bad.Cells[0][0].BorderFragments["bottom"][1] = edge
		if bad.Validate() == nil {
			t.Fatal("invalid hidden width accepted", value)
		}
	}
}

func TestMergedFragmentThemeConsumer(t *testing.T) {
	d := fragmentDeck(t)
	d.Theme = builtinDeckTheme("clean-light")
	data := d.Objects.Tables["data"]
	edge := data.Cells[0][0].BorderFragments["bottom"][1]
	edge.Color = "theme:accent-6"
	data.Cells[0][0].BorderFragments["bottom"][1] = edge
	d.Objects.Tables["data"] = data
	after := cloneDeck(d)
	after.Theme.Colors.Accent[5] = "#ff00aa"
	if sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0, cols: 245, rows: 56}) {
		t.Fatal("hidden perimeter role missed by cache")
	}
}

func TestMergedFragmentCoveredCompatibility(t *testing.T) {
	d := fragmentDeck(t)
	data := d.Objects.Tables["data"]
	// Older covered-slot paint remains readable; public normalization must
	// retain sparse components but never override a canonical authored value.
	data.Cells[1][0].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"bottom": testEdge("#00ff55", 9)}}
	data.Cells[0][0].BorderFragments["bottom"][0].Color = "#ff0055"
	data.Cells[0][0].BorderFragments["bottom"][0].Width = nil
	d.Objects.Tables["data"] = data
	before, err := d.projectTable(data, sceneRect{Width: 600, Height: 300}, "#223344")
	if err != nil {
		t.Fatal(err)
	}
	public := participantSlideObjects(d.Objects, d.Slides[0])
	after, err := d.projectTable(public.Tables["data"], sceneRect{Width: 600, Height: 300}, "#223344")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Borders, after.Borders) {
		t.Fatal("covered-to-owner normalization changed audience paint", before.Borders, after.Borders)
	}
	if data.Cells[1][0].Style == nil || data.Cells[0][0].BorderFragments["bottom"][0].Width != nil {
		t.Fatal("public projection mutated authored legacy data")
	}
	// A single-track explicit segment also consumes theme roles, despite not
	// needing a merged span. Do not accidentally memoize it with default cells.
	one, _ := newTableData(1, 2)
	one.Cells[0][1].BorderFragments = map[string][]TableBorderStyle{"bottom": {{Color: "theme:accent-6"}}}
	d.Objects.Tables["data"] = one
	d.Theme = builtinDeckTheme("clean-light")
	next := cloneDeck(d)
	next.Theme.Colors.Accent[5] = "#ff00aa"
	if sceneProjectionInputsEqual(d, next, sceneResponseKey{index: 0, cols: 245, rows: 56}) {
		t.Fatal("single-track fragment skipped by theme memo")
	}
}

func TestMergedFragmentOwnershipPrivacyAndBounds(t *testing.T) {
	d := fragmentDeck(t)
	original := cloneDeck(d)
	copy := cloneDeck(d)
	segments := copy.Objects.Tables["data"].Cells[0][0].BorderFragments["bottom"]
	*segments[1].Width = 9
	if !reflect.DeepEqual(d, original) {
		t.Fatal("fragment width aliases deck clone")
	}
	var extended TableBorderStyle
	if err := json.Unmarshal([]byte(`{"color":"theme:accent-6","width":4,"private":{"note":"secret"}}`), &extended); err != nil {
		t.Fatal(err)
	}
	d.Objects.Tables["data"].Cells[0][0].BorderFragments["bottom"][1] = extended
	copy = cloneDeck(d)
	(*copy.Objects.Tables["data"].Cells[0][0].BorderFragments["bottom"][1].Extra)["private"] = json.RawMessage(`"changed"`)
	if string((*extended.Extra)["private"]) != `{"note":"secret"}` {
		t.Fatal("fragment extension aliases clone")
	}
	public := participantSlideObjects(d.Objects, d.Slides[0])
	encoded, _ := json.Marshal(public)
	if bytes.Contains(encoded, []byte("secret")) || bytes.Contains(encoded, []byte("private")) {
		t.Fatal("audience fragment extension leak")
	}
	if err := public.Tables["data"].Validate(); err != nil {
		t.Fatal(err)
	}
	if public.Tables["data"].Cells[0][0].BorderFragments["bottom"][1].Color != "theme:accent-6" {
		t.Fatal("audience border paint lost")
	}
	if extended.Extra == nil {
		t.Fatal("audience sanitizing mutated source")
	}
	for _, mutate := range []func(*TableData){
		func(x *TableData) { x.Cells[0][0].BorderFragments["bottom"] = []TableBorderStyle{{}} },
		func(x *TableData) { x.Cells[0][0].BorderFragments["diagonal"] = []TableBorderStyle{{}, {}} },
		func(x *TableData) { x.Cells[1][1].BorderFragments = map[string][]TableBorderStyle{"left": {{}}} },
		func(x *TableData) { x.Cells[0][0].BorderFragments["top"][0].Color = "red" },
	} {
		x := cloneTable(d.Objects.Tables["data"])
		mutate(&x)
		if x.Validate() == nil {
			t.Fatal("invalid fragment accepted")
		}
	}
}

func TestMergedFragmentSharedStructuralArtifacts(t *testing.T) {
	root := os.Getenv("KEYNOPE_FRAGMENT_EVIDENCE")
	if root == "" {
		t.Skip("shared JS structural artifacts not requested")
	}
	raw, err := os.ReadFile(filepath.Join(root, "structures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]TableData
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if err := data.Validate(); err != nil {
				t.Fatal(err)
			}
			d := fragmentDeck(t)
			d.Theme = builtinDeckTheme("clean-light")
			d.Objects.Tables["data"] = data
			scene, err := d.projectTable(data, sceneRect{Width: 600, Height: 300}, "#223344")
			if err != nil {
				t.Fatal(err)
			}
			if len(scene.Cells) == 0 || len(scene.Borders) == 0 {
				t.Fatal("empty structural scene")
			}
			for _, edge := range scene.Borders {
				if edge.Axis == "horizontal" && (edge.Track < 0 || edge.Track > len(data.Rows) || edge.To > len(data.Columns)) || edge.Axis == "vertical" && (edge.Track < 0 || edge.Track > len(data.Columns) || edge.To > len(data.Rows)) {
					t.Fatal("fragment outside table", edge)
				}
			}
			md, err := serializeDeck("Structure.md", d)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Structure.md", md)
			if err != nil || !reflect.DeepEqual(data, reopened.Objects.Tables["data"]) {
				t.Fatal("structural persistence loss", err)
			}
			if err := saveDeck(filepath.Join(root, name+".md"), d); err != nil {
				t.Fatal(err)
			}
			original := fragmentDeck(t)
			s := newNativeEditorSession("Structure.md", original)
			original = cloneDeck(s.deck)
			v := s.version
			if err := s.apply(nativeEditorAction{Action: "set-table", Slide: 0, ObjectID: "table", TableData: &data, SceneRevision: &v}); err != nil {
				t.Fatal(err)
			}
			if len(s.undo) != 1 {
				t.Fatal("not one structural history commit")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, original) {
				t.Fatal("structural undo loss", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck.Objects.Tables[s.deck.Slides[0].Elements[0].ObjectID], data) {
				t.Fatal("structural redo loss", err)
			}
			for _, side := range []string{"left", "right", "top", "bottom"} {
				for i, edge := range data.Cells[0][0].BorderFragments[side] {
					if edge.Color == "" {
						continue
					}
					er, ec := 0, i
					if side == "left" || side == "right" {
						er, ec = i, 0
					}
					format := d.tableCellFormat(data, 0, data.Cells[0][0], "#223344")
					got := d.tableCellBorderAt(data, 0, 0, side, er, ec, format.BorderColor, format.BorderWidth)
					if got.color != d.tableColor(edge.Color) || edge.Width != nil && got.width != *edge.Width {
						t.Fatal("structural paint changed", side, i, got, edge)
					}
				}
			}
		})
	}
}

func TestMergedFragmentBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_FRAGMENT_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	d := fragmentDeck(t)
	if err := saveDeck(filepath.Join(root, "fragments.md"), d); err != nil {
		t.Fatal(err)
	}
}
