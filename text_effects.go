package main

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

type textGradient struct {
	start     rgbColour
	end       rgbColour
	direction string
}

type textShadow struct {
	colour rgbColour
	glyph  rune
	x      int
	y      int
}

type rgbColour struct {
	r int
	g int
	b int
}

type textEffectBounds struct {
	minRow int
	maxRow int
	minCol int
	maxCol int
}

func parseElementTextGradient(query string) (textGradient, bool) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return textGradient{}, false
	}
	start, startOK := parseRGBHex(values.Get("gradient-start"))
	end, endOK := parseRGBHex(values.Get("gradient-end"))
	if !startOK || !endOK {
		return textGradient{}, false
	}
	direction := values.Get("gradient-dir")
	switch direction {
	case "vertical", "diagonal":
	default:
		direction = "horizontal"
	}
	return textGradient{start: start, end: end, direction: direction}, true
}

func parseElementTextShadow(query string) (textShadow, bool) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return textShadow{}, false
	}
	var glyph rune
	switch values.Get("shadow") {
	case "soft":
		glyph = '░'
	case "solid":
		glyph = '█'
	default:
		return textShadow{}, false
	}
	colour, ok := parseRGBHex(values.Get("shadow-color"))
	if !ok {
		colour = rgbColour{r: 255, g: 255, b: 255}
	}
	x := clampInt(intQueryDefault(values, "shadow-x", 1), -4, 4)
	y := clampInt(intQueryDefault(values, "shadow-y", 1), -4, 4)
	if x == 0 && y == 0 {
		x, y = 1, 1
	}
	return textShadow{colour: colour, glyph: glyph, x: x, y: y}, true
}

func parseRGBHex(value string) (rgbColour, bool) {
	r, g, b, ok := parseHexColour(value)
	return rgbColour{r: r, g: g, b: b}, ok
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func textEffectElement(element Element) bool {
	switch element.Kind {
	case "heading", "text", "text-image", "bullet", "page-number", "image":
		return true
	default:
		return false
	}
}

func padTextRowsForShadow(element Element, rows []string) []string {
	if !textEffectElement(element) || len(rows) == 0 {
		return rows
	}
	shadow, ok := parseElementTextShadow(element.Query)
	if !ok {
		return rows
	}
	right := max(0, shadow.x)
	bottom := max(0, shadow.y)
	width := maxLineDisplayWidth(rows)
	paddedWidth := width + right
	out := make([]string, 0, len(rows)+bottom)
	for _, row := range rows {
		visibleWidth := displayWidth(stripANSI(row))
		out = append(out, row+strings.Repeat(" ", max(0, width-visibleWidth)+right))
	}
	for index := 0; index < bottom; index++ {
		out = append(out, strings.Repeat(" ", paddedWidth))
	}
	return out
}

func applyTextElementEffects(lines []Line, slide Slide) []Line {
	if len(lines) == 0 || len(slide.Elements) == 0 {
		return lines
	}
	byElement := map[int][]int{}
	for index, line := range lines {
		if line.Element < 0 || line.Element >= len(slide.Elements) || line.Role == "outline" || line.Role == "shape" {
			continue
		}
		if !textEffectElement(slide.Elements[line.Element]) {
			continue
		}
		byElement[line.Element] = append(byElement[line.Element], index)
	}
	if len(byElement) == 0 {
		return lines
	}

	first := map[int]int{}
	shadows := map[int][]Line{}
	for element, indices := range byElement {
		first[element] = indices[0]
		bounds, ok := textLinesBounds(lines, indices)
		if !ok {
			continue
		}
		if gradient, ok := parseElementTextGradient(slide.Elements[element].Query); ok {
			for _, index := range indices {
				lines[index].Text = gradientTextLine(lines[index], bounds, gradient)
			}
		}
		if shadow, ok := parseElementTextShadow(slide.Elements[element].Query); ok {
			shadows[element] = shadowTextLines(lines, indices, shadow)
		}
	}
	if len(shadows) == 0 {
		return lines
	}

	out := make([]Line, 0, len(lines)+len(shadows)*2)
	for index, line := range lines {
		if first[line.Element] == index {
			out = append(out, shadows[line.Element]...)
		}
		out = append(out, line)
	}
	return out
}

func textLinesBounds(lines []Line, indices []int) (textEffectBounds, bool) {
	bounds := textEffectBounds{minRow: math.MaxInt, minCol: math.MaxInt, maxRow: math.MinInt, maxCol: math.MinInt}
	for _, index := range indices {
		line := lines[index]
		plain := []rune(stripANSI(line.Text))
		for offset, character := range plain {
			if character == ' ' {
				continue
			}
			bounds.minRow = min(bounds.minRow, line.Row)
			bounds.maxRow = max(bounds.maxRow, line.Row)
			bounds.minCol = min(bounds.minCol, line.Col+offset)
			bounds.maxCol = max(bounds.maxCol, line.Col+offset)
		}
	}
	return bounds, bounds.minRow != math.MaxInt
}

func gradientTextLine(line Line, bounds textEffectBounds, gradient textGradient) string {
	plain := []rune(stripANSI(line.Text))
	var out strings.Builder
	for offset, character := range plain {
		if character == ' ' {
			out.WriteRune(character)
			continue
		}
		t := gradientPosition(line.Row, line.Col+offset, bounds, gradient.direction)
		colour := interpolateRGB(gradient.start, gradient.end, t)
		fmt.Fprintf(&out, "\033[38;2;%d;%d;%dm%c", colour.r, colour.g, colour.b, character)
	}
	if out.Len() > 0 {
		out.WriteString("\033[39m")
	}
	return out.String()
}

func gradientPosition(row, col int, bounds textEffectBounds, direction string) float64 {
	var numerator, denominator int
	switch direction {
	case "vertical":
		numerator, denominator = row-bounds.minRow, bounds.maxRow-bounds.minRow
	case "diagonal":
		numerator = row - bounds.minRow + col - bounds.minCol
		denominator = bounds.maxRow - bounds.minRow + bounds.maxCol - bounds.minCol
	default:
		numerator, denominator = col-bounds.minCol, bounds.maxCol-bounds.minCol
	}
	if denominator <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, float64(numerator)/float64(denominator)))
}

