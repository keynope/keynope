package app

import (
	"fmt"
	"math"
	"strings"

	"keynope/internal/pptx"
)

func tablePPTXColor(color string) string {
	if color == "" || color == "transparent" || color == "none" {
		return "none"
	}
	return color
}
func sceneTableToPPTX(source *sceneTable) *pptx.Table {
	table, _ := sceneTableToPPTXSegments(source)
	return table
}

func sceneTableToPPTXSegments(source *sceneTable) (*pptx.Table, bool) {
	seams := indexSceneTableBorders(source)
	varying := false
	t := &pptx.Table{Rows: append([]float64(nil), source.Rows...), Columns: append([]float64(nil), source.Columns...), Header: source.Header, Banded: source.Banded, Cells: make([][]pptx.TableCell, len(source.Rows))}
	for r := range t.Cells {
		t.Cells[r] = make([]pptx.TableCell, len(t.Columns))
		for c := range t.Cells[r] {
			t.Cells[r][c].Hidden = true
		}
	}
	for _, cell := range source.Cells {
		convertRun := func(run sceneRun) pptx.TextRun {
			converted := pptx.TextRun{Text: run.Text, Color: run.Color, Bold: run.Bold, Italic: run.Italic, Underline: run.Underline}
			if run.FontID != "" {
				font := *cell.Text
				font.FontID = run.FontID
				converted.FontFace = pptxFontFace(&font)
			}
			if run.FontSize > 0 {
				converted.FontSize = run.FontSize * pptxPointsPerSceneUnit
			}
			return converted
		}
		converted := pptx.TableCell{Text: sceneTextPlain(cell.Text), FontFace: pptxFontFace(cell.Text), FontSize: pptxTextFontSize(cell.Text), LineHeight: cell.Text.LineHeight, RowSpan: cell.RowSpan, ColSpan: cell.ColSpan, Color: tablePPTXColor(cell.Paint.Color), Fill: tablePPTXColor(cell.Fill), BorderColor: tablePPTXColor(cell.BorderColor), BorderWidth: cell.BorderWidth, Padding: cell.ContentBounds.X, Align: cell.Text.Align, Vertical: cell.Text.Vertical}
		var varies bool
		converted.Borders, varies = seams.cellSides(cell)
		varying = varying || varies
		if len(cell.Text.Paragraphs) > 0 {
			for i, p := range cell.Text.Paragraphs {
				if i > 0 {
					converted.Runs = append(converted.Runs, pptx.TextRun{Text: "\n"})
				}
				for _, run := range p.Runs {
					converted.Runs = append(converted.Runs, convertRun(run))
				}
			}
		} else {
			for _, run := range cell.Text.Runs {
				converted.Runs = append(converted.Runs, convertRun(run))
			}
		}
		t.Cells[cell.Row][cell.Column] = converted
		// One border per physical perimeter track, not one widest winner for
		// a merged side. Covered cells keep standard DrawingML continuations;
		// no image fallback, extra line objects or proprietary extensions.
		if seams != nil {
			for _, side := range sceneTableSides(cell) {
				for offset := side.from; offset < side.to; offset++ {
					r, c := cell.Row, offset
					switch side.name {
					case "bottom":
						r = cell.Row + cell.RowSpan - 1
					case "left":
						r, c = offset, cell.Column
					case "right":
						r, c = offset, cell.Column+cell.ColSpan-1
					}
					if r == cell.Row && c == cell.Column {
						continue
					}
					covered := &t.Cells[r][c]
					if covered.Borders == nil {
						covered.Borders = map[string]pptx.TableBorder{}
					}
					covered.Borders[side.name] = seams.at(side.axis, side.track, offset)
				}
			}
		}
	}
	return t, varying
}

type sceneTableSide struct {
	name, axis      string
	track, from, to int
}

func sceneTableSides(cell sceneTableCell) []sceneTableSide {
	return []sceneTableSide{
		{"left", "vertical", cell.Column, cell.Row, cell.Row + cell.RowSpan},
		{"right", "vertical", cell.Column + cell.ColSpan, cell.Row, cell.Row + cell.RowSpan},
		{"top", "horizontal", cell.Row, cell.Column, cell.Column + cell.ColSpan},
		{"bottom", "horizontal", cell.Row + cell.RowSpan, cell.Column, cell.Column + cell.ColSpan},
	}
}

