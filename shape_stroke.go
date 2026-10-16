package main

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

type shapeStrokeDefaults struct {
	Color string
	Width float64
}

func (deck Deck) applyShapeStrokeDefaults(slide Slide) Slide {
	// This render-only context travels with resolved slides and master previews,
	// so legacy layout, attachment projection and modern scenes share axes.
	slide.canvasWidth, slide.canvasHeight = 0, 0
	if deck.PageSize != nil {
		slide.canvasWidth, slide.canvasHeight = deck.pageDimensions()
	}
	slide.ShapeStrokeDefaults = nil
	if deck.Theme != nil {
		slide.ShapeStrokeDefaults = &shapeStrokeDefaults{deck.Theme.Defaults.ShapeLine, deck.Theme.Defaults.LineWidth}
	}
	return slide
}

// Use the paint resolver rather than a parallel interpretation of inheritance.
// Stroke widths are physical scene pixels, independent of the character grid.
func resolvedShapeStrokeWidth(slide Slide, element Element, q url.Values, width, height int) float64 {
	// This layout-only projection carries stroke defaults, not the whole
	// palette. A valid colour role still has a visible border for port spacing.
	if role := strings.TrimPrefix(q.Get("shape-stroke"), "theme:"); role != q.Get("shape-stroke") && validThemeColorToken(role) {
		q = cloneQueryValues(q)
		q.Set("shape-stroke", "#000000")
	}
	deck := Deck{}
	if defaults := slide.ShapeStrokeDefaults; defaults != nil {
		deck.Theme = &DeckTheme{Defaults: ThemeDefaults{ShapeLine: defaults.Color, LineWidth: defaults.Width}}
	}
	canvas := slide.physicalCanvas()
	paint := scenePaintFromValues(element, slide, canvas.Width/float64(max(1, width)), canvas.Height/float64(max(1, height)), q, nil)
	paint = deck.applyThemeObjectPaint(element, slide, q, paint)
	if paint.Stroke == "" || paint.StrokeWidth <= 0 {
		return 0
	}
	return paint.StrokeWidth
}

type shapeBoundarySegment struct{ a, b connectorPoint }

func shapeBoundarySegments(geometry sceneShapeGeometry, width, height float64) []shapeBoundarySegment {
	var segments []shapeBoundarySegment
	for _, contour := range geometry.Contours {
		for i, a := range contour {
			b := contour[(i+1)%len(contour)]
			segments = append(segments, shapeBoundarySegment{connectorPoint{a.X * width, a.Y * height}, connectorPoint{b.X * width, b.Y * height}})
		}
	}
	return segments
}

func shapeSegmentDistanceSquared(point connectorPoint, segment shapeBoundarySegment) float64 {
	dx, dy := segment.b.X-segment.a.X, segment.b.Y-segment.a.Y
	t := 0.0
	if length := dx*dx + dy*dy; length > 0 {
		t = clampFloat(((point.X-segment.a.X)*dx+(point.Y-segment.a.Y)*dy)/length, 0, 1)
	}
	x, y := point.X-segment.a.X-t*dx, point.Y-segment.a.Y-t*dy
	return x*x + y*y
}

// Intersect the round-joined physical outline with a horizontal/vertical ray.
// Capsule end circles plus the rectangular segment body give exact outer
// boundaries of the shared flattened contour, including sloped/concave edges.
func shapeStrokeAxisExtent(segments []shapeBoundarySegment, cross, radius float64, vertical bool) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	include := func(v float64) { lo = math.Min(lo, v); hi = math.Max(hi, v) }
	for _, segment := range segments {
		a, b := segment.a, segment.b
		if vertical {
			a.X, a.Y = a.Y, a.X
			b.X, b.Y = b.Y, b.X
		}
		for _, endpoint := range []connectorPoint{a, b} {
			if d := cross - endpoint.Y; math.Abs(d) <= radius {
				delta := math.Sqrt(math.Max(0, radius*radius-d*d))
				include(endpoint.X - delta)
				include(endpoint.X + delta)
			}
		}
		dx, dy := b.X-a.X, b.Y-a.Y
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}
		nx, ny := -dy*radius/length, dx*radius/length
		quad := [4]connectorPoint{{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny}, {b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny}}
		for i, p := range quad {
			q := quad[(i+1)%4]
			if p.Y == q.Y {
				if math.Abs(p.Y-cross) < 1e-9 {
					include(p.X)
					include(q.X)
				}
				continue
			}
			if cross >= math.Min(p.Y, q.Y) && cross <= math.Max(p.Y, q.Y) {
				include(p.X + (q.X-p.X)*(cross-p.Y)/(q.Y-p.Y))
			}
		}
	}
	return lo, hi, !math.IsInf(lo, 1)
}

