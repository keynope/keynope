package main

import (
	"fmt"
	"net/url"
	"testing"
)

func TestConnectorPortIndexMatchesScan(t *testing.T) {
	var ports []shapePort
	for i := 0; i < 400; i++ {
		for j, side := range []string{"left", "right", "top", "bottom"} {
			p := shapePort{ID: fmt.Sprint(i), Side: side, X: float64(i), Y: float64(j)}
			if i%2 == 0 {
				p.Direction = "top"
			}
			ports = append(ports, p)
		}
	}
	// Preserve the existing last-match semantics for duplicate authored IDs.
	ports = append(ports, shapePort{ID: "0", Side: "left", X: 91, Y: 92, Direction: "bottom"})
	index := indexConnectorPorts(ports)
	for i := -1; i <= 400; i++ {
		for _, side := range []string{"left", "right", "top", "bottom", "missing", ""} {
			q := url.Values{"connector-from": {fmt.Sprint(i)}, "connector-from-side": {side}, "connector-to": {fmt.Sprint(399 - i)}, "connector-to-side": {"left"}}
			var wantA, wantB shapePort
			for _, p := range ports {
				if p.ID == q.Get("connector-from") && p.Side == q.Get("connector-from-side") {
					wantA = p
				}
				if p.ID == q.Get("connector-to") && p.Side == q.Get("connector-to-side") {
					wantB = p
				}
			}
			if wantA.Direction != "" {
				wantA.Side = wantA.Direction
			}
			if wantB.Direction != "" {
				wantB.Side = wantB.Direction
			}
			a, b, ok := index.endpoints(q)
			valid := wantA.ID != "" && wantB.ID != "" && wantA.ID != wantB.ID
			if a != wantA || b != wantB || ok != valid {
				t.Fatalf("lookup %v differs from scan", q)
			}
		}
	}
	q := url.Values{"connector-from": {"0"}, "connector-from-side": {"left"}, "connector-to": {"0"}, "connector-to-side": {"right"}}
	if _, _, ok := index.endpoints(q); ok {
		t.Fatal("self-connection accepted")
	}
	ports[len(ports)-1].X = 101
	next := indexConnectorPorts(ports)
	a, _, _ := next.endpoints(q)
	old, _, _ := index.endpoints(q)
	if a.X != 101 || old.X != 91 {
		t.Fatal("layout pass did not isolate port positions")
	}
}
