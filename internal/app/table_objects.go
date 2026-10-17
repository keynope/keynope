package app

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strings"
)

// Tables are owned authored payloads, not a collection of text-box elements.
// A table edit copies the registry before publishing it and forks shared
// payload references, so editing one instance never changes another instance.
func newTableData(rows, columns int) (TableData, error) {
	if rows < 1 || rows > 100 || columns < 1 || columns > 100 {
		return TableData{}, fmt.Errorf("tables need 1–100 rows and columns")
	}
	table := TableData{Version: 1, Rows: make([]TableRow, rows), Columns: make([]TableColumn, columns), Cells: make([][]TableCell, rows)}
	for r := range table.Rows {
		table.Rows[r].Height = 1
		table.Cells[r] = make([]TableCell, columns)
	}
	for c := range table.Columns {
		table.Columns[c].Width = 1
	}
	table.Style.Header = true
	table.Style.Banded = true
	return table, nil
}

func applyTableCommand(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	var slide *Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			return false, errInvalidEditorAction
		}
		slide = masterSlideAt(deck, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			return false, errInvalidEditorAction
		}
		slide = &deck.Slides[action.Slide]
	}
	if action.Action == "insert-table" {
		rows, columns := action.Rows, action.Cols
		if rows == 0 {
			rows = 3
		}
		if columns == 0 {
			columns = 3
		}
		table, err := newTableData(rows, columns)
		if err != nil {
			return false, err
		}
		objects := deck.Objects.cloneForPayloadReplacement()
		if objects == nil {
			objects = &DeckObjects{Version: 1}
		}
		if objects.Tables == nil {
			objects.Tables = map[string]TableData{}
		}
		payloadID := newStableID("table-data")
		objects.Tables[payloadID] = table
		deck.Objects = objects
		slide.Elements = append(append([]Element(nil), slide.Elements...), Element{Kind: "table", ID: newStableID("table"), ObjectID: payloadID, Query: "left=24&top=10&width=160&height=30"})
		return true, nil
	}
	if action.Action != "set-table" || action.TableData == nil {
		return false, errInvalidEditorAction
	}
	if err := action.TableData.Validate(); err != nil {
		return false, err
	}
	// The same whole-group lock preflight used by other semantic editors.
	if err := requireSelectionObjectCommand(*deck, *slide, action.ObjectID, editingGroup, "table.edit"); err != nil {
		return false, err
	}
	for i, element := range slide.Elements {
		if element.ID != action.ObjectID {
			continue
		}
		if element.Kind != "table" || deck.Objects == nil {
			return false, editorFailure("missing-reference", "", action.ObjectID, fmt.Errorf("table no longer exists"))
		}
		previous, ok := deck.Objects.Tables[element.ObjectID]
		if !ok {
			return false, editorFailure("missing-reference", "", action.ObjectID, fmt.Errorf("table payload is missing"))
		}
		dataChanged := !reflect.DeepEqual(previous, *action.TableData)
		geometryChanged := false
		prepared := *deck
		// Geometry is validated against an isolated authored target. The real
		// deck remains intact for payload consumer counting across all scopes.
		prepared.Slides = []Slide{cloneSlide(*slide)}
		if action.Geometry != nil {
			geometry := action
			geometry.Slide = 0
			geometry.SceneMaster = false
			geometry.ObjectIDs = []string{element.ID}
			geometry.GeometryResize = true
			var err error
			geometryChanged, err = applySelectionGeometry(&prepared, geometry, editingGroup)
			if err != nil {
				return false, err
			}
		}
		if !dataChanged {
			if geometryChanged {
				*slide = prepared.Slides[0]
			}
			return geometryChanged, nil
		}
		objects := deck.Objects.cloneForPayloadReplacement()
		id := element.ObjectID
		// Resolve references across every authored scope, including masters.
		// Element IDs are scoped, not globally unique. Count authored instances
		// rather than filtering by the selected ID (which can recur elsewhere).
		count := 0
		visit := func(s Slide) {
			for _, e := range s.Elements {
				if e.Kind == "table" && e.ObjectID == id {
					count++
				}
			}
		}
		for _, s := range deck.Slides {
			visit(s)
		}
		visit(deck.Masters.Base.Slide)
		for _, layout := range deck.Masters.Layouts {
			visit(layout.Slide)
		}
		if count > 1 {
			id = newStableID("table-data")
		}
		objects.Tables[id] = cloneTable(*action.TableData)
		*slide = prepared.Slides[0]
		slide.Elements[i].ObjectID = id
		deck.Objects = objects
		return true, nil
	}
	return false, fmt.Errorf("table no longer exists")
}

