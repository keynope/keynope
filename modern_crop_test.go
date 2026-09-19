package main

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

func TestModernCropSourceAndHistory(t *testing.T) {
	d := sceneFixture(t)
	d.Slides[0].Engagement = nil
	d.Slides[0].EngagementResult = nil
	s := newNativeEditorSession("Crop.md", d)
	before := cloneDeck(s.deck)
	revision := s.version
	crop := sceneCrop{Left: .2, Top: .1, Right: .3, Bottom: .1}
	a := nativeEditorAction{Action: "set-scene-crop", SceneRevision: &revision, ObjectID: "image", Crop: &crop}
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck.Assets, before.Assets) {
		t.Fatal("crop mutated embedded image source")
	}
	scene, err := buildSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range scene.Objects {
		if o.ID == "image" {
			found = o.Media.Crop != nil && *o.Media.Crop == crop
		}
	}
	if !found {
		t.Fatal("scene lost crop")
	}
	path := filepath.Join(t.TempDir(), "Crop.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Assets, s.deck.Assets) {
		t.Fatal("save changed source")
	}
	found = false
	scene, err = buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.Media != nil && o.Media.Crop != nil && *o.Media.Crop == crop {
			found = true
		}
	}
	if !found {
		t.Fatal("crop did not survive reopen")
	}
	if err := s.apply(a); err == nil {
		t.Fatal("stale crop accepted")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("crop undo was not exact")
	}
	for _, invalid := range []sceneCrop{{Left: math.NaN()}, {Top: -.1}, {Left: .8, Right: .3}, {Bottom: 1}} {
		revision = s.version
		a.Crop = &invalid
		if err := s.apply(a); err == nil {
			t.Fatal("invalid crop accepted")
		}
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("invalid crop mutated deck")
	}
}

func TestModernImageMaskOnlyChange(t *testing.T) {
	d := sceneFixture(t)
	d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Mask.md", d)
	before := cloneDeck(s.deck)
	mask, revision := "ellipse", s.version
	a := nativeEditorAction{Action: "set-scene-crop", SceneRevision: &revision, ObjectID: "image", Crop: &sceneCrop{}, ModernMask: &mask}
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
	if len(s.undo) != 1 || !reflect.DeepEqual(s.deck.Assets, before.Assets) {
		t.Fatal("mask must be undoable without changing source")
	}
	path := filepath.Join(t.TempDir(), "Mask.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range scene.Objects {
		if o.Media != nil && o.Media.Mask == "ellipse" {
			found = true
		}
	}
	if !found {
		t.Fatal("mask lost after save/reopen")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("mask undo must exactly restore document")
	}
	mask = "url(https://example.com)"
	revision = s.version
	if err := s.apply(a); err == nil {
		t.Fatal("invalid mask accepted")
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("invalid mask modified deck")
	}
}

func TestRetroCropRetainsSharedMediaAndRoundTrip(t *testing.T) {
	d := sceneFixture(t)
	d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("RetroCrop.md", d)
	before := cloneDeck(s.deck)
	crop, mask, revision := sceneCrop{Left: .2, Right: .2}, "ellipse", s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-crop", SceneRevision: &revision, ObjectID: "image", Crop: &crop, ModernMask: &mask}); err != nil {
		t.Fatal(err)
	}
	check := func(deck Deck, retro bool) {
		t.Helper()
		scene, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range scene.Objects {
			if object.ID == "image" {
				if object.Media == nil || object.Media.Source == "" || object.Media.Crop == nil || *object.Media.Crop != crop || object.Media.Mask != mask {
					t.Fatal("shared source/crop/mask missing")
				}
				if (object.RetroLines != nil) != retro {
					t.Fatal("wrong treatment")
				}
				return
			}
		}
		t.Fatal("image absent")
	}
	check(s.deck, true)
	modern := cloneDeck(s.deck)
	modern.Slides[0].Elements[len(modern.Slides[0].Elements)-1].Query = setQueryValue(modern.Slides[0].Elements[len(modern.Slides[0].Elements)-1].Query, "image-style", "modern")
	check(modern, false)
	path := filepath.Join(t.TempDir(), "Crop.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	check(loaded, true)
	if !reflect.DeepEqual(before.Assets, s.deck.Assets) {
		t.Fatal("source changed")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("undo changed original")
	}
}
