package app

import (
	"image/color"
	"os"
	"reflect"
	"testing"
)

func TestSceneEmbeddedPixelsDoNotDependOnMaterialisedFile(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	id := newStableID("scene-memory-source")
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, A: 255})}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{ID: "image", Kind: "image", AssetID: id, Query: "left=8&top=8&width=40&height=20&image-fit=contain&image-style=retro&glyph=blocks"}}}}}
	materializeDeckAssets(&d)
	path := d.Slides[0].Elements[0].Path
	t.Cleanup(func() {
		os.Remove(path)
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	before := cloneDeck(d)
	if got := sceneEmbeddedImagePath(d, d.Slides[0].Elements[0]); got != "keynope-asset:"+id+".png" {
		t.Fatal("owned source remained file-backed", got)
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	again, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scene, again) {
		t.Fatal("owned scene changed when incidental cache file disappeared")
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("scene normalization changed authored paths/assets")
	}
	foreign := d.Slides[0].Elements[0]
	foreign.Path = "/tmp/not-owned.png"
	if sceneEmbeddedImagePath(d, foreign) != foreign.Path {
		t.Fatal("foreign local reference was rewritten")
	}
	missing := cloneDeck(d)
	delete(missing.Assets, id)
	if sceneEmbeddedImagePath(missing, d.Slides[0].Elements[0]) != path {
		t.Fatal("missing deck asset treated as owned")
	}
	replacement := asset
	replacement.Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	registerEmbeddedStillAsset(id, replacement)
	if sceneEmbeddedImagePath(d, d.Slides[0].Elements[0]) != path {
		t.Fatal("different registered pixels reused by ID alone")
	}
	d.Assets[id] = replacement
	next, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(scene, next) {
		t.Fatal("matching same-ID pixel replacement did not invalidate scene")
	}
}

func BenchmarkSceneEmbeddedGeometryIdentity(b *testing.B) {
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(b, color.NRGBA{R: 255, A: 255})}
	id := newStableID("geometry-memory-bench")
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{ID: "image", Kind: "image", AssetID: id, Query: "left=8&top=8&width=40&height=20&image-fit=contain"}}}}}
	materializeDeckAssets(&d)
	path := d.Slides[0].Elements[0].Path
	b.Cleanup(func() {
		os.Remove(path)
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	for _, owned := range []bool{false, true} {
		name := "file-backed"
		if owned {
			name = "owned-pixels"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				slide := d.Slides[0]
				slide.Elements = append([]Element(nil), slide.Elements...)
				if owned {
					slide.Elements[0].Path = sceneEmbeddedImagePath(d, slide.Elements[0])
				}
				if _, ok := sceneLayoutKey(slide, 245, 56); !ok {
					b.Fatal("uncacheable source")
				}
			}
		})
	}
}
