package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The label belongs to its shape, not to the slide's selection/order list.
// A separate query keeps typography independent of the shape's fill/effects.
type shapeLabelData struct {
	Text  string `json:"text"`
	Query string `json:"query,omitempty"`
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
	v.Set("render", "truetype")
	v.Set("text-align", "center")
	v.Set("text-valign", "middle")
	if !v.Has("fg") {
		v.Set("fg", "#ffffff")
	}
	return Element{Kind: "text", Text: data.Text, Query: v.Encode(), ID: e.ID}, true
}

var shapeLabelColourTag = regexp.MustCompile(`(?i)\[color=#[0-9a-f]{6}\]|\[/color\]`)

func shapeLabelSize(label Element, cols, rows int) (int, int) {
	// Match the fixed-advance TrueType renderer, ignoring colour markup.
	plain := shapeLabelColourTag.ReplaceAllString(label.Text, "")
	q, _ := url.ParseQuery(label.Query)
	size, percent := float64(trueTypeSize(label)), trueTypeWidthPercent(label)/100
	longest := 0.0
	for _, line := range strings.Split(plain, "\n") {
		w := 0.0
		for _, token := range splitEmojiText(line) {
			if token.assetKey != "" {
				w += size*.8*percent + 2*math.Max(1, size/40)
			} else {
				w += float64(len([]rune(token.text))) * size * .8 * .4167 * percent
			}
		}
		longest = math.Max(longest, w)
	}
	w := int(math.Ceil(longest * float64(cols) / 1920))
	h := int(math.Ceil(float64(len(strings.Split(plain, "\n"))) * size * 1.2 * float64(rows) / 1080))
	padding := 1
	if q.Has("outline") {
		padding += int(math.Ceil(size * .035 * float64(cols) / 1920))
	}
	if q.Has("shadow") {
		padding += 2 + max(abs(intQueryDefault(q, "shadow-x", 1)), abs(intQueryDefault(q, "shadow-y", 1)))
	}
	return max(1, w) + 2*padding, max(1, h) + 2*padding
}

func shapeLabelFits(shape string, w, h, tw, th int) bool {
	// Check the actual half-cell silhouette, not its rectangular bounds.
	if tw > w || th > h {
		return false
	}
	left, top := (w-tw)/2, (h-th)/2
	// All supported silhouettes are convex. Testing the rectangle's four
	// corners is sufficient and avoids scanning millions of pixels per edit.
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

func fitShapeLabel(e *Element, cols, rows int) {
	label, ok := shapeLabel(*e)
	if !ok {
		return
	}
	q, _ := url.ParseQuery(e.Query)
	w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
	tw, th := shapeLabelSize(label, cols, rows)
	if shapeLabelFits(shapeName(*e), w, h, tw*2, th*2) {
		return
	}
	nw, nh := fitShapeLabelDimensions(shapeName(*e), w, h, tw*2, th*2)
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
	x := float64(left) + shapeSubcellOffset(q, "shape-offset-x")
	y := float64(top) + shapeSubcellOffset(q, "shape-offset-y")
	for _, k := range []string{"left_pct", "right_pct", "right", "bottom", "align", "valign", "row_delta"} {
		q.Del(k)
	}
	q.Set("left", strconv.Itoa(int(math.Floor(x))))
	q.Set("top", strconv.Itoa(int(math.Floor(y))))
	q.Set("shape-offset-x", strconv.FormatFloat(x-math.Floor(x), 'f', 1, 64))
	q.Set("shape-offset-y", strconv.FormatFloat(y-math.Floor(y), 'f', 1, 64))
	q.Set("width", strconv.FormatFloat(float64(nw)/2, 'f', 1, 64))
	q.Set("height", strconv.FormatFloat(float64(nh)/2, 'f', 1, 64))
	e.Query = q.Encode()
}

func fitSlideShapeLabels(slide *Slide, cols, rows int) {
	for i := range slide.Elements {
		fitShapeLabel(&slide.Elements[i], cols, rows)
	}
}

func shapeLabelLine(e Element, index, row, col, cols, rows int) (Line, bool) {
	label, ok := shapeLabel(e)
	if !ok {
		return Line{}, false
	}
	q, _ := url.ParseQuery(e.Query)
	w, h := shapeLabelSize(label, cols, rows)
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
