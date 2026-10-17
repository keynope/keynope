package app

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShapeStrokeThemeRole(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("Stroke.md", strokeInheritanceFixture())
		id := "instance"
		if master {
			s.masterMode, s.currentMaster, id = true, 1, "master"
		}
		before := cloneDeck(s.deck)
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: s.state().Current, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{id}, Property: "paint.strokeToken", PropertyValue: json.RawMessage(`"accent-2"`)}); err != nil {
			t.Fatal(err)
		}
		check := func(d Deck, color string) {
			t.Helper()
			resolved := d.ResolveSlide(0, false)
			for _, element := range resolved.Elements {
				if element.ID == "instance" {
					q, _ := url.ParseQuery(element.Query)
					if width := resolvedShapeStrokeWidth(d.applyShapeStrokeDefaults(resolved), element, q, 245, 56); width != 7.5 {
						t.Fatalf("themed border lost routing width: %g", width)
					}
				}
			}
			scene, err := buildDocumentSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range scene.Objects {
				if object.ID == "instance" {
					if object.Paint.Stroke != color || object.Paint.StrokeWidth != 7.5 {
						t.Fatalf("wrong themed stroke: %+v", object.Paint)
					}
					return
				}
			}
			t.Fatal("missing shape")
		}
		check(s.deck, s.deck.Theme.Colors.Accent[1])
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("undo", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("redo", err)
		}
		s.deck.Theme.Colors.Accent[1] = "#ab3456"
		check(s.deck, "#ab3456")
		file := filepath.Join(t.TempDir(), "stroke.md")
		if err := saveDeck(file, s.deck); err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeck(file)
		if err != nil {
			t.Fatal(err)
		}
		check(loaded, "#ab3456")
		snapshot, err := captureObjectStyle(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), "instance")
		if err != nil {
			t.Fatal(err)
		}
		if field := snapshot.Fields["paint.strokeColor"]; field.Token != "accent-2" || field.Value != "#ab3456" {
			t.Fatal(field)
		}
		if err := snapshot.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, withTheme := range []bool{false, true} {
			d := Deck{Slides: []Slide{{Elements: []Element{{ID: "target", Kind: "shape", Query: "shape=square"}}}}}
			want := "#ab3456"
			if withTheme {
				d.Theme = builtinDeckTheme("clean-light")
				want = "theme:accent-2"
			}
			if _, err := applyObjectStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"target"}, StyleData: snapshot}, ""); err != nil {
				t.Fatal(err)
			}
			q, _ := url.ParseQuery(d.Slides[0].Elements[0].Query)
			if q.Get("shape-stroke") != want {
				t.Fatalf("paste theme=%v: %v", withTheme, q)
			}
		}
	}
}
