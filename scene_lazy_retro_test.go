package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func themedSharedTextFixture(count int) Deck {
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern", Extra: map[string]json.RawMessage{"theme": json.RawMessage(`"studio-v1"`)}}, Slides: []Slide{{PageNumber: "hide"}}}
	for i := 0; i < count; i++ {
		style := "modern"
		if i%2 == 0 {
			style = "retro"
		}
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprint("text-", i), Kind: "text", Text: "Shared text", Query: fmt.Sprintf("element-style=%s&render=truetype&ttf-size=32&modern-size=32&left=%d&top=%d&width=30&height=3", style, i%8*30, i%16*3)})
	}
	return deck
}

func TestThemedSharedTextKeepsTreatmentPaint(t *testing.T) {
	deck := themedSharedTextFixture(20)
	scene, err := buildMixedSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 20 {
		t.Fatalf("objects: %d", len(scene.Objects))
	}
	for i, object := range scene.Objects {
		style := deck.elementStyle(deck.Slides[0], deck.Slides[0].Elements[i])
		if object.Style != style || object.Text == nil || object.RetroText != nil || object.RetroLines != nil {
			t.Fatalf("shared text not retained: %+v", object)
		}
		if object.Paint.Color != deck.themeColors(style).Text {
			t.Fatalf("wrong %s text colour: %+v", style, object.Paint)
		}
	}
}

func BenchmarkThemedSharedTextScene(b *testing.B) {
	deck := themedSharedTextFixture(500)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := buildMixedSlideScene(deck, 0, 245, 56); err != nil {
			b.Fatal(err)
		}
	}
}
