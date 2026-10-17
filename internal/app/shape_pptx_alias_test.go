package app

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

func TestEditablePPTXShapeAliasesPreserveGeometry(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"pill", "terminator", "data", "preparation"} {
		for _, size := range [][2]float64{{400, 100}, {100, 400}, {200, 200}} {
			for _, depth := range []float64{.2, .43} {
				q := url.Values{"shape": {kind}, "shape-depth": {strconv.FormatFloat(depth, 'f', -1, 64)}}
				geometry := shapeGeometry(shapeSpecFromValues(q), size[0]/size[1])
				o, warnings := sceneObjectToPPTX(sceneObject{ID: kind, Kind: "shape", Shape: kind, Bounds: sceneRect{Width: size[0], Height: size[1]}, ShapeGeometry: &geometry, Paint: scenePaint{Color: "#ff0000"}})
				for _, w := range warnings {
					if w.Code == "shape-adjustments-approximated" || w.Code == "shape-geometry-approximated" {
						t.Fatal("equivalent native shape still loses geometry", kind, w)
					}
				}
				native, canonical := "roundRect", "rounded"
				if kind == "data" {
					native, canonical = "parallelogram", "parallelogram"
				}
				if kind == "preparation" {
					native, canonical = "hexagon", "hexagon"
				}
				adjustmentCount := 1
				if native == "hexagon" {
					adjustmentCount = 2
					if o.ShapeAdjustments["vf"] != 115470 {
						t.Fatal("hexagon is missing its required Office vertical factor", o.ShapeAdjustments)
					}
				}
				if o.Shape != native || len(o.ShapeAdjustments) != adjustmentCount {
					t.Fatal("wrong adjustable native preset", kind, o)
				}
				var data bytes.Buffer
				if _, err := pptx.Write(context.Background(), &data, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*o}}}}); err != nil {
					t.Fatal(err)
				}
				p, _, err := pptx.Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), pptx.Options{})
				if err != nil {
					t.Fatal(err)
				}
				object := p.Slides[0].Objects[0]
				values := url.Values{"shape": {importShape(object.Shape)}}
				if w := importShapeAdjustments(object, values); len(w) != 0 {
					t.Fatal(w)
				}
				got := shapeGeometry(shapeSpecFromValues(values), size[0]/size[1])
				if got.Spec.Kind != canonical || len(got.Contours) != len(geometry.Contours) {
					t.Fatal("canonical shape changed geometry", kind, got.Spec)
				}
				for i, contour := range geometry.Contours {
					if len(contour) != len(got.Contours[i]) {
						t.Fatal("native roundtrip changed curve topology", kind)
					}
					for j, v := range contour {
						if math.Abs(v.X-got.Contours[i][j].X) > 2e-5 || math.Abs(v.Y-got.Contours[i][j].Y) > 2e-5 {
							t.Fatal("native roundtrip changed outline", kind, size, v, got.Contours[i][j])
						}
					}
				}
				for _, pair := range [][2]float64{{got.TextRegion.X, geometry.TextRegion.X}, {got.TextRegion.Y, geometry.TextRegion.Y}, {got.TextRegion.Width, geometry.TextRegion.Width}, {got.TextRegion.Height, geometry.TextRegion.Height}} {
					if math.Abs(pair[0]-pair[1]) > 4e-5 {
						t.Fatal("alias label interior changed", kind)
					}
				}
			}
		}
	}
}

func TestFixedOfficeFlowchartAliasesStillImport(t *testing.T) {
	for preset, want := range map[string]string{"flowChartInputOutput": "data", "flowChartPreparation": "preparation", "flowChartTerminator": "terminator"} {
		if got := importShape(preset); got != want {
			t.Fatal("legacy Office preset no longer imports", preset, got)
		}
		q := url.Values{}
		if warnings := importShapeAdjustments(pptx.Object{Shape: preset, Width: 400, Height: 100}, q); len(warnings) != 0 {
			t.Fatal(warnings)
		}
	}
}

func TestPPTXShapeAliasInheritedExportFixture(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck(), Slides: []Slide{{ID: "aliases", LayoutID: "blank"}}}
	for i, kind := range []string{"pill", "terminator", "data", "preparation"} {
		q := url.Values{"shape": {kind}, "shape-depth": {"0.43"}, "left": {strconv.Itoa(12 + i%2*116)}, "top": {strconv.Itoa(8 + i/2*23)}, "width": {"100"}, "height": {"17"}, "fg": {"#0e7490"}, "shape-stroke": {"#55ffff"}, "shape-stroke-width": {"2"}}
		d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{ID: kind, Kind: "shape", Query: q.Encode()})
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code == "shape-adjustments-approximated" || w.Code == "shape-geometry-approximated" {
			t.Fatal("inherited alias lost adjustment", w)
		}
	}
	loaded, _, err := importPPTXDocument(data, "Aliases.pptx")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range loaded.Slides[0].Elements {
		q, _ := url.ParseQuery(e.Query)
		if q.Get("fg") == "#0E7490" {
			counts[q.Get("shape")]++
		}
	}
	if counts["rounded"] != 2 || counts["parallelogram"] != 1 || counts["hexagon"] != 1 {
		t.Fatal("inherited alias objects flattened/omitted", counts, loaded.Slides[0].Elements, report)
	}
	if root := os.Getenv("KEYNOPE_SHAPE_ALIAS_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "aliases.md"), d); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "aliases.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
