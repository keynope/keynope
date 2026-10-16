package main

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func objectLayoutProofDeck(t testing.TB) Deck {
	t.Helper()
	deck := powerupDenseEditingDeck(t)
	// Distinct keys make counters independent of test ordering. Rewrite only
	// the fixture's stable IDs and connector/group references, not geometry.
	prefix := strings.ReplaceAll(t.Name(), "/", "-") + "-"
	for i := range deck.Slides[0].Elements {
		e := &deck.Slides[0].Elements[i]
		e.ID = prefix + e.ID
		q, _ := url.ParseQuery(e.Query)
		for _, field := range []string{"connector-from", "connector-to", "group"} {
			if value := q.Get(field); value != "" {
				q.Set(field, prefix+value)
			}
		}
		e.Query = q.Encode()
	}
	return deck
}

func objectLayoutProjection(t testing.TB, deck Deck, cold bool) (slideScene, *sceneRenderMetrics) {
	t.Helper()
	metrics := &sceneRenderMetrics{DisableLayoutCache: cold}
	scene, err := buildStyledSceneTargetsMeasured(deck, deck.ResolveSlide(0, false), 0, 245, 56, true, nil, metrics)
	if err != nil {
		t.Fatal(err)
	}
	return scene, metrics
}

func TestSceneObjectLayoutScopesEditsToTheirGeometry(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := objectLayoutProofDeck(t)
	objectLayoutProjection(t, deck, false)
	for _, edit := range []struct{ name, kind, key, value string }{
		{"text", "text", "", "Changed text"},
		{"font", "text", "modern-size", "31"},
		{"shape", "shape", "width", "21.25"},
		{"image", "image", "width", "31.5"},
	} {
		t.Run(edit.name, func(t *testing.T) {
			index := -1
			for i, e := range deck.Slides[0].Elements {
				if e.Kind == edit.kind {
					index = i
					break
				}
			}
			if index < 0 {
				t.Fatal("fixture missing edit kind", edit.kind)
			}
			e := &deck.Slides[0].Elements[index]
			if edit.key == "" {
				e.Text = edit.value
			} else {
				e.Query = setQueryValue(e.Query, edit.key, edit.value)
			}
			before := cloneDeck(deck)
			warm, metrics := objectLayoutProjection(t, deck, false)
			cold, control := objectLayoutProjection(t, deck, true)
			if !reflect.DeepEqual(warm, cold) || !reflect.DeepEqual(before, deck) {
				t.Fatal("incremental geometry changed cold scene or caller input")
			}
			if metrics.LayoutBuilds != 0 || control.LayoutBuilds != 1 || !reflect.DeepEqual(metrics.ObjectLayoutBuilds, []string{e.ID}) || len(metrics.ObjectLayoutHits) < 40 {
				t.Fatalf("unrelated layout work: warm=%+v cold=%+v", metrics, control)
			}
		})
	}
}

func TestSceneObjectLayoutRetainsFlowAndPositionedNeighbours(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{
		{ID: "flow-before", Kind: "text", Text: "First", Query: "render=truetype&modern-size=24&width=100"},
		{ID: "fixed", Kind: "text", Text: "Fixed", Query: "render=truetype&modern-size=24&top=20&left=30&width=50&height=4"},
		{ID: "flow-after", Kind: "text", Text: "Last", Query: "render=truetype&modern-size=24&width=100"},
	}}}}
	objectLayoutProjection(t, deck, false)
	deck.Slides[0].Elements[0].Text = "First\nSecond\nThird\nFourth"
	warm, metrics := objectLayoutProjection(t, deck, false)
	cold, _ := objectLayoutProjection(t, deck, true)
	if !reflect.DeepEqual(warm, cold) || !reflect.DeepEqual(metrics.ObjectLayoutBuilds, []string{"flow-before", "flow-after"}) || !reflect.DeepEqual(metrics.ObjectLayoutHits, []string{"fixed"}) {
		t.Fatalf("flow cascade changed independent placement: %+v", metrics)
	}
	if math.Abs(warm.Objects[1].Bounds.Y-20*1080.0/56) > 1e-9 || warm.Objects[2].Bounds.Y <= warm.Objects[0].Bounds.Y {
		t.Fatalf("flow order or explicit anchor was lost: %+v", warm.Objects)
	}
}

