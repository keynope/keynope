package app

import (
	"net/url"
	"sync"
)

// Cache only semantic text projection, after master/font inheritance resolves.
// Paint, world geometry, links and editor metadata are recomposed on every
// scene build. The frontend owns shaping/wrapping for the current box.
type sceneTextObjectKey struct {
	element      Element
	style, theme string
	wordFonts    string
	typefaces    string
	scale        float64
}

// Strip only known non-text dependencies. Unknown query extensions remain in
// the key conservatively, so a future text feature cannot accidentally reuse
// stale data. Resolved typography and embedded rich runs stay in the key.
func sceneTextDependencyKey(deck Deck, element Element, style string, values url.Values) sceneTextObjectKey {
	// Borrow value slices only while encoding the immutable key. Neither this
	// map nor its slices escape; copying discarded geometry/paint fields used to
	// allocate on every cache hit.
	q := make(url.Values)
	wordFonts := url.Values{}
	if styles := decodeModernRuns(values.Get("modern-runs")); styles != nil && styles.Source == modernRunFingerprint(element.Text, values.Get("ttf-weight")) {
		for _, run := range styles.Runs {
			if run.FontRole != "" {
				wordFonts.Set(run.FontRole, deck.wordFontID(run.FontRole, run.FontID))
			}
		}
	}
	for key, candidates := range values {
		switch key {
		case "top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align",
			"width", "height", "text-box", "object-offset-x", "object-offset-y", "object-rotation", "orientation",
			"fg", "header", "bg", "theme-fg", "modern-opacity", "outline",
			"gradient-start", "gradient-end", "gradient-dir", "shadow", "shadow-color", "shadow-x", "shadow-y",
			"fx-blur", "fx-blur-axes", "fx-posterize", "fx-colour", "fx-shadows", "fx-midtones", "fx-highlights", "fx-glow-radius", "fx-glow-colour", "fx-glow-threshold", "fx-glow-intensity",
			"link", "group", "object-name", "object-locked", "object-hidden", "layer", "render":
			continue
		}
		q[key] = candidates
	}
	// Positional align values stay excluded, but legacy justify is semantic
	// paragraph formatting. Share its key with the equivalent modern spelling.
	if !values.Has("text-align") && values.Get("align") == "justify" {
		q.Set("text-align", "justify")
	}
	profile := "legacy"
	if deck.usesConcreteAppearance() {
		profile = "concrete"
	} else if _, ok := deckThemes[deck.themeID()]; ok {
		profile = "legacy-themed"
	}
	return sceneTextObjectKey{
		element: Element{Kind: element.Kind, Level: element.Level, Text: element.Text, Query: q.Encode()},
		style:   style, theme: profile, wordFonts: wordFonts.Encode(), typefaces: typefaceAvailabilityKey(deck.Typefaces), scale: deck.appearanceTextScale("modern"),
	}
}

type sceneTextObjectEntry struct {
	wrapped     [8]struct{ width, height float64 }
	wrappedNext int
	natural     sceneRect // Intrinsic physical extents only; never world geometry.
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

func (c *sceneTextObjectCache) wrappedHeight(key sceneTextObjectKey, text *sceneText, width float64) (float64, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		for _, v := range entry.wrapped {
			if v.width == width && v.height > 0 {
				return v.height, true
			}
		}
	}
	height := sceneTextWrappedHeight(text, width)
	c.put(key, sceneObject{Kind: "text", Text: text}, nil)
	c.mu.Lock()
	if entry, ok = c.entries[key]; ok {
		entry.wrapped[entry.wrappedNext%len(entry.wrapped)] = struct{ width, height float64 }{width, height}
		entry.wrappedNext = (entry.wrappedNext + 1) % len(entry.wrapped)
		c.entries[key] = entry
	}
	c.mu.Unlock()
	return height, false
}

