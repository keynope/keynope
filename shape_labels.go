package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// The label belongs to its shape, not to the slide's selection/order list.
// A separate query keeps typography independent of the shape's fill/effects.
type shapeLabelData struct {
	Text  string          `json:"text"`
	Query string          `json:"query,omitempty"`
	Extra *jsonExtensions `json:"-"`
}

func (s shapeLabelData) MarshalJSON() ([]byte, error) {
	type stored shapeLabelData
	return encodeJSONExtensions(stored(s), s.Extra)
}

func (s *shapeLabelData) UnmarshalJSON(data []byte) error {
	type stored shapeLabelData
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "text", "query")
	if err != nil {
		return err
	}
	*s = shapeLabelData(value)
	s.Extra = extra
	return nil
}

// Migrate only the nested typography. Shape geometry/paint and unknown label
// extension fields must survive this document-boundary operation unchanged.
func standardShapeLabel(element Element) Element {
	q, _ := url.ParseQuery(element.Query)
	raw, err := base64.StdEncoding.DecodeString(q.Get("shape-label"))
	if err != nil || len(raw) > 65536 {
		return element
	}
	var fields map[string]json.RawMessage
	var label shapeLabelData
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(raw, &label) != nil {
		return element
	}
	values, _ := url.ParseQuery(label.Query)
	if values.Get("render") == "truetype" && !values.Has("font") && !values.Has("glyph") && !values.Has("source") && !values.Has("scale") && !values.Has("text-size") {
		return element
	}
	updated := standardTextElement(Element{Kind: "text", Text: label.Text, Query: label.Query})
	if updated.Query == label.Query {
		return element
	}
	fields["query"], _ = json.Marshal(updated.Query)
	encoded, err := json.Marshal(fields)
	if err != nil {
		return element
	}
	q.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
	element.Query = q.Encode()
	return element
}

func shapeLabel(e Element) (Element, bool) {
	if e.Kind != "shape" {
		return Element{}, false
	}
	q, _ := url.ParseQuery(e.Query)
	raw, err := base64.StdEncoding.DecodeString(q.Get("shape-label"))
	var data shapeLabelData
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &data) != nil || data.Text == "" {
		return Element{}, false
	}
	v, _ := url.ParseQuery(data.Query)
	v = shapeLabelStyleValues(e, v)
	v = concreteTextDefaults(v)
	v.Set("render", "truetype")
	if !v.Has("text-align") {
		alignment := "center"
		if v.Get("align") == "justify" {
			alignment = "justify" // Historical paragraph alias, not box placement.
		}
		v.Set("text-align", alignment)
	}
	if !v.Has("text-valign") {
		v.Set("text-valign", "middle")
	}
	if !v.Has("fg") && !v.Has("theme-fg") && !v.Has("default-color") {
		v.Set("fg", "#ffffff")
	}
	return standardTextElement(Element{Kind: "text", Text: data.Text, Query: v.Encode(), ID: e.ID}), true
}

func shapeLabelSize(label Element, cols, rows int, canvas ...sceneRect) (int, int) {
	canvasWidth, canvasHeight := pptxCanvasDimensions(canvas)
	q, _ := url.ParseQuery(label.Query)
	// Use the same semantic runs and physical font advances as ordinary text.
	// Stripping markup and counting base-font characters misses inline sizes,
	// mixed fonts, tab stops, trailing lines and paragraph spacing.
	text := (Deck{Appearance: &DeckAppearance{Version: 3}}).modernSceneTextFromValues(label, q)
	physicalWidth, physicalHeight := sceneTextNaturalSize(text)
	size := text.Size
	w := int(math.Ceil(physicalWidth * float64(cols) / canvasWidth))
	h := int(math.Ceil(physicalHeight * float64(rows) / canvasHeight))
	paddingX, paddingY := 1, 1
	if q.Has("outline") {
		paddingX += int(math.Ceil(size * .035 * float64(cols) / canvasWidth))
		paddingY += int(math.Ceil(size * .035 * float64(rows) / canvasHeight))
	}
	if q.Has("shadow") {
		padding := 2 + max(abs(intQueryDefault(q, "shadow-x", 1)), abs(intQueryDefault(q, "shadow-y", 1)))
		paddingX += padding
		paddingY += padding
	}
	return max(1, w) + 2*paddingX, max(1, h) + 2*paddingY
}

