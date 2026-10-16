package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestThemeColourSelectionRoleHistoryAndThemeChanges(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := capabilityFixture()
		s.deck.Theme = builtinDeckTheme("clean-light")
		if master {
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.masterMode = true
			s.currentMaster = 0
		}
		before := cloneDeck(s.deck)
		version := s.version
		a := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, SceneMaster: master, ObjectIDs: []string{"text-a", "text-b", "shape-a"}, Property: "paint.colorToken", PropertyValue: json.RawMessage(`"accent-1"`)}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		property := s.transportState().SelectionCapabilities.Properties["paint.colorToken"]
		if property.State != "value" || property.Value != "accent-1" || property.Provenance != "explicit" {
			t.Fatal("role did not accept", property)
		}
		accepted := cloneDeck(s.deck)
		depth := len(s.undo)
		a.SceneRevision = &s.version
		if err := s.apply(a); err != nil || len(s.undo) != depth {
			t.Fatal("role no-op made history", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("role undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(accepted, s.deck) {
			t.Fatal("role redo failed", err)
		}
		s.deck.Theme.Colors.Accent[0] = "#0055aa"
		if got := s.transportState().SelectionCapabilities.Properties["paint.color"].Value; got != "#0055aa" {
			t.Fatal("theme change froze assigned colour", got)
		}
	}
}

func TestThemeColourAllRolesResolveWithCompatibilityOrAuthoredTheme(t *testing.T) {
	for _, authored := range []bool{false, true} {
		for _, role := range []string{"background", "surface", "text", "heading", "muted", "line", "accent-1", "accent-2", "accent-3", "accent-4", "accent-5", "accent-6"} {
			s := capabilityFixture()
			if authored {
				s.deck.Theme = builtinDeckTheme("clean-light")
			}
			version := s.version
			data, _ := json.Marshal(role)
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a"}, Property: "paint.colorToken", PropertyValue: data}); err != nil {
				t.Fatal(role, err)
			}
			s.selected = 0
			s.selection = map[int]bool{0: true}
			p := s.transportState().SelectionCapabilities.Properties
			if p["paint.colorToken"].Value != role || p["paint.color"].Value != s.deck.themeTokenColor(role) {
				t.Fatal("role did not resolve", role, p)
			}
			if !authored && s.deck.Theme != nil {
				t.Fatal("role unexpectedly authored compatibility theme")
			}
		}
	}
}

func TestThemeColourSelectorValidationAndMixedTokenSemantics(t *testing.T) {
	for _, value := range []string{`"custom"`, `"accent-7"`, `"#ff55aa"`, `"Text"`, `23`, `null`} {
		s := capabilityFixture()
		version := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a", "shape-a"}, Property: "paint.colorToken", PropertyValue: json.RawMessage(value)})
	}
	s := capabilityFixture()
	s.deck.Theme = builtinDeckTheme("clean-light")
	s.deck.Theme.Colors.Accent[0] = s.deck.Theme.Colors.Accent[1]
	s.deck.Slides[0].Elements[0].Query += "&theme-fg=accent-1"
	s.deck.Slides[0].Elements[1].Query += "&theme-fg=accent-2"
	s.selection = map[int]bool{0: true, 1: true}
	s.selected = 0
	p := s.transportState().SelectionCapabilities.Properties
	if p["paint.colorToken"].State != "mixed" || p["paint.color"].State != "value" {
		t.Fatal("equal RGB collapsed distinct semantic roles", p)
	}
	s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, Element{ID: "image", Kind: "image", Query: "object-locked=1"})
	version := s.version
	assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a", "image"}, Property: "paint.colorToken", PropertyValue: json.RawMessage(`"text"`)})
}

func themeColourFixture() Deck {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	d.Masters.Layouts[0].ID = "colours"
	d.Masters.Layouts[0].Slide.Elements = []Element{{ID: "master-title", SlotID: "title", PlaceholderRole: "title", Kind: "heading", Level: 1, Text: "Master", Query: "fg=%23aabbcc&header=%23aa0055&shadow=solid"}}
	d.Slides = []Slide{{ID: "slide", LayoutID: "colours", Elements: []Element{{ID: "instance", MasterSlotID: "title", Kind: "heading", Level: 1, Text: "Instance", Query: "master-clear=shadow&top=4&width=100&height=8"}}}}
	return d
}

