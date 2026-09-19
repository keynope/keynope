package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModernPresentationProjectionAndPrivacy(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements[0].Query += "&link=https%3A%2F%2Fkeynope.sh%2F"
	deck.Appearance = &DeckAppearance{Version: 1, Mode: "modern"}
	resolved := deck.ResolvedSlides()
	if resolved[0].ModernScene == nil || deck.Slides[0].ModernScene != nil {
		t.Fatal("Modern scene must be derived, not persisted into authored slides")
	}
	pages := exportSlidePages(resolved[0], 0, 1, 245, 56)
	if len(pages) != 1 || pages[0].Scene == nil || len(pages[0].Lines) != 0 || len(pages[0].ContentFrames) != 0 {
		t.Fatal("Modern output must not include conflicting Retro raster frames")
	}
	participant, err := participantRenderedDeck(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(participant)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET_NOTES", "SECRET_PROMPT", "SECRET_IDENTITY", "roleAssignments", `"engagement":`, `"revision":`, `"editable":`} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("participant payload contains private/editor field %s", secret)
		}
	}
	if participant.Pages[0].Scene == nil || !participant.Pages[0].HideChromePageNumber {
		t.Fatal("participant lost Modern scene")
	}
	html, err := exportHTMLDocument("Scene.md", resolved, 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<style data-keynope-modern-fonts>") || !strings.Contains(html, `"scene":`) {
		t.Fatal("standalone export lacks its scene or portable font data")
	}
	// A synthetic artifact for Chromium/WebKit integration tests. Never load a
	// customer deck, contact list or real workshop archive into these fixtures.
	if dir := os.Getenv("KEYNOPE_MODERN_PRESENTATION_FIXTURE"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string][]byte{"modern.html": []byte(html), "participant.json": data, "modern-fonts.css": []byte(modernFontsCSS())} {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		presenter, err := exportHTMLDocument("Scene.md", resolved, 245, 56, preservedExportHead{}, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "presenter.html"), []byte(presenter), 0600); err != nil {
			t.Fatal(err)
		}
		deck.Appearance = nil
		retro, err := exportHTMLDocument("Scene.md", deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "retro.html"), []byte(retro), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMixedStandaloneExportFixture(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	deck.Slides[0].Elements[0].Query = setQueryValue(deck.Slides[0].Elements[0].Query, "element-style", "modern")
	// This mixed fixture uses Retro metrics for the body, unlike the original
	// all-Modern fixture. Give its two lines an explicitly adequate authored box.
	deck.Slides[0].Elements[1].Query = setQueryValue(deck.Slides[0].Elements[1].Query, "ttf-size", "65")
	deck.Slides[0].Elements[1].Query = setQueryValue(deck.Slides[0].Elements[1].Query, "height", "9")
	deck.Slides[0].Elements[5].Query = setQueryValue(deck.Slides[0].Elements[5].Query, "element-style", "modern")
	if deck.Slides[0].Elements[5].Kind != "image" {
		t.Fatal("fixture image moved")
	}
	_, err := applyElementStyle(&deck, nativeEditorAction{Slide: 0, Kind: "label", Name: "modern", ObjectIDs: []string{"box"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyModernTextStyle(&deck, nativeEditorAction{Slide: 0, Kind: "label", ObjectIDs: []string{"box"}, LabelPaint: map[string]string{"modern-opacity": "0.5"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	second := cloneSlide(deck.Slides[0])
	second.Elements[0].Text = "Second mixed page"
	deck.Slides = append(deck.Slides, second)
	// Exercise a legacy/default document with explicit per-element overrides,
	// not only the editor's already-migrated version-2 appearance envelope.
	deck.Appearance = nil
	html, err := exportHTMLDocument("Mixed.md", deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "SECRET_NOTES") {
		t.Fatal("speaker notes leaked into standalone output")
	}
	if file := os.Getenv("KEYNOPE_MIXED_EXPORT_FIXTURE"); file != "" {
		if err := os.WriteFile(file, []byte(html), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStandaloneExportOmitsPrivateActivityDefinition(t *testing.T) {
	for _, style := range []string{"legacy", "retro", "modern"} {
		t.Run(style, func(t *testing.T) {
			deck := sceneFixture(t)
			if style != "legacy" {
				deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
			}
			deck.Slides[0].Engagement.Code = "PRIVATE_SESSION_CODE"
			before, _ := json.Marshal(deck)
			slides := deck.ResolvedSlides()
			for _, presenter := range []bool{false, true} {
				html, err := exportHTMLDocument("Private.md", slides, 245, 56, preservedExportHead{}, presenter)
				if err != nil {
					t.Fatal(err)
				}
				for _, secret := range []string{"SECRET_PROMPT", "PRIVATE_SESSION_CODE"} {
					if strings.Contains(html, secret) != presenter {
						t.Fatalf("presenter=%v: incorrect activity disclosure for %s", presenter, secret)
					}
				}
				for _, secret := range []string{"SECRET_NOTES", "SECRET_IDENTITY"} {
					if strings.Contains(html, secret) {
						t.Fatalf("export leaked %s", secret)
					}
				}
			}
			after, _ := json.Marshal(deck)
			if string(before) != string(after) {
				t.Fatal("export mutated source deck")
			}
		})
	}
}

func TestMixedScenePreservesBackdropInParticipantAndExport(t *testing.T) {
	d := sceneFixture(t)
	d.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
	d.Slides[0].Effect = "stars"
	d.Slides[0].Background = "topography"
	d.Slides[0].EffectSet = true
	d.Slides[0].BackgroundSet = true
	local := exportSlidePages(d.ResolvedSlides()[0], 0, 1, 245, 56)[0]
	participant, err := participantRenderedDeck(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []exportPage{local, participant.Pages[0]} {
		if p.Scene == nil || p.Effect != "stars" || p.Background != "topography" || len(p.BackgroundLines) == 0 {
			t.Fatal("mixed scene dropped authored backdrop")
		}
		if len(p.Lines) != 0 {
			t.Fatal("scene duplicated legacy content")
		}
	}
}
