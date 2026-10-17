package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestTableDefaultsKeepCellsAndResolveInheritance(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	data := deck.Objects.Tables["cell-data"]
	before := cloneTable(data)
	set := func(property string, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		action.Property, action.PropertyValue, action.CellRange = property, encoded, nil
		data, _, err = setTableDefaultPropertyData(deck, data, action)
		if err != nil {
			t.Fatal(property, err)
		}
		deck.Objects.Tables["cell-data"] = data
	}
	set("table.fontId", "c64")
	set("table.fontSize", 120.0)
	set("table.fontWidth", 72.0)
	set("table.lineHeight", 1.6)
	set("table.colorToken", "heading")
	set("table.header", true)
	set("table.banded", true)
	set("table.headerFillToken", "accent-3")
	set("table.bandFillToken", "surface")
	set("table.borders.right.width", 2.5)
	set("table.borders.right.colorToken", "line")
	if !reflect.DeepEqual(before.Cells, data.Cells) || !reflect.DeepEqual(before.Rows, data.Rows) || !reflect.DeepEqual(before.Columns, data.Columns) {
		t.Fatal("editing defaults changed cell overrides/content/spans/tracks")
	}
	caps, err := tableCellStyleCapabilities(deck, action, "", 12)
	if err != nil || len(caps.Properties) != 58 || caps.Properties["table.fontWidth"].Value != float64(72) || caps.Properties["table.lineHeight"].Value != float64(1.6) || caps.Properties["table.fontSize"].Value != float64(120) || caps.Properties["table.headerFillToken"].Value != "accent-3" || caps.Properties["table.headerFillToken"].Provenance != "explicit" || !reflect.DeepEqual(caps.Contexts["table-defaults"], []string{"table-a"}) {
		t.Fatal("default capability state is wrong", caps, err)
	}
	projection, err := deck.projectTable(data, sceneRect{Width: 1000, Height: 600}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range projection.Cells {
		if cell.Row == 0 && cell.Column == 0 && (cell.Text.Size != 80 || cell.Text.FontID != "mono" || cell.Fill != deck.Theme.Colors.Accent[1]) {
			t.Fatal("default edit overrode explicit cell formatting", cell)
		}
		if cell.Row == 0 && cell.Column == 1 && (cell.Text.Size != 120 || cell.Text.FontID != "c64" || cell.Fill != deck.Theme.Colors.Accent[2]) {
			t.Fatal("header didn't inherit the new defaults", cell)
		}
		if cell.Row == 1 && cell.Column == 0 && (cell.Fill != deck.Theme.Colors.Surface || cell.Text.Size != 120 || cell.Text.Runs[0].Color != "#aabbcc") {
			t.Fatal("merged/banded cell or inline override lost", cell)
		}
	}
	set("table.headerFillToken", "none")
	if data.Style.HeaderFill != "none" {
		t.Fatal("header None was not explicit")
	}
	set("table.headerFillToken", "")
	if data.Style.HeaderFill != "" || !reflect.DeepEqual(before.Cells, data.Cells) {
		t.Fatal("header Inherit changed cell overrides")
	}
	md, err := serializeDeck("defaults.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("defaults.md", md)
	if err != nil || !reflect.DeepEqual(reopened.Objects.Tables["cell-data"], data) {
		t.Fatal("defaults didn't survive Markdown", err)
	}
}

func TestTableDefaultCommandsNormalMasterCOWHistoryAndValidation(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck, action := tableCellPropertyFixture(t)
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "other-table", Kind: "table", ObjectID: "cell-data"})
		s := newNativeEditorSession("Defaults.md", deck)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide.Elements = []Element{deck.Slides[0].Elements[0]}
		}
		before := cloneDeck(s.deck)
		version := s.version
		action.SceneMaster, action.SceneRevision, action.CellRange = master, &version, nil
		action.Property, action.PropertyValue = "table.fontSize", json.RawMessage(`140`)
		if err := s.apply(action); err != nil || s.version != version+1 || len(s.undo) != 1 || !reflect.DeepEqual(s.deck.Objects.Tables["cell-data"], before.Objects.Tables["cell-data"]) {
			t.Fatal("default command failed transaction/shared ownership", master, err)
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("Undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("Redo failed", err)
		}
		version = s.version
		action.Property = "table.header"
		for _, invalid := range []string{`null`, ` null `, `"true"`, `1`, `[]`, `{}`, `""`} {
			action.PropertyValue = json.RawMessage(invalid)
			if err := s.apply(action); err == nil || !reflect.DeepEqual(after, s.deck) || s.version != version {
				t.Fatal("invalid default flag partially applied", master, invalid, err)
			}
		}
	}
}

func TestTableDefaultIncrementAndLockedScope(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	action.Property, action.PropertyValue, action.CellRange = "table.fontSize", nil, nil
	delta := 1.0
	action.PropertyDelta = &delta
	before := cloneTable(deck.Objects.Tables["cell-data"])
	caps, err := tableCellStyleCapabilities(deck, action, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	base := caps.Properties["table.fontSize"].Value.(float64)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	data := deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if *data.Style.Cell.FontSize != base+1 || !reflect.DeepEqual(data.Cells, before.Cells) {
		t.Fatal("default increment edited actual cells")
	}
	deck.Slides[0].Elements[0].Query += "&object-locked=1"
	old := cloneDeck(deck)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err == nil || !reflect.DeepEqual(old, deck) {
		t.Fatal("locked defaults edited", err)
	}
}

func TestTableDefaultsEditablePPTXResolvedFormatting(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	data := deck.Objects.Tables["cell-data"]
	for property, value := range map[string]any{"table.fontSize": 64, "table.header": true, "table.banded": true, "table.headerFillToken": "accent-4", "table.bandFillToken": "surface", "table.borders.right.width": 2.5, "table.borders.right.colorToken": "line"} {
		action.Property = property
		action.PropertyValue, _ = json.Marshal(value)
		var err error
		data, _, err = setTableDefaultPropertyData(deck, data, action)
		if err != nil {
			t.Fatal(err)
		}
	}
	deck.Objects.Tables["cell-data"] = data
	encoded, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code != "table-border-segments-reader-dependent" && warning.Code != "object-extra-not-exported" {
			t.Fatal("unexpected default export warning", warning)
		}
	}
	read, _, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var table *pptx.Table
	for _, object := range read.Slides[0].Objects {
		if object.Kind == "table" {
			table = object.Table
		}
	}
	if table == nil || !table.Header || !table.Banded || table.Cells[0][1].FontSize != 32 || table.Cells[0][0].FontSize != 40 || table.Cells[1][0].ColSpan != 2 {
		t.Fatal("editable defaults/overrides/spans were not exported", table)
	}
	if !strings.EqualFold(table.Cells[0][1].Fill, deck.Theme.Colors.Accent[3]) || !strings.EqualFold(table.Cells[1][0].Fill, deck.Theme.Colors.Surface) {
		t.Fatal("default header/band role was not resolved", table.Cells)
	}
}