func TestThemeColourOverridesMasterAliasesAndInheritRestoresThem(t *testing.T) {
	d := themeColourFixture()
	s := newNativeEditorSession("Colours.md", d)
	s.selected = 0
	s.selection = map[int]bool{0: true}
	edit := func(property, value string) {
		t.Helper()
		version := s.version
		data, _ := json.Marshal(value)
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, Property: property, PropertyValue: data}); err != nil {
			t.Fatal(err)
		}
	}
	color := func() string {
		t.Helper()
		scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range scene.Objects {
			if o.ID == "instance" {
				return o.Paint.Color
			}
		}
		t.Fatal("missing instance")
		return ""
	}
	if got := color(); got != "#aa0055" {
		t.Fatal("fixture inheritance", got)
	}
	edit("paint.colorToken", "accent-1")
	if got := color(); got != s.deck.Theme.Colors.Accent[0] {
		t.Fatal("master fg/header beat token", got)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("fg") || q.Has("header") || q.Get("master-clear") != "fg,header,shadow" {
		t.Fatal("alias clearing did not stay scoped", q)
	}
	path := filepath.Join(t.TempDir(), "Colours.md")
	if err := saveDeck(path, s.deck); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	s.deck = loaded
	if got := color(); got != s.deck.Theme.Colors.Accent[0] {
		t.Fatal("portable token lost", got)
	}
	edit("paint.color", "#55aaff")
	if got := color(); got != "#55aaff" {
		t.Fatal("master header beat explicit RGB", got)
	}
	if got := s.transportState().SelectionCapabilities.Properties["paint.colorToken"].Value; got != "custom" {
		t.Fatal("RGB not shown as custom", got)
	}
	edit("paint.colorToken", "")
	if got := color(); got != "#aa0055" {
		t.Fatal("inherit did not restore master colour", got)
	}
	q, _ = url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Get("master-clear") != "shadow" || q.Get("top") != "4" {
		t.Fatal("inherit changed unrelated metadata", q)
	}
}

func TestThemeColourStyleTransferKeepsRoleAcrossMasterAliases(t *testing.T) {
	d := themeColourFixture()
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "source", Kind: "text", Text: "Token source", Query: "theme-fg=accent-2&top=20&modern-size=60"})
	s := newNativeEditorSession("Style.md", d)
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "copy-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"source"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, StyleData: s.styleClipboard.Clone()}); err != nil {
		t.Fatal(err)
	}
	resolved := s.deck.ResolveSlide(0, false)
	for _, e := range resolved.Elements {
		if e.ID == "instance" {
			q, _ := url.ParseQuery(e.Query)
			if q.Has("header") || q.Has("fg") || q.Get("theme-fg") != "accent-2" {
				t.Fatal("pasted token lost precedence", q)
			}
			if q.Get("shadow") != "solid" {
				t.Fatal("absent pasted shadow did not restore master inheritance")
			}
		}
	}
}

func TestThemeColourResetStyleClearsSuppressionButKeepsGeometry(t *testing.T) {
	d := themeColourFixture()
	e := &d.Slides[0].Elements[0]
	e.Query = "master-clear=shadow,modern-size,left&fg=%2355ffff&modern-size=64&top=4&width=100&height=8"
	s := newNativeEditorSession("Reset.md", d)
	version := s.version
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "reset-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}}); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Get("master-clear") != "left" || q.Get("top") != "4" || q.Get("width") != "100" || q.Get("height") != "8" || q.Has("fg") || q.Has("modern-size") {
		t.Fatal("reset suppressed master style or changed geometry", q)
	}
	for _, e := range s.deck.ResolveSlide(0, false).Elements {
		if e.ID == "instance" {
			q, _ = url.ParseQuery(e.Query)
			if q.Get("header") != "#aa0055" || q.Get("shadow") != "solid" || e.Text != "Instance" {
				t.Fatal("reset did not restore master style/content", e)
			}
		}
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("reset undo failed", err)
	}
}

func TestThemeColourCopyPreservesInheritedRole(t *testing.T) {
	d := themeColourFixture()
	d.Masters.Layouts[0].Slide.Elements[0].Query = "theme-fg=accent-3"
	snapshot, err := captureObjectStyle(d, d.Slides[0], d.ResolveSlide(0, false), "instance")
	if err != nil || snapshot.Fields["paint.color"].Token != "accent-3" || snapshot.Fields["paint.color"].Value != d.Theme.Colors.Accent[2] {
		t.Fatal("inherited role flattened during copy", err, snapshot)
	}
}

func TestThemeColourVisualFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_THEME_COLOUR_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	d := themeColourFixture()
	d.Slides[0].Elements[0].Text = "Master-bound colour"
	d.Slides[0].Elements[0].Query = "master-clear=shadow&left=15&top=8&width=215&height=12"
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "body", Kind: "text", Text: "Choose a theme role, then change the palette.", Query: "left=15&top=27&width=215&height=8&render=truetype&modern-font=sans&modern-size=64"})
	if err := saveDeck(filepath.Join(root, "colours.md"), d); err != nil {
		t.Fatal(err)
	}
}
