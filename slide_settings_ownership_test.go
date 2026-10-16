package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSlideSettingsOwnAppearanceAndProtectContent(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{ID: "unrelated", Elements: []Element{{ID: "unrelated-text", Kind: "text", Text: "Keep me"}}}, {ID: "selected", LayoutID: "blank", Elements: []Element{{ID: "body", Kind: "text", Text: "Protected content", Query: "render=truetype&modern-font=sans&modern-size=40&left=10&top=10"}}}}, Assets: map[string]DeckAsset{"background": clipboardTestAsset(t)}}
			if scope == "tab" {
				d.Slides[1].TabID = "guide"
			}
			s := newNativeEditorSession("Settings.md", d)
			s.current = 1
			var target *Slide
			if scope == "base" || scope == "layout" {
				s.masterMode = true
				s.currentMaster = 0
				if scope == "layout" {
					s.currentMaster = 3
				}
				target = masterSlideAt(&s.deck, s.currentMaster)
				target.Elements = []Element{{ID: "master-body", Kind: "text", Text: "Protected master", Query: "render=truetype&modern-font=sans&modern-size=40&left=10&top=10"}}
			} else {
				target = &s.deck.Slides[1]
			}
			target.Notes = "PRIVATE speaker notes"
			target.Extra = extensionFixture("futureOwner", `{"keep":true}`)
			target.Engagement = &EngagementDefinition{ID: "storm-1", Kind: "storm", Prompt: "Keep activity"}
			target.EngagementResult = activityResultFixture()
			target.PageNumber = pageNumberHide
			standardizeDeckText(&s.deck)
			before := cloneDeck(s.deck)
			if dir := os.Getenv("KEYNOPE_SLIDE_SETTINGS_EVIDENCE"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := saveDeck(filepath.Join(dir, scope+".md"), s.deck); err != nil {
					t.Fatal(err)
				}
			}
			original := cloneSlide(*target)
			incoming := cloneSlide(original)
			incoming.BG, incoming.BGSet = "#123456", true
			incoming.BackgroundMedia = &slideBackgroundMedia{AssetID: "background", Fit: "cover", Opacity: .7, OpacitySet: true, Query: "brightness=1.4"}
			incoming.BackgroundMediaSet = true
			incoming.ID, incoming.LayoutID, incoming.TabID = "different-owner", "missing-layout", "different-tab"
			incoming.Elements = []Element{{Kind: "text", Text: "Do not replace the contents"}}
			incoming.Notes, incoming.Extra, incoming.Engagement, incoming.EngagementResult = "", nil, nil, nil
			expected := cloneSlide(original)
			expected.BG, expected.BGSet = "48;2;18;52;86", true
			expected.BackgroundMedia = cloneSlideBackgroundMedia(incoming.BackgroundMedia)
			expected.BackgroundMediaSet = true
			if err := s.apply(nativeEditorAction{Action: "update-slide", SlideData: &incoming}); err != nil {
				t.Fatal(err)
			}
			if s.masterMode {
				target = masterSlideAt(&s.deck, s.currentMaster)
			} else {
				target = &s.deck.Slides[1]
			}
			if !reflect.DeepEqual(*target, expected) {
				t.Fatalf("settings altered non-appearance metadata\ngot: %#v\nwant: %#v", *target, expected)
			}
			accepted := cloneDeck(s.deck)
			incoming.BackgroundMedia.Query = "brightness=0.2"
			incoming.BackgroundMedia.Fit = "stretch"
			if !reflect.DeepEqual(s.deck, accepted) {
				t.Fatal("background settings alias caller input")
			}
			if !reflect.DeepEqual(s.deck.Slides[0], before.Slides[0]) {
				t.Fatal("settings changed an unrelated slide")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("settings Undo lost content", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
				t.Fatal("settings Redo lost ownership", err)
			}
		})
	}
}

func TestRebindLayoutRetainsDeterministicLocalOrder(t *testing.T) {
	d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "title-subtitle"}}}
	// Unmatched bindings become local objects. Their order must not come from
	// Go's map iteration, especially when multiple objects share a position.
	d.Slides[0].Elements = []Element{
		{ID: "bound-a", Kind: "text", Text: "A", MasterSlotID: "title-subtitle-title"},
		{ID: "local", Kind: "text", Text: "Local"},
		{ID: "bound-b", Kind: "text", Text: "B", MasterSlotID: "title-subtitle-subtitle"},
		{ID: "bound-c", Kind: "text", Text: "C", MasterSlotID: "future-slot"},
	}
	for i := 0; i < 100; i++ {
		copy := cloneDeck(d)
		if !copy.RebindSlideLayout(0, "blank") {
			t.Fatal("rebind failed")
		}
		var ids []string
		for _, e := range copy.Slides[0].Elements {
			ids = append(ids, e.ID)
			if e.MasterSlotID != "" {
				t.Fatal("unmatched binding was not detached")
			}
		}
		if !reflect.DeepEqual(ids, []string{"bound-a", "local", "bound-b", "bound-c"}) {
			t.Fatal("rebind reordered local/unmatched content", ids)
		}
	}
}
