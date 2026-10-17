package app

import (
	"bytes"
	"context"
	"image/gif"
	"math"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func retroBackgroundDeck(t *testing.T, fit string, animated bool) Deck {
	t.Helper()
	id, asset := pptxAnimationFixture(t)
	if !animated {
		asset.Data = asset.Frames[0].Data
		asset.MIME = "image/png"
		asset.Frames = nil
	}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "background", BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: id, Fit: fit, Opacity: .6, OpacitySet: true, Query: "image-style=retro&glyph=blocks&brightness=1.5&modern-mask=ellipse&modern-crop=0.1,0.05,0,0.1"}, Elements: []Element{{ID: "above", Kind: "text", Text: "Above background", Query: "render=truetype&top=4&left=3&modern-size=40"}}}}}
}

func TestPPTXRetroBackgroundRenderedPackage(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, fit := range []string{"contain", "cover", "stretch"} {
		for _, animated := range []bool{false, true} {
			t.Run(fit+map[bool]string{true: "-animated", false: "-still"}[animated], func(t *testing.T) {
				d := retroBackgroundDeck(t, fit, animated)
				before := cloneDeck(d)
				scene, err := buildDocumentSlideScene(d, 0, 245, 56)
				if err != nil || scene.BackgroundMedia == nil || scene.BackgroundMedia.RetroLines == nil {
					t.Fatal("missing rendered background", err)
				}
				expected, _, err := rasterPPTXRetroImage(pptxRetroBackgroundObject(scene.BackgroundMedia, 0))
				if err != nil {
					t.Fatal(err)
				}
				data, report, err := exportPPTXDocument(d)
				if err != nil {
					t.Fatal(err)
				}
				for _, warning := range report.Warnings {
					if warning.Code == "background-image-treatment-approximated" || warning.Code == "retro-image-capture-unavailable" || warning.Code == "retro-image-animation-frozen" {
						t.Fatal(warning)
					}
				}
				loaded, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if len(loaded.Slides[0].Objects) != 2 || !strings.EqualFold(loaded.Slides[0].Background, scene.Background) {
					t.Fatal("background/base/foreground ordering lost")
				}
				background := loaded.Slides[0].Objects[0]
				if background.Name != "Slide background" || !bytes.Equal(background.Image, expected.Image) || background.ImageMIME != expected.ImageMIME || background.ImageOpacity == nil || math.Abs(*background.ImageOpacity-.6) > .00001 || math.Abs(background.X+64) > .001 || math.Abs(background.Y+64) > .001 || background.Width != 2048 || background.Height != 1208 {
					t.Fatal("captured background substituted, re-treated or misplaced", background.Name, background.ImageMIME, background.X, background.Y, background.Width, background.Height)
				}
				if animated {
					animation, err := gif.DecodeAll(bytes.NewReader(background.Image))
					if err != nil || len(animation.Image) != 2 || animation.LoopCount != 2 || !reflect.DeepEqual(animation.Delay, []int{7, 19}) {
						t.Fatal("rendered background animation lost", err)
					}
				}
				if !reflect.DeepEqual(before, d) {
					t.Fatal("background capture mutated deck")
				}
			})
		}
	}
}

func TestPPTXRetroBackgroundUnsupportedCaptureIsNotSourceSubstitution(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := retroBackgroundDeck(t, "contain", false)
	d.Slides[0].BackgroundMedia.Query = "image-style=retro&glyph=braille"
	presentation, report, err := deckToPPTX(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range presentation.Slides[0].Objects {
		if object.Kind == "image" {
			t.Fatal("unsupported sampled background silently substituted original media")
		}
	}
	noted := false
	for _, warning := range report.Warnings {
		if warning.Code == "retro-image-capture-unavailable" && warning.Object == "keynope-background-image-0" {
			noted = true
		}
	}
	if !noted {
		t.Fatal("background omission had no conversion note", report)
	}
}