func shapePortsAtStrokeBoundary(ports []shapePort, q url.Values, x, y, stroke float64, width, height int, canvas ...sceneRect) []shapePort {
	if stroke <= 0 {
		return ports
	}
	cw, ch := pptxCanvasDimensions(canvas)
	sx, sy := cw/float64(max(1, width)), ch/float64(max(1, height))
	w, h := shapeDimension(q, "width", 12), shapeDimension(q, "height", 6)
	geometry := shapeGeometry(shapeSpecFromValues(q), w*sx/(h*sy))
	segments := shapeBoundarySegments(geometry, w*sx, h*sy)
	x += shapeSubcellOffset(q, "shape-offset-x") + objectPlacementOffset(q, "x")
	y += shapeSubcellOffset(q, "shape-offset-y") + objectPlacementOffset(q, "y")
	for i := range ports {
		p := &ports[i]
		vertical := p.Side == "top" || p.Side == "bottom"
		cross := (p.Y - y) * sy
		if vertical {
			cross = (p.X - x) * sx
		}
		lo, hi, ok := shapeStrokeAxisExtent(segments, cross, stroke/2, vertical)
		if !ok {
			continue
		}
		switch p.Side {
		case "top":
			p.Y = y + lo/sy
		case "bottom":
			p.Y = y + hi/sy
		case "left":
			p.X = x + lo/sx
		case "right":
			p.X = x + hi/sx
		}
	}
	return ports
}

// Shape strokes are independent of label paint and of the legacy outline
// presets. Missing keys inherit; "none" and width zero are explicit Off.
// A supplied inherited width includes zero (explicit Off). Only callers with
// no theme/default width use the legacy two-pixel fallback for a chosen colour.
func explicitShapeStroke(q url.Values, paint scenePaint, inheritedWidth ...float64) scenePaint {
	if q.Has("shape-stroke") {
		if q.Get("shape-stroke") == "none" {
			paint.Stroke = ""
		} else if color, ok := normalizeHexColour(q.Get("shape-stroke")); ok {
			paint.Stroke = color
		}
	}
	if paint.Stroke != "" && paint.StrokeWidth <= 0 {
		paint.StrokeWidth = 2
		if len(inheritedWidth) > 0 {
			paint.StrokeWidth = inheritedWidth[0]
		}
	}
	if width, err := strconv.ParseFloat(q.Get("shape-stroke-width"), 64); q.Has("shape-stroke-width") && err == nil && !math.IsNaN(width) && !math.IsInf(width, 0) && width >= 0 && width <= 64 {
		paint.StrokeWidth = width
	}
	return paint
}

