package main

import (
	"math"
	"testing"
)

func TestRotatedConnectorObstacleSilhouette(t *testing.T) {
	for _, angle := range []float64{0, 37.5, 90, -125} {
		o := connectorObstacle{X: 50, Y: 15, W: 60, H: 16, Shape: "circle", Rotation: angle}
		p := projectConnectorObstacle(o, 245, 56)
		for y := 0; y < o.H; y++ {
			for x := 0; x < o.W; x++ {
				localX, localY := o.X+(float64(x)+.5)/2, o.Y+(float64(y)+.5)/2
				dx, dy := (localX-p.cx)*p.sx, (localY-p.cy)*p.sy
				wx, wy := p.cx+(dx*p.c-dy*p.s)/p.sx, p.cy+(dx*p.s+dy*p.c)/p.sy
				if p.contains(wx, wy) != shapeCellFilled(o.Shape, x, y, o.W, o.H) {
					t.Fatalf("angle %g pixel %d,%d changed coverage", angle, x, y)
				}
			}
		}
		if p.contains(p.left-1, p.top-1) {
			t.Fatal("outside bounds counted as solid")
		}
	}
}

func TestConnectorAvoidsRotatedObstacle(t *testing.T) {
	o := connectorObstacle{X: 50, Y: 20, W: 80, H: 12, Shape: "square", ID: "obstacle", Rotation: 90}
	p := projectConnectorObstacle(o, 245, 56)
	// This horizontal path crosses the rotated shape above the original box.
	y := p.cy - 6
	a, b := shapePort{ID: "a", Side: "right", X: 30, Y: y}, shapePort{ID: "b", Side: "left", X: 110, Y: y}
	e := Element{Kind: "connector", Query: "connector-mode=elbow&connector-width=1"}
	path := routedConnectorPath(e, a, b, []connectorObstacle{o}, 245, 56)
	if len(path) < 3 {
		t.Fatal("no detour", path)
	}
	for i := 1; i < len(path); i++ {
		u, v := path[i-1], path[i]
		steps := int(math.Ceil((math.Abs(v.X-u.X) + math.Abs(v.Y-u.Y)) * 8))
		for j := 0; j <= steps; j++ {
			f := float64(j) / float64(max(1, steps))
			if p.contains(u.X+(v.X-u.X)*f, u.Y+(v.Y-u.Y)*f) {
				t.Fatal("route crosses rotated silhouette", path)
			}
		}
	}
}

func TestConnectorObstaclesKeepAuthoredRotation(t *testing.T) {
	slide := Slide{Elements: []Element{{ID: "rotated", Kind: "shape", Query: "shape=square&top=10&left=30&width=20&height=10&object-rotation=37.5"}}}
	obstacles := connectorObstacles(slide, layout(slide, 245, 56), 245, 56)
	if len(obstacles) != 1 || obstacles[0].Rotation != 37.5 {
		t.Fatalf("rotation lost in layout: %+v", obstacles)
	}
}
