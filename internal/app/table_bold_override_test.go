package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func headerBoldFixture(t *testing.T) Deck {
	t.Helper()
	data, err := newTableData(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	data.Style.Header = true
	data.Cells[0][0].Text = ""
	data.Cells[0][0].Runs = []ObjectTextRun{{Text: "Header "}, {Text: "normal", BoldOff: true}, {Text: " bold", Bold: true}}
	data.Cells[0][1].Text = "Inherited heading"
	data.Cells[1][0].Text = "Body"
	size := 50.0
	data.Style.Cell.FontID, data.Style.Cell.FontSize = "sans", &size
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "bold-slide", Elements: []Element{{ID: "bold-table", Kind: "table", ObjectID: "bold-data", Query: "left=20&top=8&width=200&height=35"}}}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"bold-data": data}}}
}

func TestTableHeaderExplicitNormalProjectionPersistenceAndPPTX(t *testing.T) {
	d := headerBoldFixture(t)
	before := cloneDeck(d)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	runs := scene.Objects[0].Table.Cells[0].Text.Runs
	if !runs[0].Bold || runs[1].Bold || !runs[1].BoldOff || !runs[2].Bold {
		t.Fatal("header forced explicit normal weight back to bold", runs)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("header projection baked defaults")
	}
	md, err := serializeDeck("Header.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Header.md", md)
	if err != nil || !reflect.DeepEqual(d.Objects, loaded.Objects) {
		t.Fatal("normal weight lost on save/reopen", err)
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err, report)
	}
	imported, _, err := importPPTXDocument(data, "Header.pptx")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range imported.Objects.Tables {
		found := false
		for _, run := range table.Cells[0][0].Runs {
			if run.Text == "normal" {
				found = true
				if run.Bold || !run.BoldOff {
					t.Fatal("editable PPTX lost explicit normal header word", run)
				}
			}
		}
		if !found {
			t.Fatal("PPTX flattened header words")
		}
	}
	if root := os.Getenv("KEYNOPE_HEADER_BOLD_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "header.md"), md, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "header.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWordExplicitNormalSnapshotResetAndNoOp(t *testing.T) {
	runs := []sceneRun{{Text: "ABC", Bold: true}}
	rangeWord := &textStyleRange{Start: 0, End: 1}
	snapshot := &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{"text.bold": {Present: true, Value: "false"}}}
	off, changed, err := applyTextRangeStyle(runs, rangeWord, snapshot, true, false)
	if err != nil || !changed || off[0].Bold || !off[0].BoldOff || !off[1].Bold {
		t.Fatal("word unbold did not set independent override", off, err)
	}
	repeated, changed, err := applyTextRangeStyle(off, rangeWord, snapshot, true, false)
	if err != nil || changed || !reflect.DeepEqual(repeated, off) {
		t.Fatal("unbold repeat is not a no-op")
	}
	reset, changed, err := applyTextRangeStyle(off, rangeWord, nil, true, true)
	if err != nil || !changed || reset[0].BoldOff || !reset[0].Bold {
		t.Fatal("reset did not restore header emphasis", reset, err)
	}
	on := &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{"text.bold": {Present: true, Value: "true"}}}
	bold, _, err := applyTextRangeStyle(off, rangeWord, on, true, false)
	if err != nil || bold[0].BoldOff || !bold[0].Bold {
		t.Fatal("bold on retained off marker")
	}
	plain := []sceneRun{{Text: "ABC"}}
	_, changed, err = applyTextRangeStyle(plain, rangeWord, snapshot, false, false)
	if err != nil || changed {
		t.Fatal("ordinary normal no-op materialised header metadata")
	}
	encoded, err := encodeModernRuns("ABC", "bold", "text", off)
	if err != nil {
		t.Fatal(err)
	}
	stored := decodeModernRuns(encoded)
	if stored == nil || !reflect.DeepEqual(stored.Runs, off) {
		t.Fatal("explicit normal modern runs lost")
	}
	if validSceneRun(sceneRun{Text: "A", Bold: true, BoldOff: true}) {
		t.Fatal("conflicting bold overrides accepted")
	}
}

func TestHeaderNormalHistoryOwnedNormalAndMaster(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := headerBoldFixture(t)
		d.EnsureDefaultMasters()
		if master {
			d.Masters.Base.Slide = cloneSlide(d.Slides[0])
		}
		s := newNativeEditorSession("Header.md", d)
		s.masterMode, s.currentMaster = master, 0
		before := cloneDeck(s.deck)
		table := cloneTable(d.Objects.Tables["bold-data"])
		table.Cells[0][0].Runs[0].BoldOff = true
		rev := s.version
		a := nativeEditorAction{Action: "set-table", Slide: 0, SceneMaster: master, SceneRevision: &rev, ObjectID: "bold-table", TableData: &table}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		table.Cells[0][0].Runs[0].BoldOff = false
		if !reflect.DeepEqual(after, s.deck) || len(s.undo) != 1 {
			t.Fatal("header normal aliases caller/history")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("header normal undo", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("header normal redo", err)
		}
	}
	var run ObjectTextRun
	if err := json.Unmarshal([]byte(`{"text":"A","boldOff":true,"future":1}`), &run); err != nil || !run.BoldOff || run.Extra == nil {
		t.Fatal("override/extension decode", err)
	}
	run.BoldOff = false
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ObjectTextRun
	if json.Unmarshal(raw, &loaded) != nil || loaded.BoldOff || loaded.Extra == nil {
		t.Fatal("cleared off marker resurrected from extensions")
	}
}