func interpolateRGB(start, end rgbColour, position float64) rgbColour {
	mix := func(a, b int) int {
		return int(math.Round(float64(a) + (float64(b)-float64(a))*position))
	}
	return rgbColour{r: mix(start.r, end.r), g: mix(start.g, end.g), b: mix(start.b, end.b)}
}

type textCellPosition struct {
	row int
	col int
}

func shadowTextLines(lines []Line, indices []int, shadow textShadow) []Line {
	occupied := map[textCellPosition]bool{}
	for _, index := range indices {
		line := lines[index]
		for offset, character := range []rune(stripANSI(line.Text)) {
			if character != ' ' {
				occupied[textCellPosition{row: line.Row, col: line.Col + offset}] = true
			}
		}
	}
	shadowCells := map[int]map[int]bool{}
	for position := range occupied {
		target := textCellPosition{row: position.row + shadow.y, col: position.col + shadow.x}
		if occupied[target] {
			continue
		}
		if shadowCells[target.row] == nil {
			shadowCells[target.row] = map[int]bool{}
		}
		shadowCells[target.row][target.col] = true
	}
	rows := make([]int, 0, len(shadowCells))
	for row := range shadowCells {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	out := make([]Line, 0, len(rows))
	for _, row := range rows {
		columns := make([]int, 0, len(shadowCells[row]))
		for column := range shadowCells[row] {
			columns = append(columns, column)
		}
		sort.Ints(columns)
		if len(columns) == 0 {
			continue
		}
		minCol, maxCol := columns[0], columns[len(columns)-1]
		cells := make([]rune, maxCol-minCol+1)
		for index := range cells {
			cells[index] = ' '
		}
		for _, column := range columns {
			cells[column-minCol] = shadow.glyph
		}
		query := removeTextEffectLinkKeys(lines[indices[0]].Query)
		text := fmt.Sprintf("\033[38;2;%d;%d;%dm%s\033[39m", shadow.colour.r, shadow.colour.g, shadow.colour.b, string(cells))
		out = append(out, Line{Text: text, Role: "shadow", Row: row, Col: minCol, Element: lines[indices[0]].Element, Query: query})
	}
	return out
}

func removeTextEffectLinkKeys(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	for _, key := range []string{"link", "url", "slide"} {
		values.Del(key)
	}
	return values.Encode()
}
