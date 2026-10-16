package main

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

type tableCellRange struct {
	R0 int `json:"r0"`
	C0 int `json:"c0"`
	R1 int `json:"r1"`
	C1 int `json:"c1"`
}

// Cells retain the table's authored identity. The range is a sub-selection,
// not a collection of fake top-level objects. Reuse the command-capability
// contract and expose its cell cardinality independently of object counts.
type tableCellStyleSelection struct {
	selectionCapabilityState
	CellRange tableCellRange `json:"cellRange"`
	CellCount int            `json:"cellCount"`
}

func tableCellStyleCapabilities(deck Deck, action nativeEditorAction, group string, revision int64) (*tableCellStyleSelection, error) {
	if tableDefaultPropertyKnown(action.Property) && action.CellRange == nil {
		action.CellRange = &tableCellRange{}
	}
	slide, owner, data, err := tableCellStyleTarget(deck, action)
	if err != nil {
		return nil, err
	}
	return tableCellStyleCapabilitiesForTarget(deck, action, group, revision, slide, owner, data, deck.tableSelectionOwnerColor(action, owner))
}

// Prepared drafts already own the authored scope and its resolved paint. Keep
// capability computation independent of a complete deck/current-index array.
func tableCellStyleCapabilitiesForTarget(deck Deck, action nativeEditorAction, group string, revision int64, slide Slide, owner Element, data TableData, color string) (*tableCellStyleSelection, error) {
	if tableDefaultPropertyKnown(action.Property) && action.CellRange == nil {
		action.CellRange = &tableCellRange{}
	}
	if action.TableData != nil {
		data = *action.TableData
	}
	if err := data.Validate(); err != nil {
		return nil, err
	}
	anchors, err := tableCellRangeAnchors(data, action.CellRange)
	if err != nil {
		return nil, err
	}
	state := &tableCellStyleSelection{selectionCapabilityState: selectionCapabilityState{
		Version: 1, Revision: revision, Context: "table-cells", TargetIDs: []string{owner.ID},
		Properties: map[string]selectionPropertyState{}, Commands: map[string]selectionCommandState{},
	}, CellRange: *action.CellRange, CellCount: len(anchors)}
	groupReason := ""
	if elementGroup(owner) != "" && elementGroup(owner) != group {
		groupReason = "Enter the group to edit cell formatting."
	}
	_, editErr := typographySelection(slide, []string{owner.ID}, group)
	for _, command := range []string{"style.copy", "style.paste", "style.reset"} {
		reason := groupReason
		if command == "style.copy" && len(anchors) != 1 {
			reason = "Select one anchor cell to copy formatting."
		} else if command != "style.copy" && editErr != nil {
			reason = editErr.Error()
		}
		state.Commands[command] = selectionCommandState{Enabled: reason == "", EligibleIDs: []string{owner.ID}, TotalCount: 1, Reason: reason}
	}
	populateTableCellProperties(deck, owner, data, anchors, state, color)
	populateTableDefaultProperties(deck, owner, data, state, color)
	return state, nil
}

var tableCellStyleFields = []string{"fill", "color", "borderColor", "borderWidth", "fontId", "fontSize", "fontWidth", "lineHeight", "align", "vertical", "padding", "bold", "italic", "underline", "borders.left.color", "borders.left.width", "borders.right.color", "borders.right.width", "borders.top.color", "borders.top.width", "borders.bottom.color", "borders.bottom.width"}

func tableCellMetricRange(name string) (low, high float64, ok bool) {
	switch name {
	case "fontSize":
		return 1, 1024, true
	case "fontWidth":
		return 1, 200, true
	case "lineHeight":
		return .5, 4, true
	case "padding", "borderWidth", "borders.left.width", "borders.right.width", "borders.top.width", "borders.bottom.width":
		return 0, 100, true
	}
	return 0, 0, false
}

func tableCellStyleField(key string) bool {
	for _, field := range tableCellStyleFields {
		if key == "cell."+field {
			return true
		}
	}
	return false
}

