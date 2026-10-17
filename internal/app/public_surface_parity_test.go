package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Only editing affordances are normalized, never object geometry, artwork,
// typography, source media, scene diagnostics or timeline behavior.
func publicParityScene(scene slideScene) slideScene {
	scene.Revision, scene.Conversion = nil, nil
	scene.SlideIndex, scene.Master = 0, false
	scene.Objects = append([]sceneObject(nil), scene.Objects...)
	for i := range scene.Objects {
		o := &scene.Objects[i]
		o.Editable, o.EditRuns, o.TextCapabilities = false, nil, nil
		if o.Kind == "shape" && o.Label != nil {
			empty := true
			for _, run := range o.Label.Runs {
				empty = empty && run.Text == ""
			}
			if empty && len(o.Label.Paragraphs) == 0 {
				o.Label, o.LabelPaint = nil, nil // Editor's uncommitted blank label draft.
			}
		}
	}
	return scene
}

type publicSurfaceBrowserCase struct {
	File   string       `json:"file"`
	Scenes []slideScene `json:"scenes"`
}

func TestPowerupPublicSurfaceSceneParity(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	var browserCases []publicSurfaceBrowserCase
	var manifest struct {
		Decks []struct {
			File  string
			Pages int
		}
	}
	raw, err := os.ReadFile(testRepositoryPath("testdata/powerup/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Decks {
		t.Run(entry.File, func(t *testing.T) {
			file := testRepositoryPath("testdata", "powerup", entry.File)
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData(file, raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, size := range []DeckPageSize{{Version: 1, Width: 1920, Height: 1080}, {Version: 1, Width: 1080, Height: 1920}, {Version: 1, Width: 2560, Height: 1080}} {
				t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
					deck := cloneDeck(loaded)
					deck.PageSize = size.Clone()
					for i := range deck.Slides {
						deck.Slides[i].Notes = "SURFACE_PRIVATE_NOTES"
					}
					deck.Extra = &jsonExtensions{"private": json.RawMessage(`"SURFACE_PRIVATE_EXTENSION"`)}
					s := newNativeEditorSession(file, deck)
					before := cloneDeck(s.deck)
					presenter := projectExportDeck(file, s.deck.ResolvedSlides(), 245, 56, true)
					standalone := projectExportDeck(file, s.deck.ResolvedSlides(), 245, 56, false)
					_, pdf, err := projectPDFExport(file, s.deck, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					if len(presenter.Pages) != entry.Pages || len(standalone.Pages) != entry.Pages || len(pdf.Pages) != entry.Pages {
						t.Fatal("public projection changed corpus page count")
					}
					for i := range s.deck.Slides {
						w := httptest.NewRecorder()
						s.handleScene(w, httptest.NewRequest("GET", fmt.Sprintf("/api/editor/scene?slide=%d", i), nil))
						if w.Code != 200 {
							t.Fatal(w.Code, w.Body.String())
						}
						var editor slideScene
						if err := json.Unmarshal(w.Body.Bytes(), &editor); err != nil {
							t.Fatal(err)
						}
						participant, err := participantRenderedDeck(s.deck, i)
						if err != nil {
							t.Fatal(err)
						}
						want := publicParityScene(editor)
						for name, page := range map[string]exportPage{"presenter": presenter.Pages[i], "standalone": standalone.Pages[i], "participant": participant.Pages[0], "pdf-input": pdf.Pages[i]} {
							if name != "presenter" {
								checkAudiencePage(t, page)
							}
							if page.Scene == nil {
								t.Fatal(name, "missing shared scene")
							}
							actual := publicParityScene(*page.Scene)
							// Normalize the transport JSON's nil/empty representation only
							// by comparing wire values on both sides, not zeroing fields.
							a, err := json.Marshal(actual)
							if err != nil {
								t.Fatal(name, "invalid public scene JSON", err)
							}
							b, err := json.Marshal(want)
							if err != nil {
								t.Fatal("invalid editor scene JSON", err)
							}
							if string(a) != string(b) {
								at := 0
								for at < len(a) && at < len(b) && a[at] == b[at] {
									at++
								}
								t.Fatalf("%s slide %d differs at byte %d: actual=%s editor=%s", name, i, at, a[max(0, at-80):min(len(a), at+180)], b[max(0, at-80):min(len(b), at+180)])
							}
							wire, err := json.Marshal(page)
							if err != nil {
								t.Fatal(name, "invalid page JSON", err)
							}
							for _, secret := range []string{"SURFACE_PRIVATE_NOTES", "SURFACE_PRIVATE_EXTENSION"} {
								if strings.Contains(string(wire), secret) {
									t.Fatal(name, "private authoring metadata leaked")
								}
							}
						}
					}
					if !reflect.DeepEqual(s.deck, before) {
						t.Fatal("public projection mutated editor deck")
					}
					if root := os.Getenv("KEYNOPE_SURFACE_EVIDENCE"); root != "" && (size.Width == 1920 || os.Getenv("KEYNOPE_SURFACE_ALL_ASPECTS") == "1") {
						if err := os.MkdirAll(root, 0700); err != nil {
							t.Fatal(err)
						}
						file := strings.TrimSuffix(entry.File, ".md") + ".html"
						if os.Getenv("KEYNOPE_SURFACE_ALL_ASPECTS") == "1" {
							file = fmt.Sprintf("%s-%dx%d.html", strings.TrimSuffix(entry.File, ".md"), size.Width, size.Height)
						}
						html, err := exportProjectedHTML(standalone, preservedExportHead{}, false)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(root, file), []byte(html), 0600); err != nil {
							t.Fatal(err)
						}
						var scenes []slideScene
						// Compare the filtered output with the original projection,
						// not a second copy of its own sanitized JSON.
						for _, page := range presenter.Pages {
							scenes = append(scenes, *page.Scene)
						}
						browserCases = append(browserCases, publicSurfaceBrowserCase{file, scenes})
					}
				})
			}
		})
	}
	if root := os.Getenv("KEYNOPE_SURFACE_EVIDENCE"); root != "" {
		raw, err := json.Marshal(browserCases)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "cases.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
