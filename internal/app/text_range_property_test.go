package app

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWordSizeDeltaMixedInheritedAndClamped(t *testing.T) {
	runs := []sceneRun{{Text: "A", FontSize: 30, ColorToken: "accent-1", Color: "#112233"}, {Text: "B", Bold: true}, {Text: "C", FontSize: 1024}, {Text: " tail", Italic: true}}
	before := append([]sceneRun(nil), runs...)
	got, changed, err := applyTextRangeSizeDelta(runs, &textStyleRange{0, 3}, 64, 1)
	if err != nil || !changed || len(got) != 4 || got[0].FontSize != 31 || got[1].FontSize != 65 || got[2].FontSize != 1024 || !reflect.DeepEqual(got[3], runs[3]) || got[0].ColorToken != "accent-1" || !got[1].Bold || !reflect.DeepEqual(runs, before) {
		t.Fatal("increment flattened or changed unrelated data", got, err)
	}
	for _, delta := range []float64{-1, 0} {
		got, changed, err = applyTextRangeSizeDelta([]sceneRun{{Text: "Inherited"}}, &textStyleRange{0, 9}, 1, delta)
		if err != nil || changed || got[0].FontSize != 0 {
			t.Fatal("clamped/no-op increment materialized inheritance", got, err)
		}
	}
	for _, delta := range []float64{1025, math.Inf(1), math.NaN()} {
		if _, _, err := applyTextRangeSizeDelta(runs, &textStyleRange{0, 1}, 64, delta); err == nil {
			t.Fatal("invalid delta accepted", delta)
		}
	}
}

func TestTableWordTypedSizeIncrement(t *testing.T) {
	s := newNativeEditorSession("Typed.md", tableWordThemeFixture(t))
	version := s.version
	delta := 1.0
	a := nativeEditorAction{Action: "set-selection-property", StyleContext: "text-range", SceneRevision: &version, ObjectID: "table-word", TextRuns: []sceneRun{{Text: "Small", FontSize: 30}, {Text: " Inherited"}}, TextRange: &textStyleRange{0, 15}, Property: "text.size", PropertyDelta: &delta}
	w := tableWordStyleCall(t, s, a, map[string]any{"row": 0, "column": 0, "rows": 2, "columns": 2, "header": false, "baseSize": 64})
	if w.Code != 200 {
		t.Fatal("shared word increment rejected", w.Code, w.Body.String())
	}
	var response struct {
		Runs []sceneRun                `json:"textRuns"`
		Caps *selectionCapabilityState `json:"selectionCapabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Runs) != 2 || response.Runs[0].FontSize != 31 || response.Runs[1].FontSize != 65 || response.Caps.Properties["text.size"].State != "mixed" || s.version != version || len(s.undo) != 0 {
		t.Fatal("increment flattened mixed sizes or committed early", response)
	}
	a.PropertyValue = json.RawMessage(`50`)
	w = tableWordStyleCall(t, s, a, map[string]any{"row": 0, "column": 0, "rows": 2, "columns": 2, "baseSize": 64})
	if w.Code != 400 {
		t.Fatal("ambiguous set/increment accepted", w.Code)
	}
}

func TestWordPropertyBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_TABLE_WORD_STYLE_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	d := tableWordThemeFixture(t)
	table := d.Objects.Tables["table-word-data"]
	table.Cells[0][1].Runs = []ObjectTextRun{{Text: "Small", FontID: "mono", FontSize: 30, Color: "#112233", ColorToken: "accent-1"}, {Text: " Inherited", Color: "#ff55aa"}}
	d.Objects.Tables["table-word-data"] = table
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "word-properties.md"), d); err != nil {
		t.Fatal(err)
	}
}

func TestWordIncrementInheritedBodyDraftAndRejections(t *testing.T) {
	d, _ := wordStyleFixture(t)
	s := newNativeEditorSession("Body.md", d)
	version := s.version
	delta := 1.0
	base := 120.0
	a := nativeEditorAction{Action: "set-selection-property", StyleContext: "text-range", SceneRevision: &version, ObjectID: "words", TextRuns: []sceneRun{{Text: "Plain"}}, TextRange: &textStyleRange{0, 5}, Property: "text.size", PropertyDelta: &delta, ModernSize: &base}
	w := tableWordStyleCall(t, s, a, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Runs []sceneRun                `json:"textRuns"`
		Caps *selectionCapabilityState `json:"selectionCapabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Runs[0].FontSize != 121 || response.Caps.Properties["text.size"].Value != float64(121) || s.version != version || len(s.undo) != 0 {
		t.Fatal("body increment ignored local parent size or committed", response)
	}
	for _, test := range []struct {
		name   string
		change func(*nativeEditorAction)
	}{
		{"wrong-property", func(a *nativeEditorAction) { a.Property = "text.font" }},
		{"wrong-action", func(a *nativeEditorAction) { a.Action = "copy-object-style" }},
		{"invalid-base", func(a *nativeEditorAction) { v := 0.0; a.ModernSize = &v }},
		{"oversized-delta", func(a *nativeEditorAction) { v := 1025.0; a.PropertyDelta = &v }},
		{"set-and-step", func(a *nativeEditorAction) { a.PropertyValue = json.RawMessage(`90`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := a
			test.change(&bad)
			before := cloneDeck(s.deck)
			w := tableWordStyleCall(t, s, bad, nil)
			if w.Code != 400 || !reflect.DeepEqual(s.deck, before) || s.version != version || len(s.undo) != 0 || s.styleClipboard != nil {
				t.Fatal("invalid word increment accepted or mutated", w.Code, w.Body.String())
			}
		})
	}
}
