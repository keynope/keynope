package app

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"keynope/internal/pptx"
)

func TestPPTXImageReflection(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 128})
	source.SetNRGBA(1, 1, color.NRGBA{0, 0, 255, 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	for _, flips := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		o := pptx.Object{Image: encoded.Bytes(), FlipH: flips[0], FlipV: flips[1], CropLeft: .1, CropRight: .2, CropTop: .3, CropBottom: .4, FillLeft: .15, FillRight: .25, Width: 20, Height: 20, Rotation: 35}
		if err := reflectPPTXImage(&o); err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(o.Image))
		if err != nil {
			t.Fatal(err)
		}
		x, y := 0, 0
		if flips[0] {
			x = 1
		}
		if flips[1] {
			y = 1
		}
		if color.NRGBAModel.Convert(got.At(x, y)) != source.NRGBAAt(0, 0) {
			t.Fatal("pixel reflection lost colour/alpha")
		}
		if flips[0] && (o.CropLeft != .2 || o.FillLeft != .25) {
			t.Fatal("horizontal insets lost")
		}
		if flips[1] && o.CropTop != .4 {
			t.Fatal("vertical inset lost")
		}
		if o.FlipH || o.FlipV || o.Rotation != 35 || o.Width != 20 {
			t.Fatal("frame mutated", o)
		}
	}
}

func TestPPTXGIFReflectionKeepsTimingAndDisposal(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 4, 3), palette)
	second := image.NewPaletted(image.Rect(0, 1, 1, 2), palette)
	second.SetColorIndex(0, 1, 1)
	a := &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{5, 17}, Disposal: []byte{gif.DisposalNone, gif.DisposalBackground}, LoopCount: 3, Config: image.Config{ColorModel: palette, Width: 4, Height: 3}}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, a); err != nil {
		t.Fatal(err)
	}
	oversized := append([]byte(nil), encoded.Bytes()...)
	descriptor := bytes.IndexByte(oversized, 0x2c)
	if descriptor < 0 {
		t.Fatal("missing frame")
	}
	for j := descriptor + 5; j < descriptor+9; j++ {
		oversized[j] = 255
	}
	if validatePPTXReflectionGIF(oversized) == nil {
		t.Fatal("oversized frame accepted before decode")
	}
	if validatePPTXReflectionGIF(encoded.Bytes()[:len(encoded.Bytes())-2]) == nil {
		t.Fatal("truncated GIF accepted")
	}
	o := pptx.Object{Image: encoded.Bytes(), FlipH: true}
	if err := reflectPPTXImage(&o); err != nil {
		t.Fatal(err)
	}
	got, err := gif.DecodeAll(bytes.NewReader(o.Image))
	if err != nil {
		t.Fatal(err)
	}
	if got.Image[1].Bounds() != image.Rect(3, 1, 4, 2) || got.Image[1].ColorIndexAt(3, 1) != 1 || got.Delay[1] != 17 || got.LoopCount != 3 || got.Disposal[1] != gif.DisposalBackground {
		t.Fatal("animation changed", got)
	}
}
