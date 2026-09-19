package main

import (
	"encoding/json"
	"testing"
)

func TestModernSharpnessProjection(t *testing.T) {
	for _, query := range []string{"sharpness=2", "sharpness=0.2&tint=%23ff0000", "sharpness=2&brightness=1.5&tint=%23ff0000"} {
		deck := sceneFixture(t)
		for i, e := range deck.Slides[0].Elements {
			if e.Kind == "image" {
				deck.Slides[0].Elements[i].Query += "&" + query
			}
		}
		before, err := json.Marshal(deck)
		if err != nil {
			t.Fatal(err)
		}
		scene, err := buildSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, object := range scene.Objects {
			if object.Kind != "image" {
				continue
			}
			found = true
			opts := parseImageASCIIOptions(query)
			if object.Media.Sharpness != opts.sharpness {
				t.Fatal("lost sharpness")
			}
			want := 0
			if opts.brightness != 1 {
				want = 1
			}
			if object.Media.SharpnessAfter != want {
				t.Fatal("sharpness must follow adjustments and precede tint")
			}
		}
		after, err := json.Marshal(deck)
		if err != nil {
			t.Fatal(err)
		}
		if !found || string(after) != string(before) {
			t.Fatal("projection missing image or changed source deck")
		}
	}
}

func TestMalformedImageAdjustmentsStillSerialize(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf", "1e999", "not-a-number", "-2"} {
		deck := sceneFixture(t)
		for i, element := range deck.Slides[0].Elements {
			if element.Kind != "image" {
				continue
			}
			for _, key := range []string{"brightness", "contrast", "saturation", "sharpness", "modern-opacity"} {
				element.Query = setQueryValue(element.Query, key, value)
			}
			deck.Slides[0].Elements[i] = element
		}
		scene, err := buildSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(scene); err != nil {
			t.Fatalf("malformed value %q broke scene serialization: %v", value, err)
		}
	}
}
