package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

var tableDefaultPropertyIDs = func() []string {
	ids := []string{"table.header", "table.banded", "table.headerFill", "table.headerFillToken", "table.bandFill", "table.bandFillToken"}
	for _, property := range tableCellPropertyIDs {
		ids = append(ids, "table."+strings.TrimPrefix(property, "cell."))
	}
	return ids
}()

func tableDefaultPropertyKnown(property string) bool {
	for _, id := range tableDefaultPropertyIDs {
		if property == id {
			return true
		}
	}
	return false
}

// Treat defaults as authored formatting on a synthetic empty anchor only while
// resolving their inherited values. It is never added to the document/scene.
// This reuses the same default font, palette and side-border rules as cells.
func tableDefaultPropertyProjection(data TableData) TableData {
	base, _ := newTableData(1, 1)
	base.Style = TableStyle{}
	style := cloneCellStyle(data.Style.Cell)
	base.Cells[0][0].Style = &style
	return base
}

func populateTableDefaultProperties(deck Deck, owner Element, data TableData, state *tableCellStyleSelection, color string) {
	defaults := &tableCellStyleSelection{selectionCapabilityState: selectionCapabilityState{Properties: map[string]selectionPropertyState{}, Commands: state.Commands}}
	populateTableCellProperties(deck, owner, tableDefaultPropertyProjection(data), [][2]int{{0, 0}}, defaults, color)
	for property, value := range defaults.Properties {
		state.Properties["table."+strings.TrimPrefix(property, "cell.")] = value
	}
	for property, value := range map[string]bool{"table.header": data.Style.Header, "table.banded": data.Style.Banded} {
		state.Properties[property] = selectionPropertyState{State: "value", Value: value, Provenance: "explicit", EligibleIDs: []string{owner.ID}, TotalCount: 1, Editable: state.Commands["style.reset"].Enabled, Reason: state.Commands["style.reset"].Reason}
	}
	for _, field := range []string{"headerFill", "bandFill"} {
		raw := data.Style.HeaderFill
		row := 0
		preview := data
		preview.Style.Header, preview.Style.Banded = true, false
		if field == "bandFill" {
			raw, row = data.Style.BandFill, 1
			preview.Style.Header, preview.Style.Banded = false, true
		}
		format := deck.tableCellFormat(preview, row, TableCell{}, color)
		provenance := "inherited"
		if raw != "" {
			provenance = "explicit"
		}
		for property, value := range map[string]any{"table." + field: format.Style.Fill, "table." + field + "Token": tableCellTokenValue(raw, raw != "")} {
			state.Properties[property] = selectionPropertyState{State: "value", Value: value, Provenance: provenance, EligibleIDs: []string{owner.ID}, TotalCount: 1, Editable: state.Commands["style.reset"].Enabled, Reason: state.Commands["style.reset"].Reason}
		}
	}
	state.Contexts = map[string][]string{"table-cells": {owner.ID}, "table-defaults": {owner.ID}}
}

func setTableDefaultPropertyData(deck Deck, data TableData, action nativeEditorAction) (TableData, bool, error) {
	return setTableDefaultPropertyPrepared(deck, data, action, nil)
}

func setTableDefaultPropertyPrepared(deck Deck, data TableData, action nativeEditorAction, ownerColor *string) (TableData, bool, error) {
	if !tableDefaultPropertyKnown(action.Property) {
		return TableData{}, false, fmt.Errorf("unknown table default property")
	}
	if err := data.Validate(); err != nil {
		return TableData{}, false, err
	}
	name := strings.TrimPrefix(action.Property, "table.")
	draft := cloneTable(data)
	if name == "header" || name == "banded" {
		var value any
		if action.PropertyDelta != nil || len(action.PropertyValue) == 0 || json.Unmarshal(action.PropertyValue, &value) != nil {
			return TableData{}, false, fmt.Errorf("table flag requires a boolean")
		}
		flag, valid := value.(bool)
		if !valid {
			return TableData{}, false, fmt.Errorf("table flag requires a boolean")
		}
		if name == "header" {
			draft.Style.Header = flag
		} else {
			draft.Style.Banded = flag
		}
	} else if strings.HasPrefix(name, "headerFill") || strings.HasPrefix(name, "bandFill") {
		if action.PropertyDelta != nil {
			return TableData{}, false, fmt.Errorf("table fill cannot be incremented")
		}
		// Fill properties have precisely the same typed RGB/role/None/Inherit
		// semantics as cell fill. Do not create a separate colour parser.
		command := action
		command.Property = "cell.fill"
		if strings.HasSuffix(name, "Token") {
			command.Property += "Token"
		}
		command.CellRange = &tableCellRange{}
		base, _ := newTableData(1, 1)
		styled, _, err := setTableCellPropertyPrepared(deck, base, command, ownerColor)
		if err != nil {
			return TableData{}, false, err
		}
		fill := ""
		if styled.Cells[0][0].Style != nil {
			fill = styled.Cells[0][0].Style.Fill
		}
		if strings.HasPrefix(name, "headerFill") {
			draft.Style.HeaderFill = fill
		} else {
			draft.Style.BandFill = fill
		}
	} else {
		command := action
		command.Property, command.CellRange = "cell."+name, &tableCellRange{}
		base := tableDefaultPropertyProjection(data)
		// Increment resolves the owning default style against the real theme/
		// owner, then edits only this synthetic style (never an actual cell).
		styled, _, err := setTableCellPropertyPrepared(deck, base, command, ownerColor)
		if err != nil {
			return TableData{}, false, err
		}
		draft.Style.Cell = TableCellStyle{}
		if styled.Cells[0][0].Style != nil {
			draft.Style.Cell = cloneCellStyle(*styled.Cells[0][0].Style)
		}
	}
	if err := draft.Validate(); err != nil {
		return TableData{}, false, err
	}
	return draft, !reflect.DeepEqual(data, draft), nil
}
