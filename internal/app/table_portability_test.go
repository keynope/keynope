package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func portableMergedTableFixture(t *testing.T) Deck {
	t.Helper()
	data, err := newTableData(3, 3)
	if err != nil {
		t.Fatal(err)
	}
	data.Columns = []TableColumn{{Width: 2}, {Width: 3}, {Width: 2}}
	data.Cells[0][0] = TableCell{ColSpan: 3, Runs: []ObjectTextRun{
		{Text: "Workshop ", Bold: true}, {Text: "plan", FontID: "c64", FontSize: 64, Color: "#55aaff"},
	}}
	data.Cells[0][1].Hidden, data.Cells[0][2].Hidden = true, true
	data.Cells[1][0] = TableCell{RowSpan: 2, Text: "Shared owner"}
	data.Cells[2][0].Hidden = true
	data.Cells[1][1].Runs = []ObjectTextRun{{Text: "Discuss ", FontID: "mono", FontSize: 50}, {Text: "together", Italic: true, Underline: true, Color: "#ff0055"}}
	data.Cells[1][2].Text, data.Cells[2][1].Text, data.Cells[2][2].Text = "10 min", "Build", "20 min"
	size, padding := 48.0, 8.0
	data.Style.Cell = TableCellStyle{FontID: "mono", FontSize: &size, Padding: &padding, Color: "#ffffff", BorderColor: "#55aaff", Align: "center", Vertical: "middle"}
	data.Style.Cell.Borders = map[string]TableBorderStyle{"left": testEdge("#8899aa", 1)}
	data.Cells[1][1].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"bottom": testEdge("#ff0055", 3), "right": testEdge("none", 0)}}
	data.Style.HeaderFill, data.Style.BandFill = "#163c62", "#243044"
	if err := data.Validate(); err != nil {
		t.Fatal(err)
	}
	theme := builtinDeckTheme("clean-dark")
	theme.Fonts.Body.FontID = "mono"
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: theme, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"portable-data": data}}, Slides: []Slide{{ID: "portable-slide", Elements: []Element{{ID: "portable-table", Kind: "table", ObjectID: "portable-data", Query: "left=24&top=8&width=180&height=32"}}}}}
}

func TestMergedTableCrossDeckClipboardHistory(t *testing.T) {
	source := portableMergedTableFixture(t)
	want := cloneTable(source.Objects.Tables["portable-data"])
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var wire ObjectClipboard
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			existing, _ := newTableData(1, 1)
			existing.Cells[0][0].Text = "Destination preserved"
			destination := Deck{Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"portable-data": existing}}, Slides: []Slide{{Elements: []Element{{ID: "portable-table", Kind: "table", ObjectID: "portable-data", Query: "width=100&height=10"}}}}}
			s := newNativeEditorSession("destination.md", destination, false, false)
			if master {
				if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
					t.Fatal(err)
				}
			}
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &wire}); err != nil {
				t.Fatal(err)
			}
			target := s.deck.Slides[0]
			if master {
				target = *masterSlideAt(&s.deck, s.currentMaster)
			}
			pasted := target.Elements[len(target.Elements)-1]
			if pasted.ID == "portable-table" || pasted.ObjectID == "portable-data" {
				t.Fatal("paste did not remap colliding identities")
			}
			if !reflect.DeepEqual(s.deck.Objects.Tables[pasted.ObjectID], want) || !reflect.DeepEqual(s.deck.Objects.Tables["portable-data"], existing) {
				t.Fatal("paste changed merged cells, styles or destination")
			}
			after := cloneDeck(s.deck)
			if len(s.undo) != 1 {
				t.Fatal("paste was not one transaction")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck, before) {
				t.Fatal("undo did not restore collision destination")
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck, after) {
				t.Fatal("redo changed payload identity/data")
			}
			md, err := serializeDeck("destination.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("destination.md", md)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reopened.Objects.Tables[pasted.ObjectID], want) {
				t.Fatal("saved copy lost merged rich cells")
			}
			copy := s.deck.Objects.Tables[pasted.ObjectID]
			copy.Cells[0][0].Runs[0].Text = "Changed copy"
			if source.Objects.Tables["portable-data"].Cells[0][0].Runs[0].Text != "Workshop " || wire.Objects.Tables["portable-data"].Cells[0][0].Runs[0].Text != "Workshop " {
				t.Fatal("pasted runs alias source/clipboard")
			}
		})
	}
}

