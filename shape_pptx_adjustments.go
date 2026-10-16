package main

import (
	"math"
	"net/url"
	"strconv"

	"keynope/internal/pptx"
)

// DrawingML arrow depth uses the short side; Keynope depth uses the
// arrow's longitudinal axis. Shaft fraction and rounded radius otherwise
// use the same normalized units. Never compensate by changing object bounds.
func exportShapeAdjustments(object sceneObject) (map[string]int64, bool) {
	if object.ShapeGeometry == nil {
		return nil, false
	}
	spec := object.ShapeGeometry.Spec
	value := func(n float64) int64 { return int64(math.Round(n * 100000)) }
	switch spec.Kind {
	case "pill", "terminator":
		return map[string]int64{"adj": 50000}, true
	case "rounded":
		return map[string]int64{"adj": value(spec.Adjustments["radius"])}, true
	case "parallelogram", "data", "trapezoid", "hexagon", "preparation", "chevron":
		short := math.Min(object.Bounds.Width, object.Bounds.Height)
		if short <= 0 {
			return nil, false
		}
		depth := spec.Adjustments["depth"] * object.Bounds.Width / short
		if math.IsNaN(depth) || math.IsInf(depth, 0) || depth*100000 > math.MaxInt32 {
			return nil, false
		}
		adjustments := map[string]int64{"adj": value(depth)}
		if spec.Kind == "hexagon" || spec.Kind == "preparation" {
			// PowerPoint's hexagon has two adjustments. Once avLst is
			// nonempty, Office requires vf alongside adj; omitting it
			// triggers repair even though the core schema accepts the list.
			adjustments["vf"] = 115470
		}
		return adjustments, true
	case "arrow-right", "arrow-left", "arrow-up", "arrow-down":
		w, h := object.Bounds.Width, object.Bounds.Height
		short := math.Min(w, h)
		if short <= 0 {
			return nil, false
		}
		axis := w
		if spec.Kind == "arrow-up" || spec.Kind == "arrow-down" {
			axis = h
		}
		depth := spec.Adjustments["depth"] * axis / short
		if depth*100000 > math.MaxInt32 {
			return nil, false
		}
		return map[string]int64{"adj1": value(spec.Adjustments["neck"]), "adj2": value(depth)}, true
	}
	return nil, false
}

func importShapeAdjustments(object pptx.Object, query url.Values) []pptx.Warning {
	set := func(key string, value, minimum, maximum float64) bool {
		clamped := clampFloat(value, minimum, maximum)
		query.Set("shape-"+key, strconv.FormatFloat(clamped, 'f', -1, 64))
		return math.Abs(clamped-value) > 1e-9
	}
	guide := func(key string, fallback float64) float64 {
		if value, ok := object.ShapeAdjustments[key]; ok {
			return float64(value) / 100000
		}
		return fallback
	}
	clamped := false
	switch object.Shape {
	case "roundRect":
		clamped = set("radius", guide("adj", .16667), .01, .5)
	case "parallelogram", "trapezoid", "hexagon", "chevron":
		if object.Width <= 0 || object.Height <= 0 {
			return nil
		}
		fallback, minimum := .25, .02
		if object.Shape == "chevron" {
			fallback, minimum = .5, .05
		}
		if object.Shape == "hexagon" {
			minimum = .05
		}
		clamped = set("depth", guide("adj", fallback)*math.Min(object.Width, object.Height)/object.Width, minimum, .45)
		if object.Shape == "hexagon" {
			if vf, ok := object.ShapeAdjustments["vf"]; ok && vf != 115470 {
				return []pptx.Warning{{Object: object.ID, Code: "shape-geometry-approximated", Message: "Custom hexagon vertical scaling uses Keynope's full-height outline."}}
			}
		}
	case "rightArrow", "leftArrow", "upArrow", "downArrow":
		axis := object.Width
		if object.Shape == "upArrow" || object.Shape == "downArrow" {
			axis = object.Height
		}
		if axis <= 0 {
			return nil
		}
		clamped = set("neck", guide("adj1", .5), .1, .8)
		clamped = set("depth", guide("adj2", .5)*math.Min(object.Width, object.Height)/axis, .1, .65) || clamped
	default:
		if len(object.ShapeAdjustments) > 0 {
			return []pptx.Warning{{Object: object.ID, Code: "shape-adjustments-approximated", Message: "Custom adjustments for this shape are not supported and use Keynope defaults."}}
		}
	}
	if clamped {
		return []pptx.Warning{{Object: object.ID, Code: "shape-adjustments-clamped", Message: "Shape adjustments exceeded Keynope's editable range and were clamped."}}
	}
	return nil
}
