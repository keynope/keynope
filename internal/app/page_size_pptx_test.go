package app

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"keynope/internal/pptx"
)

func TestPageSizePPTXImportBoundsRoundingAndAttachedDiagram(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		d := connectedPPTXDeck(t)
		d.PageSize = size.Clone()
		data, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, report, err := importPPTXDocument(data, "Attached.pptx")
		if err != nil {
			t.Fatal(err)
		}
		for slideIndex, slide := range loaded.Slides {
			found := false
			for _, e := range slide.Elements {
				if e.Kind != "connector" {
					continue
				}
				found = true
				q, _ := url.ParseQuery(e.Query)
				if q.Get("connector-from") == "" || q.Get("connector-to") == "" {
					t.Fatal("lost diagram attachment")
				}
			}
			if !found {
				t.Fatal("custom-aspect diagram became unattached artwork", slideIndex, report)
			}
		}
	}
	for _, page := range []pptx.Presentation{{Width: 0, Height: 6858000}, {Width: 6350 * 100, Height: 6350 * 100}, {Width: 6350 * 7680, Height: 6350 * 7680}} {
		page.Slides = []pptx.Slide{{}}
		if _, _, err := importPPTXPresentation(page, pptx.Report{}); err == nil {
			t.Fatal("out-of-policy page size accepted")
		}
	}
	p := pptx.Presentation{Width: 12192001, Height: 6858000, Slides: []pptx.Slide{{Objects: []pptx.Object{{Kind: "text", Text: "Outside", X: -12.5, Y: -3.25, Width: 80, Height: 40, FontSize: 24}}}}}
	d, report, err := importPPTXPresentation(p, pptx.Report{})
	if err != nil {
		t.Fatal(err)
	}
	rounded := false
	for _, warning := range report.Warnings {
		if warning.Code == "page-size-rounded" {
			rounded = warning.Fallback == "partial-conversion"
		}
	}
	if !rounded {
		t.Fatal("subpixel page rounding was not diagnosed", report)
	}
	scene, err := buildDocumentSlideScene(d, 0, 245, 56)
	if err != nil || len(scene.Objects) != 1 || math.Abs(scene.Objects[0].Bounds.X+12.5) > .001 || math.Abs(scene.Objects[0].Bounds.Y+3.25) > .001 {
		t.Fatal("off-canvas origin was clamped", err, scene.Objects, d.Slides[0].Elements)
	}
}

func TestPageSizePPTXPackageDimensionsAndEditableObjects(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		d := structuredFixture()
		d.PageSize = size.Clone()
		d.Appearance = &DeckAppearance{Version: 3}
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "body", Kind: "text", Text: "Physical size", Query: "render=truetype&modern-font=sans&modern-size=48&width=100&height=10&text-box=1&top=3&left=5"})
		before := cloneDeck(d)
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Width != int64(size.Width)*6350 || loaded.Height != int64(size.Height)*6350 {
			t.Fatal("PPTX lost physical canvas", loaded.Width, loaded.Height, size)
		}
		if len(loaded.Slides[0].Objects) != len(scene.Objects) {
			t.Fatal("objects lost")
		}
		for i, object := range loaded.Slides[0].Objects {
			bounds := scene.Objects[i].Bounds
			for _, pair := range [][2]float64{{object.X, bounds.X}, {object.Y, bounds.Y}, {object.Width, bounds.Width}, {object.Height, bounds.Height}} {
				if math.Abs(pair[0]-pair[1]) > 1.0/6350 {
					t.Fatal("PPTX rescaled physical geometry", pair)
				}
			}
			if object.Kind == "text" && object.FontSize != 24 {
				t.Fatal("page size rescaled font", object.FontSize)
			}
		}
		if !reflect.DeepEqual(d, before) {
			t.Fatal("export changed authored deck")
		}
		imported, _, err := importPPTXDocument(data, "Physical.pptx")
		if err != nil {
			t.Fatal(err)
		}
		actualWidth, actualHeight := imported.pageDimensions()
		if actualWidth != float64(size.Width) || actualHeight != float64(size.Height) {
			t.Fatal("application import letterboxed source page")
		}
		reprojected, err := buildDocumentSlideScene(imported, 0, 245, 56)
		if err != nil || len(reprojected.Objects) != len(scene.Objects) {
			t.Fatal("application roundtrip lost objects", err)
		}
		for i, object := range reprojected.Objects {
			original := scene.Objects[i]
			for _, pair := range [][2]float64{{object.Bounds.X, original.Bounds.X}, {object.Bounds.Y, original.Bounds.Y}, {object.Bounds.Width, original.Bounds.Width}, {object.Bounds.Height, original.Bounds.Height}} {
				if math.Abs(pair[0]-pair[1]) > .001 {
					t.Fatal("application import rescaled geometry", object.Kind, pair)
				}
			}
			if object.Text != nil && (object.Text.Size != original.Text.Size || object.Text.WidthScale != original.Text.WidthScale) {
				t.Fatal("import rescaled physical font")
			}
		}
	}
}

