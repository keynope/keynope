package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestElementStyleInheritance(t *testing.T) {
	d := sceneFixture(t)
	d.Masters = defaultMasterDeck()
	d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	s := Slide{}
	e := Element{Kind: "text", Text: "Hello"}
	if d.elementStyle(s, e) != "modern" {
		t.Fatal("deck default")
	}
	d.Masters.Base.Slide.DefaultStyle = "retro"
	if d.elementStyle(s, e) != "retro" {
		t.Fatal("base default without layout")
	}
	if len(d.Masters.Layouts) == 0 {
		t.Fatal("fixture needs layout")
	}
	d.Masters.Layouts[0].Slide.DefaultStyle = "modern"
	s.LayoutID = d.Masters.Layouts[0].ID
	if d.elementStyle(s, e) != "modern" {
		t.Fatal("layout default")
	}
	s.DefaultStyle = "retro"
	if d.elementStyle(s, e) != "retro" {
		t.Fatal("slide override")
	}
	e.Query = "element-style=modern"
	if d.elementStyle(s, e) != "modern" {
		t.Fatal("element override")
	}
	e.Query = ""
	if d.elementStyle(s, e) != "retro" {
		t.Fatal("cleared override")
	}
	s.DefaultStyle = ""
	s.LayoutID = "missing"
	if d.elementStyle(s, e) != "retro" {
		t.Fatal("missing layout fallback")
	}
}

func TestShapeLabelStyleIsIndependent(t *testing.T) {
	for _, owner := range []string{"retro", "modern"} {
		for _, override := range []string{"", "retro", "modern"} {
			d := sceneFixture(t)
			d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
			labelQuery := "ttf-size=65"
			if override != "" {
				labelQuery += "&element-style=" + override
			}
			data, _ := json.Marshal(shapeLabelData{Text: "Label", Query: labelQuery})
			q := url.Values{"shape": {"square"}, "width": {"80"}, "height": {"12"}, "top": {"4"}, "left": {"5"}, "element-style": {owner}, "shape-label": {base64.StdEncoding.EncodeToString(data)}}
			d.Slides[0].Elements = []Element{{ID: "shape", Kind: "shape", Query: q.Encode()}}
			before, _ := json.Marshal(d)
			scene, err := buildMixedSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			var object sceneObject
			for _, o := range scene.Objects {
				if o.ID == "shape" {
					object = o
				}
			}
			want := override
			if want == "" {
				want = owner
			}
			if object.Style != owner || object.LabelStyle != want || object.RetroLabel != nil || want == "retro" && object.Label.FontID != "c64" {
				t.Fatalf("owner=%s override=%s: styles=%s/%s retroLabel=%v", owner, override, object.Style, object.LabelStyle, object.RetroLabel != nil)
			}
			if object.RetroLines != nil {
				for _, line := range object.RetroLines.Lines {
					if line.Role == "shape-label" {
						t.Fatal("label baked into shape body")
					}
				}
			}
			after, _ := json.Marshal(d)
			if !bytes.Equal(before, after) {
				t.Fatal("projection changed source")
			}
		}
	}
}

func TestUnifiedSessionRejectsRetiredShapeLabelStyleTransaction(t *testing.T) {
	d := sceneFixture(t)
	s := newNativeEditorSession("Labels.md", d)
	var id string
	for _, e := range s.deck.Slides[0].Elements {
		if e.Kind == "shape" {
			id = e.ID
			break
		}
	}
	if id == "" {
		t.Fatal("missing fixture shape")
	}
	before := cloneDeck(s.deck)
	revision := s.version
	a := nativeEditorAction{Action: "set-element-style", Kind: "label", Slide: 0, ObjectIDs: []string{id}, Name: "retro", SceneRevision: &revision}
	if err := s.apply(a); err != errInvalidEditorAction {
		t.Fatalf("retired label-style action returned %v", err)
	}
	if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
		t.Fatal("retired label-style action mutated the unified document")
	}
}

func TestElementStyleAppearanceUpgradeIsAtomic(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Keep me", Query: "fg=%23ff0055"}}}}, Appearance: &DeckAppearance{Version: 1, Mode: "retro", Extra: map[string]json.RawMessage{"extension": json.RawMessage(`"` + strings.Repeat("x", 70000) + `"`)}}}
	before, _ := json.Marshal(d)
	changed, err := applyElementStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, Name: "modern"}, "")
	if err == nil || changed {
		t.Fatal("oversized appearance must reject the entire style transaction")
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("failed schema upgrade changed the document")
	}
}

