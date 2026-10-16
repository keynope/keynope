package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

var tableCellPropertyIDs = func() []string {
	ids := []string{}
	for _, field := range tableCellStyleFields {
		if field == "bold" || field == "italic" || field == "underline" {
			continue // Rich-word emphasis retains its independently targeted editor.
		}
		ids = append(ids, "cell."+field)
		if field == "fill" || field == "color" || field == "borderColor" || strings.HasSuffix(field, ".color") {
			ids = append(ids, "cell."+field+"Token")
		}
	}
	return ids
}()

func tableCellPropertyKnown(property string) bool {
	for _, id := range tableCellPropertyIDs {
		if id == property {
			return true
		}
	}
	return false
}

func tableCellAuthoredValue(style *TableCellStyle, name string) (any, bool) {
	if style == nil {
		return nil, false
	}
	if parts := strings.Split(name, "."); len(parts) == 3 {
		edge := style.Borders[parts[1]]
		if parts[2] == "color" {
			if edge.Color != "" {
				return edge.Color, true
			}
			name = "borderColor"
		} else {
			if edge.Width != nil {
				return *edge.Width, true
			}
			name = "borderWidth"
		}
	}
	var value string
	var number *float64
	switch name {
	case "fill":
		value = style.Fill
	case "color":
		value = style.Color
	case "borderColor":
		value = style.BorderColor
	case "fontId":
		value = style.FontID
	case "align":
		value = style.Align
	case "vertical":
		value = style.Vertical
	case "borderWidth":
		number = style.BorderWidth
	case "fontSize":
		number = style.FontSize
	case "fontWidth":
		number = style.FontWidth
	case "lineHeight":
		number = style.LineHeight
	case "padding":
		number = style.Padding
	}
	if number != nil {
		return *number, true
	}
	return value, value != ""
}

func (deck Deck) tableSelectionOwnerColor(action nativeEditorAction, owner Element) string {
	var resolved Slide
	if action.SceneMaster {
		resolved = masterViewPreview(deck.Masters, action.Slide)
	} else {
		resolved = deck.ResolveSlide(action.Slide, false)
	}
	return deck.tableResolvedOwnerColor(resolved, owner)
}

func (deck Deck) tableResolvedOwnerColor(resolved Slide, owner Element) string {
	for _, element := range resolved.Elements {
		if element.ID == owner.ID {
			owner = element
			break
		}
	}
	q, err := url.ParseQuery(owner.Query)
	treatment := "modern"
	if !deck.usesConcreteAppearance() {
		if validElementStyle(q.Get("element-style")) {
			treatment = q.Get("element-style")
		} else if deck.Appearance != nil {
			treatment = deck.elementStyle(resolved, owner)
		}
	}
	resolved.ThemeColors = deck.themeColors(treatment)
	paint := deck.applyThemeObjectPaint(owner, resolved, q, scenePaintFromValues(owner, resolved, 1, 1, q, err))
	if (deck.Theme != nil || deck.hasStructuredTextFallback(owner)) && !q.Has("fg") && !q.Has("header") && !q.Has("theme-fg") && resolved.FG == "" {
		return ""
	}
	return paint.Color
}

func (deck Deck) hasStructuredTextFallback(owner Element) bool {
	if deck.Objects == nil {
		return false
	}
	if owner.Kind == "table" {
		return deck.Objects.Tables[owner.ObjectID].Style.Cell.DefaultColor != ""
	}
	if owner.Kind == "chart" {
		return deck.Objects.Charts[owner.ObjectID].Options.DefaultColor != ""
	}
	return false
}

func tableCellTokenValue(raw any, explicit bool) string {
	if !explicit {
		return ""
	}
	if color, ok := raw.(string); ok && strings.HasPrefix(color, "theme:") {
		return strings.TrimPrefix(color, "theme:")
	}
	if raw == "none" {
		return "none"
	}
	return "custom"
}

