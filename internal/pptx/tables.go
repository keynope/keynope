package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Table is the supported DrawingML subset, in scene units (font size remains
// points). It contains concrete paint, not Keynope authoring/theme references.
type Table struct {
	Rows, Columns  []float64
	Cells          [][]TableCell
	Header, Banded bool
}
type TextRun struct {
	Text, Color string
	FontFace    string
	// Read-time provenance, independent from concrete rendering fallbacks.
	// These are actual source references, never guessed from matching RGB or
	// font names. Writers currently retain concrete paint/font behaviour.
	ColorThemeSlot          string
	FontThemeRole           string // major or minor Latin font; empty means concrete.
	ColorThemeSource        string
	FontThemeSource         string
	FontSize                float64
	Bold, Italic, Underline bool
}
type TableCell struct {
	ColorThemeSlot, ColorThemeSource   string
	FontThemeRole, FontThemeSource     string
	FillThemeSlot, FillThemeSource     string
	BorderThemeSlot, BorderThemeSource string
	Text                               string
	Runs                               []TextRun
	RowSpan, ColSpan                   int
	Hidden                             bool
	FontFace                           string
	FontSize                           float64
	LineHeight                         float64 // Zero leaves Office's default paragraph spacing.
	Color, Fill, BorderColor           string
	BorderWidth, Padding               float64
	Borders                            map[string]TableBorder // Nil uses the legacy uniform border.
	Align, Vertical                    string
}
type TableBorder struct {
	ColorThemeSlot, ColorThemeSource string
	Color                            string
	Width                            float64
}

func (t *Table) Validate() error { return validateTable(t) }

func validateTable(t *Table) error {
	if t == nil || len(t.Rows) < 1 || len(t.Rows) > 100 || len(t.Columns) < 1 || len(t.Columns) > 100 || len(t.Cells) != len(t.Rows) {
		return fmt.Errorf("table requires a rectangular 1–100 track grid")
	}
	for _, tracks := range [][]float64{t.Rows, t.Columns} {
		for _, v := range tracks {
			if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 1e8 {
				return fmt.Errorf("table track must be positive and finite")
			}
		}
	}
	owners := make([][]bool, len(t.Rows))
	for r := range owners {
		owners[r] = make([]bool, len(t.Columns))
		if len(t.Cells[r]) != len(t.Columns) {
			return fmt.Errorf("table cell grid does not match tracks")
		}
	}
	textBytes := 0
	for r, row := range t.Cells {
		for c, cell := range row {
			if len(cell.Borders) > 4 {
				return fmt.Errorf("too many table border sides")
			}
			for side, border := range cell.Borders {
				if side != "left" && side != "right" && side != "top" && side != "bottom" {
					return fmt.Errorf("invalid table border side")
				}
				if !tableColorValid(border.Color) || border.Width < 0 || border.Width > 1e5 || math.IsNaN(border.Width) || math.IsInf(border.Width, 0) {
					return fmt.Errorf("invalid table border")
				}
			}
			if cell.Hidden {
				if !owners[r][c] || cell.Text != "" || len(cell.Runs) > 0 || cell.RowSpan != 0 || cell.ColSpan != 0 {
					return fmt.Errorf("invalid covered table cell")
				}
				continue
			}
			if owners[r][c] || cell.RowSpan < 0 || cell.ColSpan < 0 {
				return fmt.Errorf("overlapping table cells")
			}
			rs, cs := max(1, cell.RowSpan), max(1, cell.ColSpan)
			if rs > len(t.Rows)-r || cs > len(t.Columns)-c {
				return fmt.Errorf("table span exceeds grid")
			}
			for y := r; y < r+rs; y++ {
				for x := c; x < c+cs; x++ {
					if owners[y][x] || (y != r || x != c) && !t.Cells[y][x].Hidden {
						return fmt.Errorf("invalid table merge")
					}
					owners[y][x] = true
				}
			}
			if cell.LineHeight != 0 && (math.IsNaN(cell.LineHeight) || math.IsInf(cell.LineHeight, 0) || cell.LineHeight < .5 || cell.LineHeight > 4) {
				return fmt.Errorf("invalid table line spacing")
			}
			for _, v := range []float64{cell.BorderWidth, cell.Padding, cell.FontSize} {
				if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 1e5 {
					return fmt.Errorf("invalid table cell metric")
				}
			}
			for _, color := range []string{cell.Color, cell.Fill, cell.BorderColor} {
				if !tableColorValid(color) {
					return fmt.Errorf("invalid table colour")
				}
			}
			textBytes += len(cell.Text)
			for _, run := range cell.Runs {
				textBytes += len(run.Text)
				if !tableColorValid(run.Color) {
					return fmt.Errorf("invalid table run colour")
				}
				if run.FontSize < 0 || run.FontSize > 1e5 || math.IsNaN(run.FontSize) || math.IsInf(run.FontSize, 0) {
					return fmt.Errorf("invalid table run font size")
				}
			}
			if textBytes > 4<<20 {
				return fmt.Errorf("table text exceeds limit")
			}
		}
	}
	return nil
}
func tableColorValid(color string) bool {
	if color == "" || color == "none" {
		return true
	}
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(color[1:], 16, 24)
	return err == nil
}
func tablePaint(color string) string {
	if color == "" || color == "none" {
		return `<a:noFill/>`
	}
	return solidFill(color)
}

func tableXML(o Object, id int, linkID string) string {
	t := o.Table
	var b strings.Builder
	fmt.Fprintf(&b, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="%s"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm rot="%d"><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr firstRow="%d" bandRow="%d"/><a:tblGrid>`, id, escapeAttr(objectDisplayName(o)), int64(math.Round(o.Rotation*60000)), emu(o.X), emu(o.Y), emu(o.Width), emu(o.Height), boolNumber(t.Header), boolNumber(t.Banded))
	for _, w := range tableEMUTracks(t.Columns, o.Width) {
		fmt.Fprintf(&b, `<a:gridCol w="%d"/>`, w)
	}
	b.WriteString(`</a:tblGrid>`)
	owners := make([][][2]int, len(t.Rows))
	for r := range owners {
		owners[r] = make([][2]int, len(t.Columns))
	}
	for r, row := range t.Cells {
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
	heights := tableEMUTracks(t.Rows, o.Height)
	for r, row := range t.Cells {
		fmt.Fprintf(&b, `<a:tr h="%d">`, heights[r])
		for c, cell := range row {
			owner := owners[r][c]
			anchor := t.Cells[owner[0]][owner[1]]
			b.WriteString(`<a:tc`)
			if c == owner[1] && anchor.ColSpan > 1 {
				fmt.Fprintf(&b, ` gridSpan="%d"`, anchor.ColSpan)
			}
			if r == owner[0] && anchor.RowSpan > 1 {
				fmt.Fprintf(&b, ` rowSpan="%d"`, anchor.RowSpan)
			}
			if c != owner[1] {
				b.WriteString(` hMerge="1"`)
			}
			if r != owner[0] {
				b.WriteString(` vMerge="1"`)
			}
			b.WriteString(`>`)
			b.WriteString(tableTextBody(cell))
			vertical := "t"
			if cell.Vertical == "middle" {
				vertical = "ctr"
			} else if cell.Vertical == "bottom" {
				vertical = "b"
			}
			padding := emu(cell.Padding)
			fmt.Fprintf(&b, `<a:tcPr marL="%d" marR="%d" marT="%d" marB="%d" anchor="%s">`, padding, padding, padding, padding, vertical)
			for _, side := range [][2]string{{"lnL", "left"}, {"lnR", "right"}, {"lnT", "top"}, {"lnB", "bottom"}} {
				border := TableBorder{Color: cell.BorderColor, Width: cell.BorderWidth}
				if override, ok := cell.Borders[side[1]]; ok {
					border = override
				}
				// Merge continuations retain the owner's perimeter paint, but
				// never paint internal lines through a merged rectangle.
				if cell.Hidden {
					border = TableBorder{Color: "none"}
					perimeter := side[1] == "left" && c == owner[1] || side[1] == "right" && c == owner[1]+max(1, anchor.ColSpan)-1 || side[1] == "top" && r == owner[0] || side[1] == "bottom" && r == owner[0]+max(1, anchor.RowSpan)-1
					if perimeter {
						border = TableBorder{Color: anchor.BorderColor, Width: anchor.BorderWidth}
						if override, ok := anchor.Borders[side[1]]; ok {
							border = override
						}
						if override, ok := cell.Borders[side[1]]; ok {
							border = override
						}
					}
				}
				fmt.Fprintf(&b, `<a:%s w="%d">`, side[0], emu(border.Width))
				if border.Width <= 0 {
					b.WriteString(`<a:noFill/>`)
				} else {
					b.WriteString(tablePaint(border.Color))
				}
				fmt.Fprintf(&b, `</a:%s>`, side[0])
			}
			b.WriteString(tablePaint(cell.Fill))
			b.WriteString(`</a:tcPr></a:tc>`)
		}
		b.WriteString(`</a:tr>`)
	}
	b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return b.String()
}

// Tracks are rounded at accumulated boundaries so their EMU sum exactly
// equals the frame size, rather than introducing independent rounding seams.
func tableEMUTracks(weights []float64, total float64) []int64 {
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	out := make([]int64, len(weights))
	used := 0.0
	previous := int64(0)
	for i, w := range weights {
		used += w
		edge := emu(total * used / sum)
		if i == len(weights)-1 {
			edge = emu(total)
		}
		out[i] = edge - previous
		previous = edge
	}
	return out
}
func tableTextBody(cell TableCell) string {
	runs := cell.Runs
	if len(runs) == 0 {
		runs = []TextRun{{Text: cell.Text}}
	}
	align := "l"
	switch cell.Align {
	case "center":
		align = "ctr"
	case "end", "right":
		align = "r"
	case "justify":
		align = "just"
	}
	face := cell.FontFace
	if face == "" {
		face = "Arial"
	}
	size := cell.FontSize
	if size <= 0 {
		size = 18
	}
	color := cell.Color
	if color == "" {
		color = "#000000"
	}
	var b strings.Builder
	b.WriteString(`<a:txBody><a:bodyPr/><a:lstStyle/>`)
	start := func() {
		fmt.Fprintf(&b, `<a:p><a:pPr algn="%s">`, align)
		if cell.LineHeight > 0 {
			fmt.Fprintf(&b, `<a:lnSpc><a:spcPct val="%d"/></a:lnSpc>`, int(math.Round(cell.LineHeight*100000)))
		}
		fmt.Fprintf(&b, `<a:defRPr sz="%d">%s<a:latin typeface="%s"/></a:defRPr></a:pPr>`, int(math.Round(size*100)), tablePaint(color), escapeAttr(face))
	}
	start()
	for _, run := range runs {
		runFace, runSize := face, size
		if run.FontFace != "" {
			runFace = run.FontFace
		}
		if run.FontSize > 0 {
			runSize = run.FontSize
		}
		parts := strings.Split(run.Text, "\n")
		for i, part := range parts {
			if i > 0 {
				b.WriteString(`</a:p>`)
				start()
			}
			if part == "" {
				continue
			}
			underline := "none"
			if run.Underline {
				underline = "sng"
			}
			paint := color
			if run.Color != "" {
				paint = run.Color
			}
			// DrawingML a:t is xsd:string (whitespace-preserving), not Word's
			// text type with an xml:space attribute. Keep the text bytes as-is.
			fmt.Fprintf(&b, `<a:r><a:rPr lang="en-US" sz="%d" b="%d" i="%d" u="%s">%s<a:latin typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`, int(math.Round(runSize*100)), boolNumber(run.Bold), boolNumber(run.Italic), underline, tablePaint(paint), escapeAttr(runFace), escapeText(part))
		}
	}
	b.WriteString(`</a:p></a:txBody>`)
	return b.String()
}

func parseTableBorders(tc *node, resolver colourResolver, warn func(string, string)) map[string]TableBorder {
	var borders map[string]TableBorder
	pr := findChild(tc, "tcPr")
	for _, side := range [][2]string{{"lnL", "left"}, {"lnR", "right"}, {"lnT", "top"}, {"lnB", "bottom"}} {
		ln := findChild(pr, side[0])
		if ln == nil {
			continue
		}
		border := TableBorder{Color: "none"}
		if solid := findChild(ln, "solidFill"); solid != nil {
			border.Color, _ = resolvedPaint(solid, resolver)
			border.ColorThemeSlot = resolvedThemeColourSlot(solid, resolver)
			border.ColorThemeSource = themeReferenceSource(border.ColorThemeSlot, resolver.colourPart)
			border.Width = .75
		}
		if attr(ln, "w") != "" {
			width, err := strconv.ParseFloat(attr(ln, "w"), 64)
			if err != nil {
				width = math.NaN()
			}
			border.Width = width / 6350
		}
		if borders == nil {
			borders = make(map[string]TableBorder, 4)
		}
		borders[side[1]] = border
		if findChild(ln, "gradFill") != nil || findChild(ln, "prstDash") != nil && attr(findChild(ln, "prstDash"), "val") != "solid" || findChild(ln, "custDash") != nil || attr(ln, "cmpd") != "" && attr(ln, "cmpd") != "sng" {
			warn("table-border-effect-approximated", "A table border gradient, dash or compound stroke was reduced to solid colour.")
		}
	}
	return borders
}

func parseTableFrame(n *node, resolver colourResolver) (*Object, []Warning, error) {
	tbl := findFirst(n, "tbl")
	if tbl == nil {
		return nil, []Warning{{Code: "unsupported-graphic-frame", Message: "A chart or other unsupported graphic frame was not imported."}}, nil
	}
	warnings := []Warning{}
	warningCodes := map[string]bool{}
	id := attr(findFirst(n, "cNvPr"), "id")
	warn := func(code, message string) {
		if warningCodes[code] {
			return
		}
		warningCodes[code] = true
		warnings = append(warnings, Warning{Object: id, Code: code, Message: message})
	}
	t := &Table{}
	grid := children(findChild(tbl, "tblGrid"), "gridCol")
	rows := children(tbl, "tr")
	if len(grid) < 1 || len(grid) > 100 || len(rows) < 1 || len(rows) > 100 {
		return nil, nil, fmt.Errorf("PowerPoint table exceeds the 1–100 track limit")
	}
	metric := func(n *node, name string) (float64, error) {
		v, err := strconv.ParseFloat(attr(n, name), 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 6.35e11 {
			return 0, fmt.Errorf("invalid PowerPoint table track")
		}
		return v / 6350, nil
	}
	for _, col := range grid {
		w, err := metric(col, "w")
		if err != nil {
			return nil, nil, err
		}
		t.Columns = append(t.Columns, w)
	}
	pr := findChild(tbl, "tblPr")
	t.Header = drawingMLBool(attr(pr, "firstRow"))
	t.Banded = drawingMLBool(attr(pr, "bandRow"))
	style := resolvedTableStyle(pr, resolver.parts, warn)
	owners := make([][][2]int, len(rows))
	nodes := make([][]*node, len(rows))
	for r, row := range rows {
		h, err := metric(row, "h")
		if err != nil {
			return nil, nil, err
		}
		t.Rows = append(t.Rows, h)
		nodes[r] = children(row, "tc")
		if len(nodes[r]) != len(grid) {
			return nil, nil, fmt.Errorf("PowerPoint table grid is not rectangular")
		}
		t.Cells = append(t.Cells, make([]TableCell, len(grid)))
		owners[r] = make([][2]int, len(grid))
		for c := range owners[r] {
			owners[r][c] = [2]int{-1, -1}
		}
	}
	span := func(n *node, name string) (int, error) {
		s := attr(n, name)
		if s == "" {
			return 1, nil
		}
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 100 {
			return 0, fmt.Errorf("invalid PowerPoint table span")
		}
		return v, nil
	}
	for r, row := range nodes {
		for c, tc := range row {
			if style != nil {
				tc = styledTableCell(tc, style, pr, r, c, len(rows), len(grid), warn)
			}
			owner := owners[r][c]
			hm, vm := drawingMLBool(attr(tc, "hMerge")), drawingMLBool(attr(tc, "vMerge"))
			if owner[0] >= 0 {
				for _, name := range []string{"rowSpan", "gridSpan"} {
					if _, err := span(tc, name); err != nil {
						return nil, nil, err
					}
				}
				if hm != (c != owner[1]) || vm != (r != owner[0]) {
					return nil, nil, fmt.Errorf("inconsistent PowerPoint table merge continuations")
				}
				if strings.Join(paragraphText(tc), "") != "" {
					return nil, nil, fmt.Errorf("covered PowerPoint table cell contains text")
				}
				borders := parseTableBorders(tc, resolver, warn)
				t.Cells[r][c] = TableCell{Hidden: true, Borders: borders}
				continue
			}
			if hm || vm {
				return nil, nil, fmt.Errorf("orphan PowerPoint table merge continuation")
			}
			rs, err := span(tc, "rowSpan")
			if err != nil {
				return nil, nil, err
			}
			cs, err := span(tc, "gridSpan")
			if err != nil {
				return nil, nil, err
			}
			if rs > len(rows)-r || cs > len(grid)-c {
				return nil, nil, fmt.Errorf("PowerPoint table span exceeds grid")
			}
			for y := r; y < r+rs; y++ {
				for x := c; x < c+cs; x++ {
					if owners[y][x][0] >= 0 {
						return nil, nil, fmt.Errorf("overlapping PowerPoint table spans")
					}
					owners[y][x] = [2]int{r, c}
				}
			}
			cell := parseTableCell(tc, resolver, warn)
			cell.RowSpan, cell.ColSpan = rs, cs
			t.Cells[r][c] = cell
		}
	}
	if err := validateTable(t); err != nil {
		return nil, nil, err
	}
	x, y, w, h, rot := transform(n)
	for _, v := range []float64{x, y, w, h, rot} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, nil, fmt.Errorf("invalid PowerPoint table frame")
		}
	}
	if w <= 0 || h <= 0 {
		return nil, nil, fmt.Errorf("PowerPoint table has empty frame bounds")
	}
	return &Object{ID: id, Name: attr(findFirst(n, "cNvPr"), "name"), Kind: "table", X: x, Y: y, Width: w, Height: h, Rotation: rot, Table: t}, warnings, nil
}

func parseTableCell(tc *node, resolver colourResolver, warn func(string, string)) TableCell {
	// Cell defaults and the first run are different scopes: a styled first
	// word must not become the font/size/colour of the entire editable cell.
	def := findFirst(tc, "defRPr")
	defaultStyle := overlayTextRun(TextRun{}, def, resolver)
	fallbackStyle := overlayTextRun(TextRun{}, findFirst(tc, "rPr"), resolver)
	face, size, color := defaultStyle.FontFace, defaultStyle.FontSize, defaultStyle.Color
	fontRole, fontSource := defaultStyle.FontThemeRole, defaultStyle.FontThemeSource
	colorSlot, colorSource := defaultStyle.ColorThemeSlot, defaultStyle.ColorThemeSource
	if face == "" {
		face = fallbackStyle.FontFace
	}
	if size <= 0 {
		size = fallbackStyle.FontSize
	}
	if color == "" && findChild(def, "noFill") == nil {
		color = fallbackStyle.Color
		// The legacy first-word fallback is concrete, not evidence that the
		// cell default itself was linked to that word's source theme.
	}
	if size <= 0 {
		size = 18
	}
	if face == "" {
		face = "Arial"
	}
	if color == "" {
		color = "#000000"
	}
	if findChild(def, "noFill") != nil {
		color = "none"
	}
	cell := TableCell{FontFace: face, FontSize: size, Color: color, Fill: "none", BorderColor: "none", Padding: 10, Vertical: "top", Align: "start"}
	cell.FontThemeRole, cell.FontThemeSource = fontRole, fontSource
	cell.ColorThemeSlot, cell.ColorThemeSource = colorSlot, colorSource
	cell.Align, _ = textAlignment(tc)
	if cell.Align == "" {
		cell.Align = "start"
	}
	pr := findChild(tc, "tcPr")
	if solid := findChild(pr, "solidFill"); solid != nil {
		cell.Fill, _ = resolvedPaint(solid, resolver)
		cell.FillThemeSlot = resolvedThemeColourSlot(solid, resolver)
		cell.FillThemeSource = themeReferenceSource(cell.FillThemeSlot, resolver.colourPart)
	}
	if findChild(pr, "gradFill") != nil || findChild(pr, "blipFill") != nil {
		warn("table-fill-approximated", "A table-cell gradient or image fill was not retained.")
	}
	switch attr(pr, "anchor") {
	case "ctr":
		cell.Vertical = "middle"
	case "b":
		cell.Vertical = "bottom"
	}
	padding := []float64{10, 10, 10, 10}
	for i, key := range []string{"marL", "marR", "marT", "marB"} {
		if v, err := strconv.ParseFloat(attr(pr, key), 64); err == nil && v >= 0 && !math.IsInf(v, 0) {
			padding[i] = v / 6350
		}
	}
	cell.Padding = padding[0]
	for _, v := range padding[1:] {
		if v != cell.Padding {
			warn("table-margins-approximated", "Unequal table-cell margins use the left margin on every side.")
			break
		}
	}
	cell.Borders = parseTableBorders(tc, resolver, warn)
	for _, side := range []string{"left", "right", "top", "bottom"} {
		if border, ok := cell.Borders[side]; ok {
			cell.BorderColor, cell.BorderWidth = border.Color, border.Width
			cell.BorderThemeSlot, cell.BorderThemeSource = border.ColorThemeSlot, border.ColorThemeSource
			break
		}
	}
	paragraphs := children(findChild(tc, "txBody"), "p")
	for pIndex, p := range paragraphs {
		if v, err := strconv.ParseFloat(attr(findChild(findChild(findChild(p, "pPr"), "lnSpc"), "spcPct"), "val"), 64); err == nil && v >= 50000 && v <= 400000 {
			if pIndex == 0 {
				cell.LineHeight = v / 100000
			} else if math.Abs(cell.LineHeight-v/100000) > 1e-9 {
				warn("table-line-spacing-approximated", "Varying paragraph line spacing uses the first paragraph's spacing in the editable table cell.")
			}
		}
		if pIndex > 0 {
			cell.Runs = append(cell.Runs, TextRun{Text: "\n"})
		}
		def := findChild(findChild(p, "pPr"), "defRPr")
		baseStyle := overlayTextRun(TextRun{}, def, resolver)
		baseFace, baseSize, baseColor, baseBold, baseItalic := baseStyle.FontFace, baseStyle.FontSize, baseStyle.Color, baseStyle.Bold, baseStyle.Italic
		for _, child := range p.Children {
			if child.Name == "br" {
				cell.Runs = append(cell.Runs, TextRun{Text: "\n"})
				continue
			}
			if child.Name != "r" && child.Name != "fld" {
				continue
			}
			rp := findChild(child, "rPr")
			runStyle := overlayTextRun(TextRun{}, rp, resolver)
			rf, rs, rc, rb, ri := runStyle.FontFace, runStyle.FontSize, runStyle.Color, runStyle.Bold, runStyle.Italic
			if rf == "" {
				rf = baseFace
				if baseFace != "" {
					runStyle.FontThemeRole, runStyle.FontThemeSource = baseStyle.FontThemeRole, baseStyle.FontThemeSource
				}
			}
			if rf == "" && (runStyle.FontThemeRole != cell.FontThemeRole || runStyle.FontThemeSource != cell.FontThemeSource) {
				rf = face
			}
			if rs <= 0 {
				rs = baseSize
			}
			if rf == face && runStyle.FontThemeRole == cell.FontThemeRole && runStyle.FontThemeSource == cell.FontThemeSource {
				rf = ""
			}
			if rs == size {
				rs = 0
			}
			if attr(rp, "b") == "" {
				rb = baseBold
			}
			if attr(rp, "i") == "" {
				ri = baseItalic
			}
			if rc == "" {
				rc = baseColor
				if !hasExplicitShapeFill(rp) {
					if baseColor != "" {
						runStyle.ColorThemeSlot, runStyle.ColorThemeSource = baseStyle.ColorThemeSlot, baseStyle.ColorThemeSource
					}
				}
			}
			if findChild(rp, "noFill") != nil {
				rc = "none"
			}
			if rc == "" && (runStyle.ColorThemeSlot != cell.ColorThemeSlot || runStyle.ColorThemeSource != cell.ColorThemeSource) {
				rc = color
			}
			if rc == color && runStyle.ColorThemeSlot == cell.ColorThemeSlot && runStyle.ColorThemeSource == cell.ColorThemeSource {
				rc = ""
			}
			text := ""
			if tn := findChild(child, "t"); tn != nil {
				text = tn.Text
			}
			cell.Runs = append(cell.Runs, TextRun{Text: text, Color: rc, Bold: rb, Italic: ri, Underline: attr(rp, "u") != "" && attr(rp, "u") != "none", FontFace: rf, FontSize: rs, ColorThemeSlot: runStyle.ColorThemeSlot, ColorThemeSource: runStyle.ColorThemeSource, FontThemeRole: runStyle.FontThemeRole, FontThemeSource: runStyle.FontThemeSource})
		}
	}
	var plain strings.Builder
	for _, run := range cell.Runs {
		plain.WriteString(run.Text)
	}
	cell.Text = plain.String()
	return cell
}