type editorTableData struct {
	ObjectID string    `json:"objectId"`
	Data     TableData `json:"data"`
}

type sceneTable struct {
	Header  bool             `json:"header,omitempty"`
	Banded  bool             `json:"banded,omitempty"`
	Rows    []float64        `json:"rows"`
	Columns []float64        `json:"columns"`
	Cells   []sceneTableCell `json:"cells"`
	// Non-nil, including empty, distinguishes shared seams from legacy scenes
	// whose renderer still uses individual cell CSS borders.
	Borders []sceneTableBorder `json:"borders"`
}
type sceneTableCell struct {
	Row           int        `json:"row"`
	Column        int        `json:"column"`
	RowSpan       int        `json:"rowSpan"`
	ColSpan       int        `json:"colSpan"`
	Header        bool       `json:"header,omitempty"`
	Bounds        sceneRect  `json:"bounds"`
	ContentBounds sceneRect  `json:"contentBounds"`
	Text          *sceneText `json:"text"`
	Paint         scenePaint `json:"paint"`
	Fill          string     `json:"fill,omitempty"`
	BorderColor   string     `json:"borderColor,omitempty"`
	BorderWidth   float64    `json:"borderWidth"`
}

// Semantic tables have no sampled layout lines to supply an integer anchor.
// Resolve every authored edge/alignment directly; subcell offsets are applied
// once by the common scene compositor after projection.
func tableSceneAnchor(q url.Values, width, height float64, cols, rows int) (float64, float64) {
	p := imagePlacementFromValues(q)
	x, y := 24.0, 10.0
	switch {
	case p.leftPct != nil:
		x = math.Round(*p.leftPct * float64(max(0, cols-1)))
	case p.rightPct != nil:
		x = float64(cols) - width - math.Round(*p.rightPct*float64(max(0, cols-1)))
	case p.left != nil:
		x = float64(*p.left)
	case p.right != nil:
		x = float64(cols) - width - float64(*p.right)
	case p.align == "center":
		x = (float64(cols) - width) / 2
	case p.align == "right":
		x = float64(cols) - width
	case p.align == "left":
		x = 0
	}
	switch {
	case p.top != nil:
		y = float64(*p.top)
	case p.bottom != nil:
		y = float64(rows) - height - float64(*p.bottom)
	case p.rowDelta != nil:
		y = float64(*p.rowDelta)
	case p.verticalAlign == "middle":
		y = (float64(rows) - height) / 2
	case p.verticalAlign == "bottom":
		y = float64(rows) - height
	case p.verticalAlign == "top":
		y = 0
	}
	return math.Max(0, x), math.Max(0, y)
}

func normalizedTableTracks(values []float64, total float64) []float64 {
	maximum := 0.0
	for _, value := range values {
		maximum = math.Max(maximum, value)
	}
	sum := 0.0
	for _, value := range values {
		sum += value / maximum
	}
	result := make([]float64, len(values))
	used := 0.0
	for i, value := range values {
		result[i] = total * (value / maximum) / sum
		if i == len(values)-1 {
			result[i] = total - used
		}
		used += result[i]
	}
	return result
}

