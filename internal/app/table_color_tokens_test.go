package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func tableTokenFixture(t *testing.T) Deck {
	d := portableMergedTableFixture(t)
	data := d.Objects.Tables["portable-data"]
	data.Style.Cell.Color = "theme:text"
	data.Style.Cell.BorderColor = "theme:line"
	data.Style.HeaderFill = "theme:accent-1"
	data.Style.BandFill = "theme:surface"
	data.Cells[1][1].Style.Fill = "theme:accent-2"
	data.Cells[1][1].Style.Color = "theme:heading"
	data.Cells[1][1].Style.Borders["bottom"] = testEdge("theme:accent-3", 3)
	data.Cells[2][1].Style = &TableCellStyle{Fill: "#aa5500", Color: "#ffffff", BorderColor: "none"}
	d.Objects.Tables["portable-data"] = data
	return d
}

func TestTableColorTokensValidateAndProjectWithoutBaking(t *testing.T) {
	d := tableTokenFixture(t)
	before := cloneDeck(d)
	check := func() {
		t.Helper()
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		table := scene.Objects[0].Table
		if table == nil {
			t.Fatal("missing table")
		}
		cell := func(r, c int) sceneTableCell {
			for _, cell := range table.Cells {
				if cell.Row == r && cell.Column == c {
					return cell
				}
			}
			t.Fatal("missing cell")
			return sceneTableCell{}
		}
		if cell(0, 0).Fill != d.Theme.Colors.Accent[0] || cell(1, 1).Fill != d.Theme.Colors.Accent[1] || cell(1, 1).Paint.Color != d.Theme.Colors.Heading {
			t.Fatal("table theme roles not resolved")
		}
		if cell(2, 1).Fill != "#aa5500" || cell(2, 1).Paint.Color != "#ffffff" || cell(2, 1).BorderColor != "none" {
			t.Fatal("palette changed explicit colours or Off")
		}
		for _, border := range table.Borders {
			if strings.HasPrefix(border.Color, "theme:") {
				t.Fatal("raw token leaked into scene border")
			}
		}
		found := false
		for _, border := range table.Borders {
			if border.Color == d.Theme.Colors.Accent[2] {
				found = true
			}
		}
		if !found {
			t.Fatal("per-edge colour role did not resolve")
		}
		if cell(1, 1).Text.Runs[1].Color != "#ff0055" {
			t.Fatal("cell theme changed explicit run colour")
		}
	}
	check()
	if !reflect.DeepEqual(d, before) {
		t.Fatal("projection baked tokens into authored data")
	}
	d.Theme.Colors.Accent[0], d.Theme.Colors.Accent[1], d.Theme.Colors.Accent[2] = "#0055aa", "#5500aa", "#aa0055"
	d.Theme.Colors.Heading = "#123456"
	check()
	if !reflect.DeepEqual(d.Objects, before.Objects) {
		t.Fatal("palette rewrote table payload")
	}
	for _, color := range []string{"theme:accent-7", "theme:Text", "theme:", "theme:evil()", "url(example)", "red"} {
		data := cloneTable(d.Objects.Tables["portable-data"])
		data.Cells[1][1].Style.Fill = color
		if data.Validate() == nil {
			t.Fatal("invalid semantic colour accepted", color)
		}
	}
}

func TestTableColorRolesHaveCompatibilityPaletteWithoutAuthoringTheme(t *testing.T) {
	data, err := newTableData(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	data.Style.Header = false
	data.Style.Banded = false
	deck := Deck{}
	for _, role := range []string{"text", "heading", "background", "surface", "muted", "line", "accent-1", "accent-2", "accent-3", "accent-4", "accent-5", "accent-6"} {
		data.Cells[0][0].Style = &TableCellStyle{Color: "theme:" + role, Fill: "theme:" + role, BorderColor: "theme:" + role}
		scene, err := deck.projectTable(data, sceneRect{Width: 400, Height: 100}, "#ffffff")
		if err != nil {
			t.Fatal(err)
		}
		want := deck.themeTokenColor(role)
		cell := scene.Cells[0]
		if cell.Paint.Color != want || cell.Fill != want || cell.BorderColor != want || deck.Theme != nil {
			t.Fatal("compatibility role failed", role, cell)
		}
	}
}

func TestTableColorTokensHistoryMasterOwnershipAndPortability(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := tableTokenFixture(t)
		s := newNativeEditorSession("Table.md", d)
		if master {
			s.masterMode = true
			s.currentMaster = 0
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "token-master-slide"
		}
		before := cloneDeck(s.deck)
		data := cloneTable(d.Objects.Tables["portable-data"])
		data.Cells[2][1].Style.Color = "theme:accent-4"
		version := s.version
		a := nativeEditorAction{Action: "set-table", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectID: "portable-table", TableData: &data}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("token undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("token redo failed", err)
		}
		file := filepath.Join(t.TempDir(), "Table.md")
		if err := saveDeck(file, s.deck); err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeck(file)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded.Objects, s.deck.Objects) {
			t.Fatal("portable role data lost")
		}
	}
	d := tableTokenFixture(t)
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(parsed.Slides) != 1 {
		t.Fatal("invalid token table PPTX", err)
	}
	var table *pptx.Table
	for _, object := range parsed.Slides[0].Objects {
		if object.Table != nil {
			table = object.Table
		}
	}
	if table == nil || !strings.EqualFold(table.Cells[1][1].Fill, d.Theme.Colors.Accent[1]) || !strings.EqualFold(table.Cells[1][1].Color, d.Theme.Colors.Heading) || !strings.EqualFold(table.Cells[1][1].Runs[1].Color, "#ff0055") {
		t.Fatal("editable PPTX did not receive resolved cell/run paint", table)
	}
	for _, row := range table.Cells {
		for _, cell := range row {
			for _, value := range []string{cell.Color, cell.Fill, cell.BorderColor} {
				if strings.HasPrefix(value, "theme:") {
					t.Fatal("semantic token leaked into PPTX paint")
				}
			}
		}
	}
}

func TestTableTokensCrossDeckClipboardFollowsDestinationPalette(t *testing.T) {
	source := tableTokenFixture(t)
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	dest := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "destination"}}}
	dest.Theme.Colors.Accent[1] = "#00aa55"
	s := newNativeEditorSession("Destination.md", dest)
	if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
		t.Fatal(err)
	}
	e := s.deck.Slides[0].Elements[0]
	table := s.deck.Objects.Tables[e.ObjectID]
	if table.Cells[1][1].Style.Fill != "theme:accent-2" {
		t.Fatal("paste flattened role")
	}
	scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.ID == e.ID {
			for _, cell := range o.Table.Cells {
				if cell.Row == 1 && cell.Column == 1 && cell.Fill != "#00aa55" {
					t.Fatal("pasted role did not follow destination palette", cell.Fill)
				}
			}
		}
	}
}

func TestTableColorTokenFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_TABLE_TOKEN_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	d := tableTokenFixture(t)
	if err := saveDeck(filepath.Join(root, "tokens.md"), d); err != nil {
		t.Fatal(err)
	}
}
