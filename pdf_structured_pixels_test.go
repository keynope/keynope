package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func readStructuredPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// A WKWebView snapshot is in backing pixels, whereas the structured fixture
// and PDF readers use 1920x1080 logical points. Keep the original PNG intact
// and only normalize the documented integral Retina scale. Unexpected sizes
// must fail rather than silently stretching a broken capture to fit.
func structuredLogicalReference(img image.Image) (image.Image, error) {
	logical := image.Rect(0, 0, 1920, 1080)
	if img.Bounds() == logical {
		return img, nil
	}
	if img.Bounds() != image.Rect(0, 0, 3840, 2160) {
		return nil, fmt.Errorf("unexpected structured snapshot bounds: %v", img.Bounds())
	}
	normalized := image.NewNRGBA(logical)
	xdraw.ApproxBiLinear.Scale(normalized, logical, img, img.Bounds(), xdraw.Src, nil)
	return normalized, nil
}

func readStructuredReferencePNG(t *testing.T, path string) image.Image {
	t.Helper()
	img, err := structuredLogicalReference(readStructuredPNG(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestStructuredLogicalReference(t *testing.T) {
	logical := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
	if actual, err := structuredLogicalReference(logical); err != nil || actual != logical {
		t.Fatal("1x reference was changed", err)
	}
	retina := image.NewNRGBA(image.Rect(0, 0, 3840, 2160))
	for y := 400; y < 800; y++ {
		for x := 600; x < 1000; x++ {
			retina.SetNRGBA(x, y, color.NRGBA{30, 70, 180, 255})
		}
	}
	actual, err := structuredLogicalReference(retina)
	if err != nil || actual.Bounds() != logical.Bounds() {
		t.Fatal("2x reference was not normalized", err)
	}
	if actual.At(350, 250) != (color.NRGBA{30, 70, 180, 255}) || actual.At(250, 250) != (color.NRGBA{}) {
		t.Fatal("backing-scale normalization changed the logical region")
	}
	for _, bounds := range []image.Rectangle{image.Rect(0, 0, 1921, 1080), image.Rect(0, 0, 3840, 1080), image.Rect(1, 1, 1921, 1081)} {
		if _, err := structuredLogicalReference(image.NewNRGBA(bounds)); err == nil {
			t.Fatal("accepted an invalid scale/frame", bounds)
		}
	}
}

// Extraction can see text that a PDF reader does not paint. Count foreground
// ink inside each expected chart caption, independently of full-page AA noise.
func TestStructuredPDFCaptionPixelDiagnostic(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_PDF_PIXELS")
	if root == "" {
		t.Skip("PDF renders not requested")
	}
	d, _ := structuredPDFDeck(t)
	document, err := makePDFExportDocument("Structured.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for i, page := range document.Manifest.Pages {
		quality := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-quality-page-%02d.png", i+1)))
		raster := readStructuredReferencePNG(t, filepath.Join(root, fmt.Sprintf("native-raster-%d.png", i+1)))
		for _, object := range page.Page.Scene.Objects {
			if object.Chart == nil {
				continue
			}
			for _, label := range object.Chart.Labels {
				var text strings.Builder
				for _, run := range label.Text.Runs {
					text.WriteString(run.Text)
				}
				value := text.String()
				if !strings.Contains(value, "Quarterly") {
					continue
				}
				bounds := image.Rect(int(object.Bounds.X+label.Bounds.X), int(object.Bounds.Y+label.Bounds.Y), int(object.Bounds.X+label.Bounds.X+label.Bounds.Width), int(object.Bounds.Y+label.Bounds.Y+label.Bounds.Height))
				ink := func(img image.Image) int {
					n := 0
					for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
						for x := bounds.Min.X; x < bounds.Max.X; x++ {
							r, g, b, _ := img.At(x, y).RGBA()
							if r < 200*257 && g < 200*257 && b < 200*257 {
								n++
							}
						}
					}
					return n
				}
				a, b := ink(quality), ink(raster)
				t.Logf("page %d title ink PDF/raster %d/%d at %v", i+1, a, b, bounds)
				if b > 100 && a < b/2 {
					t.Errorf("PDF does not paint title on page %d", i+1)
				}
			}
		}
	}
}

func structuredPixelNear(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	delta := func(x, y uint32) bool {
		if x > y {
			return x-y <= 12*257
		}
		return y-x <= 12*257
	}
	return delta(ar, br) && delta(ag, bg) && delta(ab, bb)
}

// Vector/PDF and screenshot rasterizers differ at glyph/stroke edges. Permit
// local two-pixel antialias displacement, but not omitted captions/shifted plots.
func structuredPixelMismatch(a, b image.Image) int {
	missing := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if structuredPixelNear(a.At(x, y), b.At(x, y)) {
				continue
			}
			near := false
			for dy := -2; dy <= 2 && !near; dy++ {
				for dx := -2; dx <= 2; dx++ {
					p := image.Pt(x+dx, y+dy)
					if p.In(bounds) && structuredPixelNear(a.At(x, y), b.At(p.X, p.Y)) {
						near = true
						break
					}
				}
			}
			if !near {
				missing++
			}
		}
	}
	return missing
}

func TestStructuredPDFPixelParity(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_PDF_PIXELS")
	if root == "" {
		t.Skip("PDF Poppler renders not requested")
	}
	report := map[string][2]int{}
	raw, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	if err := json.Unmarshal(raw, &pages); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= len(pages); i++ {
		name := func(prefix string) string { return filepath.Join(root, prefix+"-"+strconv.Itoa(i)+".png") }
		quality := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-quality-page-%0*d.png", len(strconv.Itoa(len(pages))), i)))
		raster := readStructuredReferencePNG(t, name("native-raster"))
		if quality.Bounds() != raster.Bounds() || quality.Bounds().Dx() != 1920 || quality.Bounds().Dy() != 1080 {
			t.Fatal("PDF/screenshot dimensions differ", i, quality.Bounds(), raster.Bounds())
		}
		a, b := structuredPixelMismatch(quality, raster), structuredPixelMismatch(raster, quality)
		report[name("native-quality-page")] = [2]int{a, b}
		t.Logf("page %d unmatched vector/raster pixels: %d/%d of %d", i, a, b, 1920*1080)
		if a > 1920*1080/500 || b > 1920*1080/500 {
			t.Errorf("structured PDF visual parity failed on page %d", i)
		}
	}
	// Ensure the matcher cannot accept white/empty output as a rendering proof.
	paint := image.NewRGBA(image.Rect(0, 0, 120, 120))
	blank := image.NewRGBA(paint.Bounds())
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			blank.Set(x, y, color.White)
			paint.Set(x, y, color.White)
			if x > 30 && x < 90 && y > 30 && y < 90 {
				paint.Set(x, y, color.RGBA{R: 20, G: 60, B: 180, A: 255})
			}
		}
	}
	if structuredPixelMismatch(paint, blank) < 3000 {
		t.Fatal("empty-output negative control passed")
	}
	raw, _ = json.MarshalIndent(struct {
		Passed     bool              `json:"passed"`
		Method     string            `json:"method"`
		Mismatches map[string][2]int `json:"mismatches"`
	}{!t.Failed(), "Strict 12-channel/2px-neighbour match, 0.2% maximum", report}, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "pixel-report.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}
