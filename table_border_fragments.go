package main

import (
	"fmt"
	"reflect"
	"strings"
)

func cloneTableBorderFragments(source map[string][]TableBorderStyle) map[string][]TableBorderStyle {
	if source == nil {
		return nil
	}
	result := make(map[string][]TableBorderStyle, len(source))
	for side, segments := range source {
		result[side] = make([]TableBorderStyle, len(segments))
		for i, segment := range segments {
			segment.Extra = cloneJSONExtensions(segment.Extra)
			if segment.Width != nil {
				value := *segment.Width
				segment.Width = &value
			}
			result[side][i] = segment
		}
	}
	return result
}

func tableBorderFragmentIndex(row, column int, side string, edgeRow, edgeColumn int) int {
	if side == "left" || side == "right" {
		return edgeRow - row
	}
	return edgeColumn - column
}

// Migrate retained native continuation paint to sparse owner-relative segments.
// This also protects old prototype/clipboard payloads during public sanitizing.
func normalizeTableBorderFragments(table *TableData) {
	for r, cells := range table.Cells {
		for c, cell := range cells {
			if cell.Hidden {
				continue
			}
			for _, side := range []string{"left", "right", "top", "bottom"} {
				tableBorderPositions(*table, r, c, side, func(er, ec int) {
					hidden := &table.Cells[er][ec]
					if !hidden.Hidden || hidden.Style == nil {
						return
					}
					edge, ok := hidden.Style.Borders[side]
					if !ok {
						return
					}
					owner := &table.Cells[r][c]
					if owner.BorderFragments == nil {
						owner.BorderFragments = map[string][]TableBorderStyle{}
					}
					if owner.BorderFragments[side] == nil {
						length := max(1, owner.ColSpan)
						if side == "left" || side == "right" {
							length = max(1, owner.RowSpan)
						}
						owner.BorderFragments[side] = make([]TableBorderStyle, length)
					}
					i := tableBorderFragmentIndex(r, c, side, er, ec)
					// Canonical fragments win if an older covered-slot override
					// coexists. Audience normalization must match editor paint.
					existing := owner.BorderFragments[side][i]
					if existing.Color != "" {
						edge.Color = existing.Color
					}
					if existing.Width != nil {
						edge.Width = existing.Width
					}
					if existing.Extra != nil {
						edge.Extra = existing.Extra
					}
					owner.BorderFragments[side][i] = edge
					delete(hidden.Style.Borders, side)
					if len(hidden.Style.Borders) == 0 {
						hidden.Style.Borders = nil
					}
					if reflect.DeepEqual(*hidden.Style, TableCellStyle{}) {
						hidden.Style = nil
					}
				})
			}
		}
	}
}

// Exterior physical track positions are shared by rendering and property reads.
func tableBorderPositions(table TableData, row, column int, side string, visit func(int, int)) {
	cell := table.Cells[row][column]
	rs, cs := max(1, cell.RowSpan), max(1, cell.ColSpan)
	switch side {
	case "left", "right":
		c := column
		if side == "right" {
			c += cs - 1
		}
		for r := row; r < row+rs; r++ {
			visit(r, c)
		}
	case "top", "bottom":
		r := row
		if side == "bottom" {
			r += rs - 1
		}
		for c := column; c < column+cs; c++ {
			visit(r, c)
		}
	}
}

func (deck Deck) tableCellBorderAt(table TableData, row, column int, side string, edgeRow, edgeColumn int, color string, width float64) tableBorderCandidate {
	value := deck.tableCellBorder(table, row, column, side, color, width)
	return deck.tableBorderFragmentAt(value, table, row, column, side, edgeRow, edgeColumn)
}

func (deck Deck) tableBorderFragmentAt(value tableBorderCandidate, table TableData, row, column int, side string, edgeRow, edgeColumn int) tableBorderCandidate {
	value = deck.tableBorderFragment(value, table.Cells[edgeRow][edgeColumn], side)
	segments := table.Cells[row][column].BorderFragments[side]
	i := tableBorderFragmentIndex(row, column, side, edgeRow, edgeColumn)
	if i >= 0 && i < len(segments) {
		edge := segments[i]
		if edge.Color != "" {
			value.color = deck.tableColor(edge.Color)
		}
		if edge.Width != nil {
			value.width = *edge.Width
		}
		if edge.Color != "" || edge.Width != nil {
			value.priority = 4
		}
	}
	return value
}

func (deck Deck) tableBorderFragment(value tableBorderCandidate, cell TableCell, side string) tableBorderCandidate {
	if !cell.Hidden || cell.Style == nil {
		return value
	}
	edge := cell.Style.Borders[side]
	if edge.Color != "" {
		value.color = deck.tableColor(edge.Color)
	}
	if edge.Width != nil {
		value.width = *edge.Width
	}
	if edge.Color != "" || edge.Width != nil {
		value.priority = 4
	}
	return value
}

