package app

import (
	"container/list"
	"net/url"
	"strconv"
	"sync"
)

const maxSceneObjectLayoutEntries = 2048
const maxSceneObjectLayoutBytes = 8 << 20

type sceneObjectLayoutKey struct {
	input [32]byte
	flow  preparedLayoutFlow
}

type sceneObjectLayoutEntry struct {
	geometry sceneLayoutEntry // Owned blank bounds/anchors, never text or media.
	flow     preparedLayoutFlow
}

type sceneObjectLayoutCache struct {
	mu      sync.Mutex
	entries map[sceneObjectLayoutKey]*list.Element
	order   list.List
	bytes   int
}

type retainedObjectLayout struct {
	key   sceneObjectLayoutKey
	entry sceneObjectLayoutEntry
}

var sharedSceneObjectLayouts sceneObjectLayoutCache

func (cache *sceneObjectLayoutCache) get(key sceneObjectLayoutKey) (sceneObjectLayoutEntry, bool) {
	cache.mu.Lock()
	retained, found := cache.entries[key]
	var entry sceneObjectLayoutEntry
	if found {
		cache.order.MoveToFront(retained)
		entry = retained.Value.(retainedObjectLayout).entry
	}
	cache.mu.Unlock()
	if found {
		entry.geometry.lines = append([]Line(nil), entry.geometry.lines...)
	}
	return entry, found
}

func (cache *sceneObjectLayoutCache) put(key sceneObjectLayoutKey, entry sceneObjectLayoutEntry) {
	const overhead = 192 // Fingerprint, flow, LRU links, map and entry bookkeeping.
	cost := entry.geometry.cost + overhead
	if cost > maxSceneLayoutEntryBytes {
		return
	}
	entry.geometry.lines = append([]Line(nil), entry.geometry.lines...)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, exists := cache.entries[key]; exists {
		return
	}
	for len(cache.entries) >= maxSceneObjectLayoutEntries || cache.bytes+cost > maxSceneObjectLayoutBytes {
		oldest := cache.order.Back()
		old := oldest.Value.(retainedObjectLayout)
		delete(cache.entries, old.key)
		cache.bytes -= old.entry.geometry.cost + overhead
		cache.order.Remove(oldest)
	}
	if cache.entries == nil {
		cache.entries = make(map[sceneObjectLayoutKey]*list.Element)
	}
	cache.entries[key] = cache.order.PushFront(retainedObjectLayout{key: key, entry: entry})
	cache.bytes += cost
}

// This is the concrete scene's geometry preparation, not a second layout
// engine: misses invoke the same prepared layout core with its exact flow
// state. Positioned objects do not depend on the flowing-text cursor. Flow
// objects retain it in the key, so a changed paragraph invalidates downstream
// flow placements but not explicitly positioned neighbours. Connector geometry
// is resolved by the scene from the assembled shape anchors afterwards.
func sceneObjectGeometryLayout(slide Slide, cols, rows int, metrics *sceneRenderMetrics, registration uint64) ([]Line, bool) {
	var fingerprint sceneLayoutFingerprint
	flow := preparedLayoutFlow{}
	var lines []Line
	for index, element := range slide.Elements {
		if !isTrueType(element) && (element.Kind == "connector" || element.Kind == "table" || element.Kind == "chart") {
			continue // These have no legacy layout block in the shared core.
		}
		query, err := url.ParseQuery(element.Query)
		if err != nil {
			return nil, false
		}
		// Typography/defaults and fitted shape labels are already materialized
		// in the prepared element. The core only consumes its canvas and query;
		// unrelated theme paint, notes, activity archives and other objects must
		// neither invalidate nor become retained inputs of its geometry.
		single := Slide{Elements: []Element{element}, canvasWidth: slide.canvasWidth, canvasHeight: slide.canvasHeight}
		input, valid := fingerprint.key(single, cols, rows)
		if !valid {
			return nil, false
		}
		dependent := !imagePlacementFromValues(query).hasVerticalOffset()
		key := sceneObjectLayoutKey{input: input}
		if dependent {
			key.flow = flow
		}
		entry, found := sharedSceneObjectLayouts.get(key)
		if !found {
			localFlow := key.flow
			painted := layoutPreparedSlideWithFlow(single, cols, rows, &localFlow)
			geometry, valid := sceneLayoutSkeleton(painted)
			if !valid {
				return nil, false
			}
			entry = sceneObjectLayoutEntry{geometry: geometry, flow: localFlow}
			if registration != embeddedImageRegistrationRevision.Load() {
				return nil, false
			}
			// File-backed native sources have independent stamps in addition
			// to compact-source registration. Do not publish an obsolete key.
			if element.Kind == "image" {
				if current, ok := fingerprint.key(single, cols, rows); !ok || current != input {
					return nil, false
				}
			}
			sharedSceneObjectLayouts.put(key, entry)
			if metrics != nil {
				metrics.ObjectLayoutBuilds = append(metrics.ObjectLayoutBuilds, element.ID)
			}
		} else if metrics != nil {
			metrics.ObjectLayoutHits = append(metrics.ObjectLayoutHits, element.ID)
		}
		if dependent {
			flow = entry.flow
		}
		for _, line := range entry.geometry.lines {
			line.Element = index
			lines = append(lines, line)
		}
	}
	return lines, registration == embeddedImageRegistrationRevision.Load()
}

// Sample a concrete Retro image alone instead of rerunning layout for every
// unrelated text/shape/media object. Unpositioned images use the exact retained
// first-image-row anchor as row_delta: unlike a new top constraint this does not
// reduce available image height, so natural/flow sizing remains unchanged.
func sceneIsolatedImageRows(slide Slide, index int, anchor *int, cols, rows int) ([]Line, bool) {
	if index < 0 || index >= len(slide.Elements) || slide.Elements[index].Kind != "image" {
		return nil, false
	}
	element := slide.Elements[index]
	query, err := url.ParseQuery(element.Query)
	if err != nil {
		return nil, false
	}
	if !imagePlacementFromValues(query).hasVerticalOffset() {
		if anchor == nil {
			return nil, false
		}
		query.Set("row_delta", strconv.Itoa(*anchor))
	}
	isolated := element
	isolated.Query = query.Encode()
	slide.Elements = []Element{isolated}
	own := layoutPreparedSlide(slide, cols, rows)
	for i := range own {
		own[i].Element, own[i].Query = index, element.Query
	}
	return own, true
}
