package main

import (
	"bytes"
	"context"
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"keynope/internal/pptx"
)

func TestNaturalTextUsesResolvedPhysicalFontsAtEveryPageSize(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"text", "heading", "code", "bullet"} {
		for _, font := range []string{"c64", "sans", "mono"} {
			e := Element{ID: "text", Kind: kind, Level: 1, Text: "Wide text\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=64&modern-width=80&align=center&valign=middle"}
			for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1440, 1080}, {1, 1080, 1920}, {1, 3840, 1080}} {
				for _, grid := range [][2]int{{245, 56}, {160, 90}} {
					d := Deck{Appearance: &DeckAppearance{Version: 3}, PageSize: size.Clone(), Slides: []Slide{{Elements: []Element{e}}}}
					before := cloneDeck(d)
					scene, err := buildDocumentSlideScene(d, 0, grid[0], grid[1])
					if err != nil || len(scene.Objects) != 1 {
						t.Fatal(err, scene)
					}
					o := scene.Objects[0]
					expectedWidth := math.Max(chartTextWidth("Wide text", o.Text), chartTextWidth("Second line", o.Text))
					if kind == "bullet" {
						expectedWidth += 64 * 1.1
					}
					expectedWidth = math.Ceil(expectedWidth + 1)
					expectedHeight := math.Ceil(2 * 64 * o.Text.LineHeight)
					if o.Bounds.Width != expectedWidth || o.Bounds.Height != expectedHeight || math.Abs(o.Bounds.X-(scene.Width-expectedWidth)/2) > 1e-7 || math.Abs(o.Bounds.Y-(scene.Height-expectedHeight)/2) > 1e-7 {
						t.Fatal("natural text frame uses wrong font, page or anchor", kind, font, size, grid, o.Bounds, expectedWidth, expectedHeight)
					}
					if o.Text.Size != 64 || !reflect.DeepEqual(before, d) {
						t.Fatal("natural fit changed font/source")
					}
				}
			}
		}
	}
}

func TestNaturalTextRunMetricsTabsAndParagraphSpacing(t *testing.T) {
	base := sceneText{FontID: "sans", Size: 32, WidthScale: .8, LineHeight: 1.25}
	large := base
	large.FontID = "mono"
	large.Size = 96
	text := base
	text.Runs = []sceneRun{{Text: "W", FontID: "mono", FontSize: 96}, {Text: "i"}}
	w, h := sceneTextNaturalSize(&text)
	if w != math.Ceil(chartTextWidth("W", &large)+chartTextWidth("i", &base)+1) || h != 120 {
		t.Fatal("mixed run font/size or return to base ignored", w, h)
	}
	text = base
	text.Runs = []sceneRun{{Text: "A", Color: "#ff0000"}, {Text: "V", Color: "#00ff00"}}
	w, _ = sceneTextNaturalSize(&text)
	if w != math.Ceil(chartTextWidth("AV", &base)+1) {
		t.Fatal("paint-only boundary removed kerning")
	}
	text = base
	text.Runs = []sceneRun{{Text: "A\tB"}}
	tab := chartTextWidth("    ", &base)
	want := (math.Floor(chartTextWidth("A", &base)/tab)+1)*tab + chartTextWidth("B", &base)
	w, _ = sceneTextNaturalSize(&text)
	if w != math.Ceil(want+1) {
		t.Fatal("tab stop ignored", w, want)
	}
	text = base
	text.ParagraphBefore = .2
	text.ParagraphAfter = .3
	text.Paragraphs = []sceneParagraph{{Bullet: true, Runs: []sceneRun{{Text: "First"}}}, {Bullet: true, Runs: []sceneRun{{Text: "Second\ncontinued"}}}}
	_, h = sceneTextNaturalSize(&text)
	if h != math.Ceil(3*32*1.25+2*(.2+.3)*32) {
		t.Fatal("paragraph padding or continuation height ignored", h)
	}
	text = base
	text.FontRoles = map[string]string{"body": "mono"}
	text.Runs = []sceneRun{{Text: "WWW", FontID: "sans", FontRole: "body"}}
	font := base
	font.FontID = "mono"
	w, _ = sceneTextNaturalSize(&text)
	if w != math.Ceil(chartTextWidth("WWW", &font)+1) {
		t.Fatal("resolved word font role ignored")
	}
}

