package app

import (
	"bytes"
	"context"
	"fmt"
	"keynope/internal/pptx"
	"math"
	"strings"
	"testing"
)

func TestPPTXCharacterEffectsRoundTrip(t *testing.T) {
	sx, sy := 1920.0/245, 1080.0/56
	for _, label := range []bool{false, true} {
		for _, alpha := range []float64{0, .5, 1} {
			for _, stroke := range []string{"#111111", "#ffffff"} {
				for _, soft := range []bool{false, true} {
					t.Run(fmt.Sprintf("label=%t/alpha=%g/stroke=%s/soft=%t", label, alpha, stroke, soft), func(t *testing.T) {
						paint := scenePaint{Color: "#ffaa55", Stroke: stroke, StrokeWidth: 2, ShadowColor: "#55aaff", ShadowX: -sx, ShadowY: 2 * sy, Opacity: &alpha}
						if soft {
							paint.ShadowBlur = 8
						}
						text := &sceneText{FontID: "sans", Size: 80, WidthScale: 1, LineHeight: 1.25, Runs: []sceneRun{{Text: "Editable effect"}}}
						source := sceneObject{ID: "effects", Kind: "text", Text: text, Bounds: sceneRect{X: 100, Y: 100, Width: 600, Height: 200}, Paint: paint}
						if label {
							parentAlpha := .4
							source.Kind = "shape"
							source.Shape = "square"
							source.Text = nil
							source.Label = text
							source.LabelPaint = &paint
							source.Paint = scenePaint{Color: "#aaff55", Opacity: &parentAlpha}
						}
						object, warnings := sceneObjectToPPTX(source)
						if object == nil || len(warnings) > 0 {
							t.Fatal(object, warnings)
						}
						var out bytes.Buffer
						if _, err := pptx.Write(context.Background(), &out, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*object}}}}); err != nil {
							t.Fatal(err)
						}
						loaded, report, err := pptx.Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), pptx.Options{})
						if err != nil || len(report.Warnings) > 0 {
							t.Fatal(err, report)
						}
						element, warnings, _, err := pptxObjectToElement(loaded.Slides[0].Objects[0], 1, 0, 0)
						if err != nil || len(warnings) > 0 {
							t.Fatal(err, warnings)
						}
						if label {
							var ok bool
							element, ok = shapeLabel(element)
							if !ok {
								t.Fatal("label missing")
							}
						}
						actual := scenePaintFromValues(element, Slide{}, sx, sy, mustQuery(element.Query), nil)
						if !strings.EqualFold(actual.Stroke, stroke) || actual.StrokeWidth != 2 || !strings.EqualFold(actual.ShadowColor, paint.ShadowColor) || math.Abs(actual.ShadowX-paint.ShadowX) > .001 || math.Abs(actual.ShadowY-paint.ShadowY) > .001 || actual.ShadowBlur != paint.ShadowBlur || actual.Opacity == nil || math.Abs(*actual.Opacity-alpha) > 1e-6 {
							t.Fatalf("character paint changed: %+v vs %+v", actual, paint)
						}
					})
				}
			}
		}
	}
}

func TestPPTXCharacterEffectApproximationWarnings(t *testing.T) {
	alpha := .8
	object := pptx.Object{ID: "foreign", Kind: "shape", Text: "Label", Fill: "#ffffff", Width: 100, Height: 100, Opacity: .4, OpacitySet: true, TextEffects: &pptx.TextEffects{Stroke: "#ff0000", StrokeWidth: 3, Opacity: &alpha, Shadow: &pptx.ShapeShadow{Color: "#aaff55", X: 100, Y: 100, Blur: 40}}}
	element, warnings, _, err := pptxObjectToElement(object, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, warning := range warnings {
		codes[warning.Code] = true
	}
	for _, code := range []string{"label-opacity-clamped", "text-outline-approximated", "text-shadow-approximated"} {
		if !codes[code] {
			t.Fatal("missing loss warning", code, warnings)
		}
	}
	label, ok := shapeLabel(element)
	if !ok || mustQuery(label.Query).Get("modern-opacity") != "1" {
		t.Fatal("invalid label opacity", label)
	}
}

func TestPPTXTextAndLabelGradientRoundTrip(t *testing.T) {
	for _, label := range []bool{false, true} {
		for _, direction := range []string{"horizontal", "vertical", "diagonal"} {
			for _, alpha := range []float64{0, .35, 1} {
				t.Run(fmt.Sprintf("label=%v/%s/alpha=%g", label, direction, alpha), func(t *testing.T) {
					paint := scenePaint{Color: "#ff0055", GradientStart: "#ff0055", GradientEnd: "#55ffff", GradientDirection: direction, Stroke: "#111111", StrokeWidth: 2, Opacity: &alpha}
					text := &sceneText{FontID: "c64", Size: 96, WidthScale: 1, Align: "center", Vertical: "middle", Runs: []sceneRun{{Text: "Editable gradient", Bold: true}}}
					source := sceneObject{ID: "gradient", Kind: "text", Text: text, Bounds: sceneRect{X: 100, Y: 100, Width: 600, Height: 200}, Paint: paint}
					if label {
						parentAlpha := .4
						source.Kind, source.Shape, source.Text, source.Label, source.LabelPaint = "shape", "square", nil, text, &paint
						source.Paint = scenePaint{Color: "#aaff55", Opacity: &parentAlpha}
					}
					object, warnings := sceneObjectToPPTX(source)
					if object == nil || object.TextEffects == nil || object.TextEffects.Gradient == nil || len(warnings) != 1 || warnings[0].Code != "text-gradient-reader-dependent" {
						t.Fatal("gradient omitted or loss diagnostic incorrect", object, warnings)
					}
					var out bytes.Buffer
					if _, err := pptx.Write(context.Background(), &out, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*object}}}}); err != nil {
						t.Fatal(err)
					}
					loaded, report, err := pptx.Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), pptx.Options{})
					if err != nil || len(report.Warnings) > 0 {
						t.Fatal(err, report)
					}
					element, warnings, _, err := pptxObjectToElement(loaded.Slides[0].Objects[0], 1, 0, 0)
					if err != nil || len(warnings) > 0 {
						t.Fatal(err, warnings)
					}
					if label {
						var ok bool
						element, ok = shapeLabel(element)
						if !ok {
							t.Fatal("gradient label was lost")
						}
					}
					actual := scenePaintFromValues(element, Slide{}, 1920.0/245, 1080.0/56, mustQuery(element.Query), nil)
					if !strings.EqualFold(actual.GradientStart, paint.GradientStart) || !strings.EqualFold(actual.GradientEnd, paint.GradientEnd) || actual.GradientDirection != direction || actual.Opacity == nil || math.Abs(*actual.Opacity-alpha) > 1e-6 || actual.Stroke != paint.Stroke {
						t.Fatalf("character gradient changed: %+v versus %+v", actual, paint)
					}
				})
			}
		}
	}
}
