package main

import (
	"reflect"
	"testing"
)

func TestShapeStyleCopyKeepsResolvedStrokeDefaults(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, detached := range []bool{false, true} {
		for _, width := range []float64{0, 1.75} {
			for _, noStroke := range []bool{false, true} {
				source := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "source", Elements: []Element{{ID: "source-shape", Kind: "shape", Query: "shape=square&left=10&top=10&width=60&height=15"}}}}}
				source.Theme.Defaults.LineWidth = width
				if noStroke {
					source.Theme.Defaults.ShapeLine = "none"
				}
				if detached {
					var err error
					source, err = concreteThemeDeck(source)
					if err != nil {
						t.Fatal(err)
					}
				}
				resolved := source.ResolveSlide(0, false)
				caps := buildSelectionCapabilities(source, source.Slides[0], resolved, []string{"source-shape"}, "", 0)
				if caps.Properties["paint.strokeWidth"].Value != width {
					t.Fatalf("detached=%v noStroke=%v: inspector did not preserve configured width %g: %+v", detached, noStroke, width, caps.Properties["paint.strokeWidth"])
				}
				snapshot, err := captureObjectStyle(source, source.Slides[0], resolved, "source-shape")
				if err != nil {
					t.Fatal(err)
				}
				if !snapshot.Fields["paint.strokeColor"].Present || !snapshot.Fields["paint.strokeWidth"].Present {
					t.Fatalf("resolved stroke omitted from snapshot: detached=%v none=%v width=%g", detached, noStroke, width)
				}
				target := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("bold"), Slides: []Slide{{ID: "target", Elements: []Element{{ID: "target-shape", Kind: "shape", Query: "shape=circle&left=90&top=15&width=40&height=20&shape-stroke=%23ff0055&shape-stroke-width=7"}}}}}
				before := cloneDeck(target)
				beforeScene, err := buildSlideScene(before, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				changed, err := applyObjectStyle(&target, nativeEditorAction{Action: "paste-object-style", Slide: 0, ObjectIDs: []string{"target-shape"}, StyleData: snapshot}, "")
				if err != nil || !changed {
					t.Fatal("style paste failed", err)
				}
				want, we := buildSlideScene(source, 0, 245, 56)
				got, ge := buildSlideScene(target, 0, 245, 56)
				if we != nil || ge != nil {
					t.Fatal(we, ge)
				}
				wp, gp := want.Objects[0].Paint, got.Objects[0].Paint
				if wp.Color != gp.Color || wp.NoFill != gp.NoFill || wp.Stroke != gp.Stroke || wp.Stroke != "" && wp.StrokeWidth != gp.StrokeWidth {
					t.Fatalf("style paste lost appearance detached=%v none=%v width=%g: want %+v got %+v", detached, noStroke, width, wp, gp)
				}
				if got.Objects[0].Bounds != beforeScene.Objects[0].Bounds || before.Slides[0].Elements[0].Text != target.Slides[0].Elements[0].Text || got.Objects[0].Shape != beforeScene.Objects[0].Shape {
					t.Fatal("style transfer changed geometry/content")
				}
			}
		}
	}
}

func TestShapeResolvedStyleTransferHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", Elements: []Element{
			{ID: "source", Kind: "shape", Query: "shape=square&left=10&top=4&width=60&height=15"},
			{ID: "target", Kind: "shape", Query: "shape=circle&left=100&top=4&width=60&height=15&shape-stroke=%23ff0055&shape-stroke-width=7"},
		}}}}
		deck.Theme.Defaults.LineWidth = 0
		deck, err := concreteThemeDeck(deck)
		if err != nil {
			t.Fatal(err)
		}
		if master {
			deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
			deck.Masters.Base.Slide.ID = "base-style"
		}
		s := newNativeEditorSession("strokes.md", deck)
		s.masterMode = master
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "copy-object-style", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"source"}}); err != nil {
			t.Fatal(err)
		}
		before := cloneDeck(s.deck)
		apply := func(action string) {
			t.Helper()
			version := s.version
			if err := s.apply(nativeEditorAction{Action: action, Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"target"}, StyleData: s.styleClipboard.Clone()}); err != nil {
				t.Fatal(err)
			}
		}
		apply("paste-object-style")
		after := cloneDeck(s.deck)
		if len(s.undo) != 1 {
			t.Fatal("paste did not create one history entry")
		}
		version = s.version
		apply("paste-object-style")
		if version != s.version || len(s.undo) != 1 {
			t.Fatal("identical paste was not a no-op")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("stroke style undo changed authored state", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("stroke style redo changed authored state", err)
		}
		data, err := serializeDeck("strokes.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("strokes.md", data)
		if err != nil {
			t.Fatal(err)
		}
		view := loaded.Slides[0]
		if master {
			view = loaded.Masters.Base.Slide
		}
		caps := buildSelectionCapabilities(loaded, view, view, []string{"target"}, "", 0)
		if caps.Properties["paint.strokeWidth"].Value != float64(0) {
			t.Fatal("Off width changed on save/reopen", caps.Properties["paint.strokeWidth"])
		}
	}
}