func TestPageSizePPTXBackgroundAndCaptureDimensions(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		d := retroBackgroundDeck(t, "contain", false)
		d.PageSize = size.Clone()
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		object := pptxImageBackgroundObject(scene.BackgroundMedia, 0, sceneRect{Width: scene.Width, Height: scene.Height})
		expected, _, err := rasterPPTXRetroImage(object)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
		if err != nil {
			t.Fatal(err)
		}
		picture := loaded.Slides[0].Objects[0]
		if picture.Width != float64(size.Width+128) || picture.Height != float64(size.Height+128) || !bytes.Equal(picture.Image, expected.Image) {
			t.Fatal("background uses old canvas")
		}
		d.Slides[0].BackgroundMedia.Query = "image-style=retro&glyph=braille"
		items, err := buildPPTXCaptureItems(context.Background(), d)
		if err != nil || len(items) != 1 || items[0].Width != float64(size.Width) || items[0].Height != float64(size.Height) || items[0].Object.Bounds.Width != float64(size.Width) {
			t.Fatal("capture manifest uses old canvas", err, items)
		}
		if output := os.Getenv("KEYNOPE_PAGE_SIZE_PPTX_PROOF"); output != "" && size.Width == 1080 {
			if err := os.MkdirAll(output, 0700); err != nil {
				t.Fatal(err)
			}
			if err := saveDeck(filepath.Join(output, "portrait.md"), d); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPageSizeRetroRasterUsesPhysicalGrid(t *testing.T) {
	object := sceneObject{ID: "physical", Kind: "image", Bounds: sceneRect{X: 10, Y: 20, Width: 10, Height: 20}, RetroLines: &sceneRetroLines{CanvasWidth: 960, CanvasHeight: 2160, Cols: 192, Rows: 108, Lines: []exportLine{{Row: 1, Parts: []exportPart{{Col: 2, Text: "▀▄", Color: "#ff0055"}}}}}}
	picture, _, err := rasterPPTXRetroImage(object)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(picture.Image))
	if err != nil {
		t.Fatal(err)
	}
	ink := color.NRGBA{255, 0, 85, 255}
	for _, point := range []struct {
		x, y int
		want color.NRGBA
	}{{66, 68, ink}, {66, 79, color.NRGBA{}}, {71, 68, color.NRGBA{}}, {71, 79, ink}} {
		if actual := color.NRGBAModel.Convert(img.At(point.x, point.y)).(color.NRGBA); actual != point.want {
			t.Fatal("sampled pixel uses widescreen cell dimensions", point, actual)
		}
	}
	object.RetroLines.CanvasHeight = 0
	if _, _, err := rasterPPTXRetroImage(object); err == nil {
		t.Fatal("partial physical canvas accepted")
	}
}
