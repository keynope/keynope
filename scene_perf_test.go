package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func denseMixedSceneFixture(t testing.TB, count int) Deck {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 4), uint8(y * 8), 120, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	id, asset, path, err := embeddedImageAsset(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: "hide"}}}
	for i := 0; i < count; i++ {
		group := i / 5
		left, top := group%10*24, group%10*5
		style := "modern"
		if group%2 == 0 {
			style = "retro"
		}
		e := Element{ID: fmt.Sprintf("object-%d", i), Query: fmt.Sprintf("element-style=%s&left=%d&top=%d&width=16&height=3", style, left, top)}
		switch i % 5 {
		case 0:
			e.Kind = "text"
			e.Text = "Mixed slide"
			e.Query += "&render=truetype&ttf-size=32&modern-size=32"
		case 1, 2:
			e.Kind = "shape"
			e.Query += "&shape=square&fg=%2355aaff"
		case 3:
			e.Kind = "image"
			e.AssetID = id
			e.Path = path
			e.Query += "&modern-mask=ellipse"
		case 4:
			e.Kind = "connector"
			e.Query = fmt.Sprintf("element-style=%s&connector-from=object-%d&connector-to=object-%d&connector-from-side=right&connector-to-side=left&connector-mode=elbow", style, i-3, i-2)
		}
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, e)
	}
	return deck
}

func TestDenseMixedSceneFixture(t *testing.T) {
	deck := denseMixedSceneFixture(t, 100)
	before, _ := json.Marshal(deck)
	scene, err := buildMixedSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 100 {
		t.Fatalf("got %d objects", len(scene.Objects))
	}
	counts, styles := map[string]int{}, map[string]int{}
	for _, object := range scene.Objects {
		counts[object.Kind]++
		styles[object.Style]++
	}
	if counts["text"] != 20 || counts["shape"] != 40 || counts["image"] != 20 || counts["connector"] != 20 || styles["retro"] != 50 || styles["modern"] != 50 {
		t.Fatalf("counts %v styles %v", counts, styles)
	}
	after, _ := json.Marshal(deck)
	if !bytes.Equal(before, after) {
		t.Fatal("projection changed fixture")
	}
}

func BenchmarkMixedSceneEndpoint(b *testing.B) {
	for _, count := range []int{100, 500, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			session := nativeEditorSession{deck: denseMixedSceneFixture(b, count), version: 1}
			request := httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil)
			warm := httptest.NewRecorder()
			session.handleScene(warm, request)
			if warm.Code != 200 {
				b.Fatal(warm.Body.String())
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				response := httptest.NewRecorder()
				session.handleScene(response, request)
				if response.Code != 200 {
					b.Fatal(response.Body.String())
				}
				b.ReportMetric(float64(response.Body.Len()), "response-bytes")
			}
		})
	}
}

// Phase timings use the opt-in buffered endpoint; compare the unprofiled
// benchmark for allocation/total cost. No transport or display frame is timed.
func BenchmarkMixedScenePhases(b *testing.B) {
	session := nativeEditorSession{deck: denseMixedSceneFixture(b, 1000), version: 1}
	request := httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil)
	warm := httptest.NewRecorder()
	session.handleScene(warm, request)
	if warm.Code != 200 {
		b.Fatal(warm.Body.String())
	}
	totals := map[string]float64{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		session.handleScene(response, request)
		if response.Code != 200 {
			b.Fatal(response.Body.String())
		}
		for _, timing := range response.Header().Values("Server-Timing") {
			name, value, ok := strings.Cut(timing, ";dur=")
			ms, err := strconv.ParseFloat(value, 64)
			if !ok || err != nil {
				b.Fatalf("invalid phase: %q", timing)
			}
			totals[name] += ms
		}
	}
	b.StopTimer()
	for name, ms := range totals {
		b.ReportMetric(ms*1000/float64(b.N), name+"-us/op")
	}
}

// Explicitly separates route-cache misses from the warm endpoint benchmark.
// Asset decoding may remain warm; this is not a complete cold application boot.
func BenchmarkColdRouteSceneEndpoint(b *testing.B) {
	session := nativeEditorSession{deck: denseMixedSceneFixture(b, 1000), version: 1}
	request := httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		connectorRouteCache.mu.Lock()
		connectorRouteCache.entries = nil
		connectorRouteCache.order.Init()
		connectorRouteCache.bytes = 0
		connectorRouteCache.mu.Unlock()
		b.StartTimer()
		response := httptest.NewRecorder()
		session.handleScene(response, request)
		if response.Code != 200 {
			b.Fatal(response.Body.String())
		}
		b.ReportMetric(float64(response.Body.Len()), "response-bytes")
	}
}

// Synthetic and intentionally dense: no private decks or activity results.
// Measures Go scene generation separately from browser layout/paint/transport.
func BenchmarkDenseScene(b *testing.B) {
	for _, style := range []string{"modern", "retro"} {
		for _, count := range []int{100, 500} {
			b.Run(fmt.Sprintf("%s/%d", style, count), func(b *testing.B) {
				deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{PageNumber: "hide"}}}
				for i := 0; i < count; i++ {
					deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprint("text-", i), Kind: "text", Text: "Dense slide text", Query: fmt.Sprintf("render=truetype&ttf-size=32&modern-size=32&left=%d&top=%d&width=30&height=3", i%8*30, i%16*3)})
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if _, err := buildMixedSlideScene(deck, 0, 245, 56); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Includes the production endpoint's document snapshot, resolved layout,
// editing metadata and JSON encoding. Does not include sockets or browser paint.
func BenchmarkSceneEndpoint(b *testing.B) {
	for _, style := range []string{"modern", "retro"} {
		for _, count := range []int{100, 500, 1000} {
			b.Run(fmt.Sprintf("%s/%d", style, count), func(b *testing.B) {
				deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{PageNumber: "hide"}}}
				for i := 0; i < count; i++ {
					deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprint("text-", i), Kind: "text", Text: "Dense slide text", Query: fmt.Sprintf("render=truetype&ttf-size=32&modern-size=32&left=%d&top=%d&width=30&height=3", i%8*30, i%16*3)})
				}
				session := nativeEditorSession{deck: deck, version: 1}
				request := httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil)
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					response := httptest.NewRecorder()
					session.handleScene(response, request)
					if response.Code != 200 {
						b.Fatal(response.Body.String())
					}
					b.ReportMetric(float64(response.Body.Len()), "response-bytes")
				}
			})
		}
	}
}
