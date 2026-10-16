package pptx

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEditableCharacterGradientPackage(t *testing.T) {
	var slides []Slide
	for _, direction := range []string{"horizontal", "vertical", "diagonal"} {
		for _, alpha := range []float64{0, .35, 1} {
			g := &ShapeGradient{Start: "#FF0055", End: "#55FFFF", Direction: direction}
			o := Object{ID: "gradient", Kind: "text", X: 180, Y: 180, Width: 1400, Height: 320, FontFace: "Arial", FontSize: 40, Color: g.Start, Text: "Editable gradient", TextEffects: &TextEffects{Gradient: g, Opacity: &alpha, Stroke: "#111111", StrokeWidth: 1, Shadow: &ShapeShadow{Color: "#ffffff", X: 4, Y: 4, Blur: 8}}, Paragraphs: []TextParagraph{{Runs: []TextRun{{Text: "Editable ", Bold: true}, {Text: "gradient", Italic: true}}}}}
			slides = append(slides, Slide{Background: "#101820", Objects: []Object{o}})
		}
	}
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Slides: slides}); err != nil {
		t.Fatal(err)
	}
	loaded, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	for i, slide := range loaded.Slides {
		o, want := slide.Objects[0], slides[i].Objects[0]
		if o.Text != want.Text || o.Kind != "text" || o.TextEffects == nil || !reflect.DeepEqual(o.TextEffects.Gradient, want.TextEffects.Gradient) || o.TextEffects.Opacity == nil || *o.TextEffects.Opacity != *want.TextEffects.Opacity || o.TextEffects.Shadow == nil || o.TextEffects.Stroke != "#111111" {
			t.Fatalf("gradient or composed character effects lost on slide %d: %+v", i, o)
		}
	}
	parts := packageParts(t, out.Bytes())
	root, err := parseXML(parts["ppt/slides/slide1.xml"], 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range descendants(root, "rPr") {
		if findChild(pr, "gradFill") == nil || findChild(pr, "solidFill") != nil || findChild(pr, "ln") == nil || findChild(pr, "effectLst") == nil {
			t.Fatal("gradient is not editable character fill", string(parts["ppt/slides/slide1.xml"]))
		}
	}
	if findFirst(root, "blip") != nil || strings.Contains(string(parts["ppt/slides/slide1.xml"]), "data:") {
		t.Fatal("text gradient was flattened")
	}
	if file := os.Getenv("KEYNOPE_TEXT_GRADIENT_FIXTURE"); file != "" {
		if err := os.WriteFile(file, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidCharacterGradientRejectsBeforeWrite(t *testing.T) {
	for _, g := range []*ShapeGradient{{Start: "invalid", End: "#ffffff", Direction: "horizontal"}, {Start: "#000000", End: "#ffffff", Direction: "radial"}} {
		var out bytes.Buffer
		_, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "text", Text: "Keep editable", TextEffects: &TextEffects{Gradient: g}}}}}})
		if err == nil || out.Len() != 0 {
			t.Fatal("bad character gradient wrote bytes", err, out.Len())
		}
	}
}

func TestCharacterGradientUnsupportedAndMixedPaintIsDiagnosed(t *testing.T) {
	fill := `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0055"/></a:gs><a:gs pos="100000"><a:srgbClr val="55FFFF"/></a:gs></a:gsLst><a:lin ang="0" scaled="1"/></a:gradFill>`
	for name, body := range map[string]string{
		"unsupported angle": strings.ReplaceAll(fill, `ang="0"`, `ang="1800000"`),
		"mixed run colour":  fill + `</a:rPr><a:t>First</a:t></a:r><a:r><a:rPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill>`,
		"mixed direction":   fill + `</a:rPr><a:t>First</a:t></a:r><a:r><a:rPr>` + strings.ReplaceAll(fill, `ang="0"`, `ang="5400000"`),
	} {
		t.Run(name, func(t *testing.T) {
			n, err := parseXML([]byte(`<p:sp xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:txBody><a:p><a:r><a:rPr>`+body+`</a:rPr><a:t>Last</a:t></a:r></a:p></p:txBody></p:sp>`), 64)
			if err != nil {
				t.Fatal(err)
			}
			_, loss := parseTextEffects(n, defaultColourResolver())
			if !loss {
				t.Fatal("unsupported character fill was silently reported exact")
			}
		})
	}
}
