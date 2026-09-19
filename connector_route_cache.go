package main

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

type routeCacheEntry struct {
	key    [32]byte
	points []connectorPoint
}

// Retain hot routes without periodically discarding the entire working set.
// Hash complete routing inputs, not IDs alone. Copies isolate callers from
// cache ownership; entry and byte limits bound even pathological routes.
type routeCache struct {
	mu                       sync.Mutex
	limit, maxEntries, bytes int
	order                    list.List
	entries                  map[[32]byte]*list.Element
}

var connectorRouteCache = routeCache{limit: 16 << 20, maxEntries: 1024}

func (c *routeCache) get(key [32]byte) []connectorPoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		return append([]connectorPoint(nil), entry.Value.(routeCacheEntry).points...)
	}
	return nil
}

func (c *routeCache) put(key [32]byte, points []connectorPoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cost := 32 + 16*len(points)
	if cost > c.limit || c.maxEntries <= 0 || len(points) == 0 {
		return
	}
	if entry := c.entries[key]; entry != nil {
		c.bytes -= 32 + 16*len(entry.Value.(routeCacheEntry).points)
		c.order.Remove(entry)
		delete(c.entries, key)
	}
	for c.bytes+cost > c.limit || c.order.Len() >= c.maxEntries {
		entry := c.order.Back()
		old := entry.Value.(routeCacheEntry)
		delete(c.entries, old.key)
		c.bytes -= 32 + 16*len(old.points)
		c.order.Remove(entry)
	}
	if c.entries == nil {
		c.entries = make(map[[32]byte]*list.Element)
	}
	c.entries[key] = c.order.PushFront(routeCacheEntry{key, append([]connectorPoint(nil), points...)})
	c.bytes += cost
}

func connectorRouteKey(data []byte) [32]byte { return sha256.Sum256(data) }