func TestElementStyleUpgradesOnlyOnChange(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Hello"}}}}}
	action := nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, Name: "inherit"}
	if changed, err := applyElementStyle(&d, action, ""); err != nil || changed || d.Appearance != nil {
		t.Fatalf("no-op must not migrate document: changed=%v err=%v", changed, err)
	}
	action.Name = "modern"
	if changed, err := applyElementStyle(&d, action, ""); err != nil || !changed || !d.usesElementStyles() || d.AppearanceMode() != "retro" {
		t.Fatalf("style change must opt into mixed rendering without changing default: changed=%v err=%v", changed, err)
	}
}

func TestElementStyleSceneEndpointUsesDocumentDefaults(t *testing.T) {
	d := sceneFixture(t)
	d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	s := &nativeEditorSession{deck: d}
	for _, tc := range []struct{ query, want string }{
		{"", "retro"},
		{"&styles=resolved", "retro"},
		{"&styles=modern", "modern"},
		{"&report=deck", "modern"},
	} {
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0"+tc.query, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %s", tc.query, w.Body.String())
		}
		var scene slideScene
		if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
			t.Fatal(err)
		}
		if scene.DefaultStyle != tc.want {
			t.Fatalf("%s: default=%s, want=%s", tc.query, scene.DefaultStyle, tc.want)
		}
	}
}

func TestUnifiedSessionRejectsRetiredStyleDefaultTransactions(t *testing.T) {
	for _, scope := range []string{"deck", "slide", "master"} {
		t.Run(scope, func(t *testing.T) {
			d := sceneFixture(t)
			d.Masters = defaultMasterDeck()
			d.Slides[0].Elements[0].Query += "&element-style=retro"
			s := newNativeEditorSession("Defaults.md", d)
			if scope == "master" {
				if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
					t.Fatal(err)
				}
			}
			before := cloneDeck(s.deck)
			revision := s.version
			kind := scope
			if scope == "master" {
				kind = "slide"
			}
			a := nativeEditorAction{Action: "set-style-default", Kind: kind, Name: "modern", Slide: 0, SceneMaster: scope == "master", SceneRevision: &revision}
			if err := s.apply(a); err != errInvalidEditorAction {
				t.Fatalf("retired %s default action returned %v", scope, err)
			}
			if !reflect.DeepEqual(s.deck, before) || s.version != revision || len(s.undo) != 0 {
				t.Fatal("retired default action mutated the unified document")
			}
		})
	}
}

func TestStyleDefaultResetAndValidation(t *testing.T) {
	d := Deck{Slides: []Slide{{DefaultStyle: "modern", Elements: []Element{{ID: "one", Kind: "text", Text: "One", Query: "element-style=modern"}}}}, Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro"}}
	for _, action := range []nativeEditorAction{
		{Kind: "deck", Name: "inherit"}, {Kind: "other", Name: "modern"},
		{Kind: "slide", Name: "broken"}, {Kind: "slide", Name: "retro", Slide: 4},
	} {
		before, _ := json.Marshal(d)
		changed, err := applyStyleDefault(&d, action)
		after, _ := json.Marshal(d)
		if err == nil || changed || !bytes.Equal(after, before) {
			t.Fatal("invalid default mutated document")
		}
	}
	if changed, err := applyStyleDefault(&d, nativeEditorAction{Kind: "slide", Name: "inherit"}); err != nil || !changed {
		t.Fatalf("reset: %v", err)
	}
	if d.Slides[0].DefaultStyle != "" || d.slideDefaultStyle(d.Slides[0]) != "retro" || d.elementStyle(d.Slides[0], d.Slides[0].Elements[0]) != "modern" {
		t.Fatal("reset must restore inheritance, preserving explicit element style")
	}
	if changed, err := applyStyleDefault(&d, nativeEditorAction{Kind: "slide", Name: "inherit"}); err != nil || changed {
		t.Fatal("repeated reset must be no-op")
	}
	path := filepath.Join(t.TempDir(), "Defaults.md")
	data, err := serializeDeck(path, d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slides[0].DefaultStyle != "" || loaded.AppearanceMode() != "retro" || loaded.elementStyle(loaded.Slides[0], loaded.Slides[0].Elements[0]) != "modern" {
		t.Fatal("save/reopen lost default or override")
	}
}

func TestElementStylePersistenceAndResolutionIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Styles.md")
	d, err := parseDeckData(path, []byte("<!-- default-style=modern -->\n\n<!-- element-style=retro -->\n# Retro\n\n<!-- element-style=modern -->\nModern\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Slides[0].DefaultStyle != "modern" || len(d.Slides[0].Elements) != 2 {
		t.Fatalf("parse: %+v", d.Slides[0])
	}
	before, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	r := d.ResolveSlide(0, false)
	if d.elementStyle(r, r.Elements[0]) != "retro" || d.elementStyle(r, r.Elements[1]) != "modern" {
		t.Fatal("lost mixed overrides")
	}
	after, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("resolution mutated authored document")
	}
	data, err := serializeDeck(path, d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slides[0].DefaultStyle != "modern" {
		t.Fatal("lost slide default")
	}
	for i, want := range []string{"retro", "modern"} {
		if loaded.elementStyle(loaded.Slides[0], loaded.Slides[0].Elements[i]) != want {
			t.Fatalf("lost element %d style", i)
		}
	}
}

func TestInheritedMasterElementStyleUsesReceivingSlide(t *testing.T) {
	d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank", DefaultStyle: "modern"}}}
	d.Masters.Base.Slide.DefaultStyle = "retro"
	d.Masters.Base.Slide.Elements = []Element{
		{ID: "inheriting", Kind: "text", Text: "Inherited"},
		{ID: "explicit", Kind: "text", Text: "Retro", Query: "element-style=retro"},
	}
	r := d.ResolveSlide(0, false)
	seen := 0
	for _, e := range r.Elements {
		if e.ID == "inheriting" || e.ID == "explicit" {
			want := "modern"
			if e.ID == "explicit" {
				want = "retro"
			}
			if !e.Inherited || d.elementStyle(r, e) != want {
				t.Fatalf("incorrect master resolution: %+v", e)
			}
			seen++
		}
	}
	if seen != 2 {
		t.Fatal("missing inherited elements")
	}
	if d.Masters.Base.Slide.Elements[0].Query != "" {
		t.Fatal("resolved style became authored override")
	}
}

func TestUnifiedSessionRejectsRetiredGroupedElementStyleTransaction(t *testing.T) {
	d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{Elements: []Element{
		{ID: "first", Kind: "text", Text: "Hello", Query: "group=g&fg=%23ff5500&top=2"},
		{ID: "second", Kind: "shape", Query: "group=g&shape=square&top=10&width=8&height=4"},
	}}}}
	s := newNativeEditorSession("Styles.md", d, false, false)
	before := cloneDeck(s.deck)
	s.savedDeck = cloneDeck(s.deck)
	revision := s.version
	a := nativeEditorAction{Action: "set-element-style", Slide: 0, ObjectIDs: []string{"first"}, Name: "modern", SceneRevision: &revision}
	if err := s.apply(a); err != errInvalidEditorAction {
		t.Fatalf("retired grouped style action returned %v", err)
	}
	if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
		t.Fatal("retired grouped style action mutated the unified document")
	}
}