func shapeLabelFits(shape string, w, h, tw, th int) bool {
	// Check the actual half-cell silhouette, not its rectangular bounds.
	if tw > w || th > h {
		return false
	}
	left, top := (w-tw)/2, (h-th)/2
	// The four legacy silhouettes use this convex-corner fitting rule. New
	// concave/adjustable shapes use their registered content region below.
	for _, y := range []int{top, top + th - 1} {
		for _, x := range []int{left, left + tw - 1} {
			if !shapeCellFilled(shape, x, y, w, h) {
				return false
			}
		}
	}
	return true
}

func fitShapeLabelDimensions(shape string, w, h, tw, th int) (int, int) {
	originalW, originalH := w, h
	if shapeLabelFits(shape, w, h, tw, th) {
		return w, h
	}
	// Test each axis through the centre of the silhouette. When the text
	// height already fits, crossing a curved/sloping edge while typing is
	// horizontal overflow, even if height occupies more of its available space.
	growX := !shapeLabelFits(shape, w, h, tw, 1)
	growY := !shapeLabelFits(shape, w, h, 1, th)
	if !growX && !growY {
		growX = true
	}
	// Initial text may need space on both axes. Satisfy height once, then
	// widen to fit the line; never keep adding height just to fit its length.
	if growX && growY {
		if shape != "square" {
			// Leave clearance at curved tips: a rectangle touching the tip
			// cannot be fitted by any practical amount of horizontal growth.
			h = max(h, th+max(4, th/4))
		}
		for h < 2000 && !shapeLabelFits(shape, max(w, tw), h, 1, th) {
			h++
		}
		growY = false
	}
	for nw, nh := w, h; nw <= 2000 && nh <= 2000; {
		if shapeLabelFits(shape, nw, nh, tw, th) {
			return nw, nh
		}
		if growX {
			nw++
		}
		if growY {
			nh++
		}
	}
	return originalW, originalH
}

func fitShapeLabel(e *Element, cols, rows int, canvas ...sceneRect) {
	sharedShapeLabelFits.fit(e, cols, rows, canvas...)
}

