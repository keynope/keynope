package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPageSizePDFManifestAndOwnedFixtures(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	output := os.Getenv("KEYNOPE_PAGE_SIZE_PDF_PROOF")
	for _, page := range []DeckPageSize{{1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		title := fmt.Sprintf("Page %d x %d", page.Width, page.Height)
		deck := Deck{PageSize: page.Clone(), Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "quality", Notes: "PRIVATE PAGE-SIZE NOTES", BG: "48;2;238;243;248", Elements: []Element{
			{ID: "title", Kind: "text", Text: title, Query: "render=truetype&modern-font=sans&modern-size=48&align=center&top=8"},
			{ID: "body", Kind: "text", Text: "Same physical font size.\nEvery edge remains visible.", Query: "render=truetype&modern-font=mono&modern-size=30&align=center&top=20"},
			{ID: "top-left", Kind: "shape", Query: "shape=square&left=0&top=0&width=10&height=3&fg=%23ff0000"},
			{ID: "top-right", Kind: "shape", Query: "shape=square&right=0&top=0&width=10&height=3&fg=%2300aa00"},
			{ID: "bottom-left", Kind: "shape", Query: "shape=square&left=0&bottom=0&width=10&height=3&fg=%230055ff"},
			{ID: "bottom-right", Kind: "shape", Query: "shape=square&right=0&bottom=0&width=10&height=3&fg=%23ffcc00"},
		}}}}
		deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
		deck.Slides[1].ID = "fallback"
		deck.Slides[1].Effect = "warp"
		deck.Slides = append(deck.Slides, Slide{ID: "private-tab", TabID: "private", Elements: []Element{{Kind: "text", Text: "PRIVATE PAGE-SIZE TAB"}}})
		before := cloneDeck(deck)
		document, err := makePDFExportDocument("Custom pages.md", deck, 245, 56)
		if err != nil || !reflect.DeepEqual(deck, before) {
			t.Fatal("PDF projection failed or changed source", err)
		}
		if len(document.Manifest.Pages) != 2 || !document.Manifest.Pages[0].Quality || document.Manifest.Pages[1].Quality {
			t.Fatal("wrong page count/quality classification")
		}
		for _, p := range document.Manifest.Pages {
			if p.Width != float64(page.Width) || p.Height != float64(page.Height) || p.Page.Scene == nil || p.Page.Scene.Width != p.Width || p.Page.Scene.Height != p.Height {
				t.Fatal("PDF manifest lost authored size", page, p.Width, p.Height)
			}
			for _, object := range p.Page.Scene.Objects {
				if object.Text != nil && object.ID == "title" && object.Text.Size != 48 {
					t.Fatal("export scaled font rather than page")
				}
			}
		}
		for _, html := range []string{document.Content, document.FallbackContent} {
			if strings.Contains(html, "PRIVATE PAGE-SIZE") {
				t.Fatal("PDF includes private authoring content")
			}
		}
		if output == "" {
			continue
		}
		dir := filepath.Join(output, fmt.Sprintf("%dx%d", page.Width, page.Height))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string]string{"quality.html": document.Content, "exact.html": document.FallbackContent} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		manifest, err := json.Marshal(document.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPageSizePDFRenderedCorners(t *testing.T) {
	root := os.Getenv("KEYNOPE_PAGE_SIZE_PDF_PROOF")
	if root == "" {
		t.Skip("optional rendered PDF/capture check")
	}
	for _, size := range [][2]int{{1440, 1080}, {1080, 1920}, {2560, 1080}} {
		for _, name := range []string{"native-render-1.png", "native-render-2.png", "browser-render-1.png", "browser-render-2.png", "native-exact.png"} {
			file := filepath.Join(root, fmt.Sprintf("%dx%d", size[0], size[1]), name)
			f, err := os.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			b := img.Bounds()
			if math.Abs(float64(b.Dx())/float64(b.Dy())-float64(size[0])/float64(size[1])) > .005 {
				t.Fatal("rendered page aspect changed", file, b)
			}
			for _, corner := range []struct {
				x, y    float64
				r, g, b int
			}{{.01, .01, 255, 0, 0}, {.99, .01, 0, 170, 0}, {.01, .99, 0, 85, 255}, {.99, .99, 255, 204, 0}} {
				x, y := int(float64(b.Dx())*corner.x), int(float64(b.Dy())*corner.y)
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						r, g, blue, a := img.At(x+dx, y+dy).RGBA()
						if abs(int(r>>8)-corner.r) > 35 || abs(int(g>>8)-corner.g) > 35 || abs(int(blue>>8)-corner.b) > 35 || a < 65000 {
							t.Fatal("PDF/capture clipped or displaced corner", file, corner.x, corner.y, r>>8, g>>8, blue>>8)
						}
					}
				}
			}
		}
	}
}
