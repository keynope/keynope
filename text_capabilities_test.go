package main

import (
	"reflect"
	"testing"
)

func TestTextCapabilityMatrix(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code", "text-image", "shape", "image", "connector", "page-number", "unknown"} {
		for _, style := range []string{"retro", "modern"} {
			for _, render := range []string{"", "render=truetype"} {
				e := Element{Kind: kind, Query: render}
				want := []string{}
				switch kind {
				case "text", "heading", "bullet", "code", "text-image":
					want = append(want, "size")
					if style == "modern" || render != "" {
						want = append(want, "width")
					}
					want = append(want, "emphasis", "alignment")
					if style == "modern" {
						want = append(want, "font", "paragraph")
					}
				}
				if got := textCapabilities(e, style); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s/%s/%s: %v, want %v", kind, style, render, got, want)
				}
				if supportsTextCapability(e, style, "unknown") {
					t.Fatal("unknown capability supported")
				}
			}
		}
	}
}

func TestTextCapabilitySceneAndSelection(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "m", Kind: "text", Text: "Modern", Query: "element-style=modern&render=truetype&top=3&group=g"},
		{ID: "r", Kind: "text", Text: "Retro", Query: "element-style=retro&render=truetype&top=20&group=g"},
		{ID: "s", Kind: "shape", Query: "shape=square&width=10&height=5&top=30&group=g"},
	}}}}
	scene, err := buildMixedSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 3 {
		t.Fatalf("expected every fixture object in scene, got %d", len(scene.Objects))
	}
	for _, o := range scene.Objects {
		var element Element
		for _, e := range deck.Slides[0].Elements {
			if e.ID == o.ID {
				element = e
			}
		}
		if !reflect.DeepEqual(o.TextCapabilities, textCapabilities(element, o.Style)) {
			t.Fatalf("scene and command disagree for %s", o.ID)
		}
	}
	before := cloneSlide(deck.Slides[0])
	selected, err := typographySelection(before, []string{"m"}, "")
	if err != nil || len(selected) != 3 {
		t.Fatalf("group: %v %v", selected, err)
	}
	selected, err = typographySelection(before, []string{"m"}, "g")
	if err != nil || len(selected) != 1 {
		t.Fatalf("entered group: %v %v", selected, err)
	}
	if !reflect.DeepEqual(before, deck.Slides[0]) {
		t.Fatal("preflight mutated slide")
	}
	selected, err = typographySelection(before, []string{"m", "m"}, "")
	if err != nil || len(selected) != 3 {
		t.Fatalf("duplicate selection should be idempotent: %v %v", selected, err)
	}
	for _, ids := range [][]string{{"missing"}, {}} {
		if _, err := typographySelection(before, ids, ""); err == nil {
			t.Fatalf("accepted invalid identities %v", ids)
		}
	}
	ambiguous := cloneSlide(before)
	ambiguous.Elements[1].ID = "m"
	if _, err := typographySelection(ambiguous, []string{"m"}, ""); err == nil {
		t.Fatal("accepted duplicate document identities")
	}
	before.Elements[2].Query += "&object-locked=1"
	if _, err := typographySelection(before, []string{"m"}, ""); err == nil {
		t.Fatal("ignored locked non-text group member")
	}
}
