package main

import (
	"encoding/json"
	"reflect"
)

// Owned by one editor's serial workspace generator. Returned pages are borrowed
// read-only until JSON encoding; do not use them as editable document state.
type workspacePageCache struct {
	previous     Deck
	cols, rows   int
	pages        map[int]workspacePageEntry
	hits, misses int
	changed      []int
}
type workspacePageEntry struct {
	pages []exportPage
	bytes int
}

func (c *workspacePageCache) render(deck Deck, master bool, current, cols, rows int) ([]exportPage, error) {
	// General callers lend mutable document state. Own and normalise it before
	// retaining anything in the cache.
	return c.renderOwned(cloneDeckForRender(deck), master, current, cols, rows)
}

// renderOwned consumes an already-owned render snapshot. The WASM workspace
// bridge creates that snapshot while holding the editor read lock; cloning it
// again here used to copy every slide twice for every editor command.
func (c *workspacePageCache) renderOwned(deck Deck, master bool, current, cols, rows int) ([]exportPage, error) {
	c.changed = nil
	if master {
		return editorWorkspacePages(deck, true, current, cols, rows)
	}
	if cols <= 0 || rows <= 0 {
		return nil, errInvalidEditorAction
	}
	// Both sides are owned render snapshots with the same normalisation. The
	// cache may retain this deck until the next request.
	sharedChanged := c.cols != cols || c.rows != rows || len(c.previous.Slides) != len(deck.Slides) ||
		c.previous.HideActivityQR != deck.HideActivityQR ||
		!reflect.DeepEqual(c.previous.Appearance, deck.Appearance) ||
		!workspaceMastersEqual(c.previous.Masters, deck.Masters) ||
		!reflect.DeepEqual(c.previous.Assets, deck.Assets) ||
		!reflect.DeepEqual(c.previous.Tabs, deck.Tabs)
	retained := make(map[int]workspacePageEntry)
	var result []exportPage
	bytes := 0
	for i, slide := range deck.Slides {
		entry, hit := c.pages[i]
		if sharedChanged || !hit || !reflect.DeepEqual(c.previous.Slides[i], slide) {
			c.changed = append(c.changed, i)
			entry.pages = editorSlideWorkspacePages(deck, i, cols, rows)
			entry.bytes = 65 << 20
			if encoded, err := json.Marshal(entry.pages); err == nil {
				entry.bytes = len(encoded)
			}
			c.misses++
		} else {
			c.hits++
		}
		result = append(result, entry.pages...)
		// Bound retained projections independently of the returned workspace.
		// Encoding size is a conservative payload budget, not a heap-size claim.
		if len(retained) < 128 && bytes+entry.bytes <= 64<<20 {
			retained[i] = entry
			bytes += entry.bytes
		}
	}
	c.previous = deck
	c.cols, c.rows, c.pages = cols, rows, retained
	return result, nil
}

// Names and layout-list ordering only affect the master editor sidebar.
// Normal slides resolve layouts by stable ID, so treating those changes as a
// new visual master needlessly rebuilds every page in a large deck.
func workspaceMastersEqual(a, b MasterDeck) bool {
	if a.Version != b.Version || !reflect.DeepEqual(a.Extra, b.Extra) ||
		!reflect.DeepEqual(a.Base.Extra, b.Base.Extra) || !reflect.DeepEqual(a.Base.Slide, b.Base.Slide) ||
		len(a.Layouts) != len(b.Layouts) {
		return false
	}
	aLayouts := make(map[string]MasterLayout, len(a.Layouts))
	for _, layout := range a.Layouts {
		aLayouts[layout.ID] = layout
	}
	for _, layout := range b.Layouts {
		previous, ok := aLayouts[layout.ID]
		if !ok || !reflect.DeepEqual(previous.Extra, layout.Extra) || !reflect.DeepEqual(previous.Slide, layout.Slide) {
			return false
		}
	}
	return true
}
