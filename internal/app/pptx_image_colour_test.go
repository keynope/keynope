package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"keynope/internal/pptx"
)

func BenchmarkPPTXSharpenedImage(b *testing.B) {
	source := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			source.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		b.Fatal(err)
	}
	media := &sceneMedia{Sharpness: 1.6, SharpnessAfter: 1, ColourMatrices: sceneImageColourMatrices("brightness=1.3&tint=%2355aaff")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := pptx.Object{Image: encoded.Bytes()}
		if err := bakePPTXImageColour(&o, media); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPPTXPixelRowCacheBoundsAndReuse(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {7, 2}, {8, 15}} {
		width, height := size[0], size[1]
		calls := 0
		source := func(x, y int) [4]float64 { calls++; return [4]float64{float64(x), float64(y), .5, 1} }
		cached := cachedPPTXPixelRows(width, height, source)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						got := cached(x+dx, y+dy)
						want := [4]float64{float64(max(0, min(width-1, x+dx))), float64(max(0, min(height-1, y+dy))), .5, 1}
						if got != want {
							t.Fatal("cached pixel differs", got, want)
						}
					}
				}
			}
		}
		if calls != width*height {
			t.Fatalf("source evaluated %d times, want %d", calls, width*height)
		}
	}
	calls := 0
	f := cachedPPTXPixelRows(1<<20, 3, func(x, y int) [4]float64 { calls++; return [4]float64{float64(x)} })
	f(5, 0)
	if calls != 1 {
		t.Fatal("wide source allocated and filled unbounded row")
	}
}

func TestPPTXSharpeningAndBlur(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	for x, value := range []uint8{50, 100, 150} {
		source.SetNRGBA(x, 0, color.NRGBA{value, value, value, 128})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sharp float64
		want  []uint8
	}{{2, []uint8{33, 100, 167}}, {.2, []uint8{63, 100, 137}}, {1, []uint8{50, 100, 150}}} {
		o := pptx.Object{Image: append([]byte(nil), encoded.Bytes()...)}
		if err := bakePPTXImageColour(&o, &sceneMedia{Sharpness: tc.sharp}); err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(o.Image))
		if err != nil {
			t.Fatal(err)
		}
		for x, want := range tc.want {
			p := color.NRGBAModel.Convert(got.At(x, 0)).(color.NRGBA)
			if p != (color.NRGBA{want, want, want, 128}) {
				t.Fatalf("sharpness %v pixel %d: %v", tc.sharp, x, p)
			}
		}
	}
	cache := &pptxImageColourCache{}
	render := func(sharp float64, after int) []byte {
		t.Helper()
		o := pptx.Object{Image: append([]byte(nil), encoded.Bytes()...)}
		warnings := exportPPTXImageColour(&o, &sceneMedia{Sharpness: sharp, SharpnessAfter: after, ColourMatrices: sceneImageColourMatrices("brightness=2")}, 0, cache)
		if len(warnings) != 1 || warnings[0].Code != "image-colour-baked" {
			t.Fatal(warnings)
		}
		return o.Image
	}
	a, b, c := render(2, 0), render(2, 1), render(.2, 1)
	if bytes.Equal(a, b) || bytes.Equal(b, c) {
		t.Fatal("sharpness order or strength was lost")
	}
	if !bytes.Equal(a, render(2, 0)) || len(cache.images) != 3 {
		t.Fatal("treatment cache identity incorrect")
	}
}

