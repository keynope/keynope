package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func cellStyleTransferFixture(t *testing.T) (Deck, TableData) {
	t.Helper()
	table, err := newTableData(3, 3)
	if err != nil {
		t.Fatal(err)
	}
	fontSize, width, off := 80.0, 3.25, 0.0
	table.Cells[0][0] = TableCell{Text: "Source private text", Style: &TableCellStyle{FontID: "mono", FontSize: &fontSize, Fill: "theme:accent-2", Color: "#123456", Borders: map[string]TableBorderStyle{"left": {Color: "theme:accent-3", Width: &width}, "bottom": {Color: "none", Width: &off}}, Extra: &jsonExtensions{"future": json.RawMessage(`{"preserve":true}`)}}}
	table.Cells[1][0] = TableCell{Runs: []ObjectTextRun{{Text: "Target one", Color: "#aabbcc", Extra: &jsonExtensions{"future": json.RawMessage(`true`)}}}, ColSpan: 2, Extra: &jsonExtensions{"cell-extra": json.RawMessage(`42`)}}
	table.Cells[1][1] = TableCell{Hidden: true}
	table.Cells[2][2].Text = "Unselected"
	return Deck{Theme: builtinDeckTheme("clean-light")}, table
}

func TestTableCellStyleBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_CELL_STYLE_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	deck, table := cellStyleTransferFixture(t)
	deck.Appearance = &DeckAppearance{Version: 3}
	table.Cells[0][0].Text = "Source"
	table.Style.Header = false
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
	deck.Slides = []Slide{{ID: "cell-styles", Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data", Query: "left=16&top=8&width=205&height=38"}}}}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "cells.md"), deck); err != nil {
		t.Fatal(err)
	}
	recipient, _ := newTableData(3, 3)
	recipient.Style.Header = false
	recipient.Cells[2][2].Text = "Recipient text"
	deck.Theme = nil
	deck.Objects.Tables["cell-data"] = recipient
	if err := saveDeck(filepath.Join(root, "recipient.md"), deck); err != nil {
		t.Fatal(err)
	}
}

func TestTableCellStyleCommandTransactionsAndMasterCopyOnWrite(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck, table := cellStyleTransferFixture(t)
		deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
		owner := Element{ID: "table-a", Kind: "table", ObjectID: "cell-data", Query: "left=10&top=10&width=160&height=30"}
		other := owner
		other.ID = "table-b"
		deck.Slides = []Slide{{ID: "cells", Elements: []Element{owner, other}}}
		s := newNativeEditorSession("Cell styles.md", deck)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = Slide{ID: "master-cells", Elements: []Element{owner}}
		}
		before := cloneDeck(s.deck)
		version := s.version
		command := nativeEditorAction{Action: "copy-object-style", StyleContext: "table-cells", SceneMaster: master, SceneRevision: &version, Slide: 0, ObjectID: "table-a", CellRange: &tableCellRange{}}
		if err := s.apply(command); err != nil || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("cell copy changed history/document", err)
		}
		command.Action, command.StyleData = "paste-object-style", s.transportState().StyleClipboard
		command.CellRange = &tableCellRange{R0: 1, C0: 0, R1: 1, C1: 1}
		if err := s.apply(command); err != nil || s.version != version+1 || len(s.undo) != 1 {
			t.Fatal("cell paste not one atomic transaction", err)
		}
		target := s.deck.Slides[0].Elements[0]
		original := before.Slides[0].Elements[0]
		if master {
			target = s.deck.Masters.Base.Slide.Elements[0]
			original = before.Masters.Base.Slide.Elements[0]
		}
		if target.ObjectID == "cell-data" || target.Query != original.Query || !reflect.DeepEqual(s.deck.Objects.Tables["cell-data"], before.Objects.Tables["cell-data"]) {
			t.Fatal("cell paste broke shared payload/geometry ownership")
		}
		after := cloneDeck(s.deck)
		version = s.version
		if err := s.apply(command); err != nil || s.version != version || len(s.undo) != 1 {
			t.Fatal("repeat cell paste was not a no-op", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("cell Undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("cell Redo failed", err)
		}
		version = s.version
		command.CellRange.R1 = 100
		if err := s.apply(command); err == nil || !reflect.DeepEqual(after, s.deck) || s.version != version {
			t.Fatal("invalid cell range partially committed")
		}
	}
}

