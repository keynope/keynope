package main

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestRetroCropUsesSourceFrame(t *testing.T) {
	source := image.NewNRGBA(image.Rect(10, 20, 50, 60))
	for y := 20; y < 60; y++ {
		for x := 10; x < 50; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 30 {
				c = color.NRGBA{B: 255, A: 255}
			}
			source.SetNRGBA(x, y, c)
		}
	}
	before := append([]byte(nil), source.Pix...)
	opts := parseImageASCIIOptions("modern-crop=0.5,0,0,0&stretch=1")
	wantOpts := opts
	wantOpts.crop = nil
	for _, quality := range []bool{false, true} {
		got := renderUnicodeImage(source, source.Bounds(), opts, 10, 10, quality)
		want := renderUnicodeImage(source, image.Rect(30, 20, 50, 60), wantOpts, 10, 10, quality)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("crop differs, quality=%v", quality)
		}
	}
	if !reflect.DeepEqual(before, source.Pix) {
		t.Fatal("source mutated")
	}
	if imageRenderQuery("modern-mask=ellipse&left=2") == imageRenderQuery("modern-mask=rounded&left=2") {
		t.Fatal("mask missing from cache key")
	}
}

func TestRetroMasksBeforeGlyphSampling(t *testing.T) {
	for _, mask := range []string{"ellipse", "rounded"} {
		pixels := make([]rgba8, 40*40)
		for i := range pixels {
			pixels[i] = rgba8{r: 200, a: 255}
		}
		applySampledImageMask(pixels, 40, 40, mask)
		if pixels[0].a != 0 || pixels[20*40+20].a != 255 {
			t.Fatalf("bad %s mask", mask)
		}
		for y := 0; y < 40; y++ {
			for x := 0; x < 40; x++ {
				if pixels[y*40+x] != pixels[y*40+39-x] || pixels[y*40+x] != pixels[(39-y)*40+x] {
					t.Fatalf("asymmetric %s mask", mask)
				}
			}
		}
	}
}
