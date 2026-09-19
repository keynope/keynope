package main

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Independent pre-optimisation formula: deliberately reparses typography for
// each token, preserving the old rounding/emoji-spacing behaviour for parity.
func referenceMeasuredTrueTypeBounds(e Element, cols, rows int) (int, int) {
	q, _ := url.ParseQuery(e.Query)
	size := float64(trueTypeSize(e))
	longest := 0.0
	for _, line := range strings.Split(e.Text, "\n") {
		width := 0.0
		for _, token := range splitEmojiText(line) {
			if token.assetKey != "" {
				width += size*.8*trueTypeWidthPercent(e)/100 + 2*math.Max(1, size/40)
			} else {
				width += float64(len([]rune(token.text))) * size * .8 * .4167 * trueTypeWidthPercent(e) / 100
			}
		}
		longest = math.Max(longest, width)
	}
	w := int(math.Ceil(longest * float64(cols) / 1920))
	h := int(math.Ceil(float64(len(strings.Split(e.Text, "\n"))) * size * 1.2 * float64(rows) / 1080))
	return max(1, min(cols, intQueryDefault(q, "width", w))), max(1, min(rows, intQueryDefault(q, "height", h)))
}

func TestMeasuredTrueTypeBoundsParity(t *testing.T) {
	for _, text := range []string{"", "Hello\n", "a😍b✨c\n👨‍👩‍👧‍👦", "é中\n\nend"} {
		for _, kind := range []string{"text", "heading", "bullet", "code"} {
			for _, query := range []string{"", "ttf-size=193&ttf-width=37.5", "ttf-size=-1&ttf-width=NaN", "ttf-width=Inf", "ttf-width=500&height=4", "ttf-size=999&width=30", "ttf-size=bad&ttf-width=0", "ttf-width=50&ttf-width=100"} {
				for _, size := range [][2]int{{245, 56}, {1920, 1080}} {
					e := Element{Kind: kind, Level: 2, Text: text, Query: query}
					w, h := trueTypeBounds(e, size[0], size[1])
					wantW, wantH := referenceMeasuredTrueTypeBounds(e, size[0], size[1])
					if w != wantW || h != wantH {
						t.Fatalf("%+v: got %dx%d, want %dx%d", e, w, h, wantW, wantH)
					}
				}
			}
		}
	}
}

func BenchmarkMeasuredTrueTypeBounds(b *testing.B) {
	e := Element{Kind: "text", Text: strings.Repeat("Text 😍 more ✨ words\n", 40), Query: "ttf-size=97&ttf-width=75"}
	for _, name := range []string{"reference", "shared-query"} {
		b.Run(name, func(b *testing.B) {
			measure := trueTypeBounds
			if name == "reference" {
				measure = referenceMeasuredTrueTypeBounds
			}
			measure(e, 245, 56) // Exclude one-time emoji catalogue initialisation.
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				measure(e, 245, 56)
			}
		})
	}
}

func TestExplicitTrueTypeBoundsIgnoreContent(t *testing.T) {
	for _, text := range []string{"", "A", strings.Repeat("Long text 😍\n", 1000)} {
		for _, tc := range []struct {
			query string
			w, h  int
		}{
			{"width=30&height=3", 30, 3},
			{"width=999&height=999", 245, 56},
			{"width=0&height=-5", 1, 1},
			{"width=30&width=90&height=3", 30, 3},
		} {
			e := Element{Kind: "text", Text: text, Query: tc.query}
			w, h := trueTypeBounds(e, 245, 56)
			if w != tc.w || h != tc.h {
				t.Fatalf("%s: got %dx%d, want %dx%d", tc.query, w, h, tc.w, tc.h)
			}
		}
	}
}

func TestIncompleteTrueTypeBoundsKeepContentSizing(t *testing.T) {
	for _, query := range []string{"", "width=bad", "height=bad", "width=30", "height=3", "width=30&height=bad", "width=bad&height=3"} {
		e := Element{Kind: "text", Text: "Hello\nWorld", Query: query}
		w, h := trueTypeBounds(e, 1920, 1080)
		wantW, wantH := 162, 233
		if strings.Contains(query, "width=30") {
			wantW = 30
		}
		if strings.Contains(query, "height=3") {
			wantH = 3
		}
		if w != wantW || h != wantH {
			t.Fatalf("%s: got %dx%d, want %dx%d", query, w, h, wantW, wantH)
		}
	}
}

func BenchmarkExplicitTrueTypeBounds(b *testing.B) {
	for _, count := range []int{1, 1000} {
		text := strings.Repeat("Text 😍\n", count)
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			e := Element{Kind: "text", Text: text, Query: "width=30&height=3"}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				trueTypeBounds(e, 245, 56)
			}
		})
	}
}
