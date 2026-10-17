package app

import (
	"net/url"
	"reflect"
	"testing"
)

func TestPromoteBackgroundAtomicHistoryAndScope(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := sceneFixture(t)
		d.Slides[0].Engagement = nil
		d.Slides[0].EngagementResult = nil
		var image Element
		for _, e := range d.Slides[0].Elements {
			if e.Kind == "image" {
				image = e
				break
			}
		}
		image.Query = "image-style=modern&image-fit=contain&modern-opacity=0&brightness=1.4&modern-mask=ellipse"
		d.Slides[0].Elements = []Element{image}
		s := newNativeEditorSession("Promotion.md", d)
		if master {
			s.deck.Masters.Base.Slide.Elements = []Element{image}
			s.masterMode = true
			s.currentMaster = 0
		}
		before := cloneDeck(s.deck)
		version := s.version
		action := nativeEditorAction{Action: "promote-image-background", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectID: image.ID}
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		target := s.deck.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
		}
		bg := target.BackgroundMedia
		if bg == nil || bg.Fit != "contain" || bg.Opacity != 0 || !bg.OpacitySet || !target.BackgroundMediaSet {
			t.Fatal("promotion lost frame or alpha", bg)
		}
		q, _ := url.ParseQuery(bg.Query)
		if q.Get("brightness") != "1.4" || q.Get("modern-mask") != "ellipse" || q.Has("modern-opacity") {
			t.Fatal("treatment lost or doubled", q)
		}
		source, _ := url.ParseQuery(target.Elements[0].Query)
		if source.Get("object-hidden") != "1" || source.Get("modern-opacity") != "0" {
			t.Fatal("source not retained intact", source)
		}
		after := cloneDeck(s.deck)
		if len(s.undo) != 1 {
			t.Fatal("promotion split history", len(s.undo))
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("undo", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("redo", err)
		}
		assertEditorFailureUnchanged(t, s, action)
	}
}

func TestPromoteBackgroundRejectsLockedAndMissing(t *testing.T) {
	d := sceneFixture(t)
	var id string
	for i, e := range d.Slides[0].Elements {
		if e.Kind == "image" {
			id = e.ID
			d.Slides[0].Elements[i].Query += "&object-locked=1"
			break
		}
	}
	s := newNativeEditorSession("Promotion.md", d)
	version := s.version
	action := nativeEditorAction{Action: "promote-image-background", Slide: 0, SceneRevision: &version, ObjectID: id}
	assertEditorFailureUnchanged(t, s, action)
	action.ObjectID = "missing"
	assertEditorFailureUnchanged(t, s, action)
}

