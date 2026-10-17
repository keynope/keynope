package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func pptxAnimationFixture(t *testing.T) (string, DeckAsset) {
	t.Helper()
	palette := color.Palette{color.Transparent, color.NRGBA{R: 200, G: 100, B: 50, A: 255}}
	first, second := image.NewPaletted(image.Rect(0, 0, 4, 3), palette), image.NewPaletted(image.Rect(0, 0, 4, 3), palette)
	first.SetColorIndex(0, 0, 1)
	second.SetColorIndex(3, 2, 1)
	first.SetColorIndex(1, 1, 1)
	second.SetColorIndex(2, 1, 1)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{7, 19}, Disposal: []byte{gif.DisposalBackground, gif.DisposalBackground}, LoopCount: 2, Config: image.Config{Width: 4, Height: 3, ColorModel: palette}}); err != nil {
		t.Fatal(err)
	}
	id, asset, _, err := embeddedImageAsset(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return id, asset
}

func TestPPTXAnimationRetainsAdjustedFramesTimingAndTransparentDisposal(t *testing.T) {
	_, asset := pptxAnimationFixture(t)
	media := sceneMediaSource(asset)
	media.ColourMatrices = sceneImageColourMatrices("brightness=1.5&tint=%230000ff")
	media.Sharpness, media.SharpnessAfter = 1.3, 1
	before := cloneDeckForRender(Deck{Assets: map[string]DeckAsset{"source": asset}})
	opacity := .6
	o := pptx.Object{ID: "animation", Image: asset.Data, ImageMIME: "image/png", Width: 300, Height: 200, CropLeft: .1, FillTop: .2, Shape: "ellipse", ImageOpacity: &opacity}
	ok, warnings := exportPPTXAnimatedImage(&o, &media, 1)
	if !ok || o.ImageMIME != "image/gif" || o.CropLeft != .1 || o.FillTop != .2 || o.Width != 300 || o.Shape != "ellipse" || *o.ImageOpacity != .6 {
		t.Fatal("animation/frame lost", ok, warnings)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(o.Image))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != 2 || !reflect.DeepEqual(decoded.Delay, []int{7, 19}) || decoded.LoopCount != 2 {
		t.Fatal("timing/loop lost", decoded.Delay, decoded.LoopCount)
	}
	frames := decodeGIFAnimation(decoded)
	for i, frame := range media.Frames {
		data, mime, _ := dataURI(frame.Source)
		control := pptx.Object{Image: data, ImageMIME: mime}
		if err := bakePPTXImageColour(&control, &media); err != nil {
			t.Fatal(err)
		}
		want, err := png.Decode(bytes.NewReader(control.Image))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 3; y++ {
			for x := 0; x < 4; x++ {
				got, w := color.NRGBAModel.Convert(frames[i].image.At(x, y)).(color.NRGBA), color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
				if got != w {
					t.Fatal("frame adjustment or transparency differs", i, x, y, got, w)
				}
			}
		}
	}
	if _, _, _, alpha := frames[1].image.At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("previous frame leaked into transparent area")
	}
	if p := color.NRGBAModel.Convert(frames[0].image.At(0, 0)).(color.NRGBA); p.R != 0 || p.G != 0 || p.B == 0 {
		t.Fatal("tint not baked in animation", p)
	}
	if !reflect.DeepEqual(before.Assets["source"], asset) {
		t.Fatal("animation export mutated source")
	}
}

