package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func restrictedTypefaceDeck(t *testing.T) (Deck, string) {
	t.Helper()
	deck := typefaceDeck(t)
	data := bytes.Clone(gomono.TTF)
	binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], 2)
	face := testTypeface(t, data)
	deck.Typefaces = map[string]DeckTypeface{face.ID(): face}
	deck.Slides[0].Elements[0].Query = setQueryValue(deck.Slides[0].Elements[0].Query, "modern-font", face.ID())
	return deck, face.ID()
}

func TestFontExportApprovalAndSnapshotIsolation(t *testing.T) {
	deck, id := restrictedTypefaceDeck(t)
	deck.Theme = builtinDeckThemes()[0].Clone()
	deck.Theme.Fonts.Body.FontID = id
	deck.Theme.Fonts.Heading.FontID = id
	deck.Theme.Fonts.Mono.FontID = id
	s := newNativeEditorSession("Fonts.md", deck)
	post := func(decision fontExportApproval) int {
		t.Helper()
		data, _ := json.Marshal(decision)
		w := httptest.NewRecorder()
		s.handleFontExport(w, httptest.NewRequest("POST", "/api/editor/font-export", bytes.NewReader(data)))
		return w.Code
	}
	decision := fontExportApproval{Owner: s.notesSessionID, Version: s.version, Format: "pptx"}
	if _, _, err := s.deckForFontExport("pptx"); err == nil {
		t.Fatal("unconfirmed export accepted")
	}
	if post(decision) != 400 {
		t.Fatal("missing rights decision accepted")
	}
	decision.Owner = "wrong"
	decision.Confirmed = true
	if post(decision) != 409 {
		t.Fatal("stale owner accepted")
	}
	decision.Owner = s.notesSessionID
	if post(decision) != 200 {
		t.Fatal("rights confirmation rejected")
	}
	allowed, _, err := s.deckForFontExport("pptx")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(allowed.Typefaces[id].Data, s.deck.Typefaces[id].Data) {
		t.Fatal("rights confirmation changed original flags")
	}
	if _, _, err := s.deckForFontExport("pptx"); err == nil {
		t.Fatal("approval was not consumed")
	}
	decision.Confirmed = false
	decision.Format = "html"
	decision.Replacements = map[string]string{id: "sans"}
	if post(decision) != 200 {
		t.Fatal("replacement rejected")
	}
	replaced, _, err := s.deckForFontExport("html")
	if err != nil {
		t.Fatal(err)
	}
	if mustQuery(replaced.Slides[0].Elements[0].Query).Get("modern-font") != "sans" {
		t.Fatal("replacement not applied")
	}
	if !strings.Contains(s.deck.Slides[0].Elements[0].Query, id) {
		t.Fatal("export changed authored deck")
	}
	if replaced.Theme.Fonts.Body.FontID != "sans" || s.deck.Theme.Fonts.Body.FontID != id {
		t.Fatal("theme replacement was not isolated to export")
	}
	if fonts, err := restrictedExportFonts(replaced, "html"); err != nil || len(fonts) > 0 {
		t.Fatal("replacement still uses restricted font", fonts, err)
	}
	decision.Replacements = map[string]string{id: id}
	if post(decision) != 400 {
		t.Fatal("restricted replacement silently accepted")
	}
	decision.Replacements = nil
	decision.Confirmed = true
	decision.Format = "pdf"
	if post(decision) != 200 {
		t.Fatal("confirmation rejected")
	}
	s.version++
	if _, _, err := s.deckForFontExport("pdf"); err == nil {
		t.Fatal("stale revision exported")
	}
}

func TestFontExportListsOnlyUsedFonts(t *testing.T) {
	deck, id := restrictedTypefaceDeck(t)
	data := bytes.Clone(goregular.TTF)
	binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], 4)
	unused := testTypeface(t, data)
	deck.Typefaces[unused.ID()] = unused
	fonts, err := restrictedExportFonts(deck, "html")
	if err != nil || len(fonts) != 1 || fonts[0].ID != id {
		t.Fatal("unused font in warning", fonts, err)
	}
	deck.Slides[0].TabID = "private"
	for _, format := range []string{"pdf", "pptx", "png"} {
		if fonts, err := restrictedExportFonts(deck, format); err != nil || len(fonts) != 0 {
			t.Fatal("excluded tab/raster font in warning", format, fonts, err)
		}
	}
	if fonts, err := restrictedExportFonts(deck, "html"); err != nil || len(fonts) != 1 {
		t.Fatal("HTML tab omitted", fonts, err)
	}
}

func TestFontExportReplacementAcrossBindings(t *testing.T) {
	deck, id := restrictedTypefaceDeck(t)
	rich := withConvertedRuns(Element{ID: "rich", Kind: "text", Text: "Rich text", Query: "modern-font=" + id}, []sceneRun{{Text: "Rich text", FontID: id}})
	label, _ := json.Marshal(shapeLabelData{Text: "Label", Query: rich.Query})
	shape := Element{ID: "shape", Kind: "shape", Query: "shape=square&label-modern-font=" + id + "&shape-label=" + base64.StdEncoding.EncodeToString(label)}
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, rich, shape)
	deck.Masters.Base.Slide.Elements = []Element{rich}
	deck.Masters.Layouts = []MasterLayout{{ID: "layout", Name: "Layout", Slide: Slide{Elements: []Element{shape}}}}
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"table": {Style: TableStyle{Cell: TableCellStyle{FontID: id}}, Cells: [][]TableCell{{{Style: &TableCellStyle{FontID: id}, Runs: []ObjectTextRun{{Text: "Cell", FontID: id}}}}}}}, Charts: map[string]ChartData{"chart": {Options: ChartOptions{FontID: id, DefaultFontID: id}}}}
	before, _ := json.Marshal(deck)
	exported := cloneDeckForRender(deck)
	replaceExportTypefaces(&exported, map[string]string{id: "mono"})
	check := func(e Element) {
		t.Helper()
		q := mustQuery(e.Query)
		if strings.Contains(q.Get("modern-font"), id) || strings.Contains(q.Get("label-modern-font"), id) {
			t.Fatal("unreplaced query")
		}
		if styles := decodeModernRuns(q.Get("modern-runs")); styles != nil {
			for _, run := range styles.Runs {
				if run.FontID == id {
					t.Fatal("unreplaced rich run")
				}
			}
		}
		if label, ok := shapeLabel(e); ok && strings.Contains(label.Query, id) {
			t.Fatal("unreplaced label")
		}
	}
	for _, e := range exported.Slides[0].Elements {
		check(e)
	}
	check(exported.Masters.Base.Slide.Elements[0])
	check(exported.Masters.Layouts[0].Slide.Elements[0])
	table := exported.Objects.Tables["table"]
	if table.Style.Cell.FontID != "mono" || table.Cells[0][0].Style.FontID != "mono" || table.Cells[0][0].Runs[0].FontID != "mono" || exported.Objects.Charts["chart"].Options.DefaultFontID != "mono" {
		t.Fatal("unreplaced structured data")
	}
	after, _ := json.Marshal(deck)
	if !bytes.Equal(before, after) {
		t.Fatal("snapshot replacement changed source")
	}
}
