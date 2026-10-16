package main

import (
	"bytes"
	"context"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"keynope/internal/pptx"
)

func TestCustomCatalogueShapeNativePackageRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	seen := map[int64]bool{}
	for _, kind := range []string{"manual-input", "database", "document", "callout", "speech-bubble", "brace", "label", "star", "burst", "cloud"} {
		code := customPPTXKindCode(kind)
		if seen[code] {
			t.Fatal("shape recipe code collision")
		}
		seen[code] = true
		for _, size := range [][2]float64{{400, 100}, {100, 400}, {200, 200}} {
			for _, variant := range []float64{.17358941, .43210976} {
				q := url.Values{"shape": {kind}, "shape-depth": {strconv.FormatFloat(variant, 'f', -1, 64)}, "shape-radius": {"0.12345678"}}
				geometry := shapeGeometry(shapeSpecFromValues(q), size[0]/size[1])
				o, warnings := sceneObjectToPPTX(sceneObject{ID: kind, Kind: "shape", Shape: kind, Bounds: sceneRect{Width: size[0], Height: size[1]}, ShapeGeometry: &geometry, Paint: scenePaint{Color: "#0088aa"}})
				if len(warnings) != 0 || o.CustomGeometry == nil || o.Shape != "custom" {
					t.Fatal("custom shape still approximated", kind, warnings)
				}
				var data bytes.Buffer
				if _, err := pptx.Write(context.Background(), &data, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*o}}}}); err != nil {
					t.Fatal(err)
				}
				p, report, err := pptx.Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), pptx.Options{})
				if err != nil || len(report.Warnings) != 0 {
					t.Fatal(err, report)
				}
				object := p.Slides[0].Objects[0]
				if !reflect.DeepEqual(o.CustomGeometry, object.CustomGeometry) {
					t.Fatal("native path lost controls/contours", kind)
				}
				e, warnings, _, err := pptxObjectToElement(object, 1, 0, 0)
				if err != nil || len(warnings) != 0 || e.Kind != "shape" {
					t.Fatal("catalogue restore failed", kind, err, warnings)
				}
				query, _ := url.ParseQuery(e.Query)
				if query.Get("shape") != kind {
					t.Fatal("custom shape became a preset/image", kind, query)
				}
				got := shapeSpecFromValues(query)
				for key, value := range geometry.Spec.Adjustments {
					if math.Abs(got.Adjustments[key]-value) > .000001 {
						t.Fatal("custom parameter changed", kind, key, value, got.Adjustments[key])
					}
				}
			}
		}
	}
}

func TestCustomShapeRecipeDoesNotOverrideEditedGeometry(t *testing.T) {
	geometry := shapeGeometry(ShapeSpec{Kind: "speech-bubble", Adjustments: map[string]float64{"radius": .12, "depth": .22}}, 2)
	for _, problem := range []string{"path", "rect", "kind", "version", "parameter", "flip", "fill"} {
		o, _ := sceneObjectToPPTX(sceneObject{Kind: "shape", Shape: "speech-bubble", Bounds: sceneRect{Width: 400, Height: 200}, ShapeGeometry: &geometry, Paint: scenePaint{Color: "#123456"}})
		switch problem {
		case "path":
			o.CustomGeometry.Paths[0].Commands[0].Points[0].X += 1000
		case "rect":
			o.CustomGeometry.TextRect[0] += 1000
		case "kind":
			o.ShapeAdjustments["knKind"] = 0
		case "version":
			o.ShapeAdjustments["knVersion"] = 2
		case "parameter":
			o.ShapeAdjustments["kn_depth"] = 400000
		case "flip":
			o.FlipH = true
		case "fill":
			o.CustomGeometry.Paths[0].Fill = false
		}
		q := url.Values{"unrelated": {"preserve"}}
		if restoreCustomPPTXShape(*o, q) || len(q) != 1 {
			t.Fatal("stale recipe overrode visible path", problem, q)
		}
		_, warnings, _, err := pptxObjectToElement(*o, 1, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range warnings {
			found = found || w.Code == "custom-shape-geometry-approximated"
		}
		if !found {
			t.Fatal("unsupported edited freeform silently lost", problem)
		}
	}
}

func TestCustomDatabaseDetailPaintRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	geometry := shapeGeometry(shapeSpecFromValues(url.Values{"shape": {"database"}, "shape-depth": {"0.21"}}), 2)
	for _, stroke := range []string{"", "#ffcc55"} {
		for _, alpha := range []float64{0, .35, 1} {
			o, warnings := sceneObjectToPPTX(sceneObject{Kind: "shape", Shape: "database", Bounds: sceneRect{Width: 400, Height: 200}, ShapeGeometry: &geometry, Paint: scenePaint{Color: "#123456", Stroke: stroke, StrokeWidth: 6, Opacity: &alpha}})
			if len(warnings) != 0 || o.CustomGeometry == nil || len(o.CustomGeometry.Paths) != 2 {
				t.Fatal("database body/detail not native", warnings)
			}
			if o.CustomGeometry.Paths[0].NoStroke != (stroke == "") || o.CustomGeometry.Paths[1].Fill || o.CustomGeometry.Paths[1].NoStroke {
				t.Fatal("body/detail paint coupled incorrectly")
			}
			var data bytes.Buffer
			if _, err := pptx.Write(context.Background(), &data, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*o}}}}); err != nil {
				t.Fatal(err)
			}
			p, _, err := pptx.Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			e, warnings, _, err := pptxObjectToElement(p.Slides[0].Objects[0], 1, 0, 0)
			if err != nil || len(warnings) != 0 {
				t.Fatal(err, warnings)
			}
			q, _ := url.ParseQuery(e.Query)
			if q.Get("shape") != "database" || (stroke == "" && q.Get("shape-stroke") != "none") || (stroke != "" && q.Get("shape-stroke") != "#FFCC55") {
				t.Fatal("detail stroke became body outline", q)
			}
			if stroke == "" {
				o.Stroke = "#ffffff"
				if restoreCustomPPTXShape(*o, url.Values{}) {
					t.Fatal("externally edited cap paint silently discarded")
				}
			}
		}
	}
}

