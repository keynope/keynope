package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackgroundFitMediaRoundTripMatrix(t *testing.T) {
	_, animation := animationCacheFixture(t)
	animation.LoopCount = 2
	still := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, G: 100, A: 255})}
	vector, width, height, err := sanitizeSVG(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	pixels, err := rasterizeSVG(context.Background(), vector, width, height)
	if err != nil {
		t.Fatal(err)
	}
	svg := DeckAsset{MIME: "image/png", Width: width, Height: height, Data: pixels,
		Source: &DeckAssetSource{MIME: "image/png", Width: width, Height: height, Data: pixels},
		Vector: &DeckAssetVector{Version: 1, MIME: svgMediaType, Width: width, Height: height, Data: vector}}
	type proof struct {
		Name  string     `json:"name"`
		Scene slideScene `json:"scene"`
	}
	var proofs []proof
	for kind, asset := range map[string]DeckAsset{"still": still, "animation": animation, "svg": svg} {
		id := newStableID("background-matrix")
		t.Cleanup(func() {
			os.Remove(embeddedAssetPath(id, asset))
			embeddedStillAssets.Lock()
			delete(embeddedStillAssets.assets, id)
			delete(embeddedStillAssets.identities, id)
			embeddedStillAssets.Unlock()
			embeddedAnimatedAssets.Lock()
			delete(embeddedAnimatedAssets.assets, id)
			delete(embeddedAnimatedAssets.identities, id)
			embeddedAnimatedAssets.Unlock()
		})
		for _, style := range []string{"modern", "retro"} {
			for _, fit := range []string{"contain", "cover", "stretch"} {
				name := kind + "-" + style + "-" + fit
				deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "background-page", BackgroundMediaSet: true,
					BackgroundMedia: &slideBackgroundMedia{AssetID: id, Fit: fit, Opacity: .8, OpacitySet: true, Query: "image-style=" + style + "&glyph=blocks&modern-mask=ellipse&modern-crop=0.1,0.05,0,0.1&brightness=1.2"},
					Elements:        []Element{{ID: "title", Kind: "text", Text: "Background fit", Query: "render=truetype&top=1&left=2&modern-size=32"}}}}}
				before := cloneDeck(deck)
				scene, err := buildDocumentSlideScene(deck, 0, 80, 40)
				if err != nil || scene.BackgroundMedia == nil {
					t.Fatal(name, "missing source", err)
				}
				bg := scene.BackgroundMedia
				if bg.Fit != fit || bg.Mask != "ellipse" || bg.Crop == nil || bg.Opacity != .8 || (bg.RetroLines != nil) != (style == "retro") {
					t.Fatal(name, "lost treatment")
				}
				if style == "retro" && kind == "animation" {
					if len(bg.RetroLines.Frames) != 3 || bg.RetroLines.LoopCount != 2 {
						t.Fatal(name, "animation missing")
					}
					for i, frame := range bg.RetroLines.Frames {
						if frame.DelayMS != animation.Frames[i].DelayMS {
							t.Fatal(name, "animation timing changed")
						}
					}
				}
				data, err := serializeDeck("Background.md", deck)
				if err != nil {
					t.Fatal(name, err)
				}
				loaded, err := parseDeckData("Background.md", data)
				if err != nil {
					t.Fatal(name, err)
				}
				reopened, err := buildDocumentSlideScene(loaded, 0, 80, 40)
				if err != nil || !reflect.DeepEqual(bg, reopened.BackgroundMedia) || !reflect.DeepEqual(before, deck) {
					t.Fatal(name, "round trip changed background or mutated input", err)
				}
				proofs = append(proofs, proof{name, scene})
			}
		}
	}
	if output := os.Getenv("KEYNOPE_BACKGROUND_FIT_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(proofs)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "scenes.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetroBackgroundUsesSlideFitAndFrameWithoutDiskCache(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 48; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 5), G: uint8(y * 20), B: 180, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	asset := DeckAsset{MIME: "image/png", Width: 48, Height: 12, Data: encoded.Bytes()}
	id := newStableID("background-fit")
	t.Cleanup(func() {
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	deck := Deck{Assets: map[string]DeckAsset{id: asset}}
	var results [][]exportLine
	for _, fit := range []string{"contain", "cover", "stretch"} {
		slide := Slide{BackgroundMedia: &slideBackgroundMedia{AssetID: id, Fit: fit}}
		query := "image-style=retro&glyph=blocks&modern-crop=0.1,0,0,0.1&modern-mask=rounded&brightness=1.2"
		got := sceneBackgroundRetroLines(deck, slide, id, asset, query, 40, 20)
		values, _ := url.ParseQuery(query)
		values.Set("image-fit", fit)
		values.Set("keynope-fit-aspect", "1.7777777777777777")
		values.Set("left", "0")
		values.Set("top", "0")
		values.Set("width", "40")
		values.Set("height", "20")
		values.Set("stretch", "1")
		isolate := slide
		isolate.ThemeColors = deck.themeColors("retro")
		isolate.Elements = []Element{{Kind: "image", AssetID: id, Path: "keynope-asset:" + id + ".png", Query: values.Encode()}}
		want := exportLines(layout(isolate, 40, 20), isolate, 40, 20, 0)
		if got == nil || !reflect.DeepEqual(got.Lines, want) {
			t.Fatalf("%s background differs from fitted in-memory foreground sampling", fit)
		}
		// Foreground framing metadata must not move or resize a slide background.
		dirty := query + "&left_pct=0.3&right=2&bottom=5&align=right&row_delta=3&object-offset-x=0.5&object-offset-y=0.5&scale=0.3&image-fit=legacy"
		if changed := sceneBackgroundRetroLines(deck, slide, id, asset, dirty, 40, 20); !reflect.DeepEqual(changed, got) {
			t.Fatalf("%s background retained old foreground placement", fit)
		}
		results = append(results, got.Lines)
	}
	if reflect.DeepEqual(results[0], results[1]) || reflect.DeepEqual(results[1], results[2]) {
		t.Fatal("fit choices did not affect background pixels")
	}
}
