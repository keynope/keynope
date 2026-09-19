package main

import (
	"math"
	"testing"
)

func connectorPathCrossesObstacle(path []connectorPoint, obstacles []connectorObstacle, cols, rows int) bool {
	projected := make([]projectedConnectorObstacle, len(obstacles))
	for index, obstacle := range obstacles {
		projected[index] = projectConnectorObstacle(obstacle, cols, rows)
	}
	for index := 1; index < len(path); index++ {
		a, b := path[index-1], path[index]
		steps := max(1, int(math.Ceil((math.Abs(b.X-a.X)+math.Abs(b.Y-a.Y))*8)))
		for step := 0; step <= steps; step++ {
			fraction := float64(step) / float64(steps)
			x, y := a.X+(b.X-a.X)*fraction, a.Y+(b.Y-a.Y)*fraction
			for obstacleIndex := range projected {
				if projected[obstacleIndex].contains(x, y) {
					return true
				}
			}
		}
	}
	return false
}

func TestDenseConnectorRoutingStillFindsClearPath(t *testing.T) {
	const cols, rows = 245, 56
	// One central barrier invalidates the direct elbow. Numerous small,
	// independently positioned objects stress the routing graph without closing
	// the clear lower corridor. A dense slide must not silently fall back to a
	// path through the central shape solely because its graph is large.
	obstacles := []connectorObstacle{{X: 100, Y: 20, W: 80, H: 32, Width: 40, Height: 16, Shape: "square", ID: "barrier"}}
	for index := 0; index < 120; index++ {
		x := 8 + math.Mod(float64(index)*17.137, 225)
		y := 1 + math.Mod(float64(index)*7.319, 14)
		if index%2 != 0 {
			y = 40 + math.Mod(float64(index)*5.173, 13)
		}
		obstacles = append(obstacles, connectorObstacle{X: x, Y: y, W: 2, H: 2, Width: 1, Height: 1, Shape: "circle", ID: "noise"})
	}
	a := shapePort{ID: "a", Side: "right", X: 5, Y: 28}
	b := shapePort{ID: "b", Side: "left", X: 240, Y: 28}
	element := Element{Kind: "connector", Query: "connector-mode=elbow&connector-width=1"}
	path := routedConnectorPath(element, a, b, obstacles, cols, rows)
	if len(path) < 4 {
		t.Fatalf("dense route did not detour: %+v", path)
	}
	if connectorPathCrossesObstacle(path, obstacles, cols, rows) {
		t.Fatalf("dense route crossed a silhouette despite an open corridor: %+v", path)
	}
}
