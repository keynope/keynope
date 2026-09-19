package main

import (
	"net/http/httptest"
	"testing"
)

func TestSceneSnapshotOmitsArchivesWithoutChangingDocument(t *testing.T) {
	slide := archivedLayoutFixture()
	deck := Deck{Slides: []Slide{slide}, Masters: MasterDeck{Layouts: []MasterLayout{{ID: "custom", Slide: slide}}}}
	deck.Masters.Base.Slide = slide
	snapshot := cloneDeckForScene(deck)
	if snapshot.Slides[0].EngagementResult != nil || snapshot.Masters.Base.Slide.EngagementResult != nil || snapshot.Masters.Layouts[0].Slide.EngagementResult != nil {
		t.Fatal("scene snapshot retained private archives")
	}
	if deck.Slides[0].EngagementResult == nil || deck.Masters.Base.Slide.EngagementResult == nil || deck.Masters.Layouts[0].Slide.EngagementResult == nil {
		t.Fatal("scene snapshot removed authored results")
	}
	snapshot.Slides[0].Elements[0].Text = "Changed"
	if deck.Slides[0].Elements[0].Text != "Hello" {
		t.Fatal("scene snapshot shares mutable elements")
	}
	// Private archives must not affect even the editor's scene response.
	session := nativeEditorSession{deck: Deck{Slides: []Slide{slide}}, version: 1}
	request := httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil)
	with := httptest.NewRecorder()
	session.handleScene(with, request)
	session.deck.Slides[0].EngagementResult = nil
	without := httptest.NewRecorder()
	session.handleScene(without, request)
	if with.Code != 200 || without.Code != 200 || with.Body.String() != without.Body.String() {
		t.Fatal("archive presence changed scene response")
	}
}

func TestRenderSnapshotOwnership(t *testing.T) {
	payload := []byte{1, 2, 3}
	deck := Deck{Slides: []Slide{{Elements: []Element{{Text: "Original"}}}}, Assets: map[string]DeckAsset{"image": {Data: payload, Frames: []DeckAssetFrame{{Data: payload, DelayMS: 70}}, Source: &DeckAssetSource{Data: payload, Width: 100, Frames: []DeckAssetFrame{{Data: payload, DelayMS: 90}}}}}}
	snapshot := cloneDeckForRender(deck)
	a := snapshot.Assets["image"]
	if &a.Data[0] != &payload[0] || &a.Source.Data[0] != &payload[0] || &a.Frames[0].Data[0] != &payload[0] || &a.Source.Frames[0].Data[0] != &payload[0] {
		t.Fatal("render snapshot copied immutable media bytes")
	}
	snapshot.Slides[0].Elements[0].Text = "Preview"
	a.Source.Width = 200
	a.Frames[0].DelayMS = 10
	a.Source.Frames[0].DelayMS = 20
	delete(snapshot.Assets, "image")
	original := deck.Assets["image"]
	if deck.Slides[0].Elements[0].Text != "Original" || original.Source.Width != 100 || original.Frames[0].DelayMS != 70 || original.Source.Frames[0].DelayMS != 90 {
		t.Fatal("preview changed authored metadata")
	}
	deck.Assets["image"] = DeckAsset{Data: []byte{9}}
	if a.Data[0] != 1 {
		t.Fatal("asset replacement changed in-flight preview")
	}
	owned := cloneDeck(Deck{Assets: map[string]DeckAsset{"image": original}})
	owned.Assets["image"].Data[0] = 8
	if payload[0] != 1 {
		t.Fatal("history snapshot lost deep ownership")
	}
}

func BenchmarkRenderSnapshot(b *testing.B) {
	deck := Deck{Assets: map[string]DeckAsset{"media": {Data: make([]byte, 8<<20), Source: &DeckAssetSource{Data: make([]byte, 16<<20)}}}}
	b.Run("deep", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cloneDeck(deck)
		}
	})
	b.Run("render", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cloneDeckForRender(deck)
		}
	})
}

func BenchmarkSceneArchiveSnapshot(b *testing.B) {
	deck := Deck{Slides: []Slide{archivedLayoutFixture()}}
	for _, scene := range []bool{false, true} {
		name := "render"
		if scene {
			name = "scene"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if scene {
					_ = cloneDeckForScene(deck)
				} else {
					_ = cloneDeckForRender(deck)
				}
			}
		})
	}
}
