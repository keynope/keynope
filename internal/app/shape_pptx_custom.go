package app

import (
	"hash/fnv"
	"math"
	"net/url"
	"strconv"
	"strings"

	"keynope/internal/pptx"
)

func customPPTXShapeKind(kind string) bool {
	switch kind {
	case "manual-input", "database", "document", "callout", "speech-bubble", "brace", "label", "star", "burst", "cloud":
		return true
	}
	return false
}

func customPPTXKindCode(kind string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(kind))
	return int64(h.Sum32() & math.MaxInt32)
}

// Consume only our registry's numeric M/L/C/Z output, never imported SVG/XML.
// Native cubic control points are preserved rather than flattening the curves.
func customPPTXGeometry(geometry *sceneShapeGeometry) (*pptx.CustomGeometry, bool) {
	if geometry == nil || !customPPTXShapeKind(geometry.Spec.Kind) {
		return nil, false
	}
	return registryPPTXGeometry(geometry)
}

func registryPPTXGeometry(geometry *sceneShapeGeometry) (*pptx.CustomGeometry, bool) {
	if geometry == nil || len(geometry.Details) > 15 {
		return nil, false
	}
	paths := make([]pptx.GeometryPath, 0, 1+len(geometry.Details))
	for i, source := range append([]string{geometry.Path}, geometry.Details...) {
		path, ok := customPPTXNumericPath(source, i == 0)
		if !ok {
			return nil, false
		}
		paths = append(paths, path)
	}
	r := geometry.TextRegion
	unit := func(n float64) int64 { return int64(math.Round(n * float64(pptx.GeometryUnits))) }
	return &pptx.CustomGeometry{Paths: paths, TextRect: [4]int64{unit(r.X), unit(r.Y), unit(r.X + r.Width), unit(r.Y + r.Height)}}, true
}

func customPPTXNumericPath(source string, filled bool) (pptx.GeometryPath, bool) {
	tokens := strings.Fields(strings.NewReplacer("M", " M ", "L", " L ", "C", " C ", "Z", " Z ").Replace(source))
	if len(tokens) > 14000 {
		return pptx.GeometryPath{}, false
	}
	path := pptx.GeometryPath{Fill: filled}
	for i := 0; i < len(tokens); {
		op := tokens[i]
		i++
		kind, n := "", 0
		switch op {
		case "M":
			kind, n = "move", 2
		case "L":
			kind, n = "line", 2
		case "C":
			kind, n = "cubic", 6
		case "Z":
			kind = "close"
		default:
			return pptx.GeometryPath{}, false
		}
		if len(tokens)-i < n {
			return pptx.GeometryPath{}, false
		}
		command := pptx.GeometryCommand{Kind: kind}
		for j := 0; j < n; j += 2 {
			x, ex := strconv.ParseFloat(tokens[i+j], 64)
			y, ey := strconv.ParseFloat(tokens[i+j+1], 64)
			if ex != nil || ey != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Abs(x) > 4 || math.Abs(y) > 4 {
				return pptx.GeometryPath{}, false
			}
			command.Points = append(command.Points, pptx.GeometryPoint{X: int64(math.Round(x * float64(pptx.GeometryUnits))), Y: int64(math.Round(y * float64(pptx.GeometryUnits)))})
		}
		i += n
		path.Commands = append(path.Commands, command)
	}
	return path, true
}

func customPPTXShapeMarkers(spec ShapeSpec) map[string]int64 {
	guides := map[string]int64{"knVersion": 1, "knKind": customPPTXKindCode(spec.Kind)}
	for key, value := range spec.Adjustments {
		guides["kn_"+key] = int64(math.Round(value * float64(pptx.GeometryUnits)))
	}
	return guides
}

// Constant guides carry the supported semantic recipe, but are not trusted as
// a substitute for the visible path. Restore only when the numeric geometry
// and text rectangle still match that recipe; edited/foreign paths warn.
func restoreCustomPPTXShape(object pptx.Object, q url.Values) bool {
	if object.CustomGeometry == nil || len(object.CustomGeometry.Paths) == 0 || object.ShapeAdjustments["knVersion"] != 1 || object.FlipH || object.FlipV || object.Width <= 0 || object.Height <= 0 {
		return false
	}
	for _, definition := range shapeDefinitions {
		if customPPTXKindCode(definition.ID) != object.ShapeAdjustments["knKind"] || len(object.ShapeAdjustments) != 2+len(definition.Adjustments) {
			continue
		}
		values := url.Values{"shape": {definition.ID}}
		for _, adjustment := range definition.Adjustments {
			v, exists := object.ShapeAdjustments["kn_"+adjustment.Key]
			if !exists {
				return false
			}
			value := float64(v) / float64(pptx.GeometryUnits)
			if value < adjustment.Min || value > adjustment.Max {
				return false
			}
			values.Set("shape-"+adjustment.Key, strconv.FormatFloat(value, 'f', -1, 64))
		}
		geometry := shapeGeometry(shapeSpecFromValues(values), object.Width/object.Height)
		expected, ok := registryPPTXGeometry(&geometry)
		if ok && definition.ID == "database" && object.CustomGeometry.Paths[0].NoStroke {
			// With no body outline, the native line paint belongs solely to
			// the cap detail. Never discard externally edited detail paint.
			if !strings.EqualFold(object.Stroke, "#202833") || math.Abs(object.StrokeWidth-.75) > .000001 {
				return false
			}
			expected.Paths[0].NoStroke = true
		}
		if !ok || !customPPTXGeometryMatches(expected, object.CustomGeometry) {
			return false
		}
		for key, value := range values {
			q[key] = value
		}
		return true
	}
	return false
}

func customPPTXGeometryMatches(a, b *pptx.CustomGeometry) bool {
	if a == nil || b == nil || len(a.Paths) != len(b.Paths) {
		return false
	}
	near := func(x, y int64) bool { return math.Abs(float64(x)-float64(y)) <= 4 }
	for i := range a.TextRect {
		if !near(a.TextRect[i], b.TextRect[i]) {
			return false
		}
	}
	for i, path := range a.Paths {
		other := b.Paths[i]
		if path.Fill != other.Fill || path.NoStroke != other.NoStroke || len(path.Commands) != len(other.Commands) {
			return false
		}
		for j, c := range path.Commands {
			d := other.Commands[j]
			if c.Kind != d.Kind || len(c.Points) != len(d.Points) {
				return false
			}
			for k, p := range c.Points {
				if !near(p.X, d.Points[k].X) || !near(p.Y, d.Points[k].Y) {
					return false
				}
			}
		}
	}
	return true
}
