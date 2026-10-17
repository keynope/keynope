package app

import (
	"container/list"
	"image"
	"sync"
)

// Retain only a bounded hot set. Loading/conversion happens outside this lock;
// publication rechecks for a concurrent winner. Callers treat pixels as read-only
// and return owned row slices. The byte budget bounds retained, not peak, memory.
type imageRuntimeCache[V any] struct {
	mu                       sync.Mutex
	entries                  map[string]*list.Element
	order                    list.List
	bytes, limit, maxEntries int
}
type imageRuntimeEntry[V any] struct {
	key   string
	value V
	cost  int
}

var asciiImageCache = imageRuntimeCache[*asciiImageAnimation]{limit: 64 << 20, maxEntries: 128}
var fastASCIIImageCache = imageRuntimeCache[[]string]{limit: 16 << 20, maxEntries: 256}
var decodedStillImageCache = imageRuntimeCache[image.Image]{limit: 32 << 20, maxEntries: 128}
var emojiGlyphCache = imageRuntimeCache[[]string]{limit: 16 << 20, maxEntries: 256}

func (c *imageRuntimeCache[V]) footprint() browserCacheFootprint {
	c.mu.Lock()
	defer c.mu.Unlock()
	return browserCacheFootprint{Entries: len(c.entries), Bytes: c.bytes}
}

func (c *imageRuntimeCache[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node := c.entries[key]; node != nil {
		c.order.MoveToFront(node)
		return node.Value.(imageRuntimeEntry[V]).value, true
	}
	var zero V
	return zero, false
}
func (c *imageRuntimeCache[V]) remove(node *list.Element) {
	entry := node.Value.(imageRuntimeEntry[V])
	delete(c.entries, entry.key)
	c.bytes -= entry.cost
	c.order.Remove(node)
}
func (c *imageRuntimeCache[V]) put(key string, value V, cost int) V {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node := c.entries[key]; node != nil {
		c.order.MoveToFront(node)
		return node.Value.(imageRuntimeEntry[V]).value
	}
	// Cost functions saturate at MaxInt for unusable bounds. Check before adding
	// bookkeeping so hostile/custom image dimensions cannot overflow accounting.
	if c.limit < 128 || cost < 0 || cost > c.limit-128 || len(key) > c.limit-128-cost || c.maxEntries <= 0 {
		return value
	}
	cost += len(key) + 128
	if cost > c.limit {
		return value
	}
	for c.bytes+cost > c.limit || len(c.entries) >= c.maxEntries {
		c.remove(c.order.Back())
	}
	if c.entries == nil {
		c.entries = make(map[string]*list.Element)
	}
	c.entries[key] = c.order.PushFront(imageRuntimeEntry[V]{key, value, cost})
	c.bytes += cost
	return value
}

// Lazy animation rows can grow after admission. Reweigh only the matching
// instance; a late old instance must not evict/reweight its newer replacement.
func (c *imageRuntimeCache[V]) reweigh(key string, matches func(V) bool, cost int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node := c.entries[key]
	if node == nil || !matches(node.Value.(imageRuntimeEntry[V]).value) {
		return
	}
	if c.limit < 128 || cost < 0 || cost > c.limit-128 || len(key) > c.limit-128-cost {
		c.remove(node)
		return
	}
	entry := node.Value.(imageRuntimeEntry[V])
	cost += len(key) + 128
	if cost <= entry.cost {
		return
	} // Delayed accounting must not undo a newer growth.
	c.bytes -= entry.cost
	entry.cost = cost
	node.Value = entry
	c.bytes += entry.cost
	c.order.MoveToFront(node)
	for c.bytes > c.limit {
		c.remove(c.order.Back())
	}
}
func (c *imageRuntimeCache[V]) clear() {
	c.mu.Lock()
	c.entries = nil
	c.order.Init()
	c.bytes = 0
	c.mu.Unlock()
}

func imageRowsCost(rows []string) int {
	maxInt := int(^uint(0) >> 1)
	if len(rows) > (maxInt-64)/16 {
		return maxInt
	}
	cost := 64 + len(rows)*16
	for _, row := range rows {
		cost = addImageRuntimeCost(cost, len(row))
	}
	return cost
}
func addImageRuntimeCost(a, b int) int {
	maxInt := int(^uint(0) >> 1)
	if a < 0 || b < 0 || b > maxInt-a {
		return maxInt
	}
	return a + b
}
func decodedImageCost(img image.Image) int {
	if img == nil {
		return 0
	}
	switch value := img.(type) {
	case *image.NRGBA:
		return len(value.Pix) + 128
	case *image.RGBA:
		return len(value.Pix) + 128
	case *image.NRGBA64:
		return len(value.Pix) + 128
	case *image.RGBA64:
		return len(value.Pix) + 128
	case *image.Gray:
		return len(value.Pix) + 128
	case *image.Gray16:
		return len(value.Pix) + 128
	case *image.Alpha:
		return len(value.Pix) + 128
	case *image.Alpha16:
		return len(value.Pix) + 128
	case *image.Paletted:
		return len(value.Pix) + len(value.Palette)*32 + 128
	case *image.YCbCr:
		return len(value.Y) + len(value.Cb) + len(value.Cr) + 256
	}
	b := img.Bounds()
	w, h := int64(b.Dx()), int64(b.Dy())
	maxInt := int64(^uint(0) >> 1)
	if w <= 0 || h <= 0 {
		return 256
	}
	if w > (maxInt-256)/8/h {
		return int(maxInt)
	}
	return int(w*h*8) + 256
}
func (animation *asciiImageAnimation) runtimeCostLocked() int {
	maxInt := int(^uint(0) >> 1)
	if len(animation.frames) > (maxInt-256)/64 {
		return maxInt
	}
	cost := addImageRuntimeCost(256+len(animation.frames)*64, addImageRuntimeCost(len(animation.source), len(animation.cachePath)))
	for _, frame := range animation.frames {
		cost = addImageRuntimeCost(cost, addImageRuntimeCost(decodedImageCost(frame.image), imageRowsCost(frame.rows)))
	}
	return cost
}
func (animation *asciiImageAnimation) runtimeCost() int {
	animation.mu.Lock()
	defer animation.mu.Unlock()
	return animation.runtimeCostLocked()
}
func (animation *asciiImageAnimation) reweigh(cost int) {
	if animation.runtimeKey != "" {
		asciiImageCache.reweigh(animation.runtimeKey, func(value *asciiImageAnimation) bool { return value == animation }, cost)
	}
}