func TestMergedTableParticipantAndHTMLProjection(t *testing.T) {
	deck := portableMergedTableFixture(t)
	deck.Slides[0].Notes = "PRIVATE NOTES"
	deck.Slides[0].Engagement = &EngagementDefinition{ID: "private-activity", Kind: "storm", Prompt: "PRIVATE PROMPT"}
	extra := &jsonExtensions{"private": json.RawMessage(`"PRIVATE EXTENSION"`)}
	data := deck.Objects.Tables["portable-data"]
	data.Style.Cell.FontID = "" // Exercise inherited body typography in the public adapter.
	data.Extra, data.Style.Extra, data.Style.Cell.Extra = extra, extra, extra
	border := data.Style.Cell.Borders["left"]
	border.Extra = extra
	data.Style.Cell.Borders["left"] = border
	border = data.Cells[1][1].Style.Borders["bottom"]
	border.Extra = extra
	data.Cells[1][1].Style.Borders["bottom"] = border
	data.Rows[0].Extra, data.Columns[0].Extra = extra, extra
	data.Cells[0][0].Extra, data.Cells[0][0].Runs[0].Extra = extra, extra
	deck.Objects.Tables["portable-data"] = data
	unused, _ := newTableData(1, 1)
	unused.Cells[0][0].Text = "PRIVATE UNUSED TABLE"
	deck.Objects.Tables["unused"] = unused
	deck.Objects.Builds = map[string]SlideBuild{"portable-slide": {Version: 1, Steps: []BuildStep{{TargetIDs: []string{"portable-table"}, Effect: "appear"}}}}
	deck.Theme.Extra, deck.Theme.Fonts.Body.Extra = extra, extra
	before, _ := json.Marshal(deck)
	local, err := buildDocumentSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := participantRenderedDeck(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*participant.Pages[0].Scene, local) {
		t.Fatal("participant merged table diverges from local scene")
	}
	_, presenter, err := projectPresenterDocument(filepath.Join(t.TempDir(), "table.md"), deck.ResolvedSlides(), 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	exported := projectExportDeck("table.md", deck.ResolvedSlides(), 245, 56, false)
	if len(presenter) != 1 || len(presenter[0]) != 1 || len(exported.Pages) != 1 || !reflect.DeepEqual(presenter[0][0].Scene, &local) || !reflect.DeepEqual(exported.Pages[0].Scene, &local) {
		t.Fatal("HTML/presenter table layout differs")
	}
	md, err := participantSlideMarkdown(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Presentation.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Objects == nil || len(reopened.Objects.Tables) != 1 || len(reopened.Objects.Builds) != 0 {
		t.Fatal("audience archive lost table or leaked other payloads")
	}
	for _, public := range []any{reopened, participant, exported} {
		encoded, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("PRIVATE")) {
			t.Fatal("participant/output leaked private authoring data", string(encoded))
		}
	}
	rebuilt, err := buildDocumentSlideScene(reopened, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt.Objects) != 1 || len(local.Objects) != 1 {
		t.Fatalf("missing table after public Markdown: local=%+v scene=%+v", local, rebuilt)
	}
	if !reflect.DeepEqual(rebuilt.Objects[0].Table, local.Objects[0].Table) {
		t.Fatal("audience Markdown changed theme-dependent table typography/layout")
	}
	after, _ := json.Marshal(deck)
	if !bytes.Equal(before, after) {
		t.Fatal("audience projection mutated authored deck")
	}
}

func TestParticipantStructuredArchiveIsScopedAndOwned(t *testing.T) {
	deck := structuredFixture()
	deck.Appearance = &DeckAppearance{Version: 3}
	deck.Slides = append(deck.Slides, Slide{Elements: []Element{{ID: "public-chart", Kind: "chart", ObjectID: "chart-1", Query: "width=100&height=20"}}})
	extra := &jsonExtensions{"private": json.RawMessage(`"PRIVATE EXTENSION"`)}
	chart := deck.Objects.Charts["chart-1"]
	chart.Extra, chart.Options.Extra, chart.Series[0].Extra = extra, extra, extra
	deck.Objects.Charts["chart-1"] = chart
	deck.Objects.Extra = extra
	deck.Theme = builtinDeckTheme("clean-light")
	deck.Theme.Name = "PRIVATE THEME NAME"
	deck.Theme.Extra, deck.Theme.Colors.Extra, deck.Theme.Fonts.Extra, deck.Theme.Defaults.Extra = extra, extra, extra, extra
	deck.Theme.Fonts.Heading.Extra, deck.Theme.Fonts.Body.Extra, deck.Theme.Fonts.Mono.Extra = extra, extra, extra
	before, _ := json.Marshal(deck)
	s := newNativeEditorSession("author.md", deck, false, false)
	w := httptest.NewRecorder()
	s.handleParticipantPage(w, httptest.NewRequest("GET", "/api/editor/participant-page?slide=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	public, err := parseDeckData("Presentation.md", []byte(response.Markdown))
	if err != nil {
		t.Fatal(err)
	}
	if public.Objects == nil || len(public.Objects.Charts) != 1 || len(public.Objects.Tables) != 0 || len(public.Objects.Builds) != 0 {
		t.Fatal("participant included an unrelated table/timeline")
	}
	encoded, _ := json.Marshal(public)
	if bytes.Contains(encoded, []byte("PRIVATE")) {
		t.Fatal("nested authoring extensions or theme name disclosed")
	}
	got := public.Objects.Charts["chart-1"]
	want := cloneChart(chart)
	want.Extra, want.Options.Extra, want.Series[0].Extra = nil, nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatal("known chart data changed during privacy projection")
	}
	got.Series[0].Values[0] = 999
	if deck.Objects.Charts["chart-1"].Series[0].Values[0] != 1 {
		t.Fatal("public chart shares authored values")
	}
	public.Theme.Colors.Accent[0] = "#ff0000"
	after, _ := json.Marshal(deck)
	if !bytes.Equal(before, after) {
		t.Fatal("projection shares theme/metadata storage")
	}
	deck.Slides[1].Elements[0].ObjectID = "missing"
	if _, err := participantSlideMarkdown(deck, 1); err == nil {
		t.Fatal("missing payload was silently dropped")
	}
}