func TestElementStyleBatchRejectsWithoutPartialMutation(t *testing.T) {
	s := Slide{Elements: []Element{{ID: "a", Kind: "text", Text: "A"}, {ID: "b", Kind: "text", Text: "B", Query: "object-locked=1"}}}
	before := cloneSlide(s)
	for _, ids := range [][]string{{"a", "b"}, {"a", "missing"}, {""}} {
		if _, err := setElementStyles(&s, ids, "modern"); err == nil {
			t.Fatal("accepted invalid batch")
		}
		if !reflect.DeepEqual(before, s) {
			t.Fatal("partial mutation")
		}
	}
	if changed, err := setElementStyles(&s, []string{"a"}, "inherit"); err != nil || changed {
		t.Fatal("inherit no-op changed document")
	}
}

func TestSceneIgnoresRetiredRetroTextTreatment(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "retro", Kind: "text", Text: "Retro", Query: "render=truetype&element-style=retro&glyph=braille&top=1&width=80&height=10&ttf-size=97"},
		{ID: "modern", Kind: "text", Text: "Modern", Query: "render=truetype&element-style=modern&top=15&width=80&height=10"},
	}}}}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 2 {
		t.Fatal("missing scene objects")
	}
	if o := scene.Objects[0]; o.RetroText != nil || o.RetroLines != nil || o.Text == nil || o.Text.FontID != "c64" || o.Text.Size != 97 {
		t.Fatal("obsolete glyph metadata must not bypass shared C64 typography")
	}
	if scene.Objects[1].RetroText != nil || scene.Objects[1].Text == nil {
		t.Fatal("Modern sibling lost shaped rendering")
	}
	for _, diagnostic := range scene.Diagnostics {
		if diagnostic.ObjectID == "retro" && diagnostic.Code == "art-treatment" {
			t.Fatal("retired text treatment must not require an artwork adapter")
		}
	}
}

func TestSceneRetainsRetroShapeCells(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "retro", Kind: "shape", Query: "element-style=retro&shape=circle&top=2&width=12&height=7&fg=%2355aaff"},
		{ID: "modern", Kind: "shape", Query: "element-style=modern&shape=square&top=20&width=12&height=7"},
	}}}}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 2 || scene.Objects[0].RetroLines == nil || len(scene.Objects[0].RetroLines.Lines) == 0 || scene.Objects[1].RetroLines != nil {
		t.Fatal("mixed shape adapters not preserved")
	}
	for _, l := range scene.Objects[0].RetroLines.Lines {
		if l.Element != 0 {
			t.Fatal("neighbour baked into shape")
		}
	}
}

