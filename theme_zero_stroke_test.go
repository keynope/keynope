package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestThemeZeroStrokeWidthIsExplicitOff(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, test := range []struct {
		name, query string
		width       float64
	}{
		{"inherited", "", 0},
		{"explicit-color", "&shape-stroke=%23ff0055", 0},
		{"theme-color", "&shape-stroke=theme%3Aaccent-2", 0},
		{"explicit-width", "&shape-stroke=%23ff0055&shape-stroke-width=3.5", 3.5},
		{"outline-preset", "&outline=light", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{PageNumber: "hide", Elements: []Element{{ID: "shape", Kind: "shape", Query: "shape=square&left=30&top=10&width=30&height=10" + test.query}}}}}
			d.Theme.Defaults.LineWidth = 0
			for _, deck := range []Deck{d, roundTripAppearanceDeck(t, d)} {
				scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
				if err != nil || len(scene.Objects) != 1 {
					t.Fatal(err, scene.Objects)
				}
				o := scene.Objects[0]
				if o.Paint.StrokeWidth != test.width {
					t.Fatalf("theme Off became a fallback stroke: %+v", o.Paint)
				}
				resolved := deck.ResolveSlide(0, false)
				caps := buildSelectionCapabilities(deck, deck.Slides[0], resolved, []string{"shape"}, "", 0)
				if got := caps.Properties["paint.strokeWidth"].Value; got != test.width {
					t.Fatalf("inspector width differs from rendered width: %v != %g", got, test.width)
				}
				slide := deck.applyShapeStrokeDefaults(deck.ResolveSlide(0, false))
				q := slideElementQuery(slide, nil, 0)
				if width := resolvedShapeStrokeWidth(slide, slide.Elements[0], q, 245, 56); width != test.width {
					t.Fatalf("routing disagrees with rendered stroke: %g != %g", width, test.width)
				}
				ppt, _ := sceneObjectToPPTX(o)
				if test.width == 0 && ppt.Stroke != "" {
					t.Fatal("PPTX emitted a border for theme Off", ppt.Stroke)
				}
			}
		})
	}
}

func TestThemeStrokeWidthResetKeepsZeroAndFollowsTheme(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide", PageNumber: "hide", Elements: []Element{{ID: "shape", Kind: "shape", Query: "shape=square&left=30&top=10&width=30&height=10&shape-stroke=theme%3Aaccent-2"}}}}}
		d.Theme.Defaults.LineWidth = 0
		s := newNativeEditorSession("Widths.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		}
		before := cloneDeck(s.deck)
		set := func(value json.RawMessage) {
			t.Helper()
			revision := s.version
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"shape"}, Property: "paint.strokeWidth", PropertyValue: value}); err != nil {
				t.Fatal(master, err)
			}
		}
		width := func(want float64) {
			t.Helper()
			scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			// In master mode inspect the owned Base shape, not the normal slide.
			if master {
				scene, err = buildStyledSlideScene(s.deck, s.deck.Masters.Base.Slide, 0, 245, 56, true)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, o := range scene.Objects {
				if o.ID == "shape" {
					if o.Paint.StrokeWidth != want {
						t.Fatalf("master=%v: got %g want %g", master, o.Paint.StrokeWidth, want)
					}
					return
				}
			}
			t.Fatal("missing shape")
		}
		width(0)
		set(json.RawMessage(`3.5`))
		width(3.5)
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("stroke Undo changed unrelated state", master, err)
		}
		width(0)
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("stroke Redo changed unrelated state", master, err)
		}
		set(json.RawMessage(`""`))
		width(0)
		theme := s.deck.Theme.Clone()
		theme.Defaults.LineWidth = 4.25
		if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
			t.Fatal(err)
		}
		width(4.25)
	}
}
