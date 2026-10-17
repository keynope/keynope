package app

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func strokeInheritanceFixture() Deck {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	d.Masters.Layouts = []MasterLayout{{ID: "stroke", Name: "Stroke", Slide: Slide{Elements: []Element{{ID: "master", SlotID: "slot", PlaceholderRole: placeholderBody, Kind: "shape", Query: "shape=square&width=40&height=12&shape-stroke=%2300aa55&shape-stroke-width=7.5&shadow=solid"}}}}}
	d.Slides = []Slide{{ID: "slide", LayoutID: "stroke", PageNumber: pageNumberHide, Elements: []Element{{ID: "instance", MasterSlotID: "slot", Kind: "shape", Query: "shape=square&left=30&top=10&master-clear=shadow"}}}}
	return d
}

func TestShapeStrokeInheritanceVisualFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_STROKE_INHERITANCE_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	d := strokeInheritanceFixture()
	label, err := encodeShapeLabel(shapeLabelData{Text: "Inherited edge", Query: "modern-font=sans&modern-size=32&fg=%23172e46"})
	if err != nil {
		t.Fatal(err)
	}
	d.Slides[0].Elements[0].Query += "&shape-label=" + url.QueryEscape(label)
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "source", Kind: "shape", Query: "shape=square&left=100&top=10&width=40&height=12&outline=dark&fg=%2355aaff"})
	if err := saveDeck(filepath.Join(root, "inheritance.md"), d); err != nil {
		t.Fatal(err)
	}
}

func TestShapeStrokeOutlineOverridesMasterAliases(t *testing.T) {
	for _, preset := range []string{"none", "light", "dark"} {
		t.Run(preset, func(t *testing.T) {
			s := newNativeEditorSession("Stroke.md", strokeInheritanceFixture())
			before := cloneDeck(s.deck)
			version := s.version
			raw, _ := json.Marshal(preset)
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, Property: "paint.outline", PropertyValue: raw}); err != nil {
				t.Fatal(err)
			}
			scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, o := range scene.Objects {
				if o.ID != "instance" {
					continue
				}
				found = true
				if preset == "none" && (o.Paint.Stroke != "" || o.Paint.StrokeWidth != 0) || preset != "none" && (o.Paint.Stroke == "#00aa55" || o.Paint.StrokeWidth == 7.5) {
					t.Fatal("master arbitrary stroke beat explicit outline preset", o.Paint)
				}
			}
			if !found {
				t.Fatalf("missing resolved shape in scene: %+v", scene.Objects)
			}
			q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
			if q.Get("master-clear") != "shadow,shape-stroke,shape-stroke-width" || q.Get("left") != "30" || q.Get("top") != "10" {
				t.Fatal("stroke override suppression not scoped", q)
			}
			after := cloneDeck(s.deck)
			file := filepath.Join(t.TempDir(), "Stroke.md")
			if err := saveDeck(file, after); err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeck(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range loaded.ResolveSlide(0, false).Elements {
				if e.ID == "instance" {
					q, _ := url.ParseQuery(e.Query)
					if q.Has("shape-stroke") || q.Has("shape-stroke-width") || q.Get("outline") != preset {
						t.Fatal("Markdown reopen lost stroke suppression", q)
					}
				}
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("undo did not restore inherited stroke", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("redo did not restore outline", err)
			}
			version = s.version
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, Property: "paint.outline", PropertyValue: json.RawMessage(`""`)}); err != nil {
				t.Fatal(err)
			}
			resolved := s.deck.ResolveSlide(0, false)
			q, _ = url.ParseQuery(resolved.Elements[0].Query)
			if q.Get("shape-stroke") != "#00aa55" || q.Get("shape-stroke-width") != "7.5" || q.Has("shadow") {
				t.Fatal("Inherit did not restore master stroke independently", q)
			}
		})
	}
}

func TestShapeStrokeStyleTransferOutlineAliases(t *testing.T) {
	for _, preset := range []string{"none", "dark", "light"} {
		s := newNativeEditorSession("Stroke.md", strokeInheritanceFixture())
		version := s.version
		snapshot := &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{
			"paint.outline":     {Present: true, Value: preset},
			"paint.strokeColor": {},
			"paint.strokeWidth": {},
		}}
		if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, StyleData: snapshot}); err != nil {
			t.Fatal(err)
		}
		for _, e := range s.deck.ResolveSlide(0, false).Elements {
			if e.ID == "instance" {
				q, _ := url.ParseQuery(e.Query)
				if q.Has("shape-stroke") || q.Has("shape-stroke-width") || q.Has("shadow") || q.Get("outline") != preset {
					t.Fatal("pasted outline failed to suppress master aliases", q)
				}
			}
		}
		version = s.version
		if err := s.apply(nativeEditorAction{Action: "reset-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}}); err != nil {
			t.Fatal(err)
		}
		for _, e := range s.deck.ResolveSlide(0, false).Elements {
			if e.ID == "instance" {
				q, _ := url.ParseQuery(e.Query)
				if q.Get("shape-stroke") != "#00aa55" || q.Get("shape-stroke-width") != "7.5" || q.Get("shadow") != "solid" || q.Get("left") != "30" || q.Get("top") != "10" {
					t.Fatal("style reset did not restore inherited formatting", q)
				}
			}
		}
	}
}

