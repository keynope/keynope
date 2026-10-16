package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBackgroundTreatmentOverridesRemainLocal(t *testing.T) {
	inherited := Slide{BackgroundMedia: &slideBackgroundMedia{AssetID: "bg", Fit: "contain", Opacity: .5, Query: "brightness=1.4"}, BackgroundMediaSet: true}
	for _, query := range []string{"brightness=1", "modern-mask=ellipse", "tint=%23ff0000"} {
		effective := cloneSlide(inherited)
		effective.BackgroundMedia.Query = query
		target := Slide{}
		setStyleOverrides(&target, inherited, effective)
		if !target.BackgroundMediaSet || target.BackgroundMedia == nil || target.BackgroundMedia.Query != query {
			t.Fatal("treatment override discarded", query, target.BackgroundMedia)
		}
		if inherited.BackgroundMedia.Query != "brightness=1.4" {
			t.Fatal("master changed")
		}
	}
}

func TestExplicitTransparentBackgroundAndLegacyDefault(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		d := Deck{Assets: map[string]DeckAsset{"bg": clipboardTestAsset(t)}, Slides: []Slide{{BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: "bg", OpacitySet: explicit}}}}
		encoded, err := serializeDeck("Background.md", d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Background.md", encoded)
		if err != nil {
			t.Fatal(err)
		}
		scene, err := buildDocumentSlideScene(loaded, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		want := 1.0
		if explicit {
			want = 0
		}
		if scene.BackgroundMedia == nil || scene.BackgroundMedia.Opacity != want || scene.BackgroundMedia.Paint == nil || scene.BackgroundMedia.Paint.Opacity == nil || *scene.BackgroundMedia.Paint.Opacity != want {
			t.Fatal("wrong background alpha", explicit, scene.BackgroundMedia)
		}
		wire, err := json.Marshal(scene.BackgroundMedia)
		if err != nil {
			t.Fatal(err)
		}
		if explicit && !bytes.Contains(wire, []byte(`"opacity":0`)) {
			t.Fatal("zero opacity omitted from wire", string(wire))
		}
		presentation, _, err := deckToPPTX(loaded)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range presentation.Slides[0].Objects {
			if o.Kind == "image" {
				found = true
				if o.ImageOpacity == nil || *o.ImageOpacity != want {
					t.Fatal("PPTX opacity mismatch")
				}
			}
		}
		if !found {
			t.Fatal("missing background image")
		}
	}
}

func TestBackgroundMediaRoundTripsInheritsAndBuildsSceneLayer(t *testing.T) {
	assetID := "background-test"
	masters := defaultMasterDeck()
	masters.Base.Slide.BackgroundMedia = &slideBackgroundMedia{AssetID: assetID, Fit: "contain", Opacity: .6, Query: "brightness=1.4&image-style=retro&glyph=braille&outline=light&shadow=soft&shadow-color=%23ffffff&gradient-start=%23ff0000&gradient-end=%2300ff00&gradient-dir=horizontal"}
	masters.Base.Slide.BackgroundMediaSet = true
	deck := Deck{
		Masters: masters,
		Assets:  map[string]DeckAsset{assetID: clipboardTestAsset(t)},
		Slides:  []Slide{{LayoutID: "blank", Elements: []Element{{ID: "title", Kind: "text", Text: "Foreground"}}}},
	}
	encoded, err := serializeDeck("background.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("background.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	resolved := loaded.ResolveSlide(0, false)
	if resolved.BackgroundMedia == nil || resolved.BackgroundMedia.AssetID != assetID || resolved.BackgroundMedia.Fit != "contain" || resolved.BackgroundMedia.Query != "brightness=1.4&image-style=retro&glyph=braille&outline=light&shadow=soft&shadow-color=%23ffffff&gradient-start=%23ff0000&gradient-end=%2300ff00&gradient-dir=horizontal" {
		t.Fatalf("background media inheritance lost: %#v", resolved.BackgroundMedia)
	}
	scene, err := buildStyledSlideScene(loaded, resolved, 0, 245, 56, true)
	if err != nil {
		t.Fatal(err)
	}
	if scene.BackgroundMedia == nil || scene.BackgroundMedia.Fit != "contain" || scene.BackgroundMedia.Opacity != .6 || scene.BackgroundMedia.Source == "" || len(scene.BackgroundMedia.ColourMatrices) == 0 || scene.BackgroundMedia.RetroLines == nil {
		t.Fatalf("scene background media missing: %#v", scene.BackgroundMedia)
	}
	if scene.BackgroundMedia.Paint == nil || scene.BackgroundMedia.Paint.Stroke != "#ffffff" || scene.BackgroundMedia.Paint.GradientStart != "#ff0000" || scene.BackgroundMedia.Paint.ShadowBlur == 0 || scene.BackgroundMedia.Paint.Opacity == nil || *scene.BackgroundMedia.Paint.Opacity != .6 {
		t.Fatalf("background visual treatment missing: %#v", scene.BackgroundMedia.Paint)
	}
	presentation, report, err := deckToPPTX(loaded)
	if err != nil {
		t.Fatal(err)
	}
	// Braille still requires font-aware capture. Export must diagnose this,
	// rather than silently replacing the visible artwork with its source PNG.
	for _, object := range presentation.Slides[0].Objects {
		if object.Kind == "image" {
			t.Fatal("unsupported braille background substituted original image")
		}
	}
	noted := false
	for _, warning := range report.Warnings {
		if warning.Code == "retro-image-capture-unavailable" && warning.Object == "keynope-background-image-0" {
			noted = true
		}
	}
	if !noted {
		t.Fatal("missing unsupported braille capture note", report)
	}
}

func TestBackgroundImagePPTXFramingAndMask(t *testing.T) {
	deck := Deck{Assets: map[string]DeckAsset{"bg": {MIME: "image/png", Width: 400, Height: 200, Data: []byte("source")}}, Slides: []Slide{{BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: "bg", Fit: "contain", Opacity: .7, Query: "image-style=modern&modern-mask=ellipse"}}}}
	presentation, _, err := deckToPPTX(deck)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range presentation.Slides[0].Objects {
		if object.Kind != "image" {
			continue
		}
		if object.Shape != "ellipse" || object.FillTop <= 0 || object.FillBottom != object.FillTop || object.ImageOpacity == nil || *object.ImageOpacity != .7 {
			t.Fatalf("background frame lost: %+v", object)
		}
		return
	}
	t.Fatal("missing background picture")
}

func TestBackgroundMediaCanExplicitlyClearInheritedImage(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.BackgroundMedia = &slideBackgroundMedia{AssetID: "base"}
	masters.Base.Slide.BackgroundMediaSet = true
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank", BackgroundMediaSet: true}}}
	resolved := deck.ResolveSlide(0, false)
	if resolved.BackgroundMedia != nil {
		t.Fatalf("explicit clear inherited a background: %#v", resolved.BackgroundMedia)
	}
}
