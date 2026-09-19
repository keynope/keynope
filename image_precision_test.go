package main

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContinuousImageBox(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		d := sceneFixture(t)
		d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		e := d.Slides[0].Elements[len(d.Slides[0].Elements)-1]
		e.Query = "align=center&valign=middle&width=37.125&height=8.875&stretch=1&modern-mask=ellipse&modern-crop=0.1,0.1,0.1,0.1"
		d.Slides[0].Elements = []Element{e}
		d.Slides[0].Engagement = nil
		d.Slides[0].EngagementResult = nil
		check := func(deck Deck) sceneObject {
			t.Helper()
			s, err := buildMixedSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			o := s.Objects[0]
			for _, pair := range [][2]float64{{o.Bounds.Width, 37.125 * 1920 / 245}, {o.Bounds.Height, 8.875 * 1080 / 56}, {o.Bounds.X, (245 - 37.125) / 2 * 1920 / 245}, {o.Bounds.Y, (56 - 8.875) / 2 * 1080 / 56}} {
				if math.Abs(pair[0]-pair[1]) > 1e-7 {
					t.Fatalf("%s image box differs: %+v", style, o.Bounds)
				}
			}
			if style == "retro" && (o.SampleScale == nil || o.RetroLines == nil) {
				t.Fatal("missing precise sampled mapping")
			}
			return o
		}
		before := check(d)
		path := filepath.Join(t.TempDir(), "Image.md")
		data, err := serializeDeck(path, d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		after := check(loaded)
		if !reflect.DeepEqual(before.Media, after.Media) {
			t.Fatal("source/crop/mask changed on reopen")
		}
	}
}
