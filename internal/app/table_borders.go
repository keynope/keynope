package app

// Borders use logical track indices, not baked coordinates. Track resizing
// updates their geometry without resolving styles or projecting text again.
type sceneTableBorder struct {
	Axis  string  `json:"axis"` // horizontal or vertical
	Track int     `json:"track"`
	From  int     `json:"from"`
	To    int     `json:"to"`
	Color string  `json:"color"`
	Width float64 `json:"width"`
}

type tableBorderCandidate struct {
	color    string
	width    float64
	priority int // theme=0, table=1, table side=2, cell=3, cell side=4
	owner    int // stable row-major tie break; top/left wins
}

func (c tableBorderCandidate) visible() bool {
	return c.width > 0 && c.color != "" && c.color != "none"
}

func chooseTableBorder(a, b tableBorderCandidate) tableBorderCandidate {
	if a.owner < 0 {
		return b
	}
	if b.owner < 0 {
		return a
	}
	if a.priority != b.priority {
		if b.priority > a.priority {
			return b
		}
		return a
	}
	// Explicit Off wins a tie. Otherwise the wider equal-priority border wins,
	// then the top/left owner. This is independent of painting/z-order.
	if a.visible() != b.visible() {
		if !b.visible() {
			return b
		}
		return a
	}
	if a.width != b.width {
		if b.width > a.width {
			return b
		}
		return a
	}
	if b.owner < a.owner {
		return b
	}
	return a
}

func (deck Deck) tableCellBorder(table TableData, row, column int, side, color string, width float64) tableBorderCandidate {
	candidate := tableBorderCandidate{color: color, width: width, owner: row*len(table.Columns) + column}
	apply := func(color string, width *float64, priority int) {
		if color != "" {
			candidate.color = deck.tableColor(color)
		}
		if width != nil {
			candidate.width = *width
		}
		if color != "" || width != nil {
			candidate.priority = priority
		}
	}
	base := table.Style.Cell
	apply(base.BorderColor, base.BorderWidth, 1)
	edge := base.Borders[side]
	apply(edge.Color, edge.Width, 2)
	if style := table.Cells[row][column].Style; style != nil {
		apply(style.BorderColor, style.BorderWidth, 3)
		edge = style.Borders[side]
		apply(edge.Color, edge.Width, 4)
	}
	return candidate
}

func resolveTableBorders(table TableData, scene *sceneTable, decks ...Deck) []sceneTableBorder {
	deck := Deck{}
	if len(decks) > 0 {
		deck = decks[0]
	}
	rows, cols := len(table.Rows), len(table.Columns)
	owners := make([][]int, rows)
	for r := range owners {
		owners[r] = make([]int, cols)
	}
	candidates := make([][4]tableBorderCandidate, len(scene.Cells))
	for i, cell := range scene.Cells {
		for sideIndex, side := range []string{"left", "right", "top", "bottom"} {
			candidates[i][sideIndex] = deck.tableCellBorder(table, cell.Row, cell.Column, side, cell.BorderColor, cell.BorderWidth)
		}
		for r := cell.Row; r < cell.Row+cell.RowSpan; r++ {
			for c := cell.Column; c < cell.Column+cell.ColSpan; c++ {
				owners[r][c] = i
			}
		}
	}
	absent := tableBorderCandidate{owner: -1}
	result := make([]sceneTableBorder, 0, 2*(rows+cols+2))
	appendEdge := func(axis string, track, offset int, candidate tableBorderCandidate) {
		if !candidate.visible() {
			return
		}
		if len(result) > 0 {
			last := &result[len(result)-1]
			if last.Axis == axis && last.Track == track && last.To == offset && last.Color == candidate.color && last.Width == candidate.width {
				last.To++
				return
			}
		}
		result = append(result, sceneTableBorder{Axis: axis, Track: track, From: offset, To: offset + 1, Color: candidate.color, Width: candidate.width})
	}
	for r := 0; r <= rows; r++ {
		for c := 0; c < cols; c++ {
			a, b := absent, absent
			if r > 0 {
				value := candidates[owners[r-1][c]][3]
				a = deck.tableBorderFragmentAt(value, table, value.owner/cols, value.owner%cols, "bottom", r-1, c)
			}
			if r < rows {
				value := candidates[owners[r][c]][2]
				b = deck.tableBorderFragmentAt(value, table, value.owner/cols, value.owner%cols, "top", r, c)
			}
			if a.owner == b.owner {
				continue
			} // interior of a merged owner
			appendEdge("horizontal", r, c, chooseTableBorder(a, b))
		}
	}
	for c := 0; c <= cols; c++ {
		for r := 0; r < rows; r++ {
			a, b := absent, absent
			if c > 0 {
				value := candidates[owners[r][c-1]][1]
				a = deck.tableBorderFragmentAt(value, table, value.owner/cols, value.owner%cols, "right", r, c-1)
			}
			if c < cols {
				value := candidates[owners[r][c]][0]
				b = deck.tableBorderFragmentAt(value, table, value.owner/cols, value.owner%cols, "left", r, c)
			}
			if a.owner == b.owner {
				continue
			}
			appendEdge("vertical", c, r, chooseTableBorder(a, b))
		}
	}
	return result
}
