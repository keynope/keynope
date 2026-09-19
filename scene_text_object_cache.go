package main

import "sync"

// Cache only independent shaped-text projection, after full layout has resolved
// flow and master inheritance. Geometry and all appearance dependencies are
// explicit keys; layout and connector routing are deliberately not cached here.
type sceneTextObjectKey struct {
	element                      Element
	bounds                       sceneRect
	style, theme, fg, header, bg string
	scale                        float64
	index, count, cols, rows     int
}
type sceneTextObjectEntry struct {
	object      sceneObject
	diagnostics []sceneDiagnostic
	cost        int
}
type sceneTextObjectCache struct {
	mu      sync.Mutex
	entries map[sceneTextObjectKey]sceneTextObjectEntry
	bytes   int
}

var sharedSceneTextObjects sceneTextObjectCache

func cloneShapedTextObject(o sceneObject) sceneObject {
	o.TextCapabilities = append([]string(nil), o.TextCapabilities...)
	if o.Paint.Opacity != nil {
		v := *o.Paint.Opacity
		o.Paint.Opacity = &v
	}
	if o.Text != nil {
		text := *o.Text
		text.Runs = append([]sceneRun(nil), text.Runs...)
		text.Paragraphs = append([]sceneParagraph(nil), text.Paragraphs...)
		for i := range text.Paragraphs {
			text.Paragraphs[i].Runs = append([]sceneRun(nil), text.Paragraphs[i].Runs...)
		}
		if text.EmojiFonts != nil {
			text.EmojiFonts = make(map[string]string, len(o.Text.EmojiFonts))
			for k, v := range o.Text.EmojiFonts {
				text.EmojiFonts[k] = v
			}
		}
		o.Text = &text
	}
	return o
}
func (c *sceneTextObjectCache) get(key sceneTextObjectKey) (sceneObject, []sceneDiagnostic, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return sceneObject{}, nil, false
	}
	return cloneShapedTextObject(entry.object), append([]sceneDiagnostic(nil), entry.diagnostics...), true
}
func (c *sceneTextObjectCache) put(key sceneTextObjectKey, o sceneObject, diagnostics []sceneDiagnostic) {
	if o.Kind != "text" || o.Text == nil || o.RetroText != nil || o.RetroLines != nil {
		return
	}
	cost := len(key.element.Text) + len(key.element.Query) + 2048
	for _, value := range []string{key.element.Kind, key.element.Path, key.element.AssetID, key.element.ID, key.element.SlotID, key.element.PlaceholderRole, key.element.MasterSlotID, key.style, key.theme, key.fg, key.header, key.bg} {
		cost += len(value)
	}
	for glyph, font := range o.Text.EmojiFonts {
		cost += len(glyph) + len(font) + 64
	}
	for _, run := range o.Text.Runs {
		cost += len(run.Text) + 64
	}
	for _, paragraph := range o.Text.Paragraphs {
		cost += 64
		for _, run := range paragraph.Runs {
			cost += len(run.Text) + 64
		}
	}
	for _, diagnostic := range diagnostics {
		cost += len(diagnostic.ObjectID) + len(diagnostic.Code) + len(diagnostic.Message) + 64
	}
	const limit = 8 << 20
	if cost > limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		return
	}
	// Bounded generational eviction avoids an LRU allocation on every hit.
	if c.entries == nil || c.bytes+cost > limit || len(c.entries) >= 2048 {
		c.entries = make(map[sceneTextObjectKey]sceneTextObjectEntry)
		c.bytes = 0
	}
	c.entries[key] = sceneTextObjectEntry{cloneShapedTextObject(o), append([]sceneDiagnostic(nil), diagnostics...), cost}
	c.bytes += cost
}
