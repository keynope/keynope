package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func masterProjectionOwnershipFixture() Deck {
	d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{ID: "child", LayoutID: "title", PageNumber: pageNumberHide, Extra: extensionFixture("futureSlide", `{"keep":true}`), Elements: []Element{{ID: "child-title", Kind: "heading", Level: 1, Text: "Child title", MasterSlotID: "title-title", Extra: extensionFixture("futureChild", `{"target":"payload"}`)}}}}}
	d.Masters.Layouts[1].Slide.Elements[0].Extra = extensionFixture("futureTemplate", `{"keep":true}`)
	d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{ID: "decoration", Kind: "shape", Query: "shape=square&left=1&top=1&width=2&height=2", Extra: extensionFixture("futureDecoration", `{"keep":true}`)})
	return d
}

func TestMasterProjectionRetainsOwnedMetadata(t *testing.T) {
	d := masterProjectionOwnershipFixture()
	d.Slides[0].PageNumber = pageNumberShow
	d.Masters.Base.Slide.Elements[0].Extra = extensionFixture("futureNumber", `{"keep":true}`)
	before := cloneDeck(d)
	resolved := d.ResolveSlide(0, false)
	if !reflect.DeepEqual(resolved.Extra, d.Slides[0].Extra) {
		t.Fatal("resolution lost authored slide metadata")
	}
	(*resolved.Extra)["futureSlide"][0] = '['
	for i := range resolved.Elements {
		e := &resolved.Elements[i]
		if e.ID == "child-title" && !reflect.DeepEqual(e.Extra, d.Slides[0].Elements[0].Extra) {
			t.Fatal("resolution replaced child metadata with template metadata")
		}
		if e.Extra != nil {
			for key := range *e.Extra {
				(*e.Extra)[key][0] = '['
			}
		}
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("resolved element metadata aliases authored masters")
	}
}

func TestStoreResolvedPlaceholderPreservesMetadata(t *testing.T) {
	d := masterProjectionOwnershipFixture()
	original := cloneDeck(d)
	resolved := d.ResolveSlide(0, false)
	for i := range resolved.Elements {
		if resolved.Elements[i].ID == "child-title" {
			resolved.Elements[i].Text = "Edited title"
		}
	}
	d.StoreResolvedSlide(0, resolved)
	if !reflect.DeepEqual(d.Slides[0].Extra, original.Slides[0].Extra) || !reflect.DeepEqual(d.Slides[0].Elements[0].Extra, original.Slides[0].Elements[0].Extra) {
		t.Fatal("resolved text commit erased opaque metadata")
	}
	data, err := serializeDeck("Metadata.md", d)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Metadata.md", data)
	if err != nil || !reflect.DeepEqual(reopened.Slides[0].Elements[0].Extra, original.Slides[0].Elements[0].Extra) {
		t.Fatal("metadata lost on save/reopen", err)
	}
}

func TestDetachRetainsSlideAndInstanceMetadata(t *testing.T) {
	d := masterProjectionOwnershipFixture()
	original := cloneDeck(d)
	if !d.DetachSlide(0) {
		t.Fatal("detach failed")
	}
	if !reflect.DeepEqual(d.Slides[0].Extra, original.Slides[0].Extra) {
		t.Fatal("detach discarded slide extensions")
	}
	for _, e := range d.Slides[0].Elements {
		if e.ID == "child-title" && !reflect.DeepEqual(e.Extra, original.Slides[0].Elements[0].Extra) {
			t.Fatal("detach discarded child extensions")
		}
	}
}

func TestPlaceholderMetadataSurvivesUnchangedProjection(t *testing.T) {
	d := masterProjectionOwnershipFixture()
	d.Slides[0].Elements[0].Placeholder = true
	original := cloneDeck(d)
	d.StoreResolvedSlide(0, d.ResolveSlide(0, true))
	if len(d.Slides[0].Elements) != 1 || !reflect.DeepEqual(d.Slides[0].Elements[0].Extra, original.Slides[0].Elements[0].Extra) {
		t.Fatal("metadata-only placeholder override was discarded")
	}
}

func TestMasterBoundTextCommandPreservesOpaqueMetadata(t *testing.T) {
	s := newNativeEditorSession("Bound.md", masterProjectionOwnershipFixture())
	before := cloneDeck(s.deck)
	if dir := os.Getenv("KEYNOPE_MASTER_METADATA_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(dir, "bound.md"), before); err != nil {
			t.Fatal(err)
		}
	}
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", Slide: 0, SceneRevision: &version, ObjectID: "child-title", TextRuns: []sceneRun{{Text: "Edited title"}}}); err != nil {
		t.Fatal(err)
	}
	accepted := cloneDeck(s.deck)
	if !reflect.DeepEqual(accepted.Slides[0].Elements[0].Extra, before.Slides[0].Elements[0].Extra) || !reflect.DeepEqual(accepted.Slides[0].Extra, before.Slides[0].Extra) {
		t.Fatal("text command dropped opaque instance metadata")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("Undo did not restore bound metadata", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
		t.Fatal("Redo did not restore bound metadata", err)
	}
}