func fitShapeLabelUncached(e *Element, cols, rows int, canvas ...sceneRect) {
	if cols <= 0 || rows <= 0 {
		return
	}
	label, ok := shapeLabel(*e)
	if !ok {
		return
	}
	q, _ := url.ParseQuery(e.Query)
	w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
	canvasWidth, canvasHeight := pptxCanvasDimensions(canvas)
	tw, th := shapeLabelSize(label, cols, rows, canvas...)
	nw, nh := w, h
	if name := shapeName(*e); name == "square" || name == "circle" || name == "triangle" || name == "diamond" {
		if shapeLabelFits(name, w, h, tw*2, th*2) {
			return
		}
		nw, nh = fitShapeLabelDimensions(name, w, h, tw*2, th*2)
	} else {
		// Concave shapes cannot use the old convex-corner predicate. Their
		// registered content region is authoritative and may be off-centre.
		// Rounded regions depend on aspect ratio. Re-evaluate after growth so
		// another layout/port pass cannot grow the same label again.
		spec := shapeSpecFromValues(q)
		settled := false
		for attempt := 0; attempt < 32; attempt++ {
			region := shapeGeometry(spec, (float64(nw)*canvasWidth/float64(cols))/(float64(nh)*canvasHeight/float64(rows))).TextRegion
			if region.Width <= 0 || region.Height <= 0 {
				return
			}
			nextW, nextH := max(nw, int(math.Ceil(float64(tw*2)/region.Width))), max(nh, int(math.Ceil(float64(th*2)/region.Height)))
			if nextW > 2000 || nextH > 2000 {
				return
			}
			if nextW == nw && nextH == nh {
				settled = true
				break
			}
			nw, nh = nextW, nextH
		}
		if !settled {
			return
		}
	}
	if nw == w && nh == h {
		return
	}
	p := parseImagePlacement(e.Query)
	left := placementLeftCol(p, cols, (w+1)/2)
	if !p.hasHorizontalOffset() {
		if p.align == "center" {
			left = (cols - (w+1)/2) / 2
		} else if p.align == "right" {
			left = cols - (w+1)/2
		}
	}
	top := placementTopRow(p, rows, (h+1)/2, 0)
	// Keep the existing top-left corner fixed: text growth extends the right
	// and/or bottom edge. The label is centred again in the expanded shape.
	x, y := preciseShapeAnchor(q, float64(left), float64(top), cols, rows)
	x += shapeSubcellOffset(q, "shape-offset-x") + objectPlacementOffset(q, "x")
	y += shapeSubcellOffset(q, "shape-offset-y") + objectPlacementOffset(q, "y")
	for _, k := range []string{"left_pct", "right_pct", "right", "bottom", "align", "valign", "row_delta"} {
		q.Del(k)
	}
	q.Set("left", strconv.Itoa(int(math.Floor(x))))
	q.Set("top", strconv.Itoa(int(math.Floor(y))))
	for axis, value := range map[string]float64{"x": x, "y": y} {
		fraction := value - math.Floor(value)
		half := math.Floor(fraction*2) / 2
		q.Set("shape-offset-"+axis, strconv.FormatFloat(half, 'f', 1, 64))
		q.Del("object-offset-" + axis)
		if remainder := fraction - half; remainder != 0 {
			q.Set("object-offset-"+axis, strconv.FormatFloat(remainder, 'f', -1, 64))
		}
	}
	// Sampling decides whether an axis must grow, but must not round the
	// authored dimension on the other axis (or create an inherited override).
	if nw != w {
		q.Set("width", strconv.FormatFloat(float64(nw)/2, 'f', 1, 64))
	}
	if nh != h {
		q.Set("height", strconv.FormatFloat(float64(nh)/2, 'f', 1, 64))
	}
	e.Query = q.Encode()
}

func fitSlideShapeLabels(slide *Slide, cols, rows int, canvas ...sceneRect) {
	physical := slide.physicalCanvas()
	if len(canvas) > 0 {
		physical = canvas[0]
	}
	for i := range slide.Elements {
		fitShapeLabel(&slide.Elements[i], cols, rows, physical)
	}
}

func shapeLabelLine(e Element, index, row, col, cols, rows int, canvas ...sceneRect) (Line, bool) {
	label, ok := shapeLabel(e)
	if !ok {
		return Line{}, false
	}
	q, _ := url.ParseQuery(e.Query)
	w, h := shapeLabelSize(label, cols, rows, canvas...)
	left := float64(col) + shapeSubcellOffset(q, "shape-offset-x") + (float64(shapeHalfCells(q, "width", 12))/2-float64(w))/2
	top := float64(row) + shapeSubcellOffset(q, "shape-offset-y") + (float64(shapeHalfCells(q, "height", 6))/2-float64(h))/2
	v, _ := url.ParseQuery(label.Query)
	v.Set("width", strconv.Itoa(w))
	v.Set("height", strconv.Itoa(h))
	// Preserve sub-row placement instead of shifting the label when its
	// centred origin falls between the renderer's integer terminal rows.
	v.Set("shape-label-dy", strconv.FormatFloat(top-math.Round(top), 'f', -1, 64))
	return Line{Element: index, Role: "shape-label", Row: int(math.Round(top)), Col: int(math.Round(left)), Text: strings.Repeat(" ", w), Query: v.Encode()}, true
}
