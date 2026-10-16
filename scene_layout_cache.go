package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

const maxSceneLayoutEntries = 128
const maxSceneLayoutBytes = 8 << 20
const maxSceneLayoutEntryBytes = 1 << 20

type sceneLayoutEntry struct {
	lines []Line // Geometry skeletons only: no authored text, ANSI pixels or queries.
	cost  int
}

type sceneLayoutCache struct {
	mu      sync.Mutex
	entries map[[32]byte]sceneLayoutEntry
	bytes   int
}

var sharedSceneLayouts sceneLayoutCache

func sceneLayoutImageIdentity(element Element) (string, bool) {
	if asset, animated := embeddedAnimatedAsset(element.Path); animated && len(asset.Frames) > 1 {
		q, err := url.ParseQuery(element.Query)
		if err != nil || imageFit(q.Get("image-fit")) == "" {
			return "", false // Natural alpha-trimmed frame bounds may vary over time.
		}
	}
	if strings.HasPrefix(element.Path, "keynope-asset:") {
		return embeddedImageCacheIdentity(element.Path), true
	}
	// Native still images use the app's materialised embedded cache. Match
	// that exact path, not arbitrary local references. Its stamp follows the
	// existing decoded-image cache; registered compact-source identity detects
	// replacing an asset under the same ID as well.
	expected := filepath.Join(imageCacheDirectory(), "embedded", element.AssetID+".png")
	if !validObjectID(element.AssetID) || filepath.Clean(element.Path) != filepath.Clean(expected) {
		return "", false
	}
	return nativeEmbeddedImageCacheIdentity(element.Path)
}

func layoutHashString(h *bufio.Writer, value string) {
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
	h.Write(size[:])
	h.WriteString(value)
}

// Hash exact resolved geometry inputs. Unknown extensions remain conservative.
// Only known, non-image paint/name keys are removed; effects, glyph conversion,
// font/run data, shape labels, anchors and structural order remain dependencies.
func sceneLayoutKey(slide Slide, cols, rows int) ([32]byte, bool) {
	var fingerprint sceneLayoutFingerprint
	return fingerprint.key(slide, cols, rows)
}

// One request-local writer is reused across object keys. Allocating a 4 KiB
// hashing buffer per unchanged object would defeat incremental layout's gain.
type sceneLayoutFingerprint struct {
	digest hash.Hash
	writer *bufio.Writer
}

