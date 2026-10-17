package app

import (
	"image"
	"image/color"
	"net/url"
	"path/filepath"
	"testing"
)

func TestFittedImageSampling(t *testing.T) {
	img := image.NewNRGBA(image.Rect(10, 20, 50, 40))
	for y := 20; y < 40; y++ {
		for x := 10; x < 50; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	for _, filter := range []bool{false, true} {
		for _, fit := range []string{"contain", "cover", "stretch"} {
			pixels := sampleFittedImage(img, img.Bounds(), 20, 20, fit, filter)
			if len(pixels) != 400 {
				t.Fatal("wrong frame size")
			}
			for y := 0; y < 20; y++ {
				for x := 0; x < 20; x++ {
					want := uint8(255)
					if fit == "contain" && (y < 5 || y >= 15) {
						want = 0
					}
					if pixels[y*20+x].a != want {
						t.Fatalf("fit %s filter %v at %d,%d: %v", fit, filter, x, y, pixels[y*20+x])
					}
				}
			}
		}
	}
}

func TestImageFitOptionsAndCacheIdentity(t *testing.T) {
	for _, fit := range []string{"contain", "cover", "stretch"} {
		q := "image-fit=" + fit + "&top=10"
		if parseImageASCIIOptions(q).fit != fit || parseImageASCIIOptions(imageRenderQuery(q)).fit != fit {
			t.Fatal("lost fit", fit)
		}
	}
	if parseImageASCIIOptions("image-fit=invalid").fit != "" {
		t.Fatal("legacy default changed")
	}
}

func TestFittedImageUsesDisplayedAspect(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.SetNRGBA(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	// A square sample grid projected into a 2:1 display frame must occupy
	// only its middle half horizontally for a square source to remain square.
	pixels := sampleFittedImageAspect(img, img.Bounds(), 20, 20, 2, "contain", false)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			want := uint8(0)
			if x >= 5 && x < 15 {
				want = 255
			}
			if pixels[y*20+x].a != want {
				t.Fatalf("display aspect lost at %d,%d", x, y)
			}
		}
	}
}

func TestImageFitSceneAndRoundTrip(t *testing.T) {
	for _, fit := range []string{"contain", "cover", "stretch"} {
		d := sceneFixture(t)
		d.Slides[0].Engagement = nil
		d.Slides[0].EngagementResult = nil
		for i, e := range d.Slides[0].Elements {
			if e.ID == "image" {
				d.Slides[0].Elements[i].Query = "image-style=modern&image-fit=" + fit + "&width=60.5&height=20.25&left=10&top=10&modern-crop=0.1,0,0.1,0"
			}
		}
		path := filepath.Join(t.TempDir(), "Fit.md")
		data, err := serializeDeck(path, d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData(path, data)
		if err != nil {
			t.Fatal(err)
		}
		scene, err := buildSlideScene(loaded, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range scene.Objects {
			if o.Kind == "image" && o.Media != nil && o.Media.Fit == fit {
				found = true
				if o.Media.Crop == nil || o.Media.Crop.Left != .1 {
					t.Fatal("fit discarded crop")
				}
			}
		}
		if !found {
			t.Fatal("fit did not survive serialization", fit)
		}
		for _, e := range loaded.Slides[0].Elements {
			q, _ := url.ParseQuery(e.Query)
			if q.Has("keynope-fit-aspect") {
				t.Fatal("render-only aspect leaked into authored state")
			}
		}
	}
}