func TestShapeStrokeLegacyMasterAliasResolution(t *testing.T) {
	for _, preset := range []string{"none", "dark", "light"} {
		d := strokeInheritanceFixture()
		d.Slides[0].Elements[0].Query += "&outline=" + preset
		// Existing decks and direct element updates need no migration mutation.
		before := cloneDeck(d)
		resolved := d.ResolveSlide(0, false)
		q, _ := url.ParseQuery(resolved.Elements[0].Query)
		if q.Has("shape-stroke") || q.Has("shape-stroke-width") || q.Get("outline") != preset || q.Has("shadow") {
			t.Fatal("legacy local preset lost precedence", preset, q)
		}
		if !reflect.DeepEqual(before, d) {
			t.Fatal("resolution rewrote authored deck")
		}
	}
	q, _ := url.ParseQuery(mergeQueries("outline=light&shape-stroke-width=7.5&shadow=solid", "shape-stroke=%23ff55aa&shape-stroke-width=3.25"))
	if q.Has("outline") || q.Get("shape-stroke") != "#ff55aa" || q.Get("shape-stroke-width") != "3.25" || q.Get("shadow") != "solid" {
		t.Fatal("local custom edge did not beat master preset", q)
	}
	q, _ = url.ParseQuery(mergeQueries("shape-stroke=%2300aa55&shape-stroke-width=7.5", "outline=light&shape-stroke-width=3.25"))
	if q.Has("shape-stroke") || q.Get("outline") != "light" || q.Get("shape-stroke-width") != "3.25" {
		t.Fatal("explicit local width was lost with preset", q)
	}
}

func TestShapeStrokeColourPreservesPresetWidth(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		d := strokeInheritanceFixture()
		d.Theme.Defaults.LineWidth = 6
		if inherited {
			d.Masters.Layouts[0].Slide.Elements[0].Query = "shape=square&width=40&height=12&outline=light"
		} else {
			d.Slides[0].Elements[0].Query += "&outline=light"
		}
		s := newNativeEditorSession("Stroke.md", d)
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, Property: "paint.strokeColor", PropertyValue: json.RawMessage(`"#ff55aa"`)}); err != nil {
			t.Fatal(err)
		}
		scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range scene.Objects {
			if object.ID == "instance" && (object.Paint.Stroke != "#ff55aa" || object.Paint.StrokeWidth != 2) {
				t.Fatal("colour-only edit changed preset width", inherited, object.Paint)
			}
		}
	}
}

func TestShapeStrokeMasterAliasOutputMappings(t *testing.T) {
	for _, preset := range []string{"none", "dark", "light"} {
		d := strokeInheritanceFixture()
		d.Slides[0].Elements[0].Query += "&outline=" + preset
		want := map[string]string{"none": "", "dark": "#111111", "light": "#ffffff"}[preset]
		public, err := participantSlideMarkdown(d, 0)
		if err != nil {
			t.Fatal(err)
		}
		published, err := parseDeckData("Public.md", public)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range []Deck{d, published} {
			scene, err := buildDocumentSlideScene(source, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, object := range scene.Objects {
				if object.ID == "instance" {
					found = true
					if object.Paint.Stroke != want {
						t.Fatal("published edge differs from authored scene", preset, object.Paint)
					}
				}
			}
			if !found {
				t.Fatal("output lost inherited shape")
			}
		}
		pdf, err := makePDFExportDocument("Stroke.md", d, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if len(pdf.Manifest.Pages) != 1 || len(pdf.Manifest.Pages[0].Page.Scene.Objects) != 1 || pdf.Manifest.Pages[0].Page.Scene.Objects[0].Paint.Stroke != want {
			t.Fatal("PDF manifest lost resolved edge", preset)
		}
		data, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Slides) != 1 {
			t.Fatal("wrong PPTX slide count")
		}
		found := 0
		for _, object := range p.Slides[0].Objects {
			// The exporter also emits a full-slide background rectangle. Do not
			// confuse it with the authored 40-cell shape being verified.
			if math.Abs(object.Width-40*1920.0/245) > .001 {
				continue
			}
			found++
			width := 1.0 // DrawingML point conversion is half the scene pixels.
			if preset == "none" {
				width = 0
			}
			if strings.ToLower(object.Stroke) != want || math.Abs(object.StrokeWidth-width) > .001 {
				t.Fatal("editable PPTX edge differs from authored scene", preset, object)
			}
		}
		if found != 1 {
			t.Fatal("PPTX lost or duplicated authored shape", preset, found)
		}
	}
}