func TestSceneTransparentRetroShapeRetainsCoverage(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{{ID: "shape", Kind: "shape", Query: "element-style=retro&shape=square&transparent=1&top=2&width=12&height=7&fg=%2355aaff"}}}}}
	before, _ := json.Marshal(d)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 {
		t.Fatal("missing transparent shape")
	}
	r := scene.Objects[0].RetroLines
	if r == nil || r.ShapeOpacity == nil || *r.ShapeOpacity != .5 || len(r.Lines) == 0 {
		t.Fatal("missing unblended fill coverage")
	}
	for _, l := range r.Lines {
		if l.Role == "shape" && len(l.Parts) == 0 {
			t.Fatal("discarded fill")
		}
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("transparent query changed in authored document")
	}
}

func TestSceneRetroStaticImageRetainsSamples(t *testing.T) {
	d := sceneFixture(t)
	for i := range d.Slides[0].Elements {
		if d.Slides[0].Elements[i].Kind == "image" {
			d.Slides[0].Elements[i].Query += "&element-style=retro&glyph=blocks"
		}
	}
	before, _ := json.Marshal(d)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "image" {
			found = true
			if object.RetroLines == nil || len(object.RetroLines.Lines) == 0 {
				t.Fatal("static image was not sampled")
			}
			if object.Media == nil || object.Media.Source == "" {
				t.Fatal("Retro image must retain its source for shared crop editing")
			}
			blocks := false
			for _, line := range object.RetroLines.Lines {
				for _, part := range line.Parts {
					blocks = blocks || strings.ContainsAny(part.Text, "█▀▄▘▝▖▗▌▐▙▟▛▜▚▞")
				}
			}
			if !blocks {
				t.Fatal("sampled image contains no block artwork")
			}
		}
	}
	if !found {
		t.Fatal("missing image")
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("sampling rewrote source asset")
	}
}

func TestSceneRetroConnectorPreservesRouteAndArrowSamples(t *testing.T) {
	d := sceneFixture(t)
	for i, e := range d.Slides[0].Elements {
		if e.Kind == "connector" {
			d.Slides[0].Elements[i].Query += "&element-style=retro"
		}
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range scene.Objects {
		if o.Kind == "connector" {
			found = true
			if o.RetroLines == nil || len(o.RetroLines.Lines) == 0 || len(o.Points) < 2 || o.Arrows != "end" {
				t.Fatal("connector lost raster or routing data")
			}
		}
	}
	if !found {
		t.Fatal("connector disappeared")
	}
}

func TestMixedSceneResolvesDefaultsWithoutChangingPreviewOrDocument(t *testing.T) {
	d := sceneFixture(t)
	d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	d.Slides[0].Elements[0].Query += "&element-style=modern"
	before, _ := json.Marshal(d)
	scene, err := buildMixedSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.ID == "title" {
			if o.Style != "modern" || o.RetroText != nil || o.RetroLines != nil {
				t.Fatal("explicit Modern override lost")
			}
		} else if o.Style != "retro" || o.RetroText == nil && o.RetroLines == nil && !(o.Kind == "text" && o.Text != nil && o.Text.FontID == "c64") {
			t.Fatalf("inherited Retro not dispatched for %s", o.ID)
		}
	}
	preview, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range preview.Objects {
		if o.Style != "modern" {
			t.Fatal("conversion preview unexpectedly inherited Retro")
		}
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("render materialised style overrides")
	}
	payload, _ := json.Marshal(scene)
	if bytes.Contains(payload, []byte("SECRET_")) {
		t.Fatal("mixed projection leaked workshop data")
	}
	d.Slides[0].DefaultStyle = "modern"
	scene, err = buildMixedSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.Style != "modern" {
			t.Fatal("slide default was ignored")
		}
	}
}

func TestMixedTransparentImageKeepsSampledCoverage(t *testing.T) {
	d := sceneFixture(t)
	for i, e := range d.Slides[0].Elements {
		if e.Kind == "image" {
			d.Slides[0].Elements[i].Query += "&element-style=retro&transparent=1"
		}
	}
	scene, err := buildMixedSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range scene.Objects {
		if o.ID == "image" {
			found = true
			r := o.RetroLines
			if r == nil || r.ContentOpacity == nil || *r.ContentOpacity != .5 || len(r.Lines) == 0 {
				t.Fatal("missing image alpha/coverage")
			}
			for _, l := range r.Lines {
				if l.Role == "transparent-image" {
					t.Fatal("image coverage still deferred to old compositor")
				}
			}
		}
	}
	if !found {
		t.Fatal("missing image")
	}
}
