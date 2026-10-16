package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func tableCellPropertyFixture(t *testing.T) (Deck, nativeEditorAction) {
	deck, data := cellStyleTransferFixture(t)
	data.Style.Header = false
	deck.Appearance = &DeckAppearance{Version: 3}
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"cell-data": data}}
	deck.Slides = []Slide{{ID: "cells", Elements: []Element{{ID: "table-a", Kind: "table", ObjectID: "cell-data", Query: "left=16&top=8&width=205&height=38"}}}}
	return deck, nativeEditorAction{Action: "set-selection-property", StyleContext: "table-cells", ObjectID: "table-a", CellRange: &tableCellRange{}, Property: "cell.fontSize"}
}

func TestTableCellPropertyProjectionParityAndProvenance(t *testing.T) {
	for _, theme := range []string{"", "clean-light", "clean-dark"} {
		for _, ownerColor := range []string{"", "fg=%23ffffff", "fg=%23123456", "theme-fg=accent-4"} {
			deck, action := tableCellPropertyFixture(t)
			deck.Theme = builtinDeckTheme(theme)
			if theme == "" {
				deck.Theme = nil
			}
			deck.Slides[0].Elements[0].Query += "&" + ownerColor
			data := deck.Objects.Tables["cell-data"]
			data.Style.Header, data.Style.Banded = true, true
			deck.Objects.Tables["cell-data"] = data
			scene, err := buildSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			for _, cell := range scene.Objects[0].Table.Cells {
				action.CellRange = &tableCellRange{R0: cell.Row, R1: cell.Row, C0: cell.Column, C1: cell.Column}
				caps, err := tableCellStyleCapabilities(deck, action, "", 8)
				if err != nil {
					t.Fatal(err)
				}
				align := cell.Text.Align
				if align == "start" {
					align = "left"
				} else if align == "end" {
					align = "right"
				}
				for property, want := range map[string]any{"cell.fill": cell.Fill, "cell.color": cell.Paint.Color, "cell.fontId": cell.Text.FontID, "cell.fontSize": cell.Text.Size, "cell.align": align, "cell.vertical": cell.Text.Vertical, "cell.padding": cell.ContentBounds.X} {
					got := caps.Properties[property]
					if got.State != "value" || !reflect.DeepEqual(got.Value, want) || !got.Editable {
						t.Fatalf("%s/%s cell %d,%d %s: %#v want %#v", theme, ownerColor, cell.Row, cell.Column, property, got, want)
					}
				}
			}
		}
	}
	deck, action := tableCellPropertyFixture(t)
	data := deck.Objects.Tables["cell-data"]
	data.Cells[0][0].Style = nil
	data.Cells[0][1].Style = &TableCellStyle{Fill: deck.Theme.Colors.Background}
	data.Cells[0][2].Style = &TableCellStyle{Fill: "theme:background"}
	deck.Objects.Tables["cell-data"] = data
	action.CellRange = &tableCellRange{R0: 0, R1: 0, C0: 0, C1: 2}
	caps, err := tableCellStyleCapabilities(deck, action, "", 8)
	if err != nil || caps.Properties["cell.fill"].State != "value" || caps.Properties["cell.fill"].Provenance != "mixed" || caps.Properties["cell.fillToken"].State != "mixed" {
		t.Fatal("equality conflated with authored/role provenance", caps, err)
	}
	// Unknown cell text/extension metadata must not enter the property DTO.
	raw, _ := json.Marshal(caps)
	if strings.Contains(string(raw), "Source private text") || strings.Contains(string(raw), "future") {
		t.Fatal("cell capability leaked semantic content/extensions")
	}
}

