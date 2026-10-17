package app

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSceneLayoutPaintReuseMatchesColdMixedProjection(t *testing.T) {
	d := powerupDenseEditingDeck(t)
	for _, change := range []struct{ key, value string }{{"fg", "#ff0055"}, {"header", "#55ffff"}, {"bg", "#123456"}, {"modern-opacity", "0.4"}, {"object-name", "Named text"}} {
		before := cloneDeck(d)
		// Prime this exact non-paint context, regardless of earlier test order.
		if _, err := buildSlideScene(d, 0, 245, 56); err != nil {
			t.Fatal(err)
		}
		d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, change.key, change.value)
		metrics := &sceneRenderMetrics{}
		warm, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, metrics)
		if err != nil {
			t.Fatal(err)
		}
		coldMetrics := &sceneRenderMetrics{DisableLayoutCache: true}
		cold, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, coldMetrics)
		if err != nil || !reflect.DeepEqual(warm, cold) {
			t.Fatal("paint-only reuse changed mixed scene", change.key, err)
		}
		if metrics.LayoutCacheHits != 1 || metrics.LayoutBuilds != 0 || coldMetrics.LayoutCacheHits != 0 || coldMetrics.LayoutBuilds != 1 {
			t.Fatalf("paint-only edit still rebuilt layout: %s warm=%+v cold=%+v", change.key, metrics, coldMetrics)
		}
		d = before
	}
}

func TestSceneLayoutCacheRetroArtworkAlwaysFresh(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		d := sceneFixture(t)
		d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		for _, change := range []string{"fg", "modern-opacity", "object-name"} {
			_, err := buildMixedSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			d.Slides[0].Elements[0].Query = setQueryValue(d.Slides[0].Elements[0].Query, change, map[string]string{"fg": "#ff0055", "modern-opacity": "0.4", "object-name": "Fresh artwork"}[change])
			warm, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, &sceneRenderMetrics{})
			if err != nil {
				t.Fatal(err)
			}
			cold, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, &sceneRenderMetrics{DisableLayoutCache: true})
			if err != nil || !reflect.DeepEqual(warm, cold) {
				t.Fatal("cached geometry leaked stale sampled artwork", style, change, err)
			}
		}
	}
}

func TestSceneLayoutProofCorpusColdParity(t *testing.T) {
	for name, deck := range powerupProofCorpus(t) {
		t.Run(name, func(t *testing.T) {
			for index := range deck.Slides {
				slide := deck.ResolveSlide(index, false)
				if _, err := buildStyledSceneTargetsMeasured(deck, slide, index, 245, 56, true, nil, &sceneRenderMetrics{}); err != nil {
					t.Fatal(err)
				}
				warm, err := buildStyledSceneTargetsMeasured(deck, slide, index, 245, 56, true, nil, &sceneRenderMetrics{})
				if err != nil {
					t.Fatal(err)
				}
				cold, err := buildStyledSceneTargetsMeasured(deck, slide, index, 245, 56, true, nil, &sceneRenderMetrics{DisableLayoutCache: true})
				if err != nil || !reflect.DeepEqual(warm, cold) {
					t.Fatalf("page %d geometry reuse differs from cold projection: %v", index, err)
				}
			}
		})
	}
}

func TestSceneLayoutKeyInvalidatesGeometryAndUnknownInputs(t *testing.T) {
	slide := Slide{Elements: []Element{{ID: "body", Kind: "text", Text: "Layout", Query: "render=truetype&modern-font=mono&left=5&top=5&width=100&height=10"}}}
	key, valid := sceneLayoutKey(slide, 245, 56)
	if !valid {
		t.Fatal("known geometry not cacheable")
	}
	for _, change := range []struct{ key, value string }{{"left", "6"}, {"width", "90"}, {"modern-font", "sans"}, {"modern-size", "80"}, {"shadow", "solid"}, {"outline", "dark"}, {"link", "https://keynope.sh"}, {"object-hidden", "1"}, {"future-layout", "value"}} {
		next := cloneSlide(slide)
		next.Elements[0].Query = setQueryValue(next.Elements[0].Query, change.key, change.value)
		got, _ := sceneLayoutKey(next, 245, 56)
		if got == key {
			t.Fatal("geometry/future field did not invalidate", change.key)
		}
	}
	for _, change := range []string{"content", "id", "viewport", "inherited", "stroke-default"} {
		next, width := cloneSlide(slide), 245
		switch change {
		case "content":
			next.Elements[0].Text += " longer"
		case "id":
			next.Elements[0].ID = "different"
		case "viewport":
			width++
		case "inherited":
			next.Elements[0].Inherited = true
		case "stroke-default":
			next.ShapeStrokeDefaults = &shapeStrokeDefaults{Color: "#ffffff", Width: 4}
		}
		got, _ := sceneLayoutKey(next, width, 56)
		if got == key {
			t.Fatal("context did not invalidate", change)
		}
	}
	for _, opaque := range []bool{false, true} {
		next := cloneSlide(slide)
		if opaque {
			next.Elements[0].Extra = extensionFixture("future", `true`)
		} else {
			next.Elements[0].Kind = "future-widget"
		}
		if _, ok := sceneLayoutKey(next, 245, 56); ok {
			t.Fatal("unknown adapter/metadata reused geometry")
		}
	}
	for _, query := range []string{"font=legacy-font", "%66ont=legacy-font"} {
		next := cloneSlide(slide)
		next.Elements[0].Query += "&" + query
		if _, ok := sceneLayoutKey(next, 245, 56); ok {
			t.Fatal("retired font/encoded keys were cached", query)
		}
	}
}

