package pptx

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestTextEffectsAreEditableCharacterPaint(t *testing.T) {
	var slides []Slide
	for _, rich := range []bool{false, true} {
		alpha := .2
		object := Object{ID: "effect-text", Kind: "text", Width: 600, Height: 200, Text: "Editable words", Color: "#ffaa55", FontFace: "Arial", FontSize: 40, TextEffects: &TextEffects{Stroke: "#111111", StrokeWidth: 1, Shadow: &ShapeShadow{Color: "#ffffff", X: 8, Y: 20, Blur: 8}, Opacity: &alpha}}
		if rich {
			object.Paragraphs = []TextParagraph{{Runs: []TextRun{{Text: "Editable ", Bold: true, Color: "#ffaa55"}, {Text: "words", Italic: true, Color: "#55aaff"}}}}
		}
		slides = append(slides, Slide{Objects: []Object{object}})
	}
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Slides: slides}); err != nil {
		t.Fatal(err)
	}
	if file := os.Getenv("KEYNOPE_TEXT_EFFECTS_FIXTURE"); file != "" {
		if err := os.WriteFile(file, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, _, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, slide := range loaded.Slides {
		object := slide.Objects[0]
		if object.Text != "Editable words" || object.Kind != "text" || object.TextEffects == nil || object.TextEffects.Shadow == nil || object.TextEffects.StrokeWidth != 1 || !strings.EqualFold(object.TextEffects.Stroke, "#111111") {
			t.Fatal(object)
		}
		if object.Opacity != .2 || !object.OpacitySet {
			t.Fatal("character alpha lost", object.Opacity)
		}
		xml := string(packageParts(t, out.Bytes())["ppt/slides/slide"+string(rune('1'+i))+".xml"])
		root, err := parseXML([]byte(xml), 64)
		if err != nil {
			t.Fatal(err)
		}
		pr := findFirst(root, "rPr")
		if findChild(pr, "ln") == nil || findChild(pr, "effectLst") == nil || findFirst(root, "blip") != nil {
			t.Fatal("text flattened or missing character effects", xml)
		}
		shapeProperties := findFirst(root, "spPr")
		if findChild(shapeProperties, "effectLst") != nil {
			t.Fatal("character shadow incorrectly applied to the box")
		}
	}
}
