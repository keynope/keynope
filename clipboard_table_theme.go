package main

import (
	"fmt"
	"strings"
)

// Only source formatting, never a second table/content archive or a hidden
// per-object theme. Entries belong to a selected table owner, not a payload ID.
type clipboardTableThemeFallback struct {
	FontID      string                    `json:"fontId"`
	FontSize    float64                   `json:"fontSize"`
	FontWidth   float64                   `json:"fontWidth"`
	LineHeight  float64                   `json:"lineHeight"`
	BorderColor string                    `json:"borderColor"`
	Colors      map[string]string         `json:"colors,omitempty"`
	Cells       []clipboardTableCellPaint `json:"cells"`
}

type clipboardTableCellPaint struct {
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Fill   string `json:"fill"`
	Color  string `json:"color"`
}

func tableStyleColorValues(table TableData, visit func(string)) {
	style := func(s TableCellStyle) {
		visit(s.Fill)
		visit(s.Color)
		visit(s.BorderColor)
		for _, edge := range s.Borders {
			visit(edge.Color)
		}
	}
	style(table.Style.Cell)
	visit(table.Style.HeaderFill)
	visit(table.Style.BandFill)
	for _, row := range table.Cells {
		for _, cell := range row {
			if cell.Style != nil {
				style(*cell.Style)
			}
			for _, edges := range cell.BorderFragments {
				for _, edge := range edges {
					visit(edge.Color)
				}
			}
		}
	}
}

func captureClipboardTableTheme(deck Deck, owner Element, table TableData) clipboardTableThemeFallback {
	font := deck.Theme.Fonts.Body
	fallback := clipboardTableThemeFallback{FontID: font.FontID, FontSize: font.Size, FontWidth: font.Width, LineHeight: font.LineHeight, BorderColor: deck.Theme.Colors.Line}
	tableStyleColorValues(table, func(value string) {
		if strings.HasPrefix(value, "theme:") {
			if fallback.Colors == nil {
				fallback.Colors = map[string]string{}
			}
			fallback.Colors[value] = deck.tableColor(value)
		}
	})
	color := deck.tableResolvedOwnerColor(Slide{}, owner)
	for r, row := range table.Cells {
		for c, cell := range row {
			if cell.Hidden {
				continue
			}
			format := deck.tableCellFormat(table, r, cell, color)
			fallback.Cells = append(fallback.Cells, clipboardTableCellPaint{Row: r, Column: c, Fill: format.Style.Fill, Color: format.Color})
		}
	}
	return fallback
}

func (fallback clipboardTableThemeFallback) validate(table TableData) error {
	style := TableCellStyle{FontID: fallback.FontID, FontSize: &fallback.FontSize, FontWidth: &fallback.FontWidth, LineHeight: &fallback.LineHeight, BorderColor: fallback.BorderColor}
	if err := style.Validate(); err != nil {
		return err
	}
	if !validBundledFont(fallback.FontID) {
		return fmt.Errorf("invalid clipboard table font")
	}
	if _, ok := normalizeHexColour(fallback.BorderColor); !ok {
		return fmt.Errorf("invalid clipboard table border fallback")
	}
	used := map[string]bool{}
	tableStyleColorValues(table, func(value string) {
		if strings.HasPrefix(value, "theme:") {
			used[value] = true
		}
	})
	if len(used) != len(fallback.Colors) {
		return fmt.Errorf("unrelated clipboard table palette")
	}
	for role, color := range fallback.Colors {
		_, rgb := normalizeHexColour(color)
		if !used[role] || !validThemeColorToken(strings.TrimPrefix(role, "theme:")) || !rgb {
			return fmt.Errorf("invalid clipboard table colour fallback")
		}
	}
	index := 0
	for r, row := range table.Cells {
		for c, cell := range row {
			if cell.Hidden {
				continue
			}
			if index >= len(fallback.Cells) {
				return fmt.Errorf("missing clipboard table cell paint")
			}
			paint := fallback.Cells[index]
			if paint.Row != r || paint.Column != c {
				return fmt.Errorf("invalid clipboard table cell paint owner")
			}
			for _, color := range []string{paint.Fill, paint.Color} {
				_, rgb := normalizeHexColour(color)
				if !rgb && color != "none" {
					return fmt.Errorf("invalid clipboard table cell paint")
				}
			}
			index++
		}
	}
	if index != len(fallback.Cells) {
		return fmt.Errorf("unrelated clipboard table cell paint")
	}
	return nil
}

func applyClipboardTableTheme(table TableData, fallback clipboardTableThemeFallback) TableData {
	table = cloneTable(table)
	resolve := func(value string) string {
		if color, ok := fallback.Colors[value]; ok {
			return color
		}
		return value
	}
	resolveStyle := func(style *TableCellStyle) {
		if style.FontID == "theme" {
			style.FontID = fallback.FontID
		}
		style.Fill, style.Color, style.BorderColor = resolve(style.Fill), resolve(style.Color), resolve(style.BorderColor)
		for side, edge := range style.Borders {
			edge.Color = resolve(edge.Color)
			style.Borders[side] = edge
		}
	}
	resolveStyle(&table.Style.Cell)
	table.Style.HeaderFill, table.Style.BandFill = resolve(table.Style.HeaderFill), resolve(table.Style.BandFill)
	base := &table.Style.Cell
	if base.FontID == "" {
		base.FontID = fallback.FontID
	}
	if base.FontSize == nil {
		size := fallback.FontSize
		base.FontSize = &size
	}
	if base.FontWidth == nil {
		width := fallback.FontWidth
		base.FontWidth = &width
	}
	if base.LineHeight == nil {
		height := fallback.LineHeight
		base.LineHeight = &height
	}
	if base.BorderColor == "" {
		base.BorderColor = fallback.BorderColor
	}
	for r, row := range table.Cells {
		for c := range row {
			cell := &table.Cells[r][c]
			if cell.Style != nil {
				resolveStyle(cell.Style)
			}
			for side, edges := range cell.BorderFragments {
				for i := range edges {
					edges[i].Color = resolve(edges[i].Color)
				}
				cell.BorderFragments[side] = edges
			}
		}
	}
	for _, paint := range fallback.Cells {
		cell := &table.Cells[paint.Row][paint.Column]
		if cell.Style == nil {
			cell.Style = &TableCellStyle{}
		}
		cell.Style.Fill, cell.Style.Color = paint.Fill, paint.Color
	}
	return table
}
