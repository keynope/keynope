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
	check(0) // Detached slides do not consume the base background.
	deck.Assets = map[string]DeckAsset{"unused": {Data: []byte{1}}}
	check(0)
	deck.Assets["unused"] = DeckAsset{Data: []byte{2}}
	check(0)
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
	if rebuilt := cache.misses - before; rebuilt != 0 {
		t.Fatalf("unused master mutation rebuilt %d pages", rebuilt)
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

func TestWorkspaceCacheRetainsCurrentLateSlide(t *testing.T) {
	deck := themedSharedTextFixture(1)
	for len(deck.Slides) < 140 {
		deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
	}
	var cache workspacePageCache
	check := func(current, misses int) {
		t.Helper()
		before := cache.misses
		got, err := cache.render(deck, false, current, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		want, err := editorWorkspacePages(deck, false, current, 245, 56)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("priority changed workspace output", err)
		}
		if cache.misses-before != misses {
			t.Fatalf("current %d rebuilt %d pages, want %d", current, cache.misses-before, misses)
		}
		if _, exists := cache.pages[current]; !exists {
			t.Fatal("current slide evicted", current)
		}
		if len(cache.pages) != 128 || len(cache.consumers.base) != 128 {
			t.Fatal("cache/index bounds", len(cache.pages), len(cache.consumers.base))
		}
	}
	check(139, 140)
	if _, retained := cache.pages[0]; retained {
		t.Fatal("first slide outranked active late slide")
	}
	check(139, 12)
	deck.Slides[139].Elements[0].Text = "Late edit"
	check(139, 13)
	deck.Slides[139].Notes = "Private speaker note"
	check(139, 12)
	check(0, 12)
	check(0, 12)
}

func TestWorkspacePagePriorityBudget(t *testing.T) {
	entries := make([]workspacePageEntry, 5)
	for i := range entries {
		entries[i] = workspacePageEntry{bytes: 20 << 20, refs: workspaceReferences{assets: map[string]bool{fmt.Sprint(i): true}}}
	}
	retained, index := retainWorkspacePages(entries, 3)
	for _, i := range []int{3, 2, 4} {
		if _, ok := retained[i]; !ok || !index.assets[fmt.Sprint(i)][i] {
			t.Fatal("wrong priority/index", retained, i)
		}
	}
	if len(retained) != 3 {
		t.Fatal("byte budget exceeded", len(retained))
	}
	entries[3].bytes = 65 << 20
	retained, _ = retainWorkspacePages(entries, 3)
	if _, ok := retained[3]; ok {
		t.Fatal("oversized page retained")
	}
	if len(retained) != 3 {
		t.Fatal("oversized active page prevented useful neighbour retention")
	}
}