// Lazily measure only natural-width consumers. Explicit boxes never scan their
// content for unused geometry. Intrinsic dimensions are page/paint independent
// and share the bounded semantic key, not a second unbounded text cache.
func (c *sceneTextObjectCache) naturalSize(key sceneTextObjectKey, text *sceneText) (sceneRect, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && entry.natural.Width > 0 {
		return entry.natural, true
	}
	width, height := sceneTextNaturalSize(text)
	size := sceneRect{Width: width, Height: height}
	c.put(key, sceneObject{Kind: "text", Text: text}, nil)
	c.mu.Lock()
	if entry, ok = c.entries[key]; ok {
		entry.natural = size
		c.entries[key] = entry
	}
	c.mu.Unlock()
	return size, false
}

func cloneShapedTextObject(o sceneObject) sceneObject {
	o.TextCapabilities = append([]string(nil), o.TextCapabilities...)
	if o.Paint.Opacity != nil {
		v := *o.Paint.Opacity
		o.Paint.Opacity = &v
	}
	if o.Text != nil {
		text := *o.Text
		if text.FontRoles != nil {
			text.FontRoles = make(map[string]string, len(o.Text.FontRoles))
			for key, value := range o.Text.FontRoles {
				text.FontRoles[key] = value
			}
		}
		if text.ColorTokens != nil {
			text.ColorTokens = make(map[string]string, len(o.Text.ColorTokens))
			for key, value := range o.Text.ColorTokens {
				text.ColorTokens[key] = value
			}
		}
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
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return sceneObject{}, nil, false
	}
	// Entries are immutable after publication. Copy the snapshot outside the
	// shared lock; eviction cannot invalidate its Go-owned backing storage.
	return cloneShapedTextObject(entry.object), append([]sceneDiagnostic(nil), entry.diagnostics...), true
}
func (c *sceneTextObjectCache) put(key sceneTextObjectKey, o sceneObject, diagnostics []sceneDiagnostic) {
	if o.Kind != "text" || o.Text == nil || o.RetroText != nil || o.RetroLines != nil {
		return
	}
	cost := len(key.element.Text) + len(key.element.Query) + len(key.wordFonts) + len(key.typefaces) + 2048
	for _, face := range o.Text.TypefaceMetrics {
		cost += len(face.Data) + 512
	}
	for _, value := range []string{key.element.Kind, key.element.Path, key.element.AssetID, key.element.ID, key.element.SlotID, key.element.PlaceholderRole, key.element.MasterSlotID, key.style, key.theme} {
		cost += len(value)
	}
	for glyph, font := range o.Text.EmojiFonts {
		cost += len(glyph) + len(font) + 64
	}
	for role, font := range o.Text.FontRoles {
		cost += len(role) + len(font) + 64
	}
	for role, color := range o.Text.ColorTokens {
		cost += len(role) + len(color) + 64
	}
	for _, value := range []string{o.Text.FontID, o.Text.Family, o.Text.Role, o.Text.Align, o.Text.Vertical, o.Text.EmojiTint} {
		cost += len(value)
	}
	runCost := func(run sceneRun) int {
		return len(run.Text) + len(run.FontRole) + len(run.FontID) + len(run.Color) + len(run.ColorToken) + 128
	}
	for _, run := range o.Text.Runs {
		cost += runCost(run)
	}
	for _, paragraph := range o.Text.Paragraphs {
		cost += 64
		for _, run := range paragraph.Runs {
			cost += runCost(run)
		}
	}
	for _, diagnostic := range diagnostics {
		cost += len(diagnostic.ObjectID) + len(diagnostic.Code) + len(diagnostic.Message) + 64
	}
	const limit = 8 << 20
	if cost > limit {
		return
	}
	// Avoid copying on a known hit, then recheck after creating an owned entry:
	// concurrent misses may prepare the same key but only one is published.
	c.mu.Lock()
	_, exists := c.entries[key]
	c.mu.Unlock()
	if exists {
		return
	}
	entry := sceneTextObjectEntry{object: cloneShapedTextObject(o), diagnostics: append([]sceneDiagnostic(nil), diagnostics...), cost: cost}
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
	c.entries[key] = entry
	c.bytes += cost
}
