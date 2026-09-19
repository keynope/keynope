package main

import (
	"math"
	"net/url"
	"path/filepath"
	"testing"
)

func TestContinuousSharedTextDimensions(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{Elements: []Element{{ID: "precise", Kind: "text", Text: "Keep my font", Query: "render=truetype&top=4&left=5&width=37.125&height=8.75&ttf-size=97&text-box=1"}}}}}
		w, h := trueTypeBounds(deck.Slides[0].Elements[0], 245, 56)
		if w != 38 || h != 9 {
			t.Fatalf("covering grid box = %dx%d", w, h)
		}
		check := func(d Deck) {
			t.Helper()
			scene, err := buildMixedSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			if len(scene.Objects) != 1 {
				t.Fatalf("unexpected objects: %d", len(scene.Objects))
			}
			o := scene.Objects[0]
			if o.RetroText != nil || o.RetroLines != nil {
				t.Fatal("fixture did not exercise shared text")
			}
			if math.Abs(o.Bounds.Width-37.125*1920/245) > 1e-7 || math.Abs(o.Bounds.Height-8.75*1080/56) > 1e-7 {
				t.Fatalf("%s dimensions rounded: %+v", style, o.Bounds)
			}
			if o.Bounds.X != 5*1920.0/245 || o.Bounds.Y != 4*1080.0/56 {
				t.Fatalf("explicit origin moved: %+v", o.Bounds)
			}
		}
		check(deck)
		path := filepath.Join(t.TempDir(), "Precise.md")
		data, err := serializeDeck(path, deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		check(loaded)
	}
}

func TestAuthoredDimensionValidation(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "wrong", ""} {
		if _, ok := authoredObjectDimension(url.Values{"width": {value}}, "width", 245); ok {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, pair := range []struct {
		value string
		want  float64
	}{{"37.125", 37.125}, {"9999", 245}, {".5", 1}, {"0", 1}, {"-5", 1}} {
		v, ok := authoredObjectDimension(url.Values{"width": {pair.value}}, "width", 245)
		if !ok || v != pair.want {
			t.Fatalf("%q -> %g", pair.value, v)
		}
	}
}

func TestFractionalTextAnchors(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		for _, tc := range []struct {
			query string
			x, y  float64
		}{
			{"align=center&valign=middle", (245 - 37.125) / 2, (56 - 8.75) / 2},
			{"align=right&valign=bottom", 245 - 37.125, 56 - 8.75},
			{"right=3&bottom=2", 245 - 37.125 - 3, 56 - 8.75 - 2},
			{"right_pct=0.25&bottom=2", 245 - 37.125 - 61, 56 - 8.75 - 2},
			{"left=5&top=4&align=center&valign=middle", 5, 4},
			{"left_pct=0.25&right=3&top=4", 61, 4},
			{"left=5&row_delta=4&valign=middle", 5, 4},
		} {
			deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: style}, Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Anchored", Query: "render=truetype&text-box=1&width=37.125&height=8.75&" + tc.query}}}}}
			scene, err := buildMixedSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			if len(scene.Objects) != 1 {
				t.Fatal("missing text")
			}
			b := scene.Objects[0].Bounds
			if math.Abs(b.X-tc.x*1920/245) > 1e-7 || math.Abs(b.Y-tc.y*1080/56) > 1e-7 {
				t.Fatalf("%s %s: got %+v want %g,%g cells", style, tc.query, b, tc.x, tc.y)
			}
		}
	}
	q := url.Values{"align": {"center"}, "valign": {"middle"}}
	if x, y := preciseTextAnchor(q, 38, 9, 103, 23, 245, 56); x != 103 || y != 23 {
		t.Fatal("integer placement changed")
	}
}
