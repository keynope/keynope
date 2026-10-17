package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Cell regions follow the same owner-centred affine transform as the scene.
// The AABB only limits iteration; inverse membership excludes neighbouring
// cells/seams rather than accepting their ink inside a rotated AABB corner.
type pdfCellRegion struct {
	bounds                 image.Rectangle
	local                  sceneRect
	x, y, cx, cy, cos, sin float64
}

func rotatedPDFCellRegion(owner sceneObject, cell sceneTableCell) pdfCellRegion {
	local := cell.ContentBounds
	local.X += cell.Bounds.X
	local.Y += cell.Bounds.Y
	angle := owner.Rotation * math.Pi / 180
	r := pdfCellRegion{local: local, x: owner.Bounds.X, y: owner.Bounds.Y, cx: owner.Bounds.Width / 2, cy: owner.Bounds.Height / 2, cos: math.Cos(angle), sin: math.Sin(angle)}
	left, top, right, bottom := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range [][2]float64{{local.X, local.Y}, {local.X + local.Width, local.Y}, {local.X, local.Y + local.Height}, {local.X + local.Width, local.Y + local.Height}} {
		dx, dy := p[0]-r.cx, p[1]-r.cy
		x, y := r.x+r.cx+dx*r.cos-dy*r.sin, r.y+r.cy+dx*r.sin+dy*r.cos
		left, top, right, bottom = math.Min(left, x), math.Min(top, y), math.Max(right, x), math.Max(bottom, y)
	}
	r.bounds = image.Rect(int(math.Floor(left)), int(math.Floor(top)), int(math.Ceil(right)), int(math.Ceil(bottom)))
	return r
}

func (r pdfCellRegion) contains(x, y int) bool {
	dx, dy := float64(x)+.5-r.x-r.cx, float64(y)+.5-r.y-r.cy
	xLocal, yLocal := r.cx+dx*r.cos+dy*r.sin, r.cy-dx*r.sin+dy*r.cos
	return xLocal >= r.local.X && xLocal < r.local.X+r.local.Width && yLocal >= r.local.Y && yLocal < r.local.Y+r.local.Height
}

func (r pdfCellRegion) ink(img image.Image, bg, fg pdfRGB) pdfInkStats {
	var result pdfInkStats
	area := r.bounds.Intersect(img.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if !r.contains(x, y) {
				continue
			}
			alpha := pdfInkAlpha(pdfPixel(img.At(x, y)), bg, fg)
			result.Mass += alpha
			result.X += float64(x) * alpha
			result.Y += float64(y) * alpha
		}
	}
	if result.Mass > 0 {
		result.X /= result.Mass
		result.Y /= result.Mass
	}
	return result
}

func pdfCellTextInks(cell sceneTableCell) []pdfRGB {
	var inks []pdfRGB
	add := func(run sceneRun) {
		if strings.TrimSpace(run.Text) == "" {
			return
		}
		value := run.Color
		if value == "" {
			value = cell.Paint.Color
		}
		if value == "none" {
			return
		}
		ink := pdfHexColour(value)
		for _, old := range inks {
			if old == ink {
				return
			}
		}
		inks = append(inks, ink)
	}
	if cell.Text != nil {
		for _, run := range cell.Text.Runs {
			add(run)
		}
		for _, paragraph := range cell.Text.Paragraphs {
			for _, run := range paragraph.Runs {
				add(run)
			}
		}
	}
	return inks
}