func tableBorderAuthoredAt(table TableData, row, column int, side, part string, edgeRow, edgeColumn int) (any, bool) {
	raw, explicit := tableCellAuthoredValue(table.Cells[row][column].Style, "borders."+side+"."+part)
	segments := table.Cells[row][column].BorderFragments[side]
	i := tableBorderFragmentIndex(row, column, side, edgeRow, edgeColumn)
	if i >= 0 && i < len(segments) {
		edge := segments[i]
		if part == "color" && edge.Color != "" {
			return edge.Color, true
		}
		if part == "width" && edge.Width != nil {
			return *edge.Width, true
		}
	}
	cell := table.Cells[edgeRow][edgeColumn]
	if cell.Hidden && cell.Style != nil {
		edge := cell.Style.Borders[side]
		if part == "color" && edge.Color != "" {
			return edge.Color, true
		}
		if part == "width" && edge.Width != nil {
			return *edge.Width, true
		}
	}
	return raw, explicit
}

func tableHasContinuationBorders(table TableData, row, column int) bool {
	if len(table.Cells[row][column].BorderFragments) > 0 {
		return true
	}
	for _, side := range []string{"left", "right", "top", "bottom"} {
		found := false
		tableBorderPositions(table, row, column, side, func(r, c int) {
			cell := table.Cells[r][c]
			found = found || cell.Hidden && cell.Style != nil && len(cell.Style.Borders) > 0
		})
		if found {
			return true
		}
	}
	return false
}

// A whole-side/whole-cell edit replaces only the edited component. Preserve
// widths when changing colour (and vice versa), and keep compatible extensions.
func clearTableContinuationProperties(table *TableData, row, column int, fields map[string]ObjectStyleField, reset bool) {
	for _, side := range []string{"left", "right", "top", "bottom"} {
		_, colorAll := fields["cell.borderColor"]
		_, widthAll := fields["cell.borderWidth"]
		_, colorSide := fields["cell.borders."+side+".color"]
		_, widthSide := fields["cell.borders."+side+".width"]
		clearColor, clearWidth := reset || colorAll || colorSide, reset || widthAll || widthSide
		if !clearColor && !clearWidth {
			continue
		}
		owner := &table.Cells[row][column]
		segments := owner.BorderFragments[side]
		any := false
		for i := range segments {
			if clearColor {
				segments[i].Color = ""
			}
			if clearWidth {
				segments[i].Width = nil
			}
			any = any || segments[i].Color != "" || segments[i].Width != nil || segments[i].Extra != nil
		}
		if !any {
			delete(owner.BorderFragments, side)
		}
		if len(owner.BorderFragments) == 0 {
			owner.BorderFragments = nil
		}
		tableBorderPositions(*table, row, column, side, func(r, c int) {
			cell := &table.Cells[r][c]
			if !cell.Hidden || cell.Style == nil {
				return
			}
			style := cloneCellStyle(*cell.Style)
			edge, exists := style.Borders[side]
			if !exists {
				return
			}
			if clearColor {
				edge.Color = ""
			}
			if clearWidth {
				edge.Width = nil
			}
			if edge.Color == "" && edge.Width == nil && edge.Extra == nil {
				delete(style.Borders, side)
			} else {
				style.Borders[side] = edge
			}
			if len(style.Borders) == 0 {
				style.Borders = nil
			}
			cell.Style = &style
			if reflect.DeepEqual(style, TableCellStyle{}) {
				cell.Style = nil
			}
		})
	}
}

// Flat style transfer must not pretend a varying side has one source value.
// Omit that component (including the uniform fallback) just as mixed rich-word
// emphasis is omitted. Uniform sides and unrelated style remain transferable.
func omitMixedContinuationStyle(deck Deck, table TableData, row, column int, snapshot *ObjectStyleSnapshot) {
	for _, part := range []string{"color", "width"} {
		anyMixed := false
		for _, side := range []string{"left", "right", "top", "bottom"} {
			first := true
			var value any
			var present bool
			mixed := false
			tableBorderPositions(table, row, column, side, func(r, c int) {
				raw, explicit := tableBorderAuthoredAt(table, row, column, side, part, r, c)
				if first {
					value, present, first = raw, explicit, false
				} else {
					mixed = mixed || explicit != present || !reflect.DeepEqual(raw, value)
				}
			})
			key := "cell.borders." + side + "." + part
			if mixed {
				delete(snapshot.Fields, key)
				anyMixed = true
			} else {
				field := ObjectStyleField{Present: present}
				if present {
					field.Value = fmt.Sprint(value)
					if strings.HasPrefix(field.Value, "theme:") {
						field.Token = strings.TrimPrefix(field.Value, "theme:")
						field.Value = deck.themeTokenColor(field.Token)
					}
				}
				snapshot.Fields[key] = field
			}
		}
		if anyMixed {
			name := "border" + strings.ToUpper(part[:1]) + part[1:]
			delete(snapshot.Fields, "cell."+name)
		}
	}
}
