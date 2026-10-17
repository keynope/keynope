package app

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelectionPaintAcceptedValuesAndHistory(t *testing.T) {
	edits := []struct {
		property, json string
		accepted       interface{}
	}{
		{"paint.color", `"#AA55FF"`, "#aa55ff"},
		{"paint.outline", `"light"`, "light"},
		{"paint.outline", `"none"`, "none"},
		{"paint.gradientDirection", `"vertical"`, "vertical"},
		{"paint.gradientStart", `"#FF0000"`, "#ff0000"},
		{"paint.gradientEnd", `"#00FF00"`, "#00ff00"},
		{"paint.gradientDirection", `"none"`, "none"},
		{"paint.shadow", `"solid"`, "solid"},
		{"paint.shadowColor", `"#5555AA"`, "#5555aa"},
		{"paint.shadowX", `-3`, -3.0},
		{"paint.shadowY", `4`, 4.0},
		{"paint.shadow", `"none"`, "none"},
	}
	for _, master := range []bool{false, true} {
		for _, edit := range edits {
			t.Run(map[bool]string{false: "slide", true: "master"}[master]+"/"+edit.property+"/"+edit.json, func(t *testing.T) {
				s := capabilityFixture()
				if master {
					s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
					s.masterMode, s.currentMaster = true, 0
				}
				before := cloneDeck(s.deck)
				version := s.version
				action := nativeEditorAction{Action: "set-selection-property", Property: edit.property, PropertyValue: json.RawMessage(edit.json), SceneRevision: &version, SceneMaster: master, Slide: 0, ObjectIDs: []string{"text-a", "text-b", "shape-a"}}
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				property := s.actionTransportState(action.Action).SelectionCapabilities.Properties[edit.property]
				if property.State != "value" || !reflect.DeepEqual(property.Value, edit.accepted) || property.Provenance != "explicit" {
					t.Fatalf("wrong accepted paint: %+v", property)
				}
				if len(s.undo) != 1 || s.version != version+1 {
					t.Fatal("not one transaction")
				}
				after := cloneDeck(s.deck)
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("undo", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
					t.Fatal("redo", err)
				}
				version = s.version
				depth := len(s.undo)
				if err := s.apply(action); err != nil || s.version != version || len(s.undo) != depth {
					t.Fatal("exact repeated paint dirtied history", err)
				}
			})
		}
	}
}

func TestSelectionPaintSubsetAndContentIsolation(t *testing.T) {
	s := capabilityFixture()
	s.deck.Slides[0].Elements[0].Query += "&header=%2300ff00&theme-fg=heading&link=https%3A%2F%2Fexample.com&object-rotation=12"
	s.deck.Slides[0].Elements[0].Text = "Base [color=#ffffff]inline white[/color]"
	s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, Element{ID: "paint-image", Kind: "image", Path: "photo.png", Query: "brightness=1.7&glyph=braille&image-style=retro"}, Element{ID: "paint-line", Kind: "connector", Query: "connector-width=3"})
	s.selection = map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true}
	caps := s.transportState().SelectionCapabilities
	if p := caps.Properties["paint.color"]; len(p.EligibleIDs) != 4 || p.TotalCount != 5 {
		t.Fatalf("colour subset: %+v", p)
	}
	if p := caps.Properties["paint.shadow"]; len(p.EligibleIDs) != 4 || p.TotalCount != 5 {
		t.Fatalf("effect subset: %+v", p)
	}
	before := cloneSlide(s.deck.Slides[0])
	for _, edit := range []struct{ property, value string }{{"paint.color", `"#ff55aa"`}, {"paint.shadow", `"soft"`}} {
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: caps.TargetIDs, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}); err != nil {
			t.Fatal(err)
		}
	}
	for i, e := range s.deck.Slides[0].Elements {
		q, _ := url.ParseQuery(e.Query)
		old, _ := url.ParseQuery(before.Elements[i].Query)
		for _, key := range []string{"modern-size", "modern-font", "width", "height", "top", "link", "object-rotation", "brightness", "glyph", "image-style", "connector-width"} {
			if q.Get(key) != old.Get(key) {
				t.Fatalf("paint changed %s of %s", key, e.ID)
			}
		}
		if e.Text != before.Elements[i].Text || e.Path != before.Elements[i].Path {
			t.Fatal("paint changed content")
		}
		if e.Kind == "image" && q.Has("fg") {
			t.Fatal("base colour altered image")
		}
		if e.Kind == "connector" && q.Has("shadow") {
			t.Fatal("effect altered connector")
		}
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("header") || q.Has("theme-fg") || q.Get("fg") != "#ff55aa" {
		t.Fatal("colour aliases not replaced", q)
	}
}

