package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestSampledMaskUsesDisplayedRadius(t *testing.T) {
	for _, aspect := range []float64{.4, 1, 2.5} {
		for _, size := range [][2]int{{80, 80}, {160, 48}, {48, 160}} {
			w, h := size[0], size[1]
			pixels := make([]rgba8, w*h)
			for i := range pixels {
				pixels[i] = rgba8{r: 200, a: 255}
			}
			applySampledImageMaskAspect(pixels, w, h, aspect, "rounded")
			// Independent signed-distance test in a displayed frame 100px high.
			fw, fh := aspect*100, 100.0
			r := math.Min(fw, fh) * .12
			for y := range h {
				for x := range w {
					px, py := (float64(x)+.5)*fw/float64(w), (float64(y)+.5)*fh/float64(h)
					cx, cy := math.Max(r, math.Min(fw-r, px)), math.Max(r, math.Min(fh-r, py))
					inside := math.Hypot(px-cx, py-cy) <= r
					if (pixels[y*w+x].a != 0) != inside {
						t.Fatalf("display radius mismatch: aspect=%g grid=%v at %d,%d", aspect, size, x, y)
					}
				}
			}
		}
	}
}

func TestForegroundFrameMaskRoundTripMatrix(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight })
	fixture := sceneFixture(t)
	var still DeckAsset
	for _, asset := range fixture.Assets {
		still = asset
	}
	_, animation := animationCacheFixture(t)
	vector, w, h, err := sanitizeSVG(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	pixels, err := rasterizeSVG(context.Background(), vector, w, h)
	if err != nil {
		t.Fatal(err)
	}
	svg := DeckAsset{MIME: "image/png", Width: w, Height: h, Data: pixels, Source: &DeckAssetSource{MIME: "image/png", Width: w, Height: h, Data: pixels}, Vector: &DeckAssetVector{Version: 1, MIME: svgMediaType, Width: w, Height: h, Data: vector}}
	type proof struct {
		Name  string     `json:"name"`
		Scene slideScene `json:"scene"`
	}
	var proofs []proof
	for kind, asset := range map[string]DeckAsset{"still": still, "animation": animation, "svg": svg} {
		id := newStableID("frame-mask")
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
		if len(asset.Frames) > 0 {
			registerEmbeddedAnimatedAsset(id, asset)
		} else {
			registerEmbeddedStillAsset(id, asset)
		}
		for _, style := range []string{"modern", "retro"} {
			for _, fit := range []string{"contain", "cover", "stretch"} {
				name := kind + "-" + style + "-" + fit
				deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "frames", BG: "48;2;24;32;42", PageNumber: "hide"}}}
				for i, frame := range [][5]float64{{20.25, 15.5, 55.25, 10.25, 17}, {100.5, 12.25, 31.25, 29.25, -12}, {160.75, 14.75, 55.25, 22.25, 0}} {
					q := url.Values{"image-style": {style}, "image-fit": {fit}, "glyph": {"blocks"}, "modern-mask": {"rounded"}, "modern-crop": {"0.1,0.05,0.1,0.05"}, "brightness": {"1.1"}}
					for j, key := range []string{"left", "top", "width", "height", "object-rotation"} {
						value := frame[j]
						if j < 2 {
							q.Set([]string{"object-offset-x", "object-offset-y"}[j], strconv.FormatFloat(value-math.Floor(value), 'f', -1, 64))
							value = math.Floor(value)
						}
						q.Set(key, strconv.FormatFloat(value, 'f', -1, 64))
					}
					deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprintf("image-%d", i), Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: q.Encode()})
				}
				canonicalizeSlideElementOrder(&deck.Slides[0], 245, 56)
				before := cloneDeck(deck)
				scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
				if err != nil || len(scene.Objects) != 3 {
					t.Fatal(name, err, len(scene.Objects))
				}
				for i, object := range scene.Objects {
					var owner int
					if _, err := fmt.Sscanf(object.ID, "image-%d", &owner); err != nil || owner < 0 || owner > 2 {
						t.Fatal("invalid fixture owner", object.ID)
					}
					frame := [][4]float64{{20.25, 15.5, 55.25, 10.25}, {100.5, 12.25, 31.25, 29.25}, {160.75, 14.75, 55.25, 22.25}}[owner]
					for j, value := range []float64{object.Bounds.X, object.Bounds.Y, object.Bounds.Width, object.Bounds.Height} {
						scale := 1920.0 / 245
						if j%2 == 1 {
							scale = 1080.0 / 56
						}
						if math.Abs(value-frame[j]*scale) > 1e-7 {
							t.Fatal(name, "fractional frame moved or resized", i, j, value)
						}
					}
					if object.Rotation != []float64{17, -12, 0}[owner] {
						t.Fatal(name, "lost rotation", i)
					}
					if object.Media == nil || object.Media.Fit != fit || object.Media.Mask != "rounded" || object.Media.Crop == nil || (object.RetroLines != nil) != (style == "retro") {
						t.Fatal(name, "lost frame treatment", i)
					}
					if style == "retro" && kind == "animation" && (len(object.RetroLines.Frames) != 3 || object.RetroLines.Frames[1].DelayMS != animation.Frames[1].DelayMS) {
						t.Fatal(name, "lost animation", i)
					}
				}
				data, err := serializeDeck("Frames.md", deck)
				if err != nil {
					t.Fatal(name, err)
				}
				loaded, err := parseDeckData("Frames.md", data)
				if err != nil {
					t.Fatal(name, err)
				}
				reopened, err := buildDocumentSlideScene(loaded, 0, 245, 56)
				if err != nil || !reflect.DeepEqual(scene.Objects, reopened.Objects) || !reflect.DeepEqual(before, deck) {
					t.Fatal(name, "projection/round trip changed frame or input", err)
				}
				for _, element := range loaded.Slides[0].Elements {
					q, _ := url.ParseQuery(element.Query)
					if q.Has("keynope-fit-aspect") {
						t.Fatal("render aspect leaked into authored metadata")
					}
				}
				proofs = append(proofs, proof{name, scene})
			}
		}
	}
	if output := os.Getenv("KEYNOPE_FRAME_MASK_EVIDENCE"); output != "" {
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