func TestBackgroundRestoresForegroundPlacementAndCurrentTreatment(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			d := sceneFixture(t)
			var image Element
			for _, e := range d.Slides[0].Elements {
				if e.Kind == "image" {
					image = e
					break
				}
			}
			image.Query = "left=12&top=8&object-offset-x=0.5&object-offset-y=0.25&width=42.5&height=19.5&object-rotation=17&image-fit=contain&image-style=retro&brightness=1.4&modern-crop=0.1,0.1,0,0&modern-mask=ellipse&modern-opacity=0.6&link=https%3A%2F%2Fexample.com&custom-detail=retained"
			original, _ := url.ParseQuery(image.Query)
			d.Slides[0].Elements = []Element{image}
			d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
			if scope == "tab" {
				d.Slides[0].TabID = "reference"
				_, animation := animationCacheFixture(t)
				animation.LoopCount = 2
				d.Assets[image.AssetID] = animation
			}
			s := newNativeEditorSession("RoundTrip.md", d)
			master := scope == "base" || scope == "layout"
			if master {
				s.masterMode = true
				if scope == "layout" {
					s.currentMaster = 1
				}
				masterSlideAt(&s.deck, s.currentMaster).Elements = []Element{image}
			}
			target := func() *Slide {
				if master {
					return masterSlideAt(&s.deck, s.currentMaster)
				}
				return &s.deck.Slides[0]
			}
			apply := func(name string) {
				t.Helper()
				version, index := s.version, 0
				if master {
					index = s.currentMaster
				}
				if err := s.apply(nativeEditorAction{Action: name, Slide: index, SceneMaster: master, SceneRevision: &version, ObjectID: image.ID}); err != nil {
					t.Fatal(err)
				}
			}
			apply("promote-image-background")
			if target().BackgroundMedia.SourceObjectID != image.ID {
				t.Fatal("missing owner")
			}
			// Background edits are separate from the original placement. Exercise
			// portable metadata as well as the in-memory command route.
			target().BackgroundMedia.Query = "image-style=modern&brightness=0.8&modern-mask=rounded&tint=%2355aaff&shadow=soft&shadow-color=%23ffffff"
			target().BackgroundMedia.Fit = "stretch"
			target().BackgroundMedia.Opacity, target().BackgroundMedia.OpacitySet = 0, true
			encoded, err := serializeDeck("RoundTrip.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("RoundTrip.md", encoded)
			if err != nil {
				t.Fatal(err)
			}
			s.deck = loaded
			before := cloneDeck(s.deck)
			apply("restore-background-image")
			got := target()
			if got.BackgroundMedia != nil || !got.BackgroundMediaSet {
				t.Fatal("background not explicitly cleared")
			}
			q, _ := url.ParseQuery(got.Elements[0].Query)
			for _, key := range []string{"left", "top", "object-offset-x", "object-offset-y", "width", "height", "object-rotation", "link", "custom-detail"} {
				if q.Get(key) != original.Get(key) {
					t.Fatalf("placement/ownership %s changed: %v", key, q)
				}
			}
			if q.Has("object-hidden") || q.Has("modern-crop") || q.Get("modern-opacity") != "0" || q.Get("image-fit") != "stretch" || q.Get("image-style") != "modern" || q.Get("brightness") != "0.8" || q.Get("tint") != "#55aaff" || q.Get("modern-mask") != "rounded" || q.Get("shadow") != "soft" {
				t.Fatal("current treatment not restored", q)
			}
			if got.Elements[0].AssetID != image.AssetID {
				t.Fatal("source replaced")
			}
			after := cloneDeck(s.deck)
			if !reflect.DeepEqual(before.Assets, after.Assets) {
				t.Fatal("restoration changed source pixels or animation timing")
			}
			if len(s.undo) != 2 {
				t.Fatal("restore split history", len(s.undo))
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("restore undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("restore redo", err)
			}
		})
	}
}

func TestRestoreBackgroundRejectsMissingForeignLockedAndStaleOwners(t *testing.T) {
	d := sceneFixture(t)
	var image Element
	for _, e := range d.Slides[0].Elements {
		if e.Kind == "image" {
			image = e
			break
		}
	}
	d.Slides[0].Elements = []Element{image}
	s := newNativeEditorSession("Restore.md", d)
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "promote-image-background", Slide: 0, SceneRevision: &version, ObjectID: image.ID}); err != nil {
		t.Fatal(err)
	}
	version = s.version
	action := nativeEditorAction{Action: "restore-background-image", Slide: 0, SceneRevision: &version, ObjectID: image.ID}
	for _, corrupt := range []func(){
		func() { s.deck.Slides[0].BackgroundMedia.SourceObjectID = "another-object" },
		func() { s.deck.Slides[0].Elements = nil },
		func() { s.deck.Slides[0].Elements[0].AssetID = "another-asset" },
		func() { s.deck.Slides[0].Elements[0].Query += "&object-locked=1" },
		func() { s.deck.Slides[0].BackgroundMediaSet = false },
	} {
		before := cloneDeck(s.deck)
		corrupt()
		assertEditorFailureUnchanged(t, s, action)
		s.deck = before
	}
	stale := version - 1
	action.SceneRevision = &stale
	assertEditorFailureUnchanged(t, s, action)
}
