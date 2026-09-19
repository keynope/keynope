package main

import "testing"

func TestConnectorRoutingPassDependencies(t *testing.T) {
	base := connectorObstacle{X: 30, Y: 10, W: 20, H: 12, Shape: "square", ID: "obstacle", Rotation: 37.5}
	a, b := shapePort{ID: "a", X: 5, Y: 5, Side: "right"}, shapePort{ID: "b", X: 80, Y: 40, Side: "left"}
	pass := connectorRoutingPass{obstacles: []connectorObstacle{base}}
	want := pass.key(a, b, 100, 56, 1)
	if !pass.ready || pass.key(a, b, 100, 56, 1) != want {
		t.Fatal("routing pass did not retain a stable fingerprint")
	}
	for _, mutate := range []func(*connectorObstacle){
		func(o *connectorObstacle) { o.X++ },
		func(o *connectorObstacle) { o.Y++ },
		func(o *connectorObstacle) { o.W++ },
		func(o *connectorObstacle) { o.H++ },
		func(o *connectorObstacle) { o.Shape = "circle" },
		func(o *connectorObstacle) { o.Rotation++ },
		func(o *connectorObstacle) { o.ID = "replacement" },
	} {
		changed := base
		mutate(&changed)
		next := connectorRoutingPass{obstacles: []connectorObstacle{changed}}
		if next.key(a, b, 100, 56, 1) == want {
			t.Fatalf("changed obstacle reused route: %+v", changed)
		}
	}
	changedA, changedB := a, b
	changedA.X++
	changedB.Side = "top"
	for _, key := range [][32]byte{
		pass.key(changedA, b, 100, 56, 1), pass.key(a, changedB, 100, 56, 1),
		pass.key(a, b, 101, 56, 1), pass.key(a, b, 100, 57, 1), pass.key(a, b, 100, 56, 2),
	} {
		if key == want {
			t.Fatal("endpoint, viewport or width change reused route")
		}
	}
	if pass.obstacles[0] != base {
		t.Fatal("fingerprinting mutated source obstacles")
	}
}

func TestConnectorRoutingPassSkipsManualRoutes(t *testing.T) {
	pass := connectorRoutingPass{}
	pass.path(Element{Query: "connector-mode=straight"}, shapePort{}, shapePort{X: 10}, 100, 56)
	if pass.ready {
		t.Fatal("straight connector needlessly fingerprinted obstacles")
	}
}
