package main

import (
	"math"
	"net/url"
	"path/filepath"
	"testing"
)

func TestIntegerTextAnchorDoesNotAllocate(t *testing.T) {
	q := url.Values{"align": {"right"}, "valign": {"bottom"}, "right": {"2"}}
	allocations := testing.AllocsPerRun(100, func() {
		x, y := preciseTextAnchor(q, 30, 3, 213, 53, 245, 56)
		if x != 213 || y != 53 {
			t.Fatal("integer layout anchor changed")
		}
	})
	if allocations != 0 {
		t.Fatalf("integer anchor allocated %g times", allocations)
	}
}

func TestFractionalTextAnchorReusesParsedPlacement(t *testing.T) {
	q := url.Values{"right": {"2"}, "bottom": {"3"}, "fg": {"#55aaff"}, "link": {"https://example.test/?a=b&c=d"}}
	before := q.Encode()
	x, y := preciseTextAnchor(q, 30.25, 3.5, 213, 49, 245, 56)
	if x != 212.75 || y != 49.5 {
		t.Fatalf("fractional anchored edges changed: %g,%g", x, y)
	}
	if q.Encode() != before {
		t.Fatal("placement mutated shared metadata")
	}
}

func TestContinuousObjectPlacement(t *testing.T) {
	near := func(a, b float64) {
		t.Helper()
		if math.Abs(a-b) > 1e-7 {
			t.Fatalf("%g != %g", a, b)
		}
	}
	for _, style := range []string{"retro", "modern"} {
		deck := sceneFixture(t)
		deck.Slides[0].Engagement = nil
		deck.Slides[0].EngagementResult = nil
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		before, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for i, e := range deck.Slides[0].Elements {
			if e.Kind == "connector" {
				continue
			}
			e.Query = setQueryValue(setQueryValue(e.Query, "object-offset-x", "0.123456"), "object-offset-y", "0.765432")
			deck.Slides[0].Elements[i] = e
		}
		after, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for i, o := range after.Objects {
			if o.Kind == "connector" {
				continue
			}
			near(o.Bounds.X-before.Objects[i].Bounds.X, .123456*1920/245)
			near(o.Bounds.Y-before.Objects[i].Bounds.Y, .765432*1080/56)
			near(o.Bounds.Width, before.Objects[i].Bounds.Width)
			near(o.Bounds.Height, before.Objects[i].Bounds.Height)
			if o.RetroLines != nil || o.RetroText != nil || o.RetroLabel != nil {
				if o.SampleOffset == nil {
					t.Fatal("retained artwork missing local-origin compensation")
				}
			}
		}
		path := filepath.Join(t.TempDir(), "Placement.md")
		data, err := serializeDeck(path, deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range loaded.Slides[0].Elements {
			if e.Kind != "connector" {
				q, _ := url.ParseQuery(e.Query)
				near(objectPlacementOffset(q, "x"), .123456)
				near(objectPlacementOffset(q, "y"), .765432)
			}
		}
	}
	e := Element{ID: "shape", Kind: "shape", Query: "shape=circle&width=12&height=8&object-rotation=37.5"}
	before := rotateShapePorts(shapePorts(e, 20, 10), e, 20, 10, 245, 56)
	e.Query += "&object-offset-x=0.123456&object-offset-y=0.765432"
	after := rotateShapePorts(shapePorts(e, 20, 10), e, 20, 10, 245, 56)
	for i, p := range after {
		near(p.X-before[i].X, .123456)
		near(p.Y-before[i].Y, .765432)
	}
	for _, invalid := range []string{"NaN", "+Inf", "-0.1", "1", "12", "oops"} {
		if objectPlacementOffset(url.Values{"object-offset-x": {invalid}}, "x") != 0 {
			t.Fatal("invalid offset accepted")
		}
	}
}
