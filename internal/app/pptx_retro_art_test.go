package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/gif"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"keynope/internal/pptx"
)

func TestPPTXRetroBlockImageKeepsRenderedPixels(t *testing.T) {
	o := sceneObject{ID: "art", Name: "Sampled image", Kind: "image", Bounds: sceneRect{X: 10, Y: 20, Width: 20, Height: 10}, Rotation: 23, RetroLines: &sceneRetroLines{Cols: 192, Rows: 108, Lines: []exportLine{{Row: 2, Parts: []exportPart{{Col: 1, Text: "▀▄", Color: "#ff0055"}}}}}}
	got, warnings := sceneObjectToPPTX(o)
	if got == nil || got.Kind != "image" || got.ImageMIME != "image/png" {
		t.Fatal("sampled image was omitted", got, warnings)
	}
	img, err := png.Decode(bytes.NewReader(got.Image))
	if err != nil {
		t.Fatal(err)
	}
	if got.X != -54 || got.Y != -44 || got.Width != 148 || got.Height != 138 || got.Rotation != 23 || got.Name != o.Name {
		t.Fatal("sampled picture lost padded geometry/identity", got)
	}
	for _, p := range []struct {
		x, y int
		want color.NRGBA
	}{{69, 66, color.NRGBA{255, 0, 85, 255}}, {69, 72, color.NRGBA{}}, {79, 66, color.NRGBA{}}, {79, 72, color.NRGBA{255, 0, 85, 255}}} {
		if actual := color.NRGBAModel.Convert(img.At(p.x, p.y)).(color.NRGBA); actual != p.want {
			t.Fatal(p, actual)
		}
	}
	found := false
	for _, w := range warnings {
		if w.Code == "retro-image-rasterized" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing raster editability warning", warnings)
	}
}