func TestNaturalTextBoundsPreserveAuthoredAxesAndAnchor(t *testing.T) {
	for _, query := range []string{"left=5&top=7", "align=center&valign=middle", "right=2&bottom=3", "right_pct=0.1&bottom=3"} {
		q, _ := url.ParseQuery(query)
		old := sceneRect{X: 100, Y: 200, Width: 400, Height: 300}
		natural := sceneRect{Width: 250, Height: 80}
		got := naturalSceneTextBounds(old, natural, q)
		if got.Width != 250 || got.Height != 80 {
			t.Fatal("natural extents ignored")
		}
		if q.Has("left") {
			if got.X != old.X || got.Y != old.Y {
				t.Fatal("explicit origin moved")
			}
		} else if q.Get("align") == "center" {
			if got.X+got.Width/2 != old.X+old.Width/2 || got.Y+got.Height/2 != old.Y+old.Height/2 {
				t.Fatal("centre moved")
			}
		} else if got.X+got.Width != old.X+old.Width || got.Y+got.Height != old.Y+old.Height {
			t.Fatal("anchored edge moved")
		}
		q.Set("height", "12")
		got = naturalSceneTextBounds(old, natural, q)
		if got.Height != old.Height || got.Y != old.Y {
			t.Fatal("authored height changed")
		}
		q.Set("width", "50")
		if naturalSceneTextBounds(old, natural, q) != old {
			t.Fatal("bounded frame changed")
		}
	}
}

func TestNaturalTextCacheLazyBoundedAndConcurrent(t *testing.T) {
	var cache sceneTextObjectCache
	key := sceneTextObjectKey{element: Element{Kind: "text", Text: "Wide text"}}
	text := &sceneText{FontID: "mono", Size: 64, WidthScale: 1, LineHeight: 1.25, Runs: []sceneRun{{Text: "Wide text"}}}
	cache.put(key, sceneObject{Kind: "text", Text: text}, nil)
	if cache.entries[key].natural.Width != 0 {
		t.Fatal("bounded text eagerly measured")
	}
	first, hit := cache.naturalSize(key, text)
	if hit || first.Width <= 0 {
		t.Fatal("missing lazy natural extent")
	}
	second, hit := cache.naturalSize(key, text)
	if !hit || first != second {
		t.Fatal("natural extent not reused")
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				size, hit := cache.naturalSize(key, text)
				if !hit || size != first {
					t.Error("unstable concurrent natural extent")
				}
			}
		}()
	}
	workers.Wait()
	if len(cache.entries) != 1 || cache.bytes != cache.entries[key].cost {
		t.Fatal("natural extent created a second cache")
	}
}