func populateTableCellProperties(deck Deck, owner Element, data TableData, anchors [][2]int, state *tableCellStyleSelection, ownerColor string) {
	textCache := map[string]*sceneText{}
	for _, property := range tableCellPropertyIDs {
		state.Properties[property] = selectionPropertyState{State: "unavailable", EligibleIDs: []string{owner.ID}, TotalCount: 1, Editable: state.Commands["style.reset"].Enabled, Reason: state.Commands["style.reset"].Reason}
	}
	for _, pos := range anchors {
		r, c := pos[0], pos[1]
		cell := data.Cells[r][c]
		format := deck.tableCellFormat(data, r, cell, ownerColor)
		key := format.TextValues.Encode()
		text := textCache[key]
		if text == nil {
			text = deck.modernSceneTextFromValues(Element{Kind: "text"}, format.TextValues)
			textCache[key] = text
		}
		for _, property := range tableCellPropertyIDs {
			token := strings.HasSuffix(property, "Token")
			name := strings.TrimSuffix(strings.TrimPrefix(property, "cell."), "Token")
			names := []string{name}
			if name == "borderColor" || name == "borderWidth" {
				part := "color"
				if name == "borderWidth" {
					part = "width"
				}
				names = []string{"borders.left." + part, "borders.right." + part, "borders.top." + part, "borders.bottom." + part}
			}
			for _, field := range names {
				raw, explicit := tableCellAuthoredValue(cell.Style, field)
				var value any
				switch name {
				case "fill":
					value = format.Style.Fill
				case "color":
					value = format.Color
				case "fontId":
					value = text.FontID
				case "fontSize":
					value = text.Size
				case "fontWidth":
					value = deck.tableCellFontWidth(format)
				case "lineHeight":
					value = text.LineHeight
				case "padding":
					value = format.Padding
				case "align":
					value = text.Align
					if value == "start" {
						value = "left"
					} else if value == "end" {
						value = "right"
					}
				case "vertical":
					value = text.Vertical
				}
				if parts := strings.Split(field, "."); len(parts) == 3 {
					tableBorderPositions(data, r, c, parts[1], func(er, ec int) {
						edge := deck.tableCellBorderAt(data, r, c, parts[1], er, ec, format.BorderColor, format.BorderWidth)
						raw, explicit := tableBorderAuthoredAt(data, r, c, parts[1], parts[2], er, ec)
						var v any = edge.color
						if parts[2] == "width" {
							v = edge.width
						}
						if token {
							v = tableCellTokenValue(raw, explicit)
						}
						accumulateTableCellProperty(state, property, v, explicit)
					})
					continue
				}
				if token {
					value = tableCellTokenValue(raw, explicit)
				}
				accumulateTableCellProperty(state, property, value, explicit)
			}
		}
	}
	for property, p := range state.Properties {
		if p.State == "mixed" {
			p.Value = nil
		}
		state.Properties[property] = p
	}
}

func accumulateTableCellProperty(state *tableCellStyleSelection, property string, value any, explicit bool) {
	p := state.Properties[property]
	provenance := "inherited"
	if explicit {
		provenance = "explicit"
	}
	if p.State == "unavailable" {
		p.State, p.Value, p.Provenance = "value", value, provenance
	} else {
		if !reflect.DeepEqual(value, p.Value) {
			p.State = "mixed"
		}
		if provenance != p.Provenance {
			p.Provenance = "mixed"
		}
	}
	state.Properties[property] = p
}

func setTableCellPropertyData(deck Deck, data TableData, action nativeEditorAction) (TableData, bool, error) {
	return setTableCellPropertyPrepared(deck, data, action, nil)
}

