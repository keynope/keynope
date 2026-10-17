package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

var outputHorizontalAlignments = []string{"left", "center", "right", "justify"}
var outputVerticalAlignments = []string{"top", "middle", "bottom"}

const alignmentOutputText = "Alignment sample\nSecond line"

// All twelve boxes inherit frame/font/paint from authored placeholders. Only
// alignment is applied below, through the real stable-ID command boundary.
func alignmentOutputFixture(t *testing.T, kind, font string) *nativeEditorSession {
	t.Helper()
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base"}, Layouts: []MasterLayout{{ID: "alignment", Name: "Alignment"}}}, Slides: []Slide{{ID: "alignment-slide", LayoutID: "alignment", PageNumber: pageNumberHide, Notes: "ALIGNMENT_PRIVATE_NOTES", BG: "48;2;15;23;32", BGSet: true}}}
	d.Extra = extensionFixture("private", `"ALIGNMENT_PRIVATE_EXTENSION"`)
	for row, vertical := range outputVerticalAlignments {
		for col, horizontal := range outputHorizontalAlignments {
			id := vertical + "-" + horizontal
			q := url.Values{"left": {fmt.Sprint(5 + col*60)}, "top": {fmt.Sprint(3 + row*17)}, "width": {"55"}, "height": {"14"}, "text-box": {"1"}, "render": {"truetype"}, "modern-font": {font}, "modern-size": {"42"}, "modern-width": {"100"}, "fg": {"#ffeeaa"}, "text-align": {"center"}, "text-valign": {"middle"}, "object-name": {id}}
			master := Element{ID: "master-" + id, SlotID: id, PlaceholderRole: placeholderBody, Kind: kind, Text: alignmentOutputText, Query: q.Encode()}
			child := Element{ID: id, MasterSlotID: id, Kind: kind, Text: alignmentOutputText}
			if kind == "heading" {
				master.Level, child.Level = 2, 2
			}
			if kind == "shape" {
				labelQuery := url.Values{"render": {"truetype"}, "modern-font": {font}, "modern-size": {"42"}, "modern-width": {"100"}, "fg": {"#ffeeaa"}}
				data, err := json.Marshal(shapeLabelData{Text: alignmentOutputText, Query: labelQuery.Encode()})
				if err != nil {
					t.Fatal(err)
				}
				q.Set("shape", "square")
				q.Set("fg", "#183858")
				q.Set("shape-label", base64.StdEncoding.EncodeToString(data))
				master.Query = q.Encode()
				master.Text, child.Text = "[shape:square]", "[shape:square]"
			}
			d.Masters.Layouts[0].Slide.Elements = append(d.Masters.Layouts[0].Slide.Elements, master)
			d.Slides[0].Elements = append(d.Slides[0].Elements, child)
		}
	}
	s := newNativeEditorSession("Alignment outputs.md", d)
	for _, vertical := range outputVerticalAlignments {
		for _, horizontal := range outputHorizontalAlignments {
			prefix := "text."
			if kind == "shape" {
				prefix = "label."
			}
			for _, item := range []struct{ property, value string }{{prefix + "align", horizontal}, {prefix + "vertical", vertical}} {
				revision := s.version
				raw, _ := json.Marshal(item.value)
				if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{vertical + "-" + horizontal}, Property: item.property, PropertyValue: raw}); err != nil {
					t.Fatal(kind, font, item, err)
				}
			}
		}
	}
	return s
}

func checkAlignmentOutputScene(t *testing.T, name string, scene *slideScene, kind, font string) {
	t.Helper()
	if scene == nil || len(scene.Objects) != 12 {
		t.Fatal(name, "missing alignment objects")
	}
	seen := map[string]bool{}
	for _, object := range scene.Objects {
		text := object.Text
		if kind == "shape" {
			text = object.Label
		}
		parts := strings.Split(object.ID, "-")
		if len(parts) != 2 || seen[object.ID] || text == nil {
			t.Fatal(name, "invalid/duplicate object", object.ID, object.Kind, text)
		}
		seen[object.ID] = true
		align := map[string]string{"left": "start", "center": "center", "right": "end", "justify": "justify"}[parts[1]]
		if align == "" || text.Align != align || text.Vertical != parts[0] || text.FontID != font || sceneTextPlain(text) != alignmentOutputText {
			t.Fatalf("%s/%s: alignment/font/content changed: %+v", name, object.ID, text)
		}
	}
	wire, err := json.Marshal(scene)
	if err != nil || bytes.Contains(wire, []byte("ALIGNMENT_PRIVATE")) {
		t.Fatal(name, "private or invalid scene", err)
	}
}