type sceneTableBorderIndex struct{ horizontal, vertical [][]pptx.TableBorder }

// Expand coalesced seams once per table. A 100x100 table uses at most 20,200
// entries; cell/perimeter export is then linear, not a seam scan per cell.
func indexSceneTableBorders(source *sceneTable) *sceneTableBorderIndex {
	if source.Borders == nil {
		return nil
	}
	rows, cols := len(source.Rows), len(source.Columns)
	index := &sceneTableBorderIndex{horizontal: make([][]pptx.TableBorder, rows+1), vertical: make([][]pptx.TableBorder, cols+1)}
	for r := range index.horizontal {
		index.horizontal[r] = make([]pptx.TableBorder, cols)
	}
	for c := range index.vertical {
		index.vertical[c] = make([]pptx.TableBorder, rows)
	}
	for _, edge := range source.Borders {
		tracks := index.horizontal
		if edge.Axis == "vertical" {
			tracks = index.vertical
		}
		for offset := edge.From; offset < edge.To; offset++ {
			tracks[edge.Track][offset] = pptx.TableBorder{Color: tablePPTXColor(edge.Color), Width: edge.Width}
		}
	}
	return index
}

func (index *sceneTableBorderIndex) at(axis string, track, offset int) pptx.TableBorder {
	tracks := index.horizontal
	if axis == "vertical" {
		tracks = index.vertical
	}
	border := tracks[track][offset]
	border.Color = tablePPTXColor(border.Color)
	return border
}

// Anchor defaults describe the first segment; continuation cells hold every
// later perimeter segment. Distinct sides remain concrete, editable borders.
func (index *sceneTableBorderIndex) cellSides(cell sceneTableCell) (map[string]pptx.TableBorder, bool) {
	if index == nil {
		return nil, false
	}
	result := make(map[string]pptx.TableBorder, 4)
	varying := false
	for _, side := range sceneTableSides(cell) {
		first := index.at(side.axis, side.track, side.from)
		for offset := side.from + 1; offset < side.to; offset++ {
			border := index.at(side.axis, side.track, offset)
			varying = varying || !strings.EqualFold(first.Color, border.Color) || first.Width != border.Width
		}
		result[side.name] = first
	}
	return result, varying
}