func setTableCellPropertyPrepared(deck Deck, data TableData, action nativeEditorAction, ownerColor *string) (TableData, bool, error) {
	if tableDefaultPropertyKnown(action.Property) {
		return setTableDefaultPropertyPrepared(deck, data, action, ownerColor)
	}
	if action.PropertyDelta != nil {
		return incrementTableCellPropertyPrepared(deck, data, action, ownerColor)
	}
	if !tableCellPropertyKnown(action.Property) || len(action.PropertyValue) == 0 || bytes.Equal(bytes.TrimSpace(action.PropertyValue), []byte("null")) {
		return TableData{}, false, fmt.Errorf("invalid table cell property")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(action.Property, "cell."), "Token")
	field := ObjectStyleField{}
	var value any
	if err := json.Unmarshal(action.PropertyValue, &value); err != nil {
		return TableData{}, false, err
	}
	if value != "" {
		field.Present = true
		if number, ok := value.(float64); ok {
			if _, _, metric := tableCellMetricRange(name); !metric {
				return TableData{}, false, fmt.Errorf("cell property requires a string")
			}
			field.Value = strconv.FormatFloat(number, 'f', -1, 64)
		} else if text, ok := value.(string); ok {
			if _, _, metric := tableCellMetricRange(name); metric {
				return TableData{}, false, fmt.Errorf("cell property requires a number")
			}
			if strings.HasSuffix(action.Property, "Token") {
				if text == "none" {
					field.Value = "none"
				} else if !validThemeColorToken(text) {
					return TableData{}, false, fmt.Errorf("invalid cell colour role")
				} else {
					field.Token, field.Value = text, deck.themeTokenColor(text)
				}
			} else {
				field.Value = text
				if name == "fill" || name == "color" || name == "borderColor" || strings.HasSuffix(name, ".color") {
					if color, ok := normalizeHexColour(text); ok {
						field.Value = color
					}
				}
			}
		} else {
			return TableData{}, false, fmt.Errorf("invalid cell property type")
		}
	}
	snapshot := &ObjectStyleSnapshot{Version: 1, Context: "table-cells", ThemeSignature: objectStyleThemeSignature(deck), Fields: map[string]ObjectStyleField{"cell." + name: field}}
	// Editing a whole-cell border property replaces that property on all four
	// sides, just like the existing controls. Preserve the other side property.
	if name == "borderColor" || name == "borderWidth" {
		part := "color"
		if name == "borderWidth" {
			part = "width"
		}
		for _, side := range []string{"left", "right", "top", "bottom"} {
			snapshot.Fields["cell.borders."+side+"."+part] = ObjectStyleField{}
		}
	}
	return applyTableCellStyleData(deck, data, action.CellRange, snapshot, false)
}

func incrementTableCellProperty(deck Deck, data TableData, action nativeEditorAction) (TableData, bool, error) {
	return incrementTableCellPropertyPrepared(deck, data, action, nil)
}