func TestSelectionPaintRejectsInvalidValuesAtomically(t *testing.T) {
	for _, edit := range []struct{ property, value string }{{"paint.color", `"red"`}, {"paint.color", `12`}, {"paint.outline", `"theme"`}, {"paint.outline", `"bogus"`}, {"paint.shadow", `"hard"`}, {"paint.gradientDirection", `"radial"`}, {"paint.gradientEnd", `"url(foo)"`}, {"paint.shadowX", `4.1`}, {"paint.shadowY", `-5`}, {"paint.shadowX", `"1"`}} {
		t.Run(edit.property+edit.value, func(t *testing.T) {
			s := capabilityFixture()
			version := s.version
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Property: edit.property, PropertyValue: json.RawMessage(edit.value), Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a", "text-b", "shape-a"}})
		})
	}
	for _, scenario := range []string{"locked-excluded", "stale", "missing-id"} {
		s := capabilityFixture()
		s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, Element{ID: "excluded", Kind: "image", Query: "object-locked=1"})
		ids := []string{"text-a", "excluded"}
		version := s.version
		if scenario == "stale" {
			ids = ids[:1]
			version--
		}
		if scenario == "missing-id" {
			ids = []string{"text-a", "missing"}
		}
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Property: "paint.color", PropertyValue: json.RawMessage(`"#ffffff"`), Slide: 0, SceneRevision: &version, ObjectIDs: ids})
	}
}

func TestSelectionPaintOffInheritAndProjection(t *testing.T) {
	s := capabilityFixture()
	s.deck.Theme = builtinDeckThemes()[0].Clone()
	s.selection = map[int]bool{2: true}
	s.selected = 2
	for _, property := range []string{"paint.outline", "paint.shadow", "paint.gradientDirection"} {
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: property, PropertyValue: json.RawMessage(`"none"`), Slide: 0, SceneRevision: &version, ObjectIDs: []string{"shape-a"}}); err != nil {
			t.Fatal(err)
		}
	}
	e := s.deck.ResolveSlide(0, false).Elements[2]
	q, _ := url.ParseQuery(e.Query)
	q.Set("gradient-start", "#ff0000")
	q.Set("gradient-end", "#0000ff")
	e.Query = q.Encode()
	p := s.deck.applyThemeObjectPaint(e, s.deck.ResolveSlide(0, false), q, scenePaintFor(e, s.deck.ResolveSlide(0, false), 1, 1))
	if p.StrokeWidth != 0 || p.ShadowColor != "" || p.GradientStart != "" || elementHasOutline(e) {
		t.Fatal("off did not suppress renderer effects", p)
	}
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: "paint.outline", PropertyValue: json.RawMessage(`""`), Slide: 0, SceneRevision: &version, ObjectIDs: []string{"shape-a"}}); err != nil {
		t.Fatal(err)
	}
	if state := s.transportState().SelectionCapabilities.Properties["paint.outline"]; state.Provenance != "inherited" {
		t.Fatalf("inherit did not clear override: %+v", state)
	}
	// A mode change keeps endpoints; disabling retains them for re-enabling.
	shape := &s.deck.Slides[0].Elements[2]
	shape.Query = setQueryValue(shape.Query, "gradient-start", "#aaff55")
	shape.Query = setQueryValue(shape.Query, "gradient-end", "#ff0055")
	for _, mode := range []string{"diagonal", "none", "vertical"} {
		version = s.version
		data, _ := json.Marshal(mode)
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: "paint.gradientDirection", PropertyValue: data, Slide: 0, SceneRevision: &version, ObjectIDs: []string{"shape-a"}}); err != nil {
			t.Fatal(err)
		}
	}
	q, _ = url.ParseQuery(s.deck.Slides[0].Elements[2].Query)
	if q.Get("gradient-start") != "#aaff55" || q.Get("gradient-end") != "#ff0055" {
		t.Fatal("mode change lost colour stops", q)
	}
}

