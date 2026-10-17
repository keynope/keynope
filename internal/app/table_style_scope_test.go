package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestScopedTableStyleCapabilitiesAndCommandsMatchFullDeck(t *testing.T) {
	for _, mode := range []int{-1, 0, 3} {
		deck, table := cellStyleTransferFixture(t)
		deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": table}}
		owner := Slide{ID: "selected", FG: "38;2;85;170;255", FGSet: true, Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data"}}}
		deck.Slides = []Slide{{ID: "unused-a"}, {ID: "unused-b"}, owner}
		s := newNativeEditorSession("Cells.md", deck)
		s.current = 2
		index := 2
		if mode >= 0 {
			s.masterMode, s.currentMaster, index = true, mode, mode
			if mode == 0 {
				s.deck.Masters.Base.Slide = cloneSlide(owner)
			} else {
				s.deck.Masters.Layouts = []MasterLayout{{ID: "unused-a"}, {ID: "unused-b"}, {ID: "chosen", Slide: cloneSlide(owner)}}
			}
		}
		heavy := wordStyleHeavyFixture(t)
		for id, data := range heavy.Objects.Tables {
			s.deck.Objects.Tables[id] = data
		}
		s.deck.Assets = map[string]DeckAsset{"unused": {Data: make([]byte, 1<<20)}}
		before, revision := cloneDeck(s.deck), s.version
		style, err := captureTableCellStyle(s.deck, table, &tableCellRange{})
		if err != nil {
			t.Fatal(err)
		}
		for _, operation := range []string{"cell-style-capabilities", "copy-object-style", "paste-object-style", "reset-object-style", "cell.fontSize", "cell.padding+", "table.fontSize+", "table.headerFillToken"} {
			a := nativeEditorAction{Action: operation, Slide: index, SceneMaster: mode >= 0, SceneRevision: &revision, StyleContext: "table-cells", ObjectID: "table-a", TableData: &table, CellRange: &tableCellRange{}, StyleData: style}
			switch operation {
			case "cell.fontSize":
				a.Action, a.Property, a.PropertyValue = "set-selection-property", operation, json.RawMessage(`72`)
			case "cell.padding+", "table.fontSize+":
				delta := 1.0
				a.Action, a.Property, a.PropertyDelta = "set-selection-property", operation[:len(operation)-1], &delta
			case "table.headerFillToken":
				a.Action, a.Property, a.PropertyValue = "set-selection-property", operation, json.RawMessage(`"accent-2"`)
			}
			body, _ := json.Marshal(a)
			w := httptest.NewRecorder()
			s.handleTableCellStyleDraft(w, httptest.NewRequest("POST", "/api/editor/table-cell-style-draft", bytes.NewReader(body)))
			if w.Code != 200 {
				t.Fatal(mode, operation, w.Code, w.Body.String())
			}
			var got struct {
				Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
				Data         TableData                `json:"tableData"`
				Clipboard    *ObjectStyleSnapshot     `json:"styleClipboard"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			var expected TableData
			switch a.Action {
			case "set-selection-property":
				expected, _, err = setTableCellPropertyData(s.deck, table, a)
			case "paste-object-style", "reset-object-style":
				expected, _, err = applyTableCellStyleData(s.deck, table, a.CellRange, style, a.Action == "reset-object-style")
			case "copy-object-style":
				if !reflect.DeepEqual(got.Clipboard, style) || !reflect.DeepEqual(s.styleClipboard, style) {
					t.Fatal("copy lost style")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.Action == "set-selection-property" || a.Action == "paste-object-style" || a.Action == "reset-object-style" {
				if !reflect.DeepEqual(expected, got.Data) {
					t.Fatal("prepared mutation differs", mode, operation)
				}
				a.TableData = &expected
			}
			want, err := tableCellStyleCapabilities(s.deck, a, "", revision)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(want)
			var normalized tableCellStyleSelection
			_ = json.Unmarshal(encoded, &normalized)
			if !reflect.DeepEqual(got.Capabilities, &normalized) {
				t.Fatal("scoped capability differs", mode, operation)
			}
			if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
				t.Fatal("draft changed document")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("draft cached")
			}
		}
	}
}

func TestTableStyleScopeCaptureOwnershipAndBounds(t *testing.T) {
	d := structuredFixture()
	d.Theme = builtinDeckTheme("clean-light")
	d.Slides[0].Notes = "private"
	d.Assets = map[string]DeckAsset{"unrelated": {Data: make([]byte, 1<<20)}}
	a := nativeEditorAction{ObjectID: "table-element"}
	scope, owner, err := captureTableStyleScope(d, a)
	if err != nil || owner.ObjectID != "table-1" {
		t.Fatal(err)
	}
	if scope.deck.Slides != nil || scope.deck.Assets != nil || scope.source.Notes != "" || len(scope.deck.Objects.Tables) != 1 || len(scope.deck.Objects.Tables[owner.ObjectID].Cells) != 0 || len(scope.deck.Objects.Charts) != 0 || len(scope.deck.Objects.Builds) != 0 {
		t.Fatal("unrelated/private payload retained")
	}
	scope.source.Elements[0].Query = "changed"
	scope.deck.Theme.Colors.Accent[0] = "#123456"
	if d.Slides[0].Elements[0].Query == "changed" || d.Theme.Colors.Accent[0] == "#123456" {
		t.Fatal("scope aliases document")
	}
	d.Masters.Layouts = []MasterLayout{{ID: "same", Slide: d.Slides[0]}, {ID: "same", Slide: d.Slides[0]}}
	a.SceneMaster, a.Slide = true, 2
	if _, _, err := captureTableStyleScope(d, a); err == nil {
		t.Fatal("wrong duplicate-layout owner accepted")
	}
}

type tableStyleContextProbe struct {
	context.Context
	session *nativeEditorSession
	changed bool
}

func (p *tableStyleContextProbe) Err() error {
	// The first cancellation boundary is outside the document lock, after
	// capture. Do not mutate from the copy operation's in-lock boundary.
	if !p.changed {
		p.changed = true
		p.session.mu.Lock()
		p.session.current++
		p.session.mu.Unlock()
	}
	return nil
}

func TestTableStyleDraftRejectsStalePreparation(t *testing.T) {
	for _, operation := range []string{"cell-style-capabilities", "copy-object-style", "reset-object-style"} {
		s := newNativeEditorSession("Cells.md", structuredFixture())
		revision := s.version
		data := cloneTable(s.deck.Objects.Tables["table-1"])
		a := nativeEditorAction{Action: operation, ObjectID: "table-element", SceneRevision: &revision, StyleContext: "table-cells", TableData: &data, CellRange: &tableCellRange{}}
		body, _ := json.Marshal(a)
		probe := &tableStyleContextProbe{Context: context.Background(), session: s}
		w := httptest.NewRecorder()
		s.handleTableCellStyleDraft(w, httptest.NewRequest("POST", "/api/editor/table-cell-style-draft", bytes.NewReader(body)).WithContext(probe))
		if w.Code != 409 || s.styleClipboard != nil || len(s.undo) != 0 {
			t.Fatal(operation, "stale result published", w.Code)
		}
	}
}

func BenchmarkTableStyleScopeCapture(b *testing.B) {
	d := wordStyleHeavyFixture(b)
	base := structuredFixture()
	d.Slides[0] = base.Slides[0]
	d.Objects.Tables["table-1"] = base.Objects.Tables["table-1"]
	a := nativeEditorAction{ObjectID: "table-element"}
	for _, scoped := range []bool{false, true} {
		b.Run(fmt.Sprint("scoped=", scoped), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if scoped {
					if _, _, err := captureTableStyleScope(d, a); err != nil {
						b.Fatal(err)
					}
				} else if len(cloneDeckForScene(d).Slides) == 0 {
					b.Fatal("missing snapshot")
				}
			}
		})
	}
}
