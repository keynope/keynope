package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type alignmentInkBand struct {
	Bounds image.Rectangle
	Pixels int
}

// Only the authored yellow text qualifies; the dark page and blue shape body
// cannot make a missing label pass. Each contiguous painted text line is
// measured separately, so a correctly placed longest line cannot mask a
// misplaced short line, lost wrap, missing glyphs or vertical misalignment.
func alignmentPDFInkBands(img image.Image, frame sceneRect, scale int) []alignmentInkBand {
	bounds := image.Rect(int(math.Floor(frame.X*float64(scale))), int(math.Floor(frame.Y*float64(scale))), int(math.Ceil((frame.X+frame.Width)*float64(scale))), int(math.Ceil((frame.Y+frame.Height)*float64(scale)))).Intersect(img.Bounds())
	var bands []alignmentInkBand
	lastRow := -2
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		left, right, count := bounds.Max.X, bounds.Min.X, 0
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r >= 180*257 && g >= 165*257 && b >= 70*257 && b < 210*257 && a >= 250*257 {
				left, right, count = min(left, x), max(right, x+1), count+1
			}
		}
		if count == 0 {
			continue
		}
		row := image.Rect(left, y, right, y+1)
		if y > lastRow+1 {
			bands = append(bands, alignmentInkBand{Bounds: row, Pixels: count})
		} else {
			last := &bands[len(bands)-1]
			last.Bounds, last.Pixels = last.Bounds.Union(row), last.Pixels+count
		}
		lastRow = y
	}
	return bands
}

func compareAlignmentPDFInk(actual, reference []alignmentInkBand, scale int) error {
	if len(reference) < 2 || len(actual) != len(reference) {
		return fmt.Errorf("painted text line count %d, want %d (at least two)", len(actual), len(reference))
	}
	for i, want := range reference {
		got := actual[i]
		for axis, pair := range [][2]int{{got.Bounds.Min.X, want.Bounds.Min.X}, {got.Bounds.Max.X, want.Bounds.Max.X}, {got.Bounds.Min.Y, want.Bounds.Min.Y}, {got.Bounds.Max.Y, want.Bounds.Max.Y}} {
			if abs(pair[0]-pair[1]) > 2*scale {
				return fmt.Errorf("line %d edge %d shifted: %v, want %v", i+1, axis, got.Bounds, want.Bounds)
			}
		}
		if want.Pixels < 20*scale || got.Pixels < 20*scale {
			return fmt.Errorf("line %d has no substantial painted ink: %d, want %d", i+1, got.Pixels, want.Pixels)
		}
	}
	return nil
}

func TestTextAlignmentPDFInkOracleControls(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 100))
	for _, r := range []image.Rectangle{image.Rect(20, 15, 130, 25), image.Rect(20, 45, 100, 55)} {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, color.RGBA{255, 238, 170, 255})
			}
		}
	}
	reference := alignmentPDFInkBands(img, sceneRect{Width: 160, Height: 100}, 1)
	if err := compareAlignmentPDFInk(reference, reference, 1); err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"erase", "horizontal", "vertical", "short-line", "lost-ink"} {
		got := append([]alignmentInkBand(nil), reference...)
		switch control {
		case "erase":
			got = nil
		case "horizontal":
			for i := range got {
				got[i].Bounds = got[i].Bounds.Add(image.Pt(10, 0))
			}
		case "vertical":
			for i := range got {
				got[i].Bounds = got[i].Bounds.Add(image.Pt(0, 10))
			}
		case "short-line":
			got[1].Bounds = got[1].Bounds.Add(image.Pt(10, 0))
		case "lost-ink":
			got[1].Pixels = 0
		}
		if compareAlignmentPDFInk(got, reference, 1) == nil {
			t.Fatal("vacuous native PDF oracle accepted", control)
		}
	}
}

func TestTextAlignmentNativePDFPaintedGeometry(t *testing.T) {
	root := os.Getenv("KEYNOPE_ALIGNMENT_PDF_PIXELS")
	if root == "" {
		t.Skip("actual native PDF/independent Poppler renders not requested")
	}
	checked := 0
	type objectProof struct {
		ID       string
		PDF      []alignmentInkBand
		Snapshot []alignmentInkBand
	}
	type pageProof struct {
		Size            string
		Page            int
		Objects         []objectProof
		EdgeDifferences [2]int
		EdgeBudget      int
	}
	var records []pageProof
	for _, size := range []string{"1920x1080", "1080x1920", "2560x1080"} {
		dir := filepath.Join(root, size)
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest pdfExportManifest
		if err := json.Unmarshal(data, &manifest); err != nil || len(manifest.Pages) != 15 {
			t.Fatal("invalid native PDF fixture manifest", err)
		}
		for index, page := range manifest.Pages {
			record := pageProof{Size: size, Page: index + 1}
			actual := readStructuredPNG(t, filepath.Join(dir, fmt.Sprintf("poppler-%02d.png", index+1)))
			reference := readStructuredPNG(t, filepath.Join(dir, fmt.Sprintf("native-exact-%d.png", index+1)))
			scale := reference.Bounds().Dx() / int(page.Width)
			if scale < 1 || scale > 2 || reference.Bounds() != actual.Bounds() || actual.Bounds().Dx() != int(page.Width)*scale || actual.Bounds().Dy() != int(page.Height)*scale {
				t.Fatal("PDF/snapshot physical dimensions changed", size, index, actual.Bounds(), reference.Bounds())
			}
			if page.Page.Scene == nil || len(page.Page.Scene.Objects) != 12 {
				t.Fatal("missing expected aligned objects", size, index)
			}
			for _, object := range page.Page.Scene.Objects {
				want := alignmentPDFInkBands(reference, object.Bounds, scale)
				got := alignmentPDFInkBands(actual, object.Bounds, scale)
				if err := compareAlignmentPDFInk(got, want, scale); err != nil {
					t.Errorf("%s page %d %s: %v", size, index+1, object.ID, err)
				}
				record.Objects = append(record.Objects, objectProof{object.ID, got, want})
				checked++
			}
			// Keep the established PDF edge comparator and original 0.2% page
			// budget. Raw bright-pixel counts are not cross-rasterizer coverage:
			// PDF font edges and WebView snapshots have different AA weights.
			// Per-line bounds above independently catch even short-line shifts.
			palette := pdfScenePalette(page.Page.Scene)
			forward, reverse := pdfEdgeMismatch(actual, reference, palette), pdfEdgeMismatch(reference, actual, palette)
			budget := actual.Bounds().Dx() * actual.Bounds().Dy() / 500
			if forward > budget || reverse > budget {
				t.Errorf("%s page %d: PDF edge differences %d/%d exceed unchanged budget %d", size, index+1, forward, reverse, budget)
			}
			record.EdgeDifferences, record.EdgeBudget = [2]int{forward, reverse}, budget
			records = append(records, record)
			t.Logf("%s page %d: edge differences %d/%d, budget %d", size, index+1, forward, reverse, budget)
		}
	}
	if checked != 540 {
		t.Fatal("incomplete native text/shape-label PDF matrix", checked)
	}
	wire, err := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "objects": checked, "pages": records}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "painted-geometry-report.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	if !t.Failed() {
		t.Log("540 native PDF text/label objects: per-line ink bounds and existing edge-coverage budget match original native snapshots")
	}
}
