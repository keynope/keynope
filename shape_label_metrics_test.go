package main

import (
	"math"
	"net/url"
	"strings"
	"testing"
)

func TestShapeLabelConcreteFontMetrics(t *testing.T) {
	for _, font := range []string{"c64", "sans", "mono"} {
		label := Element{Kind: "text", Text: "Wide label WWW iii", Query: "render=truetype&modern-font=" + font + "&modern-size=40&modern-width=80"}
		q, _ := url.ParseQuery(label.Query)
		style := (Deck{Appearance: &DeckAppearance{Version: 3}}).modernSceneTextFromValues(label, q)
		w, h := shapeLabelSize(label, 245, 56)
		wantW := int(math.Ceil(math.Ceil(chartTextWidth(label.Text, style)+1)*245/1920)) + 2
		wantH := int(math.Ceil(math.Ceil(style.Size*style.LineHeight)*56/1080)) + 2
		if w != wantW || h != wantH {
			t.Fatalf("%s label sizing differs from bundled font: %dx%d != %dx%d", font, w, h, wantW, wantH)
		}
		label.Query = setQueryValue(label.Query, "modern-size", "80")
		bigW, bigH := shapeLabelSize(label, 245, 56)
		if bigW <= w || bigH <= h {
			t.Fatal("modern size ignored", font, w, h, bigW, bigH)
		}
		label.Query = setQueryValue(label.Query, "modern-width", "40")
		thinW, thinH := shapeLabelSize(label, 245, 56)
		if thinW >= bigW || thinH != bigH {
			t.Fatal("font width altered height or was ignored", font)
		}
	}
}

func TestSharedBundledFontMetricsDoNotOverflowLongLabels(t *testing.T) {
	style := &sceneText{FontID: "c64", Size: 1024, WidthScale: .4167}
	text := strings.Repeat("W", 65536)
	w := chartTextWidth(text, style)
	if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) || math.Abs(w-chartTextWidth("W", style)*65536) > .01 {
		t.Fatal("long label advance overflow", w)
	}
}
