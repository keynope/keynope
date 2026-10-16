package main

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImageRuntimeCacheBudgetsLRUAndGrowth(t *testing.T) {
	cache := imageRuntimeCache[*int]{limit: 1000, maxEntries: 3}
	a, b, c, d := 1, 2, 3, 4
	cache.put("a", &a, 20)
	cache.put("b", &b, 20)
	cache.get("a")
	cache.put("c", &c, 20)
	cache.put("d", &d, 20)
	if _, hit := cache.get("b"); hit {
		t.Fatal("cold entry was retained instead of hot entry")
	}
	if winner := cache.put("a", &b, 10); winner != &a {
		t.Fatal("concurrent publication replaced canonical value")
	}
	count, bytes := len(cache.entries), cache.bytes
	cache.put("oversized", &a, 1001)
	cache.put("overflow", &a, int(^uint(0)>>1))
	if len(cache.entries) != count || cache.bytes != bytes {
		t.Fatal("oversized entry evicted working data")
	}
	cache.reweigh("a", func(v *int) bool { return v == &b }, 9999)
	if _, hit := cache.get("a"); !hit {
		t.Fatal("old instance changed another entry")
	}
	cache.reweigh("a", func(v *int) bool { return v == &a }, 500)
	cost := cache.entries["a"].Value.(imageRuntimeEntry[*int]).cost
	cache.reweigh("a", func(v *int) bool { return v == &a }, 100)
	if cache.entries["a"].Value.(imageRuntimeEntry[*int]).cost != cost {
		t.Fatal("late growth accounting reduced retained cost")
	}
	cache.reweigh("a", func(v *int) bool { return v == &a }, 1001)
	if _, hit := cache.get("a"); hit {
		t.Fatal("oversized lazy entry remained retained")
	}
	cache.clear()
	if len(cache.entries) != 0 || cache.order.Len() != 0 || cache.bytes != 0 || cache.limit != 1000 || cache.maxEntries != 3 {
		t.Fatal("clear corrupted cache configuration")
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprint(i % 9)
				cache.put(key, &a, 20)
				cache.get(key)
			}
		}()
	}
	workers.Wait()
	if cache.bytes > cache.limit || len(cache.entries) > cache.maxEntries || cache.order.Len() != len(cache.entries) {
		t.Fatal("concurrent cache exceeded limits")
	}
	var disabled imageRuntimeCache[int]
	disabled.put("none", 1, 0)
	if len(disabled.entries) != 0 {
		t.Fatal("disabled cache retained data")
	}
}

func TestImageRuntimeRowsOwnStorageAndTrackLazyFrames(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	exportImageAnimationMu.Lock()
	previous := exportImageAnimationPosition
	position := time.Duration(0)
	exportImageAnimationPosition = &position
	t.Cleanup(func() { exportImageAnimationPosition = previous; exportImageAnimationMu.Unlock() })
	slide, asset := animationCacheFixture(t)
	path := slide.Elements[0].Path
	const query = "image-fit=contain&glyph=blocks&stretch=1"
	want := append([]string(nil), renderASCIIImage(path, query, 18, 9)...)
	got := renderASCIIImage(path, query, 18, 9)
	if len(got) == 0 {
		t.Fatal("empty animation fixture")
	}
	got[0] = "corrupt"
	if !reflect.DeepEqual(renderASCIIImage(path, query, 18, 9), want) {
		t.Fatal("caller modified cached animation rows")
	}
	fast := renderFastASCIIImage(path, query, 18, 9)
	copy := append([]string(nil), fast...)
	fast[0] = "corrupt"
	if !reflect.DeepEqual(renderFastASCIIImage(path, query, 18, 9), copy) {
		t.Fatal("caller modified cached fast rows")
	}
	key := fmt.Sprintf("%s?%s@%dx%d", embeddedImageCacheIdentity(path), imageRenderQuery(query), 18, 9)
	animation, hit := asciiImageCache.get(key)
	if !hit {
		t.Fatal("animation was not retained")
	}
	before := asciiImageCache.footprint().Bytes
	var workers sync.WaitGroup
	for frame := range asset.Frames {
		workers.Add(1)
		go func(frame int) {
			defer workers.Done()
			for i := 0; i < 5; i++ {
				if len(animation.frameRows(frame)) == 0 {
					t.Error("missing lazy frame")
				}
			}
		}(frame)
	}
	workers.Wait()
	animation.warmCache()
	if !animation.allRowsRendered() || asciiImageCache.footprint().Bytes < before {
		t.Fatal("lazy rows were not accounted")
	}
	asciiImageCache.mu.Lock()
	entry := asciiImageCache.entries[key].Value.(imageRuntimeEntry[*asciiImageAnimation])
	asciiImageCache.mu.Unlock()
	if entry.cost < animation.runtimeCost()+len(key) {
		t.Fatal("concurrent reweight undercounted animation")
	}
	if decodedImageCost(image.NewNRGBA(image.Rect(0, 0, 8, 8))) != 8*8*4+128 {
		t.Fatal("pixel allocation cost mismatch")
	}
	if addImageRuntimeCost(int(^uint(0)>>1), 1) != int(^uint(0)>>1) {
		t.Fatal("accounting overflow")
	}
}

func TestEmojiRuntimeCacheClampsKeyAndOwnsRows(t *testing.T) {
	key := emojiAssetKey("😍")
	want := renderEmojiGlyph(key, 40)
	if len(want) == 0 {
		t.Fatal("missing emoji")
	}
	before := emojiGlyphCache.footprint().Entries
	for _, height := range []int{41, 100, 1000000} {
		rows := renderEmojiGlyph(key, height)
		if !reflect.DeepEqual(rows, want) {
			t.Fatal("clamped emoji changed pixels")
		}
		rows[0] = "corrupt"
	}
	if emojiGlyphCache.footprint().Entries != before || !reflect.DeepEqual(renderEmojiGlyph(key, 40), want) {
		t.Fatal("unclamped key growth or borrowed rows")
	}
	local := imageRuntimeCache[[]string]{limit: 512, maxEntries: 2}
	rows := []string{strings.Repeat("x", 400)}
	local.put("too-large", rows, imageRowsCost(rows))
	if local.footprint().Entries != 0 {
		t.Fatal("large rows exceeded budget")
	}
}
