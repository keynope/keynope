package main

import (
	"net/url"
	"strings"
)

// align/valign place the box; text-align/text-valign place its contents.
func textBoxAlignment(element Element) string {
	q, _ := url.ParseQuery(element.Query)
	if value := q.Get("text-align"); value != "" {
		return value
	}
	if q.Get("align") == "justify" {
		return "justify"
	} // Existing decks.
	return "left"
}

func textBoxOffsets(element Element, rows []string, width, row int) (int, int) {
	q, _ := url.ParseQuery(element.Query)
	y := 0
	if q.Get("text-box") == "1" {
		extra := max(0, intQueryDefault(q, "height", len(rows))-len(rows))
		if q.Get("text-valign") == "middle" {
			y = extra / 2
		} else if q.Get("text-valign") == "bottom" {
			y = extra
		}
	}
	x := 0
	align := textBoxAlignment(element)
	if (align == "center" || align == "right") && len(rows) > 0 {
		lineHeight := max(1, editGlyphHeight(element))
		start := max(0, row) / lineHeight * lineHeight
		if start < len(rows) {
			used := maxLineDisplayWidth(rows[start:min(len(rows), start+lineHeight)])
			x = max(0, width-used)
			if align == "center" {
				x /= 2
			}
		}
	}
	return y, x
}

func alignTextBoxRows(element Element, rows []string, width int) []string {
	if !isTrueType(element) && element.Kind != "text" && element.Kind != "text-image" && element.Kind != "heading" && element.Kind != "bullet" && element.Kind != "code" {
		return rows
	}
	if isTrueType(element) {
		return rows
	}
	q, _ := url.ParseQuery(element.Query)
	if q.Get("text-align") == "" && q.Get("text-valign") == "" {
		return rows
	}
	y, _ := textBoxOffsets(element, rows, width, 0)
	out := make([]string, y, len(rows)+y)
	for i, row := range rows {
		_, x := textBoxOffsets(element, rows, width, i)
		out = append(out, strings.Repeat(" ", x)+row)
	}
	if q.Get("text-box") == "1" {
		height := max(1, min(4096, intQueryDefault(q, "height", len(out))))
		if len(out) > height {
			out = out[:height]
		}
		for len(out) < height {
			out = append(out, "")
		}
		for i, row := range out {
			out[i] = padRight(cropANSIVisible(row, width), width)
		}
	}
	return out
}
