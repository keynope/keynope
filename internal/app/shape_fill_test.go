package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestThemeNoShapeFillIsNotInheritance(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{PageNumber: "hide", Elements: []Element{{ID: "outline-only", Kind: "shape", Query: "shape=square&left=10&top=10&width=30&height=10"}}}}}
	d.Theme.Defaults.ShapeFill = "none"
	for _, deck := range []Deck{d, roundTripAppearanceDeck(t, d)} {
		scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
		if err != nil || len(scene.Objects) != 1 {
			t.Fatal(err, scene.Objects)
		}
		raw, _ := json.Marshal(scene.Objects[0].Paint)
		var paint map[string]interface{}
		_ = json.Unmarshal(raw, &paint)
		if paint["noFill"] != true {
			t.Fatalf("theme no-fill treated as inherited fill: %s", raw)
		}
	}
}

func TestShapeFillMetadataAndMasterOverrides(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, test := range []struct {
		query string
		none  bool
	}{
		{"shape-fill=none&fg=%23ff0055", true},
		{"shape-fill=none&gradient-start=%23ff0055&gradient-end=%2300ffff&gradient-dir=horizontal", true},
		{"shape-fill=solid", false},
		{"fg=%23ff0055", false},
		{"gradient-start=%23ff0055&gradient-end=%2300ffff&gradient-dir=horizontal", false},
		{"transparent=1", true},
	} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base"}, Layouts: []MasterLayout{{ID: "layout", Slide: Slide{Elements: []Element{{ID: "template", SlotID: "slot", PlaceholderRole: placeholderBody, Kind: "shape", Query: "shape=square&left=10&top=10&width=30&height=10"}}}}}}, Slides: []Slide{{ID: "page", LayoutID: "layout", PageNumber: "hide", Elements: []Element{{ID: "instance", MasterSlotID: "slot", Kind: "shape", Query: "shape=square&" + test.query}}}}}
		d.Theme.Defaults.ShapeFill = "none"
		for _, deck := range []Deck{d, roundTripAppearanceDeck(t, d)} {
			scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, o := range scene.Objects {
				if o.ID == "instance" {
					found = true
					if o.Paint.NoFill != test.none {
						t.Fatal(test.query, o.Paint)
					}
				}
			}
			if !found {
				t.Fatal("missing master instance", test.query)
			}
		}
	}
	for _, query := range []string{"shape-fill=bad", "shape-fill=none&shape-fill=solid", "shape-fill="} {
		if validateShapeAdjustments(Element{Kind: "shape", Query: query}) == nil {
			t.Fatal("invalid explicit fill accepted", query)
		}
	}
}

func TestShapeFillOverridesAndStyleOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide", PageNumber: "hide", Elements: []Element{
			{ID: "source", Kind: "shape", Query: "shape=square&left=10&top=10&width=30&height=10"},
			{ID: "target", Kind: "shape", Query: "shape=circle&left=60&top=10&width=30&height=10&fg=%23ff0055"},
		}}}}
		d.Theme.Defaults.ShapeFill = "none"
		s := newNativeEditorSession("Fill.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		}
		check := func(id string, none bool) {
			t.Helper()
			slide := s.deck.ResolveSlide(0, false)
			if master {
				slide = s.deck.Masters.Base.Slide
			}
			scene, err := buildStyledSlideScene(s.deck, slide, 0, 245, 56, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range scene.Objects {
				if object.ID == id {
					if object.Paint.NoFill != none {
						t.Fatal(master, id, object.Paint)
					}
					return
				}
			}
			t.Fatal("missing shape", id)
		}
		check("source", true)
		check("target", false)
		before := cloneDeck(s.deck)
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "copy-object-style", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"source"}}); err != nil {
			t.Fatal(err)
		}
		snapshot := s.transportState().StyleClipboard
		if snapshot.Fields["paint.fill"].Value != "none" || snapshot.Validate() != nil {
			t.Fatal(snapshot)
		}
		if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"target"}, StyleData: snapshot}); err != nil {
			t.Fatal(err)
		}
		check("target", true)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("fill style Undo", err)
		}
		check("target", false)
		for _, value := range []string{"none", "solid", ""} {
			version = s.version
			raw, _ := json.Marshal(value)
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"target"}, Property: "paint.fill", PropertyValue: raw}); err != nil {
				t.Fatal(err)
			}
			check("target", value == "none") // explicit target colour wins when fill inherits.
		}
		version = s.version
		rejected := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"target"}, Property: "paint.fill", PropertyValue: json.RawMessage(`"bad"`)}); err == nil || !reflect.DeepEqual(rejected, s.deck) {
			t.Fatal("invalid fill was not atomic", err)
		}
	}
}

func TestNoFillClipboardAndPPTXRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{PageNumber: "hide", Elements: []Element{{ID: "source", Kind: "shape", Query: "shape=square&left=10&top=10&width=30&height=10&shape-stroke=%23ff0055"}}}}}
	d.Theme.Defaults.ShapeFill = "none"
	bundle, err := makeObjectClipboard(d, d.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	plain := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{PageNumber: "hide"}}}
	elements, assets, objects, err := prepareObjectClipboardPaste(plain, bundle)
	if err != nil {
		t.Fatal(err)
	}
	plain.Slides[0].Elements, plain.Assets, plain.Objects = elements, assets, objects
	scene, err := buildDocumentSlideScene(plain, 0, 245, 56)
	if err != nil || len(scene.Objects) != 1 || !scene.Objects[0].Paint.NoFill {
		t.Fatal("clipboard lost no fill", err, scene.Objects)
	}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, o := range p.Slides[0].Objects {
		if o.Kind == "shape" && strings.EqualFold(o.Stroke, "#ff0055") {
			found = true
			if o.Fill != "" || o.Gradient != nil {
				t.Fatal("PPTX no-fill became filled", o)
			}
		}
	}
	if !found {
		t.Fatal("PPTX missing outline-only shape")
	}
	loaded, _, err := importPPTXDocument(data, "No fill.pptx")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, e := range loaded.Slides[0].Elements {
		q, _ := url.ParseQuery(e.Query)
		if strings.EqualFold(q.Get("shape-stroke"), "#ff0055") {
			found = true
			if q.Get("shape-fill") != "none" {
				t.Fatal("PPTX import lost explicit no-fill", q)
			}
		}
	}
	if !found {
		t.Fatal("import missing outline-only shape")
	}
}

func TestLegacyRetroNoFillKeepsOnlyBorderCoverage(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro"}, Slides: []Slide{{PageNumber: "hide", Elements: []Element{{ID: "shape", Kind: "shape", Query: "shape=square&left=10&top=10&width=30&height=10&element-style=retro&shape-fill=none&shape-stroke=%23ff0055"}}}}}
	for _, deck := range []Deck{d, roundTripAppearanceDeck(t, d)} {
		scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
		if err != nil || len(scene.Objects) != 1 {
			t.Fatal(err, scene.Objects)
		}
		o := scene.Objects[0]
		if !o.Paint.NoFill || o.RetroLines == nil || len(o.RetroLines.Lines) != 0 || len(o.RetroStroke) == 0 {
			t.Fatal("no-fill dropped border or retained filled Retro rows", o)
		}
	}
}
