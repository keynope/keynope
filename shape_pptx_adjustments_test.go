package main

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestShapeAdjustmentAspectRoundTrip(t *testing.T) {
	for _, kind := range []string{"rounded", "arrow-right", "arrow-left", "arrow-up", "arrow-down", "parallelogram", "trapezoid", "hexagon", "chevron"} {
		for _, size := range [][2]float64{{400, 100}, {100, 400}, {150, 150}} {
			q := url.Values{"shape": {kind}, "shape-radius": {"0.31"}, "shape-depth": {"0.43"}, "shape-neck": {"0.27"}}
			geometry := shapeGeometry(shapeSpecFromValues(q), size[0]/size[1])
			object := sceneObject{Kind: "shape", Shape: kind, Bounds: sceneRect{Width: size[0], Height: size[1]}, ShapeGeometry: &geometry}
			mapped, warnings := sceneObjectToPPTX(object)
			for _, warning := range warnings {
				if warning.Code == "shape-adjustments-approximated" {
					t.Fatal("supported mapping warned", kind)
				}
			}
			got := url.Values{}
			if warnings := importShapeAdjustments(*mapped, got); len(warnings) != 0 {
				t.Fatal(warnings)
			}
			keys := []string{"depth"}
			if strings.HasPrefix(kind, "arrow-") {
				keys = append(keys, "neck")
			}
			if kind == "rounded" {
				keys = []string{"radius"}
			}
			for _, key := range keys {
				value, _ := strconv.ParseFloat(got.Get("shape-"+key), 64)
				want := geometry.Spec.Adjustments[key]
				if math.Abs(value-want) > 0.00002 {
					t.Fatalf("%s %v %s: %g != %g", kind, size, key, value, want)
				}
			}
			if kind != "rounded" {
				axis := size[0]
				if kind == "arrow-up" || kind == "arrow-down" {
					axis = size[1]
				}
				guide := "adj"
				if strings.HasPrefix(kind, "arrow-") {
					guide = "adj2"
				}
				depth := float64(mapped.ShapeAdjustments[guide]) / 100000 * math.Min(size[0], size[1])
				if math.Abs(depth-.43*axis) > 0.001 {
					t.Fatal("DrawingML head length differs")
				}
			}
		}
	}
}

func TestHexagonPPTXIncludesCompleteOfficeAdjustmentSet(t *testing.T) {
	for _, kind := range []string{"hexagon", "preparation"} {
		for _, size := range [][2]float64{{400, 100}, {100, 400}, {150, 150}} {
			q := url.Values{"shape": {kind}, "shape-depth": {"0.31"}}
			geometry := shapeGeometry(shapeSpecFromValues(q), size[0]/size[1])
			object := sceneObject{Kind: "shape", Shape: kind, Bounds: sceneRect{Width: size[0], Height: size[1]}, ShapeGeometry: &geometry}
			mapped, warnings := sceneObjectToPPTX(object)
			if mapped == nil || len(warnings) != 0 || mapped.Shape != "hexagon" {
				t.Fatalf("%s %v: missing native hexagon: %+v %v", kind, size, mapped, warnings)
			}
			if len(mapped.ShapeAdjustments) != 2 || mapped.ShapeAdjustments["vf"] != 115470 {
				t.Fatalf("Office requires both hexagon adjustments: %+v", mapped.ShapeAdjustments)
			}
			wantDepth := int64(math.Round(.31 * size[0] / math.Min(size[0], size[1]) * 100000))
			if mapped.ShapeAdjustments["adj"] != wantDepth {
				t.Fatal("adding vf changed the authored depth", mapped.ShapeAdjustments, wantDepth)
			}
			got := url.Values{}
			if warnings := importShapeAdjustments(*mapped, got); len(warnings) != 0 || got.Get("shape-depth") != "0.31" {
				t.Fatalf("hexagon adjustment roundtrip: %v %v", got, warnings)
			}
		}
	}
}

func TestShapeAdjustmentsFullDeckRoundTrip(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, kind := range []string{"rounded", "arrow-right", "arrow-left", "arrow-up", "arrow-down", "parallelogram", "trapezoid", "hexagon", "chevron"} {
		deck.Slides = append(deck.Slides, Slide{Elements: []Element{{ID: kind, Kind: "shape", Query: "shape=" + kind + "&width=80&height=20&left=10&top=5&fg=%23ff0000&shape-radius=0.31&shape-depth=0.43&shape-neck=0.27"}}})
	}
	data, _, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := importPPTXDocument(data, "Adjusted.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Slides) != len(deck.Slides) {
		t.Fatal("lost slides")
	}
	for i, slide := range loaded.Slides {
		found := false
		for _, element := range slide.Elements {
			q, _ := url.ParseQuery(element.Query)
			if q.Get("fg") != "#FF0000" {
				continue
			}
			found = true
			want, _ := url.ParseQuery(deck.Slides[i].Elements[0].Query)
			keys := []string{"shape-depth"}
			if strings.HasPrefix(want.Get("shape"), "arrow-") {
				keys = append(keys, "shape-neck")
			}
			if i == 0 {
				keys = []string{"shape-radius"}
			}
			for _, key := range keys {
				got, _ := strconv.ParseFloat(q.Get(key), 64)
				expected, _ := strconv.ParseFloat(want.Get(key), 64)
				if math.Abs(got-expected) > 0.00002 {
					t.Fatalf("slide %d %s: %g != %g", i, key, got, expected)
				}
			}
		}
		if !found {
			t.Fatal("missing editable shape", i)
		}
	}
}