func TestPPTXRetroPicturePackageUsesArtNotSource(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := denseMixedSceneFixture(t, 5)
	d.Slides[0].Elements[3].Query += "&object-name=Raster+picture&brightness=1.6&modern-opacity=0.4"
	before := cloneDeck(d)
	scene, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var original sceneObject
	for _, o := range scene.Objects {
		if o.ID == d.Slides[0].Elements[3].ID {
			original = o
		}
	}
	expected, _, err := rasterPPTXRetroImage(original)
	if err != nil {
		t.Fatal(err)
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	foundNote := false
	for _, w := range report.Warnings {
		if w.Object == original.ID {
			if w.Code == "retro-image-rasterized" {
				foundNote = true
			}
			if w.Code == "retro-image-capture-unavailable" || w.Code == "retro-artwork-omitted" {
				t.Fatal("rasterized image still omitted", w)
			}
		}
	}
	if !foundNote {
		t.Fatal("missing raster note", report)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range p.Slides[0].Objects {
		if o.Name == "Raster picture" {
			found = true
			if o.Kind != "image" || !bytes.Equal(o.Image, expected.Image) || o.ImageOpacity == nil || math.Abs(*o.ImageOpacity-.4) > .00001 {
				t.Fatal("PPTX changed artwork, double-transformed colour or lost opacity", o.Name, o.Kind, o.ImageOpacity)
			}
		}
	}
	if !found {
		t.Fatal("raster picture missing from valid package")
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("export modified authored data/assets")
	}
}

func retroAnimationFixture() sceneObject {
	red := exportLine{Row: 2, Role: "image", Parts: []exportPart{{Col: 1, Text: "█", Color: "#ff0055"}}}
	blue := exportLine{Row: 2, Role: "image", Parts: []exportPart{{Col: 2, Text: "█", Color: "#0055ff"}}}
	green := blue
	green.Parts = []exportPart{{Col: 2, Text: "█", Color: "#00aa55"}}
	return sceneObject{ID: "animation", Kind: "image", Bounds: sceneRect{X: 10, Y: 20, Width: 20, Height: 10}, RetroLines: &sceneRetroLines{Cols: 192, Rows: 108, LoopCount: 3, Lines: []exportLine{red}, Frames: []exportContentFrame{{Full: true, DelayMS: 17, Lines: []exportLine{red}}, {DelayMS: 23, Clear: []string{"2:1:0:image"}, Update: []exportLine{blue}}, {DelayMS: 70, Update: []exportLine{green, red}}}}}
}

func TestPPTXRetroAnimationKeepsRenderedFramesAndTiming(t *testing.T) {
	a := .4
	o := retroAnimationFixture()
	o.Paint.Opacity = &a
	before, _ := json.Marshal(o)
	got, warnings, err := rasterPPTXRetroImage(o)
	if err != nil || got == nil || got.ImageOpacity == nil || *got.ImageOpacity != a {
		t.Fatal(err, got)
	}
	if got.ImageMIME != "image/gif" {
		t.Fatal("sampled animation frozen", warnings)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(got.Image))
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 3 || animation.LoopCount != 3 || !reflect.DeepEqual(animation.Delay, []int{2, 2, 7}) {
		t.Fatal("frame/timing/loop loss", animation.Delay, animation.LoopCount)
	}
	if color.NRGBAModel.Convert(animation.Image[0].At(69, 69)).(color.NRGBA) != (color.NRGBA{255, 0, 85, 255}) || color.NRGBAModel.Convert(animation.Image[1].At(69, 69)).(color.NRGBA).A != 0 || color.NRGBAModel.Convert(animation.Image[1].At(79, 69)).(color.NRGBA) != (color.NRGBA{0, 85, 255, 255}) || color.NRGBAModel.Convert(animation.Image[2].At(79, 69)).(color.NRGBA) != (color.NRGBA{0, 170, 85, 255}) {
		t.Fatal("delta clear/update frame content lost")
	}
	found, rounded := false, false
	for _, w := range warnings {
		if w.Code == "retro-image-animation-embedded" {
			found = true
		}
		if w.Code == "image-animation-timing-rounded" {
			rounded = true
		}
		if w.Code == "retro-image-animation-frozen" {
			t.Fatal("valid animation frozen", warnings)
		}
	}
	if !found || !rounded {
		t.Fatal("missing GIF/timing notes", warnings)
	}
	after, _ := json.Marshal(o)
	if !bytes.Equal(before, after) {
		t.Fatal("animation capture mutated source")
	}
}

func TestPPTXRetroAnimationFailureKeepsRenderedPNG(t *testing.T) {
	for _, kind := range []string{"budget", "font", "delay", "loop"} {
		o := retroAnimationFixture()
		switch kind {
		case "budget":
			o.Bounds.Width = 4000
			o.Bounds.Height = 2500
		case "font":
			o.RetroLines.Frames[1].Update[0].Parts[0].Text = "⣿"
		case "delay":
			o.RetroLines.Frames[1].DelayMS = 1
		case "loop":
			o.RetroLines.LoopCount = 65536
		}
		got, warnings, err := rasterPPTXRetroImage(o)
		if err != nil || got == nil || got.ImageMIME != "image/png" {
			t.Fatal("failed animation discarded first rendered frame", kind, err)
		}
		found := false
		for _, w := range warnings {
			if w.Code == "retro-image-animation-frozen" {
				found = true
			}
		}
		if !found {
			t.Fatal("failed animation silently frozen", kind, warnings)
		}
	}
}

func TestPPTXRetroAnimationPackageUsesRenderedFrames(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	id, asset := pptxAnimationFixture(t)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "retro-animation", Elements: []Element{{ID: "art", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: "top=4&left=10&width=30&height=10&image-style=retro&glyph=blocks&brightness=1.5&modern-opacity=0.4&object-name=Rendered+animation"}}}}}
	before := cloneDeck(d)
	scene, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var expected *pptx.Object
	for _, object := range scene.Objects {
		if object.ID == "art" {
			if object.RetroLines == nil || len(object.RetroLines.Frames) != 2 {
				t.Fatal("fixture did not exercise sampled frames")
			}
			expected, _, err = rasterPPTXRetroImage(object)
		}
	}
	if err != nil || expected == nil || expected.ImageMIME != "image/gif" {
		t.Fatal("fixture capture failed", err)
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	noted := false
	for _, warning := range report.Warnings {
		if warning.Object == "art" {
			if warning.Code == "retro-image-animation-embedded" {
				noted = true
			}
			if warning.Code == "retro-image-animation-frozen" || warning.Code == "retro-image-capture-unavailable" {
				t.Fatal(warning)
			}
		}
	}
	loaded, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range loaded.Slides[0].Objects {
		if object.Name != "Rendered animation" {
			continue
		}
		found = true
		if object.ImageMIME != "image/gif" || !bytes.Equal(object.Image, expected.Image) || object.ImageOpacity == nil || math.Abs(*object.ImageOpacity-.4) > .00001 {
			t.Fatalf("package substituted source frames, reapplied treatment or lost opacity: mime=%s bytes=%d/%d equal=%v opacity=%v", object.ImageMIME, len(object.Image), len(expected.Image), bytes.Equal(object.Image, expected.Image), object.ImageOpacity)
		}
		animation, err := gif.DecodeAll(bytes.NewReader(object.Image))
		if err != nil || len(animation.Image) != 2 || animation.LoopCount != 2 || !reflect.DeepEqual(animation.Delay, []int{7, 19}) {
			t.Fatal("package lost frame timing", err)
		}
	}
	if !found || !noted || !reflect.DeepEqual(d, before) {
		t.Fatal("missing animation/note or modified authored deck", found, noted)
	}
	// Legacy decks have no concrete-appearance version: the inherited Retro
	// treatment must still match the editor, not silently export original media.
	d.Appearance = nil
	legacyScene, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	legacyExpected, _, err := rasterPPTXRetroImage(legacyScene.Objects[0])
	if err != nil {
		t.Fatal(err)
	}
	legacyPackage, _, err := deckToPPTX(d)
	if err != nil {
		t.Fatal(err)
	}
	legacyFound := false
	for _, object := range legacyPackage.Slides[0].Objects {
		if object.ID == "art" {
			legacyFound = bytes.Equal(object.Image, legacyExpected.Image)
		}
	}
	if !legacyFound {
		t.Fatal("legacy export ignored rendered Retro treatment")
	}
}

