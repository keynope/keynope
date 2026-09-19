package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
)

func TestConcreteAppearanceMaterialisesLegacyIntent(t *testing.T) {
	label, _ := json.Marshal(shapeLabelData{Text: "Label", Query: "element-style=modern&render=truetype"})
	deck := Deck{
		Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro", Extra: map[string]json.RawMessage{"theme": json.RawMessage(`"studio-v1"`)}},
		Masters:    MasterDeck{Base: MasterLayout{ID: "base", Slide: Slide{DefaultStyle: "modern"}}, Layouts: []MasterLayout{{ID: "layout", Slide: Slide{DefaultStyle: "retro"}}}},
		Slides: []Slide{{LayoutID: "layout", Elements: []Element{
			{ID: "text", Kind: "text", Text: "C64", Query: "element-style=retro&render=truetype"},
			{ID: "image", Kind: "image", Query: "element-style=modern"},
			{ID: "shape", Kind: "shape", Query: "element-style=retro&shape=square&shape-label=" + url.QueryEscape(base64.StdEncoding.EncodeToString(label))},
			{ID: "line", Kind: "connector", Query: "element-style=retro"},
		}}},
	}
	canonicalizeConcreteAppearance(&deck)
	if !deck.usesConcreteAppearance() || deck.Appearance.Version != 3 || deck.themeID() != "studio-v1" {
		t.Fatalf("appearance was not canonicalised without losing theme: %#v", deck.Appearance)
	}
	if deck.Slides[0].DefaultStyle != "" || deck.Masters.Base.Slide.DefaultStyle != "" || deck.Masters.Layouts[0].Slide.DefaultStyle != "" {
		t.Fatal("generic style defaults survived migration")
	}
	for _, element := range deck.Slides[0].Elements {
		q, _ := url.ParseQuery(element.Query)
		if q.Has("element-style") {
			t.Fatalf("generic style survived on %s", element.ID)
		}
		switch element.ID {
		case "text":
			if q.Get("modern-font") != "c64" {
				t.Fatal("Retro text was not expressed as a C64 font")
			}
		case "image":
			if q.Get("image-style") != "modern" {
				t.Fatal("image treatment was not materialised")
			}
		case "shape":
			resolved, ok := shapeLabel(element)
			if !ok {
				t.Fatal("shape label lost")
			}
			lq, _ := url.ParseQuery(resolved.Query)
			if lq.Get("modern-font") != "sans" || lq.Has("element-style") {
				t.Fatalf("shape label font was not materialised: %v", lq)
			}
		}
	}
}

func TestConcreteSceneUsesOnlyImageTreatment(t *testing.T) {
	deck := sceneFixture(t)
	canonicalizeConcreteAppearance(&deck)
	for index := range deck.Slides[0].Elements {
		element := &deck.Slides[0].Elements[index]
		q, _ := url.ParseQuery(element.Query)
		if element.Kind == "image" {
			q.Set("image-style", "retro")
		}
		element.Query = q.Encode()
	}
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		switch object.Kind {
		case "shape", "connector":
			if object.RetroLines != nil {
				t.Fatalf("%s still has a generic Retro treatment", object.Kind)
			}
		case "image":
			if object.RetroLines == nil || object.Media == nil {
				t.Fatal("Retro image must retain both conversion and source media")
			}
		}
	}
}

func TestConcreteFontSwitchKeepsAuthoredMetrics(t *testing.T) {
	deck := Deck{Appearance: &DeckAppearance{Version: 3}}
	element := Element{Kind: "text", Text: "Same size", Query: "modern-font=c64&modern-size=97&modern-width=100&render=truetype"}
	c64 := deck.modernSceneText(element)
	q, _ := url.ParseQuery(element.Query)
	q.Set("modern-font", "sans")
	element.Query = q.Encode()
	sans := deck.modernSceneText(element)
	if c64.Size != 97 || sans.Size != 97 {
		t.Fatalf("font switch resized text: c64=%v sans=%v", c64.Size, sans.Size)
	}
	if c64.WidthScale != .4167 || sans.WidthScale != 1 {
		t.Fatalf("font baselines are wrong: c64=%v sans=%v", c64.WidthScale, sans.WidthScale)
	}
}
