package app

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestShapeRasterCatalogueMatchesUncached(t *testing.T) {
	for _, definition := range shapeDefinitions {
		for _, width := range []float64{.5, 11.5, 36} {
			for _, limit := range []int{0, 8, 245} {
				q := url.Values{"shape": {definition.ID}, "width": {fmt.Sprint(width)}, "height": {"9.5"}, "shape-offset-x": {"0.5"}, "shape-offset-y": {"0.5"}}
				for _, adjustment := range definition.Adjustments {
					q.Set("shape-"+adjustment.Key, fmt.Sprint(adjustment.Max))
				}
				w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
				ox, oy := int(math.Round(shapeSubcellOffset(q, "shape-offset-x")*2)), int(math.Round(shapeSubcellOffset(q, "shape-offset-y")*2))
				if limit > 0 {
					w = min(w, max(1, limit*2-ox))
				}
				want := renderShapeSilhouette(shapeGeometryForValues(q), w, h, ox, oy)
				element := Element{Kind: "shape", Query: q.Encode()}
				for i := 0; i < 2; i++ {
					if got := renderShapeRows(element, limit); !reflect.DeepEqual(got, want) {
						t.Fatal("cached silhouette changed", definition.ID, width, limit)
					}
				}
			}
		}
	}
}

func TestShapeRasterCacheOwnershipAndBounds(t *testing.T) {
	var cache shapeRasterCache
	key := shapeRasterKey{spec: "owned", width: 3, height: 2}
	source := []string{"▟█", "▀ "}
	first := cache.rows(key, func() []string { return source })
	source[0], first[1] = "Caller changed", "Caller changed"
	second := cache.rows(key, func() []string { t.Fatal("cache miss"); return nil })
	if !reflect.DeepEqual(second, []string{"▟█", "▀ "}) {
		t.Fatal("cache retained caller's slice", second)
	}
	second[0] = "Returned mutation"
	if cache.rows(key, func() []string { return nil })[0] != "▟█" {
		t.Fatal("cache exposed its stored slice")
	}
	for i := 0; i < shapeRasterEntryLimit*2; i++ {
		cache.rows(shapeRasterKey{spec: fmt.Sprint(i)}, func() []string { return []string{strings.Repeat("█", 1000)} })
		if len(cache.entries) > shapeRasterEntryLimit || cache.bytes > shapeRasterByteLimit {
			t.Fatal("raster cache is unbounded")
		}
	}
	oversize := shapeRasterKey{spec: "oversize"}
	cache.rows(oversize, func() []string { return []string{strings.Repeat("x", shapeRasterItemLimit+1)} })
	if _, retained := cache.entries[oversize]; retained {
		t.Fatal("oversized raster retained")
	}
}

func TestShapeRasterCacheConcurrent(t *testing.T) {
	var cache shapeRasterCache
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 200; n++ {
				got := cache.rows(shapeRasterKey{spec: fmt.Sprint(n % 5)}, func() []string { return []string{"▟█"} })
				if !reflect.DeepEqual(got, []string{"▟█"}) {
					t.Error("concurrent raster differs")
				}
				got[0] = "Independent result"
			}
		}()
	}
	workers.Wait()
}