func TestPPTXRetroAnimationRowPaintOrder(t *testing.T) {
	o := retroAnimationFixture()
	red := o.RetroLines.Frames[0].Lines[0]
	blue := red
	blue.Element = 1 // Same pixels, distinct row identity, painted over red.
	blue.Parts = []exportPart{{Col: 1, Text: "█", Color: "#0055ff"}}
	o.RetroLines.Frames = []exportContentFrame{
		{Full: true, DelayMS: 100, Lines: []exportLine{red, blue}},
		{DelayMS: 100, Update: []exportLine{red}},                                 // Updating retains order.
		{DelayMS: 100, Clear: []string{"2:1:0:image"}, Update: []exportLine{red}}, // Reinsert goes last.
		{Full: true, DelayMS: 100, Lines: []exportLine{blue}},
	}
	got, _, err := rasterPPTXRetroImage(o)
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(got.Image))
	if err != nil || len(animation.Image) != 4 {
		t.Fatal(err)
	}
	for i, want := range []color.NRGBA{{0, 85, 255, 255}, {0, 85, 255, 255}, {255, 0, 85, 255}, {0, 85, 255, 255}} {
		if actual := color.NRGBAModel.Convert(animation.Image[i].At(69, 69)).(color.NRGBA); actual != want {
			t.Fatal("row insertion order differs", i, actual, want)
		}
	}
}

func TestPPTXRetroRasterBoundsAndUnsupportedGlyphs(t *testing.T) {
	valid := func() sceneObject {
		return sceneObject{Kind: "image", Bounds: sceneRect{X: 20.35, Y: 30.125, Width: 48.375, Height: 29.125}, Rotation: 33, RetroLines: &sceneRetroLines{Cols: 192, Rows: 108, Lines: []exportLine{{Row: 3, Parts: []exportPart{{Col: 2, Text: "█", Color: "#ff0055"}}}}}}
	}
	for _, invalid := range []string{"glyph", "font", "pixels", "non-finite", "scale", "colour", "opacity"} {
		o := valid()
		switch invalid {
		case "glyph":
			o.RetroLines.Lines[0].Parts[0].Text = "⣿"
		case "font":
			o.RetroLines.Lines[0].TrueType = &exportTrueType{}
		case "pixels":
			o.Bounds.Width = 100000
		case "non-finite":
			o.Bounds.X = math.NaN()
		case "scale":
			o.SampleScale = &connectorPoint{X: -1, Y: 1}
		case "colour":
			o.RetroLines.Lines[0].Parts[0].Color = "url(https://invalid)"
		case "opacity":
			x := math.NaN()
			o.RetroLines.ContentOpacity = &x
		}
		if got, _, err := rasterPPTXRetroImage(o); err == nil || got != nil {
			t.Fatal("invalid art accepted", invalid)
		}
	}
	o := valid()
	got, _, err := rasterPPTXRetroImage(o)
	if err != nil {
		t.Fatal(err)
	}
	// The padded bitmap centre must follow rotation about the fractional
	// authored centre. Compare transformed centres in scene coordinates.
	a := o.Rotation * math.Pi / 180
	dx := (got.Width - o.Bounds.Width - 128) / 2
	dy := (got.Height - o.Bounds.Height - 128) / 2
	wantX := o.Bounds.X + o.Bounds.Width/2 + math.Cos(a)*dx - math.Sin(a)*dy
	wantY := o.Bounds.Y + o.Bounds.Height/2 + math.Sin(a)*dx + math.Cos(a)*dy
	if math.Abs(got.X+got.Width/2-wantX) > 1e-9 || math.Abs(got.Y+got.Height/2-wantY) > 1e-9 {
		t.Fatal("rotation centre drift", got)
	}
}

