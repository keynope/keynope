package app

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// A short, missing merged-cell border can be smaller than the whole-page
// tolerance. Check each independently painted perimeter segment as ink instead
// of inferring its presence from the page-level score or selectable text.
func TestMergedPerimeterPDFSegmentCoverage(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_PDF_PIXELS")
	if root == "" {
		t.Skip("PDF renders not requested")
	}
	a := readStructuredPNG(t, filepath.Join(root, "native-quality-page-01.png"))
	b := readStructuredReferencePNG(t, filepath.Join(root, "native-raster-1.png"))
	if a.Bounds() != image.Rect(0, 0, 1920, 1080) || b.Bounds() != a.Bounds() {
		t.Fatal("unexpected PDF/reference dimensions", a.Bounds(), b.Bounds())
	}
	d, _ := structuredPDFDeck(t)
	document, err := makePDFExportDocument("Structured.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	scene := document.Manifest.Pages[0].Page.Scene
	o := scene.Objects[1]
	if o.Table == nil || o.Rotation != 0 {
		t.Fatal("expected the unrotated merged perimeter fixture")
	}
	positions := func(sizes []float64) []float64 {
		p := make([]float64, len(sizes)+1)
		for i, size := range sizes {
			p[i+1] = p[i] + size
		}
		return p
	}
	xs, ys := positions(o.Table.Columns), positions(o.Table.Rows)
	palette := pdfScenePalette(scene)
	type result struct {
		Axis       string  `json:"axis"`
		Track      int     `json:"track"`
		Offset     int     `json:"offset"`
		Color      string  `json:"color"`
		Width      float64 `json:"width"`
		PDF, Exact pdfInkStats
	}
	var records []result
	controls := map[string]bool{}
	for _, edge := range o.Table.Borders {
		// Only the eight independently painted sides of the 2x2 merged
		// owner, not neighbouring cells or invisible internal merge seams.
		if edge.Axis == "horizontal" && (edge.Track != 0 && edge.Track != 2 || edge.From >= 2) || edge.Axis == "vertical" && (edge.Track != 0 && edge.Track != 2 || edge.From >= 2) {
			continue
		}
		for offset := edge.From; offset < min(edge.To, 2); offset++ {
			if edge.Width <= 0 || edge.Color == "" || edge.Color == "none" {
				t.Fatal("fixture no longer exercises eight visible perimeter segments", edge)
			}
			var cx, cy, halfX, halfY float64
			if edge.Axis == "horizontal" {
				cx, cy = o.Bounds.X+(xs[offset]+xs[offset+1])/2, o.Bounds.Y+ys[edge.Track]
				halfX, halfY = 20, edge.Width/2+3
			} else {
				cx, cy = o.Bounds.X+xs[edge.Track], o.Bounds.Y+(ys[offset]+ys[offset+1])/2
				halfX, halfY = edge.Width/2+3, 20
			}
			r := image.Rect(int(math.Floor(cx-halfX)), int(math.Floor(cy-halfY)), int(math.Ceil(cx+halfX)), int(math.Ceil(cy+halfY)))
			fg := pdfHexColour(edge.Color)
			bg := pdfCaptionBackground(b, r, fg, pdfHexColour(scene.Background), palette)
			actual, exact := pdfCaptionInk(a, r, bg, fg), pdfCaptionInk(b, r, bg, fg)
			records = append(records, result{edge.Axis, edge.Track, offset, edge.Color, edge.Width, actual, exact})
			if !pdfCaptionInkAgrees(actual, exact) {
				t.Errorf("merged %s track %d segment %d: PDF %+v / exact %+v", edge.Axis, edge.Track, offset, actual, exact)
			}
			if len(controls) == 0 {
				if r.Dx()*r.Dy() >= 1920*1080/500 {
					t.Fatal("segment negative controls must fit below the page error budget")
				}
				background := color.NRGBA{uint8(bg[0]), uint8(bg[1]), uint8(bg[2]), 255}
				for _, name := range []string{"missing", "wrong-colour", "shifted"} {
					broken := image.NewRGBA(r)
					draw.Draw(broken, r, &image.Uniform{background}, image.Point{}, draw.Src)
					switch name {
					case "wrong-colour":
						draw.Draw(broken, r, &image.Uniform{color.NRGBA{20, 240, 60, 255}}, image.Point{}, draw.Src)
					case "shifted":
						delta := image.Pt(0, 6)
						if edge.Axis == "vertical" {
							delta = image.Pt(6, 0)
						}
						draw.Draw(broken, r.Add(delta), a, r.Min, draw.Src)
					}
					controls[name] = !pdfCaptionInkAgrees(pdfCaptionInk(broken, r, bg, fg), exact)
				}
			}
		}
	}
	if len(records) != 8 || len(controls) != 3 {
		t.Fatal("incomplete perimeter coverage", len(records), controls)
	}
	for name, rejected := range controls {
		if !rejected {
			t.Errorf("perimeter corruption was not detected: %s", name)
		}
	}
	raw, err := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "segments": records, "rejectedControls": controls}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "perimeter-coverage-report.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}
