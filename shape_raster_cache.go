package main

import "sync"

// Only immutable silhouette rows are cached: never colour, captions, authored
// objects, source maps or connector positions. All raster inputs are explicit.
type shapeRasterKey struct {
	spec                string
	aspect              float64
	width, height, x, y int
}

type shapeRasterCache struct {
	mu      sync.Mutex
	entries map[shapeRasterKey][]string
	bytes   int
}

const shapeRasterByteLimit = 4 << 20
const shapeRasterEntryLimit = 256
const shapeRasterItemLimit = 256 << 10

var sharedShapeRasters shapeRasterCache

func (cache *shapeRasterCache) rows(key shapeRasterKey, render func() []string) []string {
	cache.mu.Lock()
	rows, found := cache.entries[key]
	cache.mu.Unlock()
	if found {
		return append([]string(nil), rows...)
	}
	rows = render()
	cost := len(key.spec) + 128 + len(rows)*16
	for _, row := range rows {
		cost += len(row)
	}
	if cost > shapeRasterItemLimit {
		return rows
	}
	owned := append([]string(nil), rows...)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, found := cache.entries[key]; !found {
		if cache.entries == nil || cache.bytes+cost > shapeRasterByteLimit || len(cache.entries) >= shapeRasterEntryLimit {
			cache.entries, cache.bytes = make(map[shapeRasterKey][]string), 0
		}
		cache.entries[key] = owned
		cache.bytes += cost
	}
	return rows
}