func TestRotatedPDFCellMaskRejectsNeighbourInk(t *testing.T) {
	owner := sceneObject{Bounds: sceneRect{X: 20, Y: 20, Width: 120, Height: 80}, Rotation: 37}
	cell := sceneTableCell{Bounds: sceneRect{X: 10, Y: 10}, ContentBounds: sceneRect{X: 3, Y: 3, Width: 90, Height: 36}}
	r := rotatedPDFCellRegion(owner, cell)
	img := image.NewRGBA(image.Rect(0, 0, 180, 150))
	bg, fg := color.White, color.NRGBA{30, 70, 180, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	outside := 0
	for y := r.bounds.Min.Y; y < r.bounds.Max.Y; y++ {
		for x := r.bounds.Min.X; x < r.bounds.Max.X; x++ {
			if !r.contains(x, y) {
				outside++
				img.Set(x, y, fg)
			}
		}
	}
	if outside == 0 || r.ink(img, pdfPixel(bg), pdfPixel(fg)).Mass != 0 {
		t.Fatal("rotated region admitted a neighbouring cell's ink")
	}
	for y := r.bounds.Min.Y; y < r.bounds.Max.Y; y++ {
		for x := r.bounds.Min.X; x < r.bounds.Max.X; x++ {
			if r.contains(x, y) {
				img.Set(x, y, fg)
			}
		}
	}
	if r.ink(img, pdfPixel(bg), pdfPixel(fg)).Mass < 100 {
		t.Fatal("rotation mask lost its own cell interior")
	}
}

func TestRotatedTablePDFCellInkCoverage(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_PDF_PIXELS")
	if root == "" {
		t.Skip("set KEYNOPE_STRUCTURED_PDF_PIXELS for native PDF renders")
	}
	deck, _ := structuredPDFDeck(t)
	document, err := makePDFExportDocument("Rotated cell proof.md", deck, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	type record struct {
		Page, Row, Column int
		BackingScale      int
		Color             pdfRGB
		PDF, Reference    pdfInkStats
	}
	var records []record
	cells := 0
	controls := map[string]bool{}
	for index, page := range document.Manifest.Pages {
		for _, owner := range page.Page.Scene.Objects {
			if owner.Table == nil || owner.Rotation == 0 {
				continue
			}
			b := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-raster-%d.png", index+1)))
			scale := 1
			qualityPath := filepath.Join(root, fmt.Sprintf("native-quality-page-%02d.png", index+1))
			if b.Bounds() == image.Rect(0, 0, 3840, 2160) {
				// Compare fine rotated glyphs at the snapshot's original backing
				// resolution. Downsampling thin italic strokes first can change
				// their colour-separated centroid. Thresholds remain in logical
				// pixels and mass is normalized by the pixel-area scale below.
				scale = 2
				binary, err := exec.LookPath("pdftoppm")
				if err != nil {
					t.Fatal("Retina PDF comparison requires Poppler:", err)
				}
				prefix := filepath.Join(root, fmt.Sprintf("native-quality-backing-%d", index+1))
				page := strconv.Itoa(index + 1)
				if output, err := exec.CommandContext(t.Context(), binary, "-f", page, "-l", page, "-singlefile", "-scale-to-x", "3840", "-scale-to-y", "2160", "-png", filepath.Join(root, "native-quality.pdf"), prefix).CombinedOutput(); err != nil {
					t.Fatalf("render PDF at reference density: %v\n%s", err, output)
				}
				qualityPath = prefix + ".png"
			}
			a := readStructuredPNG(t, qualityPath)
			if a.Bounds() != image.Rect(0, 0, 1920*scale, 1080*scale) || b.Bounds() != a.Bounds() {
				t.Fatal("unexpected PDF/reference dimensions", a.Bounds(), b.Bounds())
			}
			logicalInk := func(ink pdfInkStats) pdfInkStats {
				ink.Mass /= float64(scale * scale)
				ink.X /= float64(scale)
				ink.Y /= float64(scale)
				return ink
			}
			scaleRect := func(r sceneRect) sceneRect {
				factor := float64(scale)
				return sceneRect{X: r.X * factor, Y: r.Y * factor, Width: r.Width * factor, Height: r.Height * factor}
			}
			owner.Bounds = scaleRect(owner.Bounds)
			for _, cell := range owner.Table.Cells {
				if strings.TrimSpace(sceneTextPlain(cell.Text)) == "" {
					continue
				}
				cells++
				cell.Bounds, cell.ContentBounds = scaleRect(cell.Bounds), scaleRect(cell.ContentBounds)
				r := rotatedPDFCellRegion(owner, cell)
				fill := cell.Fill
				if fill == "" || fill == "none" {
					fill = page.Page.Scene.Background
				}
				bg := pdfHexColour(fill)
				inks := pdfCellTextInks(cell)
				if len(inks) == 0 {
					t.Fatal("nonempty visible cell has no foreground proof", cell.Row, cell.Column)
				}
				for _, fg := range inks {
					q, ref := logicalInk(r.ink(a, bg, fg)), logicalInk(r.ink(b, bg, fg))
					records = append(records, record{index + 1, cell.Row, cell.Column, scale, fg, q, ref})
					if !pdfCaptionInkAgrees(q, ref) {
						t.Errorf("rotated cell [%d,%d] ink %v: PDF %+v reference %+v", cell.Row, cell.Column, fg, q, ref)
					}
					if cell.Row != 0 || cell.Column != 0 {
						continue
					}
					for name, delta := range map[string]int{"missing rotated cell": 0, "shifted rotated cell": 8 * scale, "wrong-colour rotated cell": -1} {
						broken := image.NewRGBA(a.Bounds())
						draw.Draw(broken, a.Bounds(), a, a.Bounds().Min, draw.Src)
						for y := r.bounds.Min.Y; y < r.bounds.Max.Y; y++ {
							for x := r.bounds.Min.X; x < r.bounds.Max.X; x++ {
								if !r.contains(x, y) {
									continue
								}
								value := bg
								if delta == -1 {
									alpha := pdfInkAlpha(pdfPixel(a.At(x, y)), bg, fg)
									wrong := pdfRGB{221, 85, 0}
									for c := range value {
										value[c] = bg[c] + alpha*(wrong[c]-bg[c])
									}
								} else if delta > 0 && r.contains(x-delta, y) {
									value = pdfPixel(a.At(x-delta, y))
								}
								broken.Set(x, y, color.NRGBA{uint8(math.Round(value[0])), uint8(math.Round(value[1])), uint8(math.Round(value[2])), 255})
							}
						}
						controls[name] = !pdfCaptionInkAgrees(logicalInk(r.ink(broken, bg, fg)), ref)
						// Show why a per-cell guard is necessary: these localized
						// defects must not rely on the full-page error budget.
						palette := pdfScenePalette(page.Page.Scene)
						logicalBroken, err := structuredLogicalReference(broken)
						if err != nil {
							t.Fatal(err)
						}
						logicalReference, err := structuredLogicalReference(b)
						if err != nil {
							t.Fatal(err)
						}
						if pdfEdgeMismatch(logicalBroken, logicalReference, palette) > 1920*1080/500 || pdfEdgeMismatch(logicalReference, logicalBroken, palette) > 1920*1080/500 {
							t.Fatal("rotated-cell control exceeded the whole-page budget", name)
						}
					}
				}
			}
		}
	}
	if cells != 16 || len(records) < 17 || len(controls) != 3 {
		t.Fatal("incomplete rotated rich-cell coverage", cells, len(records), controls)
	}
	for name, rejected := range controls {
		if !rejected {
			t.Errorf("defective output accepted: %s", name)
		}
	}
	report := struct {
		Passed   bool            `json:"passed"`
		Cells    int             `json:"checkedCells"`
		Records  []record        `json:"inkChecks"`
		Controls map[string]bool `json:"rejectedControls"`
	}{!t.Failed(), cells, records, controls}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rotated-cell-coverage-report.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
