package main

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestShapedTextCacheConcurrentOwnership(t *testing.T) {
	var cache sceneTextObjectCache
	key := sceneTextObjectKey{element: Element{ID: "shared"}}
	var workers sync.WaitGroup
	for worker := 0; worker < 12; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 100; i++ {
				o := sceneObject{Kind: "text", Text: &sceneText{Runs: []sceneRun{{Text: "source"}}, FontRoles: map[string]string{"body": "sans"}, ColorTokens: map[string]string{"text": "#ffffff"}}}
				cache.put(key, o, []sceneDiagnostic{{Code: "source"}})
				o.Text.ColorTokens["text"] = "caller"
				got, diagnostics, ok := cache.get(key)
				if !ok || got.Text.Runs[0].Text != "source" || got.Text.ColorTokens["text"] != "#ffffff" || got.Text.FontRoles["body"] != "sans" || diagnostics[0].Code != "source" {
					t.Error("concurrent cache snapshot lost ownership")
					return
				}
				got.Text.Runs[0].Text = "reader"
				got.Text.ColorTokens["text"] = "reader"
				got.Text.FontRoles["body"] = "reader"
				diagnostics[0].Code = "reader"
			}
		}()
	}
	workers.Wait()
	if len(cache.entries) != 1 || cache.bytes != cache.entries[key].cost {
		t.Fatal("concurrent misses duplicated cache accounting")
	}
}

func TestShapedTextCacheAccountsStyleStrings(t *testing.T) {
	for _, field := range []string{"color-map", "run-font", "paragraph-token", "family"} {
		t.Run(field, func(t *testing.T) {
			var cache sceneTextObjectCache
			o := sceneObject{Kind: "text", Text: &sceneText{}}
			large := strings.Repeat("x", 8<<20)
			switch field {
			case "color-map":
				o.Text.ColorTokens = map[string]string{"text": large}
			case "run-font":
				o.Text.Runs = []sceneRun{{FontID: large}}
			case "paragraph-token":
				o.Text.Paragraphs = []sceneParagraph{{Runs: []sceneRun{{ColorToken: large}}}}
			case "family":
				o.Text.Family = large
			}
			cache.put(sceneTextObjectKey{}, o, nil)
			if len(cache.entries) != 0 {
				t.Fatal("oversized style data retained")
			}
		})
	}
}

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

func TestTextProjectionConsumerInvalidation(t *testing.T) {
	defer clearSceneTextObjects()
	fixture := func() Deck {
		deck := themedSharedTextFixture(4)
		deck.Appearance = &DeckAppearance{Version: 3}
		deck.Theme = builtinDeckTheme("clean-light")
		for i := range deck.Slides[0].Elements {
			e := &deck.Slides[0].Elements[i]
			e.Text = fmt.Sprintf("Unique content %d", i)
			e.Query = fmt.Sprintf("render=truetype&top=%d&left=10&width=80&height=8", i*10)
		}
		deck.Slides[0].Elements[3].Query += "&modern-font=mono&modern-size=72&modern-width=90&modern-line-height=1.4&fg=%23ff0055"
		return deck
	}
	for _, operation := range []string{"paint", "move", "rotate", "bounds", "link", "unused-token", "inherited-color", "text", "font", "theme-font", "theme-width", "theme-line-height", "unknown-extension"} {
		t.Run(operation, func(t *testing.T) {
			clearSceneTextObjects()
			deck := fixture()
			project := func(metrics *sceneRenderMetrics) slideScene {
				t.Helper()
				scene, err := buildStyledSceneTargetMeasured(deck, deck.ResolveSlide(0, false), 0, 245, 56, true, "", metrics)
				if err != nil {
					t.Fatal(err)
				}
				return scene
			}
			project(nil)
			e := &deck.Slides[0].Elements[0]
			q, _ := url.ParseQuery(e.Query)
			wantBuilds := 0
			switch operation {
			case "paint":
				q.Set("fg", "#55ffaa")
				q.Set("shadow", "soft")
				q.Set("modern-opacity", ".4")
			case "move":
				q.Set("left", "15")
				q.Set("object-offset-x", ".125")
			case "rotate":
				q.Set("object-rotation", "23.5")
			case "bounds":
				q.Set("width", "101.5")
				q.Set("height", "9.25")
			case "link":
				q.Set("link", "https://keynope.sh")
			case "unused-token":
				deck.Theme.Colors.Accent[5] = "#ffaa00"
			case "inherited-color":
				deck.Theme.Colors.Text = "#aabbcc"
			case "text":
				e.Text = "Changed content 😍"
				wantBuilds = 1
			case "font":
				q.Set("modern-font", "mono")
				wantBuilds = 1
			case "theme-font":
				deck.Theme.Fonts.Body.FontID = "mono"
				wantBuilds = 3
			case "theme-width":
				deck.Theme.Fonts.Body.Width = 85
				wantBuilds = 3
			case "theme-line-height":
				deck.Theme.Fonts.Body.LineHeight = 1.6
				wantBuilds = 3
			case "unknown-extension":
				q.Set("future-text-effect", "new")
				wantBuilds = 1
			}
			e.Query = q.Encode()
			metrics := &sceneRenderMetrics{}
			warm := project(metrics)
			if metrics.SceneBuilds != 1 || len(metrics.TextProjectionBuilds) != wantBuilds || len(metrics.TextProjectionHits) != 4-wantBuilds {
				t.Fatalf("wrong consumers rebuilt: %+v, want %d builds", metrics, wantBuilds)
			}
			if wantBuilds == 1 && metrics.TextProjectionBuilds[0] != e.ID {
				t.Fatal("unrelated text invalidated")
			}
			clearSceneTextObjects()
			if !reflect.DeepEqual(warm, project(nil)) {
				t.Fatal("narrow cache changed output versus a cold projection")
			}
		})
	}
}

func TestTextProjectionMetricsAreOptIn(t *testing.T) {
	defer clearSceneTextObjects()
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(3), true, false)
	for _, profile := range []bool{false, true} {
		clearSceneTextObjects()
		path := "/api/editor/scene?slide=0"
		if profile {
			path += "&timing=1"
		}
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if profile && (w.Header().Get("X-Keynope-Text-Builds") != "2" || w.Header().Get("X-Keynope-Text-Cache-Hits") != "1") {
			t.Fatalf("missing projection counters: %v", w.Header())
		}
		if !profile && w.Header().Get("X-Keynope-Scene-Builds") != "" {
			t.Fatal("ordinary output exposed profiling data")
		}
	}
}
