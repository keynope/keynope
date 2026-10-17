package app

import (
	"crypto/sha256"
	"encoding/json"
	"sync"
)

// Animation tracks are expensive semantic projections, independent of edits
// to other slide objects. Store encoded immutable results so callers cannot
// mutate a subsequent scene through retained frame/part slices.
type sceneAnimationCache struct {
	mu      sync.Mutex
	entries map[[32]byte][]byte
	bytes   int
}

var sharedSceneAnimations sceneAnimationCache

func sceneAnimationKey(slide Slide, frames []DeckAssetFrame, cols, rows, count int) ([32]byte, bool) {
	if len(slide.Elements) != 1 || slide.Elements[0].Kind != "image" || fastImageRender || prewarmingImageCache {
		return [32]byte{}, false
	}
	asset, ok := embeddedAnimatedAsset(slide.Elements[0].Path)
	if !ok {
		return [32]byte{}, false
	}
	type frameKey struct {
		Digest [32]byte
		Delay  int64
	}
	keys := func(frames []DeckAssetFrame) []frameKey {
		result := make([]frameKey, len(frames))
		for i, frame := range frames {
			result[i] = frameKey{sha256.Sum256(frame.Data), frame.DelayMS}
		}
		return result
	}
	// These fields are deliberately broader than the current image renderer.
	// Unknown authored extensions remain dependencies; derived theme fields
	// excluded by Slide's JSON encoding are explicitly included.
	input := struct {
		Slide                                  Slide
		Theme                                  *themeColors
		Strokes                                *shapeStrokeDefaults
		Cols, Rows, Count, Width, Height, Loop int
		MIME                                   string
		Frames, Source                         []frameKey
	}{slide, slide.ThemeColors, slide.ShapeStrokeDefaults, cols, rows, count,
		asset.Width, asset.Height, asset.LoopCount, asset.MIME, keys(frames), keys(asset.Frames)}
	h := sha256.New()
	if json.NewEncoder(h).Encode(input) != nil {
		return [32]byte{}, false
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key, true
}

func (c *sceneAnimationCache) get(key [32]byte) ([]exportContentFrame, bool) {
	c.mu.Lock()
	data, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	var result []exportContentFrame
	if json.Unmarshal(data, &result) != nil {
		return nil, false
	}
	return result, true
}

func (c *sceneAnimationCache) put(key [32]byte, frames []exportContentFrame) {
	data, err := json.Marshal(frames)
	const limit = 16 << 20
	if err != nil || len(data) > limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		return
	}
	// Bounded generational eviction avoids adding per-frame LRU bookkeeping.
	if c.entries == nil || c.bytes+len(data) > limit || len(c.entries) >= 128 {
		c.entries = make(map[[32]byte][]byte)
		c.bytes = 0
	}
	c.entries[key] = data
	c.bytes += len(data)
}
