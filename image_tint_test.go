package main

import (
	"image"
	"image/color"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMonochromeImageColour(t *testing.T) {
	tint := rgba8{r: 255, g: 128, b: 64, a: 255}
	for _, tc := range []struct{ in, want rgba8 }{
		{rgba8{255, 255, 255, 255}, rgba8{255, 128, 64, 255}},
		{rgba8{0, 0, 0, 255}, rgba8{0, 0, 0, 255}},
		{rgba8{128, 128, 128, 120}, rgba8{128, 64, 32, 120}},
		{rgba8{255, 0, 0, 0}, rgba8{76, 38, 19, 0}},
	} {
		if got := monochromeImageColour(tc.in, tint); got != tc.want {
			t.Fatalf("%v: got %v want %v", tc.in, got, tc.want)
		}
	}
	gray := monochromeImageColour(rgba8{50, 120, 230, 255}, rgba8{255, 255, 255, 255})
	if gray.r != gray.g || gray.g != gray.b {
		t.Fatalf("white tint is not grayscale: %v", gray)
	}
	if parseImageASCIIOptions("tint=invalid").tint != nil {
		t.Fatal("invalid tint must be ignored")
	}
}

func TestImageTintPreservesGeometryAndSource(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x > 1 && y > 1 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(x * 30), uint8(y * 30), 160, 255})
			}
		}
	}
	original := append([]byte(nil), img.Pix...)
	for _, glyph := range []string{"blocks", "braille", "ascii", "dense"} {
		opts := parseImageASCIIOptions("glyph=" + glyph)
		plain := renderUnicodeImage(img, img.Bounds(), opts, 4, 2, false)
		tinted := renderUnicodeImage(img, img.Bounds(), parseImageASCIIOptions("glyph="+glyph+"&tint=%23ff0000"), 4, 2, false)
		if reflect.DeepEqual(plain, tinted) {
			t.Fatal("tint had no effect")
		}
		for i := range plain {
			if stripANSI(plain[i]) != stripANSI(tinted[i]) {
				t.Fatalf("%s tint changed coverage", glyph)
			}
		}
	}
	if !reflect.DeepEqual(img.Pix, original) {
		t.Fatal("source image modified")
	}
}

func TestImageTintDeckRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "image", Path: "sample.png", Query: "tint=%2355aaff&brightness=1.2"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(loaded.Slides[0].Elements[0].Query)
	if q.Get("tint") != "#55aaff" {
		t.Fatalf("lost tint: %s", q.Encode())
	}
	if !strings.Contains(compactImageSettingsQuery(normalizeImageSettingsQuery(q.Encode())), "tint=") {
		t.Fatal("image settings discarded tint")
	}
}

func TestImageTintAnimationFrames(t *testing.T) {
	frames := []asciiImageFrame{}
	for _, level := range []uint8{255, 128} {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.SetNRGBA(x, y, color.NRGBA{level, level, level, 255})
			}
		}
		frames = append(frames, asciiImageFrame{image: img, delay: time.Duration(level) * time.Millisecond})
	}
	animation := asciiImageAnimation{frames: frames, opts: parseImageASCIIOptions("tint=%23ff0000&shape=alpha"), width: 2, height: 1}
	for i, want := range []string{"38;2;255;0;0", "38;2;128;0;0"} {
		if rows := animation.frameRows(i); len(rows) == 0 || !strings.Contains(rows[0], want) {
			t.Fatalf("frame %d: %q", i, rows)
		}
	}
	if animation.frames[0].delay != 255*time.Millisecond || animation.frames[1].delay != 128*time.Millisecond {
		t.Fatal("frame timing changed")
	}
}
