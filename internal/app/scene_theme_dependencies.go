package app

import (
	"net/url"
	"reflect"
)

// Compare resolved theme consumers without geometry, text measurement, media
// conversion, JSON or modifying authored values. This is deliberately not a
// hand-maintained list of which colour keys each query happens to inherit: the
// production typography/paint resolvers supply the accepted values, including
// table-cell typography/seams and chart-label/mark inputs.
func sceneThemeInputsEqual(before, after Deck, source Slide, key sceneResponseKey) bool {
	if reflect.DeepEqual(before.Theme, after.Theme) {
		return true
	}
	if before.Theme == nil || after.Theme == nil {
		// Adding/removing portable themes also changes compatibility typography.
		return false
	}
	a, b := before.Theme, after.Theme
	if a.Version != b.Version || !reflect.DeepEqual(a.Extra, b.Extra) ||
		!reflect.DeepEqual(a.Colors.Extra, b.Colors.Extra) ||
		!reflect.DeepEqual(a.Fonts.Extra, b.Fonts.Extra) ||
		!reflect.DeepEqual(a.Fonts.Body.Extra, b.Fonts.Body.Extra) ||
		!reflect.DeepEqual(a.Fonts.Heading.Extra, b.Fonts.Heading.Extra) ||
		!reflect.DeepEqual(a.Fonts.Mono.Extra, b.Fonts.Mono.Extra) ||
		!reflect.DeepEqual(a.Defaults.Extra, b.Defaults.Extra) {
		return false
	}
	old, next := source, source
	if !reflect.DeepEqual(a.Fonts, b.Fonts) {
		old = before.withThemeTypography(withTrueTypeDefaults(source))
		next = after.withThemeTypography(withTrueTypeDefaults(source))
		if !reflect.DeepEqual(old.Elements, next.Elements) {
			return false
		}
	}
	// Palette-only edits need no typography query copies. The shared paint
	// resolver below reads theme-fg directly, including explicit RGB precedence.
	old.ThemeColors, next.ThemeColors = before.themeColors("modern"), after.themeColors("modern")
	// Legacy layout, effects and diagnostics consume the three canvas colours
	// as well as object paint. Explicit slide/master overrides still win.
	if slideBG(old) != slideBG(next) || slideFG(old) != slideFG(next) || slideHeaderFG(old) != slideHeaderFG(next) {
		return false
	}
	cols, rows := key.cols, key.rows
	if cols <= 0 {
		cols = 245
	}
	if rows <= 0 {
		rows = 56
	}
	sx, sy := 1920/float64(cols), 1080/float64(rows)
	for index, element := range old.Elements {
		if !wordThemeInputsEqual(before, after, element) {
			return false
		}
		switch element.Kind {
		case "table", "chart":
			// Their registered payload consumers are checked below after paint.
		case "shape":
			// Unlabelled editable shapes have a latent label in annotation JSON.
			// It inherits Body even before the author types any text.
			if !reflect.DeepEqual(a.Fonts.Body, b.Fonts.Body) {
				return false
			}
		case "text", "heading", "bullet", "code", "text-image", "page-number", "image", "connector":
		default:
			return false // Unknown adapters may consume additional theme fields.
		}
		q, err := url.ParseQuery(element.Query)
		if err != nil {
			return false
		}
		other := next.Elements[index]
		p := before.applyThemeObjectPaint(element, old, q, scenePaintFromValues(element, old, sx, sy, q, nil))
		r := after.applyThemeObjectPaint(other, next, q, scenePaintFromValues(other, next, sx, sy, q, nil))
		if !reflect.DeepEqual(p, r) {
			return false
		}
		if !structuredThemeInputsEqual(before, after, element, old, next, q, p, r, sx, sy, cols, rows) {
			return false
		}
		if label, ok := shapeLabel(element); ok {
			if !wordThemeInputsEqual(before, after, label) {
				return false
			}
			lq := mustQuery(label.Query)
			lp := before.applyThemeObjectPaint(label, old, lq, scenePaintFromValues(label, old, sx, sy, lq, nil))
			lr := after.applyThemeObjectPaint(label, next, lq, scenePaintFromValues(label, next, sx, sy, lq, nil))
			if !reflect.DeepEqual(lp, lr) {
				return false
			}
		}
	}
	return true
}

