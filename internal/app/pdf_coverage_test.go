package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type pdfRGB [3]float64

func pdfPixel(value color.Color) pdfRGB {
	r, g, b, _ := value.RGBA()
	return pdfRGB{float64(r) / 257, float64(g) / 257, float64(b) / 257}
}
func pdfNear(a, b pdfRGB) bool {
	return math.Abs(a[0]-b[0]) <= 12 && math.Abs(a[1]-b[1]) <= 12 && math.Abs(a[2]-b[2]) <= 12
}
func pdfInkAlpha(value, bg, fg pdfRGB) float64 {
	numerator, denominator := 0.0, 0.0
	for i := range value {
		d := fg[i] - bg[i]
		numerator += (value[i] - bg[i]) * d
		denominator += d * d
	}
	if denominator < 100 {
		return 0
	}
	alpha := numerator / denominator
	if alpha < .04 || alpha > 1.04 {
		return 0
	}
	for i := range value {
		if math.Abs(value[i]-(bg[i]+alpha*(fg[i]-bg[i]))) > 12 {
			return 0
		}
	}
	return math.Min(1, alpha)
}
func pdfInkMix(value, bg, fg pdfRGB) bool { return pdfInkAlpha(value, bg, fg) > 0 }

func pdfScenePalette(scene *slideScene) []pdfRGB {
	var palette []pdfRGB
	add := func(value string) {
		value = strings.TrimPrefix(value, "#")
		if len(value) != 6 {
			return
		}
		v, err := strconv.ParseUint(value, 16, 24)
		if err != nil {
			return
		}
		rgb := pdfRGB{float64(v >> 16), float64(v >> 8 & 255), float64(v & 255)}
		for _, old := range palette {
			if old == rgb {
				return
			}
		}
		palette = append(palette, rgb)
	}
	paint := func(p scenePaint) {
		for _, c := range []string{p.Color, p.Stroke, p.GradientStart, p.GradientEnd, p.ShadowColor} {
			add(c)
		}
	}
	text := func(data *sceneText) {
		if data == nil {
			return
		}
		for _, r := range data.Runs {
			add(r.Color)
		}
		for _, p := range data.Paragraphs {
			for _, r := range p.Runs {
				add(r.Color)
			}
		}
	}
	add(scene.Background)
	for _, o := range scene.Objects {
		paint(o.Paint)
		text(o.Text)
		text(o.Label)
		if o.LabelPaint != nil {
			paint(*o.LabelPaint)
		}
		if o.Table != nil {
			for _, c := range o.Table.Cells {
				add(c.Fill)
				add(c.BorderColor)
				paint(c.Paint)
				text(c.Text)
			}
			for _, b := range o.Table.Borders {
				add(b.Color)
			}
		}
		if o.Chart != nil {
			for _, p := range o.Chart.Paths {
				add(p.Fill)
				add(p.Stroke)
			}
			for _, l := range o.Chart.Labels {
				paint(l.Paint)
				text(l.Text)
			}
			for _, c := range o.Chart.SeriesColors {
				add(c)
			}
			for _, c := range o.Chart.CategoryColors {
				add(c)
			}
		}
	}
	return palette
}

