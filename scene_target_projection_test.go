package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestTargetProjectionMatchesFullScene(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := sceneFixture(t)
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		resolved := deck.ResolveSlide(0, false)
		full, err := buildStyledSlideScene(deck, resolved, 0, 245, 56, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range full.Objects {
			t.Run(style+"/"+object.ID, func(t *testing.T) {
				target, err := buildStyledSceneTarget(deck, resolved, 0, 245, 56, true, object.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(target.Objects) != 1 || !reflect.DeepEqual(target.Objects[0], object) {
					t.Fatal("target projection differs from full scene")
				}
			})
		}
	}
}

func BenchmarkTargetProjection(b *testing.B) {
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}}
	var slide Slide
	for i := 0; i < 500; i++ {
		slide.Elements = append(slide.Elements, Element{ID: fmt.Sprint("text-", i), Kind: "text", Text: "Dense slide text", Query: fmt.Sprintf("render=truetype&top=%d&left=%d&width=30&height=3&modern-size=32", i%16*3, i%8*30)})
	}
	deck.Slides = []Slide{slide}
	for _, target := range []string{"", "text-250"} {
		name := target
		if name == "" {
			name = "full"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := buildStyledSceneTarget(deck, slide, 0, 245, 56, true, target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