func validateTableCellStyleSnapshot(snapshot *ObjectStyleSnapshot) error {
	for key, field := range snapshot.Fields {
		if !tableCellStyleField(key) || len(field.Value) > 100 || field.Fallback != "" {
			return fmt.Errorf("unsupported cell style property %q", key)
		}
		if !field.Present {
			if field.Value != "" || field.Token != "" {
				return fmt.Errorf("absent cell style has a value")
			}
			continue
		}
		name := strings.TrimPrefix(key, "cell.")
		color := name == "color" || name == "fill" || name == "borderColor" || strings.HasSuffix(name, ".color")
		if field.Token != "" && (!color || !validThemeColorToken(field.Token)) {
			return fmt.Errorf("invalid cell style colour role")
		}
		if color {
			if _, valid := normalizeHexColour(field.Value); !valid && field.Value != "none" {
				return fmt.Errorf("invalid cell style colour")
			}
			continue
		}
		switch name {
		case "fontId":
			if !validObjectID(field.Value) {
				return fmt.Errorf("invalid cell font")
			}
		case "align":
			if field.Value != "left" && field.Value != "center" && field.Value != "right" && field.Value != "justify" {
				return fmt.Errorf("invalid cell alignment")
			}
		case "vertical":
			if field.Value != "top" && field.Value != "middle" && field.Value != "bottom" {
				return fmt.Errorf("invalid cell vertical alignment")
			}
		case "bold", "italic", "underline":
			if field.Value != "true" && field.Value != "false" {
				return fmt.Errorf("invalid cell emphasis")
			}
		default:
			n, err := strconv.ParseFloat(field.Value, 64)
			low, high, known := tableCellMetricRange(name)
			if !known || err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < low || n > high {
				return fmt.Errorf("invalid cell metric")
			}
		}
	}
	return nil
}

func tableCellRangeAnchors(table TableData, bounds *tableCellRange) ([][2]int, error) {
	if bounds == nil || bounds.R0 < 0 || bounds.C0 < 0 || bounds.R1 < bounds.R0 || bounds.C1 < bounds.C0 || bounds.R1 >= len(table.Rows) || bounds.C1 >= len(table.Columns) {
		return nil, fmt.Errorf("invalid table cell range")
	}
	anchors := [][2]int{}
	for r, row := range table.Cells {
		for c, cell := range row {
			if !cell.Hidden && r <= bounds.R1 && r+max(1, cell.RowSpan) > bounds.R0 && c <= bounds.C1 && c+max(1, cell.ColSpan) > bounds.C0 {
				anchors = append(anchors, [2]int{r, c})
			}
		}
	}
	if len(anchors) == 0 {
		return nil, fmt.Errorf("select a table cell")
	}
	return anchors, nil
}

