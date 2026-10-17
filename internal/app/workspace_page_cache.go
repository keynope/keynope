package app

import (
	"encoding/json"
	"reflect"
)

// Owned by one editor's serial workspace generator. Returned pages are borrowed
// read-only until JSON encoding; do not use them as editable document state.
type workspacePageCache struct {
	previous               Deck
	cols, rows             int
	pages                  map[int]workspacePageEntry
	hits, misses           int
	resolutions            int
	masterPreviews         int
	masterHits             int
	changed                []int
	consumers              workspaceConsumerIndex
	masterCurrent          int
	masterCols, masterRows int
	masterPrevious         Deck
	masterPages            []exportPage
}
type workspacePageEntry struct {
	pages       []exportPage
	bytes       int
	objectSizes [][]int
	input       Slide
	refs        workspaceReferences
}

func (c *workspacePageCache) render(deck Deck, master bool, current, cols, rows int) ([]exportPage, error) {
	// General callers lend mutable document state. Own and normalise it before
	// retaining anything in the cache.
	return c.renderOwned(cloneDeckForScene(deck), master, current, cols, rows)
}

// renderOwned consumes an already-owned render snapshot. The WASM workspace
// bridge creates that snapshot while holding the editor read lock; cloning it
// again here used to copy every slide twice for every editor command.
func (c *workspacePageCache) renderOwned(deck Deck, master bool, current, cols, rows int) ([]exportPage, error) {
	return c.renderOwnedCompared(deck, master, current, cols, rows, nil)
}

func (c *workspacePageCache) renderOwnedCompared(deck Deck, master bool, current, cols, rows int, comparisons *workspaceSceneComparisons) ([]exportPage, error) {
	c.changed = nil
	if cols <= 0 || rows <= 0 {
		return nil, errInvalidEditorAction
	}
	if master {
		if c.masterPages != nil && c.masterCurrent == current && c.masterCols == cols && c.masterRows == rows && reflect.DeepEqual(c.masterPrevious, deck) {
			c.masterHits++
			return c.masterPages, nil
		}
		c.masterPreviews++
		pages, err := editorWorkspacePages(deck, true, current, cols, rows)
		c.masterPages, c.masterPrevious = nil, Deck{}
		if err != nil {
			return nil, err
		}
		// Include the retained input as well as output in the payload budget;
		// master snapshots may carry sizeable imported source assets.
		// Reject obviously oversized media before materialising another base64
		// copy just to discover that this projection cannot be retained.
		remaining := 64 << 20
		for _, entry := range c.pages {
			remaining -= entry.bytes
		}
		if !workspaceAssetPayloadFits(deck.Assets, remaining) {
			return pages, nil
		}
		encoded, encodeErr := json.Marshal(struct {
			Input Deck
			Pages []exportPage
		}{deck, pages})
		if encodeErr == nil && len(encoded) <= remaining {
			c.masterPrevious, c.masterPages = deck, pages
			c.masterCurrent, c.masterCols, c.masterRows = current, cols, rows
		}
		return pages, nil
	}
	// Release master-only inputs on returning to normal editing, while keeping
	// normal consumers alive across the master edit to invalidate them precisely.
	c.masterPages, c.masterPrevious = nil, Deck{}
	// Both sides are owned render snapshots with the same normalisation. The
	// cache may retain this deck until the next request.
	dataChanged, masterChanged, unknownMaster := c.consumers.changedSources(c.previous, deck)
	sharedChanged := c.cols != cols || c.rows != rows || len(c.previous.Slides) != len(deck.Slides) ||
		!reflect.DeepEqual(c.previous.PageSize, deck.PageSize) ||
		c.previous.HideActivityQR != deck.HideActivityQR ||
		!reflect.DeepEqual(c.previous.Appearance, deck.Appearance) ||
		!reflect.DeepEqual(c.previous.Typefaces, deck.Typefaces) ||
		!sceneObjectHeadersEqual(c.previous.Objects, deck.Objects) ||
		unknownMaster || !reflect.DeepEqual(c.previous.Extra, deck.Extra) ||
		!reflect.DeepEqual(c.previous.Tabs, deck.Tabs)
	entries := make([]workspacePageEntry, len(deck.Slides))
	themeChanged := !reflect.DeepEqual(c.previous.Theme, deck.Theme)
	var result []exportPage
	for i, slide := range deck.Slides {
		entry, hit := c.pages[i]
		sourceChanged := !hit || !reflect.DeepEqual(workspaceSlideInput(c.previous.Slides[i]), workspaceSlideInput(slide))
		var resolved Slide
		resolvedReady := false
		resolve := func() {
			c.resolutions++
			resolved = deck.ResolveSlide(i, false)
			resolvedReady = true
			entry.refs = workspacePageReferences(slide, resolved)
		}
		needsRender := sharedChanged || !hit || sourceChanged || dataChanged[i]
		if needsRender || masterChanged[i] {
			resolve()
			needsRender = needsRender || !reflect.DeepEqual(entry.input, workspaceSlideInput(resolved))
		}
		if !needsRender && themeChanged {
			needsRender = !sceneThemeInputsEqual(c.previous, deck, entry.input, sceneResponseKey{index: i, cols: cols, rows: rows})
		}
		if needsRender {
			if !resolvedReady {
				resolve()
			}
			previousEntry := entry
			entry.input = workspaceSlideInput(resolved)
			c.changed = append(c.changed, i)
			preview := deck.resolvedSlideRenderPreview(resolved, i, cols, rows)
			entry.pages = editorPreviewWorkspacePages(deck, preview, i, cols, rows)
			entry.bytes = 65 << 20
			entry.objectSizes = nil
			if size, costs, err := workspaceEntryEncodedSize(entry.pages, entry.input, previousEntry, comparisons); err == nil {
				entry.objectSizes = costs
				entry.bytes = size + workspaceSizeBookkeeping(costs)
			}
			c.misses++
		} else {
			c.hits++
		}
		result = append(result, entry.pages...)
		entries[i] = entry
	}
	retained, consumers := retainWorkspacePages(entries, current)
	c.previous = workspaceRetainedSnapshot(deck, retained)
	c.cols, c.rows, c.pages = cols, rows, retained
	c.consumers = consumers
	return result, nil
}

