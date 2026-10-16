package main

import (
	"math"
	"net/url"
	"strconv"
)

// Subcell remainders complement integer layout anchors without changing text
// wrapping or sampled source geometry. Unlike shape coverage offsets these
// are continuous compositor translations, not half-cell raster operations.
func objectPlacementOffset(q url.Values, axis string) float64 {
	v, err := strconv.ParseFloat(q.Get("object-offset-"+axis), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= 1 {
		return 0
	}
	return v
}

// Resolve precise shape anchors before applying explicit subcell translations.
// Raster rows retain their original grid origin; the compositor translates
// them by the difference, while ports and obstacles use this origin directly.
func preciseShapeAnchor(q url.Values, x, y float64, cols, rows int) (float64, float64) {
	return preciseTextAnchor(q, shapeDimension(q, "width", 12), shapeDimension(q, "height", 6), x, y, cols, rows)
}

// Authored dimensions are continuous; integer layout uses a covering box,
// while the shared compositor consumes the precise wrapping dimensions.
func authoredObjectDimension(q url.Values, key string, limit int) (float64, bool) {
	v, err := strconv.ParseFloat(q.Get(key), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return math.Max(1, math.Min(float64(max(1, limit)), v)), true
}

// The sampled covering layout is page-bounded, but the semantic compositor
// must retain frames crossing an edge (including padded imported pictures).
// Use the existing shape dimension envelope, never an unbounded CSS/canvas size.
func sceneObjectDimension(q url.Values, key string, limit int) (float64, bool) {
	return authoredObjectDimension(q, key, max(limit, 1000))
}

// Preserve anchored edges when replacing a covering grid box with a precise
// text box. Integer-authored boxes retain their historical placement.
func preciseTextAnchor(q url.Values, width, height, x, y float64, cols, rows int) (float64, float64) {
	return preciseTextAnchorForFrame(q, width, height, x, y, cols, rows, false)
}

// Intrinsic frame replacement also re-evaluates integer-grid anchors: its
// measured width can land on an exact grid unit while the old centre rounded.
func preciseTextAnchorForFrame(q url.Values, width, height, x, y float64, cols, rows int, resized bool) (float64, float64) {
	// Covering layout clips negative anchors; physical scene placement must
	// retain them so off-page artwork is clipped, not shifted into the page.
	if raw := q.Get("left"); len(raw) > 0 && raw[0] == '-' {
		if value, err := strconv.Atoi(raw); err == nil {
			x = float64(value)
		}
	}
	if raw := q.Get("top"); len(raw) > 0 && raw[0] == '-' {
		if value, err := strconv.Atoi(raw); err == nil {
			y = float64(value)
		}
	}
	// Integer boxes already have their final anchor from layout. Fractional
	// boxes need placement rules, but never a query encode/decode round trip.
	if !resized && math.Abs(width-math.Round(width)) <= 1e-9 && math.Abs(height-math.Round(height)) <= 1e-9 {
		return x, y
	}
	p := imagePlacementFromValues(q)
	if resized || math.Abs(width-math.Round(width)) > 1e-9 {
		switch {
		case p.leftPct != nil:
		case p.rightPct != nil:
			x = math.Max(0, float64(cols)-width-math.Round(*p.rightPct*float64(max(0, cols-1))))
		case p.left != nil:
		case p.right != nil:
			x = math.Max(0, float64(cols)-width-float64(*p.right))
		case p.align == "center":
			x = (float64(cols) - width) / 2
		case p.align == "right":
			x = float64(cols) - width
		}
	}
	if resized || math.Abs(height-math.Round(height)) > 1e-9 {
		switch {
		case p.top != nil:
		case p.bottom != nil:
			y = float64(rows) - height - float64(*p.bottom)
		case p.rowDelta != nil:
		case p.verticalAlign == "middle":
			y = (float64(rows) - height) / 2
		case p.verticalAlign == "bottom":
			y = float64(rows) - height
		}
	}
	return x, y
}