func captureTableCellStyle(deck Deck, table TableData, bounds *tableCellRange) (*ObjectStyleSnapshot, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}
	anchors, err := tableCellRangeAnchors(table, bounds)
	if err != nil || len(anchors) != 1 {
		return nil, fmt.Errorf("select one anchor cell to copy formatting")
	}
	cell := table.Cells[anchors[0][0]][anchors[0][1]]
	style := TableCellStyle{}
	if cell.Style != nil {
		style = *cell.Style
	}
	raw, _ := json.Marshal(style)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	snapshot := &ObjectStyleSnapshot{Version: 1, Context: "table-cells", ThemeSignature: objectStyleThemeSignature(deck), Fields: map[string]ObjectStyleField{}}
	for _, name := range tableCellStyleFields {
		field := ObjectStyleField{}
		value := fields[name]
		if parts := strings.Split(name, "."); len(parts) == 3 {
			border := style.Borders[parts[1]]
			if parts[2] == "color" && border.Color != "" {
				value, _ = json.Marshal(border.Color)
			} else if parts[2] == "width" && border.Width != nil {
				value, _ = json.Marshal(*border.Width)
			}
		}
		if len(value) > 0 {
			field.Present = true
			if json.Unmarshal(value, &field.Value) != nil {
				field.Value = string(value)
			}
			if strings.HasPrefix(field.Value, "theme:") {
				field.Token = strings.TrimPrefix(field.Value, "theme:")
				field.Value = deck.themeTokenColor(field.Token)
			}
		}
		if name == "bold" || name == "italic" || name == "underline" {
			if len(cell.Runs) > 0 {
				values := []bool{}
				for _, run := range cell.Runs {
					if run.Text == "" {
						continue
					}
					on := run.Bold
					if name == "italic" {
						on = run.Italic
					} else if name == "underline" {
						on = run.Underline
					}
					values = append(values, on)
				}
				mixed := false
				for _, on := range values {
					mixed = mixed || on != values[0]
				}
				if mixed {
					continue // Mixed source emphasis doesn't flatten the destination.
				}
				if len(values) > 0 {
					field = ObjectStyleField{Present: true, Value: strconv.FormatBool(values[0])}
				}
			}
		}
		snapshot.Fields["cell."+name] = field
	}
	omitMixedContinuationStyle(deck, table, anchors[0][0], anchors[0][1], snapshot)
	return snapshot, snapshot.Validate()
}

func applyTableCellStyleData(deck Deck, table TableData, bounds *tableCellRange, snapshot *ObjectStyleSnapshot, reset bool) (TableData, bool, error) {
	if err := table.Validate(); err != nil {
		return TableData{}, false, err
	}
	if !reset {
		if err := snapshot.Validate(); err != nil {
			return TableData{}, false, err
		}
		if snapshot.Context != "table-cells" {
			return TableData{}, false, fmt.Errorf("copy a cell style first")
		}
	}
	anchors, err := tableCellRangeAnchors(table, bounds)
	if err != nil {
		return TableData{}, false, err
	}
	draft := cloneTable(table)
	for _, pos := range anchors {
		cell := &draft.Cells[pos[0]][pos[1]]
		style := TableCellStyle{}
		if cell.Style != nil {
			style = cloneCellStyle(*cell.Style)
		}
		if reset {
			style = TableCellStyle{Extra: cloneJSONExtensions(style.Extra)}
			for i := range cell.Runs {
				cell.Runs[i].Bold, cell.Runs[i].Italic, cell.Runs[i].Underline = false, false, false
				cell.Runs[i].Color, cell.Runs[i].FontID, cell.Runs[i].FontSize = "", "", 0
				cell.Runs[i].ColorToken = ""
				cell.Runs[i].FontRole = ""
				cell.Runs[i].BoldOff = false
			}
		} else {
			raw, _ := json.Marshal(style)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			for key, field := range snapshot.Fields {
				name := strings.TrimPrefix(key, "cell.")
				if name == "bold" || name == "italic" || name == "underline" {
					if len(cell.Runs) == 0 {
						if !field.Present || field.Value != "true" {
							continue
						}
						cell.Runs = []ObjectTextRun{{Text: cell.Text}}
						cell.Text = ""
					}
					for i := range cell.Runs {
						on := field.Present && field.Value == "true"
						switch name {
						case "bold":
							cell.Runs[i].Bold = on
						case "italic":
							cell.Runs[i].Italic = on
						case "underline":
							cell.Runs[i].Underline = on
						}
					}
					continue
				}
				value := field.Value
				if field.Token != "" && deck.themeTokenColor(field.Token) != "" && (deck.Theme != nil || snapshot.ThemeSignature == objectStyleThemeSignature(deck)) {
					value = "theme:" + field.Token
				}
				if parts := strings.Split(name, "."); len(parts) == 3 {
					if style.Borders == nil {
						style.Borders = map[string]TableBorderStyle{}
					}
					border := style.Borders[parts[1]]
					if parts[2] == "color" {
						border.Color = ""
						if field.Present {
							border.Color = value
						}
					} else {
						border.Width = nil
						if field.Present {
							n, _ := strconv.ParseFloat(value, 64)
							border.Width = &n
						}
					}
					if border.Color == "" && border.Width == nil && border.Extra == nil {
						delete(style.Borders, parts[1])
					} else {
						style.Borders[parts[1]] = border
					}
					continue
				}
				delete(fields, name)
				if field.Present {
					if _, _, metric := tableCellMetricRange(name); metric {
						n, _ := strconv.ParseFloat(value, 64)
						fields[name], _ = json.Marshal(n)
					} else {
						fields[name], _ = json.Marshal(value)
					}
				}
			}
			// Side updates and primitive fields are reconstructed together; maps
			// retain destination-only compatible extensions, never source data.
			delete(fields, "borders")
			if len(style.Borders) > 0 {
				fields["borders"], _ = json.Marshal(style.Borders)
			}
			raw, _ = json.Marshal(fields)
			if err := json.Unmarshal(raw, &style); err != nil {
				return TableData{}, false, err
			}
		}
		if len(style.Borders) == 0 {
			style.Borders = nil
		}
		cell.Style = &style
		if reflect.DeepEqual(style, TableCellStyle{}) {
			cell.Style = nil
		}
		fields := map[string]ObjectStyleField{}
		if snapshot != nil {
			fields = snapshot.Fields
		}
		clearTableContinuationProperties(&draft, pos[0], pos[1], fields, reset)
	}
	if err := draft.Validate(); err != nil {
		return TableData{}, false, err
	}
	return draft, !reflect.DeepEqual(table, draft), nil
}

