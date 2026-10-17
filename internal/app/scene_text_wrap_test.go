package app

import (
	"bytes"
	"context"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"keynope/internal/pptx"
)

func TestWrappedTextFontMetricsAndUnicodeBoundaries(t *testing.T) {
	base := sceneText{FontID: "mono", Size: 40, WidthScale: 1, LineHeight: 1.25}
	advance := chartTextWidth("A", &base)
	for _, tc := range []struct {
		text  string
		cells float64
		lines int
	}{{"", 5, 1}, {"AA AA AA AA", 5.01, 2}, {"AA AA AA AA", 3.01, 4}, {"AA AA AA AA", 2.01, 4}, {"AAAAAAA", 3.01, 3}, {"AA\nBB", 3.01, 2}, {"AA\n", 3.01, 2}, {"AA\n\nBB", 3.01, 3}, {"A\tB", 4.01, 2}, {"AA-AA-AA", 3.01, 3}} {
		text := base
		text.Runs = []sceneRun{{Text: tc.text}}
		got := sceneTextWrappedHeight(&text, tc.cells*advance)
		if got != float64(tc.lines)*50 {
			t.Fatal("wrong wrapped line count", tc, got)
		}
	}
	// An indivisible combining cluster or joined emoji is never split into
	// separate rows even when it is wider than its frame.
	for _, value := range []string{"e\u0301", "👨‍👩‍👧‍👦"} {
		text := base
		text.Runs = []sceneRun{{Text: value}}
		if height := sceneTextWrappedHeight(&text, 1); height != 50 {
			t.Fatal("grapheme cluster split", value, height)
		}
	}
	text := base
	text.ParagraphBefore = .2
	text.ParagraphAfter = .3
	text.Paragraphs = []sceneParagraph{{Bullet: true, Runs: []sceneRun{{Text: "AAAAAAA"}}}, {Bullet: true, Runs: []sceneRun{{Text: "AA"}}}}
	width := 3.01*advance + base.Size*1.1
	if h := sceneTextWrappedHeight(&text, width); h != 4*50+2*(.2+.3)*40 {
		t.Fatal("bullet indent or paragraph spacing ignored", h)
	}
}

func TestWrappedTextCacheIsWidthKeyedAndBounded(t *testing.T) {
	var cache sceneTextObjectCache
	key := sceneTextObjectKey{element: Element{Kind: "text", Text: "AA AA AA AA"}}
	text := &sceneText{FontID: "mono", Size: 40, WidthScale: 1, LineHeight: 1.25, Runs: []sceneRun{{Text: key.element.Text}}}
	advance := chartTextWidth("A", text)
	first, hit := cache.wrappedHeight(key, text, 5.01*advance)
	if hit || first != 100 {
		t.Fatal(first, hit)
	}
	second, hit := cache.wrappedHeight(key, text, 5.01*advance)
	if !hit || second != first {
		t.Fatal("width cache missed")
	}
	small, hit := cache.wrappedHeight(key, text, 2.01*advance)
	if hit || small != 200 {
		t.Fatal("width reused wrong layout")
	}
	for i := 1; i < 100; i++ {
		cache.wrappedHeight(key, text, float64(i))
	}
	if len(cache.entries) != 1 || len(cache.entries[key].wrapped) != 8 || cache.bytes != cache.entries[key].cost {
		t.Fatal("unbounded resize cache")
	}
}

func TestWrappedTextPhysicalFrameAndAuthoritativeHeight(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}, {1, 3840, 1080}} {
		d := Deck{PageSize: size.Clone(), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "wrap", Kind: "text", Text: strings.Repeat("word ", 12), Query: "render=truetype&modern-font=mono&modern-size=48&width=40&align=center&valign=middle"}}}}}
		before := cloneDeck(d)
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil || len(scene.Objects) != 1 {
			t.Fatal(err)
		}
		o := scene.Objects[0]
		width := 40 * float64(size.Width) / 245
		h := sceneTextWrappedHeight(o.Text, width)
		if math.Abs(o.Bounds.Width-width) > 1e-8 || o.Bounds.Height != h || math.Abs(o.Bounds.Y-(float64(size.Height)-h)/2) > 1e-8 || o.Text.Size != 48 {
			t.Fatal("auto height/anchor/font incorrect", size, o.Bounds, h)
		}
		if !reflect.DeepEqual(before, d) {
			t.Fatal("auto height authored into deck")
		}
		d.Slides[0].Elements[0].Query += "&height=3"
		explicit, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(explicit.Objects[0].Bounds.Height-3*float64(size.Height)/56) > 1e-8 {
			t.Fatal("explicit height replaced")
		}
	}
}

func TestWrappedTextCacheConcurrentWidths(t *testing.T) {
	var cache sceneTextObjectCache
	text := &sceneText{FontID: "mono", Size: 40, WidthScale: 1, LineHeight: 1.25, Runs: []sceneRun{{Text: strings.Repeat("word ", 20)}}}
	key := sceneTextObjectKey{element: Element{Kind: "text", Text: text.Runs[0].Text}}
	natural, _ := cache.naturalSize(key, text)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(width float64) {
			defer wg.Done()
			want := sceneTextWrappedHeight(text, width)
			for j := 0; j < 20; j++ {
				got, _ := cache.wrappedHeight(key, text, width)
				if got != want {
					t.Error("concurrent width lost ownership")
				}
			}
		}(float64(100 + i*25))
	}
	wg.Wait()
	if again, hit := cache.naturalSize(key, text); !hit || again != natural || len(cache.entries) != 1 {
		t.Fatal("width results replaced intrinsic text cache")
	}
}

func TestWrappedTextPortableAndNativePPTXHeight(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{PageSize: &DeckPageSize{1, 1080, 1920}, Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "wrap-slide", Elements: []Element{{ID: "wrap", Kind: "text", Text: strings.Repeat("word ", 12), Query: "render=truetype&modern-font=mono&modern-size=48&width=40&align=center&valign=middle"}}}}}
	scene, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	b := scene.Objects[0].Bounds
	before := cloneDeck(d)
	md, err := serializeDeck("Wrapped.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Wrapped.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if mustQuery(loaded.Slides[0].Elements[0].Query).Has("height") {
		t.Fatal("derived height persisted")
	}
	reloaded, err := buildDocumentSlideScene(loaded, 0, 245, 56)
	if err != nil || reloaded.Objects[0].Bounds != b {
		t.Fatal("reopen lost auto height", err)
	}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(native.Slides[0].Objects) != 1 {
		t.Fatal("unexpected native objects")
	}
	o := native.Slides[0].Objects[0]
	if math.Abs(o.Height-b.Height) > 1./6350+.00001 || math.Abs(o.Y-b.Y) > 1./6350+.00001 || o.FontSize != 24 || !reflect.DeepEqual(before, d) {
		t.Fatal("native export changed frame/font/source", o, b)
	}
}

func TestWrappedCoveringHeightUsesFullOffPageFrameWidth(t *testing.T) {
	e := Element{Kind: "text", Text: strings.Repeat("word ", 12), Query: "render=truetype&modern-font=mono&modern-size=48&width=1000"}
	w, h := trueTypeBoundsOnCanvas(e, 245, 56, sceneRect{Width: 1080, Height: 1920})
	if w != 245 || h != 2 {
		t.Fatal("covering allocation or full-frame wrap width changed", w, h)
	}
}