func (fingerprint *sceneLayoutFingerprint) key(slide Slide, cols, rows int) ([32]byte, bool) {
	var key [32]byte
	if cols <= 0 || rows <= 0 || len(slide.Elements) > 2048 || slide.Extra != nil {
		return key, false
	}
	if fingerprint.digest == nil {
		fingerprint.digest = sha256.New()
		fingerprint.writer = bufio.NewWriterSize(fingerprint.digest, 4096)
	} else {
		fingerprint.digest.Reset()
		fingerprint.writer.Reset(fingerprint.digest)
	}
	// Buffer the exact same length-delimited fingerprint stream. Writing through
	// hash.Hash directly made each tiny numeric/string field escape separately.
	// The fixed request-local buffer never retains authored strings or pointers.
	h := fingerprint.writer
	var numbers [16]byte
	binary.LittleEndian.PutUint64(numbers[:8], uint64(cols))
	binary.LittleEndian.PutUint64(numbers[8:], uint64(rows))
	h.Write(numbers[:])

	canvas := slide.physicalCanvas()
	binary.LittleEndian.PutUint64(numbers[:8], math.Float64bits(canvas.Width))
	binary.LittleEndian.PutUint64(numbers[8:], math.Float64bits(canvas.Height))
	h.Write(numbers[:])
	binary.LittleEndian.PutUint64(numbers[:8], uint64(len(slide.Elements)))
	binary.LittleEndian.PutUint64(numbers[8:], uint64(slide.TTFSize))
	h.Write(numbers[:])
	binary.LittleEndian.PutUint64(numbers[:8], math.Float64bits(slide.TTFWidth))
	h.Write(numbers[:8])
	layoutHashString(h, slide.DefaultStyle)
	flags := []byte{0, 0, 0, 0}
	if slide.ThemeColors != nil {
		flags[0] = 1
	}
	if slide.ShapeStrokeDefaults != nil {
		flags[1] = 1
	}
	if fastImageRender {
		flags[2] = 1
	}
	if prewarmingImageCache {
		flags[3] = 1
	}
	h.Write(flags)
	if slide.ThemeColors != nil {
		layoutHashString(h, slide.ThemeColors.Background)
		layoutHashString(h, slide.ThemeColors.Text)
		layoutHashString(h, slide.ThemeColors.Heading)
	} else {
		layoutHashString(h, "no-theme-colours")
	}
	// Stroke defaults can change the visible shape boundary/label fit.
	if d := slide.ShapeStrokeDefaults; d != nil {
		layoutHashString(h, d.Color)
		binary.LittleEndian.PutUint64(numbers[:8], math.Float64bits(d.Width))
		h.Write(numbers[:8])
	} else {
		layoutHashString(h, "no-stroke-defaults")
	}
	inputBytes := 0
	for _, e := range slide.Elements {
		if !standardElementKind(e.Kind) || e.Extra != nil {
			return key, false
		}
		inputBytes += len(e.Text) + len(e.Query) + len(e.Path) + len(e.ID) + len(e.AssetID)
		if inputBytes > maxSceneLayoutEntryBytes {
			return key, false
		}
		for _, value := range []string{e.Kind, e.Text, e.ID, e.AssetID, e.Path} {
			layoutHashString(h, value)
		}
		binary.LittleEndian.PutUint64(numbers[:8], uint64(e.Level))
		numbers[8], numbers[9] = 0, 0
		if e.Inherited {
			numbers[8] = 1
		}
		if e.Placeholder {
			numbers[9] = 1
		}
		h.Write(numbers[:10])
		if e.Kind == "image" {
			identity, valid := sceneLayoutImageIdentity(e)
			if !valid {
				return key, false
			}
			layoutHashString(h, identity)
			layoutHashString(h, e.Query)
			continue
		}
		// Do not parse and allocate maps for the same query repeatedly. Literal
		// key recognition is deliberately conservative for encoded/future keys.
		parts := strings.Split(e.Query, "&")
		kept := parts[:0]
		for _, part := range parts {
			name, value, _ := strings.Cut(part, "=")
			if strings.Contains(name, "%") || name == "font" && value != "" {
				return key, false // Encoded keys/retired font sources stay cold.
			}
			switch name {
			case "fg", "header", "bg", "theme-fg", "modern-opacity", "object-name":
				continue
			}
			kept = append(kept, part)
		}
		binary.LittleEndian.PutUint64(numbers[:8], uint64(len(kept)))
		h.Write(numbers[:8])
		for _, part := range kept {
			layoutHashString(h, part)
		}
	}
	if err := h.Flush(); err != nil {
		return key, false
	}
	fingerprint.digest.Sum(key[:0])
	return key, true
}

func (c *sceneLayoutCache) get(key [32]byte) ([]Line, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	return append([]Line(nil), entry.lines...), true
}