func TestTableCellPropertyTypedSetStepInheritanceAndOwnership(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	action.CellRange = &tableCellRange{R0: 0, R1: 0, C0: 0, C1: 1}
	data := deck.Objects.Tables["cell-data"]
	small := 20.0
	data.Cells[0][1].Style = &TableCellStyle{FontSize: &small}
	deck.Objects.Tables["cell-data"] = data
	before := cloneDeck(deck)
	delta := 1.0
	action.PropertyDelta = &delta
	changed, err := applyTableCellPropertyCommand(&deck, action, "")
	if err != nil || !changed {
		t.Fatal(err)
	}
	got := deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if *got.Cells[0][0].Style.FontSize != 81 || *got.Cells[0][1].Style.FontSize != 21 || got.Cells[0][0].Text != data.Cells[0][0].Text || !reflect.DeepEqual(got.Cells[1:], data.Cells[1:]) || !reflect.DeepEqual(got.Rows, data.Rows) || !reflect.DeepEqual(got.Columns, data.Columns) || !reflect.DeepEqual(before.Objects.Tables["cell-data"], data) {
		t.Fatal("mixed range increment flattened sizes/content or mutated source")
	}
	action.PropertyDelta = nil
	action.Property, action.PropertyValue = "cell.fontId", json.RawMessage(`"c64"`)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	got = deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if got.Cells[0][1].Style.FontID != "c64" {
		t.Fatal("font ID was mistaken for a short hexadecimal colour")
	}
	action.Property, action.PropertyValue = "cell.fillToken", json.RawMessage(`"accent-3"`)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	got = deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if got.Cells[0][1].Style.Fill != "theme:accent-3" {
		t.Fatal("semantic role not preserved")
	}
	action.PropertyValue = json.RawMessage(`"none"`)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	caps, err := tableCellStyleCapabilities(deck, action, "", 0)
	if err != nil || caps.Properties["cell.fill"].Value != "none" || caps.Properties["cell.fillToken"].Value != "none" || caps.Properties["cell.fillToken"].Provenance != "explicit" {
		t.Fatal("explicit None confused with inheritance", caps, err)
	}
	action.Property, action.PropertyValue = "cell.borderWidth", json.RawMessage(`2.5`)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	got = deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if got.Cells[0][0].Style.Borders["left"].Width != nil || got.Cells[0][0].Style.Borders["left"].Color != "theme:accent-3" || *got.Cells[0][0].Style.BorderWidth != 2.5 {
		t.Fatal("uniform width failed to replace only the side widths")
	}
	action.PropertyValue = json.RawMessage(`""`)
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	got = deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	if got.Cells[0][0].Style.BorderWidth != nil || got.Cells[0][0].Style.Borders["left"].Color != "theme:accent-3" {
		t.Fatal("Inherit removed an unrelated border colour")
	}
	before = cloneDeck(deck)
	if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil || changed || !reflect.DeepEqual(before, deck) {
		t.Fatal("repeat Inherit is not a no-op", err)
	}
	for _, bad := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`"2.5"`), json.RawMessage(`-1`), json.RawMessage(`100.1`), json.RawMessage(`true`), json.RawMessage(`{"width":2}`)} {
		action.PropertyValue = bad
		if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err == nil || changed || !reflect.DeepEqual(before, deck) {
			t.Fatal("bad typed property partially applied", string(bad), err)
		}
	}
}

func TestTableCellPropertyNormalMasterHistoryAndDeltaRejection(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck, action := tableCellPropertyFixture(t)
		s := newNativeEditorSession("Cells.md", deck)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide.Elements = append([]Element(nil), deck.Slides[0].Elements...)
		}
		before := cloneDeck(s.deck)
		action.SceneMaster, action.SceneRevision = master, &s.version
		action.Property, action.PropertyValue = "cell.padding", json.RawMessage(`15.5`)
		if err := s.apply(action); err != nil || len(s.undo) != 1 {
			t.Fatal("property not one transaction", master, err)
		}
		after := cloneDeck(s.deck)
		if master && !reflect.DeepEqual(after.Slides, before.Slides) {
			t.Fatal("master cell property changed a shared normal payload owner")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("Undo lost cell property ownership", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("Redo lost cell property ownership", err)
		}
		for _, bad := range []float64{math.NaN(), math.Inf(1), 1025} {
			action.PropertyValue, action.PropertyDelta = nil, &bad
			if err := s.apply(action); err == nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("invalid delta committed", master, err)
			}
		}
	}
}