func TestPPTXForegroundAndBackgroundAnimationsSurvivePackageReimport(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	id, asset := pptxAnimationFixture(t)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "animated", BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: id, Fit: "contain", Opacity: .6, OpacitySet: true, Query: "brightness=1.5&modern-mask=ellipse"}, Elements: []Element{{ID: "foreground", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: "top=4&left=10&width=100&height=35&image-style=modern&image-fit=contain&modern-mask=rounded&modern-crop=0.1,0,0.1,0&brightness=1.5&modern-opacity=0.4"}}}}}
	before := cloneDeck(d)
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code == "image-animation-frozen" || warning.Code == "image-colour-unavailable" {
			t.Fatal(warning)
		}
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, object := range p.Slides[0].Objects {
		if object.Kind != "image" {
			continue
		}
		images++
		if object.ImageMIME != "image/gif" {
			t.Fatal("package froze animation", object.ImageMIME)
		}
		g, err := gif.DecodeAll(bytes.NewReader(object.Image))
		if err != nil || len(g.Image) != 2 || g.LoopCount != 2 || !reflect.DeepEqual(g.Delay, []int{7, 19}) {
			t.Fatal("package animation lost", err)
		}
		if object.ImageOpacity == nil || object.Shape == "rect" {
			t.Fatal("image mask/opacity lost", object)
		}
	}
	if images != 2 || !reflect.DeepEqual(before, d) {
		t.Fatal("foreground/background missing or source changed", images, p.Slides[0].Objects, report)
	}
	loaded, _, err := importPPTXDocument(data, "Animation.pptx")
	if err != nil {
		t.Fatal(err)
	}
	animations := 0
	for _, asset := range loaded.Assets {
		if len(asset.Frames) > 1 {
			animations++
			if asset.LoopCount != 2 || asset.Frames[0].DelayMS != 70 || asset.Frames[1].DelayMS != 190 {
				t.Fatal("reimport timing lost")
			}
		}
	}
	if animations < 1 {
		t.Fatal("reimport flattened animated assets")
	}
	if root := os.Getenv("KEYNOPE_ANIMATION_EXPORT_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "animation.md"), d); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "animation.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPPTXAnimationFailureLeavesOriginalUntouched(t *testing.T) {
	_, asset := pptxAnimationFixture(t)
	for _, problem := range []string{"width", "pixels", "frames", "sources", "late-source", "delay", "loop", "matrix"} {
		media := sceneMediaSource(asset)
		switch problem {
		case "width":
			media.Width = 0
		case "pixels":
			media.Width, media.Height = 10000, 10000
		case "frames":
			media.Frames = make([]sceneMediaFrame, 10001)
		case "sources":
			media.Frames = make([]sceneMediaFrame, 2000)
			large := strings.Repeat("x", 100000)
			for i := range media.Frames {
				media.Frames[i] = sceneMediaFrame{Source: large, DelayMS: 10}
			}
		case "late-source":
			media.Frames[1].Source = "data:image/png;base64,AAAA"
		case "delay":
			media.Frames[1].DelayMS = 655351
		case "loop":
			media.LoopCount = 65536
		case "matrix":
			media.ColourMatrices = [][]float64{{1}}
		}
		o := pptx.Object{ID: "image", Image: asset.Data, ImageMIME: "image/png", ImageSVG: []byte("original")}
		before := o
		ok, warnings := exportPPTXAnimatedImage(&o, &media, 0)
		if ok || len(warnings) != 1 || warnings[0].Code != "image-animation-frozen" || !reflect.DeepEqual(before, o) {
			t.Fatal("failed export changed source", problem, ok, warnings)
		}
	}
}

func TestPPTXAnimationReportsPaletteAlphaAndTimingLoss(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 8), B: uint8(x ^ y), A: 180})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	uri := sharedSceneSources.uri("image/png", b.Bytes())
	media := sceneMedia{Width: 32, Height: 32, Frames: []sceneMediaFrame{{Source: uri, DelayMS: 17}, {Source: uri, DelayMS: 17}, {Source: uri, DelayMS: 17}}}
	data, warnings, err := encodePPTXAnimation(&media)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, w := range warnings {
		codes[w.Code] = true
	}
	for _, code := range []string{"image-animation-colours-quantized", "image-animation-alpha-approximated", "image-animation-timing-rounded"} {
		if !codes[code] {
			t.Fatal("missing loss warning", code)
		}
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Delay, []int{2, 1, 2}) {
		t.Fatal("timing drift accumulated", g.Delay)
	}
}
