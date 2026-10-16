package pptx

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
)

func TestNativeConnectorDirections(t *testing.T) {
	for _, delta := range [][2]float64{{100, 0}, {-100, 0}, {0, 100}, {0, -100}, {100, 80}, {-100, 80}, {100, -80}, {-100, -80}} {
		o := Object{ID: "line", Kind: "line", X: 200, Y: 200, Width: delta[0], Height: delta[1], Stroke: "#123456", StrokeWidth: 2, Opacity: .25, OpacitySet: true, Link: "https://example.com", LineHead: LineEnd{Type: "triangle", Width: "sm", Length: "lg"}, LineTail: LineEnd{Type: "oval", Width: "lg", Length: "med"}}
		var b bytes.Buffer
		report, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{o}}}})
		if err != nil || len(report.Warnings) != 0 {
			t.Fatal(delta, err, report)
		}
		xml := string(packageParts(t, b.Bytes())["ppt/slides/slide1.xml"])
		if !strings.Contains(xml, `<p:cxnSp>`) || !strings.Contains(xml, `prst="line"`) || !strings.Contains(xml, `<a:hlinkClick`) || strings.Contains(xml, `<p:sp>`) {
			t.Fatal(xml)
		}
		deck, _, err := Read(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len()), Options{})
		if err != nil || len(deck.Slides[0].Objects) != 1 {
			t.Fatal(delta, err)
		}
		got := deck.Slides[0].Objects[0]
		if got.LineHead != o.LineHead || got.LineTail != o.LineTail || got.Link != o.Link {
			t.Fatal("line metadata lost", got)
		}
		if got.Kind != "line" || got.Width != math.Abs(delta[0]) || got.Height != math.Abs(delta[1]) || got.X != 200+math.Min(0, delta[0]) || got.Y != 200+math.Min(0, delta[1]) || got.FlipH != (delta[0] < 0) || got.FlipV != (delta[1] < 0) || got.Opacity != .25 || got.StrokeWidth != 2 {
			t.Fatalf("%v: %+v", delta, got)
		}
	}
}

func TestConnectorArrowValidation(t *testing.T) {
	for _, end := range []LineEnd{{Type: "invalid"}, {Type: "triangle", Width: "giant"}, {Type: "oval", Length: "invalid"}} {
		var b bytes.Buffer
		_, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "line", Width: 10, LineHead: end}}}}})
		if err == nil || b.Len() != 0 {
			t.Fatal("invalid arrow wrote output", end, err)
		}
	}
}

func TestNativeConnectorInvalidGeometry(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), 1e10} {
		var b bytes.Buffer
		_, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "line", Width: v, Height: 10}}}}})
		if err == nil || b.Len() != 0 {
			t.Fatal("invalid line wrote output", v, err)
		}
	}
}

func TestNativePolylineConnectorRoutes(t *testing.T) {
	routes := [][]LinePoint{
		{{200, 100}, {300, 100}, {300, 250}, {450, 250}},
		{{450, 250}, {300, 250}, {300, 100}, {200, 100}},
		{{200, 100}, {500, 100}, {500, 400}, {100, 400}, {100, 250}, {450, 250}},
		{{200, 100}, {200, 200}, {200, 100}},
		{{200, 100}, {300, 100}, {200, 100}},
	}
	for _, route := range routes {
		object := Object{ID: "routed", Kind: "line", LinePoints: route, Stroke: "#123456", StrokeWidth: 2, LineHead: LineEnd{Type: "triangle"}, LineTail: LineEnd{Type: "triangle"}, Opacity: .5, OpacitySet: true, Link: "https://example.com"}
		var out bytes.Buffer
		report, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{object}}}})
		if err != nil || len(report.Warnings) > 0 {
			t.Fatal(err, report)
		}
		xml := string(packageParts(t, out.Bytes())["ppt/slides/slide1.xml"])
		if !strings.Contains(xml, "<a:custGeom>") || strings.Contains(xml, `prst="line"`) || strings.Count(xml, "<a:lnTo>") != len(route)-1 || strings.Contains(xml, "<a:close") {
			t.Fatal(xml)
		}
		deck, _, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := deck.Slides[0].Objects[0]
		if len(got.LinePoints) != len(route) || got.LineHead.Type != "triangle" || got.LineTail.Type != "triangle" || got.Link != object.Link || got.Opacity != .5 {
			t.Fatal(got)
		}
		for i, p := range got.LinePoints {
			if math.Abs(p.X-route[i].X) > 1.0/6350 || math.Abs(p.Y-route[i].Y) > 1.0/6350 {
				t.Fatalf("vertex %d: %+v != %+v", i, p, route[i])
			}
		}
	}
}

func TestPolylineConnectorRejectsInvalidVerticesBeforeWrite(t *testing.T) {
	for _, points := range [][]LinePoint{{{1, 2}}, make([]LinePoint, 1025), {{0, 0}, {math.NaN(), 0}}, {{0, 0}, {0, math.Inf(1)}}, {{0, 0}, {1e10, 0}}} {
		var out bytes.Buffer
		_, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "line", LinePoints: points}}}}})
		if err == nil || out.Len() != 0 {
			t.Fatal("invalid route wrote output", err, out.Len())
		}
	}
}
