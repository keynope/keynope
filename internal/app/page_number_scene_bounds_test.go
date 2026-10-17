package app

import (
	"math"
	"reflect"
	"testing"
)

func TestInheritedPageNumberUsesResolvedFontBounds(t *testing.T) {
	for _, font := range []string{"sans", "mono", "c64"} {
		d := powerupProofCorpus(t)["themes-and-overrides.md"]
		d.Theme.Fonts.Body.FontID = font
		before := cloneDeck(d)
		scene, err := buildSlideScene(d, 1, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range scene.Objects {
			if o.ID != "base-page-number" {
				continue
			}
			found = true
			expectedWidth := math.Ceil(chartTextWidth("2", o.Text) + 1)
			if o.Bounds.Width != expectedWidth || o.Bounds.Height < o.Text.Size*o.Text.LineHeight {
				t.Fatal("number uses mismatched font bounds", font, o.Bounds, o.Text)
			}
			if o.Bounds.X < 0 || o.Bounds.Y < 0 || o.Bounds.X+o.Bounds.Width > scene.Width || o.Bounds.Y+o.Bounds.Height > scene.Height {
				t.Fatal("number lost canvas anchoring", font, o.Bounds)
			}
		}
		if !found || !reflect.DeepEqual(d, before) {
			t.Fatal("missing number or authored deck mutated")
		}
		page := &d.Masters.Base.Slide.Elements[0]
		page.Query = setQueryValue(setQueryValue(page.Query, "width", "5"), "height", "4")
		explicit, err := buildSlideScene(d, 1, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range explicit.Objects {
			if o.ID == "base-page-number" && (math.Abs(o.Bounds.Width-5*1920.0/245) > 1e-6 || math.Abs(o.Bounds.Height-4*1080.0/56) > 1e-6) {
				t.Fatal("explicit page-number bounds were changed", font, o.Bounds)
			}
		}
	}
}
