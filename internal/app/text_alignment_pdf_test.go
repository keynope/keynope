package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Reuse the actual master-bound property-command fixtures, rather than author
// a second set of PDF-specific alignment flags. Every aspect contains the same
// fifteen pages and all twelve horizontal/vertical combinations on each page.
func TestTextAlignmentNativePDFFixtures(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	root := os.Getenv("KEYNOPE_ALIGNMENT_PDF_PROOF")
	for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			deck := Deck{PageSize: size.Clone(), Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base"}}}
			var kinds, fonts []string
			for _, kind := range []string{"text", "heading", "bullet", "code", "shape"} {
				for _, font := range []string{"c64", "mono", "sans"} {
					s := alignmentOutputFixture(t, kind, font)
					layout := s.deck.Masters.Layouts[0]
					layout.ID = kind + "-" + font
					if size.Height > size.Width {
						// Twelve 42px labels need two columns on portrait paper.
						// Four narrow columns legitimately trigger shape auto-growth,
						// overlapping neighbours and clipping the rightmost label.
						// Keep all alignments/fonts/sizes, but author fitting frames.
						for index := range layout.Slide.Elements {
							q, err := url.ParseQuery(layout.Slide.Elements[index].Query)
							if err != nil {
								t.Fatal(err)
							}
							q.Set("left", fmt.Sprint(5+index%2*120))
							q.Set("top", fmt.Sprint(2+index/2*9))
							q.Set("width", "110")
							q.Set("height", "7")
							layout.Slide.Elements[index].Query = q.Encode()
						}
					}
					slide := s.deck.Slides[0]
					slide.ID, slide.LayoutID = layout.ID, layout.ID
					deck.Masters.Layouts = append(deck.Masters.Layouts, layout)
					deck.Slides = append(deck.Slides, slide)
					kinds, fonts = append(kinds, kind), append(fonts, font)
				}
			}
			before := cloneDeck(deck)
			document, err := makePDFExportDocument("Alignment.md", deck, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			if len(document.Manifest.Pages) != 15 || !reflect.DeepEqual(deck, before) {
				t.Fatal("PDF alignment projection mutated source or changed page count")
			}
			for index, page := range document.Manifest.Pages {
				if !page.Quality || page.Width != float64(size.Width) || page.Height != float64(size.Height) {
					t.Fatal("Alignment page lost selectable quality classification or physical dimensions", index, page.Reasons)
				}
				checkAlignmentOutputScene(t, "native PDF fixture", page.Page.Scene, kinds[index], fonts[index])
				for i, object := range page.Page.Scene.Objects {
					b := object.Bounds
					if b.X < 0 || b.Y < 0 || b.X+b.Width > page.Width || b.Y+b.Height > page.Height {
						t.Fatal("alignment proof clips an authored owner", index, object.ID, b)
					}
					for _, neighbour := range page.Page.Scene.Objects[:i] {
						n := neighbour.Bounds
						if b.X < n.X+n.Width && b.X+b.Width > n.X && b.Y < n.Y+n.Height && b.Y+b.Height > n.Y {
							t.Fatal("alignment proof has overlapping owners", index, object.ID, neighbour.ID)
						}
					}
				}
			}
			if strings.Contains(document.Content, "ALIGNMENT_PRIVATE") || strings.Contains(document.FallbackContent, "ALIGNMENT_PRIVATE") {
				t.Fatal("Private alignment fixture metadata reached PDF HTML")
			}
			if root == "" {
				return
			}
			dir := filepath.Join(root, fmt.Sprintf("%dx%d", size.Width, size.Height))
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			manifest, err := json.Marshal(document.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"quality.html": []byte(document.Content), "raster.html": []byte(document.FallbackContent), "manifest.json": manifest} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
