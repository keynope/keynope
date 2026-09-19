package main

import (
	"math"
	"net/url"
	"path/filepath"
	"testing"
)

func TestFractionalShapeAnchorsAgreeAcrossSceneAndRouting(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		for _, tc := range []struct {
			query string
			x, y  float64
		}{
			{"align=center&valign=middle", (245 - 37.125) / 2, (56 - 8.875) / 2},
			{"align=right&valign=bottom", 245 - 37.125, 56 - 8.875},
			{"right=3&bottom=2", 245 - 37.125 - 3, 56 - 8.875 - 2},
			{"left=5&top=4&align=center&valign=middle", 5, 4},
		} {
			e := Element{ID: "shape", Kind: "shape", Text: "[shape:square]", Query: "shape=square&width=37.125&height=8.875&object-offset-x=0.125&object-offset-y=0.375&object-rotation=37.5&" + tc.query}
			d := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{Elements: []Element{e}}}}
			scene, err := buildMixedSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			b := scene.Objects[0].Bounds
			x, y := tc.x+.125, tc.y+.375
			if math.Abs(b.X-x*1920/245) > 1e-7 || math.Abs(b.Y-y*1080/56) > 1e-7 {
				t.Fatalf("%s %s: %+v", style, tc.query, b)
			}
			lines := layout(d.Slides[0], 245, 56)
			obstacles := connectorObstacles(d.Slides[0], lines, 245, 56)
			if len(obstacles) != 1 || math.Abs(obstacles[0].X-x) > 1e-7 || math.Abs(obstacles[0].Y-y) > 1e-7 {
				t.Fatalf("obstacle disagrees: %+v", obstacles)
			}
			actual := slideShapePorts(d.Slides[0], lines, 245, 56)
			// Explicit-origin reference, independent of layout's covering box.
			q, _ := url.ParseQuery(e.Query)
			for _, k := range []string{"align", "valign", "right", "bottom"} {
				q.Del(k)
			}
			e.Query = q.Encode()
			expected := rotateShapePorts(shapePorts(e, tc.x, tc.y), e, tc.x, tc.y, 245, 56)
			if len(actual) != len(expected) {
				t.Fatal("missing ports")
			}
			for i, p := range actual {
				if math.Abs(p.X-expected[i].X) > 1e-7 || math.Abs(p.Y-expected[i].Y) > 1e-7 {
					t.Fatalf("port disagrees: %+v vs %+v", p, expected[i])
				}
			}
		}
	}
}

func TestFractionalShapeSceneAndPersistence(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{Elements: []Element{{ID: "shape", Kind: "shape", Text: "[shape:square]", Query: "shape=square&top=4&left=5&width=37.125&height=8.875&object-rotation=37.5"}}}}}
		check := func(d Deck) {
			t.Helper()
			scene, err := buildMixedSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			if len(scene.Objects) != 1 {
				t.Fatal("missing shape")
			}
			o := scene.Objects[0]
			if math.Abs(o.Bounds.Width-37.125*1920/245) > 1e-7 || math.Abs(o.Bounds.Height-8.875*1080/56) > 1e-7 || o.Rotation != 37.5 {
				t.Fatalf("rounded geometry: %+v", o.Bounds)
			}
			if style == "retro" {
				if o.SampleScale == nil || o.RetroLines == nil {
					t.Fatal("missing sampled scaling")
				}
				if math.Abs(o.SampleScale.X*37-37.125) > 1e-9 || math.Abs(o.SampleScale.Y*9-8.875) > 1e-9 {
					t.Fatal("sampled extent differs from authored extent")
				}
			} else if o.SampleScale != nil {
				t.Fatal("modern geometry should not use a sampled scale")
			}
		}
		check(deck)
		path := filepath.Join(t.TempDir(), "Shape.md")
		data, err := serializeDeck(path, deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		check(loaded)
	}
}

func TestFractionalShapePortsAndObstacles(t *testing.T) {
	for _, shape := range []string{"square", "circle", "triangle", "diamond"} {
		e := Element{ID: "shape", Kind: "shape", Text: "[shape:" + shape + "]", Query: "shape=" + shape + "&width=37.125&height=8.875&object-rotation=37.5"}
		ports := shapePorts(e, 5, 4)
		rotated := rotateShapePorts(append([]shapePort(nil), ports...), e, 5, 4, 245, 56)
		o := connectorObstacle{X: 5, Y: 4, W: 74, H: 18, Width: 37.125, Height: 8.875, Shape: shape, Rotation: 37.5}
		p := projectConnectorObstacle(o, 245, 56)
		for i, port := range ports {
			dx, dy := (port.X-p.cx)*p.sx, (port.Y-p.cy)*p.sy
			x, y := p.cx+(dx*p.c-dy*p.s)/p.sx, p.cy+(dx*p.s+dy*p.c)/p.sy
			if math.Abs(x-rotated[i].X) > 1e-9 || math.Abs(y-rotated[i].Y) > 1e-9 {
				t.Fatal("port and obstacle pivots differ")
			}
		}
		// Independently sample every raster cell at its continuous centre,
		// rotate it, and ask the router about actual silhouette coverage.
		for y := 0; y < o.H; y++ {
			for x := 0; x < o.W; x++ {
				lx := o.X + (float64(x)+.5)*o.Width/float64(o.W)
				ly := o.Y + (float64(y)+.5)*o.Height/float64(o.H)
				dx, dy := (lx-p.cx)*p.sx, (ly-p.cy)*p.sy
				wx, wy := p.cx+(dx*p.c-dy*p.s)/p.sx, p.cy+(dx*p.s+dy*p.c)/p.sy
				if p.contains(wx, wy) != shapeCellFilled(shape, x, y, o.W, o.H) {
					t.Fatalf("%s cell %d,%d differs", shape, x, y)
				}
			}
		}
	}
}
