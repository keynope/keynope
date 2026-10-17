package app

import (
	"bytes"
	"context"
	"keynope/internal/pptx"
	"os"
	"strings"
	"testing"
)

func TestShapeEffectsRemainEditableAcrossPPTX(t *testing.T) {
	sx, sy := 1920.0/245, 1080.0/56
	var slides []pptx.Slide
	for _, direction := range []string{"horizontal", "vertical", "diagonal"} {
		opacity := .4
		scene := sceneObject{ID: "effect-shape", Kind: "shape", Shape: "square", Bounds: sceneRect{X: 100, Y: 100, Width: 600, Height: 300}, Paint: scenePaint{Color: "#55aaff", GradientStart: "#ff0055", GradientEnd: "#55ffff", GradientDirection: direction, ShadowColor: "#ffffff", ShadowX: -sx, ShadowY: sy, ShadowBlur: 8, Opacity: &opacity}}
		object, warnings := sceneObjectToPPTX(scene)
		if object == nil || object.Gradient == nil || object.Shadow == nil {
			t.Fatal(object, warnings)
		}
		for _, warning := range warnings {
			if warning.Code == "shape-effects-approximated" {
				t.Fatal(warning)
			}
		}
		slides = append(slides, pptx.Slide{Objects: []pptx.Object{*object}})
	}
	var out bytes.Buffer
	if _, err := pptx.Write(context.Background(), &out, pptx.Presentation{Slides: slides}); err != nil {
		t.Fatal(err)
	}
	if file := os.Getenv("KEYNOPE_SHAPE_PAINT_FIXTURE"); file != "" {
		if err := os.WriteFile(file, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, report, err := pptx.Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), pptx.Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	for i, slide := range loaded.Slides {
		element, warnings, _, err := pptxObjectToElement(slide.Objects[0], 1, 0, 0)
		if err != nil || len(warnings) > 0 {
			t.Fatal(err, warnings)
		}
		paint := scenePaintFromValues(element, Slide{}, sx, sy, mustQuery(element.Query), nil)
		if !strings.EqualFold(paint.GradientStart, "#ff0055") || !strings.EqualFold(paint.GradientEnd, "#55ffff") || paint.GradientDirection != slides[i].Objects[0].Gradient.Direction || paint.ShadowX != -sx || paint.ShadowY != sy || paint.ShadowBlur != 8 || paint.Opacity == nil || *paint.Opacity != .4 {
			t.Fatal(paint)
		}
	}
}
