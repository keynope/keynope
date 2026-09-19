package main

import (
	"reflect"
	"testing"
)

func TestSceneImageRotationKeepsLocalCropArtwork(t *testing.T) {
	for _, style := range []string{"modern", "retro"} {
		t.Run(style, func(t *testing.T) {
			deck := sceneFixture(t)
			deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
			e := deck.Slides[0].Elements[len(deck.Slides[0].Elements)-1]
			e.Query = "top=10&left=20&width=60&height=12&modern-mask=ellipse&modern-crop=0.2,0.1,0,0"
			deck.Slides[0].Elements = []Element{e}
			before, err := buildMixedSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			for orientation, angle := range map[string]float64{"cw": 90, "down": 180, "ccw": -90, "flip": 180} {
				deck.Slides[0].Elements[0].Query = e.Query + "&orientation=" + orientation
				authored := deck.Slides[0].Elements[0].Query
				scene, err := buildMixedSlideScene(deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				if len(scene.Objects) != 1 {
					t.Fatal("image missing")
				}
				object := scene.Objects[0]
				if object.Rotation != angle {
					t.Fatalf("%s rotation=%g", orientation, object.Rotation)
				}
				object.Rotation = 0
				if !reflect.DeepEqual(object, before.Objects[0]) {
					t.Fatalf("%s changed local artwork/bounds/crop", orientation)
				}
				if deck.Slides[0].Elements[0].Query != authored {
					t.Fatal("authored rotation mutated")
				}
			}
		})
	}
}