// The caller already owns this render snapshot. Keep only authored inputs and
// dependencies of retained pages; unused assets, registry payloads and evicted
// slide contents must not remain alive solely because of the workspace cache.
// Masters remain available for inheritance comparison, stripped of private
// notes/results and transient projections. Unknown adapters stay conservative.
func workspaceRetainedSnapshot(deck Deck, entries map[int]workspacePageEntry) Deck {
	snapshot := deck
	snapshot.Diagnostics = nil
	snapshot.Slides = make([]Slide, len(deck.Slides))
	snapshot.Masters.Base.Slide = workspaceSlideInput(deck.Masters.Base.Slide)
	snapshot.Masters.Layouts = append([]MasterLayout(nil), deck.Masters.Layouts...)
	for i := range snapshot.Masters.Layouts {
		snapshot.Masters.Layouts[i].Slide = workspaceSlideInput(snapshot.Masters.Layouts[i].Slide)
	}
	assets, payloads, builds := map[string]bool{}, map[workspacePayloadReference]bool{}, map[string]bool{}
	unknown := false
	for i, entry := range entries {
		snapshot.Slides[i] = workspaceSlideInput(deck.Slides[i])
		for id := range entry.refs.assets {
			assets[id] = true
		}
		for ref := range entry.refs.payloads {
			payloads[ref] = true
		}
		builds[entry.refs.build] = true
		unknown = unknown || entry.refs.unknown
	}
	if unknown {
		return snapshot
	}
	snapshot.Assets = nil
	for id := range assets {
		if asset, exists := deck.Assets[id]; exists {
			if snapshot.Assets == nil {
				snapshot.Assets = map[string]DeckAsset{}
			}
			snapshot.Assets[id] = asset
		}
	}
	if deck.Objects != nil {
		objects := &DeckObjects{Version: deck.Objects.Version, Extra: deck.Objects.Extra}
		for ref := range payloads {
			switch ref.kind {
			case "table":
				if data, exists := deck.Objects.Tables[ref.id]; exists {
					if objects.Tables == nil {
						objects.Tables = map[string]TableData{}
					}
					objects.Tables[ref.id] = data
				}
			case "chart":
				if data, exists := deck.Objects.Charts[ref.id]; exists {
					if objects.Charts == nil {
						objects.Charts = map[string]ChartData{}
					}
					objects.Charts[ref.id] = data
				}
			}
		}
		for id := range builds {
			if build, exists := deck.Objects.Builds[id]; exists {
				if objects.Builds == nil {
					objects.Builds = map[string]SlideBuild{}
				}
				objects.Builds[id] = build
			}
		}
		snapshot.Objects = objects
	}
	return snapshot
}

// Retain the active page first, then its neighbours. Workspace output keeps
// authored order, but the cache must remain useful when editing a late slide
// in a deck larger than the retained-page limit.
func retainWorkspacePages(entries []workspacePageEntry, current int) (map[int]workspacePageEntry, workspaceConsumerIndex) {
	retained, consumers := make(map[int]workspacePageEntry), newWorkspaceConsumerIndex()
	if len(entries) == 0 {
		return retained, consumers
	}
	current = max(0, min(current, len(entries)-1))
	remaining := 64 << 20
	add := func(index int) {
		if index < 0 || index >= len(entries) || len(retained) >= 128 {
			return
		}
		entry := entries[index]
		if entry.bytes <= 0 || entry.bytes > remaining {
			return
		}
		retained[index] = entry
		consumers.add(index, entry.refs)
		remaining -= entry.bytes
	}
	add(current)
	for distance := 1; distance < len(entries) && len(retained) < 128; distance++ {
		add(current - distance)
		add(current + distance)
	}
	return retained, consumers
}

// A cheap lower bound only: JSON metadata, pages and non-media payloads still
// go through the exact encoded-size check. Every serialized occurrence counts,
// even if source and derivative share the same backing bytes.
func workspaceAssetPayloadFits(assets map[string]DeckAsset, remaining int) bool {
	if remaining < 0 {
		return false
	}
	charge := func(data []byte) bool {
		if len(data) > remaining {
			return false
		}
		cost := 4 * ((len(data) + 2) / 3)
		if cost > remaining {
			return false
		}
		remaining -= cost
		return true
	}
	for _, asset := range assets {
		if !charge(asset.Data) {
			return false
		}
		for _, frame := range asset.Frames {
			if !charge(frame.Data) {
				return false
			}
		}
		if asset.Vector != nil && !charge(asset.Vector.Data) {
			return false
		}
		if asset.Source != nil {
			if !charge(asset.Source.Data) {
				return false
			}
			for _, frame := range asset.Source.Frames {
				if !charge(frame.Data) {
					return false
				}
			}
		}
	}
	return true
}
