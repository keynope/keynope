package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func tableWordThemeFixture(t *testing.T) Deck {
	t.Helper()
	data, err := newTableData(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	data.Style.Header, data.Style.Banded = false, false
	size := 64.0
	data.Style.Cell = TableCellStyle{FontID: "sans", FontSize: &size, Color: "#ffffff", Fill: "#203440"}
	// The linked cell is second, with exactly the same default/style identity
	// as the first: dependency memoisation must still inspect its word roles.
	data.Cells[0][0].Text = "Unlinked cell"
	if err := json.Unmarshal([]byte(`{"runs":[{"text":"Role","color":"#112233","colorToken":"accent-1","future":{"keep":true}},{"text":" Fixed","color":"#ff55aa"}]}`), &data.Cells[0][1]); err != nil {
		t.Fatal(err)
	}
	data.Cells[1][0].Text = "Other cell"
	data.Cells[1][1].Text = "Unchanged"
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-dark"), Slides: []Slide{{ID: "table-word-slide", FG: "38;2;255;255;255", BG: "48;2;12;22;32", FGSet: true, BGSet: true, Elements: []Element{{ID: "table-word", Kind: "table", ObjectID: "table-word-data", Query: "left=20&top=12&width=200&height=26&fg=%23ffffff"}}}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table-word-data": data}}}
}

func TestTableWordThemeProjectionPersistenceAndConsumers(t *testing.T) {
	d := tableWordThemeFixture(t)
	before := cloneDeck(d)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	cell := scene.Objects[0].Table.Cells[1]
	if cell.Text.Runs[0].Color != d.Theme.Colors.Accent[0] || cell.Text.Runs[0].ColorToken != "accent-1" || cell.Text.ColorTokens["accent-1"] != d.Theme.Colors.Accent[0] || cell.Text.Runs[1].Color != "#ff55aa" {
		t.Fatal("table word palette was not resolved", cell.Text)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("projection baked authored fallbacks")
	}
	changed := cloneDeck(d)
	changed.Theme.Colors.Accent[0] = "#55ff99"
	if sceneThemeInputsEqual(d, changed, d.Slides[0], sceneResponseKey{cols: 245, rows: 56}) {
		t.Fatal("later cell's word token missing from consumers")
	}
	unused := cloneDeck(d)
	unused.Theme.Colors.Accent[5] = "#123456"
	if !sceneThemeInputsEqual(d, unused, d.Slides[0], sceneResponseKey{cols: 245, rows: 56}) {
		t.Fatal("unused word role invalidated table")
	}
	md, err := serializeDeck("Table words.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Table words.md", md)
	if err != nil || !reflect.DeepEqual(loaded.Objects, d.Objects) {
		t.Fatal("table roles/extensions lost on reopen", err)
	}
	if root := os.Getenv("KEYNOPE_TABLE_WORD_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "table-words.md"), md, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTableWordThemeRejectInvalidRoleFallback(t *testing.T) {
	for _, raw := range []string{`{"text":"Bad","colorToken":"accent-1"}`, `{"text":"Bad","color":"#fff","colorToken":"accent-1"}`, `{"text":"Bad","color":"none","colorToken":"accent-1"}`, `{"text":"Bad","color":"#123456","colorToken":"unknown"}`} {
		d := tableWordThemeFixture(t)
		table := d.Objects.Tables["table-word-data"]
		if err := json.Unmarshal([]byte(raw), &table.Cells[0][1].Runs[0]); err != nil {
			t.Fatal(err)
		}
		if table.Validate() == nil {
			t.Fatal("invalid word role accepted", raw)
		}
	}
}

func TestTableWordThemeClipboardAndReset(t *testing.T) {
	source := tableWordThemeFixture(t)
	source.Theme.Colors.Accent[0] = "#ab3456"
	before := cloneDeck(source)
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, portable := range []bool{true, false} {
		dest := Deck{Appearance: &DeckAppearance{Version: 3}}
		if portable {
			dest.Theme = builtinDeckTheme("clean-light")
			dest.Theme.Colors.Accent[0] = "#12abcd"
		}
		elements, _, objects, err := prepareObjectClipboardPaste(dest, bundle)
		if err != nil {
			t.Fatal(err)
		}
		data := objects.Tables[elements[0].ObjectID]
		run := data.Cells[0][1].Runs[0]
		if run.Color != "#ab3456" || portable && run.ColorToken != "accent-1" || !portable && run.ColorToken != "" || !reflect.DeepEqual(run.Extra, before.Objects.Tables["table-word-data"].Cells[0][1].Runs[0].Extra) {
			t.Fatal("clipboard role/fallback or extension mismatch", run)
		}
		if portable {
			projected, err := dest.projectTable(data, sceneRect{Width: 1000, Height: 600}, "#ffffff")
			if err != nil || projected.Cells[1].Text.Runs[0].Color != "#12abcd" {
				t.Fatal("paste ignored destination palette", err)
			}
		}
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("clipboard mutated source")
	}
	reset, _, err := applyTableCellStyleData(source, source.Objects.Tables["table-word-data"], &tableCellRange{R0: 0, R1: 0, C0: 1, C1: 1}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	run := reset.Cells[0][1].Runs[0]
	if run.Color != "" || run.ColorToken != "" || !reflect.DeepEqual(run.Extra, before.Objects.Tables["table-word-data"].Cells[0][1].Runs[0].Extra) {
		t.Fatal("Reset left a hidden colour role or lost extension", run)
	}
}

func TestTableWordThemeHistoryAndEditableOutput(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := tableWordThemeFixture(t)
		s := newNativeEditorSession("Table words.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(d.Slides[0])
			s.deck.Slides[0].Elements = nil
		}
		before := cloneDeck(s.deck)
		version := s.version
		draft := cloneTable(d.Objects.Tables["table-word-data"])
		draft.Cells[0][1].Runs[0].ColorToken = "accent-2"
		if err := s.apply(nativeEditorAction{Action: "set-table", SceneMaster: master, SceneRevision: &version, ObjectID: "table-word", TableData: &draft}); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		if s.version != version+1 || len(s.undo) != 1 {
			t.Fatal("word role not one table transaction")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("word role undo mismatch", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("word role redo mismatch", err)
		}
	}
	d := tableWordThemeFixture(t)
	d.Theme.Colors.Accent[0] = "#ab3456"
	raw, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	frozen := false
	for _, warning := range report.Warnings {
		frozen = frozen || warning.Code == "word-theme-colours-frozen"
	}
	if !frozen {
		t.Fatal("word role loss undisclosed", report)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(raw), int64(len(raw)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slides) != 1 {
		t.Fatal("unexpected slide count", len(p.Slides))
	}
	var table *pptx.Table
	for _, object := range p.Slides[0].Objects {
		if object.Table != nil {
			if table != nil {
				t.Fatal("duplicate editable table")
			}
			table = object.Table
		}
	}
	if table == nil {
		t.Fatal("table was flattened or omitted")
	}
	runs := table.Cells[0][1].Runs
	if len(runs) != 2 || runs[0].Text != "Role" || !strings.EqualFold(runs[0].Color, "#ab3456") || !strings.EqualFold(runs[1].Color, "#ff55aa") {
		t.Fatal("PPTX editable colours not current", runs)
	}
	manifest, _, err := projectPDFExport("Table words.md", d, 245, 56)
	if err != nil || manifest.Pages[0].Page.Scene.Objects[0].Table.Cells[1].Text.Runs[0].Color != "#ab3456" {
		t.Fatal("PDF word role projection incorrect", err)
	}
}
