package app

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestHistoryTableEqualityFieldCoverage(t *testing.T) {
	for _, entry := range []struct {
		value  any
		fields []string
	}{
		{TableData{}, []string{"Version", "Rows", "Columns", "Cells", "Style", "Extra"}},
		{TableRow{}, []string{"Height", "Extra"}},
		{TableColumn{}, []string{"Width", "Extra"}},
		{TableCell{}, []string{"Text", "Runs", "RowSpan", "ColSpan", "Hidden", "Style", "BorderFragments", "Extra"}},
		{ObjectTextRun{}, []string{"Text", "Bold", "BoldOff", "Italic", "Underline", "Color", "ColorToken", "FontID", "FontRole", "FontSize", "Extra"}},
		{TableStyle{}, []string{"Header", "Banded", "Cell", "HeaderFill", "BandFill", "Extra"}},
		{TableCellStyle{}, []string{"DefaultColor", "Fill", "Color", "BorderColor", "BorderWidth", "Borders", "FontID", "FontSize", "FontWidth", "LineHeight", "Align", "Vertical", "Padding", "Extra"}},
		{TableBorderStyle{}, []string{"Color", "Width", "Extra"}},
		{ChartData{}, []string{"Version", "Type", "Title", "Categories", "CategoryColors", "Series", "Options", "Extra"}},
		{ChartSeries{}, []string{"LabelColor", "Name", "Values", "Color", "ColorToken", "Extra"}},
		{ChartColorStyle{}, []string{"LabelColor", "Color", "ColorToken", "Extra"}},
		{ChartOptions{}, []string{"DefaultColor", "DefaultFontID", "FontID", "FontRole", "FontWidth", "GridColor", "HideAxes", "Legend", "DataLabels", "Stacked", "ValueFormat", "XTitle", "YTitle", "Minimum", "Maximum", "Extra"}},
		{SlideBuild{}, []string{"Version", "Transition", "Steps", "Extra"}},
		{SlideTransition{}, []string{"Effect", "Direction", "DurationMS", "Extra"}},
		{BuildStep{}, []string{"TargetIDs", "Effect", "Direction", "DurationMS", "Advance", "DelayMS", "Extra"}},
	} {
		typ := reflect.TypeOf(entry.value)
		if typ.NumField() != len(entry.fields) {
			t.Fatal("new field requires exact history equality coverage", typ)
		}
		for i, field := range entry.fields {
			if typ.Field(i).Name != field {
				t.Fatal("update exact history equality", typ, field)
			}
		}
	}
}