func TestSceneObjectLayoutThemePaintDoesNotRelayoutObjects(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := objectLayoutProofDeck(t)
	objectLayoutProjection(t, deck, false)
	deck.Theme.Colors.Background = "#112233"
	deck.Theme.Colors.Text = "#aaccff"
	deck.Theme.Colors.Accent[0] = "#ee55aa"
	warm, metrics := objectLayoutProjection(t, deck, false)
	cold, _ := objectLayoutProjection(t, deck, true)
	if !reflect.DeepEqual(warm, cold) || len(metrics.ObjectLayoutBuilds) != 0 || len(metrics.ObjectLayoutHits) < 40 || metrics.LayoutBuilds != 0 {
		t.Fatal("theme paint rebuilt independent geometry or became stale", metrics)
	}
}

func TestSceneObjectLayoutRetroFlowImageRetainsItsAnchor(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	previous := exportImageAnimationPosition
	t.Cleanup(func() { exportImageAnimationPosition = previous })
	position := 119 * time.Millisecond
	exportImageAnimationPosition = &position
	deck := objectLayoutProofDeck(t)
	flow := Element{ID: "retro-flow-owner", Kind: "text", Text: "Flow before image", Query: "left=5&width=80&height=4&render=truetype&modern-size=24"}
	deck.Slides[0].Elements = append([]Element{flow}, deck.Slides[0].Elements...)
	image := -1
	for i, e := range deck.Slides[0].Elements {
		if e.Kind == "image" {
			image = i
			q, _ := url.ParseQuery(e.Query)
			q.Set("image-style", "retro")
			for _, key := range []string{"top", "bottom", "row_delta", "valign"} {
				q.Del(key)
			}
			deck.Slides[0].Elements[i].Query = q.Encode()
			break
		}
	}
	if image < 0 {
		t.Fatal("missing fixture image")
	}
	for _, height := range []string{"4", "8", "5"} {
		deck.Slides[0].Elements[0].Query = setQueryValue(deck.Slides[0].Elements[0].Query, "height", height)
		warm, metrics := objectLayoutProjection(t, deck, false)
		cold, _ := objectLayoutProjection(t, deck, true)
		if !reflect.DeepEqual(warm, cold) || metrics.LayoutBuilds != 0 || !reflect.DeepEqual(metrics.ObjectArtworkLayouts, []string{deck.Slides[0].Elements[image].ID}) {
			t.Fatal("isolated image changed flow source placement or rebuilt the slide", metrics)
		}
	}
}

func TestSceneObjectLayoutCorpusColdParityAndReorder(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	previous := exportImageAnimationPosition
	t.Cleanup(func() { exportImageAnimationPosition = previous })
	position := 119 * time.Millisecond
	exportImageAnimationPosition = &position
	for name, source := range powerupProofCorpus(t) {
		t.Run(name, func(t *testing.T) {
			deck := newNativeEditorSession("Object-layout.md", source).deck
			for _, size := range []DeckPageSize{{Version: 1, Width: 1920, Height: 1080}, {Version: 1, Width: 1080, Height: 1920}, {Version: 1, Width: 2560, Height: 1080}} {
				deck.PageSize = size.Clone()
				for index := range deck.Slides {
					for _, reverse := range []bool{false, true} {
						if reverse {
							elements := deck.Slides[index].Elements
							for a, b := 0, len(elements)-1; a < b; a, b = a+1, b-1 {
								elements[a], elements[b] = elements[b], elements[a]
							}
						}
						slide := deck.ResolveSlide(index, false)
						warm, err := buildStyledSceneTargetsMeasured(deck, slide, index, 245, 56, true, nil, &sceneRenderMetrics{})
						if err != nil {
							t.Fatal(err)
						}
						cold, err := buildStyledSceneTargetsMeasured(deck, slide, index, 245, 56, true, nil, &sceneRenderMetrics{DisableLayoutCache: true})
						if err != nil || !reflect.DeepEqual(warm, cold) {
							t.Fatalf("cold parity failed page=%d aspect=%dx%d reverse=%t: %v", index, size.Width, size.Height, reverse, err)
						}
					}
				}
			}
		})
	}
}

