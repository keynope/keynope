package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredDuplicationAndHistory(t *testing.T) {
	s := newNativeEditorSession("table.md", structuredFixture(), false, false)
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "duplicate-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	copy := s.deck.Slides[0].Elements[1]
	if copy.ObjectID == "table-1" || copy.ID == "table-element" || len(s.deck.Objects.Tables) != 2 {
		t.Fatal("duplicate shares source identity")
	}
	table := s.deck.Objects.Tables[copy.ObjectID]
	table.Cells[1][0].Text = "Copied"
	s.deck.Objects.Tables[copy.ObjectID] = table
	if s.deck.Objects.Tables["table-1"].Cells[1][0].Text != "One" {
		t.Fatal("duplicate aliases source")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Objects, s.deck.Objects) {
		t.Fatal("undo did not restore payload ownership")
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Objects.Tables) != 2 {
		t.Fatal("redo lost copied payload")
	}
}
func TestStructuredSlideCloneRemapsBuildTargets(t *testing.T) {
	s := newNativeEditorSession("deck.md", structuredFixture(), false, false)
	if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
		t.Fatal(err)
	}
	source, copy := s.deck.Slides[0], s.deck.Slides[1]
	if copy.ID == source.ID || copy.Elements[0].ObjectID == source.Elements[0].ObjectID {
		t.Fatal("cloned slide shares identity/payload")
	}
	build, ok := s.deck.Objects.Builds[copy.ID]
	if !ok || build.Steps[0].TargetIDs[0] != copy.Elements[0].ID {
		t.Fatal("cloned build targets the source slide")
	}
	if _, err := serializeDeck("clone.md", s.deck); err != nil {
		t.Fatal(err)
	}
}
func TestStructuredReferenceValidation(t *testing.T) {
	deck := structuredFixture()
	deck.Slides[0].Elements[0].ObjectID = "missing"
	if validateDeckObjectReferences(deck) == nil {
		t.Fatal("accepted missing table payload")
	}
	deck = structuredFixture()
	deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
	if validateDeckObjectReferences(deck) == nil {
		t.Fatal("accepted duplicate slide IDs")
	}
}

func structuredFixture() Deck {
	border, size := 1.0, 30.0
	return Deck{
		Slides: []Slide{{ID: "slide-1", Elements: []Element{
			{ID: "table-element", Kind: "table", ObjectID: "table-1", Query: "top=4&left_pct=0.100000&width=90&height=20"},
			{ID: "chart-element", Kind: "chart", ObjectID: "chart-1", Query: "top=26&left_pct=0.100000&width=90&height=20"},
		}}},
		Objects: &DeckObjects{Version: 1,
			Tables: map[string]TableData{"table-1": {Version: 1, Rows: []TableRow{{Height: 1}, {Height: 1}}, Columns: []TableColumn{{Width: 1}, {Width: 1}},
				Cells: [][]TableCell{{{Text: "Header", ColSpan: 2}, {Hidden: true}}, {{Text: "One", Runs: []ObjectTextRun{{Text: "One", Bold: true}}, Style: &TableCellStyle{BorderWidth: &border, FontSize: &size}}, {Text: "Two"}}}}},
			Charts: map[string]ChartData{"chart-1": {Version: 1, Type: "column", Categories: []string{"A", "B"}, Series: []ChartSeries{{Name: "Values", Values: []float64{1, 2}}}}},
			Builds: map[string]SlideBuild{"slide-1": {Version: 1, Transition: SlideTransition{Effect: "fade", DurationMS: 300}, Steps: []BuildStep{{TargetIDs: []string{"table-element"}, Effect: "appear"}}}},
		},
	}
}

func TestStructuredObjectsMarkdownRoundTrip(t *testing.T) {
	deck := structuredFixture()
	first, err := serializeDeck("portable.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("portable.md", first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck.Objects, loaded.Objects) {
		t.Fatalf("payload round trip mismatch: %#v", loaded.Objects)
	}
	if loaded.Slides[0].ID != "slide-1" || len(loaded.Slides[0].Elements) != 2 {
		t.Fatalf("slide identity/objects lost: %#v", loaded.Slides)
	}
	for i, e := range loaded.Slides[0].Elements {
		if e.Kind != deck.Slides[0].Elements[i].Kind || e.ObjectID != deck.Slides[0].Elements[i].ObjectID || e.ID != deck.Slides[0].Elements[i].ID {
			t.Fatalf("element reference lost: %#v", e)
		}
	}
	second, err := serializeDeck("portable.md", loaded)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("save is not deterministic: %v\nfirst: %s\nsecond: %s", err, first, second)
	}
	if _, err := encodeDeckObjects(nil); err != nil {
		t.Fatal(err)
	}
	legacy, err := serializeDeck("legacy.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Hello"}}}}})
	if err != nil || bytes.Contains(legacy, []byte("keynope-objects")) {
		t.Fatal("legacy deck gained object archive")
	}
}

