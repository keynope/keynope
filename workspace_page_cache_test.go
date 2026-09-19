package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestWorkspaceCacheInvalidation(t *testing.T) {
	deck := themedSharedTextFixture(2)
	deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]), cloneSlide(deck.Slides[0]))
	var cache workspacePageCache
	check := func(wantMisses int) {
		t.Helper()
		before := cache.misses
		got, err := cache.render(deck, false, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		want, err := editorWorkspacePages(deck, false, 0, 245, 56)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("cached output diverged: %v", err)
		}
		if cache.misses-before != wantMisses {
			t.Fatalf("rebuilt %d pages, want %d", cache.misses-before, wantMisses)
		}
		if len(cache.changed) != wantMisses {
			t.Fatalf("delta contains %d slide replacements, want %d", len(cache.changed), wantMisses)
		}
	}
	check(3)
	check(0)
	deck.Slides[1].Elements[0].Text = "Local edit"
	check(1)
	deck.Slides[1].Elements[0].Text = "Shared text" // Undo.
	check(1)
	deck.Appearance.Extra["theme"] = json.RawMessage(`"warm-v1"`)
	check(3)
	deck.HideActivityQR = true
	check(3)
	deck.Masters.Base.Slide.BG = "44"
	check(3)
	deck.Assets = map[string]DeckAsset{"unused": {Data: []byte{1}}}
	check(3)
	deck.Assets["unused"] = DeckAsset{Data: []byte{2}}
	check(3)
	deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
	check(4)
	if _, err := cache.render(deck, false, 0, 320, 90); err != nil {
		t.Fatal(err)
	}
	check(4)
}

func TestWorkspaceCacheIgnoresMasterNamesAndOrdering(t *testing.T) {
	deck := themedSharedTextFixture(2)
	deck.EnsureDefaultMasters()
	deck.Masters.Layouts = append(deck.Masters.Layouts,
		MasterLayout{ID: "layout-a", Name: "Alpha", Slide: Slide{FG: "37"}},
		MasterLayout{ID: "layout-b", Name: "Beta", Slide: Slide{BG: "40"}},
	)
	var cache workspacePageCache
	if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	before := cache.misses
	deck.Masters.Base.Name = "Renamed Base"
	deck.Masters.Layouts[0].Name = "Renamed Layout"
	deck.Masters.Layouts[0], deck.Masters.Layouts[1] = deck.Masters.Layouts[1], deck.Masters.Layouts[0]
	if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	if rebuilt := cache.misses - before; rebuilt != 0 {
		t.Fatalf("master names/order rebuilt %d slides, want 0", rebuilt)
	}
	deck.Masters.Layouts[0].Slide.BG = "41"
	if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	if rebuilt := cache.misses - before; rebuilt != len(deck.Slides) {
		t.Fatalf("visual master mutation total rebuilds = %d, want %d", rebuilt, len(deck.Slides))
	}
}

func BenchmarkWorkspaceLocalEdit(b *testing.B) {
	for _, cached := range []bool{false, true} {
		name := "full"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			deck := themedSharedTextFixture(10)
			for len(deck.Slides) < 100 {
				deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
			}
			var cache workspacePageCache
			if cached {
				if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				deck.Slides[50].Elements[0].Text = string(rune('A' + i%26))
				var err error
				if cached {
					_, err = cache.render(deck, false, 0, 245, 56)
				} else {
					_, err = editorWorkspacePages(deck, false, 0, 245, 56)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkOwnedWorkspaceSnapshot(b *testing.B) {
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}}
	for slideIndex := 0; slideIndex < 30; slideIndex++ {
		slide := Slide{}
		for elementIndex := 0; elementIndex < 40; elementIndex++ {
			slide.Elements = append(slide.Elements, Element{
				ID:    fmt.Sprintf("text-%d-%d", slideIndex, elementIndex),
				Kind:  "text",
				Text:  "A workshop text object",
				Query: "render=truetype&top=3&left=4&width=30&height=5",
			})
		}
		deck.Slides = append(deck.Slides, slide)
	}
	for _, duplicateClone := range []bool{true, false} {
		b.Run(fmt.Sprintf("duplicateClone=%t", duplicateClone), func(b *testing.B) {
			var cache workspacePageCache
			if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				owned := cloneDeckForRender(deck)
				var err error
				if duplicateClone {
					_, err = cache.render(owned, false, 0, 245, 56)
				} else {
					_, err = cache.renderOwned(owned, false, 0, 245, 56)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestWorkspaceCachePageLimitAndReorder(t *testing.T) {
	deck := themedSharedTextFixture(1)
	for len(deck.Slides) < 130 {
		deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
	}
	deck.Slides[1].Elements[0].Text = "Different page"
	var cache workspacePageCache
	if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	if len(cache.pages) != 128 {
		t.Fatalf("retained %d pages", len(cache.pages))
	}
	deck.Slides[0], deck.Slides[1] = deck.Slides[1], deck.Slides[0]
	before := cache.misses
	got, err := cache.render(deck, false, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	want, err := editorWorkspacePages(deck, false, 0, 245, 56)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("reordered pages differ")
	}
	if cache.misses-before != 4 {
		t.Fatalf("reorder should rebuild two moved and two uncached pages, got %d", cache.misses-before)
	}
}
