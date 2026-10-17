package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ShapeSpec is the normalized, authored geometry contract. Its query codec
// retains legacy names, while the geometry registry resolves them canonically.
type ShapeSpec struct {
	Kind        string             `json:"kind"`
	Adjustments map[string]float64 `json:"adjustments,omitempty"`
}
type shapeAdjustment struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Default float64 `json:"default"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
}
type shapeDefinition struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Category    string            `json:"category"`
	Preset      string            `json:"preset"`
	Adjustments []shapeAdjustment `json:"adjustments,omitempty"`
}

// Preset is the preferred editable Office equivalent, not a unique reverse-map
// key. Data/Preparation/Terminator share geometry with adjustable native forms;
// fixed Office flowchart aliases remain supported separately on import.
var shapeDefinitions = []shapeDefinition{
	{"square", "Rectangle", "Basic", "rect", nil},
	{"rounded", "Rounded rectangle", "Basic", "roundRect", []shapeAdjustment{{"radius", "Corner radius", .16, .01, .5}}},
	{"pill", "Pill", "Basic", "roundRect", nil},
	{"circle", "Ellipse", "Basic", "ellipse", nil},
	{"triangle", "Triangle", "Basic", "triangle", nil},
	{"diamond", "Diamond", "Basic", "diamond", nil},
	{"parallelogram", "Parallelogram", "Basic", "parallelogram", []shapeAdjustment{{"depth", "Slant", .2, .02, .45}}},
	{"trapezoid", "Trapezoid", "Basic", "trapezoid", []shapeAdjustment{{"depth", "Slant", .2, .02, .45}}},
	{"hexagon", "Hexagon", "Basic", "hexagon", []shapeAdjustment{{"depth", "Point depth", .25, .05, .45}}},
	{"process", "Process", "Flowchart", "flowChartProcess", nil},
	{"decision", "Decision", "Flowchart", "flowChartDecision", nil},
	{"data", "Data", "Flowchart", "parallelogram", []shapeAdjustment{{"depth", "Slant", .2, .02, .45}}},
	{"database", "Database", "Flowchart", "flowChartMagneticDisk", []shapeAdjustment{{"depth", "Cap depth", .15, .05, .25}}},
	{"document", "Document", "Flowchart", "flowChartDocument", []shapeAdjustment{{"depth", "Wave depth", .15, .03, .25}}},
	{"terminator", "Terminator", "Flowchart", "roundRect", nil},
	{"preparation", "Preparation", "Flowchart", "hexagon", []shapeAdjustment{{"depth", "Point depth", .2, .05, .45}}},
	{"manual-input", "Manual input", "Flowchart", "flowChartManualInput", []shapeAdjustment{{"depth", "Slope", .2, .03, .4}}},
	{"callout", "Callout", "Communication", "wedgeRectCallout", []shapeAdjustment{{"depth", "Tail depth", .22, .1, .4}}},
	{"speech-bubble", "Speech bubble", "Communication", "wedgeRoundRectCallout", []shapeAdjustment{{"radius", "Corner radius", .12, .01, .25}, {"depth", "Tail depth", .22, .1, .4}}},
	{"brace", "Brace", "Communication", "bracePair", []shapeAdjustment{{"depth", "Brace depth", .15, .05, .3}}},
	{"label", "Label", "Communication", "pentagon", []shapeAdjustment{{"depth", "Point depth", .2, .05, .4}}},
	{"arrow-right", "Right arrow", "Arrows", "rightArrow", []shapeAdjustment{{"depth", "Head depth", .35, .1, .65}, {"neck", "Shaft height", .4, .1, .8}}},
	{"arrow-left", "Left arrow", "Arrows", "leftArrow", []shapeAdjustment{{"depth", "Head depth", .35, .1, .65}, {"neck", "Shaft height", .4, .1, .8}}},
	{"arrow-up", "Up arrow", "Arrows", "upArrow", []shapeAdjustment{{"depth", "Head depth", .35, .1, .65}, {"neck", "Shaft width", .4, .1, .8}}},
	{"arrow-down", "Down arrow", "Arrows", "downArrow", []shapeAdjustment{{"depth", "Head depth", .35, .1, .65}, {"neck", "Shaft width", .4, .1, .8}}},
	{"chevron", "Chevron", "Arrows", "chevron", []shapeAdjustment{{"depth", "Point depth", .25, .05, .45}}},
	{"star", "Star", "Emphasis", "star5", []shapeAdjustment{{"depth", "Inner radius", .45, .2, .8}}},
	{"burst", "Burst", "Emphasis", "irregularSeal1", []shapeAdjustment{{"depth", "Inner radius", .7, .45, .9}}},
	{"cloud", "Cloud", "Emphasis", "cloud", nil},
}

func shapeDefinitionFor(name string) (shapeDefinition, bool) {
	switch strings.ToLower(name) {
	case "rectangle", "rect":
		name = "square"
	case "ellipse":
		name = "circle"
	case "roundrect", "rounded-rectangle":
		name = "rounded"
	default:
		name = strings.ToLower(name)
	}
	for _, d := range shapeDefinitions {
		if d.ID == name {
			return d, true
		}
	}
	return shapeDefinition{}, false
}
func shapeSpecFromValues(q url.Values) ShapeSpec {
	d, ok := shapeDefinitionFor(q.Get("shape"))
	if !ok {
		d, _ = shapeDefinitionFor("circle")
	}
	s := ShapeSpec{Kind: d.ID, Adjustments: map[string]float64{}}
	for _, a := range d.Adjustments {
		v, err := strconv.ParseFloat(q.Get("shape-"+a.Key), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			v = a.Default
		}
		s.Adjustments[a.Key] = clampFloat(v, a.Min, a.Max)
	}
	return s
}

func validateShapeAdjustments(e Element) error {
	if e.Kind != "shape" {
		return nil
	}
	q, err := url.ParseQuery(e.Query)
	if err != nil {
		return err
	}
	if q.Has("shape-fill") && (len(q["shape-fill"]) != 1 || q.Get("shape-fill") != "none" && q.Get("shape-fill") != "solid") {
		return fmt.Errorf("shape fill must be solid or none")
	}
	if q.Has("shape-stroke") && q.Get("shape-stroke") != "none" {
		if _, ok := normalizeHexColour(q.Get("shape-stroke")); !ok {
			return fmt.Errorf("shape stroke must be a hexadecimal RGB colour or none")
		}
	}
	if q.Has("shape-stroke-width") {
		v, err := strconv.ParseFloat(q.Get("shape-stroke-width"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 64 {
			return fmt.Errorf("shape stroke width must be between 0 and 64")
		}
	}
	d, ok := shapeDefinitionFor(q.Get("shape"))
	if !ok {
		return nil
	}
	for _, a := range d.Adjustments {
		key := "shape-" + a.Key
		if !q.Has(key) {
			continue
		}
		v, err := strconv.ParseFloat(q.Get(key), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < a.Min || v > a.Max {
			return fmt.Errorf("%s must be between %g and %g", a.Label, a.Min, a.Max)
		}
	}
	return nil
}

type sceneShapeGeometry struct {
	Spec       ShapeSpec          `json:"spec"`
	Path       string             `json:"path"`
	Details    []string           `json:"details,omitempty"`
	TextRegion sceneRect          `json:"textRegion"`
	Contours   [][]connectorPoint `json:"-"`
	Handles    []sceneShapeHandle `json:"handles,omitempty"`
}

// Unit-space affine tracks describe each adjustment once. The editor projects
// pointer movement back onto these tracks after undoing the object's rotation.
// Geometry, numeric fields and pointer/keyboard adjustments share the same bounds.
type sceneShapeHandle struct {
	Key    string         `json:"key"`
	Label  string         `json:"label"`
	Value  float64        `json:"value"`
	Min    float64        `json:"min"`
	Max    float64        `json:"max"`
	Origin connectorPoint `json:"origin"`
	Vector connectorPoint `json:"vector"`
	Point  connectorPoint `json:"point"`
}

func shapeAdjustmentHandles(spec ShapeSpec, aspect float64) []sceneShapeHandle {
	d, _ := shapeDefinitionFor(spec.Kind)
	var handles []sceneShapeHandle
	for _, a := range d.Adjustments {
		origin, vector := connectorPoint{}, connectorPoint{X: 1}
		switch a.Key {
		case "radius":
			if spec.Kind == "rounded" && aspect > 1 {
				vector.X = 1 / aspect
			}
		case "neck":
			origin, vector = connectorPoint{X: .1, Y: .5}, connectorPoint{Y: -.5}
		case "depth":
			switch spec.Kind {
			case "database", "manual-input":
				vector = connectorPoint{Y: 1}
			case "document":
				origin, vector = connectorPoint{X: 1, Y: 1}, connectorPoint{Y: -1}
			case "callout", "speech-bubble":
				origin, vector = connectorPoint{X: .48, Y: 1}, connectorPoint{Y: -1}
			case "label", "arrow-right", "arrow-left", "arrow-up", "arrow-down":
				origin, vector = connectorPoint{X: 1}, connectorPoint{X: -1}
			case "star", "burst":
				n := 5.0
				if spec.Kind == "burst" {
					n = 12
				}
				angle := -math.Pi/2 + math.Pi/n
				origin, vector = connectorPoint{X: .5, Y: .5}, connectorPoint{X: .5 * math.Cos(angle), Y: .5 * math.Sin(angle)}
			}
		}
		// Use the same reflection/quarter-turn as directional arrow geometry.
		switch spec.Kind {
		case "arrow-left":
			origin.X, vector.X = 1-origin.X, -vector.X
		case "arrow-up":
			origin.X, origin.Y, vector.X, vector.Y = origin.Y, 1-origin.X, vector.Y, -vector.X
		case "arrow-down":
			origin.X, origin.Y, vector.X, vector.Y = 1-origin.Y, origin.X, -vector.Y, vector.X
		}
		v := spec.Adjustments[a.Key]
		handles = append(handles, sceneShapeHandle{Key: a.Key, Label: a.Label, Value: v, Min: a.Min, Max: a.Max, Origin: origin, Vector: vector, Point: connectorPoint{X: origin.X + vector.X*v, Y: origin.Y + vector.Y*v}})
	}
	return handles
}

// The same path commands generate SVG and a bounded flattened silhouette. No
// imported path strings or executable markup are accepted by this registry.
type shapePathBuilder struct {
	text   strings.Builder
	points []connectorPoint
}

func (b *shapePathBuilder) point(command string, p connectorPoint) {
	b.text.WriteString(command + strconv.FormatFloat(p.X, 'f', 6, 64) + " " + strconv.FormatFloat(p.Y, 'f', 6, 64) + " ")
	b.points = append(b.points, p)
}
func (b *shapePathBuilder) move(x, y float64) { b.point("M", connectorPoint{x, y}) }
func (b *shapePathBuilder) line(x, y float64) { b.point("L", connectorPoint{x, y}) }
func (b *shapePathBuilder) curve(x1, y1, x2, y2, x, y float64) {
	p := b.points[len(b.points)-1]
	b.text.WriteString("C")
	for _, v := range []float64{x1, y1, x2, y2, x, y} {
		b.text.WriteString(strconv.FormatFloat(v, 'f', 6, 64) + " ")
	}
	for i := 1; i <= 24; i++ {
		t := float64(i) / 24
		u := 1 - t
		b.points = append(b.points, connectorPoint{u*u*u*p.X + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x, u*u*u*p.Y + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y})
	}
}
func (b *shapePathBuilder) close() { b.text.WriteString("Z ") }
func shapePolygon(points ...connectorPoint) shapePathBuilder {
	var b shapePathBuilder
	for i, p := range points {
		if i == 0 {
			b.move(p.X, p.Y)
		} else {
			b.line(p.X, p.Y)
		}
	}
	b.close()
	return b
}
func roundedShapePath(rx, ry, bottom float64) shapePathBuilder {
	const k = .5522847498307936
	var b shapePathBuilder
	b.move(rx, 0)
	b.line(1-rx, 0)
	b.curve(1-rx+k*rx, 0, 1, ry-k*ry, 1, ry)
	b.line(1, bottom-ry)
	b.curve(1, bottom-ry+k*ry, 1-rx+k*rx, bottom, 1-rx, bottom)
	b.line(rx, bottom)
	b.curve(rx-k*rx, bottom, 0, bottom-ry+k*ry, 0, bottom-ry)
	b.line(0, ry)
	b.curve(0, ry-k*ry, rx-k*rx, 0, rx, 0)
	b.close()
	return b
}
func shapeGeometry(spec ShapeSpec, aspect float64) sceneShapeGeometry {
	d, ok := shapeDefinitionFor(spec.Kind)
	if !ok {
		d, _ = shapeDefinitionFor("circle")
	}
	spec.Kind = d.ID
	values := url.Values{"shape": {spec.Kind}}
	for k, v := range spec.Adjustments {
		values.Set("shape-"+k, strconv.FormatFloat(v, 'f', -1, 64))
	}
	spec = shapeSpecFromValues(values)
	g := sceneShapeGeometry{Spec: spec, TextRegion: sceneRect{.08, .08, .84, .84}}
	depth, neck, radius := spec.Adjustments["depth"], spec.Adjustments["neck"], spec.Adjustments["radius"]
	var p shapePathBuilder
	switch spec.Kind {
	case "square", "process":
		p = shapePolygon(connectorPoint{0, 0}, connectorPoint{1, 0}, connectorPoint{1, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{0, 0, 1, 1}
	case "rounded", "pill", "terminator":
		if spec.Kind != "rounded" {
			radius = .5
		}
		rx, ry := radius, radius
		if aspect > 1 {
			rx = radius / aspect
		} else if aspect > 0 {
			ry = radius * aspect
		}
		p = roundedShapePath(rx, ry, 1)
		g.TextRegion = sceneRect{rx * .3, ry * .3, 1 - rx*.6, 1 - ry*.6}
	case "circle":
		p = roundedShapePath(.5, .5, 1)
		g.TextRegion = sceneRect{(1 - 1/math.Sqrt2) / 2, (1 - 1/math.Sqrt2) / 2, 1 / math.Sqrt2, 1 / math.Sqrt2}
	case "triangle":
		p = shapePolygon(connectorPoint{.5, 0}, connectorPoint{1, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{1.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 3}
	case "diamond", "decision":
		p = shapePolygon(connectorPoint{.5, 0}, connectorPoint{1, .5}, connectorPoint{.5, 1}, connectorPoint{0, .5})
		g.TextRegion = sceneRect{.25, .25, .5, .5}
	case "parallelogram", "data":
		p = shapePolygon(connectorPoint{depth, 0}, connectorPoint{1, 0}, connectorPoint{1 - depth, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{depth, .05, 1 - 2*depth, .9}
	case "trapezoid":
		p = shapePolygon(connectorPoint{depth, 0}, connectorPoint{1 - depth, 0}, connectorPoint{1, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{depth, .05, 1 - 2*depth, .9}
	case "hexagon", "preparation":
		p = shapePolygon(connectorPoint{depth, 0}, connectorPoint{1 - depth, 0}, connectorPoint{1, .5}, connectorPoint{1 - depth, 1}, connectorPoint{depth, 1}, connectorPoint{0, .5})
		g.TextRegion = sceneRect{depth, .05, 1 - 2*depth, .9}
	case "manual-input":
		p = shapePolygon(connectorPoint{0, depth}, connectorPoint{1, 0}, connectorPoint{1, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{.05, depth, .9, 1 - depth - .05}
	case "database":
		p.move(0, depth)
		p.curve(0, 0, 1, 0, 1, depth)
		p.line(1, 1-depth)
		p.curve(1, 1, 0, 1, 0, 1-depth)
		p.close()
		var top shapePathBuilder
		top.move(0, depth)
		top.curve(0, 2*depth, 1, 2*depth, 1, depth)
		g.Details = []string{top.text.String()}
		g.TextRegion = sceneRect{.05, 2 * depth, .9, 1 - 3*depth}
	case "document":
		p.move(0, 0)
		p.line(1, 0)
		p.line(1, 1-depth)
		p.curve(.75, 1-3*depth, .25, 1+depth, 0, 1-depth)
		p.close()
		g.TextRegion = sceneRect{.05, .05, .9, 1 - 2*depth - .05}
	case "callout", "speech-bubble":
		bottom := 1 - depth
		if spec.Kind == "speech-bubble" {
			p = roundedShapePath(radius, radius, bottom)
			p.text.Reset()
			p.points = nil
			p.move(radius, 0)
			p.line(1-radius, 0)
			p.curve(1, 0, 1, 0, 1, radius)
			p.line(1, bottom-radius)
			p.curve(1, bottom, 1, bottom, 1-radius, bottom)
		} else {
			p.move(0, 0)
			p.line(1, 0)
			p.line(1, bottom)
		}
		p.line(.48, bottom)
		p.line(.2, 1)
		p.line(.3, bottom)
		if spec.Kind == "speech-bubble" {
			p.line(radius, bottom)
			p.curve(0, bottom, 0, bottom, 0, bottom-radius)
			p.line(0, radius)
			p.curve(0, 0, 0, 0, radius, 0)
		} else {
			p.line(0, bottom)
		}
		p.close()
		g.TextRegion = sceneRect{.08, .08, .84, bottom - .16}
	case "label":
		p = shapePolygon(connectorPoint{0, 0}, connectorPoint{1 - depth, 0}, connectorPoint{1, .5}, connectorPoint{1 - depth, 1}, connectorPoint{0, 1})
		g.TextRegion = sceneRect{.05, .05, 1 - depth - .1, .9}
	case "brace":
		// A brace pair leaves a deliberate label interior between its two sides.
		p.move(depth, 0)
		p.curve(0, 0, depth, .4, 0, .5)
		p.curve(depth, .6, 0, 1, depth, 1)
		p.line(depth+.04, 1)
		p.curve(.04, 1, depth+.04, .6, .04, .5)
		p.curve(depth+.04, .4, .04, 0, depth+.04, 0)
		p.close()
		left := append([]connectorPoint(nil), p.points...)
		var right shapePathBuilder
		for i := len(left) - 1; i >= 0; i-- {
			v := left[i]
			if i == len(left)-1 {
				right.move(1-v.X, v.Y)
			} else {
				right.line(1-v.X, v.Y)
			}
		}
		right.close()
		g.Contours = append(g.Contours, left, right.points)
		p.text.WriteString(right.text.String())
		g.TextRegion = sceneRect{depth + .06, .05, 1 - 2*depth - .12, .9}
	case "arrow-right", "arrow-left", "arrow-up", "arrow-down":
		a := (1 - neck) / 2
		points := []connectorPoint{{0, a}, {1 - depth, a}, {1 - depth, 0}, {1, .5}, {1 - depth, 1}, {1 - depth, 1 - a}, {0, 1 - a}}
		for i, v := range points {
			switch spec.Kind {
			case "arrow-left":
				v.X = 1 - v.X
			case "arrow-up":
				v.X, v.Y = v.Y, 1-v.X
			case "arrow-down":
				v.X, v.Y = 1-v.Y, v.X
			}
			points[i] = v
		}
		p = shapePolygon(points...)
		g.TextRegion = sceneRect{.05, a, 1 - depth - .1, neck}
		if spec.Kind == "arrow-left" {
			g.TextRegion.X = depth + .05
		}
		if spec.Kind == "arrow-up" {
			g.TextRegion = sceneRect{a, depth + .05, neck, 1 - depth - .1}
		}
		if spec.Kind == "arrow-down" {
			g.TextRegion = sceneRect{a, .05, neck, 1 - depth - .1}
		}
	case "chevron":
		p = shapePolygon(connectorPoint{0, 0}, connectorPoint{1 - depth, 0}, connectorPoint{1, .5}, connectorPoint{1 - depth, 1}, connectorPoint{0, 1}, connectorPoint{depth, .5})
		g.TextRegion = sceneRect{depth + .02, .1, 1 - 2*depth - .04, .8}
	case "star", "burst":
		n := 5
		if spec.Kind == "burst" {
			n = 12
		}
		var points []connectorPoint
		for i := 0; i < n*2; i++ {
			r := .5
			if i%2 != 0 {
				r *= depth
			}
			a := -math.Pi/2 + float64(i)*math.Pi/float64(n)
			points = append(points, connectorPoint{.5 + r*math.Cos(a), .5 + r*math.Sin(a)})
		}
		p = shapePolygon(points...)
		v := depth / math.Sqrt2
		g.TextRegion = sceneRect{(1 - v) / 2, (1 - v) / 2, v, v}
	case "cloud":
		p.move(.12, .7)
		p.curve(-.04, .7, -.04, .35, .16, .32)
		p.curve(.1, .08, .38, .02, .47, .2)
		p.curve(.6, -.05, .88, .04, .84, .3)
		p.curve(1, .25, 1, .65, .9, .68)
		p.curve(.98, .94, .66, 1.03, .55, .84)
		p.curve(.4, 1.05, .15, .94, .12, .7)
		p.close()
		g.TextRegion = sceneRect{.25, .32, .5, .4}
	}
	g.Path = p.text.String()
	g.Handles = shapeAdjustmentHandles(spec, aspect)
	if len(g.Contours) == 0 {
		g.Contours = [][]connectorPoint{p.points}
	}
	return g
}
func shapeGeometryForValues(q url.Values) sceneShapeGeometry {
	w, h := shapeDimension(q, "width", 12), shapeDimension(q, "height", 6)
	return shapeGeometry(shapeSpecFromValues(q), (w*1920/245)/(h*1080/56))
}
func (g sceneShapeGeometry) contains(x, y float64) bool {
	inside := false
	for _, points := range g.Contours {
		j := len(points) - 1
		for i, p := range points {
			other := points[j]
			if (p.Y > y) != (other.Y > y) && x < (other.X-p.X)*(y-p.Y)/(other.Y-p.Y)+p.X {
				inside = !inside
			}
			j = i
		}
	}
	return inside
}

func (g sceneShapeGeometry) rowIntersections(y float64) []float64 {
	var xs []float64
	for _, points := range g.Contours {
		j := len(points) - 1
		for i, p := range points {
			other := points[j]
			if (p.Y > y) != (other.Y > y) {
				xs = append(xs, (other.X-p.X)*(y-p.Y)/(other.Y-p.Y)+p.X)
			}
			j = i
		}
	}
	sort.Float64s(xs)
	return xs
}
func shapeCatalogJSON() string {
	type choice struct {
		shapeDefinition
		Path string `json:"path"`
	}
	items := make([]choice, 0, len(shapeDefinitions))
	for _, d := range shapeDefinitions {
		items = append(items, choice{d, shapeGeometry(ShapeSpec{Kind: d.ID}, 1.5).Path})
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}