func TestPPTXColourEndToEndSVGAndBackground(t *testing.T) {
	id, asset, path, err := embeddedSVGAsset(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#808080"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), asset.Source.Data...)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: pageNumberHide, BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: id, Fit: "contain", Opacity: .6, Query: "image-style=modern&tint=%2300ff00&modern-mask=ellipse"}, Elements: []Element{{ID: "picture", Kind: "image", AssetID: id, Path: path, Query: "image-style=modern&left=2&top=2&width=20&height=12&stretch=1&image-fit=contain&tint=%23ff0000&modern-mask=rounded&modern-opacity=0.4"}}}}}
	encoded, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	baked := 0
	for _, warning := range report.Warnings {
		if warning.Code == "image-colour-baked" {
			baked++
		}
		if warning.Code == "image-colour-unavailable" {
			t.Fatal(warning)
		}
	}
	if baked != 2 {
		t.Fatalf("expected two treated images: %v", report.Warnings)
	}
	presentation, _, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, object := range presentation.Slides[0].Objects {
		if object.Kind != "image" {
			continue
		}
		images++
		if len(object.ImageSVG) != 0 {
			t.Fatal("untreated vector overrides raster treatment")
		}
		decoded, err := png.Decode(bytes.NewReader(object.Image))
		if err != nil {
			t.Fatal(err)
		}
		pixel := color.NRGBAModel.Convert(decoded.At(20, 10)).(color.NRGBA)
		if images == 1 {
			if pixel.R != 0 || pixel.G < 120 || pixel.B != 0 || object.Shape != "ellipse" || object.ImageOpacity == nil || *object.ImageOpacity != .6 {
				t.Fatal("background treatment lost", pixel, object.Shape, object.ImageOpacity)
			}
		} else if pixel.R < 120 || pixel.G != 0 || pixel.B != 0 || object.Shape != "roundRect" || object.ImageOpacity == nil || *object.ImageOpacity != .4 {
			t.Fatal("foreground treatment lost", pixel, object.Shape, object.ImageOpacity)
		}
	}
	if images != 2 {
		t.Fatal("pictures missing", images)
	}
	if !bytes.Equal(deck.Assets[id].Source.Data, original) {
		t.Fatal("export mutated deck source")
	}
}

func TestPPTXColourBakingPreservesGeometryAndSource(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	source.SetNRGBA(1, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), encoded.Bytes()...)
	opacity := .5
	o := pptx.Object{Image: encoded.Bytes(), ImageMIME: "image/png", ImageSVG: []byte("vector"), Width: 200, Height: 100, CropLeft: .1, FillTop: .2, Shape: "ellipse", ImageOpacity: &opacity}
	media := &sceneMedia{ColourMatrices: sceneImageColourMatrices("brightness=2&tint=%23ff0000")}
	if err := bakePPTXImageColour(&o, media); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.Bytes(), original) || bytes.Equal(o.Image, original) {
		t.Fatal("source mutation or no adjustment")
	}
	if o.CropLeft != .1 || o.FillTop != .2 || o.Width != 200 || o.Height != 100 || o.Shape != "ellipse" || *o.ImageOpacity != .5 || o.ImageSVG != nil {
		t.Fatal("geometry/alpha or raster fallback lost")
	}
	decoded, err := png.Decode(bytes.NewReader(o.Image))
	if err != nil {
		t.Fatal(err)
	}
	pixel := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	// First pass clamps red to 255 before tint luminance is evaluated.
	want := uint8(math.Round(.299*255 + .587*200 + .114*100))
	if pixel != (color.NRGBA{R: want, A: 128}) {
		t.Fatalf("ordered sRGB passes: got %v want red %d", pixel, want)
	}
}

func TestPPTXColourBakingFailureIsAtomic(t *testing.T) {
	for _, matrices := range [][][]float64{{{1}}, {{math.NaN(), 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0}}, sceneImageColourMatrices("brightness=2")} {
		o := pptx.Object{Image: []byte("invalid source"), ImageMIME: "image/png", ImageSVG: []byte("svg")}
		if err := bakePPTXImageColour(&o, &sceneMedia{ColourMatrices: matrices}); err == nil {
			t.Fatal("expected refusal")
		}
		if string(o.Image) != "invalid source" || string(o.ImageSVG) != "svg" {
			t.Fatal("partial mutation")
		}
	}
}

func TestPPTXColourCacheReusesPixelsNotGeometry(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	cache := &pptxImageColourCache{}
	media := &sceneMedia{ColourMatrices: sceneImageColourMatrices("brightness=1.5")}
	a := pptx.Object{Image: source.Bytes(), Width: 100}
	b := pptx.Object{Image: source.Bytes(), Width: 200, Shape: "ellipse", CropLeft: .1}
	for _, object := range []*pptx.Object{&a, &b} {
		warnings := exportPPTXImageColour(object, media, 0, cache)
		if len(warnings) != 1 || warnings[0].Code != "image-colour-baked" {
			t.Fatal(warnings)
		}
	}
	if len(cache.images) != 1 || &a.Image[0] != &b.Image[0] || b.Width != 200 || b.Shape != "ellipse" || b.CropLeft != .1 {
		t.Fatal("cache did not reuse pixels independently of geometry")
	}
	media.ColourMatrices = sceneImageColourMatrices("brightness=.5")
	c := pptx.Object{Image: source.Bytes()}
	exportPPTXImageColour(&c, media, 0, cache)
	if len(cache.images) != 2 {
		t.Fatal("different treatment reused stale pixels")
	}
	cache.bytes = 64 << 20
	d := pptx.Object{Image: source.Bytes()}
	media.ColourMatrices = sceneImageColourMatrices("brightness=.7")
	exportPPTXImageColour(&d, media, 0, cache)
	if len(cache.images) != 2 {
		t.Fatal("cache exceeded byte budget")
	}
}
