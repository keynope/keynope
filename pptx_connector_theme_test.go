package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func attachedThemePPTXFixture(t *testing.T) []byte {
	t.Helper()
	d := connectedPPTXDeck(t)
	d.Slides = d.Slides[:2]
	d.Theme.Colors.Accent[0] = "#ff0055"
	d.Slides = append(d.Slides, cloneSlide(d.Slides[0]), cloneSlide(d.Slides[0]))
	for i := range d.Slides {
		d.Slides[i].ID = []string{"primary-route", "foreign-route", "literal-route", "transformed-route"}[i]
		for j, e := range d.Slides[i].Elements {
			if e.Kind == "connector" {
				q := mustQuery(e.Query)
				q.Del("modern-opacity")
				if i == 3 {
					q.Set("modern-opacity", "0.6")
				}
				d.Slides[i].Elements[j].Query = q.Encode()
			}
		}
	}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, entry := range zr.File {
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[entry.Name] = string(content)
	}
	for _, name := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml"} {
		if !strings.Contains(parts[name], `<a:srgbClr val="FF0055"/>`) {
			t.Fatal("missing literal stroke fixture", name)
		}
		parts[name] = strings.Replace(parts[name], `<a:srgbClr val="FF0055"/>`, `<a:schemeClr val="accent1"/>`, 1)
	}
	const alpha = `<a:srgbClr val="FF0055"><a:alpha val="60000"/></a:srgbClr>`
	if !strings.Contains(parts["ppt/slides/slide4.xml"], alpha) {
		t.Fatal("missing transformed stroke fixture")
	}
	parts["ppt/slides/slide4.xml"] = strings.Replace(parts["ppt/slides/slide4.xml"], alpha, `<a:schemeClr val="accent1"><a:alpha val="60000"/></a:schemeClr>`, 1)
	parts["ppt/theme/foreign.xml"] = parts["ppt/theme/theme1.xml"]
	parts["ppt/slideMasters/foreign.xml"] = parts["ppt/slideMasters/slideMaster1.xml"]
	const rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	parts["ppt/slideMasters/_rels/foreign.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + rel + `slideLayout" Target="../slideLayouts/foreign.xml"/><Relationship Id="rId2" Type="` + rel + `theme" Target="../theme/foreign.xml"/></Relationships>`
	parts["ppt/slideLayouts/foreign.xml"] = parts["ppt/slideLayouts/slideLayout1.xml"]
	parts["ppt/slideLayouts/_rels/foreign.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + rel + `slideMaster" Target="../slideMasters/foreign.xml"/></Relationships>`
	parts["ppt/slides/_rels/slide2.xml.rels"] = strings.Replace(parts["ppt/slides/_rels/slide2.xml.rels"], `../slideLayouts/slideLayout1.xml`, `../slideLayouts/foreign.xml`, 1)
	parts["ppt/_rels/presentation.xml.rels"] = strings.Replace(parts["ppt/_rels/presentation.xml.rels"], `</Relationships>`, `<Relationship Id="foreignMaster" Type="`+rel+`slideMaster" Target="slideMasters/foreign.xml"/></Relationships>`, 1)
	parts["ppt/presentation.xml"] = strings.Replace(parts["ppt/presentation.xml"], `</p:sldMasterIdLst>`, `<p:sldMasterId id="2147483649" r:id="foreignMaster"/></p:sldMasterIdLst>`, 1)
	parts["[Content_Types].xml"] = strings.Replace(parts["[Content_Types].xml"], `</Types>`, `<Override PartName="/ppt/theme/foreign.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/><Override PartName="/ppt/slideMasters/foreign.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/><Override PartName="/ppt/slideLayouts/foreign.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/></Types>`, 1)
	names := []string{}
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(parts[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAttachedConnectorThemeNativePackageAndHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	data := attachedThemePPTXFixture(t)
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	deck, _, err := importPPTXDocument(data, "Theme routes.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 4 {
		t.Fatal("fixture lost control slides")
	}
	var scenes []slideScene
	for i, slide := range deck.Slides {
		var e Element
		for _, candidate := range slide.Elements {
			if candidate.Kind == "connector" {
				e = candidate
			}
		}
		if e.ID == "" {
			t.Fatal("native endpoint attachment not restored", i)
		}
		q := mustQuery(e.Query)
		if i == 0 && q.Get("theme-fg") != "accent-1" || i != 0 && q.Get("theme-fg") != "" {
			t.Fatal("native stroke source scope lost", i, q)
		}
		scene, err := buildSlideScene(deck, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		scenes = append(scenes, scene)
	}
	// Verify reader provenance really covers all four intended controls.
	for i, slide := range native.Slides {
		for _, o := range slide.Objects {
			if o.Kind != "line" {
				continue
			}
			if i == 0 && (o.StrokeThemeSlot != "accent1" || o.StrokeThemeSource != native.Theme.SourcePart) || i == 1 && (o.StrokeThemeSlot != "accent1" || o.StrokeThemeSource == native.Theme.SourcePart) || i >= 2 && o.StrokeThemeSlot != "" {
				t.Fatal("invalid source ownership fixture", i, o)
			}
		}
	}
	s := newNativeEditorSession("Routes.md", deck)
	beforeSession := cloneDeck(s.deck)
	theme := deck.Theme.Clone()
	theme.Colors.Accent[0] = "#00ff88"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	for i := range s.deck.Slides {
		scene, err := buildSlideScene(s.deck, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			found := false
			for _, o := range scene.Objects {
				found = found || o.Kind == "connector" && o.Paint.Color == "#00ff88"
			}
			if !found {
				t.Fatal("imported connector did not follow palette")
			}
		} else if !reflect.DeepEqual(scene, scenes[i]) {
			t.Fatal("primary edit restyled control slide", i)
		}
	}
	changed := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, beforeSession) {
		t.Fatal("theme Undo drift", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, changed) {
		t.Fatal("theme Redo drift", err)
	}
	encoded, err := serializeDeck("Routes.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Routes.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reopened.Slides {
		want, err := buildSlideScene(s.deck, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := buildSlideScene(reopened, i, 245, 56)
		if err != nil || !reflect.DeepEqual(want, actual) {
			t.Fatal("route role/geometry lost after reopen", i, err)
		}
	}
	if root := os.Getenv("KEYNOPE_CONNECTOR_THEME_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string][]byte{"source.pptx": data, "routes.md": encoded} {
			if err := os.WriteFile(filepath.Join(root, name), value, 0644); err != nil {
				t.Fatal(err)
			}
		}
		payload, _ := json.Marshal(scenes)
		if err := os.WriteFile(filepath.Join(root, "initial-scenes.json"), payload, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAttachedConnectorRestorationKeepsSourceThemePaint(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	raw := pptxThemeImportFixture(t)
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(raw), int64(len(raw)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"primary", "foreign", "literal"} {
		t.Run(mode, func(t *testing.T) {
			binding := newPPTXThemeBinding(native.Theme)
			objects := []pptx.Object{
				{ID: "a", Kind: "shape", Shape: "rect", X: 100, Y: 100, Width: 100, Height: 100, Fill: "#123456"},
				{ID: "b", Kind: "shape", Shape: "rect", X: 400, Y: 100, Width: 100, Height: 100, Fill: "#123456"},
				{ID: "wire", Kind: "line", X: 200, Y: 150, Width: 200, Stroke: native.Theme.Accent[0], StrokeWidth: 4, StrokeThemeSlot: "accent1", StrokeThemeSource: native.Theme.SourcePart, LineStart: &pptx.Connection{Target: "a"}, LineEnd: &pptx.Connection{Target: "b"}},
			}
			if mode == "foreign" {
				objects[2].StrokeThemeSource = "ppt/theme/foreign.xml"
			}
			if mode == "literal" {
				objects[2].StrokeThemeSlot, objects[2].StrokeThemeSource = "", ""
			}
			slide := Slide{}
			indices := map[int]int{}
			for i, o := range objects {
				e, _, _, err := pptxObjectToElement(o, 1, 0, 0, binding)
				if err != nil {
					t.Fatal(err)
				}
				e = binding.element(o, e)
				e.ID = o.ID
				slide.Elements = append(slide.Elements, e)
				indices[i] = i
			}
			before := cloneSlide(slide)
			restored, report := importPPTXConnections(pptx.Slide{Objects: objects}, &slide, indices, 1, 0, 0)
			if !restored["wire"] {
				t.Fatal("fixture did not restore attached connector", report)
			}
			q := mustQuery(slide.Elements[2].Query)
			if mode == "primary" {
				if q.Get("theme-fg") != "accent-1" || q.Has("fg") {
					t.Fatal("attachment restoration discarded the primary stroke role", q)
				}
			} else if q.Get("theme-fg") != "" || q.Get("fg") != native.Theme.Accent[0] {
				t.Fatal("literal/foreign stroke acquired a primary role", q)
			}
			if !reflect.DeepEqual(before.Elements[:2], slide.Elements[:2]) {
				t.Fatal("restoration restyled endpoint shapes")
			}
			deck := Deck{Theme: binding.theme.Clone(), Slides: []Slide{slide}}
			initial, err := buildSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			deck.Theme.Colors.Accent[0] = "#00ff88"
			changed, err := buildSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			paint := func(s slideScene) string {
				for _, o := range s.Objects {
					if o.ID == "wire" {
						return o.Paint.Color
					}
				}
				return ""
			}
			if mode == "primary" && paint(changed) != "#00ff88" || mode != "primary" && paint(changed) != paint(initial) {
				t.Fatal("connector palette dependency lost", paint(initial), paint(changed))
			}
		})
	}
}