func TestSceneObjectLayoutCacheOwnershipBoundsAndConcurrency(t *testing.T) {
	var cache sceneObjectLayoutCache
	geometry, valid := sceneLayoutSkeleton([]Line{{Element: 0, Role: "shape", Row: 3, Col: 5, Text: "PRIVATE ART", Query: "private=secret"}})
	if !valid {
		t.Fatal("fixture skeleton rejected")
	}
	key := sceneObjectLayoutKey{}
	cache.put(key, sceneObjectLayoutEntry{geometry: geometry, flow: preparedLayoutFlow{row: 9, hasBlock: true}})
	geometry.lines[0].Row = 1234
	entry, found := cache.get(key)
	if !found || entry.geometry.lines[0].Row == 1234 || entry.flow.row != 9 {
		t.Fatal("cache retained caller state")
	}
	for _, line := range entry.geometry.lines {
		if strings.TrimSpace(line.Text) != "" || line.Query != "" {
			t.Fatal("cache retained authored pixels or metadata")
		}
	}
	entry.geometry.lines[0].Row = -9876
	again, _ := cache.get(key)
	if again.geometry.lines[0].Row == -9876 {
		t.Fatal("cache returned mutable entry storage")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for index := 0; index < 400; index++ {
				key := sceneObjectLayoutKey{flow: preparedLayoutFlow{row: worker*400 + index}}
				cache.put(key, entry)
				if got, found := cache.get(key); found && len(got.geometry.lines) > 0 {
					got.geometry.lines[0].Row = -1
				}
			}
		}(worker)
	}
	wg.Wait()
	if len(cache.entries) > maxSceneObjectLayoutEntries || cache.bytes > maxSceneObjectLayoutBytes {
		t.Fatal("geometry cache exceeded its limits")
	}
	oversized := sceneObjectLayoutEntry{geometry: sceneLayoutEntry{cost: maxSceneLayoutEntryBytes + 1}}
	cache.put(sceneObjectLayoutKey{flow: preparedLayoutFlow{row: -1234}}, oversized)
	if _, found := cache.get(sceneObjectLayoutKey{flow: preparedLayoutFlow{row: -1234}}); found {
		t.Fatal("oversized entry retained")
	}
	// Repeated edits must evict historical keys, not flush every live owner.
	var recent sceneObjectLayoutCache
	hot := sceneObjectLayoutKey{flow: preparedLayoutFlow{row: -1}}
	recent.put(hot, entry)
	for i := 0; i < maxSceneObjectLayoutEntries+20; i++ {
		if _, found := recent.get(hot); !found {
			t.Fatal("active geometry was evicted with obsolete entries")
		}
		recent.put(sceneObjectLayoutKey{flow: preparedLayoutFlow{row: i}}, entry)
	}
}

func TestSceneObjectLayoutFingerprintAndCompatibilityFallback(t *testing.T) {
	var fingerprint sceneLayoutFingerprint
	for i := 0; i < 100; i++ {
		slide := Slide{Elements: []Element{{ID: fmt.Sprint(i), Kind: "text", Text: strings.Repeat("text", i), Query: "top=5&render=truetype"}}}
		got, eligible := fingerprint.key(slide, 245, 56)
		want, valid := sceneLayoutKey(slide, 245, 56)
		if got != want || eligible != valid {
			t.Fatal("reused fingerprint writer retained the previous key")
		}
	}
	for _, change := range []string{"legacy", "unknown-element", "unknown-slide"} {
		t.Run(change, func(t *testing.T) {
			deck := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Compatibility", Query: "top=3&render=truetype"}}}}}
			switch change {
			case "legacy":
				deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
			case "unknown-element":
				deck.Slides[0].Elements[0].Extra = extensionFixture("future", `true`)
			case "unknown-slide":
				deck.Slides[0].Extra = extensionFixture("future", `true`)
			}
			warm, metrics := objectLayoutProjection(t, deck, false)
			cold, _ := objectLayoutProjection(t, deck, true)
			if !reflect.DeepEqual(warm, cold) || len(metrics.ObjectLayoutBuilds)+len(metrics.ObjectLayoutHits) != 0 {
				t.Fatal("unknown/legacy layout used concrete object cache", metrics)
			}
		})
	}
}

func BenchmarkSceneObjectGeometryEdit(b *testing.B) {
	for _, optimized := range []bool{false, true} {
		b.Run(fmt.Sprintf("incremental=%t", optimized), func(b *testing.B) {
			deck := objectLayoutProofDeck(b)
			objectLayoutProjection(b, deck, false)
			generation := newStableID("bench")
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				// Unique content prevents a two-value warm-key benchmark from
				// measuring repeated whole-slide hits instead of real edits.
				deck.Slides[0].Elements[2].Text = fmt.Sprintf("%s %d", generation, index)
				objectLayoutProjection(b, deck, !optimized)
			}
		})
	}
}
