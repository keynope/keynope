package main

import (
	"reflect"
	"testing"
)

func TestMasterWorkspaceCacheParityAndTransitions(t *testing.T) {
	d := themedSharedTextFixture(2)
	d.EnsureDefaultMasters()
	var cache workspacePageCache
	check := func(master bool, current, cols int, reused bool) {
		t.Helper()
		before := cache.masterPreviews
		got, err := cache.render(d, master, current, cols, 56)
		want, coldErr := editorWorkspacePages(cloneDeckForRender(d), master, current, cols, 56)
		if err != nil || coldErr != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("cache differs from cold render", err, coldErr)
		}
		if master && (cache.masterPreviews == before) != reused {
			t.Fatal("unexpected master reuse", reused)
		}
	}
	check(true, 0, 245, false)
	check(true, 0, 245, true)
	if cache.masterHits != 1 || cache.hits != 0 {
		t.Fatal("master hits polluted normal metrics")
	}
	d.Masters.Base.Slide.BG = "44"
	check(true, 0, 245, false)
	check(true, 0, 245, true)
	d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{Kind: "text", Text: "Changed master", Query: "render=truetype"})
	check(true, 0, 245, false)
	check(true, 1, 245, false)
	check(true, 1, 245, true)
	check(true, 1, 300, false)
	check(false, 0, 245, false)
	if cache.masterPages != nil {
		t.Fatal("normal mode retained master pages")
	}
	check(true, 1, 245, false)
	if len(cache.pages) == 0 {
		t.Fatal("master mode discarded normal consumers")
	}
	if _, err := cache.render(d, true, -1, 245, 56); err == nil {
		t.Fatal("invalid master accepted")
	}
	check(true, 1, 245, false)
}

func TestMasterPreviewSharesNormalRetentionBudget(t *testing.T) {
	d := themedSharedTextFixture(1)
	d.EnsureDefaultMasters()
	cache := workspacePageCache{pages: map[int]workspacePageEntry{0: {bytes: 64 << 20}}}
	pages, err := cache.render(d, true, 0, 245, 56)
	if err != nil || len(pages) == 0 {
		t.Fatal("uncached master preview unavailable", err)
	}
	if cache.masterPages != nil || len(cache.masterPrevious.Slides) != 0 {
		t.Fatal("master preview exceeded shared retention budget")
	}
	delete(cache.pages, 0)
	if _, err = cache.render(d, true, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	if len(cache.masterPages) == 0 {
		t.Fatal("master preview not retained with available budget")
	}
}

func TestMasterModePreservesNormalConsumerInvalidation(t *testing.T) {
	d := workspaceConsumerFixture()
	var cache workspacePageCache
	assertWorkspaceConsumerRender(t, &cache, d, []int{0, 1, 2, 3, 4})
	if _, err := cache.render(d, true, 3, 300, 56); err != nil {
		t.Fatal(err)
	}
	d.Masters.Layouts[2].Slide.BG, d.Masters.Layouts[2].Slide.BGSet = "#ddaa77", true
	if _, err := cache.render(d, true, 3, 300, 56); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceConsumerRender(t, &cache, d, nil)
	if _, err := cache.render(d, true, 1, 245, 56); err != nil {
		t.Fatal(err)
	}
	d.Masters.Layouts[0].Slide.BG = "#ffeeaa"
	if _, err := cache.render(d, true, 1, 245, 56); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceConsumerRender(t, &cache, d, []int{0})
}

func TestWorkspaceAssetPayloadLowerBound(t *testing.T) {
	data := []byte{1, 2, 3}
	assets := map[string]DeckAsset{"asset": {Data: data, Frames: []DeckAssetFrame{{Data: data}}, Vector: &DeckAssetVector{Data: data}, Source: &DeckAssetSource{Data: data, Frames: []DeckAssetFrame{{Data: data}}}}}
	if !workspaceAssetPayloadFits(assets, 20) || workspaceAssetPayloadFits(assets, 19) {
		t.Fatal("every serialized media occurrence must count")
	}
	assets = map[string]DeckAsset{"asset": {Data: []byte{1}}}
	if !workspaceAssetPayloadFits(assets, 4) || workspaceAssetPayloadFits(assets, 3) {
		t.Fatal("base64 padding not counted")
	}
	if workspaceAssetPayloadFits(assets, -1) {
		t.Fatal("negative capacity accepted")
	}
	if !workspaceAssetPayloadFits(nil, 0) {
		t.Fatal("empty payload rejected")
	}
}

func TestMasterWorkspaceCacheOwnsInput(t *testing.T) {
	d := themedSharedTextFixture(2)
	d.EnsureDefaultMasters()
	var cache workspacePageCache
	if _, err := cache.render(d, true, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	d.Masters.Base.Name = "caller changed"
	if cache.masterPrevious.Masters.Base.Name == d.Masters.Base.Name {
		t.Fatal("retained caller-owned master")
	}
}

func BenchmarkMasterWorkspaceUnchanged(b *testing.B) {
	d := themedSharedTextFixture(2)
	d.EnsureDefaultMasters()
	for _, cached := range []bool{false, true} {
		name := "cold"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			var cache workspacePageCache
			_, _ = cache.render(d, true, 0, 245, 56)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				if cached {
					_, err = cache.render(d, true, 0, 245, 56)
				} else {
					_, err = editorWorkspacePages(cloneDeckForRender(d), true, 0, 245, 56)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