// Keep the owner-colour inheritance rules identical to scene projection. A
// missing/invalid registered payload remains conservative; valid payloads use
// the same cheap format resolvers as production, without preparing cell text.
func structuredThemeInputsEqual(before, after Deck, element Element, old, next Slide, q url.Values, p, r scenePaint, sx, sy float64, cols, rows int) bool {
	if element.Kind != "table" && element.Kind != "chart" {
		return true
	}
	if before.Objects == nil || after.Objects == nil {
		return false
	}
	oldColor, nextColor := p.Color, r.Color
	inherits := !q.Has("fg") && !q.Has("theme-fg")
	if element.Kind == "table" {
		inherits = inherits && !q.Has("header")
	}
	if inherits && old.FG == "" {
		oldColor = ""
	}
	if inherits && next.FG == "" {
		nextColor = ""
	}
	if element.Kind == "table" {
		data, exists := before.Objects.Tables[element.ObjectID]
		if !exists || !scenePayloadEqual(before.Objects, after.Objects, element.ObjectID, "table") || data.Validate() != nil {
			return false
		}
		return tableThemeInputsEqual(before, after, data, oldColor, nextColor)
	}
	data, exists := before.Objects.Charts[element.ObjectID]
	if !exists || !scenePayloadEqual(before.Objects, after.Objects, element.ObjectID, "chart") || data.Validate() != nil {
		return false
	}
	w, h := 170.0, 36.0
	if value, ok := authoredObjectDimension(q, "width", cols); ok {
		w = value
	}
	if value, ok := authoredObjectDimension(q, "height", rows); ok {
		h = value
	}
	a := before.chartInputs(data, w*sx, h*sy, oldColor, q.Get("modern-font"))
	b := after.chartInputs(data, w*sx, h*sy, nextColor, q.Get("modern-font"))
	return reflect.DeepEqual(a, b)
}

func tableThemeInputsEqual(before, after Deck, data TableData, oldColor, nextColor string) bool {
	// Many cells share default formatting. Resolve each distinct accepted query
	// once, retaining only empty-text style data within this comparison.
	fonts := [2]map[string]*sceneText{{}, {}}
	decks := [2]Deck{before, after}
	textStyle := func(index int, q url.Values) *sceneText {
		key := q.Encode()
		if style := fonts[index][key]; style != nil {
			return style
		}
		style := decks[index].modernSceneTextFromValues(Element{Kind: "text"}, q)
		fonts[index][key] = style
		return style
	}
	// Header/banded classification and the immutable authored style pointer
	// completely determine these formatting inputs. Typical large tables have
	// hundreds of default cells but only three distinct stripe contexts. This
	// request-local memo never caches mutable data across commands.
	type cellInputKey struct {
		stripe                      int
		style                       *TableCellStyle
		fragmentRow, fragmentColumn int
	}
	seen := map[cellInputKey]bool{}
	for row, cells := range data.Cells {
		stripe := 0
		if data.Style.Header && row == 0 {
			stripe = 1
		} else if data.Style.Banded && row%2 == 1 {
			stripe = 2
		}
		for column, cell := range cells {
			if cell.Hidden {
				continue
			}
			// Run roles are cell content, not default/header/stripe formatting.
			// Inspect them before the shared-style memo skips equivalent cells.
			for _, run := range cell.Runs {
				if run.ColorToken != "" && before.themeTokenColor(run.ColorToken) != after.themeTokenColor(run.ColorToken) {
					return false
				}
				if run.FontRole != "" && before.wordFontID(run.FontRole, run.FontID) != after.wordFontID(run.FontRole, run.FontID) {
					return false
				}
			}
			key := cellInputKey{stripe: stripe, style: cell.Style}
			if len(cell.BorderFragments) > 0 || (cell.RowSpan > 1 || cell.ColSpan > 1) && tableHasContinuationBorders(data, row, column) {
				key.fragmentRow, key.fragmentColumn = row+1, column+1
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			a, b := before.tableCellFormat(data, row, cell, oldColor), after.tableCellFormat(data, row, cell, nextColor)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(textStyle(0, a.TextValues), textStyle(1, b.TextValues)) {
				return false
			}
			for _, side := range []string{"left", "right", "top", "bottom"} {
				equal := true
				tableBorderPositions(data, row, column, side, func(er, ec int) {
					aEdge := before.tableCellBorderAt(data, row, column, side, er, ec, a.BorderColor, a.BorderWidth)
					bEdge := after.tableCellBorderAt(data, row, column, side, er, ec, b.BorderColor, b.BorderWidth)
					equal = equal && aEdge == bEdge
				})
				if !equal {
					return false
				}
			}
		}
	}
	return true
}