func tableCellStyleTarget(deck Deck, action nativeEditorAction) (Slide, Element, TableData, error) {
	var slide Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			return slide, Element{}, TableData{}, errInvalidEditorAction
		}
		slide = *masterSlideAt(&deck, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			return slide, Element{}, TableData{}, errInvalidEditorAction
		}
		slide = deck.Slides[action.Slide]
	}
	return tableCellStyleTargetFromSlide(deck, action, slide)
}

func tableCellStyleTargetFromSlide(deck Deck, action nativeEditorAction, slide Slide) (Slide, Element, TableData, error) {
	if action.ObjectID == "" || len(action.ObjectIDs) > 0 && (len(action.ObjectIDs) != 1 || action.ObjectIDs[0] != action.ObjectID) {
		return slide, Element{}, TableData{}, fmt.Errorf("select one table")
	}
	var owner Element
	count := 0
	for _, element := range slide.Elements {
		if element.ID == action.ObjectID {
			owner, count = element, count+1
		}
	}
	if count != 1 || owner.Kind != "table" || owner.Inherited || protectedActivityElement(owner) || deck.Objects == nil {
		return slide, owner, TableData{}, fmt.Errorf("table style target is missing or not editable")
	}
	table, ok := deck.Objects.Tables[owner.ObjectID]
	if !ok {
		return slide, owner, TableData{}, fmt.Errorf("table data is missing")
	}
	return slide, owner, table, nil
}

func applyTableCellStyleCommand(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	_, _, table, err := tableCellStyleTarget(*deck, action)
	if err != nil {
		return false, err
	}
	action.TableData = nil // Committed commands never trust a client draft.
	capabilities, err := tableCellStyleCapabilities(*deck, action, editingGroup, 0)
	if err != nil {
		return false, err
	}
	reset := action.Action == "reset-object-style"
	capabilityCommand := "style.paste"
	if reset {
		capabilityCommand = "style.reset"
	}
	if !capabilities.Commands[capabilityCommand].Enabled {
		return false, fmt.Errorf("%s", capabilities.Commands[capabilityCommand].Reason)
	}
	data, changed, err := applyTableCellStyleData(*deck, table, action.CellRange, action.StyleData, reset)
	if err != nil || !changed {
		return changed, err
	}
	command := action
	command.Action, command.TableData, command.Geometry = "set-table", &data, nil
	return applyTableCommand(deck, command, editingGroup)
}