func sceneLayoutSkeleton(lines []Line) (sceneLayoutEntry, bool) {
	// Bounds need two opposite rows per object, and routing needs the first
	// shape row's original anchor. Never retain painted rows or private text.
	type bounds struct{ left, right, top, bottom int }
	boxes := map[int]bounds{}
	anchors := map[int]Line{}
	var order []int
	for _, line := range lines {
		if line.Role == "outline" || line.Role == "shape-label" {
			continue
		}
		width := max(1, displayWidth(stripANSI(line.Text)))
		b, found := boxes[line.Element]
		if !found {
			b = bounds{line.Col, line.Col + width - 1, line.Row, line.Row}
			order = append(order, line.Element)
		} else {
			b.left, b.right = min(b.left, line.Col), max(b.right, line.Col+width-1)
			b.top, b.bottom = min(b.top, line.Row), max(b.bottom, line.Row)
		}
		boxes[line.Element] = b
		if line.Role == "shape" || line.Role == "image" {
			if _, exists := anchors[line.Element]; !exists {
				anchors[line.Element] = Line{Role: line.Role, Element: line.Element, Row: line.Row, Col: line.Col, Text: " "}
			}
		}
	}
	entry := sceneLayoutEntry{cost: 128}
	for _, index := range order {
		b := boxes[index]
		width := b.right - b.left + 1
		if width < 1 || width > 32768 || entry.cost+width*2+384 > maxSceneLayoutEntryBytes {
			return sceneLayoutEntry{}, false
		}
		if anchor, ok := anchors[index]; ok {
			entry.lines = append(entry.lines, anchor)
			entry.cost += 128
		}
		text := strings.Repeat(" ", width)
		entry.lines = append(entry.lines, Line{Role: "geometry", Element: index, Row: b.top, Col: b.left, Text: text}, Line{Role: "geometry", Element: index, Row: b.bottom, Col: b.left, Text: text})
		entry.cost += width*2 + 256
	}
	entry.cost += max(0, cap(entry.lines)-len(entry.lines)) * 128
	if entry.cost > maxSceneLayoutEntryBytes {
		return sceneLayoutEntry{}, false
	}
	return entry, true
}

func (c *sceneLayoutCache) put(key [32]byte, lines []Line) {
	entry, valid := sceneLayoutSkeleton(lines)
	if !valid {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.bytes -= old.cost
	}
	if len(c.entries) >= maxSceneLayoutEntries || c.bytes+entry.cost > maxSceneLayoutBytes {
		c.entries, c.bytes = nil, 0 // Bounded generational eviction, no timers.
	}
	if c.entries == nil {
		c.entries = map[[32]byte]sceneLayoutEntry{}
	}
	c.entries[key] = entry
	c.bytes += entry.cost
}

func sceneGeometryLayout(slide Slide, cols, rows int, metrics *sceneRenderMetrics, concrete bool) ([]Line, bool) {
	// buildStyledSceneProjection already resolves text defaults and fits labels
	// on its owned slide. A cache miss must not repeat that preparation.
	if metrics != nil && metrics.DisableLayoutCache {
		metrics.LayoutBuilds++
		return layoutPreparedSlide(slide, cols, rows), false
	}
	registration := embeddedImageRegistrationRevision.Load()
	key, eligible := sceneLayoutKey(slide, cols, rows)
	key = sceneLayoutProfileKey(key, concrete)
	if eligible {
		if lines, hit := sharedSceneLayouts.get(key); hit && registration == embeddedImageRegistrationRevision.Load() {
			if metrics != nil {
				metrics.LayoutCacheHits++
			}
			return lines, true
		}
	}
	if concrete && eligible {
		if lines, valid := sceneObjectGeometryLayout(slide, cols, rows, metrics, registration); valid {
			if current, ok := sceneLayoutKey(slide, cols, rows); ok && sceneLayoutProfileKey(current, concrete) == key && registration == embeddedImageRegistrationRevision.Load() {
				sharedSceneLayouts.put(key, lines)
				return lines, true
			}
		}
	}
	if metrics != nil {
		metrics.LayoutBuilds++
	}
	lines := layoutPreparedSlide(slide, cols, rows)
	if eligible && registration == embeddedImageRegistrationRevision.Load() {
		// Native materialised sources have file stamps too. Do not publish an
		// obsolete stamp if a cache file was replaced during this layout pass.
		if current, valid := sceneLayoutKey(slide, cols, rows); valid && sceneLayoutProfileKey(current, concrete) == key {
			sharedSceneLayouts.put(key, lines)
		}
	}
	return lines, false
}

func sceneLayoutProfileKey(key [32]byte, concrete bool) [32]byte {
	if !concrete {
		return key
	}
	// Concrete scenes derive connector bounds from routed vertices, not
	// sampled connector rows. Never share their geometry-only baseline with
	// a legacy painted-layout caller.
	profile := [33]byte{1}
	copy(profile[1:], key[:])
	return sha256.Sum256(profile[:])
}
