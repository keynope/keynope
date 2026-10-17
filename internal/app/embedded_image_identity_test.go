package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func identityTestPNG(t testing.TB, colour color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, colour)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestEmbeddedStillReplacementInvalidatesAllRenderCaches(t *testing.T) {
	id := "source-identity-still"
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, A: 255})}
	// Browser/in-memory paths; native still-image imports may materialize a
	// local PNG and use the separate file-backed decoder instead.
	path := "keynope-asset:" + id + ".png"
	registerEmbeddedStillAsset(id, asset)
	key := embeddedImageCacheIdentity(path)
	decoded := loadDecodedStillImage(path)
	regular := append([]string(nil), renderASCIIImage(path, "glyph=blocks", 8, 4)...)
	fast := append([]string(nil), renderFastASCIIImage(path, "glyph=blocks", 8, 4)...)
	if decoded == nil || len(regular) == 0 || len(fast) == 0 {
		t.Fatal("empty fixture")
	}
	registerEmbeddedStillAsset(id, asset)
	if embeddedImageCacheIdentity(path) != key || loadDecodedStillImage(path) != decoded {
		t.Fatal("identical registration discarded reusable source")
	}
	asset.Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	registerEmbeddedStillAsset(id, asset)
	if embeddedImageCacheIdentity(path) == key {
		t.Fatal("replacement retained old identity")
	}
	if got := loadDecodedStillImage(path); got == decoded || got.At(0, 0) == decoded.At(0, 0) {
		t.Fatal("decoded cache retained old pixels")
	}
	if reflect.DeepEqual(regular, renderASCIIImage(path, "glyph=blocks", 8, 4)) {
		t.Fatal("Retro cache retained old pixels")
	}
	if reflect.DeepEqual(fast, renderFastASCIIImage(path, "glyph=blocks", 8, 4)) {
		t.Fatal("fast preview retained old pixels")
	}
}

func TestNativeEmbeddedStillReplacementInvalidatesRenderCaches(t *testing.T) {
	id := newStableID("native-identity")
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, A: 255})}
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{ID: "picture", Kind: "image", AssetID: id}}}}}
	materializeDeckAssets(&d)
	path := d.Slides[0].Elements[0].Path
	t.Cleanup(func() {
		os.Remove(path)
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	before := embeddedImageCacheIdentity(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	materializeDeckAssets(&d)
	unchanged, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(unchanged.ModTime()) || embeddedImageCacheIdentity(path) != before {
		t.Fatal("identical materialisation invalidated reusable pixels", err)
	}
	regular := append([]string(nil), renderASCIIImage(path, "glyph=blocks", 8, 4)...)
	fast := append([]string(nil), renderFastASCIIImage(path, "glyph=blocks", 8, 4)...)
	asset.Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	d.Assets[id] = asset
	materializeDeckAssets(&d)
	if before == embeddedImageCacheIdentity(path) || reflect.DeepEqual(regular, renderASCIIImage(path, "glyph=blocks", 8, 4)) || reflect.DeepEqual(fast, renderFastASCIIImage(path, "glyph=blocks", 8, 4)) {
		t.Fatal("native materialised asset retained stale regular/fast pixels")
	}
}

func TestMaterializeEmbeddedStillFileReplacesSymlinkNotTarget(t *testing.T) {
	dir := t.TempDir()
	target, path := filepath.Join(dir, "private"), filepath.Join(dir, "compact.png")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedStillFile(path, []byte("pixels")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatal("materialisation overwrote a symlink target", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "pixels" {
		t.Fatal("replacement missing", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("temporary cache files leaked", err)
	}
}

func TestEmbeddedAssetIDsRejectPathTraversal(t *testing.T) {
	for _, id := range []string{"", "../escaped", "nested/path", "\\escape", ".", ".."} {
		assets := map[string]DeckAsset{id: {MIME: "image/png", Data: []byte("pixels")}}
		if _, err := encodeDeckAssets(assets); err == nil {
			t.Fatal("unsafe asset ID accepted", id)
		}
		payload, err := json.Marshal(assets)
		if err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		markdown := "<!-- keynope-assets version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->"
		if _, _, err := decodeDeckAssets(markdown); err == nil {
			t.Fatal("unsafe archived asset ID accepted", id)
		}
		d := Deck{Assets: assets, Slides: []Slide{{Elements: []Element{{Kind: "image", AssetID: id, Path: "unchanged"}}}}}
		materializeDeckAssets(&d)
		if d.Slides[0].Elements[0].Path != "unchanged" {
			t.Fatal("unsafe asset materialised", id)
		}
	}
}

func TestEmbeddedAnimationReplacementInvalidatesTrack(t *testing.T) {
	slide, asset := animationCacheFixture(t)
	id, path := slide.Elements[0].AssetID, slide.Elements[0].Path
	before := retroObjectFrames(slide, asset.Frames, 40, 20, 1)
	decoded := loadDecodedStillImage(path)
	key := embeddedImageCacheIdentity(path)
	asset.Frames[0].Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	asset.Frames[0].DelayMS = 230
	registerEmbeddedAnimatedAsset(id, asset)
	if embeddedImageCacheIdentity(path) == key || loadDecodedStillImage(path) == decoded {
		t.Fatal("animation replacement retained stale first frame")
	}
	after := retroObjectFrames(slide, asset.Frames, 40, 20, 1)
	if len(after) == 0 || after[0].DelayMS != 230 || reflect.DeepEqual(before[0].Lines, after[0].Lines) {
		t.Fatal("track retained stale pixels or timing")
	}
	if !reflect.DeepEqual(after, renderRetroObjectFrames(slide, asset.Frames, 40, 20, 1)) {
		t.Fatal("replacement differs from cold track")
	}
}

func TestEmbeddedImageSourceIdentityDependencies(t *testing.T) {
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: []byte("pixels")}
	key := embeddedImageSourceIdentity(asset)
	asset.Source = &DeckAssetSource{Data: []byte("full resolution")}
	if embeddedImageSourceIdentity(asset) != key {
		t.Fatal("unconsumed modern source invalidates Retro cache")
	}
	asset.Data[0] = 'P'
	if embeddedImageSourceIdentity(asset) == key {
		t.Fatal("in-place changed pixels ignored")
	}
	if embeddedImageCacheIdentity("/local/test.png") != "/local/test.png" {
		t.Fatal("non-embedded identity changed")
	}
}
