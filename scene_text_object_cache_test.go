package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestShapedTextCacheBounds(t *testing.T) {
	var cache sceneTextObjectCache
	object := sceneObject{Kind: "text", Text: &sceneText{Runs: []sceneRun{{Text: "small"}}}}
	for i := 0; i < 2100; i++ {
		cache.put(sceneTextObjectKey{element: Element{ID: fmt.Sprint(i)}}, object, nil)
	}
	if len(cache.entries) > 2048 || cache.bytes > 8<<20 {
		t.Fatal("cache exceeded its budget")
	}
	count := len(cache.entries)
	cache.put(sceneTextObjectKey{element: Element{Text: strings.Repeat("x", 8<<20)}}, object, nil)
	if len(cache.entries) != count {
		t.Fatal("oversized object should not displace the cache")
	}
}

func clearSceneTextObjects() {
	sharedSceneTextObjects.mu.Lock()
	defer sharedSceneTextObjects.mu.Unlock()
	sharedSceneTextObjects.entries = nil
	sharedSceneTextObjects.bytes = 0
}

func TestShapedTextCacheOwnsMutableData(t *testing.T) {
	var cache sceneTextObjectCache
	alpha := .7
	object := sceneObject{Kind: "text", TextCapabilities: []string{"size"}, Paint: scenePaint{Opacity: &alpha}, Text: &sceneText{Runs: []sceneRun{{Text: "Original"}}, Paragraphs: []sceneParagraph{{Runs: []sceneRun{{Text: "Bullet"}}}}, EmojiFonts: map[string]string{"x": "font"}}}
	key := sceneTextObjectKey{element: Element{ID: "test"}}
	cache.put(key, object, []sceneDiagnostic{{Code: "original"}})
	object.Text.Runs[0].Text = "caller mutation"
	got, diagnostics, ok := cache.get(key)
	if !ok {
		t.Fatal("cache miss")
	}
	if got.Text.Runs[0].Text != "Original" {
		t.Fatal("stored caller-owned runs")
	}
	got.Text.Runs[0].Text = "mutated"
	got.Text.Paragraphs[0].Runs[0].Text = "mutated"
	got.Text.EmojiFonts["x"] = "mutated"
	*got.Paint.Opacity = 0
	got.TextCapabilities[0] = "mutated"
	diagnostics[0].Code = "mutated"
	again, diagnostics, _ := cache.get(key)
	if again.Text.Runs[0].Text != "Original" || again.Text.Paragraphs[0].Runs[0].Text != "Bullet" || again.Text.EmojiFonts["x"] != "font" || *again.Paint.Opacity != .7 || again.TextCapabilities[0] != "size" || diagnostics[0].Code != "original" {
		t.Fatal("cache leaked mutable state")
	}
}

func TestShapedTextCacheDependenciesMatchColdProjection(t *testing.T) {
	defer clearSceneTextObjects()
	for _, mutation := range []string{"text", "paint", "geometry", "flow", "theme", "style", "slide-count", "dimensions"} {
		t.Run(mutation, func(t *testing.T) {
			clearSceneTextObjects()
			deck := themedSharedTextFixture(4)
			deck.Slides[0].Elements[1].Query = "render=truetype&width=30&height=3"
			if _, err := buildMixedSlideScene(deck, 0, 245, 56); err != nil {
				t.Fatal(err)
			}
			cols := 245
			switch mutation {
			case "text":
				deck.Slides[0].Elements[0].Text = "New text 😍"
			case "paint":
				deck.Slides[0].FG = "38;2;255;0;0"
				deck.Slides[0].FGSet = true
			case "geometry":
				deck.Slides[0].Elements[0].Query += "&object-offset-x=0.5"
			case "flow":
				deck.Slides[0].Elements[0].Query = "render=truetype&width=30&height=10&top=2"
			case "theme":
				deck.Appearance.Extra["theme"] = []byte(`"paper-v1"`)
			case "style":
				deck.Slides[0].Elements[0].Query = "render=truetype&element-style=modern&width=30&height=3"
			case "slide-count":
				deck.Slides = append(deck.Slides, Slide{})
			case "dimensions":
				cols = 120
			}
			warm, err := buildMixedSlideScene(deck, 0, cols, 56)
			if err != nil {
				t.Fatal(err)
			}
			clearSceneTextObjects()
			cold, err := buildMixedSlideScene(deck, 0, cols, 56)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(warm, cold) {
				t.Fatal("cached object projection differs from cold projection")
			}
		})
	}
}

func BenchmarkSingleTextMutationProjection(b *testing.B) {
	deck := themedSharedTextFixture(500)
	buildMixedSlideScene(deck, 0, 245, 56)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deck.Slides[0].Elements[0].Text = fmt.Sprint("Changed ", i)
		if _, err := buildMixedSlideScene(deck, 0, 245, 56); err != nil {
			b.Fatal(err)
		}
	}
}