// Sample the shared boundary in physical scene units. Each painted rectangle
// is a half-cell block (the same quadrants used by the shape fill), not a smooth
// vector outline pasted on Retro artwork. Segment/cell intersections retain
// narrow borders without gaps or scanning every pixel against every segment.
// Consecutive cells coalesce to keep the public derivative small.
func retroShapeStroke(geometry sceneShapeGeometry, bounds sceneRect, width, cw, ch float64) ([]sceneRect, bool) {
	if width <= 0 || cw <= 0 || ch <= 0 || bounds.Width <= 0 || bounds.Height <= 0 {
		return nil, false
	}
	stepX, stepY := cw/2, ch/2
	pad := width / 2
	x0, x1 := int(math.Floor(-pad/stepX)), int(math.Ceil((bounds.Width+pad)/stepX))
	y0, y1 := int(math.Floor(-pad/stepY)), int(math.Ceil((bounds.Height+pad)/stepY))
	reduced := false
	if int64(x1-x0)*int64(y1-y0) > 262144 {
		factor := math.Ceil(math.Sqrt(float64(int64(x1-x0)*int64(y1-y0)) / 262144))
		stepX *= factor
		stepY *= factor
		x0, x1 = int(math.Floor(-pad/stepX)), int(math.Ceil((bounds.Width+pad)/stepX))
		y0, y1 = int(math.Floor(-pad/stepY)), int(math.Ceil((bounds.Height+pad)/stepY))
		reduced = true
	}
	segments := shapeBoundarySegments(geometry, bounds.Width, bounds.Height)
	gridWidth := x1 - x0
	on := make([]bool, gridWidth*(y1-y0))
	for _, s := range segments {
		left := max(x0, int(math.Floor((math.Min(s.a.X, s.b.X)-pad)/stepX)))
		right := min(x1, int(math.Ceil((math.Max(s.a.X, s.b.X)+pad)/stepX)))
		top := max(y0, int(math.Floor((math.Min(s.a.Y, s.b.Y)-pad)/stepY)))
		bottom := min(y1, int(math.Ceil((math.Max(s.a.Y, s.b.Y)+pad)/stepY)))
		// Small curve fragments already have a narrow bounding box; clipping
		// those adds arithmetic without saving meaningful intersection work.
		diagonal := s.a.X != s.b.X && s.a.Y != s.b.Y && right-left > 8
		for y := top; y < bottom; y++ {
			// Only the part of the segment within this row's expanded Y
			// interval can touch a cell. Scanning the full segment rectangle
			// makes long diagonal borders cost proportional to their area.
			start, end := left, right
			if diagonal {
				rowLeft, rowRight := shapeStrokeRowRange(s, float64(y)*stepY, stepY, pad)
				// One conservative cell protects the clipping arithmetic at exact
				// grid boundaries. The unchanged distance test decides coverage.
				start = max(left, int(math.Floor(rowLeft/stepX))-1)
				end = min(right, int(math.Ceil(rowRight/stepX))+1)
			}
			for x := start; x < end; x++ {
				cell := (y-y0)*gridWidth + x - x0
				if !on[cell] && strokeSegmentTouchesCell(s.a, s.b, float64(x)*stepX, float64(y)*stepY, stepX, stepY, pad) {
					on[cell] = true
				}
			}
		}
	}
	result := []sceneRect{}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; {
			if !on[(y-y0)*gridWidth+x-x0] {
				x++
				continue
			}
			start := x
			for x < x1 && on[(y-y0)*gridWidth+x-x0] {
				x++
			}
			result = append(result, sceneRect{X: float64(start) * stepX, Y: float64(y) * stepY, Width: float64(x-start) * stepX, Height: stepY})
		}
	}
	return result, reduced
}

func shapeStrokeRowRange(s shapeBoundarySegment, y, height, radius float64) (float64, float64) {
	lo, hi := 0.0, 1.0
	if dy := s.b.Y - s.a.Y; dy != 0 {
		a := (y - radius - s.a.Y) / dy
		b := (y + height + radius - s.a.Y) / dy
		lo = clampFloat(math.Min(a, b), 0, 1)
		hi = clampFloat(math.Max(a, b), 0, 1)
	}
	dx := s.b.X - s.a.X
	a, b := s.a.X+lo*dx, s.a.X+hi*dx
	return math.Min(a, b) - radius, math.Max(a, b) + radius
}

func strokeSegmentTouchesCell(a, b connectorPoint, x, y, w, h, radius float64) bool {
	// Distance between the segment and the closed cell rectangle. If the
	// segment crosses the rectangle its distance is zero; otherwise the
	// closest points are an endpoint/edge or a corner/segment pair.
	dx, dy := b.X-a.X, b.Y-a.Y
	lo, hi := 0.0, 1.0
	clip := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		t := q / p
		if p < 0 {
			lo = math.Max(lo, t)
		} else {
			hi = math.Min(hi, t)
		}
		return lo <= hi
	}
	if clip(-dx, a.X-x) && clip(dx, x+w-a.X) && clip(-dy, a.Y-y) && clip(dy, y+h-a.Y) {
		return true
	}
	r2 := radius * radius
	for _, p := range []connectorPoint{a, b} {
		vx, vy := p.X-clampFloat(p.X, x, x+w), p.Y-clampFloat(p.Y, y, y+h)
		if vx*vx+vy*vy < r2 {
			return true
		}
	}
	for _, p := range []connectorPoint{{x, y}, {x + w, y}, {x, y + h}, {x + w, y + h}} {
		t := 0.0
		if length := dx*dx + dy*dy; length > 0 {
			t = clampFloat(((p.X-a.X)*dx+(p.Y-a.Y)*dy)/length, 0, 1)
		}
		vx, vy := p.X-a.X-t*dx, p.Y-a.Y-t*dy
		if vx*vx+vy*vy < r2 {
			return true
		}
	}
	return false
}
