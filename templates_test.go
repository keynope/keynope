package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestTemplatePreviewsMatchProductionRenderer(t *testing.T) {
	useTestAuthoredSize(t, 80, 25)
	beforeOrder := parsedDeckElementOrderChanged
	generated, err := generateTemplatePreviews()
	if err != nil {
		t.Fatal(err)
	}
	if authoredTerminalWidth != 80 || authoredTerminalHeight != 25 || beforeOrder != parsedDeckElementOrderChanged {
		t.Fatal("offline preview generator did not restore parser state")
	}
	stored, err := presentationTemplates.ReadFile("assets/templates/previews.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(stored), generated) {
		t.Fatal("production template previews are stale; run go run -tags keynope_sitegen . --template-previews assets/templates/previews.json")
	}
}

func TestTemplatesArePortableAndIndependentlyOwned(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, template := range presentationTemplateCatalog {
		t.Run(template.ID, func(t *testing.T) {
			d, err := loadPresentationTemplate(template.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Slides) == 0 || d.Theme == nil || d.Theme.Validate() != nil || len(d.Assets) != 0 || len(d.Tabs) != 0 {
				t.Fatal("template is missing a portable theme/slide or includes unrelated state")
			}
			for i, slide := range d.Slides {
				if slide.Notes != "" || slide.Engagement != nil || slide.EngagementResult != nil {
					t.Fatal("template contains private/workshop state")
				}
				scene, err := buildSlideScene(d, i, 245, 56)
				if err != nil || scene.Background != d.Theme.Colors.Background {
					t.Fatalf("theme scene: %v %+v", err, scene)
				}
				for _, object := range scene.Objects {
					if object.Bounds.X < 0 || object.Bounds.Y < 0 || object.Bounds.X+object.Bounds.Width > scene.Width+.01 || object.Bounds.Y+object.Bounds.Height > scene.Height+.01 {
						t.Fatalf("template object leaves canvas: %+v", object)
					}
				}
			}
			data, err := serializeDeck("Untitled.md", d)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("another/location/deck.md", data)
			if err != nil || !reflect.DeepEqual(d.Theme, loaded.Theme) || len(loaded.Slides) != len(d.Slides) {
				t.Fatalf("portable round trip failed: %v", err)
			}
			if !reflect.DeepEqual(d.Masters, loaded.Masters) {
				t.Fatal("template master definitions changed after save/reopen")
			}
			for i := range d.Slides {
				before, err := buildSlideScene(d, i, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				after, err := buildSlideScene(loaded, i, 245, 56)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("template scene changed after portable save/reopen: page %d: %v", i+1, err)
				}
			}
			// Selecting the template again must not inherit edits made to the
			// first instance or alter the embedded source document.
			original, err := templateMarkdown(template.ID)
			if err != nil {
				t.Fatal(err)
			}
			loaded.Theme.Colors.Accent[0] = "#abcdef"
			if loaded.Theme.Colors.Accent[0] == d.Theme.Colors.Accent[0] {
				t.Fatal("template instances share mutable theme")
			}
			fresh, err := loadPresentationTemplate(template.ID)
			if err != nil || !reflect.DeepEqual(fresh.Theme, d.Theme) || !reflect.DeepEqual(fresh.Masters, d.Masters) {
				t.Fatal("new template instance inherited edits", err)
			}
			unchanged, err := templateMarkdown(template.ID)
			if err != nil || !bytes.Equal(original, unchanged) {
				t.Fatal("embedded source template mutated", err)
			}
		})
	}
}

func TestTemplateCatalogIsReadOnlyConcurrentAndCached(t *testing.T) {
	useTestAuthoredSize(t, 96, 32)
	s := viewCommandFixture(4, false)
	before, beforeHistory, beforeVersion := cloneDeck(s.deck), len(s.undo), s.version
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			w := httptest.NewRecorder()
			s.handleTemplates(w, httptest.NewRequest(http.MethodGet, "/api/editor/templates", nil))
			var catalog []presentationTemplate
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog) != 7 {
				t.Error("invalid template catalog response")
			}
		}()
	}
	workers.Wait()
	if !reflect.DeepEqual(before, s.deck) || len(s.undo) != beforeHistory || s.version != beforeVersion || authoredTerminalWidth != 96 || authoredTerminalHeight != 32 {
		t.Fatal("previewing templates changed current deck/history/layout")
	}
	for _, test := range []struct {
		method, url string
		status      int
	}{
		{"GET", "/api/editor/templates?id=blank", 200},
		{"GET", "/api/editor/templates?id=../../.env.private", 404},
		{"GET", "/api/editor/templates?id=unknown", 404},
		{"POST", "/api/editor/templates", 405},
	} {
		w := httptest.NewRecorder()
		s.handleTemplates(w, httptest.NewRequest(test.method, test.url, nil))
		if w.Code != test.status {
			t.Fatalf("%s %s returned %d", test.method, test.url, w.Code)
		}
		if test.status == 200 && !strings.Contains(w.Body.String(), "keynope-theme") {
			t.Fatal("template source is not a normal Markdown deck")
		}
	}
}

func TestTemplateArgumentsAndEmptySlideIdentity(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	args, ok := parseArgs([]string{"--app", "--template=blank", "app/Welcome.md"})
	if !ok || !args.Untitled || args.TemplateID != "blank" {
		t.Fatal("template does not create Untitled")
	}
	for _, raw := range [][]string{
		{"--app", "--template=bad", "deck.md"},
		{"--app", "--template=blank", "--template=bold", "deck.md"},
		{"--app", "--template=blank", "--import-pptx", "deck.pptx"},
	} {
		if _, ok := parseArgs(raw); ok {
			t.Fatal("invalid template arguments accepted")
		}
	}
	d, err := parseDeckData("Untitled.md", []byte("<!-- keynope-slide-id=slide-empty -->\n"))
	if err != nil || len(d.Slides) != 1 || d.Slides[0].ID != "slide-empty" {
		t.Fatalf("empty authored slide lost identity: %v %+v", err, d.Slides)
	}
}
