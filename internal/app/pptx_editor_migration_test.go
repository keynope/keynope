package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"keynope/internal/pptx"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func legacyPPTXDiagramFixture(t *testing.T) Deck {
	t.Helper()
	label, err := json.Marshal(shapeLabelData{Text: "Editable C64 label", Query: "element-style=retro&render=truetype&fg=%23aaff55&modern-size=40"})
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro"}, Slides: []Slide{{ID: "legacy", Elements: []Element{
		{ID: "left", Kind: "shape", Text: "[shape:diamond]", Query: "element-style=retro&shape=diamond&left=10&top=10&width=55&height=24&fg=%230055aa&shape-label=" + url.QueryEscape(base64.StdEncoding.EncodeToString(label))},
		{ID: "right", Kind: "shape", Text: "[shape:square]", Query: "element-style=retro&shape=square&left=110&top=10&width=55&height=24&fg=%2355aa00"},
		{ID: "route", Kind: "connector", Query: "element-style=retro&connector-from=left&connector-from-side=right&connector-to=right&connector-to-side=left&connector-path=elbow&connector-width=1&connector-arrow=end"},
		{ID: "body", Kind: "text", Text: "Portable text", Query: "element-style=retro&render=truetype&left=5&top=40&width=100&height=6&modern-size=40"},
	}}}}
	d.Masters = defaultMasterDeck()
	d.Masters.Base.Slide.PageNumber = pageNumberHide
	for i := range d.Slides[0].Elements {
		d.Slides[0].Elements[i].Query = setQueryValue(d.Slides[0].Elements[i].Query, "object-name", d.Slides[0].Elements[i].ID)
	}
	return d
}

func TestPPTXLegacyDiagramBrowserReadback(t *testing.T) {
	root := os.Getenv("KEYNOPE_FONT_CAPTURE_EVIDENCE")
	if root == "" {
		t.Skip("browser proof not requested")
	}
	data, err := os.ReadFile(filepath.Join(root, "legacy-browser-export.json"))
	if os.IsNotExist(err) {
		t.Skip("browser proof not yet produced")
	}
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	zip, err := base64.StdEncoding.DecodeString(proof.Content)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(zip), int64(len(zip)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	shapes, labels, routes := 0, 0, 0
	for _, o := range p.Slides[0].Objects {
		switch o.Name {
		case "left", "right":
			if o.Kind != "shape" {
				t.Fatal("legacy shape became a picture")
			}
			shapes++
			if o.Text == "Editable C64 label" {
				labels++
			}
		case "route":
			routes++
			if o.Kind != "line" || o.LineStart == nil || o.LineEnd == nil {
				t.Fatal("legacy diagram lost attached native route")
			}
		}
	}
	if shapes != 2 || labels != 1 || routes != 1 {
		t.Fatal("legacy browser package missing editable diagram", shapes, labels, routes)
	}
}

func TestPPTXLegacyStyleScopesMatchEditor(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"implicit", "retro-v1", "modern-v1", "layout-v2"} {
		t.Run(mode, func(t *testing.T) {
			d := legacyPPTXDiagramFixture(t)
			for i := range d.Slides[0].Elements {
				e := &d.Slides[0].Elements[i]
				q, _ := url.ParseQuery(e.Query)
				q.Del("element-style")
				e.Query = q.Encode()
				*e = mapShapeLabelQuery(*e, func(_ shapeLabelData, q url.Values) url.Values { q.Del("element-style"); return q })
			}
			switch mode {
			case "implicit":
				d.Appearance = nil
			case "retro-v1":
				d.Appearance = &DeckAppearance{Version: 1, Mode: "retro"}
			case "modern-v1":
				d.Appearance = &DeckAppearance{Version: 1, Mode: "modern"}
			case "layout-v2":
				d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
				d.Masters.Base.Slide.DefaultStyle = "modern"
				d.Masters.Layouts[0].Slide.DefaultStyle = "retro"
				d.Slides[0].LayoutID = d.Masters.Layouts[0].ID
			}
			before := cloneDeck(d)
			opened := newNativeEditorSession("Legacy.md", d)
			want, wr, err := deckToPPTX(opened.deck)
			if err != nil {
				t.Fatal(err)
			}
			got, gr, err := deckToPPTX(d)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gr, wr) {
				t.Fatal("legacy style scope changed export relative to editor", gr, wr)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("migration modified authored document")
			}
		})
	}
}

func TestPPTXLegacyDiagramMatchesEditorMigration(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	legacy := legacyPPTXDiagramFixture(t)
	before := cloneDeck(legacy)
	opened := newNativeEditorSession("Legacy.md", legacy).deck
	want, wantReport, err := deckToPPTX(opened)
	if err != nil {
		t.Fatal(err)
	}
	got, gotReport, err := deckToPPTX(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotReport, wantReport) {
		t.Fatal("direct export differs from the same legacy deck opened in the editor", gotReport, wantReport)
	}
	shapes, labels, routes := 0, 0, 0
	for _, object := range got.Slides[0].Objects {
		if object.ID == "left" || object.ID == "right" {
			if object.Kind != "shape" {
				t.Fatal("upgraded shape rasterized")
			}
			shapes++
			if object.Text == "Editable C64 label" {
				labels++
			}
		}
		if object.ID == "route" {
			routes++
			if object.LineStart == nil || object.LineEnd == nil {
				t.Fatal("native attachments lost")
			}
		}
	}
	if shapes != 2 || labels != 1 || routes != 1 {
		t.Fatal("legacy diagram parts missing", shapes, labels, routes)
	}
	if !reflect.DeepEqual(legacy, before) {
		t.Fatal("conversion migrated authored source in place")
	}
	if items, err := buildPPTXCaptureItems(context.Background(), legacy); err != nil || len(items) != 0 {
		t.Fatal("upgraded native diagram requested unnecessary raster capture", len(items), err)
	}
	if root := os.Getenv("KEYNOPE_FONT_CAPTURE_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "legacy-diagram.md"), legacy); err != nil {
			t.Fatal(err)
		}
	}
}