func TestPPTXRetroBrowserFixtures(t *testing.T) {
	root := os.Getenv("KEYNOPE_RETRO_PPTX_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	var fixtures []map[string]interface{}
	for _, name := range []string{"quadrants", "alpha", "fractional", "native-grid"} {
		o := sceneObject{ID: name, Kind: "image", Bounds: sceneRect{X: 20, Y: 30, Width: 160, Height: 20}, RetroLines: &sceneRetroLines{Cols: 192, Rows: 108, Lines: []exportLine{{Row: 3, Parts: []exportPart{{Col: 2, Text: " ▘▝▀▖▌▞▛▗▚▐▜▄▙▟█", Color: "rgb(255,0,85)", Background: "#0055aa"}}}}}}
		if name != "quadrants" {
			a, b := .35, .65
			o.RetroLines.ContentOpacity = &a
			o.RetroLines.BackdropOpacity = &b
		}
		if name == "fractional" {
			o.Bounds = sceneRect{X: 20.35, Y: 30.125, Width: 160.375, Height: 29.125}
			o.SampleScale = &connectorPoint{X: .85, Y: 1.13}
			o.SampleOffset = &connectorPoint{X: .65, Y: -.4}
			o.Rotation = 33
		}
		if name == "native-grid" {
			o.RetroLines.Cols, o.RetroLines.Rows = 245, 56
			cw, ch := 1920.0/245, 1080.0/56
			o.Bounds = sceneRect{X: 2 * cw, Y: 3 * ch, Width: 16 * cw, Height: 2 * ch}
		}
		picture, warnings, err := rasterPPTXRetroImage(o)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, map[string]interface{}{"object": o, "png": base64.StdEncoding.EncodeToString(picture.Image), "warnings": warnings})
	}
	animated := retroAnimationFixture()
	picture, _, err := rasterPPTXRetroImage(animated)
	if err != nil || picture.ImageMIME != "image/gif" {
		t.Fatal("missing GIF proof", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(picture.Image))
	if err != nil {
		t.Fatal(err)
	}
	frames := []map[string]interface{}{}
	for i, frame := range decoded.Image {
		var pngData bytes.Buffer
		if err := png.Encode(&pngData, frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, map[string]interface{}{"png": base64.StdEncoding.EncodeToString(pngData.Bytes()), "time": []int{0, 18, 41}[i]})
	}
	fixtures = append(fixtures, map[string]interface{}{"object": animated, "frames": frames})
	for _, fit := range []string{"contain", "cover", "stretch"} {
		d := retroBackgroundDeck(t, fit, false)
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil || scene.BackgroundMedia == nil {
			t.Fatal("missing background proof", err)
		}
		object := pptxRetroBackgroundObject(scene.BackgroundMedia, 0)
		object.ID = "background-" + fit
		picture, _, err := rasterPPTXRetroImage(object)
		if err != nil {
			t.Fatal(err)
		}
		scene.Objects = []sceneObject{} // Isolate the real background painter, not a foreground adapter.
		fixtures = append(fixtures, map[string]interface{}{"object": object, "scene": scene, "png": base64.StdEncoding.EncodeToString(picture.Image)})
	}
	data, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixtures.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
