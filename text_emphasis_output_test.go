package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

// Keep the eight authored combinations, rather than comparing two renderers
// which could both have forgotten to apply the new properties.
func TestSparseTextEmphasisPublicAndPPTXOutputs(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	var cases []publicSurfaceBrowserCase
	root := os.Getenv("KEYNOPE_EMPHASIS_OUTPUT_PROOF")
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		t.Run(kind, func(t *testing.T) {
			d := inheritedEmphasisFixture(t, true)
			d.Masters.Layouts[0].Slide.Elements[0].Kind = kind
			prototype := cloneSlide(d.Slides[0])
			d.Slides = nil
			for mask := 0; mask < 8; mask++ {
				slide := cloneSlide(prototype)
				slide.ID = fmt.Sprintf("combination-%d", mask)
				slide.Notes = "EMPHASIS_PRIVATE_NOTES"
				slide.Elements[0].Kind = kind
				d.Slides = append(d.Slides, slide)
			}
			d.Extra = extensionFixture("private", `"EMPHASIS_PRIVATE_EXTENSION"`)
			s := newNativeEditorSession("Outputs.md", d)
			for mask := 0; mask < 8; mask++ {
				if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: mask}); err != nil {
					t.Fatal(err)
				}
				fields := map[string]ObjectStyleField{}
				for bit, property := range []string{"text.bold", "text.italic", "text.underline"} {
					fields[property] = ObjectStyleField{Present: true, Value: fmt.Sprint(mask&(1<<bit) != 0)}
				}
				revision := s.version
				if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: mask, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, StyleData: &ObjectStyleSnapshot{Version: 1, Fields: fields}}); err != nil {
					t.Fatal(err)
				}
			}
			before := cloneDeck(s.deck)
			presenter := projectExportDeck("Outputs.md", s.deck.ResolvedSlides(), 245, 56, true)
			standalone := projectExportDeck("Outputs.md", s.deck.ResolvedSlides(), 245, 56, false)
			_, pdf, err := projectPDFExport("Outputs.md", s.deck, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			var scenes []slideScene
			for mask := 0; mask < 8; mask++ {
				check := func(name string, scene *slideScene) {
					t.Helper()
					if scene == nil {
						t.Fatal(name, "missing scene")
					}
					found := false
					for _, object := range scene.Objects {
						if object.ID == "child-text" {
							found = true
							checkEmphasisOutputText(t, name, object.Text, mask)
						}
					}
					if !found {
						t.Fatal(name, "missing editable text")
					}
					wire, err := json.Marshal(scene)
					if err != nil || bytes.Contains(wire, []byte("EMPHASIS_PRIVATE")) {
						t.Fatal(name, "invalid/private output", err)
					}
				}
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", fmt.Sprintf("/api/editor/scene?slide=%d", mask), nil))
				var editor slideScene
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &editor) != nil {
					t.Fatal(w.Code, w.Body.String())
				}
				check("editor", &editor)
				participant, err := participantRenderedDeck(s.deck, mask)
				if err != nil {
					t.Fatal(err)
				}
				for name, page := range map[string]exportPage{"presenter": presenter.Pages[mask], "standalone": standalone.Pages[mask], "participant": participant.Pages[0], "pdf-input": pdf.Pages[mask]} {
					check(name, page.Scene)
				}
				md, err := participantSlideMarkdown(s.deck, mask)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(md, []byte("EMPHASIS_PRIVATE")) {
					t.Fatal("legacy participant Markdown leaked private data")
				}
				reopened, err := parseDeckData("Participant.md", md)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := buildDocumentSlideScene(reopened, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				check("legacy-participant", &restored)
				scenes = append(scenes, *standalone.Pages[mask].Scene)
			}
			data, _, err := exportPPTXDocument(s.deck)
			if err != nil {
				t.Fatal(err)
			}
			native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
			if err != nil || len(native.Slides) != 8 {
				t.Fatal("native editable package readback", err)
			}
			for mask, slide := range native.Slides {
				found := false
				for _, object := range slide.Objects {
					if len(object.Paragraphs) == 0 {
						continue
					}
					found = true
					var text strings.Builder
					for _, p := range object.Paragraphs {
						for _, run := range p.Runs {
							text.WriteString(run.Text)
							if run.Bold != (mask&1 != 0) || run.Italic != (mask&2 != 0) || run.Underline != (mask&4 != 0) || !strings.EqualFold(run.Color, "#aa3355") {
								t.Fatalf("native %s combination %d lost editable emphasis/colour: %+v", kind, mask, run)
							}
						}
					}
					if text.String() != "Inherited text" {
						t.Fatal("native text changed", text.String())
					}
				}
				if !found || slide.Notes != "" {
					t.Fatal("native text flattened or private notes leaked")
				}
			}
			if !reflect.DeepEqual(before, s.deck) {
				t.Fatal("output projection mutated authored state")
			}
			if root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				html, err := exportProjectedHTML(standalone, preservedExportHead{}, false)
				if err != nil {
					t.Fatal(err)
				}
				file := kind + ".html"
				if err := os.WriteFile(filepath.Join(root, file), []byte(html), 0600); err != nil {
					t.Fatal(err)
				}
				cases = append(cases, publicSurfaceBrowserCase{File: file, Scenes: scenes})
			}
		})
	}
	if root != "" {
		data, err := json.Marshal(cases)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "cases.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func checkEmphasisOutputText(t *testing.T, name string, text *sceneText, mask int) {
	t.Helper()
	if text == nil {
		t.Fatal(name, "missing semantic text")
	}
	runs := append([]sceneRun(nil), text.Runs...)
	for _, p := range text.Paragraphs {
		runs = append(runs, p.Runs...)
	}
	var visible strings.Builder
	for _, run := range runs {
		visible.WriteString(run.Text)
		if run.Bold != (mask&1 != 0) || run.Italic != (mask&2 != 0) || run.Underline != (mask&4 != 0) || run.Color != "#aa3355" {
			t.Fatalf("%s combination %d lost emphasis/colour: %+v", name, mask, run)
		}
	}
	if visible.String() != "Inherited text" {
		t.Fatal(name, "changed visible text", visible.String())
	}
}