func TestSceneLayoutSkeletonOwnershipAndBounds(t *testing.T) {
	var c sceneLayoutCache
	var key [32]byte
	c.put(key, []Line{{Element: 0, Role: "shape", Row: 3, Col: 5, Text: "SECRET ART", Query: "private=SECRET"}, {Element: 0, Role: "shape", Row: 6, Col: 2, Text: "SECRET"}, {Element: 0, Role: "outline", Text: "IGNORE"}})
	lines, ok := c.get(key)
	if !ok || len(lines) != 3 || lines[0].Row != 3 || lines[0].Col != 5 {
		t.Fatal("shape anchor not preserved", lines)
	}
	for _, line := range lines {
		if strings.TrimSpace(line.Text) != "" || line.Query != "" {
			t.Fatal("cache retained painted/private data")
		}
	}
	lines[0].Row = 999
	again, _ := c.get(key)
	if again[0].Row != 3 {
		t.Fatal("returned skeleton aliases cache")
	}
	for n := 0; n < maxSceneLayoutEntries+10; n++ {
		key[0] = byte(n)
		c.put(key, []Line{{Text: "width", Element: n}})
	}
	if len(c.entries) > maxSceneLayoutEntries || c.bytes > maxSceneLayoutBytes {
		t.Fatal("cache exceeded its bounds")
	}
	if _, ok := sceneLayoutKey(Slide{Elements: []Element{{Kind: "text", Text: strings.Repeat("x", maxSceneLayoutEntryBytes+1)}}}, 245, 56); ok {
		t.Fatal("oversized input was cached")
	}
}

func TestSceneLayoutCacheConcurrentOwnership(t *testing.T) {
	var c sceneLayoutCache
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := [32]byte{byte(n)}
			for i := 0; i < 100; i++ {
				c.put(key, []Line{{Element: n, Text: "width", Row: n}})
				if lines, ok := c.get(key); ok {
					lines[0].Query = "caller mutation"
				}
			}
		}(n)
	}
	wg.Wait()
}

func TestSceneLayoutImageIdentityAndNativeCacheStamp(t *testing.T) {
	asset := clipboardTestAsset(t)
	id := newStableID("layout-image")
	path := embeddedAssetPath(id, asset)
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{ID: "picture", Kind: "image", AssetID: id, Path: path, Query: "left=5&top=5&width=20&height=10"}}}}}
	materializeDeckAssets(&d)
	t.Cleanup(func() {
		os.Remove(path)
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	key, ok := sceneLayoutKey(d.Slides[0], 245, 56)
	if !ok {
		t.Fatal("materialized embedded asset was not cacheable")
	}
	changed := asset
	changed.Data = append(append([]byte(nil), asset.Data...), 0)
	registerEmbeddedStillAsset(id, changed)
	next, ok := sceneLayoutKey(d.Slides[0], 245, 56)
	if !ok || key == next {
		t.Fatal("same-ID compact content replacement did not invalidate")
	}
	registerEmbeddedStillAsset(id, asset)
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	next, ok = sceneLayoutKey(d.Slides[0], 245, 56)
	if !ok || next == key {
		t.Fatal("native cache-file stamp did not invalidate")
	}
	foreign := cloneSlide(d.Slides[0])
	foreign.Elements[0].Path = "/tmp/foreign-image.png"
	if _, ok := sceneLayoutKey(foreign, 245, 56); ok {
		t.Fatal("mutable local reference was treated as an embedded cache asset")
	}
	foreign.Elements[0].AssetID = ""
	if _, ok := sceneLayoutKey(foreign, 245, 56); ok {
		t.Fatal("unregistered local reference was treated as immutable geometry")
	}
}

func TestSceneLayoutNaturalAnimationStaysCold(t *testing.T) {
	id := newStableID("layout-animation")
	asset := clipboardTestAsset(t)
	asset.Frames = []DeckAssetFrame{{Data: asset.Data, DelayMS: 100}, {Data: asset.Data, DelayMS: 100}}
	registerEmbeddedAnimatedAsset(id, asset)
	t.Cleanup(func() {
		embeddedAnimatedAssets.Lock()
		delete(embeddedAnimatedAssets.assets, id)
		delete(embeddedAnimatedAssets.identities, id)
		embeddedAnimatedAssets.Unlock()
	})
	e := Element{Kind: "image", AssetID: id, Path: "keynope-asset:" + id + ".gif"}
	if _, ok := sceneLayoutImageIdentity(e); ok {
		t.Fatal("natural alpha-trimmed animation treated as fixed geometry")
	}
	e.Query = "image-fit=contain&width=20&height=10"
	if _, ok := sceneLayoutImageIdentity(e); !ok {
		t.Fatal("explicitly framed animation was not reusable")
	}
}

func BenchmarkSceneGeometryPaintCache(b *testing.B) {
	for _, count := range []int{100, 500} {
		for _, reuse := range []bool{false, true} {
			b.Run(fmt.Sprintf("objects=%d/reuse=%t", count, reuse), func(b *testing.B) {
				d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}}
				for n := 0; n < count; n++ {
					d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: fmt.Sprint(n), Kind: "text", Text: "Dense content", Query: fmt.Sprintf("render=truetype&modern-font=mono&modern-size=24&left=%d&top=%d&width=24&height=3", n%10*24, n%16*3)})
				}
				base := d.Slides[0].Elements[0].Query
				metrics := &sceneRenderMetrics{DisableLayoutCache: !reuse}
				if _, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, metrics); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					d.Slides[0].Elements[0].Query = base + []string{"&fg=%23ff0055", "&fg=%2355ffff"}[i%2]
					if _, err := buildStyledSceneTargetsMeasured(d, d.ResolveSlide(0, false), 0, 245, 56, true, nil, metrics); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
