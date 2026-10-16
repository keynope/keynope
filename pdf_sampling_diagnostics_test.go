package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestPDFDualMatteMediaPrototype(t *testing.T) {
	root := os.Getenv("KEYNOPE_PDF_MATTE_REFERENCE")
	if root == "" {
		t.Skip("isolated dual-matte prototype not requested")
	}
	for _, page := range []int{2, 10} {
		media := readStructuredPNG(t, fmt.Sprintf("/tmp/keynope-transparent-media-%d.png", page))
		original := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-exact-%d.png", page)))
		if media.Bounds() != original.Bounds() && media.Bounds() != image.Rect(0, 0, original.Bounds().Dx()*2, original.Bounds().Dy()*2) {
			t.Fatal("matte prototype changed page aspect/backing dimensions")
		}
		composite := image.NewNRGBA(media.Bounds())
		draw.Draw(composite, composite.Bounds(), &image.Uniform{color.NRGBA{16, 24, 40, 255}}, image.Point{}, draw.Src)
		draw.Draw(composite, composite.Bounds(), media, media.Bounds().Min, draw.Over)
		if composite.Bounds() != original.Bounds() {
			normalized := image.NewNRGBA(original.Bounds())
			xdraw.ApproxBiLinear.Scale(normalized, normalized.Bounds(), composite, composite.Bounds(), xdraw.Src, nil)
			composite = normalized
		}
		region := image.Rect(0, original.Bounds().Dy()*160/1080, composite.Bounds().Max.X, composite.Bounds().Max.Y)
		a := composite.SubImage(region)
		b := original.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(region)
		forward, reverse := structuredPixelMismatch(a, b), structuredPixelMismatch(b, a)
		limit := region.Dx() * region.Dy() * 3 / 1000
		t.Logf("page %d dual-matte/original raw mismatch %d/%d limit %d", page, forward, reverse, limit)
		palette := []pdfRGB{{16, 24, 40}, {230, 50, 70}, {40, 190, 100}, {40, 100, 230}, {240, 190, 30}}
		inkForward, inkReverse := pdfEdgeMismatch(a, b, palette), pdfEdgeMismatch(b, a, palette)
		t.Logf("page %d dual-matte/original ink mismatch %d/%d limit %d", page, inkForward, inkReverse, limit)
		f, err := os.Create(fmt.Sprintf("/tmp/keynope-transparent-composite-%d.png", page))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, composite)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		if inkForward > limit || inkReverse > limit {
			t.Error("isolated media reconstruction changed the original")
		}
	}
}

// Opt-in read-only diagnostics: distinguish broad colour errors from localized
// interpolation edges before trying another PDF capture change.
func TestPDFImageSamplingDiagnostics(t *testing.T) {
	root := os.Getenv("KEYNOPE_PDF_SAMPLING_DIAGNOSTICS")
	if root == "" {
		t.Skip("sampling diagnostics not requested")
	}
	for _, page := range []int{2, 10} {
		a := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("pdftocairo-%d.png", page)))
		b := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-assembled-%d.png", page)))
		if a.Bounds() != b.Bounds() {
			t.Fatal("different diagnostic image sizes")
		}
		if a.Bounds() != image.Rect(0, 0, 1080, 1920) {
			t.Fatal("these source-edge diagnostics require the owned portrait fixture")
		}
		counts := map[string]int{}
		bounds := image.Rectangle{}
		var rows, cols [12]int
		pixels := 0
		for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
			for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
				left, right := pdfPixel(a.At(x, y)), pdfPixel(b.At(x, y))
				if pdfNear(left, right) {
					continue
				}
				pixels++
				if bounds.Empty() {
					bounds = image.Rect(x, y, x+1, y+1)
				} else {
					bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
				}
				rows[y*12/a.Bounds().Dy()]++
				cols[x*12/a.Bounds().Dx()]++
				counts[fmt.Sprintf("%v -> %v", left, right)]++
			}
		}
		keys := make([]string, 0, len(counts))
		for key := range counts {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if counts[keys[i]] == counts[keys[j]] {
				return keys[i] < keys[j]
			}
			return counts[keys[i]] > counts[keys[j]]
		})
		t.Logf("page %d raw same-location mismatches %d extent %v; row bands %v; column bands %v", page, pixels, bounds, rows, cols)
		for _, key := range keys[:min(12, len(keys))] {
			t.Logf("%d: %s", counts[key], key)
		}
		exact := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-exact-%d.png", page)))
		scale := exact.Bounds().Dx() / a.Bounds().Dx()
		if scale < 1 || scale > 2 || exact.Bounds() != image.Rect(0, 0, 1080*scale, 1920*scale) {
			t.Fatal("unexpected original snapshot backing dimensions")
		}
		centre, y := 187, 700
		if page == 10 {
			centre, y = a.Bounds().Dx()/2, a.Bounds().Dy()/3
		}
		for x := centre - 20; x <= centre+20; x += 2 {
			t.Logf("profile %d,%d Cairo=%v PDFKit=%v original=%v", x, y, pdfPixel(a.At(x, y)), pdfPixel(b.At(x, y)), pdfPixel(exact.At(x*scale, y*scale)))
		}
	}
}
