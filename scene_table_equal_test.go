package main

import (
	"math"
	"reflect"
	"sort"
	"testing"
)

// Keep a future model extension from silently escaping the optimized path.
func TestSceneTableEqualityFieldCoverage(t *testing.T) {
	check := func(value any, want []string) {
		t.Helper()
		typ := reflect.TypeOf(value)
		got := []string{}
		for i := 0; i < typ.NumField(); i++ {
			got = append(got, typ.Field(i).Name)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("update exact equality for model fields", typ, got)
		}
	}
	check(sceneTable{}, []string{"Header", "Banded", "Rows", "Columns", "Cells", "Borders"})
	check(sceneTableCell{}, []string{"Row", "Column", "RowSpan", "ColSpan", "Header", "Bounds", "ContentBounds", "Text", "Paint", "Fill", "BorderColor", "BorderWidth"})
	check(scenePaint{}, []string{"NoFill", "Opacity", "Color", "GradientStart", "GradientEnd", "GradientDirection", "Stroke", "StrokeWidth", "ShadowColor", "ShadowX", "ShadowY", "ShadowBlur"})
	check(sceneRun{}, []string{"Text", "Bold", "BoldOff", "Italic", "Underline", "Color", "ColorToken", "FontID", "FontRole", "FontSize"})
	check(sceneParagraph{}, []string{"Runs", "Bullet"})
	check(sceneTableBorder{}, []string{"Axis", "Track", "From", "To", "Color", "Width"})
	want := []string{"FontRoles", "ColorTokens", "EmojiFonts", "Runs", "Paragraphs"}
	typ := reflect.TypeOf(sceneTextScalarValues{})
	for i := 0; i < typ.NumField(); i++ {
		want = append(want, typ.Field(i).Name)
	}
	check(sceneText{}, want)
	for _, entry := range []struct {
		value any
		refs  map[string]reflect.Type
	}{
		{sceneTableCell{}, map[string]reflect.Type{"Text": reflect.TypeOf((*sceneText)(nil))}},
		{scenePaint{}, map[string]reflect.Type{"Opacity": reflect.TypeOf((*float64)(nil))}},
	} {
		typ := reflect.TypeOf(entry.value)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			switch field.Type.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
				if entry.refs[field.Name] != field.Type {
					t.Fatal("new reference requires exact equality handling", typ, field.Name)
				}
			}
		}
	}
}

func TestSceneTableEqualityMatchesReflectionAtEveryLeaf(t *testing.T) {
	opacity := .5
	a := &sceneTable{Rows: []float64{10, 20}, Columns: []float64{10, 20}, Borders: []sceneTableBorder{{Axis: "horizontal", Width: 1}}, Cells: []sceneTableCell{{Text: &sceneText{FontRoles: map[string]string{"heading": "mono"}, ColorTokens: map[string]string{"text": "#123456"}, EmojiFonts: map[string]string{"face": "font"}, Runs: []sceneRun{{Text: "word", FontSize: 12}}, Paragraphs: []sceneParagraph{{Bullet: true, Runs: []sceneRun{{Text: "paragraph"}}}}}, Paint: scenePaint{Opacity: &opacity}}}}
	b := cloneSceneTable(a)
	check := func(path string) {
		t.Helper()
		if got, want := workspaceSceneTableEqual(a, b), reflect.DeepEqual(a, b); got != want {
			t.Fatal("equality differs from reflection", path, got, want)
		}
	}
	check("initial")
	changes := 0
	var walk func(reflect.Value, string)
	walk = func(v reflect.Value, path string) {
		old := reflect.New(v.Type()).Elem()
		old.Set(v)
		if v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
			v.SetZero()
			check(path + "/nil")
			v.Set(old)
			changes++
		}
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem(), path+"/*")
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i), path+"/"+v.Type().Field(i).Name)
			}
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			check(path + "/empty")
			v.Set(old)
			changes++
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), path+"/item")
			}
		case reflect.Map:
			v.Set(reflect.MakeMap(v.Type()))
			check(path + "/empty")
			v.Set(old)
			changes++
			for _, key := range v.MapKeys() {
				value := v.MapIndex(key)
				next := reflect.New(value.Type()).Elem()
				next.SetString(value.String() + " changed")
				v.SetMapIndex(key, next)
				check(path + "/map value")
				v.SetMapIndex(key, value)
				changes++
			}
		case reflect.String:
			v.SetString(v.String() + " changed")
			check(path)
			v.Set(old)
			changes++
		case reflect.Bool:
			v.SetBool(!v.Bool())
			check(path)
			v.Set(old)
			changes++
		case reflect.Int:
			v.SetInt(v.Int() + 1)
			check(path)
			v.Set(old)
			changes++
		case reflect.Float64:
			v.SetFloat(v.Float() + 1)
			check(path)
			v.SetFloat(math.NaN())
			check(path + "/NaN")
			v.Set(old)
			changes += 2
		default:
			t.Fatal("extend equality oracle for new field type", path, v.Type())
		}
	}
	walk(reflect.ValueOf(b).Elem(), "table")
	if changes < 100 {
		t.Fatal("insufficient leaf coverage", changes)
	}
	check("restored")
	// Shared pointer/slice identities retain DeepEqual semantics even for NaNs.
	a.Rows[0] = math.NaN()
	b.Rows = a.Rows
	check("shared NaN row")
	b.Cells = a.Cells
	check("shared cells")
	if !workspaceSceneTableEqual(a, a) || workspaceSceneTableEqual(nil, a) || !workspaceSceneTableEqual(nil, nil) {
		t.Fatal("pointer/nil semantics changed")
	}
}

func TestSceneTableEqualityNilEmptyAndSharedNaNs(t *testing.T) {
	for _, pair := range [][2]*sceneTable{
		{nil, &sceneTable{}},
		{&sceneTable{}, &sceneTable{Rows: []float64{}}},
		{&sceneTable{}, &sceneTable{Columns: []float64{}}},
		{&sceneTable{}, &sceneTable{Cells: []sceneTableCell{}}},
		{&sceneTable{}, &sceneTable{Borders: []sceneTableBorder{}}},
	} {
		if workspaceSceneTableEqual(pair[0], pair[1]) != reflect.DeepEqual(pair[0], pair[1]) {
			t.Fatal("nil/empty table conflated")
		}
	}
	for _, b := range []*sceneText{{FontRoles: map[string]string{}}, {ColorTokens: map[string]string{}}, {EmojiFonts: map[string]string{}}, {Runs: []sceneRun{}}, {Paragraphs: []sceneParagraph{}}} {
		if sceneShapedTextEqual(&sceneText{}, b) {
			t.Fatal("nil/empty text fields conflated")
		}
	}
	nan := math.NaN()
	if !sceneFloatPointerEqual(&nan, &nan) {
		t.Fatal("shared NaN pointer differs")
	}
	other := math.NaN()
	if sceneFloatPointerEqual(&nan, &other) {
		t.Fatal("separate NaNs compare equal")
	}
	paragraphs := []sceneParagraph{{Runs: []sceneRun{{FontSize: math.NaN()}}}}
	a, b := &sceneText{Paragraphs: paragraphs}, &sceneText{Paragraphs: paragraphs}
	if !sceneShapedTextEqual(a, b) || !reflect.DeepEqual(a, b) {
		t.Fatal("shared paragraph identity changed")
	}
}
