package main

import (
	"bytes"
	"context"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"keynope/internal/pptx"
)

func TestCuratedShapePPTXEditablePresets(t *testing.T) {
	d := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, definition := range shapeDefinitions {
		label, err := encodeShapeLabel(shapeLabelData{Text: definition.Title, Query: "render=truetype&modern-font=sans&modern-size=30&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery("shape=" + definition.ID + "&left=10&top=8&width=80&height=30&fg=%230e7490")
		q.Set("shape-label", label)
		d.Slides = append(d.Slides, Slide{Elements: []Element{{ID: definition.ID, Kind: "shape", Query: q.Encode()}}})
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code == "shape-adjustments-approximated" || warning.Code == "shape-geometry-approximated" {
			t.Fatal("catalogue shape still uses an approximate default", warning)
		}
	}
	p, readReport, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(readReport.Warnings) > 0 {
		t.Fatal(err, readReport)
	}
	if len(p.Slides) != len(shapeDefinitions) {
		t.Fatal("shape pages omitted")
	}
	for i, slide := range p.Slides {
		// The export also emits an editable background rectangle. Check the
		// authored coloured shape, not a fixed object count or first position.
		found := false
		custom := map[string]bool{"manual-input": true, "database": true, "document": true, "callout": true, "speech-bubble": true, "brace": true, "label": true, "star": true, "burst": true, "cloud": true}[shapeDefinitions[i].ID]
		for _, object := range slide.Objects {
			if object.Fill == "#0E7490" && object.Kind == "shape" && ((!custom && object.Shape == shapeDefinitions[i].Preset) || (custom && object.Shape == "custom" && object.CustomGeometry != nil)) {
				found = true
			}
		}
		if !found {
			t.Fatalf("not an editable native preset: %+v", slide.Objects)
		}
	}
	loaded, _, err := importPPTXDocument(data, "Presets.pptx")
	if err != nil {
		t.Fatal(err)
	}
	for _, slide := range loaded.Slides {
		if len(slide.Elements) < 1 {
			t.Fatal("native shape imported as an image")
		}
		for _, element := range slide.Elements {
			if element.Kind != "shape" {
				t.Fatal("native preset flattened on import")
			}
		}
	}
	if output := os.Getenv("KEYNOPE_SHAPE_EVIDENCE"); output != "" {
		if err = os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(output, "editable-presets.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// Whole-catalogue regression: canonical aliases may change names, never the
// shared contours, label interior or authored frame. This is semantic package
// evidence, not an assertion about a third-party Office renderer.
func TestCuratedShapePPTXAllGeometryAndAdjustmentExtremes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, definition := range shapeDefinitions {
		for _, size := range [][2]float64{{400, 100}, {100, 400}, {200, 200}} {
			for _, position := range []float64{0, .372159, 1} {
				q := url.Values{"shape": {definition.ID}}
				for _, adjustment := range definition.Adjustments {
					q.Set("shape-"+adjustment.Key, strconv.FormatFloat(adjustment.Min+(adjustment.Max-adjustment.Min)*position, 'f', -1, 64))
				}
				want := shapeGeometry(shapeSpecFromValues(q), size[0]/size[1])
				o, warnings := sceneObjectToPPTX(sceneObject{ID: definition.ID, Kind: "shape", Shape: definition.ID, Bounds: sceneRect{X: 50, Y: 50, Width: size[0], Height: size[1]}, ShapeGeometry: &want, Paint: scenePaint{Color: "#0e7490", Stroke: "#55ffff", StrokeWidth: 2}})
				if o == nil || len(warnings) != 0 {
					t.Fatal(definition.ID, warnings)
				}
				var data bytes.Buffer
				if _, err := pptx.Write(context.Background(), &data, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*o}}}}); err != nil {
					t.Fatal(err)
				}
				p, report, err := pptx.Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), pptx.Options{})
				if err != nil || len(report.Warnings) != 0 {
					t.Fatal(err, report)
				}
				native := p.Slides[0].Objects[0]
				e, warnings, _, err := pptxObjectToElement(native, 1, 0, 0)
				if err != nil || len(warnings) != 0 || e.Kind != "shape" {
					t.Fatal(definition.ID, err, warnings)
				}
				query, _ := url.ParseQuery(e.Query)
				got := shapeGeometry(shapeSpecFromValues(query), native.Width/native.Height)
				if len(got.Contours) != len(want.Contours) {
					t.Fatal("contour count changed", definition.ID)
				}
				for i, contour := range want.Contours {
					if len(contour) != len(got.Contours[i]) {
						t.Fatal("contour topology changed", definition.ID)
					}
					for j, point := range contour {
						if math.Abs(point.X-got.Contours[i][j].X) > .00005 || math.Abs(point.Y-got.Contours[i][j].Y) > .00005 {
							t.Fatal("contour drift", definition.ID, size, position, point, got.Contours[i][j])
						}
					}
				}
				for _, pair := range [][2]float64{{got.TextRegion.X, want.TextRegion.X}, {got.TextRegion.Y, want.TextRegion.Y}, {got.TextRegion.Width, want.TextRegion.Width}, {got.TextRegion.Height, want.TextRegion.Height}} {
					if math.Abs(pair[0]-pair[1]) > .00005 {
						t.Fatal("label region drift", definition.ID, size, position, pair)
					}
				}
				for _, pair := range [][2]float64{{native.X, o.X}, {native.Y, o.Y}, {native.Width, o.Width}, {native.Height, o.Height}} {
					if math.Abs(pair[0]-pair[1]) > 1.0/6350 {
						t.Fatal("native frame drift", definition.ID, pair)
					}
				}
			}
		}
	}
}