func pptxTableToData(source *pptx.Table, scale float64, bindings ...*pptxThemeBinding) (TableData, []pptx.Warning, error) {
	if source == nil {
		return TableData{}, nil, fmt.Errorf("table has no cell data")
	}
	if err := source.Validate(); err != nil {
		return TableData{}, nil, err
	}
	t, err := newTableData(len(source.Rows), len(source.Columns))
	if err != nil {
		return t, nil, err
	}
	warnings := []pptx.Warning{}
	warn := func(code, message string) { warnings = append(warnings, pptx.Warning{Code: code, Message: message}) }
	t.Style.Header, t.Style.Banded = source.Header, source.Banded
	binding := pptxBinding(bindings)
	if binding != nil {
		freezePPTXTableDefaults(&t)
	}
	for r, v := range source.Rows {
		t.Rows[r].Height = v
	}
	for c, v := range source.Columns {
		t.Columns[c].Width = v
	}
	metric := func(value, cap float64, name string) float64 {
		result := value * scale
		if result > cap {
			warn("table-metric-clamped", "Imported table "+name+" exceeds the editor limit and was clamped.")
		}
		return math.Min(cap, result)
	}
	owners := make([][][2]int, len(source.Cells))
	for r := range owners {
		owners[r] = make([][2]int, len(source.Columns))
	}
	for r, row := range source.Cells {
		for c, cell := range row {
			if cell.Hidden {
				continue
			}
			for y := r; y < r+max(1, cell.RowSpan); y++ {
				for x := c; x < c+max(1, cell.ColSpan); x++ {
					owners[y][x] = [2]int{r, c}
				}
			}
		}
	}
	for r, row := range source.Cells {
		for c, cell := range row {
			if cell.Hidden {
				converted := TableCell{Hidden: true}
				owner := owners[r][c]
				anchor := source.Cells[owner[0]][owner[1]]
				edges := map[string]TableBorderStyle{}
				for side, edge := range cell.Borders {
					perimeter := side == "left" && c == owner[1] || side == "right" && c == owner[1]+max(1, anchor.ColSpan)-1 || side == "top" && r == owner[0] || side == "bottom" && r == owner[0]+max(1, anchor.RowSpan)-1
					if perimeter {
						width := metric(edge.Width, 100, "border width")
						edges[side] = TableBorderStyle{Color: binding.tableColor(tablePPTXColor(edge.Color), edge.ColorThemeSlot, edge.ColorThemeSource), Width: &width}
					}
				}
				if len(edges) > 0 {
					converted.Style = &TableCellStyle{Borders: edges}
				}
				t.Cells[r][c] = converted
				continue
			}
			align := cell.Align
			switch align {
			case "start", "":
				align = "left"
			case "end":
				align = "right"
			}
			padding, border, size := metric(cell.Padding, 100, "padding"), metric(cell.BorderWidth, 100, "border width"), metric(cell.FontSize*2, 1024, "font size")
			if size <= 0 {
				size = 36 * scale
			}
			style := &TableCellStyle{Fill: cell.Fill, Color: cell.Color, BorderColor: cell.BorderColor, BorderWidth: &border, FontID: importFont(cell.FontFace), FontSize: &size, Align: align, Vertical: cell.Vertical, Padding: &padding}
			if binding != nil {
				style.Fill = binding.tableColor(style.Fill, cell.FillThemeSlot, cell.FillThemeSource)
				style.Color = binding.tableColor(style.Color, cell.ColorThemeSlot, cell.ColorThemeSource)
				style.BorderColor = binding.tableColor(style.BorderColor, cell.BorderThemeSlot, cell.BorderThemeSource)
				if binding.font(cell.FontThemeRole, cell.FontThemeSource, cell.FontFace) == "body" {
					style.FontID = "theme"
				}
			}
			if cell.LineHeight >= .5 && cell.LineHeight <= 4 {
				spacing := cell.LineHeight
				style.LineHeight = &spacing
			}
			if cell.Borders != nil {
				style.Borders = make(map[string]TableBorderStyle, len(cell.Borders))
				for side, edge := range cell.Borders {
					width := metric(edge.Width, 100, "border width")
					style.Borders[side] = TableBorderStyle{Color: binding.tableColor(tablePPTXColor(edge.Color), edge.ColorThemeSlot, edge.ColorThemeSource), Width: &width}
				}
			}
			if style.Fill == "" {
				style.Fill = "none"
			}
			if style.BorderColor == "" {
				style.BorderColor = "none"
			}
			lower := strings.ToLower(cell.FontFace)
			if !strings.Contains(lower, "keynope") && !strings.Contains(lower, "c64") && !strings.Contains(lower, "arial") && !strings.Contains(lower, "courier") && !strings.Contains(lower, "mono") {
				warn("table-font-substituted", "Unsupported table font was replaced by a bundled font.")
			}
			converted := TableCell{Text: cell.Text, RowSpan: cell.RowSpan, ColSpan: cell.ColSpan, Style: style}
			for _, run := range cell.Runs {
				convertedRun := ObjectTextRun{Text: run.Text, Color: run.Color, Bold: run.Bold, BoldOff: source.Header && r == 0 && !run.Bold, Italic: run.Italic, Underline: run.Underline}
				if run.FontFace != "" {
					convertedRun.FontID = importFont(run.FontFace)
				}
				if run.FontSize > 0 {
					convertedRun.FontSize = metric(run.FontSize*2, 1024, "run font size")
				}
				if binding != nil {
					actualColor, actualFont := run.Color, run.FontFace
					if actualColor == "" {
						actualColor = cell.Color
					}
					if actualFont == "" {
						actualFont = cell.FontFace
					}
					if token := binding.color(run.ColorThemeSlot, run.ColorThemeSource, actualColor); token != "" {
						convertedRun.ColorToken = token
						convertedRun.Color = actualColor
					}
					if role := binding.font(run.FontThemeRole, run.FontThemeSource, actualFont); role != "" {
						convertedRun.FontRole = role
						convertedRun.FontID = importFont(actualFont)
					}
				}
				converted.Runs = append(converted.Runs, convertedRun)
			}
			t.Cells[r][c] = converted
		}
	}
	normalizeTableBorderFragments(&t)
	if err := t.Validate(); err != nil {
		return TableData{}, warnings, err
	}
	return t, warnings, nil
}
