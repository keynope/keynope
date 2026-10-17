package app

import (
	"net/url"
	"reflect"
	"testing"
)

func promotedBackgroundSession(t *testing.T, scope string) *nativeEditorSession {
	t.Helper()
	d := sceneFixture(t)
	var image Element
	for _, e := range d.Slides[0].Elements {
		if e.Kind == "image" {
			image = e
			break
		}
	}
	image.Query = "left=12&top=8&width=42&height=19&image-style=modern&brightness=1.4&modern-mask=ellipse"
	d.Slides = []Slide{{ID: "promotion-page", LayoutID: "blank", Elements: []Element{image}}}
	if scope == "tab" {
		d.Slides[0].TabID = "reference"
	}
	s := newNativeEditorSession("References.md", d)
	if scope == "base" || scope == "layout" {
		s.masterMode = true
		if scope == "layout" {
			s.currentMaster = 1
		}
		masterSlideAt(&s.deck, s.currentMaster).Elements = []Element{image}
		masterSlideAt(&s.deck, s.currentMaster).PageNumber = pageNumberHide
	}
	applyBackgroundReferenceAction(t, s, "promote-image-background", image.ID)
	return s
}

func currentBackgroundReferenceSlide(s *nativeEditorSession) *Slide {
	if s.masterMode {
		return masterSlideAt(&s.deck, s.currentMaster)
	}
	return &s.deck.Slides[s.current]
}

func applyBackgroundReferenceAction(t *testing.T, s *nativeEditorSession, name, object string) {
	t.Helper()
	version, index := s.version, s.current
	if s.masterMode {
		index = s.currentMaster
	}
	if err := s.apply(nativeEditorAction{Action: name, Slide: index, SceneMaster: s.masterMode, SceneRevision: &version, ObjectID: object}); err != nil {
		t.Fatal(name, err)
	}
}

func TestClonedBackgroundBindsCopiedForegroundOwner(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			s := promotedBackgroundSession(t, scope)
			original := cloneSlide(*currentBackgroundReferenceSlide(s))
			originalIndex := s.current
			if s.masterMode {
				originalIndex = s.currentMaster
			}
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
				t.Fatal(err)
			}
			copied := currentBackgroundReferenceSlide(s)
			owner := copied.BackgroundMedia.SourceObjectID
			if owner == "" || owner == original.BackgroundMedia.SourceObjectID {
				t.Fatal("clone retained source page owner")
			}
			count := 0
			for _, e := range copied.Elements {
				if e.ID == owner && e.Kind == "image" && e.AssetID == copied.BackgroundMedia.AssetID {
					q, _ := url.ParseQuery(e.Query)
					if q.Get("object-hidden") != "1" {
						t.Fatal("copied source is visible")
					}
					count++
				}
			}
			if count != 1 {
				t.Fatal("copied background has no unique local image")
			}
			// Real Markdown reload must retain the new relationship before restore.
			encoded, err := serializeDeck("References.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			s.deck, err = parseDeckData("References.md", encoded)
			if err != nil {
				t.Fatal(err)
			}
			cloned := cloneDeck(s.deck)
			applyBackgroundReferenceAction(t, s, "restore-background-image", owner)
			if currentBackgroundReferenceSlide(s).BackgroundMedia != nil {
				t.Fatal("copied image not restored")
			}
			var source Slide
			if s.masterMode {
				source = *masterSlideAt(&s.deck, originalIndex)
			} else {
				source = s.deck.Slides[originalIndex]
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("restoring copy changed source slide")
			}
			restored := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(cloned, s.deck) {
				t.Fatal("restore undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("clone undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(restored, s.deck) {
				t.Fatal("restore redo", err)
			}
		})
	}
}

func TestDeletedBackgroundOwnerRetainsConvertibleMedia(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			s := promotedBackgroundSession(t, scope)
			before := cloneDeck(s.deck)
			background := cloneSlideBackgroundMedia(currentBackgroundReferenceSlide(s).BackgroundMedia)
			version := s.version
			if err := s.apply(nativeEditorAction{Action: "delete-objects", SceneRevision: &version, ObjectIDs: []string{background.SourceObjectID}}); err != nil {
				t.Fatal(err)
			}
			background.SourceObjectID = ""
			if !reflect.DeepEqual(background, currentBackgroundReferenceSlide(s).BackgroundMedia) {
				t.Fatal("deletion lost media or retained dangling placement owner")
			}
			if !reflect.DeepEqual(before.Assets, s.deck.Assets) {
				t.Fatal("deleted background pixels")
			}
			after := cloneDeck(s.deck)
			remaining := len(currentBackgroundReferenceSlide(s).Elements)
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("delete undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("delete redo", err)
			}
			applyBackgroundReferenceAction(t, s, "restore-background-image", "")
			if currentBackgroundReferenceSlide(s).BackgroundMedia != nil || len(currentBackgroundReferenceSlide(s).Elements) != remaining+1 {
				t.Fatal("source-only background cannot become foreground")
			}
		})
	}
}

func TestBackgroundOwnerReconciliationIsScopedAndCopyOwned(t *testing.T) {
	media := &slideBackgroundMedia{AssetID: "original-asset", SourceObjectID: "source"}
	before := Slide{BackgroundMedia: media, BackgroundMediaSet: true, Elements: []Element{{ID: "source", Kind: "image", AssetID: "original-asset"}}}
	for _, replacement := range []Element{{ID: "source", Kind: "image", AssetID: "replacement-asset"}, {ID: "source", Kind: "shape"}} {
		after := before // Deliberately share background metadata; helper must own its delta.
		after.Elements = []Element{replacement}
		reconcileBackgroundSourceOwner(before, &after)
		if after.BackgroundMedia.SourceObjectID != "" || media.SourceObjectID != "source" {
			t.Fatal("replacement left dangling owner or changed original snapshot")
		}
	}
	foreign := before
	foreign.BackgroundMedia = &slideBackgroundMedia{AssetID: "different-asset", SourceObjectID: "source"}
	reconcileBackgroundSourceOwner(before, &foreign)
	if foreign.BackgroundMedia.SourceObjectID != "source" {
		t.Fatal("silently repaired a newly introduced unrelated reference")
	}
}

func TestReplacedBackgroundOwnerDoesNotReplaceBackgroundPixels(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			s := promotedBackgroundSession(t, scope)
			slide := currentBackgroundReferenceSlide(s)
			media := cloneSlideBackgroundMedia(slide.BackgroundMedia)
			element := slide.Elements[0]
			s.deck.Assets["replacement-asset"] = s.deck.Assets[element.AssetID]
			before := cloneDeck(s.deck)
			element.AssetID = "replacement-asset"
			element.Path = embeddedAssetPath(element.AssetID, s.deck.Assets[element.AssetID])
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &element}); err != nil {
				t.Fatal(err)
			}
			media.SourceObjectID = ""
			if !reflect.DeepEqual(media, currentBackgroundReferenceSlide(s).BackgroundMedia) {
				t.Fatal("replacement changed background pixels/treatment or left a stale owner")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("replacement undo", err)
			}
		})
	}
}
