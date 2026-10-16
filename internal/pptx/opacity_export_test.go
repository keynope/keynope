package pptx

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestEditableObjectOpacityRoundTrip(t *testing.T) {
	for _, kind := range []string{"shape", "text", "line"} {
		for _, alpha := range []float64{0, .25, 1} {
			t.Run(fmt.Sprintf("%s/%g", kind, alpha), func(t *testing.T) {
				o := Object{ID: "alpha", Kind: kind, Width: 200, Height: 80, Shape: "rect", Stroke: "#123456", StrokeWidth: 2, Opacity: alpha, OpacitySet: true}
				if kind != "line" {
					o.Text, o.Color = "Two words", "#ffffff"
					o.Paragraphs = []TextParagraph{{Runs: []TextRun{{Text: "Two ", Color: "#ff0000"}, {Text: "words", Color: "#0000ff"}}}}
				}
				if kind == "shape" {
					o.Fill = "#abcdef"
				}
				var b bytes.Buffer
				if _, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
					t.Fatal(err)
				}
				xml := string(packageParts(t, b.Bytes())["ppt/slides/slide1.xml"])
				want := fmt.Sprintf(`<a:alpha val="%d"/>`, int(alpha*100000))
				// Every explicit paint gets alpha: fill, stroke, paragraph default and runs.
				if strings.Count(xml, "<a:solidFill>") != strings.Count(xml, want) {
					t.Fatal("unmodified paint alpha", xml)
				}
				deck, _, err := Read(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len()), Options{})
				if err != nil {
					t.Fatal(err)
				}
				got := deck.Slides[0].Objects[0]
				if !got.OpacitySet || got.Opacity != alpha || got.Text != o.Text {
					t.Fatalf("lost alpha or text: %+v", got)
				}
			})
		}
	}
}

func TestObjectOpacityLegacyAndValidation(t *testing.T) {
	if strings.Contains(objectSolidFill("#ffffff", Object{}), "alpha") {
		t.Fatal("unspecified alpha became transparent")
	}
	if !strings.Contains(objectSolidFill("#ffffff", Object{Opacity: .5}), `val="50000"`) {
		t.Fatal("legacy nonzero alpha lost")
	}
	for _, alpha := range []float64{-1, 2, math.NaN(), math.Inf(1)} {
		var b bytes.Buffer
		_, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "shape", Opacity: alpha, OpacitySet: true}}}}})
		if err == nil || b.Len() != 0 {
			t.Fatal("invalid alpha wrote a package", alpha, err)
		}
	}
}