func TestTableCellStyleDraftDoesNotCommitAndRejectsStaleScope(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
	deck.Slides = []Slide{{ID: "cells", Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data"}}}}
	s := newNativeEditorSession("Draft cells.md", deck)
	version := s.version
	action := nativeEditorAction{Action: "copy-object-style", StyleContext: "table-cells", SceneRevision: &version, Slide: 0, ObjectID: "table-a", CellRange: &tableCellRange{}, TableData: &table}
	call := func(action nativeEditorAction) *httptest.ResponseRecorder {
		body, _ := json.Marshal(action)
		response := httptest.NewRecorder()
		s.handleTableCellStyleDraft(response, httptest.NewRequest(http.MethodPost, "/api/editor/table-cell-style-draft", bytes.NewReader(body)))
		return response
	}
	before := cloneDeck(s.deck)
	response := call(action)
	var copied struct {
		Clipboard *ObjectStyleSnapshot `json:"styleClipboard"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &copied) != nil || copied.Clipboard == nil {
		t.Fatal("draft copy failed", response.Code, response.Body.String())
	}
	action.Action, action.StyleData, action.CellRange = "paste-object-style", copied.Clipboard, &tableCellRange{R0: 1, C0: 0, R1: 1, C1: 1}
	response = call(action)
	var styled struct {
		Data TableData `json:"tableData"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &styled) != nil || styled.Data.Cells[1][0].Style.Fill != "theme:accent-2" || !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != 0 {
		t.Fatal("draft paste failed or committed document", response.Code, response.Body.String())
	}
	stale := version - 1
	action.SceneRevision = &stale
	if response = call(action); response.Code != http.StatusConflict || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("stale cell draft accepted")
	}
}

func TestTableCellStyleCapabilitiesAndScope(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
	deck.Slides = []Slide{{Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data"}}}}
	action := nativeEditorAction{Action: "paste-object-style", StyleContext: "table-cells", ObjectID: "table-a", CellRange: &tableCellRange{R0: 1, R1: 1, C0: 1, C1: 1}}
	state, err := tableCellStyleCapabilities(deck, action, "", 7)
	if err != nil || state.Context != "table-cells" || state.Revision != 7 || state.CellCount != 1 || !state.Commands["style.copy"].Enabled || !reflect.DeepEqual(state.TargetIDs, []string{"table-a"}) {
		t.Fatal("merged-cell capability lost owner/range", state, err)
	}
	action.CellRange = &tableCellRange{R0: 0, R1: 2, C0: 0, C1: 2}
	state, err = tableCellStyleCapabilities(deck, action, "", 7)
	if err != nil || state.CellCount != 8 || state.Commands["style.copy"].Enabled || !state.Commands["style.reset"].Enabled {
		t.Fatal("range capability incorrect", state, err)
	}
	deck.Slides[0].Elements[0].Query = "object-locked=1"
	action.CellRange = &tableCellRange{}
	state, err = tableCellStyleCapabilities(deck, action, "", 7)
	if err != nil || !state.Commands["style.copy"].Enabled || state.Commands["style.paste"].Enabled || state.Commands["style.reset"].Enabled {
		t.Fatal("lock capability incorrect", state, err)
	}
	deck.Slides[0].Elements[0].Query = "group=cell-group"
	state, err = tableCellStyleCapabilities(deck, action, "", 7)
	if err != nil || state.Commands["style.copy"].Enabled || state.Commands["style.paste"].Enabled {
		t.Fatal("closed-group cell capability enabled", state, err)
	}
	action.StyleData, err = captureTableCellStyle(deck, table, &tableCellRange{})
	if err != nil {
		t.Fatal(err)
	}
	action.CellRange = &tableCellRange{R0: 2, R1: 2, C0: 2, C1: 2}
	before := cloneDeck(deck)
	if changed, err := applyTableCellStyleCommand(&deck, action, ""); err == nil || changed || !reflect.DeepEqual(before, deck) {
		t.Fatal("cell paste escaped a closed group", err)
	}
	if changed, err := applyTableCellStyleCommand(&deck, action, "cell-group"); err != nil || !changed {
		t.Fatal("entered child could not paste", err)
	}
}

func TestTableCellStyleDraftCapabilityAndCancellation(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
	deck.Slides = []Slide{{Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data", Query: "object-locked=1"}}}}
	s := newNativeEditorSession("Cells.md", deck)
	version := s.version
	action := nativeEditorAction{Action: "cell-style-capabilities", StyleContext: "table-cells", ObjectID: "table-a", SceneRevision: &version, TableData: &table, CellRange: &tableCellRange{}}
	call := func(ctx context.Context) *httptest.ResponseRecorder {
		body, _ := json.Marshal(action)
		response := httptest.NewRecorder()
		s.handleTableCellStyleDraft(response, httptest.NewRequest(http.MethodPost, "/api/editor/table-cell-style-draft", bytes.NewReader(body)).WithContext(ctx))
		return response
	}
	response := call(context.Background())
	var result struct {
		Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Capabilities == nil || result.Capabilities.Commands["style.paste"].Enabled || !result.Capabilities.Commands["style.copy"].Enabled {
		t.Fatal("missing read-only capability response", response.Code, response.Body.String())
	}
	before := cloneDeck(s.deck)
	action.Action = "copy-object-style"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	call(ctx)
	if s.styleClipboard != nil || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("cancelled draft copy changed session")
	}
	action.Action = "reset-object-style"
	if response = call(context.Background()); response.Code != 400 || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("locked reset accepted", response.Code)
	}
	// The draft may have different merge topology from its committed source.
	// Capabilities must use that draft, rather than selecting phantom anchors.
	action.Action = "cell-style-capabilities"
	merge := deck.Objects.Clone().Tables["cell-data"]
	merge.Cells[1][0].ColSpan = 3
	merge.Cells[1][2] = TableCell{Hidden: true}
	action.TableData, action.CellRange = &merge, &tableCellRange{R0: 1, R1: 1, C0: 0, C1: 2}
	response = call(context.Background())
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Capabilities.CellCount != 1 || !result.Capabilities.Commands["style.copy"].Enabled {
		t.Fatal("draft topology was ignored", response.Body.String())
	}
}

func TestTableCellStylePersistenceProjectionAndEditablePPTX(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	style, err := captureTableCellStyle(deck, table, &tableCellRange{})
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := applyTableCellStyleData(deck, table, &tableCellRange{R0: 2, R1: 2, C0: 2, C1: 2}, style, false)
	if err != nil {
		t.Fatal(err)
	}
	deck.Appearance = &DeckAppearance{Version: 3}
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": data}}
	deck.Slides = []Slide{{ID: "cell-slide", Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data", Query: "left=16&top=8&width=205&height=38"}}}}
	md, err := serializeDeck("cells.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("cells.md", md)
	if err != nil || !reflect.DeepEqual(reopened.Objects.Tables["cell-data"], data) {
		t.Fatal("cell formatting did not survive Markdown", err)
	}
	if strings.Contains(string(md), "styleClipboard") || strings.Contains(string(md), "Cell format painter") {
		t.Fatal("ephemeral style state entered Markdown")
	}
	projection, err := reopened.projectTable(data, sceneRect{Width: 1200, Height: 600}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, cell := range projection.Cells {
		if cell.Row == 2 && cell.Column == 2 {
			matched = cell.Fill == deck.Theme.Colors.Accent[1] && cell.Text.FontID == "mono" && cell.Text.Size == 80
		}
	}
	if !matched {
		t.Fatal("shared renderer lost copied cell colour/font")
	}
	encoded, report, err := exportPPTXDocument(reopened)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code != "table-border-segments-reader-dependent" && warning.Code != "object-extra-not-exported" {
			t.Fatal("unexpected cell export warning", warning)
		}
	}
	read, _, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var native *pptx.Table
	for _, object := range read.Slides[0].Objects {
		if object.Kind == "table" {
			native = object.Table
		}
	}
	if native == nil || native.Cells[2][2].Text != "Unselected" || native.Cells[2][2].FontSize != 40 || !strings.EqualFold(native.Cells[2][2].Fill, deck.Theme.Colors.Accent[1]) || native.Cells[1][0].ColSpan != 2 {
		t.Fatal("copied formatting exported as non-editable/wrong cell data", native)
	}
}

func TestTableCellStyleSnapshotPortabilityOwnershipAndReset(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	snapshot, err := captureTableCellStyle(deck, table, &tableCellRange{R0: 0, C0: 0, R1: 0, C1: 0})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	if len(raw) > 8192 || strings.Contains(string(raw), "Source private text") || strings.Contains(string(raw), "future") || snapshot.Fields["cell.fill"].Token != "accent-2" {
		t.Fatal("cell snapshot lost token or included content/extensions", string(raw))
	}
	before := cloneTable(table)
	bounds := &tableCellRange{R0: 1, C0: 0, R1: 2, C1: 1}
	draft, changed, err := applyTableCellStyleData(deck, table, bounds, snapshot, false)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if !reflect.DeepEqual(table, before) || draft.Cells[1][0].ColSpan != 2 || !draft.Cells[1][1].Hidden || draft.Cells[1][0].Runs[0].Text != "Target one" || draft.Cells[1][0].Runs[0].Color != "#aabbcc" || draft.Cells[1][0].Extra == nil || !reflect.DeepEqual(draft.Rows, table.Rows) || !reflect.DeepEqual(draft.Columns, table.Columns) || !reflect.DeepEqual(draft.Cells[2][2], table.Cells[2][2]) {
		t.Fatal("range style changed source, content, spans or tracks")
	}
	style := draft.Cells[1][0].Style
	if style.Fill != "theme:accent-2" || style.Borders["left"].Color != "theme:accent-3" || *style.Borders["left"].Width != 3.25 || style.Borders["bottom"].Color != "none" || *style.Borders["bottom"].Width != 0 {
		t.Fatal("edge style/token/explicit Off lost", style)
	}
	if _, changed, err := applyTableCellStyleData(deck, draft, bounds, snapshot, false); err != nil || changed {
		t.Fatal("identical cell style isn't a no-op", err)
	}
	other := Deck{}
	fallback, _, err := applyTableCellStyleData(other, table, bounds, snapshot, false)
	if err != nil || fallback.Cells[1][0].Style.Fill != deck.Theme.Colors.Accent[1] || fallback.Cells[1][0].Style.Borders["left"].Color != deck.Theme.Colors.Accent[2] {
		t.Fatal("cell role fallback failed", err)
	}
	reset, changed, err := applyTableCellStyleData(deck, draft, bounds, nil, true)
	if err != nil || !changed || reset.Cells[1][0].Style != nil || reset.Cells[1][0].Runs[0].Color != "" || reset.Cells[1][0].Runs[0].Extra == nil || reset.Cells[1][0].Extra == nil {
		t.Fatal("Reset failed inheritance or content/extension ownership", err)
	}
	if _, changed, err := applyTableCellStyleData(deck, reset, bounds, nil, true); err != nil || changed {
		t.Fatal("repeated cell Reset isn't a no-op", err)
	}
}

func TestTableCellStyleValidationAndMixedEmphasis(t *testing.T) {
	deck, table := cellStyleTransferFixture(t)
	table.Cells[0][0].Text = ""
	table.Cells[0][0].Runs = []ObjectTextRun{{Text: "First", Bold: true, Italic: true}, {Text: "Second", Italic: true}}
	snapshot, err := captureTableCellStyle(deck, table, &tableCellRange{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := snapshot.Fields["cell.bold"]; exists || snapshot.Fields["cell.italic"].Value != "true" {
		t.Fatal("mixed emphasis flattened")
	}
	for _, scenario := range []string{"unknown", "range", "token", "absent", "number", "context"} {
		copy := snapshot.Clone()
		bounds := &tableCellRange{R0: 1, C0: 0, R1: 1, C1: 1}
		switch scenario {
		case "unknown":
			copy.Fields["cell.text"] = ObjectStyleField{Present: true, Value: "Replacement"}
		case "range":
			bounds.R1 = 20
		case "token":
			copy.Fields["cell.fill"] = ObjectStyleField{Present: true, Value: "#ffffff", Token: "bad"}
		case "absent":
			copy.Fields["cell.padding"] = ObjectStyleField{Value: "3"}
		case "number":
			copy.Fields["cell.padding"] = ObjectStyleField{Present: true, Value: "NaN"}
		case "context":
			copy.Context = ""
		}
		before := cloneTable(table)
		if _, changed, err := applyTableCellStyleData(deck, table, bounds, copy, false); err == nil || changed || !reflect.DeepEqual(before, table) {
			t.Fatal("invalid cell style applied", scenario, err)
		}
	}
}
