package app

import "reflect"

// Exact authored-value equality for immutable history reuse. Never compare
// only encoded sizes, revisions or live object identity. Metadata stays opaque.
func historyTableEqual(a, b TableData) bool {
	if a.Version != b.Version || !historyExtraEqual(a.Extra, b.Extra) || a.Style.Header != b.Style.Header || a.Style.Banded != b.Style.Banded || a.Style.HeaderFill != b.Style.HeaderFill || a.Style.BandFill != b.Style.BandFill || !historyExtraEqual(a.Style.Extra, b.Style.Extra) || !historyCellStyleEqual(a.Style.Cell, b.Style.Cell) {
		return false
	}
	if !historySliceShapeEqual(a.Rows, b.Rows) || !historySliceShapeEqual(a.Columns, b.Columns) || !historySliceShapeEqual(a.Cells, b.Cells) {
		return false
	}
	if !historySameSlice(a.Rows, b.Rows) {
		for i, row := range a.Rows {
			if row.Height != b.Rows[i].Height || !historyExtraEqual(row.Extra, b.Rows[i].Extra) {
				return false
			}
		}
	}
	if !historySameSlice(a.Columns, b.Columns) {
		for i, col := range a.Columns {
			if col.Width != b.Columns[i].Width || !historyExtraEqual(col.Extra, b.Columns[i].Extra) {
				return false
			}
		}
	}
	if historySameSlice(a.Cells, b.Cells) {
		return true
	}
	for r, row := range a.Cells {
		other := b.Cells[r]
		if !historySliceShapeEqual(row, other) {
			return false
		}
		if historySameSlice(row, other) {
			continue
		}
		for c, cell := range row {
			y := other[c]
			if cell.Text != y.Text || cell.RowSpan != y.RowSpan || cell.ColSpan != y.ColSpan || cell.Hidden != y.Hidden || !historyExtraEqual(cell.Extra, y.Extra) || !historyStylePointerEqual(cell.Style, y.Style) || !historyRunSliceEqual(cell.Runs, y.Runs) {
				return false
			}
			if (cell.BorderFragments != nil || y.BorderFragments != nil) && !reflect.DeepEqual(cell.BorderFragments, y.BorderFragments) {
				return false
			}
		}
	}
	return true
}

func historySliceShapeEqual[T any](a, b []T) bool {
	return (a == nil) == (b == nil) && len(a) == len(b)
}

// DeepEqual considers equal-length slices with the same backing position equal,
// including malformed NaNs. Shape must be checked separately before this call.
func historySameSlice[T any](a, b []T) bool {
	return len(a) == 0 || &a[0] == &b[0]
}

func historyExtraEqual(a, b *jsonExtensions) bool {
	return a == b || reflect.DeepEqual(a, b)
}

func historyFloatEqual(a, b *float64) bool {
	return a == b || a != nil && b != nil && *a == *b
}

func historyStylePointerEqual(a, b *TableCellStyle) bool {
	return a == b || a != nil && b != nil && historyCellStyleEqual(*a, *b)
}

func historyCellStyleEqual(a, b TableCellStyle) bool {
	strings := func(s TableCellStyle) [7]string {
		return [7]string{s.DefaultColor, s.Fill, s.Color, s.BorderColor, s.FontID, s.Align, s.Vertical}
	}
	if strings(a) != strings(b) || !historyFloatEqual(a.BorderWidth, b.BorderWidth) || !historyFloatEqual(a.FontSize, b.FontSize) || !historyFloatEqual(a.FontWidth, b.FontWidth) || !historyFloatEqual(a.LineHeight, b.LineHeight) || !historyFloatEqual(a.Padding, b.Padding) || !historyExtraEqual(a.Extra, b.Extra) || (a.Borders == nil) != (b.Borders == nil) || len(a.Borders) != len(b.Borders) {
		return false
	}
	for side, edge := range a.Borders {
		other, exists := b.Borders[side]
		if !exists || edge.Color != other.Color || !historyFloatEqual(edge.Width, other.Width) || !historyExtraEqual(edge.Extra, other.Extra) {
			return false
		}
	}
	return true
}

func historyRunSliceEqual(a, b []ObjectTextRun) bool {
	if !historySliceShapeEqual(a, b) {
		return false
	}
	if historySameSlice(a, b) {
		return true
	}
	for index, run := range a {
		other := b[index]
		if !historyExtraEqual(run.Extra, other.Extra) {
			return false
		}
		run.Extra, other.Extra = nil, nil
		if run != other {
			return false
		}
	}
	return true
}
