package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandaloneSnapshotRemovesGeneratedJoinArtwork(t *testing.T) {
	for _, style := range []string{"legacy", "retro", "modern"} {
		t.Run(style, func(t *testing.T) {
			deck := sceneFixture(t)
			if style != "legacy" {
				deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
			}
			deck.Slides[0].Elements = []Element{
				{ID: "title", Kind: "heading", Text: "Workshop introduction", PlaceholderRole: activityTitleRole},
				{ID: "qr", Kind: "code", Text: "PRIVATE_QR_PIXELS", PlaceholderRole: activityQRCodeRole},
				{Kind: "text", Text: "PRIVATE_JOIN_ADDRESS", PlaceholderRole: activityURLRole, Query: "link=https%3A%2F%2Fkeynope.sh%2Fjoin%2FSecret01"},
				{ID: "ordinary", Kind: "text", Text: "Public website", Query: "link=https%3A%2F%2Fkeynope.sh%2F"},
			}
			resolved := deck.ResolvedSlides()[0]
			before, _ := json.Marshal(resolved)
			safe := standaloneExportSlide(resolved, 0)
			if len(safe.Elements) != 2 || safe.Elements[0].ID != "title" || safe.Elements[1].ID != "ordinary" {
				t.Fatal("snapshot must retain title and ordinary authored content")
			}
			if safe.ModernScene != nil {
				for _, object := range safe.ModernScene.Objects {
					if object.ID == "qr" || object.ID == "legacy-0-2" {
						t.Fatal("generated access artwork remains in scene")
					}
				}
			}
			data, _ := json.Marshal(safe)
			html, err := exportHTMLDocument("Snapshot.md", []Slide{resolved}, 245, 56, preservedExportHead{}, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"PRIVATE_QR_PIXELS", "PRIVATE_JOIN_ADDRESS", "Secret01", "SECRET_NOTES", "SECRET_IDENTITY"} {
				if strings.Contains(string(data), secret) || strings.Contains(html, secret) {
					t.Fatalf("snapshot leaked %s", secret)
				}
			}
			after, _ := json.Marshal(resolved)
			if string(before) != string(after) {
				t.Fatal("snapshot mutated resolved source")
			}
		})
	}
}