func mergeTableCellStyle(base TableCellStyle, override *TableCellStyle) TableCellStyle {
	if override == nil {
		return base
	}
	s := *override
	if s.DefaultColor != "" {
		base.DefaultColor = s.DefaultColor
	}
	if s.Fill != "" {
		base.Fill = s.Fill
	}
	if s.Color != "" {
		base.Color = s.Color
	}
	if s.BorderColor != "" {
		base.BorderColor = s.BorderColor
	}
	if s.BorderWidth != nil {
		base.BorderWidth = s.BorderWidth
	}
	if s.FontID != "" {
		base.FontID = s.FontID
	}
	if s.FontSize != nil {
		base.FontSize = s.FontSize
	}
	if s.FontWidth != nil {
		base.FontWidth = s.FontWidth
	}
	if s.LineHeight != nil {
		base.LineHeight = s.LineHeight
	}
	if s.Align != "" {
		base.Align = s.Align
	}
	if s.Vertical != "" {
		base.Vertical = s.Vertical
	}
	if s.Padding != nil {
		base.Padding = s.Padding
	}
	return base
}

func (deck Deck) projectTable(table TableData, bounds sceneRect, color string) (*sceneTable, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}
	result := &sceneTable{Header: table.Style.Header, Banded: table.Style.Banded}
	for _, row := range table.Rows {
		result.Rows = append(result.Rows, row.Height)
	}
	for _, column := range table.Columns {
		result.Columns = append(result.Columns, column.Width)
	}
	result.Rows = normalizedTableTracks(result.Rows, bounds.Height)
	result.Columns = normalizedTableTracks(result.Columns, bounds.Width)
	y := 0.0
	for r, row := range table.Cells {
		x := 0.0
		for c, cell := range row {
			if cell.Hidden {
				x += result.Columns[c]
				continue
			}
			rs, cs := max(1, cell.RowSpan), max(1, cell.ColSpan)
			w, h := 0.0, 0.0
			for _, v := range result.Columns[c : c+cs] {
				w += v
			}
			for _, v := range result.Rows[r : r+rs] {
				h += v
			}
			format := deck.tableCellFormat(table, r, cell, color)
			style, q := format.Style, format.TextValues
			text := cell.Text
			if len(cell.Runs) > 0 {
				var content strings.Builder
				for _, run := range cell.Runs {
					content.WriteString(run.Text)
				}
				text = content.String()
			}
			// The shared projection discovers owned emoji fonts from the actual
			// content, including cells authored as rich runs rather than Text.
			t := deck.modernSceneTextFromValues(Element{Kind: "text", Text: text}, q)
			if len(cell.Runs) > 0 {
				t.Runs = nil
				t.Paragraphs = nil
				for _, run := range cell.Runs {
					t.Runs = append(t.Runs, sceneRun{Text: run.Text, Bold: run.Bold, BoldOff: run.BoldOff, Italic: run.Italic, Underline: run.Underline, Color: run.Color, ColorToken: run.ColorToken, FontID: run.FontID, FontRole: run.FontRole, FontSize: run.FontSize})
				}
			}
			deck.resolveWordColors(t)
			deck.resolveWordFonts(t)
			if style.Align != "" {
				switch style.Align {
				case "left":
					t.Align = "start"
				case "right":
					t.Align = "end"
				default:
					t.Align = style.Align
				}
			}
			if style.Vertical != "" {
				t.Vertical = style.Vertical
			}
			if table.Style.Header && r == 0 {
				for i := range t.Runs {
					if !t.Runs[i].BoldOff {
						t.Runs[i].Bold = true
					}
				}
				for i := range t.Paragraphs {
					for j := range t.Paragraphs[i].Runs {
						if !t.Paragraphs[i].Runs[j].BoldOff {
							t.Paragraphs[i].Runs[j].Bold = true
						}
					}
				}
			}
			padding, border, borderColor, textColor := format.Padding, format.BorderWidth, format.BorderColor, format.Color
			if textColor == "none" {
				textColor = "transparent"
			}
			result.Cells = append(result.Cells, sceneTableCell{Row: r, Column: c, RowSpan: rs, ColSpan: cs, Header: table.Style.Header && r == 0, Bounds: sceneRect{x, y, w, h}, ContentBounds: sceneRect{padding, padding, math.Max(1, w-2*padding), math.Max(1, h-2*padding)}, Text: t, Paint: scenePaint{Color: textColor}, Fill: style.Fill, BorderColor: borderColor, BorderWidth: border})
			x += result.Columns[c]
		}
		y += result.Rows[r]
	}
	result.Borders = resolveTableBorders(table, result, deck)
	return result, nil
}