func TestTableCellPropertyIndependentBorderIncrementAndClampNoOp(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	before := cloneDeck(deck)
	delta := 1.0
	action.Property, action.PropertyDelta = "cell.borderWidth", &delta
	if _, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	data := deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
	style := data.Cells[0][0].Style
	if *style.Borders["left"].Width != 4.25 || *style.Borders["bottom"].Width != 1 || style.Borders["bottom"].Color != "none" || style.Borders["left"].Color != "theme:accent-3" || !reflect.DeepEqual(before.Objects.Tables["cell-data"].Cells[1:], data.Cells[1:]) {
		t.Fatal("border increment flattened independent sides/Off or changed other cells", style)
	}
	for _, side := range []string{"left", "right", "top", "bottom"} {
		data.Cells[0][0].Style.Borders[side] = testEdge("none", 100)
	}
	deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID] = data
	before = cloneDeck(deck)
	if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil || changed || !reflect.DeepEqual(before, deck) {
		t.Fatal("clamped increment created metadata/history change", err)
	}
	for _, property := range []string{"cell.fill", "cell.bold", "cell.borders.left.colorToken"} {
		action.Property = property
		if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err == nil || changed || !reflect.DeepEqual(before, deck) {
			t.Fatal("non-metric increment accepted", property, err)
		}
	}
}

func TestTableCellPropertyDraftReturnsAcceptedStateWithoutCommit(t *testing.T) {
	deck, action := tableCellPropertyFixture(t)
	s := newNativeEditorSession("Draft.md", deck)
	version := s.version
	action.SceneRevision, action.TableData = &version, new(TableData)
	*action.TableData = cloneTable(s.deck.Objects.Tables["cell-data"])
	action.PropertyValue = json.RawMessage(`160`)
	before := cloneDeck(s.deck)
	call := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(action)
		response := httptest.NewRecorder()
		s.handleTableCellStyleDraft(response, httptest.NewRequest(http.MethodPost, "/api/editor/table-cell-style-draft", strings.NewReader(string(body))))
		return response
	}
	response := call()
	var result struct {
		Data         TableData                `json:"tableData"`
		Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Capabilities == nil || *result.Data.Cells[0][0].Style.FontSize != 160 || result.Capabilities.Properties["cell.fontSize"].Value != float64(160) || result.Capabilities.Properties["cell.fontSize"].Provenance != "explicit" || !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != 0 {
		t.Fatal("draft property lacked accepted state or committed document", response.Code, response.Body.String())
	}
	action.Property, action.PropertyValue = "cell.fontSize", json.RawMessage(`"160"`)
	if response = call(); response.Code != 400 || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("string numeric property accepted", response.Code)
	}
	action.PropertyValue = json.RawMessage(`160`)
	version--
	if response = call(); response.Code != 409 || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("stale numeric property accepted", response.Code)
	}
}

func BenchmarkTableCellPropertyCapabilities20x20(b *testing.B) {
	data, _ := newTableData(20, 20)
	for r := range data.Cells {
		for c := range data.Cells[r] {
			data.Cells[r][c].Text = strings.Repeat("Owned emoji 👾 long text not inspected ", 8)
		}
	}
	deck := Deck{Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"cells": data}}, Slides: []Slide{{Elements: []Element{{ID: "owner", Kind: "table", ObjectID: "cells"}}}}}
	action := nativeEditorAction{ObjectID: "owner", CellRange: &tableCellRange{R0: 0, R1: 19, C0: 0, C1: 19}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tableCellStyleCapabilities(deck, action, "", 1); err != nil {
			b.Fatal(err)
		}
	}
}