func incrementTableCellPropertyPrepared(deck Deck, data TableData, action nativeEditorAction, ownerColor *string) (TableData, bool, error) {
	name := strings.TrimPrefix(action.Property, "cell.")
	delta := *action.PropertyDelta
	_, _, metric := tableCellMetricRange(name)
	if !tableCellPropertyKnown(action.Property) || len(action.PropertyValue) != 0 || math.IsNaN(delta) || math.IsInf(delta, 0) || math.Abs(delta) > 1024 || !metric {
		return TableData{}, false, fmt.Errorf("invalid cell property increment")
	}
	if err := data.Validate(); err != nil {
		return TableData{}, false, err
	}
	anchors, err := tableCellRangeAnchors(data, action.CellRange)
	if err != nil {
		return TableData{}, false, err
	}
	color := ""
	if ownerColor != nil {
		color = *ownerColor
	} else {
		_, owner, _, err := tableCellStyleTarget(deck, action)
		if err != nil {
			return TableData{}, false, err
		}
		color = deck.tableSelectionOwnerColor(action, owner)
	}
	draft := cloneTable(data)
	for _, pos := range anchors {
		r, c := pos[0], pos[1]
		format := deck.tableCellFormat(data, r, data.Cells[r][c], color)
		style := TableCellStyle{}
		if original := draft.Cells[r][c].Style; original != nil {
			style = cloneCellStyle(*original)
		}
		increment := func(value, low, high float64) *float64 {
			next := math.Max(low, math.Min(high, value+delta))
			return &next
		}
		switch name {
		case "fontWidth", "lineHeight":
			value := deck.tableCellFontWidth(format)
			if name == "lineHeight" {
				value = deck.modernSceneTextFromValues(Element{Kind: "text"}, format.TextValues).LineHeight
			}
			low, high, _ := tableCellMetricRange(name)
			next := math.Max(low, math.Min(high, math.Round((value+delta)*1e9)/1e9))
			if delta == 0 || next == value {
				continue
			}
			if name == "fontWidth" {
				style.FontWidth = &next
			} else {
				style.LineHeight = &next
			}
		case "fontSize":
			text := deck.modernSceneTextFromValues(Element{Kind: "text"}, format.TextValues)
			if delta == 0 || math.Max(1, math.Min(1024, text.Size+delta)) == text.Size {
				continue
			}
			style.FontSize = increment(text.Size, 1, 1024)
		case "padding":
			if delta == 0 || math.Max(0, math.Min(100, format.Padding+delta)) == format.Padding {
				continue
			}
			style.Padding = increment(format.Padding, 0, 100)
		default:
			sides := []string{"left", "right", "top", "bottom"}
			if name != "borderWidth" {
				sides = []string{strings.Split(name, ".")[1]}
			}
			if style.Borders == nil {
				style.Borders = map[string]TableBorderStyle{}
			}
			for _, side := range sides {
				usesOwnerWidth := false
				tableBorderPositions(data, r, c, side, func(er, ec int) {
					segments := data.Cells[r][c].BorderFragments[side]
					i := tableBorderFragmentIndex(r, c, side, er, ec)
					if i >= 0 && i < len(segments) && segments[i].Width != nil {
						return
					}
					legacy := data.Cells[er][ec]
					if legacy.Hidden && legacy.Style != nil && legacy.Style.Borders[side].Width != nil {
						return
					}
					usesOwnerWidth = true
				})
				for i, edge := range draft.Cells[r][c].BorderFragments[side] {
					if edge.Width != nil && delta != 0 {
						next := math.Max(0, math.Min(100, *edge.Width+delta))
						edge.Width = &next
						draft.Cells[r][c].BorderFragments[side][i] = edge
					}
				}
				if delta != 0 {
					tableBorderPositions(data, r, c, side, func(er, ec int) {
						original := data.Cells[er][ec]
						if !original.Hidden || original.Style == nil {
							return
						}
						edge := original.Style.Borders[side]
						if edge.Width == nil {
							return
						}
						next := math.Max(0, math.Min(100, *edge.Width+delta))
						if next == *edge.Width {
							return
						}
						copy := cloneCellStyle(*original.Style)
						edge = copy.Borders[side]
						edge.Width = &next
						copy.Borders[side] = edge
						draft.Cells[er][ec].Style = &copy
					})
				}
				value := deck.tableCellBorder(data, r, c, side, format.BorderColor, format.BorderWidth).width
				if !usesOwnerWidth || delta == 0 || math.Max(0, math.Min(100, value+delta)) == value {
					continue
				}
				edge := style.Borders[side]
				edge.Width = increment(value, 0, 100)
				style.Borders[side] = edge
			}
			if len(style.Borders) == 0 {
				style.Borders = nil
			}
		}
		if !reflect.DeepEqual(style, TableCellStyle{}) {
			draft.Cells[r][c].Style = &style
		}
	}
	if err := draft.Validate(); err != nil {
		return TableData{}, false, err
	}
	return draft, !reflect.DeepEqual(data, draft), nil
}

func applyTableCellPropertyCommand(deck *Deck, action nativeEditorAction, group string) (bool, error) {
	action.TableData = nil
	capabilities, err := tableCellStyleCapabilities(*deck, action, group, 0)
	if err != nil {
		return false, err
	}
	property, ok := capabilities.Properties[action.Property]
	if !ok || !property.Editable {
		return false, fmt.Errorf("cell property %q is unavailable: %s", action.Property, property.Reason)
	}
	_, _, data, err := tableCellStyleTarget(*deck, action)
	if err != nil {
		return false, err
	}
	draft, changed, err := setTableCellPropertyData(*deck, data, action)
	if err != nil || !changed {
		return changed, err
	}
	action.Action, action.TableData, action.Geometry = "set-table", &draft, nil
	return applyTableCommand(deck, action, group)
}
