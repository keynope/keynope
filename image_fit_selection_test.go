package main

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestImageFitPreservesNaturalFrameAndHistory(t *testing.T) {
	for _, style := range []string{"modern", "retro"} {
		for _, fit := range []string{"contain", "cover", "stretch", "modern", "retro"} {
			if fit == style {
				continue
			}
			d := sceneFixture(t)
			d.Slides[0].Engagement = nil
			d.Slides[0].EngagementResult = nil
			for i, e := range d.Slides[0].Elements {
				if e.ID == "image" {
					d.Slides[0].Elements[i].Query = "image-style=" + style + "&align=center&top=10&width=100&height=30&brightness=1.4"
				}
			}
			s := newNativeEditorSession("Fit.md", d)
			before := cloneDeck(s.deck)
			getBounds := func() sceneRect {
				scene, err := buildSlideScene(s.deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				for _, o := range scene.Objects {
					if o.ID == "image" {
						return o.Bounds
					}
				}
				t.Fatal("missing image")
				return sceneRect{}
			}
			old := getBounds()
			revision := s.version
			value, _ := json.Marshal(fit)
			property := "image.fit"
			if fit == "modern" || fit == "retro" {
				property = "image.style"
			}
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"image"}, Property: property, PropertyValue: value}); err != nil {
				t.Fatal(err)
			}
			next := getBounds()
			for _, pair := range [][2]float64{{old.X, next.X}, {old.Y, next.Y}, {old.Width, next.Width}, {old.Height, next.Height}} {
				if math.Abs(pair[0]-pair[1]) > 1e-6 {
					t.Fatalf("%s %s moved frame: %+v -> %+v", style, fit, old, next)
				}
			}
			after := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("undo did not restore natural frame", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("redo did not restore fitting", err)
			}
		}
	}
}