func TestStructuredObjectsCloneOwnership(t *testing.T) {
	deck := structuredFixture()
	deck.Objects.Extra = &jsonExtensions{"future": json.RawMessage(`{"references":["table-1"]}`)}
	table := deck.Objects.Tables["table-1"]
	table.Cells[1][0].Extra = &jsonExtensions{"future": json.RawMessage(`[1,2]`)}
	deck.Objects.Tables["table-1"] = table
	saved := cloneDeck(deck)
	if !reflect.DeepEqual(deck.Objects, saved.Objects) {
		t.Fatal("clone changed value")
	}
	copiedTable := saved.Objects.Tables["table-1"]
	copiedTable.Cells[1][0].Runs[0].Text = "Changed"
	*copiedTable.Cells[1][0].Style.FontSize = 120
	(*copiedTable.Cells[1][0].Extra)["future"][1] = '9'
	saved.Objects.Charts["chart-1"].Series[0].Values[0] = 77
	saved.Objects.Builds["slide-1"].Steps[0].TargetIDs[0] = "different"
	(*saved.Objects.Extra)["future"][0] = '['
	if deck.Objects.Tables["table-1"].Cells[1][0].Runs[0].Text != "One" || *deck.Objects.Tables["table-1"].Cells[1][0].Style.FontSize != 30 || deck.Objects.Charts["chart-1"].Series[0].Values[0] != 1 || deck.Objects.Builds["slide-1"].Steps[0].TargetIDs[0] != "table-element" || !json.Valid((*deck.Objects.Extra)["future"]) {
		t.Fatal("clone aliases authored payload")
	}
}

func TestStructuredObjectsPreserveCompatibleExtensions(t *testing.T) {
	raw := []byte(`{"version":1,"futureKind":{"nested":[1,2]},"tables":{"t":{"version":1,"future":true,"rows":[{"height":1,"future":2}],"columns":[{"width":1}],"cells":[[{"text":"x","future":"keep","runs":[{"text":"x","future":42}]}]],"style":{"cell":{"future":"style"}}}}}`)
	var objects DeckObjects
	if err := json.Unmarshal(raw, &objects); err != nil {
		t.Fatal(err)
	}
	if err := objects.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeDeckObjects(&objects)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeDeckObjects(encoded)
	if err != nil || !reflect.DeepEqual(&objects, decoded) {
		t.Fatalf("extensions lost: %v", err)
	}
	// An opaque future kind can own references we cannot inspect: keep known data.
	deck := Deck{Objects: decoded}
	pruneUnusedDeckObjects(&deck)
	if len(deck.Objects.Tables) != 1 {
		t.Fatal("pruning discarded opaque reference")
	}
}

func TestStructuredObjectsSavePruningDoesNotMutateHistory(t *testing.T) {
	deck := structuredFixture()
	table := cloneTable(deck.Objects.Tables["table-1"])
	deck.Objects.Tables["orphan"] = table
	before := cloneDeck(deck)
	encoded, err := serializeDeck("test.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("test.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Objects.Tables["orphan"]; ok {
		t.Fatal("saved unreferenced payload")
	}
	if !reflect.DeepEqual(before.Objects, deck.Objects) {
		t.Fatal("save pruning mutated undo-owned state")
	}
}

func TestStructuredObjectsValidateGeometryAndData(t *testing.T) {
	table := structuredFixture().Objects.Tables["table-1"]
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*TableData){
		func(t *TableData) { t.Rows[0].Height = math.NaN() },
		func(t *TableData) { t.Cells[0][0].RowSpan = 3 },
		func(t *TableData) { t.Cells[0][1].Hidden = false },
		func(t *TableData) { t.Cells[1][0].Hidden = true },
		func(t *TableData) { t.Cells[1] = nil },
	} {
		broken := cloneTable(table)
		mutate(&broken)
		if broken.Validate() == nil {
			t.Fatal("accepted invalid table")
		}
	}
	chart := structuredFixture().Objects.Charts["chart-1"]
	for _, mutate := range []func(*ChartData){
		func(c *ChartData) { c.Series[0].Values[0] = math.Inf(1) },
		func(c *ChartData) { c.Series[0].Values = c.Series[0].Values[:1] },
		func(c *ChartData) { c.Type = "pie"; c.Series[0].Values[0] = -1 },
	} {
		broken := cloneChart(chart)
		mutate(&broken)
		if broken.Validate() == nil {
			t.Fatal("accepted invalid chart")
		}
	}
	build := structuredFixture().Objects.Builds["slide-1"]
	build.Steps[0].TargetIDs = []string{"same", "same"}
	if build.Validate() == nil {
		t.Fatal("accepted duplicate build target")
	}
}

func TestStructuredObjectsRejectMalformedEnvelopes(t *testing.T) {
	valid, err := encodeDeckObjects(structuredFixture().Objects)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		"<!-- keynope-objects nope -->", valid + "\n" + valid,
		strings.Replace(valid, "version=1", "version=2", 1),
		"<!-- keynope-objects version=1 base64:AAAA -->",
	} {
		if _, _, err := decodeDeckObjects(input); err == nil {
			t.Fatalf("accepted malformed envelope %.60s", input)
		}
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte(" "), maxStructuredObjectBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	bomb := "<!-- keynope-objects version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->"
	if _, _, err := decodeDeckObjects(bomb); err == nil {
		t.Fatal("accepted oversized decompressed payload")
	}
}