func TestCustomShapeInheritedPortableFixture(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	for page := 0; page < 4; page++ {
		d.Slides = append(d.Slides, Slide{ID: "custom-" + strconv.Itoa(page), LayoutID: "blank"})
	}
	for i, kind := range []string{"document", "callout", "speech-bubble", "brace", "label", "star", "burst", "cloud"} {
		label, err := encodeShapeLabel(shapeLabelData{Text: kind, Query: "render=truetype&modern-font=sans&modern-size=30&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		q := url.Values{"shape": {kind}, "shape-depth": {"0.24759341"}, "shape-radius": {"0.13"}, "left": {strconv.Itoa(8 + i%2*120)}, "top": {strconv.Itoa(7 + i%4/2*24)}, "width": {"108"}, "height": {"19"}, "fg": {"#0e7490"}, "shape-stroke": {"#55ffff"}, "shape-stroke-width": {"2"}, "shape-label": {label}}
		d.Slides[i/4].Elements = append(d.Slides[i/4].Elements, Element{ID: kind, Kind: "shape", Query: q.Encode()})
	}
	for i, treatment := range []string{"shape-stroke=none", "shape-stroke=%2355ffff&shape-stroke-width=2", "shape-stroke=%23ffcc55&shape-stroke-width=6", "shape-stroke=none&gradient-start=%230e7490&gradient-end=%2355aaff&gradient-dir=vertical"} {
		label, err := encodeShapeLabel(shapeLabelData{Text: []string{"Cap only", "Outline", "Wide outline", "Gradient"}[i], Query: "render=truetype&modern-font=sans&modern-size=30&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(treatment)
		q.Set("shape", "database")
		q.Set("shape-depth", decimal(.08+float64(i)*.05))
		q.Set("left", strconv.Itoa(8+i%2*120))
		q.Set("top", strconv.Itoa(7+i/2*24))
		q.Set("width", "108")
		q.Set("height", "19")
		q.Set("fg", "#0e7490")
		q.Set("shape-label", label)
		d.Slides[2].Elements = append(d.Slides[2].Elements, Element{ID: "database-" + strconv.Itoa(i), Kind: "shape", Query: q.Encode()})
	}
	for i, depth := range []float64{.03, .13759341, .275, .4} {
		label, err := encodeShapeLabel(shapeLabelData{Text: "Manual input", Query: "render=truetype&modern-font=sans&modern-size=30&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		q := url.Values{"shape": {"manual-input"}, "shape-depth": {decimal(depth)}, "left": {strconv.Itoa(8 + i%2*120)}, "top": {strconv.Itoa(7 + i/2*24)}, "width": {"108"}, "height": {"19"}, "fg": {"#0e7490"}, "shape-stroke": {"#55ffff"}, "shape-stroke-width": {"2"}, "shape-label": {label}}
		d.Slides[3].Elements = append(d.Slides[3].Elements, Element{ID: "manual-" + strconv.Itoa(i), Kind: "shape", Query: q.Encode()})
	}
	// Exercise an inherited path too, rather than only normal authored objects.
	d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{ID: "inherited-star", Kind: "shape", Query: "shape=star&shape-depth=0.6&left=220&top=1&width=20&height=6&fg=%23ff0055"})
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code == "shape-adjustments-approximated" || w.Code == "shape-geometry-approximated" {
			t.Fatal("custom shape still uses preset defaults", w)
		}
	}
	loaded, _, err := importPPTXDocument(data, "Custom.pptx")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, slide := range loaded.Slides {
		for _, e := range slide.Elements {
			q, _ := url.ParseQuery(e.Query)
			if q.Get("fg") == "#0E7490" || q.Get("fg") == "#FF0055" {
				if !customPPTXShapeKind(q.Get("shape")) || e.Kind != "shape" {
					t.Fatal("custom catalogue shape not editable", e)
				}
				count++
			}
		}
	}
	if count != 20 {
		t.Fatal("custom paths omitted", count)
	}
	if root := os.Getenv("KEYNOPE_CUSTOM_GEOMETRY_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "custom.md"), d); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "custom.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