func TestHistoryPayloadEqualityMatchesReflectionAtEveryLeaf(t *testing.T) {
	n := 12.0
	extra := &jsonExtensions{"future": json.RawMessage(`{"nested":true}`)}
	edge := TableBorderStyle{Color: "#123456", Width: &n, Extra: extra}
	style := TableCellStyle{DefaultColor: "#111111", Fill: "#222222", Color: "#333333", BorderColor: "#444444", BorderWidth: &n, Borders: map[string]TableBorderStyle{"top": edge}, FontID: "sans", FontSize: &n, FontWidth: &n, LineHeight: &n, Align: "center", Vertical: "middle", Padding: &n, Extra: extra}
	table := TableData{Version: 1, Rows: []TableRow{{Height: 1, Extra: extra}}, Columns: []TableColumn{{Width: 1, Extra: extra}}, Style: TableStyle{Header: true, Banded: true, Cell: style, HeaderFill: "#abcdef", BandFill: "#fedcba", Extra: extra}, Extra: extra,
		Cells: [][]TableCell{{{Text: "Cell", RowSpan: 1, ColSpan: 1, Style: &style, Extra: extra, BorderFragments: map[string][]TableBorderStyle{"bottom": {edge}}, Runs: []ObjectTextRun{{Text: "Run", Bold: true, BoldOff: true, Italic: true, Underline: true, Color: "#112233", ColorToken: "accent-1", FontID: "mono", FontRole: "heading", FontSize: 36, Extra: extra}}}}}}
	chart := newChartData("column")
	chart.Extra = extra
	chart.CategoryColors = []ChartColorStyle{{LabelColor: "#123456", Color: "#654321", ColorToken: "accent-1", Extra: extra}}
	chart.Series[0].Extra = extra
	chart.Options = ChartOptions{DefaultColor: "#111111", DefaultFontID: "sans", FontID: "mono", FontRole: "heading", FontWidth: &n, GridColor: "#222222", HideAxes: true, Legend: true, DataLabels: true, Stacked: true, ValueFormat: "integer", XTitle: "X", YTitle: "Y", Minimum: &n, Maximum: &n, Extra: extra}
	build := SlideBuild{Version: 1, Extra: extra, Transition: SlideTransition{Effect: "fade", Direction: "left", DurationMS: 400, Extra: extra}, Steps: []BuildStep{{TargetIDs: []string{"a", "b"}, Effect: "appear", Direction: "right", DurationMS: 200, Advance: "after", DelayMS: 50, Extra: extra}}}
	a := struct {
		Table TableData
		Chart ChartData
		Build SlideBuild
	}{table, chart, build}
	b := a
	b.Table, b.Chart, b.Build = cloneTable(a.Table), cloneChart(a.Chart), cloneBuild(a.Build)
	check := func(path string) {
		t.Helper()
		if got, want := historyTableEqual(a.Table, b.Table) && historyChartEqual(a.Chart, b.Chart) && historyBuildEqual(a.Build, b.Build), reflect.DeepEqual(a, b); got != want {
			t.Fatal("history equality differs from reflection", path, got, want)
		}
	}
	check("initial")
	changes := 0
	var walk func(reflect.Value, string, func())
	walk = func(v reflect.Value, path string, notify func()) {
		old := reflect.New(v.Type()).Elem()
		old.Set(v)
		changed := func(suffix string) { notify(); check(path + suffix); changes++ }
		if v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
			v.SetZero()
			changed("/nil")
			v.Set(old)
		}
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem(), path+"/*", notify)
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i), path+"/"+v.Type().Field(i).Name, notify)
			}
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			changed("/empty")
			v.Set(old)
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), path+"/item", notify)
			}
		case reflect.Map:
			v.Set(reflect.MakeMap(v.Type()))
			changed("/empty")
			v.Set(old)
			for _, key := range v.MapKeys() {
				value := v.MapIndex(key)
				mutable := reflect.New(value.Type()).Elem()
				mutable.Set(value)
				walk(mutable, path+"/map", func() { v.SetMapIndex(key, mutable); notify() })
				v.SetMapIndex(key, value)
			}
		case reflect.String:
			v.SetString(v.String() + " changed")
			changed("")
			v.Set(old)
		case reflect.Bool:
			v.SetBool(!v.Bool())
			changed("")
			v.Set(old)
		case reflect.Int:
			v.SetInt(v.Int() + 1)
			changed("")
			v.Set(old)
		case reflect.Uint8:
			v.SetUint((v.Uint() + 1) & 255)
			changed("")
			v.Set(old)
		case reflect.Float64:
			v.SetFloat(v.Float() + 1)
			changed("")
			v.SetFloat(math.NaN())
			changed("/NaN")
			v.Set(old)
		default:
			t.Fatal("extend equality oracle", path, v.Type())
		}
	}
	walk(reflect.ValueOf(&b).Elem(), "table", func() {})
	check("restored")
	if changes < 300 {
		t.Fatal("insufficient authored leaf coverage", changes)
	}
	// Preserve identity semantics for malformed shared NaNs as DeepEqual does.
	a.Table = TableData{Rows: []TableRow{{Height: math.NaN()}}}
	b = a
	check("shared NaN row")
	b.Table = cloneTable(a.Table)
	check("independent NaN row")
	a.Table = TableData{Cells: [][]TableCell{{{Runs: []ObjectTextRun{{FontSize: math.NaN()}}}}}}
	b = a
	check("shared NaN run")
	b.Table = cloneTable(a.Table)
	check("independent NaN run")
}
