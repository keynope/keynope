package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"
	"time"
)

func TestSceneAnimationFramesKeepProjectedGeometry(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight })
	slide, asset := animationCacheFixture(t)
	slide.Elements[0].Query = "top=0&left=0&width=80&height=40&stretch=1&glyph=blocks&image-fit=contain&keynope-fit-aspect=1.7777777777777777"
	// Initial scene rows and animation frames must use the same design grid,
	// not the retired terminal viewport scaling/pagination path.
	exportImageAnimationMu.Lock()
	previous := exportImageAnimationPosition
	position := time.Duration(0)
	exportImageAnimationPosition = &position
	want := exportLines(layout(slide, 80, 40), slide, 80, 40, 1)
	exportImageAnimationPosition = previous
	exportImageAnimationMu.Unlock()
	got := renderRetroObjectFrames(slide, asset.Frames, 80, 40, 1)
	if len(got) != len(asset.Frames) || !reflect.DeepEqual(got[0].Lines, want) {
		t.Fatal("animation frame was rescaled relative to the scene's sampling grid")
	}
}

func animationCacheFixture(t testing.TB) (Slide, DeckAsset) {
	t.Helper()
	frames := make([]DeckAssetFrame, 3)
	for i := range frames {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(x * 16), uint8(y * 16), uint8(i * 100), 255})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		frames[i] = DeckAssetFrame{Data: data.Bytes(), DelayMS: int64(70 + i*40)}
	}
	asset := DeckAsset{MIME: "image/gif", Width: 16, Height: 16, Frames: frames}
	id := "animation-cache-" + t.Name()
	registerEmbeddedAnimatedAsset(id, asset)
	return Slide{FG: "37", BG: "40", Elements: []Element{{Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: "top=1&left=2&width=12&height=6&stretch=1&glyph=blocks"}}}, asset
}

func TestSceneAnimationCacheParityAndOwnership(t *testing.T) {
	slide, asset := animationCacheFixture(t)
	want := renderRetroObjectFrames(slide, asset.Frames, 40, 20, 1)
	first := retroObjectFrames(slide, asset.Frames, 40, 20, 1)
	got := retroObjectFrames(slide, asset.Frames, 40, 20, 1)
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(got, want) {
		t.Fatal("cached track differs from cold rendering")
	}
	if len(got) == 0 || len(got[0].Lines) == 0 || len(got[0].Lines[0].Parts) == 0 {
		t.Fatal("empty fixture")
	}
	got[0].Lines[0].Parts[0].Text = "mutated"
	got[0].DelayMS = 999
	if again := retroObjectFrames(slide, asset.Frames, 40, 20, 1); !reflect.DeepEqual(again, want) {
		t.Fatal("caller mutated cached frame storage")
	}
	key, ok := sceneAnimationKey(slide, asset.Frames, 40, 20, 1)
	if !ok {
		t.Fatal("fixture not cacheable")
	}
	for _, change := range []func(*Slide){
		func(s *Slide) { s.Elements[0].Query += "&brightness=1.5" },
		func(s *Slide) { s.Elements[0].Query += "&modern-crop=0.1,0.1,0.8,0.8" },
		func(s *Slide) { s.ThemeColors = &themeColors{"#ffffff", "#000000", "#ff0000"} },
		func(s *Slide) { s.BG = "41" },
	} {
		changed := cloneSlide(slide)
		change(&changed)
		other, _ := sceneAnimationKey(changed, asset.Frames, 40, 20, 1)
		if other == key {
			t.Fatal("changed render dependency reused key")
		}
		if !reflect.DeepEqual(retroObjectFrames(changed, asset.Frames, 40, 20, 1), renderRetroObjectFrames(changed, asset.Frames, 40, 20, 1)) {
			t.Fatal("changed track differs from cold rendering")
		}
	}
	asset.Frames[0].DelayMS++
	other, _ := sceneAnimationKey(slide, asset.Frames, 40, 20, 1)
	if other == key {
		t.Fatal("timing omitted from key")
	}
	asset.Frames[0].Data[0] ^= 1
	third, _ := sceneAnimationKey(slide, asset.Frames, 40, 20, 1)
	if third == other {
		t.Fatal("mutated frame bytes omitted from key")
	}
}

func TestSceneAnimationCacheBounded(t *testing.T) {
	var cache sceneAnimationCache
	for i := 0; i < 300; i++ {
		key := [32]byte{byte(i), byte(i >> 8)}
		cache.put(key, []exportContentFrame{{DelayMS: int64(i)}})
	}
	if len(cache.entries) > 128 || cache.bytes > 16<<20 {
		t.Fatal("cache exceeded limits")
	}
	var total int
	for _, data := range cache.entries {
		total += len(data)
	}
	if total != cache.bytes {
		t.Fatal("eviction accounting mismatch")
	}
}

func BenchmarkSceneAnimationTrack(b *testing.B) {
	slide, asset := animationCacheFixture(b)
	for _, cached := range []bool{false, true} {
		name := "cold"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			retroObjectFrames(slide, asset.Frames, 80, 40, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if cached {
					retroObjectFrames(slide, asset.Frames, 80, 40, 1)
				} else {
					renderRetroObjectFrames(slide, asset.Frames, 80, 40, 1)
				}
			}
		})
	}
}
