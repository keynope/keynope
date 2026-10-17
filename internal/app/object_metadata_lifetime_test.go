package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOpaqueNestedMetadataPreservesObjectDependenciesOnSave(t *testing.T) {
	extra := func() *jsonExtensions {
		return &jsonExtensions{"futureLink": json.RawMessage(`{"target":"orphan-chart","other":"orphan-table"}`)}
	}
	for _, location := range []string{"table", "row", "column", "cell", "run", "cell-style", "border", "fragment", "table-style", "table-default", "chart", "series", "point", "options", "build", "transition", "step"} {
		t.Run(location, func(t *testing.T) {
			d := structuredFixture()
			table, chart, build := d.Objects.Tables["table-1"], d.Objects.Charts["chart-1"], d.Objects.Builds["slide-1"]
			switch location {
			case "table":
				table.Extra = extra()
			case "row":
				table.Rows[0].Extra = extra()
			case "column":
				table.Columns[0].Extra = extra()
			case "cell":
				table.Cells[1][0].Extra = extra()
			case "run":
				table.Cells[1][0].Runs[0].Extra = extra()
			case "cell-style":
				table.Cells[1][0].Style.Extra = extra()
			case "border":
				table.Cells[1][0].Style.Borders = map[string]TableBorderStyle{"left": {Extra: extra()}}
			case "fragment":
				table.Cells[1][0].BorderFragments = map[string][]TableBorderStyle{"left": {{Extra: extra()}}}
			case "table-style":
				table.Style.Extra = extra()
			case "table-default":
				table.Style.Cell.Extra = extra()
			case "chart":
				chart.Extra = extra()
			case "series":
				chart.Series[0].Extra = extra()
			case "point":
				chart.CategoryColors = []ChartColorStyle{{Extra: extra()}}
			case "options":
				chart.Options.Extra = extra()
			case "build":
				build.Extra = extra()
			case "transition":
				build.Transition.Extra = extra()
			case "step":
				build.Steps[0].Extra = extra()
			}
			d.Objects.Tables["table-1"], d.Objects.Charts["chart-1"], d.Objects.Builds["slide-1"] = table, chart, build
			d.Objects.Tables["orphan-table"] = cloneTable(structuredFixture().Objects.Tables["table-1"])
			d.Objects.Charts["orphan-chart"] = newChartData("column")
			before := cloneDeck(d)
			first, err := serializeDeck("opaque.md", d)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("opaque.md", first)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Objects.Tables) != 2 || len(loaded.Objects.Charts) != 2 || !hasOpaqueObjectMetadata(loaded) {
				t.Fatal("opaque reference or dependency lost")
			}
			second, err := serializeDeck("opaque.md", loaded)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("opaque save/reopen not deterministic", err)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("save changed undo-owned input")
			}
		})
	}
}

func TestAuthoredOpaqueMetadataAndRuntimeExclusions(t *testing.T) {
	extra := func() *jsonExtensions { return &jsonExtensions{"future": json.RawMessage(`"orphan-table"`)} }
	for _, location := range []string{"deck", "slide", "element", "master", "layout", "tab", "theme"} {
		d := structuredFixture()
		d.EnsureDefaultMasters()
		switch location {
		case "deck":
			d.Extra = extra()
		case "slide":
			d.Slides[0].Extra = extra()
		case "element":
			d.Slides[0].Elements[0].Extra = extra()
		case "master":
			d.Masters.Base.Extra = extra()
		case "layout":
			d.Masters.Layouts[0].Extra = extra()
		case "tab":
			d.Tabs = []DeckTab{{Extra: extra()}}
		case "theme":
			d.Theme = builtinDeckTheme("clean-light")
			d.Theme.Fonts.Body.Extra = extra()
		}
		d.Objects.Tables["orphan-table"] = cloneTable(d.Objects.Tables["table-1"])
		pruneUnusedDeckObjects(&d)
		if _, exists := d.Objects.Tables["orphan-table"]; !exists {
			t.Fatal(location, "lost opaque dependency")
		}
	}
	d := structuredFixture()
	d.Assets = map[string]DeckAsset{"media": {Data: make([]byte, 1<<20)}}
	d.Slides[0].ModernScene = &slideScene{Objects: []sceneObject{{Chart: &sceneChart{Data: ChartData{Extra: extra()}}}}}
	if hasOpaqueObjectMetadata(d) {
		t.Fatal("runtime scene counted as authored metadata")
	}
	d.Objects.Tables["orphan-table"] = cloneTable(d.Objects.Tables["table-1"])
	pruneUnusedDeckObjects(&d)
	if _, exists := d.Objects.Tables["orphan-table"]; exists {
		t.Fatal("known orphan not pruned")
	}
}

func BenchmarkOpaqueMetadataLifetimeScan(b *testing.B) {
	d := structuredFixture()
	data, err := newTableData(100, 100)
	if err != nil {
		b.Fatal(err)
	}
	d.Objects.Tables["table-1"] = data
	d.Assets = map[string]DeckAsset{"unused": {Data: make([]byte, 1<<20)}}
	if hasOpaqueObjectMetadata(d) {
		b.Fatal("known table treated as opaque")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if hasOpaqueObjectMetadata(d) {
			b.Fatal("known table treated as opaque")
		}
	}
}

func TestEditorGeneratedMastersSaveReopenStable(t *testing.T) {
	for _, kind := range []string{"legacy", "modern", "theme"} {
		d := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Hello"}}}}}
		if kind == "modern" {
			d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
		}
		if kind == "theme" {
			d.Theme = builtinDeckTheme("clean-light")
		}
		firstSession := newNativeEditorSession("default.md", d)
		first, err := serializeDeck("default.md", firstSession.deck)
		if err != nil {
			t.Fatal(kind, err)
		}
		loaded, err := parseDeckData("default.md", first)
		if err != nil {
			t.Fatal(kind, err)
		}
		secondSession := newNativeEditorSession("default.md", loaded)
		second, err := serializeDeck("default.md", secondSession.deck)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatal(kind, "generated masters changed after reopen", err)
		}
	}
}
