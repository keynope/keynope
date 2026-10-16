package main

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func TestImportedBackgroundBecomesEditableLocalImage(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout", "inherited-slide", "inherited-layout"} {
		for _, style := range []string{"modern", "retro"} {
			t.Run(scope+"/"+style, func(t *testing.T) {
				asset := clipboardTestAsset(t)
				if scope == "tab" {
					_, asset = animationCacheFixture(t)
					asset.LoopCount = 3
				}
				media := &slideBackgroundMedia{AssetID: "background", Fit: "contain", Opacity: 0, OpacitySet: true, Query: "image-style=" + style + "&glyph=blocks&brightness=1.2&modern-crop=0.1,0.1,0,0&modern-mask=ellipse&shadow=soft&shadow-color=%23ffffff&gradient-start=%23ff0055&gradient-end=%23ffffaa&left=30&top=20&width=4&height=2&object-hidden=1&object-rotation=40&group=foreign"}
				d := Deck{Assets: map[string]DeckAsset{"background": asset}, Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
				foreground := []Element{{ID: "existing", Kind: "text", Text: "Foreground", Query: "render=truetype&top=2&z-index=0&object-locked=1"}}
				s := newNativeEditorSession("ImportedBackground.md", d)
				master := scope == "base" || scope == "layout" || scope == "inherited-layout"
				if master {
					s.masterMode = true
					if scope != "base" {
						s.currentMaster = 1
					}
				}
				target := func() *Slide {
					if master {
						return masterSlideAt(&s.deck, s.currentMaster)
					}
					return &s.deck.Slides[0]
				}
				target().Elements = foreground
				if scope == "tab" {
					target().TabID = "reference"
				}
				if scope == "inherited-slide" || scope == "inherited-layout" {
					s.deck.Masters.Base.Slide.BackgroundMedia = media
					s.deck.Masters.Base.Slide.BackgroundMediaSet = true
				} else {
					target().BackgroundMedia, target().BackgroundMediaSet = media, true
				}
				before := cloneDeck(s.deck)
				s.selected, s.selection = 0, map[int]bool{0: true}
				version, index := s.version, 0
				if master {
					index = s.currentMaster
				}
				action := nativeEditorAction{Action: "restore-background-image", Slide: index, SceneMaster: master, SceneRevision: &version}
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				got := target()
				if s.selected < 0 || got.Elements[s.selected].ID != "existing" || !s.selection[s.selected] {
					t.Fatal("conversion changed selected object identity")
				}
				if got.BackgroundMedia != nil || !got.BackgroundMediaSet || len(got.Elements) != 2 {
					t.Fatal("conversion did not create an independent foreground", got)
				}
				var image Element
				for _, element := range got.Elements {
					if element.Kind == "image" {
						image = element
					} else if !reflect.DeepEqual(element, foreground[0]) {
						t.Fatal("rewrote locked foreground", element)
					}
				}
				if !validObjectID(image.ID) || image.ID == "existing" || image.AssetID != "background" {
					t.Fatal("missing fresh image ownership", image)
				}
				q, _ := url.ParseQuery(image.Query)
				cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
				for key, want := range map[string]string{"left": "0", "top": "0", "width": strconv.Itoa(cols), "height": strconv.Itoa(rows), "image-fit": "contain", "modern-opacity": "0", "modern-mask": "ellipse", "modern-crop": "0.1,0.1,0,0", "brightness": "1.2", "image-style": style, "glyph": "blocks", "gradient-start": "#ff0055", "shadow": "soft"} {
					if q.Get(key) != want {
						t.Fatalf("%s: got %q want %q", key, q.Get(key), want)
					}
				}
				for _, key := range []string{"object-hidden", "object-rotation", "group"} {
					if q.Has(key) {
						t.Fatal("imported background leaked foreground placement", q)
					}
				}
				if got.Elements[slidePaintOrder(*got)[0]].ID != image.ID {
					t.Fatal("background covered existing foreground")
				}
				if !reflect.DeepEqual(before.Assets, s.deck.Assets) {
					t.Fatal("changed source bytes/frame timing")
				}
				if scope == "inherited-slide" || scope == "inherited-layout" {
					if !reflect.DeepEqual(before.Masters.Base, s.deck.Masters.Base) {
						t.Fatal("changed inherited source owner")
					}
				}
				after := cloneDeck(s.deck)
				if len(s.undo) != 1 {
					t.Fatal("conversion split history")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("undo", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
					t.Fatal("redo", err)
				}
				encoded, err := serializeDeck("Converted.md", s.deck)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData("Converted.md", encoded)
				if err != nil {
					t.Fatal(err)
				}
				loadedTarget := &loaded.Slides[0]
				if master {
					loadedTarget = masterSlideAt(&loaded, s.currentMaster)
				}
				if loadedTarget.BackgroundMedia != nil || !loadedTarget.BackgroundMediaSet {
					t.Fatal("save lost explicit background clearing")
				}
				found := false
				for _, element := range loadedTarget.Elements {
					if element.ID == image.ID {
						found = true
						if element.Query != image.Query || element.AssetID != image.AssetID {
							t.Fatal("save lost editable source/treatment")
						}
					}
				}
				if !found {
					t.Fatal("save lost converted image")
				}
			})
		}
	}
}

func TestImportedBackgroundConversionRejectsUnavailableAndOwnedSources(t *testing.T) {
	for _, media := range []*slideBackgroundMedia{nil, {AssetID: "missing"}, {AssetID: "background", SourceObjectID: "original"}, {AssetID: "background", Query: "%invalid"}} {
		d := Deck{Assets: map[string]DeckAsset{"background": clipboardTestAsset(t)}, Slides: []Slide{{BackgroundMedia: media, BackgroundMediaSet: true}}}
		s := newNativeEditorSession("Reject.md", d)
		version := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "restore-background-image", Slide: 0, SceneRevision: &version})
	}
}
