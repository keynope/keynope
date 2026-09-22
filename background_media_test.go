package main

import "testing"

func TestBackgroundMediaRoundTripsInheritsAndBuildsSceneLayer(t *testing.T) {
	assetID := "background-test"
	masters := defaultMasterDeck()
	masters.Base.Slide.BackgroundMedia = &slideBackgroundMedia{AssetID: assetID, Fit: "contain", Opacity: .6, Query: "brightness=1.4&image-style=retro&glyph=braille&outline=light&shadow=soft&shadow-color=%23ffffff&gradient-start=%23ff0000&gradient-end=%2300ff00&gradient-dir=horizontal"}
	masters.Base.Slide.BackgroundMediaSet = true
	deck := Deck{
		Masters: masters,
		Assets:  map[string]DeckAsset{assetID: {MIME: "image/png", Width: 1, Height: 1, Data: []byte("background-pixels")}},
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
	presentation, _, err := deckToPPTX(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(presentation.Slides) != 1 || len(presentation.Slides[0].Objects) < 2 || presentation.Slides[0].Objects[1].Kind != "image" || presentation.Slides[0].Objects[1].ID != "keynope-background-image-0" {
		t.Fatalf("background media was not exported as the first slide image: %#v", presentation.Slides)
	}
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
