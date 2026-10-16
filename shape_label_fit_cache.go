package main

import "sync"

type shapeLabelFitKey struct {
	query                     string
	cols, rows                int
	canvasWidth, canvasHeight float64
}
type shapeLabelFitCache struct {
	mu      sync.Mutex
	queries map[shapeLabelFitKey]string
	bytes   int
}

var sharedShapeLabelFits shapeLabelFitCache

// Fitting reads the shape query, embedded label/style and canvas dimensions,
// and changes only Query. Cache strings, never authored elements or pointers.
// A complete query key deliberately includes unknown extensions and paint.
func (cache *shapeLabelFitCache) fit(element *Element, cols, rows int, canvas ...sceneRect) bool {
	if element.Kind != "shape" {
		return false
	}
	width, height := pptxCanvasDimensions(canvas)
	key := shapeLabelFitKey{query: element.Query, cols: cols, rows: rows, canvasWidth: width, canvasHeight: height}
	cache.mu.Lock()
	query, found := cache.queries[key]
	cache.mu.Unlock()
	if found {
		element.Query = query
		return true
	}
	fitShapeLabelUncached(element, cols, rows, canvas...)
	cost := len(key.query) + len(element.Query) + 96
	const limit = 4 << 20
	if cost > limit {
		return false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, found := cache.queries[key]; !found {
		if cache.queries == nil || cache.bytes+cost > limit || len(cache.queries) >= 2048 {
			cache.queries = make(map[shapeLabelFitKey]string)
			cache.bytes = 0
		}
		cache.queries[key] = element.Query
		cache.bytes += cost
	}
	return false
}
