package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	xdraw "golang.org/x/image/draw"
)

type mediaPDFCase struct {
	Title   string `json:"title"`
	Quality bool   `json:"quality"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// Opt-in external-reader proof. Poppler is a test-only tool, never an app
// dependency. Read the final assembled PDF, not its per-page source documents.
func TestMediaPDFIndependentReader(t *testing.T) {
	root := os.Getenv("KEYNOPE_MEDIA_PDF_READER")
	if root == "" {
		t.Skip("independent PDF reader not requested")
	}
	reader := os.Getenv("KEYNOPE_MEDIA_PDF_READER_ENGINE")
	if reader == "" {
		reader = "pdftoppm"
	}
	if reader != "pdftoppm" && reader != "pdftocairo" {
		t.Fatal("unsupported independent reader", reader)
	}
	binary, err := exec.LookPath(reader)
	if err != nil {
		t.Fatal("independent reader requires test-only "+reader, err)
	}
	version, err := exec.CommandContext(t.Context(), binary, "-v").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Reader: %s", version)
	raw, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []mediaPDFCase
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		t.Fatal("missing media cases", err)
	}
	for i, item := range cases {
		t.Run(fmt.Sprintf("%02d_%s", i+1, item.Title), func(t *testing.T) {
			reference := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-assembled-%d.png", i+1)))
			bounds := reference.Bounds()
			prefix := filepath.Join(root, fmt.Sprintf("%s-%d", reader, i+1))
			page := strconv.Itoa(i + 1)
			// Preserve the actual chosen capture's backing resolution. Scaling
			// all pages to 72 dpi would hide fine Retro artwork differences.
			output, err := exec.CommandContext(t.Context(), binary, "-f", page, "-l", page, "-singlefile", "-scale-to-x", strconv.Itoa(bounds.Dx()), "-scale-to-y", strconv.Itoa(bounds.Dy()), "-png", filepath.Join(root, "native-media-proof.pdf"), prefix).CombinedOutput()
			if err != nil {
				t.Fatalf("independent reader failed: %v: %s", err, output)
			}
			actual := readStructuredPNG(t, prefix+".png")
			if actual.Bounds() != bounds {
				t.Fatal("independent reader changed page dimensions", actual.Bounds(), bounds)
			}
			forward, reverse := structuredPixelMismatch(actual, reference), structuredPixelMismatch(reference, actual)
			limit := bounds.Dx() * bounds.Dy() * 3 / 1000
			t.Logf("independent-reader raw mismatches: %d/%d (limit %d)", forward, reverse, limit)
			if item.Quality {
				// Use the same source-ink edge contract as quality/exact checks,
				// not a larger pixel budget. Raw interpolation counts stay visible.
				palette := []pdfRGB{{16, 24, 40}, {230, 50, 70}, {40, 190, 100}, {40, 100, 230}, {240, 190, 30}, {255, 255, 255}}
				forward, reverse = pdfEdgeMismatch(actual, reference, palette), pdfEdgeMismatch(reference, actual, palette)
				t.Logf("independent-reader ink mismatches: %d/%d (limit %d)", forward, reverse, limit)
			}
			if forward > limit || reverse > limit {
				t.Errorf("independent reader changed media appearance: %d/%d", forward, reverse)
			}
		})
	}
}

func TestMediaPDFNativePixels(t *testing.T) {
	root := os.Getenv("KEYNOPE_MEDIA_PDF_PIXELS")
	if root == "" {
		t.Skip("native media captures not requested")
	}
	raw, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []mediaPDFCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, item := range cases {
		width, height := item.Width, item.Height
		if width == 0 {
			width, height = 1920, 1080
		} // Existing proof archives.
		exact := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-exact-%d.png", i+1)))
		original := exact
		if exact.Bounds() == image.Rect(0, 0, width*2, height*2) {
			// WKWebView snapshots retain the screen's 2x backing scale even
			// with an explicit snapshot width. Compare at the PDF's logical size.
			normalized := image.NewNRGBA(image.Rect(0, 0, width, height))
			xdraw.ApproxBiLinear.Scale(normalized, normalized.Bounds(), exact, exact.Bounds(), xdraw.Src, nil)
			exact = normalized
		}
		if exact.Bounds() != image.Rect(0, 0, width, height) {
			t.Fatal("exact media capture dimensions changed", i, exact.Bounds())
		}
		// Every page has coloured image/background content well below its title.
		// This independently rejects blank fallback captures, not just equal PDFs.
		colourful := 0
		for y := height * 200 / 1080; y < height*1000/1080; y += 4 {
			for x := width * 40 / 1920; x < width*1880/1920; x += 4 {
				rgb := pdfPixel(exact.At(x, y))
				if math.Max(rgb[0], math.Max(rgb[1], rgb[2]))-math.Min(rgb[0], math.Min(rgb[1], rgb[2])) > 50 && math.Max(rgb[0], math.Max(rgb[1], rgb[2])) > 70 {
					colourful++
				}
			}
		}
		if colourful < 1000 {
			t.Fatal("media missing from exact capture", i+1, item.Title, colourful)
		}
		if item.Title == "ANIMATION / FROZEN FIRST FRAME" || item.Title == "RETRO ANIMATION / FROZEN FRAME" {
			// The owned GIF starts pink and then turns blue. Assembly equality
			// alone cannot prove that export froze the correct animation frame.
			// Sample inside the opaque centre, away from its mask and alpha edge.
			centre := image.Pt(width*120/245, height*30/56)
			for y := centre.Y - 2; y <= centre.Y+2; y++ {
				for x := centre.X - 2; x <= centre.X+2; x++ {
					rgb := pdfPixel(exact.At(x, y))
					if rgb[0] < 220 || rgb[0]-rgb[2] < 40 || rgb[0]-rgb[1] < 80 {
						t.Fatalf("animation did not freeze its pink first frame on page %d at %d,%d: %v", i+1, x, y, rgb)
					}
				}
			}
		}
		assembled := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-assembled-%d.png", i+1)))
		reference := original
		if item.Quality {
			reference = readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-quality-%d.png", i+1)))
		}
		if assembled.Bounds() != reference.Bounds() {
			t.Fatal("assembled media page dimensions changed", i+1, assembled.Bounds(), reference.Bounds())
		}
		// Assembly must preserve the actual selected per-page capture, including
		// raster-only retro/effect/animation pages; titles are included here.
		assemblyLimit := reference.Bounds().Dx() * reference.Bounds().Dy() * 3 / 1000
		f, r := structuredPixelMismatch(assembled, reference), structuredPixelMismatch(reference, assembled)
		t.Logf("assembled page %d: %d/%d (limit %d)", i+1, f, r, assemblyLimit)
		if f > assemblyLimit || r > assemblyLimit {
			t.Errorf("PDF assembly changed page %d: %d/%d", i+1, f, r)
		}
		if !item.Quality {
			continue
		}
		quality := readStructuredPNG(t, filepath.Join(root, fmt.Sprintf("native-quality-%d.png", i+1)))
		if quality.Bounds() != exact.Bounds() {
			t.Fatal("quality media capture dimensions changed", i, quality.Bounds())
		}
		// Ignore title font-AA differences; compare all image/background pixels.
		region := image.Rect(0, height*160/1080, width, height)
		a := quality.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(region)
		b := exact.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(region)
		forward, reverse := structuredPixelMismatch(a, b), structuredPixelMismatch(b, a)
		limit := region.Dx() * region.Dy() * 3 / 1000
		palette := []pdfRGB{{16, 24, 40}, {230, 50, 70}, {40, 190, 100}, {40, 100, 230}, {240, 190, 30}}
		edgeForward, edgeReverse := pdfEdgeMismatch(a, b, palette), pdfEdgeMismatch(b, a, palette)
		t.Logf("page %d %s: raw %d/%d; edge-aware %d/%d (limit %d)", i+1, item.Title, forward, reverse, edgeForward, edgeReverse, limit)
		// Preserve raw counts: upscaled low-resolution source images use
		// different interpolation in PDFKit and the snapshot compositor. Only
		// local coverage of the fixture's known inks may forgive those edges.
		if edgeForward > limit || edgeReverse > limit {
			t.Errorf("native PDF image/crop/mask appearance differs on %s", item.Title)
		}
		if os.Getenv("KEYNOPE_MEDIA_PDF_STRICT_PIXELS") != "" && (forward > limit || reverse > limit) {
			t.Errorf("strict raw pixel comparison differs on %s", item.Title)
		}
		for _, control := range []string{"erased", "shifted", "recoloured"} {
			mutated := image.NewNRGBA(region)
			draw.Draw(mutated, region, &image.Uniform{color.NRGBA{16, 24, 40, 255}}, image.Point{}, draw.Src)
			if control != "erased" {
				origin := region.Min
				if control == "shifted" {
					origin.X -= 20
				}
				draw.Draw(mutated, region, b, origin, draw.Src)
			}
			if control == "recoloured" {
				changed := 0
				for y := region.Min.Y; y < region.Max.Y && changed < 12000; y++ {
					for x := region.Min.X; x < region.Max.X && changed < 12000; x++ {
						if !pdfNear(pdfPixel(b.At(x, y)), palette[0]) {
							mutated.SetNRGBA(x, y, color.NRGBA{255, 0, 255, 255})
							changed++
						}
					}
				}
			}
			f, r := pdfEdgeMismatch(mutated, b, palette), pdfEdgeMismatch(b, mutated, palette)
			if f <= limit && r <= limit {
				t.Errorf("visual comparator accepted %s media on page %d: %d/%d", control, i+1, f, r)
			}
		}
	}
}

func mediaPDFDeck(t *testing.T) (Deck, []mediaPDFCase) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	palette := []color.NRGBA{{230, 50, 70, 255}, {40, 190, 100, 255}, {40, 100, 230, 255}, {240, 190, 30, 255}}
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			value := palette[x/64+2*(y/32)]
			if x < 4 || x >= 124 || y < 4 || y >= 60 {
				value.A = 110
			}
			img.SetNRGBA(x, y, value)
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	id, asset, source, err := embeddedImageAsset(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	corpus := powerupProofCorpus(t)["media.md"]
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}}
	for key, value := range corpus.Assets {
		d.Assets[key] = value
	}
	var cases []mediaPDFCase
	add := func(title string, quality bool, elements []Element, background *slideBackgroundMedia) {
		index := len(cases)
		s := Slide{ID: fmt.Sprintf("media-proof-%d", index), BG: "48;2;16;24;40", BGSet: true, PageNumber: pageNumberHide, Notes: "PRIVATE MEDIA NOTES", BackgroundMedia: background, BackgroundMediaSet: background != nil}
		s.Elements = append([]Element{{ID: fmt.Sprintf("title-%d", index), Kind: "text", Text: title, Query: "render=truetype&modern-font=sans&modern-size=56&left=10&top=2&width=225&height=6&fg=%23ffffff"}}, elements...)
		d.Slides = append(d.Slides, s)
		cases = append(cases, mediaPDFCase{Title: title, Quality: quality})
	}
	picture := func(index int, query string) Element {
		return Element{ID: fmt.Sprintf("image-%d", index), Kind: "image", AssetID: id, Path: source, Query: query}
	}
	for _, fit := range []string{"contain", "cover", "stretch"} {
		var elements []Element
		for i, mask := range []string{"rect", "rounded", "ellipse"} {
			elements = append(elements, picture(i, fmt.Sprintf("image-style=modern&image-fit=%s&modern-mask=%s&left=%d&top=12&width=65&height=35", fit, mask, 10+i*80)))
		}
		add("MODERN "+fit+" / rect, rounded, ellipse", true, elements, nil)
	}
	add("CROP / ROTATION / OPACITY / SHADOW", true, []Element{
		picture(0, "image-style=modern&image-fit=cover&modern-crop=0.2,0.1,0,0.1&modern-mask=ellipse&left=20&top=14&width=80&height=30&object-rotation=12"),
		picture(1, "image-style=modern&image-fit=cover&modern-mask=rounded&left=130&top=14&width=80&height=30&modern-opacity=0.55&shadow=soft&shadow-color=%23000000&shadow-x=2&shadow-y=2"),
	}, nil)
	vector := corpus.Slides[0].Elements[1]
	vector.Query = "image-style=modern&image-fit=contain&modern-mask=rounded&left=30&top=12&width=180&height=36"
	add("PASSIVE SVG / ROUNDED MASK", true, []Element{vector}, nil)
	for _, glyph := range []string{"blocks", "braille"} {
		add("RETRO "+glyph+" / CROP / MASK", false, []Element{picture(0, "image-style=retro&glyph="+glyph+"&image-fit=cover&modern-crop=0.1,0,0.1,0&modern-mask=ellipse&left=20&top=12&width=200&height=36")}, nil)
	}
	add("COLOUR ADJUSTMENTS / EXACT FALLBACK", false, []Element{picture(0, "image-style=modern&image-fit=cover&left=20&top=12&width=200&height=36&brightness=0.8&contrast=1.2&saturation=0.5")}, nil)
	animation := corpus.Slides[0].Elements[2]
	animation.Query = "image-style=modern&image-fit=contain&modern-mask=ellipse&left=20&top=12&width=200&height=36"
	add("ANIMATION / FROZEN FIRST FRAME", false, []Element{animation}, nil)
	add("BACKGROUND / CROP / OPACITY", true, nil, &slideBackgroundMedia{AssetID: id, Fit: "cover", Opacity: .6, OpacitySet: true, Query: "modern-crop=0.1,0,0.1,0"})
	add("BACKGROUND ADJUSTMENTS / EXACT FALLBACK", false, nil, &slideBackgroundMedia{AssetID: id, Fit: "contain", Opacity: .6, OpacitySet: true, Query: "modern-crop=0.1,0,0.1,0&brightness=0.7"})
	add("EDITED NATURAL CROP / FIXED FRAME", true, []Element{picture(0, "image-style=modern&align=center&top=12&width=180&height=35")}, nil)
	for _, glyph := range []string{"ascii", "dense"} {
		add("RETRO "+glyph+" / CROP / MASK", false, []Element{picture(0, "image-style=retro&glyph="+glyph+"&image-fit=cover&modern-crop=0.1,0,0.1,0&modern-mask=rounded&left=20&top=12&width=200&height=36")}, nil)
	}
	animation.Query = "image-style=retro&glyph=blocks&image-fit=cover&modern-mask=ellipse&left=20&top=12&width=200&height=36"
	add("RETRO ANIMATION / FROZEN FRAME", false, []Element{animation}, nil)
	add("IMAGE GRADIENT / OUTLINE / SHARPENING", false, []Element{picture(0, "image-style=modern&image-fit=cover&modern-mask=rounded&left=20&top=12&width=200&height=36&gradient-start=%23ff0055&gradient-end=%2355ffff&gradient-dir=diagonal&outline=light&sharpness=1.4")}, nil)
	add("BACKGROUND / ROUNDED MASK", true, nil, &slideBackgroundMedia{AssetID: id, Fit: "contain", Opacity: 1, OpacitySet: true, Query: "modern-mask=rounded&modern-crop=0.1,0,0.1,0"})
	add("RETRO BACKGROUND / CROP / MASK", false, nil, &slideBackgroundMedia{AssetID: id, Fit: "cover", Opacity: 1, OpacitySet: true, Query: "image-style=retro&glyph=blocks&modern-mask=ellipse&modern-crop=0.1,0,0.1,0"})
	add("INTERLEAVED PICTURES / VECTOR / TEXT", true, []Element{
		picture(0, "image-style=modern&image-fit=stretch&left=10&top=12&width=130&height=35"),
		{ID: "middle-shape", Kind: "shape", Text: "[shape:square]", Query: "shape=square&left=60&top=18&width=90&height=26&fg=%2328be64"},
		{ID: "middle-text", Kind: "text", Text: "TEXT BETWEEN PICTURES", Query: "render=truetype&modern-font=sans&modern-size=52&left=25&top=25&width=175&height=8&fg=%23ffffff"},
		picture(1, "image-style=modern&image-fit=stretch&modern-mask=rounded&left=110&top=16&width=120&height=34&modern-opacity=0.55"),
	}, &slideBackgroundMedia{AssetID: id, Fit: "cover", Opacity: .4, OpacitySet: true})
	return d, cases
}

func TestMediaPDFClassificationAndFixture(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d, cases := mediaPDFDeck(t)
	switch os.Getenv("KEYNOPE_MEDIA_PDF_PAGE_SIZE") {
	case "portrait":
		d.PageSize = &DeckPageSize{Version: 1, Width: 1080, Height: 1920}
	case "ultrawide":
		d.PageSize = &DeckPageSize{Version: 1, Width: 2560, Height: 1080}
	}
	width, height := d.pageDimensions()
	for i := range cases {
		cases[i].Width, cases[i].Height = int(width), int(height)
		if cases[i].Title == "EDITED NATURAL CROP / FIXED FRAME" {
			if _, err := applySceneCrop(&d, nativeEditorAction{Slide: i, ObjectID: "image-0", Crop: &sceneCrop{Left: .3, Top: .1}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	document, err := makePDFExportDocument("Media proof.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Manifest.Pages) != len(cases) {
		t.Fatal("media fixture changed page count")
	}
	for i, item := range cases {
		if document.Manifest.Pages[i].Quality != item.Quality {
			t.Fatal("unexpected media quality/fallback classification", item, document.Manifest.Pages[i].Reasons)
		}
	}
	if root := os.Getenv("KEYNOPE_MEDIA_PDF_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(cases)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"quality.html": []byte(document.Content), "raster.html": []byte(document.FallbackContent), "expected.json": expected} {
			if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
