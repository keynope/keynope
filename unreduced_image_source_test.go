package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func TestUnreducedImportsRetainOwnedOriginalSource(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	img.SetNRGBA(4, 4, color.NRGBA{R: 255, A: 255})
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	_, asset, _, err := embeddedImageAsset(pngBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if asset.Source == nil || asset.Source.Width != 12 || asset.Source.Height != 8 || !bytes.Equal(asset.Source.Data, pngBytes.Bytes()) {
		t.Fatal("unreduced PNG lacks exact original source")
	}
	original := append([]byte(nil), asset.Data...)
	asset.Source.Data[0] ^= 0xff
	if !bytes.Equal(asset.Data, original) || pngBytes.Bytes()[0] != 0x89 {
		t.Fatal("source aliases derivative or caller")
	}
	palette := color.Palette{color.Transparent, color.White}
	frames := []*image.Paletted{image.NewPaletted(img.Rect, palette), image.NewPaletted(img.Rect, palette)}
	frames[0].SetColorIndex(1, 1, 1)
	frames[1].SetColorIndex(2, 2, 1)
	var gifBytes bytes.Buffer
	if err := gif.EncodeAll(&gifBytes, &gif.GIF{Image: frames, Delay: []int{12, 24}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	firstID, animation, _, err := embeddedImageAsset(gifBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if animation.Source == nil || len(animation.Source.Frames) != 2 || len(animation.Frames) != 2 {
		t.Fatal("unreduced GIF lacks original frames")
	}
	for i, frame := range animation.Source.Frames {
		if !bytes.Equal(frame.Data, animation.Frames[i].Data) || frame.DelayMS != animation.Frames[i].DelayMS {
			t.Fatal("source changed GIF pixels/timing")
		}
	}
	saved := append([]byte(nil), animation.Frames[0].Data...)
	animation.Source.Frames[0].Data[0] ^= 0xff
	if !bytes.Equal(animation.Frames[0].Data, saved) {
		t.Fatal("source frame aliases derivative")
	}
	secondID, _, _, err := embeddedImageAsset(gifBytes.Bytes())
	if err != nil || firstID != secondID {
		t.Fatal("identical unreduced GIF imports have unstable identity", err)
	}
}
