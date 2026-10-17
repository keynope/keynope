package app

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

// Deliberately retains the original area scan, with no row clipping. Its
// separate run construction supports negative coordinates (the old -1
// sentinel lost the left edge). This is a coverage oracle, not a timing gate.
func referenceStrokeScan(g sceneShapeGeometry, bounds sceneRect, width, cw, ch float64) ([]sceneRect, bool) {
	dx, dy, pad := cw/2, ch/2, width/2
	limits := func() (int, int, int, int) {
		return int(math.Floor(-pad / dx)), int(math.Ceil((bounds.Width + pad) / dx)), int(math.Floor(-pad / dy)), int(math.Ceil((bounds.Height + pad) / dy))
	}
	x0, x1, y0, y1 := limits()
	reduced := int64(x1-x0)*int64(y1-y0) > 262144
	if reduced {
		factor := math.Ceil(math.Sqrt(float64(int64(x1-x0)*int64(y1-y0)) / 262144))
		dx, dy = dx*factor, dy*factor
		x0, x1, y0, y1 = limits()
	}
	grid := make([]bool, (x1-x0)*(y1-y0))
	for _, segment := range shapeBoundarySegments(g, bounds.Width, bounds.Height) {
		left := max(x0, int(math.Floor((math.Min(segment.a.X, segment.b.X)-pad)/dx)))
		right := min(x1, int(math.Ceil((math.Max(segment.a.X, segment.b.X)+pad)/dx)))
		top := max(y0, int(math.Floor((math.Min(segment.a.Y, segment.b.Y)-pad)/dy)))
		bottom := min(y1, int(math.Ceil((math.Max(segment.a.Y, segment.b.Y)+pad)/dy)))
		for y := top; y < bottom; y++ {
			for x := left; x < right; x++ {
				index := (y-y0)*(x1-x0) + x - x0
				grid[index] = grid[index] || strokeSegmentTouchesCell(segment.a, segment.b, float64(x)*dx, float64(y)*dy, dx, dy, pad)
			}
		}
	}
	result := []sceneRect{}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; {
			if !grid[(y-y0)*(x1-x0)+x-x0] {
				x++
				continue
			}
			start := x
			for x < x1 && grid[(y-y0)*(x1-x0)+x-x0] {
				x++
			}
			result = append(result, sceneRect{X: float64(start) * dx, Y: float64(y) * dy, Width: float64(x-start) * dx, Height: dy})
		}
	}
	return result, reduced
}

func TestStrokeRowScanCoverage(t *testing.T) {
	for _, definition := range shapeDefinitions {
		for _, size := range [][2]float64{{91, 53}, {37.25, 170.5}, {640, 360}} {
			for _, width := range []float64{.25, 2, 17.5, 64} {
				for _, extreme := range []bool{false, true} {
					spec := ShapeSpec{Kind: definition.ID, Adjustments: map[string]float64{}}
					for _, adjustment := range definition.Adjustments {
						if extreme {
							spec.Adjustments[adjustment.Key] = adjustment.Max
						} else {
							spec.Adjustments[adjustment.Key] = adjustment.Min
						}
					}
					g := shapeGeometry(spec, size[0]/size[1])
					bounds := sceneRect{X: 100, Y: 200, Width: size[0], Height: size[1]}
					got, reduced := retroShapeStroke(g, bounds, width, 1920.0/245, 1080.0/56)
					want, wantReduced := referenceStrokeScan(g, bounds, width, 1920.0/245, 1080.0/56)
					if reduced != wantReduced || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s size=%v width=%g extreme=%v coverage changed", definition.ID, size, width, extreme)
					}
				}
			}
		}
	}
}

func TestStrokeRowScanNegativeAndReducedCoverage(t *testing.T) {
	g := shapeGeometry(ShapeSpec{Kind: "square"}, 1)
	got, _ := retroShapeStroke(g, sceneRect{Width: 20, Height: 20}, 2, 4, 4)
	left := false
	for _, rect := range got {
		if rect.X < 0 {
			left = true
		}
	}
	if !left {
		t.Fatal("left border outside local origin was dropped")
	}
	for _, kind := range []string{"triangle", "star", "cloud"} {
		g := shapeGeometry(ShapeSpec{Kind: kind}, 1)
		bounds := sceneRect{Width: 3000, Height: 3000}
		got, reduced := retroShapeStroke(g, bounds, 7, 4, 4)
		want, wantReduced := referenceStrokeScan(g, bounds, 7, 4, 4)
		if !reduced || !wantReduced || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s reduced coverage changed", kind)
		}
	}
}

func BenchmarkStrokeRowScan(b *testing.B) {
	for _, kind := range []string{"triangle", "star", "cloud", "square"} {
		g := shapeGeometry(ShapeSpec{Kind: kind}, 1920.0/1080)
		for _, reference := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/reference=%t", kind, reference), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if reference {
						referenceStrokeScan(g, sceneRect{Width: 1920, Height: 1080}, 2, 1920.0/245, 1080.0/56)
					} else {
						retroShapeStroke(g, sceneRect{Width: 1920, Height: 1080}, 2, 1920.0/245, 1080.0/56)
					}
				}
			})
		}
	}
}
