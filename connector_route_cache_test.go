package main

import (
	"fmt"
	"testing"
)

func TestRouteCacheWorkingSetOwnershipAndEviction(t *testing.T) {
	c := routeCache{limit: 1 << 20, maxEntries: 256}
	key := func(i int) [32]byte { return connectorRouteKey([]byte(fmt.Sprint(i))) }
	for i := 0; i < 200; i++ {
		points := []connectorPoint{{float64(i), 2}}
		c.put(key(i), points)
		points[0].X = -1
	}
	for i := 0; i < 200; i++ {
		got := c.get(key(i))
		if len(got) != 1 || got[0].X != float64(i) {
			t.Fatalf("lost route %d", i)
		}
		got[0].X = -1
	}
	if c.get(key(0))[0].X != 0 {
		t.Fatal("caller mutated cache")
	}
	// Route 0 was just touched, so the next overflow must evict route 1.
	for i := 200; i < 257; i++ {
		c.put(key(i), []connectorPoint{{1, 2}})
	}
	if c.get(key(0)) == nil || c.get(key(1)) != nil || c.order.Len() != 256 {
		t.Fatal("LRU eviction discarded hot route")
	}
	c = routeCache{limit: 96, maxEntries: 100}
	c.put(key(0), []connectorPoint{{1, 2}})
	c.put(key(1), []connectorPoint{{1, 2}})
	c.put(key(2), []connectorPoint{{1, 2}})
	if c.bytes != 96 || c.get(key(0)) != nil || c.get(key(2)) == nil {
		t.Fatal("byte budget not enforced")
	}
	c.put(key(2), []connectorPoint{{3, 4}, {5, 6}})
	if c.bytes != 64 || c.order.Len() != 1 {
		t.Fatal("replacement accounting incorrect")
	}
	c.put(key(3), make([]connectorPoint, 100))
	if c.bytes != 64 || c.get(key(3)) != nil {
		t.Fatal("oversized route cached")
	}
}