func TestSelectionPaintOffSurvivesMarkdownRoundTrip(t *testing.T) {
	s := capabilityFixture()
	s.deck.Slides[0].Elements[0].Query += "&outline=none&gradient-start=%23ff0000&gradient-end=%230000ff&gradient-dir=none&shadow=none&shadow-color=%23ffffff&shadow-x=-2&shadow-y=3"
	s.deck.Slides[0].Elements[1].Query += "&outline=light"
	path := filepath.Join(t.TempDir(), "paint.md")
	if err := saveDeck(path, s.deck); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"text-a", "text-b"} {
		var original, current Element
		for _, e := range s.deck.Slides[0].Elements {
			if e.ID == id {
				original = e
			}
		}
		for _, e := range parsed.Slides[0].Elements {
			if e.ID == id {
				current = e
			}
		}
		a, _ := url.ParseQuery(original.Query)
		b, _ := url.ParseQuery(current.Query)
		for _, key := range []string{"outline", "gradient-start", "gradient-end", "gradient-dir", "shadow", "shadow-color", "shadow-x", "shadow-y"} {
			if a.Get(key) != b.Get(key) {
				t.Fatalf("%s lost %s: %q != %q", id, key, a.Get(key), b.Get(key))
			}
		}
	}
}

func TestSelectionPaintMasterInheritanceStaysPropertyScoped(t *testing.T) {
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Layouts: []MasterLayout{{ID: "paint-layout", Name: "Paint", Slide: Slide{Elements: []Element{{ID: "paint-title-master", SlotID: "paint-title", Kind: "heading", Level: 1, Text: "Title", Query: "fg=%23aa55ff&gradient-start=%23ff0000&gradient-end=%230000ff&gradient-dir=horizontal&shadow=solid&shadow-color=%23ffffaa"}}}}}}, Slides: []Slide{{LayoutID: "paint-layout", Elements: []Element{{ID: "paint-title-local", MasterSlotID: "paint-title", Kind: "heading", Level: 1, Text: "Local title", Query: "top=3&width=100&height=12"}}}}}
	deck.Masters.Layouts[0].Slide.Elements[0].PlaceholderRole = "title"
	s := newNativeEditorSession("", deck, false)
	s.selected = 0
	s.selection = map[int]bool{0: true}
	before := cloneDeck(s.deck)
	for property, want := range map[string]string{"paint.color": "#aa55ff", "paint.gradientStart": "#ff0000", "paint.gradientEnd": "#0000ff", "paint.gradientDirection": "horizontal", "paint.shadow": "solid", "paint.shadowColor": "#ffffaa"} {
		state := s.transportState().SelectionCapabilities.Properties[property]
		if state.Value != want || state.Provenance != "inherited" {
			t.Fatalf("%s inherited state: %+v", property, state)
		}
	}
	for _, edit := range []struct{ property, value string }{{"paint.gradientDirection", `"vertical"`}, {"paint.shadow", `"none"`}, {"paint.color", `"#00ff55"`}, {"paint.color", `""`}} {
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: edit.property, PropertyValue: json.RawMessage(edit.value), Slide: 0, SceneRevision: &version, ObjectIDs: []string{"paint-title-local"}}); err != nil {
			t.Fatal(err)
		}
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("gradient-start") || q.Has("gradient-end") || q.Has("shadow-color") || q.Has("fg") || q.Get("gradient-dir") != "vertical" {
		t.Fatal("property edit flattened inheritance", q)
	}
	state := s.transportState().SelectionCapabilities
	if state.Properties["paint.gradientStart"].Value != "#ff0000" || state.Properties["paint.gradientEnd"].Value != "#0000ff" || state.Properties["paint.color"].Value != "#aa55ff" || state.Properties["paint.shadow"].Value != "none" {
		t.Fatal("property edit lost resolved inheritance", state)
	}
	if !reflect.DeepEqual(s.deck.Masters, before.Masters) {
		t.Fatal("property edit mutated authored master")
	}
}