func TestTextAlignmentPublicAndEditablePPTXOutputs(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	root := os.Getenv("KEYNOPE_ALIGNMENT_OUTPUT_PROOF")
	if root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var browserCases []publicSurfaceBrowserCase
	for _, kind := range []string{"text", "heading", "bullet", "code", "shape"} {
		for _, font := range []string{"c64", "mono", "sans"} {
			t.Run(kind+"/"+font, func(t *testing.T) {
				s := alignmentOutputFixture(t, kind, font)
				before := cloneDeck(s.deck)
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
				var editor slideScene
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &editor) != nil {
					t.Fatal("editor scene", w.Code, w.Body.String())
				}
				checkAlignmentOutputScene(t, "editor", &editor, kind, font)
				presenter := projectExportDeck("Alignment.md", s.deck.ResolvedSlides(), 245, 56, true)
				standalone := projectExportDeck("Alignment.md", s.deck.ResolvedSlides(), 245, 56, false)
				_, pdf, err := projectPDFExport("Alignment.md", s.deck, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				participant, err := participantRenderedDeck(s.deck, 0)
				if err != nil {
					t.Fatal(err)
				}
				for name, page := range map[string]exportPage{"presenter": presenter.Pages[0], "standalone": standalone.Pages[0], "participant": participant.Pages[0], "pdf-input": pdf.Pages[0]} {
					checkAlignmentOutputScene(t, name, page.Scene, kind, font)
					actual, _ := json.Marshal(publicParityScene(*page.Scene))
					want, _ := json.Marshal(publicParityScene(editor))
					if !bytes.Equal(actual, want) {
						t.Fatal(name, "alignment output differs from the editor scene")
					}
				}
				md, err := participantSlideMarkdown(s.deck, 0)
				if err != nil || bytes.Contains(md, []byte("ALIGNMENT_PRIVATE")) {
					t.Fatal("participant Markdown", err)
				}
				if root != "" {
					if err := os.WriteFile(filepath.Join(root, kind+"-"+font+".md"), md, 0600); err != nil {
						t.Fatal(err)
					}
				}
				loaded, err := parseDeckData("Participant.md", md)
				if err != nil {
					t.Fatal(err)
				}
				legacy, err := buildDocumentSlideScene(loaded, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				checkAlignmentOutputScene(t, "participant Markdown", &legacy, kind, font)
				restoreParticipantLayers(&loaded.Slides[0])
				pages := participantMarkdownExportPages(loaded, 0, 1, 245, 56)
				if len(pages) != 1 {
					t.Fatal("participant adapter changed the page count")
				}
				checkAlignmentOutputScene(t, "WASM participant projection", pages[0].Scene, kind, font)
				data, _, err := exportPPTXDocument(s.deck)
				if err != nil {
					t.Fatal(err)
				}
				native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
				if err != nil || len(native.Slides) != 1 || len(native.Slides[0].Objects) != 12 || native.Slides[0].Notes != "" {
					t.Fatal("editable package readback", err)
				}
				for _, object := range native.Slides[0].Objects {
					parts := strings.Split(object.Name, "-")
					align := ""
					if len(parts) == 2 {
						align = map[string]string{"left": "start", "center": "center", "right": "end", "justify": "justify"}[parts[1]]
					}
					if len(parts) != 2 || object.Kind != map[bool]string{true: "shape", false: "text"}[kind == "shape"] || object.Text != alignmentOutputText || len(object.Paragraphs) == 0 || object.TextAlign != align || object.VerticalAlign != parts[0] {
						t.Fatalf("PowerPoint alignment/text not editable or changed: %+v", object)
					}
					for _, paragraph := range object.Paragraphs {
						if paragraph.Align != align {
							t.Fatal("editable paragraph alignment changed", object.Name, paragraph.Align)
						}
					}
					for _, original := range editor.Objects {
						if original.ID == object.Name && (math.Abs(object.X-original.Bounds.X) > .001 || math.Abs(object.Y-original.Bounds.Y) > .001 || math.Abs(object.Width-original.Bounds.Width) > .001 || math.Abs(object.Height-original.Bounds.Height) > .001) {
							t.Fatal("editable alignment frame changed", object.Name)
						}
					}
				}
				if !reflect.DeepEqual(before, s.deck) {
					t.Fatal("output mutated document")
				}
				if root != "" {
					if err := os.MkdirAll(root, 0700); err != nil {
						t.Fatal(err)
					}
					html, err := exportProjectedHTML(standalone, preservedExportHead{}, false)
					if err != nil {
						t.Fatal(err)
					}
					file := kind + "-" + font + ".html"
					if err := os.WriteFile(filepath.Join(root, file), []byte(html), 0600); err != nil {
						t.Fatal(err)
					}
					browserCases = append(browserCases, publicSurfaceBrowserCase{File: file, Scenes: []slideScene{*standalone.Pages[0].Scene}})
				}
			})
		}
	}
	if root != "" {
		wire, err := json.Marshal(browserCases)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "cases.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
