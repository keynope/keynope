package pptx

import (
	"bytes"
	"context"
	"math"
	"testing"
)

func TestObjectFrameRejectsBeforePackageWrite(t *testing.T) {
	for _, kind := range []string{"text", "shape", "image", "line"} {
		for _, field := range []string{"x", "y", "width", "height", "rotation"} {
			for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e100} {
				t.Run(kind+"/"+field+"/"+fmtFrameValue(value), func(t *testing.T) {
					o := Object{Kind: kind, Width: 40, Height: 20}
					switch field {
					case "x":
						o.X = value
					case "y":
						o.Y = value
					case "width":
						o.Width = value
					case "height":
						o.Height = value
					case "rotation":
						o.Rotation = value
					}
					var out bytes.Buffer
					if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err == nil || out.Len() != 0 {
						t.Fatalf("invalid frame emitted package: err=%v bytes=%d", err, out.Len())
					}
				})
			}
		}
		for _, o := range []Object{{Kind: kind, Width: -1, Height: 20}, {Kind: kind, Width: 20, Height: -1}, {Kind: kind, Width: 20, Height: 20, Rotation: 40000}} {
			if kind == "line" && o.Rotation == 0 {
				continue // Signed connector deltas are normalized by its writer.
			}
			var out bytes.Buffer
			if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err == nil || out.Len() != 0 {
				t.Fatalf("invalid dimension/angle accepted for %s: %v", kind, err)
			}
		}
	}
}

func fmtFrameValue(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "positive-infinity"
	}
	if math.IsInf(value, -1) {
		return "negative-infinity"
	}
	return "overflow"
}

func TestObjectFrameAllowsOffCanvasAndSingleAxisLines(t *testing.T) {
	for _, o := range []Object{
		{Kind: "text", X: -12.5, Y: -3.25, Width: 40.5, Height: 20.25, Rotation: -90},
		{Kind: "shape", X: -12.5, Y: 3.25, Width: 40.5, Height: 20.25, Rotation: 720},
		{Kind: "line", X: -12.5, Y: -3.25, Width: 0, Height: 20.25},
		{Kind: "line", X: -12.5, Y: -3.25, Width: 40.5, Height: 0},
		{Kind: "line", X: 12.5, Y: 3.25, Width: -40.5, Height: -20.25},
	} {
		var out bytes.Buffer
		if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil || out.Len() == 0 {
			t.Fatalf("valid fractional/off-canvas frame rejected: %v", err)
		}
	}
}