// Preserve the original exact/local matcher. Only unmatched edge samples may
// use a different AA coverage of a known scene ink over the local background.
// A flat reference area cannot stand in for omitted or wrongly coloured ink.
func pdfEdgeMismatch(a, b image.Image, palette []pdfRGB) int {
	missing := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := pdfPixel(a.At(x, y))
			if pdfNear(value, pdfPixel(b.At(x, y))) {
				continue
			}
			var nearby []pdfRGB
			counts := map[pdfRGB]int{}
			near := false
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					p := image.Pt(x+dx, y+dy)
					if !p.In(bounds) {
						continue
					}
					v := pdfPixel(b.At(p.X, p.Y))
					nearby = append(nearby, v)
					counts[v]++
					if pdfNear(value, v) {
						near = true
					}
				}
			}
			if near {
				continue
			}
			var background pdfRGB
			maximum := 0
			for _, v := range nearby {
				if counts[v] > maximum {
					background = v
					maximum = counts[v]
				}
			}
			edge := false
			for _, v := range nearby {
				if !pdfNear(v, background) {
					edge = true
					break
				}
			}
			if edge {
				for _, fg := range palette {
					if !pdfInkMix(value, background, fg) {
						continue
					}
					for _, v := range nearby {
						if pdfInkMix(v, background, fg) {
							near = true
							break
						}
					}
					if near {
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

type pdfInkStats struct{ Mass, X, Y float64 }

// Circular-chart captions sit on series fills, not on the slide background.
// Choose the dominant known solid scene colour from the exact reference ROI,
// excluding the foreground. Never infer a replacement ink from the test image.
func pdfCaptionBackground(img image.Image, bounds image.Rectangle, fg, fallback pdfRGB, palette []pdfRGB) pdfRGB {
	counts := make([]int, len(palette))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !image.Pt(x, y).In(img.Bounds()) {
				continue
			}
			value := pdfPixel(img.At(x, y))
			for i, candidate := range palette {
				if !pdfNear(candidate, fg) && pdfNear(value, candidate) {
					counts[i]++
				}
			}
		}
	}
	maximum, result := 0, fallback
	for i, count := range counts {
		if count > maximum {
			maximum, result = count, palette[i]
		}
	}
	return result
}

func pdfCaptionInk(img image.Image, bounds image.Rectangle, bg, fg pdfRGB) pdfInkStats {
	var result pdfInkStats
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !image.Pt(x, y).In(img.Bounds()) {
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
func pdfCaptionInkAgrees(a, b pdfInkStats) bool {
	return b.Mass >= 8 && a.Mass >= b.Mass*.65 && a.Mass <= b.Mass*1.5 && math.Abs(a.X-b.X) <= 2 && math.Abs(a.Y-b.Y) <= 2
}
func pdfHexColour(value string) pdfRGB {
	value = strings.TrimPrefix(value, "#")
	v, _ := strconv.ParseUint(value, 16, 24)
	return pdfRGB{float64(v >> 16), float64(v >> 8 & 255), float64(v & 255)}
}

func TestPDFEdgeCoverageNegativeControls(t *testing.T) {
	bounds := image.Rect(0, 0, 160, 100)
	bg, fg := color.NRGBA{255, 255, 255, 255}, color.NRGBA{20, 60, 180, 255}
	makeImage := func(ink color.Color) *image.RGBA {
		img := image.NewRGBA(bounds)
		draw.Draw(img, bounds, &image.Uniform{bg}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(20, 50, 140, 51), &image.Uniform{ink}, image.Point{}, draw.Src)
		return img
	}
	a, b := makeImage(color.NRGBA{67, 99, 195, 255}), makeImage(color.NRGBA{161, 177, 225, 255})
	palette := []pdfRGB{pdfPixel(bg), pdfPixel(fg), pdfRGB{180, 20, 60}}
	if x, y := pdfEdgeMismatch(a, b, palette), pdfEdgeMismatch(b, a, palette); x != 0 || y != 0 {
		t.Fatal("legitimate AA coverage rejected", x, y)
	}
	blank := image.NewRGBA(bounds)
	draw.Draw(blank, bounds, &image.Uniform{bg}, image.Point{}, draw.Src)
	for name, broken := range map[string]*image.RGBA{"missing line": blank, "wrong hue": makeImage(color.NRGBA{180, 20, 60, 255}), "wrong coverage on solid": makeImage(fg)} {
		reference := makeImage(fg)
		if name == "wrong coverage on solid" {
			reference = makeImage(fg)
			draw.Draw(broken, image.Rect(20, 50, 140, 51), &image.Uniform{color.NRGBA{161, 177, 225, 255}}, image.Point{}, draw.Src)
			draw.Draw(reference, image.Rect(20, 49, 140, 54), &image.Uniform{fg}, image.Point{}, draw.Src)
			draw.Draw(broken, image.Rect(20, 49, 140, 54), &image.Uniform{color.NRGBA{161, 177, 225, 255}}, image.Point{}, draw.Src)
		}
		x, y := pdfEdgeMismatch(broken, reference, palette), pdfEdgeMismatch(reference, broken, palette)
		if x+y < 100 {
			t.Fatal("bad image accepted", name, x, y)
		}
	}
}

func TestStructuredPDFCoverageParity(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_PDF_PIXELS")
	if root == "" {
		t.Skip("PDF renders not requested")
	}
	raw, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	if err := json.Unmarshal(raw, &pages); err != nil {
		t.Fatal(err)
	}
	d, _ := structuredPDFDeck(t)
	document, err := makePDFExportDocument("Structured.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != len(document.Manifest.Pages) {
		t.Fatal("rendered corpus and current manifest page counts differ")
	}
	records := map[int][2]int{}
	controls := map[string]bool{}
	captionCount := 0
	cellCount := 0
	for i := range pages {
		a := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-quality-page-%02d.png", i+1)))
		b := readStructuredReferencePNG(t, filepath.Join(root, fmt.Sprintf("native-raster-%d.png", i+1)))
		if a.Bounds() != image.Rect(0, 0, 1920, 1080) || b.Bounds() != a.Bounds() {
			t.Fatal("unexpected rendered page bounds", i+1, a.Bounds(), b.Bounds())
		}
		palette := pdfScenePalette(document.Manifest.Pages[i].Page.Scene)
		x, y := pdfEdgeMismatch(a, b, palette), pdfEdgeMismatch(b, a, palette)
		records[i+1] = [2]int{x, y}
		t.Logf("page %d coverage unmatched %d/%d", i+1, x, y)
		if x > 1920*1080/500 || y > 1920*1080/500 {
			t.Errorf("coverage mismatch on page %d", i+1)
		}
		scene := document.Manifest.Pages[i].Page.Scene
		for _, o := range scene.Objects {
			// Small cell loss can be well below the whole-page budget. Check
			// unrotated cell interiors independently, excluding seam pixels.
			if o.Table != nil && o.Rotation == 0 {
				for _, cell := range o.Table.Cells {
					if strings.TrimSpace(sceneTextPlain(cell.Text)) == "" {
						continue
					}
					c, interior := cell.Bounds, cell.ContentBounds
					x, y := o.Bounds.X+c.X+interior.X, o.Bounds.Y+c.Y+interior.Y
					r := image.Rect(int(math.Ceil(x)), int(math.Ceil(y)), int(math.Floor(x+interior.Width)), int(math.Floor(y+interior.Height)))
					bg, fg := pdfHexColour(cell.Fill), pdfHexColour(cell.Paint.Color)
					if cell.Fill == "" || cell.Fill == "none" {
						bg = pdfHexColour(scene.Background)
					}
					bg = pdfCaptionBackground(b, r, fg, bg, palette)
					q, ref := pdfCaptionInk(a, r, bg, fg), pdfCaptionInk(b, r, bg, fg)
					cellCount++
					if !pdfCaptionInkAgrees(q, ref) {
						t.Errorf("cell ink mismatch page %d [%d,%d]: %+v / %+v", i+1, cell.Row, cell.Column, q, ref)
					}
					if i == 2 && cell.Row == 9 && cell.Column == 9 {
						background := color.NRGBA{uint8(bg[0]), uint8(bg[1]), uint8(bg[2]), 255}
						for name, delta := range map[string]int{"missing dense cell": 0, "shifted dense cell": 8} {
							broken := image.NewRGBA(a.Bounds())
							draw.Draw(broken, a.Bounds(), a, a.Bounds().Min, draw.Src)
							draw.Draw(broken, r, &image.Uniform{background}, image.Point{}, draw.Src)
							if delta != 0 {
								draw.Draw(broken, r.Add(image.Pt(delta, 0)), a, r.Min, draw.Src)
							}
							controls[name] = !pdfCaptionInkAgrees(pdfCaptionInk(broken, r, bg, fg), ref)
							m, n := pdfEdgeMismatch(broken, b, palette), pdfEdgeMismatch(b, broken, palette)
							if m > 1920*1080/500 || n > 1920*1080/500 {
								t.Fatal("dense-cell control unexpectedly exceeded the page budget", m, n)
							}
						}
					}
				}
			}
			if o.Chart == nil {
				continue
			}
			if o.Rotation != 0 {
				t.Fatal("corpus caption proof requires unrotated charts")
			}
			for _, l := range o.Chart.Labels {
				value := sceneTextPlain(l.Text)
				if strings.TrimSpace(value) == "" {
					continue
				}
				r := image.Rect(int(math.Floor(o.Bounds.X+l.Bounds.X)), int(math.Floor(o.Bounds.Y+l.Bounds.Y)), int(math.Ceil(o.Bounds.X+l.Bounds.X+l.Bounds.Width)), int(math.Ceil(o.Bounds.Y+l.Bounds.Y+l.Bounds.Height)))
				bg, fg := pdfHexColour(scene.Background), pdfHexColour(l.Paint.Color)
				bg = pdfCaptionBackground(b, r, fg, bg, palette)
				q, ref := pdfCaptionInk(a, r, bg, fg), pdfCaptionInk(b, r, bg, fg)
				captionCount++
				if !pdfCaptionInkAgrees(q, ref) {
					t.Errorf("caption ink mismatch page %d %q: %+v / %+v", i+1, value, q, ref)
				}
				if i == 4 && (value == "Quarterly workshop outcomes" || value == "Team A" || value == "-3") {
					erased := image.NewRGBA(a.Bounds())
					draw.Draw(erased, a.Bounds(), a, a.Bounds().Min, draw.Src)
					background := color.NRGBA{uint8(bg[0]), uint8(bg[1]), uint8(bg[2]), 255}
					draw.Draw(erased, r, &image.Uniform{background}, image.Point{}, draw.Src)
					controls["missing "+value] = !pdfCaptionInkAgrees(pdfCaptionInk(erased, r, bg, fg), ref)
					shifted := image.NewRGBA(a.Bounds())
					draw.Draw(shifted, a.Bounds(), a, a.Bounds().Min, draw.Src)
					draw.Draw(shifted, r, &image.Uniform{background}, image.Point{}, draw.Src)
					draw.Draw(shifted, r.Add(image.Pt(16, 0)), a, r.Min, draw.Src)
					controls["shifted "+value] = !pdfCaptionInkAgrees(pdfCaptionInk(shifted, r, bg, fg), ref)
				}
			}
		}
		if i == 4 {
			plot := scene.Objects[1].Chart.Plot
			o := scene.Objects[1].Bounds
			r := image.Rect(int(o.X+plot.X), int(o.Y+plot.Y), int(o.X+plot.X+plot.Width), int(o.Y+plot.Y+plot.Height))
			for name, delta := range map[string]int{"shifted marks": 20, "wrong fill": 0} {
				broken := image.NewRGBA(a.Bounds())
				draw.Draw(broken, a.Bounds(), a, a.Bounds().Min, draw.Src)
				draw.Draw(broken, r, &image.Uniform{color.White}, image.Point{}, draw.Src)
				if delta != 0 {
					draw.Draw(broken, r.Add(image.Pt(delta, 0)), a, r.Min, draw.Src)
				} else {
					draw.Draw(broken, r, &image.Uniform{color.NRGBA{255, 40, 70, 255}}, image.Point{}, draw.Src)
				}
				m, n := pdfEdgeMismatch(broken, b, palette), pdfEdgeMismatch(b, broken, palette)
				controls[name] = m > 1920*1080/500 || n > 1920*1080/500
				t.Logf("negative %s mismatch %d/%d", name, m, n)
			}
		}
	}
	for name, rejected := range controls {
		if !rejected {
			t.Errorf("negative control passed: %s", name)
		}
	}
	if len(controls) != 10 {
		t.Fatal("missing negative controls", controls)
	}
	raw, err = json.MarshalIndent(struct {
		Passed   bool            `json:"passed"`
		Method   string          `json:"method"`
		Pages    map[int][2]int  `json:"pages"`
		Controls map[string]bool `json:"rejectedControls"`
		Captions int             `json:"checkedCaptions"`
		Cells    int             `json:"checkedCells"`
	}{!t.Failed(), "Known scene-ink/background AA ray at existing 12-channel/2px limits; 0.2% page budget plus per-caption/cell mass/centroid", records, controls, captionCount, cellCount}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "coverage-report.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}
