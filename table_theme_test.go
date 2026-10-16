package main

import (
	"math"
	"reflect"
	"testing"
)

func TestTableHeaderContrastAcrossThemeAccents(t *testing.T) {
	if math.Abs(tableColorContrast("#ffffff", "#000000")-21) > 1e-10 || tableColorContrast("#713fd0", "#713fd0") != 1 {
		t.Fatal("sRGB contrast calculation is incorrect")
	}
	for _, theme := range builtinDeckThemes() {
		for _, accent := range theme.Colors.Accent {
			ink := tableHeaderColor(theme, accent)
			if tableColorContrast(ink, accent) < 4.5 {
				t.Fatalf("%s header contrast is insufficient for %s on %s", theme.Name, ink, accent)
			}
		}
	}
}

func TestTableThemeInheritanceAndOverrides(t *testing.T) {
	table, _ := newTableData(3, 2)
	table.Cells[0][0].Text = "Header"
	table.Cells[1][0].Text = "Inherited"
	size := 51.0
	table.Cells[1][1] = TableCell{Text: "Explicit", Style: &TableCellStyle{Fill: "none", Color: "#55aaff", BorderColor: "none", FontID: "c64", FontSize: &size}}
	original := cloneTable(table)
	for _, name := range []string{"clean-light", "clean-dark", "workshop"} {
		t.Run(name, func(t *testing.T) {
			theme := builtinDeckTheme(name)
			theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 43, Width: 81, LineHeight: 1.4}
			deck := Deck{Theme: theme}
			projection, err := deck.projectTable(table, sceneRect{Width: 600, Height: 300}, "")
			if err != nil {
				t.Fatal(err)
			}
			header, body, explicit := projection.Cells[0], projection.Cells[2], projection.Cells[3]
			if header.Fill != theme.Colors.Accent[0] || body.Fill != theme.Colors.Surface || projection.Cells[4].Fill != theme.Colors.Background {
				t.Fatal("inherited fills do not follow theme tokens", header.Fill, body.Fill)
			}
			if body.Paint.Color != theme.Colors.Text || body.BorderColor != theme.Colors.Line || body.Text.Size != 43 || body.Text.FontID != "mono" || body.Text.LineHeight != 1.4 {
				t.Fatal("inherited typography/paint does not follow theme", body)
			}
			if tableColorContrast(header.Paint.Color, header.Fill) < 4.5 {
				t.Fatal("header text is not readable", header.Paint.Color, header.Fill)
			}
			if explicit.Fill != "none" || explicit.Paint.Color != "#55aaff" || explicit.BorderColor != "none" || explicit.Text.FontID != "c64" || explicit.Text.Size != 51 {
				t.Fatal("theme overwrote explicit cell styling", explicit)
			}
			if !reflect.DeepEqual(table, original) {
				t.Fatal("projection materialized inherited values into authored table")
			}
		})
	}
}

func TestTableExplicitBaseFillSurvivesHeaderAndBandDefaults(t *testing.T) {
	table, _ := newTableData(3, 1)
	table.Style.Cell.Fill, table.Style.Cell.Color = "#123456", "#ff0055"
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	projection, err := deck.projectTable(table, sceneRect{Width: 400, Height: 300}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range projection.Cells {
		if cell.Fill != "#123456" || cell.Paint.Color != "#ff0055" {
			t.Fatal("default header/banding replaced explicit table style", cell)
		}
	}
	table.Style.HeaderFill, table.Style.BandFill = "#567890", "#098765"
	projection, _ = deck.projectTable(table, sceneRect{Width: 400, Height: 300}, "#112233")
	if projection.Cells[0].Fill != "#567890" || projection.Cells[1].Fill != "#098765" {
		t.Fatal("explicit header/band fill not honored")
	}
	table.Style.Cell.Color = ""
	projection, _ = deck.projectTable(table, sceneRect{Width: 400, Height: 300}, "#112233")
	for _, cell := range projection.Cells {
		if cell.Paint.Color != "#112233" {
			t.Fatal("theme replaced explicit owner colour")
		}
	}
}

func TestTableThemeSceneHistoryAndPortableOutputs(t *testing.T) {
	deck := portableMergedTableFixture(t)
	data := deck.Objects.Tables["portable-data"]
	data.Style = TableStyle{Header: true, Banded: true}
	data.Cells[1][2].Style = &TableCellStyle{Color: "#ff0055", Fill: "none"}
	deck.Objects.Tables["portable-data"] = data
	s := newNativeEditorSession("themed-tables.md", deck, false, false)
	snapshot := func() *sceneTable {
		t.Helper()
		scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range scene.Objects {
			if object.Table != nil {
				return object.Table
			}
		}
		t.Fatal("table not projected")
		return nil
	}
	before := snapshot()
	theme := builtinDeckTheme("clean-light")
	theme.Fonts.Body.Size = 41
	theme.Colors.Accent[0] = "#713fd0"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before.Cells[0].Fill == after.Cells[0].Fill || after.Cells[0].Fill != theme.Colors.Accent[0] || after.Cells[1].Text.Size != 41 {
		t.Fatal("production scene did not update inherited table style", before, after)
	}
	if after.Cells[3].Paint.Color != "#ff0055" || after.Cells[3].Fill != "none" {
		t.Fatal("production scene lost explicit cell styling")
	}
	if !reflect.DeepEqual(s.deck.Objects.Tables["portable-data"], data) {
		t.Fatal("theme command rewrote table metadata")
	}
	if len(s.undo) != 1 {
		t.Fatal("theme edit did not commit exactly once")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(), before) {
		t.Fatal("undo did not restore inherited styles")
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(), after) {
		t.Fatal("redo did not restore inherited styles")
	}
	md, err := serializeDeck("themed-tables.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("themed-tables.md", md)
	if err != nil {
		t.Fatal(err)
	}
	public, err := participantSlideMarkdown(s.deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := parseDeckData("participant.md", public)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []Deck{loaded, participant} {
		scene, err := buildDocumentSlideScene(output, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(scene.Objects[0].Table, after) {
			t.Fatal("portable output changed theme resolution")
		}
	}
	native := sceneTableToPPTX(after)
	if native.Cells[0][0].Fill != after.Cells[0].Fill || native.Cells[1][0].Color != theme.Colors.Text || native.Cells[1][0].BorderColor != theme.Colors.Line {
		t.Fatal("editable PPTX did not receive resolved theme styles")
	}
	// Explicit owner/master colours are provenance, even when their bytes
	// equal the theme's default text colour. Header auto-contrast must not win.
	s.deck.Slides[0].FG = themeANSI(theme.Colors.Text, false)
	if snapshot().Cells[0].Paint.Color != theme.Colors.Text {
		t.Fatal("explicit slide foreground was overridden")
	}
}
