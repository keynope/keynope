package main

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sync"
)

// Table cell projection is local to its width/height. Position, rotation, object
// paint and editing annotations are recomposed by the scene owner. Keep all
// authored table inputs in the content key. Theme changes are compared through
// the same cell/run/seam consumers as scene invalidation, not the entire theme.
type sceneTableCacheEntry struct {
	table     *sceneTable
	theme     *DeckTheme // Owned public render inputs only, never theme extensions.
	cost      int        // Input/output accounting; dependency snapshot is added by store.
	themeCost int
}
type sceneTableCacheItem struct {
	key   [32]byte
	entry sceneTableCacheEntry
}
type sceneTableCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*list.Element
	order   list.List
	bytes   int
}

var sharedSceneTables sceneTableCache

func cloneSceneTable(source *sceneTable) *sceneTable {
	if source == nil {
		return nil
	}
	result := *source
	result.Rows = append([]float64(nil), source.Rows...)
	result.Columns = append([]float64(nil), source.Columns...)
	result.Cells = append([]sceneTableCell(nil), source.Cells...)
	// An empty, non-nil border array selects semantic seams in the renderer.
	if source.Borders != nil {
		result.Borders = append([]sceneTableBorder{}, source.Borders...)
	}
	for i, cell := range source.Cells {
		owned := cloneShapedTextObject(sceneObject{Text: cell.Text, Paint: cell.Paint})
		result.Cells[i].Text, result.Cells[i].Paint = owned.Text, owned.Paint
	}
	return &result
}

func (cache *sceneTableCache) project(deck Deck, table TableData, bounds sceneRect, color string) (*sceneTable, error, bool) {
	// Tables use these appearance values, not private/future profile metadata.
	// Keep the concrete/legacy distinction even when a theme is added/removed.
	input, keyErr := json.Marshal(struct {
		Table         TableData
		Concrete      bool
		ThemeID       string
		TextScale     float64
		Width, Height float64
		Color         string
		Typefaces     string
	}{table, deck.usesConcreteAppearance(), deck.themeID(), deck.appearanceTextScale("modern"), bounds.Width, bounds.Height, color, typefaceAvailabilityKey(deck.Typefaces)})
	key := sha256.Sum256(input)
	theme := sceneTableThemeInputs(deck.Theme)
	if keyErr == nil {
		cache.mu.Lock()
		var entry sceneTableCacheEntry
		if node := cache.entries[key]; node != nil {
			entry = node.Value.(sceneTableCacheItem).entry
			cache.order.MoveToFront(node)
		}
		cache.mu.Unlock()
		if entry.table != nil {
			previous := Deck{Theme: entry.theme, Appearance: deck.Appearance}
			sameTheme := reflect.DeepEqual(entry.theme, theme)
			if sameTheme || tableThemeInputsEqual(previous, deck, table, color, color) {
				// Publish a new immutable dependency snapshot so subsequent edits
				// need not repeat the comparison. Concurrent readers own the old one.
				if !sameTheme {
					entry.theme = ownSceneTableTheme(theme)
					cache.store(key, entry)
				}
				return cloneSceneTable(entry.table), nil, true
			}
		}
	}
	projected, err := deck.projectTable(table, bounds, color)
	if err != nil || keyErr != nil {
		return projected, err, false
	}
	encoded, encodeErr := json.Marshal(projected)
	if encodeErr != nil {
		return projected, nil, false
	}
	// Encoded authored input/output plus fixed bookkeeping estimate, bounded
	// by both bytes and entry count. Do not retain document or runtime state.
	cost := len(input) + len(encoded) + 256
	for _, cell := range projected.Cells {
		if cell.Text != nil {
			for _, face := range cell.Text.TypefaceMetrics {
				cost += len(face.Data) + 512
			}
		}
	}
	const limit = 8 << 20
	if cost > limit {
		return projected, nil, false
	}
	entry := sceneTableCacheEntry{table: cloneSceneTable(projected), theme: ownSceneTableTheme(theme), cost: cost}
	cache.store(key, entry)
	return projected, nil, false
}

// Only public colour/font values are consumed by table projection. Do not
// retain an authored theme name, private extensions, document or payload here.
func sceneTableThemeInputs(source *DeckTheme) *DeckTheme {
	if source == nil {
		return nil
	}
	theme := &DeckTheme{Version: source.Version, Colors: source.Colors, Fonts: source.Fonts}
	theme.Colors.Extra, theme.Fonts.Extra = nil, nil
	theme.Fonts.Body.Extra, theme.Fonts.Heading.Extra, theme.Fonts.Mono.Extra = nil, nil, nil
	return theme
}

func ownSceneTableTheme(theme *DeckTheme) *DeckTheme {
	if theme == nil {
		return nil
	}
	owned := *theme
	owned.Colors.Accent = append([]string(nil), theme.Colors.Accent...)
	return &owned
}

func (cache *sceneTableCache) store(key [32]byte, entry sceneTableCacheEntry) {
	encoded, err := json.Marshal(entry.theme)
	if err != nil {
		return
	}
	entry.cost += len(encoded) + 512 - entry.themeCost
	entry.themeCost = len(encoded) + 512
	if entry.cost > 8<<20 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if node := cache.entries[key]; node != nil {
		cache.bytes -= node.Value.(sceneTableCacheItem).entry.cost
		cache.order.Remove(node)
		delete(cache.entries, key)
	}
	if cache.entries == nil {
		cache.entries = make(map[[32]byte]*list.Element)
	}
	for cache.bytes+entry.cost > 8<<20 || len(cache.entries) >= 64 {
		node := cache.order.Back()
		if node == nil {
			return
		}
		item := node.Value.(sceneTableCacheItem)
		cache.bytes -= item.entry.cost
		delete(cache.entries, item.key)
		cache.order.Remove(node)
	}
	cache.entries[key] = cache.order.PushFront(sceneTableCacheItem{key, entry})
	cache.bytes += entry.cost
}