func TestNaturalTextAlignmentAndWidthCommandsKeepPhysicalFrame(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}, {1, 3840, 1080}} {
			for _, font := range []string{"c64", "sans", "mono"} {
				for _, actionName := range []string{"set-modern-text-style", "set-text-width"} {
					d := Deck{PageSize: size.Clone(), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "natural", Kind: "text", Text: "Wide text\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=64&modern-width=80&align=center&valign=middle"}}}}}
					s := newNativeEditorSession("Natural.md", d)
					if master {
						s.masterMode = true
						s.currentMaster = 0
						s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
					}
					sceneBounds := func() sceneRect {
						var scene slideScene
						if master {
							scene = *s.deck.masterRenderPreview(0, 245, 56).ModernScene
						} else {
							var err error
							scene, err = buildMixedSlideScene(s.deck, 0, 245, 56)
							if err != nil {
								t.Fatal(err)
							}
						}
						for _, o := range scene.Objects {
							if o.ID == "natural" {
								return o.Bounds
							}
						}
						t.Fatal("missing natural text")
						return sceneRect{}
					}
					old := sceneBounds()
					before := cloneDeck(s.deck)
					value, align, vertical, revision := 81., "justify", "middle", s.version
					action := nativeEditorAction{Action: actionName, ObjectIDs: []string{"natural"}, SceneMaster: master, SceneRevision: &revision}
					if actionName == "set-text-width" {
						action.TextWidth = &value
					} else {
						action.TextAlign = &align
						action.TextVertical = &vertical
					}
					if err := s.apply(action); err != nil {
						t.Fatal(actionName, master, err)
					}
					after := sceneBounds()
					for _, p := range [][2]float64{{old.X, after.X}, {old.Y, after.Y}, {old.Width, after.Width}, {old.Height, after.Height}} {
						if math.Abs(p[0]-p[1]) > 1e-7 {
							t.Fatal("style command changed physical frame", master, size, font, actionName, old, after)
						}
					}
					if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
						t.Fatal("style frame undo not exact", err)
					}
				}
			}
		}
	}
}

func TestPageSizeTrueTypeCoveringBoundsUsePhysicalAxes(t *testing.T) {
	e := Element{Kind: "text", Text: "A line\nSecond", Query: "render=truetype&ttf-size=97"}
	for _, canvas := range []sceneRect{{Width: 1920, Height: 1080}, {Width: 1080, Height: 1920}, {Width: 3840, Height: 1080}} {
		w, h := trueTypeBoundsOnCanvas(e, 245, 56, canvas)
		longest := float64(len("Second")) * 97 * .8 * .4167
		if w != int(math.Ceil(longest*245/canvas.Width)) || h != int(math.Ceil(2*97*1.2*56/canvas.Height)) {
			t.Fatal("covering text uses fixed physical axes", canvas, w, h)
		}
		fixed := e
		fixed.Query += "&width=32&height=10"
		if w, h := trueTypeBoundsOnCanvas(fixed, 245, 56, canvas); w != 32 || h != 10 {
			t.Fatal("authored box changed")
		}
	}
	// Bound the existing sampled fallback even when natural semantic text is long.
	e.Text = strings.Repeat("W", 65536)
	if w, h := trueTypeBoundsOnCanvas(e, 245, 56, sceneRect{Width: 1080, Height: 1920}); w != 245 || h > 56 {
		t.Fatal("covering fallback allocation lost its page bound")
	}
}

func TestNaturalTextPortableAndNativePPTXFrame(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, font := range []string{"c64", "sans", "mono"} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, PageSize: &DeckPageSize{1, 1080, 1920}, Slides: []Slide{{ID: "natural-slide", Elements: []Element{{ID: "natural", Kind: "text", Text: "Wide text\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=64&modern-width=80&align=center&valign=middle"}}}}}
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		before := cloneDeck(d)
		md, err := serializeDeck("Natural.md", d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Natural.md", md)
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := buildDocumentSlideScene(loaded, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		var reopened sceneObject
		for _, o := range reloaded.Objects {
			if o.ID == "natural" {
				reopened = o
			}
		}
		if reopened.Bounds != scene.Objects[0].Bounds || mustQuery(loaded.Slides[0].Elements[0].Query).Has("width") {
			t.Fatal("save/reopen baked or changed natural frame")
		}
		data, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(native.Slides) != 1 || len(native.Slides[0].Objects) != 1 {
			t.Fatal("unexpected native objects")
		}
		o := native.Slides[0].Objects[0]
		b := scene.Objects[0].Bounds
		for _, p := range [][2]float64{{o.X, b.X}, {o.Y, b.Y}, {o.Width, b.Width}, {o.Height, b.Height}} {
			if math.Abs(p[0]-p[1]) > 1./6350+.00001 {
				t.Fatal("native frame differs from natural scene", font, o, b)
			}
		}
		if o.FontSize != 32 || !reflect.DeepEqual(before, d) {
			t.Fatal("export changed physical font or authored source", o.FontSize)
		}
	}
}
