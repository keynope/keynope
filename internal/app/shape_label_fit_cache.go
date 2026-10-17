package app

import "sync"

type shapeLabelFitKey struct {
	query                     string
	typefaces                 string
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
	return cache.fitWithFonts(element, cols, rows, nil, canvas...)
}

func (cache *shapeLabelFitCache) fitWithFonts(element *Element, cols, rows int, fonts map[string]DeckTypeface, canvas ...sceneRect) bool {
	if element.Kind != "shape" {
		return false
	}
	width, height := pptxCanvasDimensions(canvas)
	key := shapeLabelFitKey{query: element.Query, typefaces: typefaceAvailabilityKey(fonts), cols: cols, rows: rows, canvasWidth: width, canvasHeight: height}
	cache.mu.Lock()
	query, found := cache.queries[key]
	cache.mu.Unlock()
	if found {
		element.Query = query
		return true
	}
	fitShapeLabelWithFontsUncached(element, cols, rows, fonts, canvas...)
	cost := len(key.query) + len(key.typefaces) + len(element.Query) + 96
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
