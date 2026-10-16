package pptx

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
)

func TestNoFillShapesKeepTheirSemanticType(t *testing.T) {
	objects := []Object{
		{ID: "empty", Name: "Empty no-fill shape", Kind: "shape", Shape: "rect", X: 10, Y: 20, Width: 100, Height: 80},
		{ID: "label", Name: "Labelled no-fill shape", Kind: "shape", Shape: "ellipse", X: 150, Y: 20, Width: 100, Height: 80, Text: "Label", Color: "#123456", FontFace: "Arial", FontSize: 20},
		{ID: "text", Name: "Actual textbox", Kind: "text", Shape: "rect", X: 300, Y: 20, Width: 100, Height: 80, Text: "Text", Color: "#123456", FontFace: "Arial", FontSize: 20},
	}
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: objects}}}); err != nil {
		t.Fatal(err)
	}
	xml := string(packageParts(t, out.Bytes())["ppt/slides/slide1.xml"])
	if strings.Count(xml, `<a:noFill/>`) < 3 {
		t.Fatal("missing explicit no-fill", xml)
	}
	got, _, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides) != 1 || len(got.Slides[0].Objects) != len(objects) {
		t.Fatal("invisible editable shape was dropped", got.Slides)
	}
	for i, o := range got.Slides[0].Objects {
		if o.Kind != objects[i].Kind || o.Text != objects[i].Text || o.Shape != objects[i].Shape || o.Fill != "" || o.Stroke != "" {
			t.Fatal("no-fill object changed type/paint/content", i, o)
		}
	}
}

func TestEditableShapeGradientAndShadow(t *testing.T) {
	for _, direction := range []string{"horizontal", "vertical", "diagonal"} {
		for _, opacity := range []float64{0, .4, 1} {
			source := Object{ID: "paint", Kind: "shape", Shape: "rect", X: 10, Y: 20, Width: 500, Height: 200, Fill: "#123456", Opacity: opacity, OpacitySet: true, Gradient: &ShapeGradient{Start: "#ff0055", End: "#55ffff", Direction: direction}, Shadow: &ShapeShadow{Color: "#ffffff", X: -8, Y: 20, Blur: 8}}
			shadowAlpha := .7
			source.Shadow.Opacity = &shadowAlpha
			var out bytes.Buffer
			report, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{source}}}})
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			xml := string(packageParts(t, out.Bytes())["ppt/slides/slide1.xml"])
			if !strings.Contains(xml, "<a:gradFill") || !strings.Contains(xml, "<a:outerShdw") || strings.Contains(xml, "<p:pic>") {
				t.Fatal(xml)
			}
			loaded, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
			if err != nil || len(report.Warnings) > 0 || len(loaded.Slides[0].Objects) != 1 {
				t.Fatal(err, report)
			}
			got := loaded.Slides[0].Objects[0]
			if got.Gradient == nil || !strings.EqualFold(got.Gradient.Start, source.Gradient.Start) || !strings.EqualFold(got.Gradient.End, source.Gradient.End) || got.Gradient.Direction != direction || got.Opacity != opacity || !got.OpacitySet || got.Shadow == nil {
				t.Fatal(got)
			}
			if !strings.EqualFold(got.Shadow.Color, source.Shadow.Color) || math.Abs(got.Shadow.X-source.Shadow.X) > .001 || math.Abs(got.Shadow.Y-source.Shadow.Y) > .001 || got.Shadow.Blur != 8 {
				t.Fatal(got.Shadow)
			}
			if got.Shadow.Opacity == nil || *got.Shadow.Opacity != shadowAlpha {
				t.Fatal("independent shadow alpha lost", got.Shadow)
			}
		}
	}
}

func TestShapePaintRejectsInvalidBeforeWrite(t *testing.T) {
	for _, object := range []Object{
		{Gradient: &ShapeGradient{Start: `#ff00"`, End: "#ffffff", Direction: "horizontal"}},
		{Gradient: &ShapeGradient{Start: "#ffffff", End: "#ffffff", Direction: "bad"}},
		{Shadow: &ShapeShadow{Color: "#ffffff", Blur: -1}},
		{Shadow: &ShapeShadow{Color: "#ffffff", X: math.NaN()}},
	} {
		object.Kind = "shape"
		object.Width = 100
		object.Height = 100
		var out bytes.Buffer
		if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{object}}}}); err == nil || out.Len() != 0 {
			t.Fatal("invalid paint wrote output", err)
		}
	}
}

func TestShapePaintInheritanceAndExplicitOverrides(t *testing.T) {
	alpha := .4
	parent := Object{Kind: "shape", Shape: "rect", X: 10, Y: 20, Width: 500, Height: 200, Fill: "#ff0055", Opacity: .4, OpacitySet: true, Gradient: &ShapeGradient{Start: "#ff0055", End: "#55ffff", Direction: "diagonal"}, Shadow: &ShapeShadow{Color: "#ffffff", X: 10, Y: 20, Blur: 8, Opacity: &alpha}}
	for _, test := range []struct {
		name, paint      string
		gradient, shadow bool
		fill             string
	}{
		{"omitted", "", true, true, "#ff0055"},
		{"no-fill", `<a:noFill/>`, false, true, ""},
		{"solid-reset", `<a:solidFill><a:srgbClr val="123456"/></a:solidFill>`, false, true, "#123456"},
		{"shadow-off", `<a:effectLst/>`, true, false, "#ff0055"},
		{"both-off", `<a:noFill/><a:effectLst/>`, false, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			n, err := parseXML([]byte(`<p:sp xmlns:p="p" xmlns:a="a"><p:nvSpPr><p:cNvPr id="child"/></p:nvSpPr><p:spPr>`+test.paint+`</p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>Child</a:t></a:r></a:p></p:txBody></p:sp>`), 64)
			if err != nil {
				t.Fatal(err)
			}
			object, err := parseShape(n, nil, colourResolver{}, &inheritedShape{object: parent, hasGeometry: true}, false)
			if err != nil || object == nil {
				t.Fatal(err)
			}
			if (object.Gradient != nil) != test.gradient || (object.Shadow != nil) != test.shadow || !strings.EqualFold(object.Fill, test.fill) {
				t.Fatal(object)
			}
			if object.Gradient != nil {
				object.Gradient.Start = "#123456"
				if parent.Gradient.Start != "#ff0055" {
					t.Fatal("child gradient aliases parent")
				}
			}
			if object.Shadow != nil {
				*object.Shadow.Opacity = .9
				if *parent.Shadow.Opacity != .4 {
					t.Fatal("child shadow alpha aliases parent")
				}
				object.Shadow.X = 99
				if parent.Shadow.X != 10 {
					t.Fatal("child shadow aliases parent")
				}
			}
		})
	}
}
