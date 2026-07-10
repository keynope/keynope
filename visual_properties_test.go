package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisualPropertiesResolveBaseLayoutAndSlidePrecedence(t *testing.T) {
	masters := defaultMasterDeck()
	base := &masters.Base.Slide
	base.FG, base.FGSet = "31", true
	base.BG, base.BGSet = "44", true
	base.HeaderFG, base.HeaderFGSet = "33", true
	base.Background, base.BackgroundSet = "mesh", true
	base.Effect, base.EffectSet = "stars", true

	layout := &masters.Layouts[0].Slide
	layout.FG, layout.FGSet = "36", true
	layout.BG, layout.BGSet = "", true
	layout.Background, layout.BackgroundSet = "", true
	layout.Effect, layout.EffectSet = "fireworks", true

	slide := Slide{LayoutID: "blank"}
	slide.HeaderFG, slide.HeaderFGSet = "35", true
	slide.Effect, slide.EffectSet = "", true
	deck := Deck{Masters: masters, Slides: []Slide{slide}}
	resolved := deck.ResolveSlide(0, false)
	if resolved.FG != "36" || resolved.BG != "" || resolved.HeaderFG != "35" || resolved.Background != "" || resolved.Effect != "" {
		t.Fatalf("resolved visual properties = %#v", resolved)
	}
	if !resolved.FGSet || !resolved.BGSet || !resolved.HeaderFGSet || !resolved.BackgroundSet || !resolved.EffectSet {
		t.Fatalf("resolved visual property flags = %#v", resolved)
	}
}

func TestVisualPropertyModesKeepInheritDistinctFromNone(t *testing.T) {
	slide := Slide{FG: "31", FGSet: true}
	setVisualProperty(&slide, visualForeground, false, "")
	if slide.FGSet || slide.FG != "" || visualPropertyMode(slide, visualForeground, true) != "inherit" {
		t.Fatalf("foreground inherit = %#v", slide)
	}
	setVisualProperty(&slide, visualForeground, true, "")
	if !slide.FGSet || slide.FG != "" || visualPropertyMode(slide, visualForeground, true) != "none" {
		t.Fatalf("foreground none = %#v", slide)
	}
	setVisualProperty(&slide, visualForeground, true, "#00aaff")
	if !slide.FGSet || slide.FG != "38;2;0;170;255" || visualPropertyMode(slide, visualForeground, true) != "choose" {
		t.Fatalf("foreground explicit = %#v", slide)
	}
}

func TestExplicitNoneColorsRoundTripMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none-colors.md")
	deck := Deck{Slides: []Slide{{
		Elements:    []Element{{Kind: "text", Text: "Content"}},
		FGSet:       true,
		BGSet:       true,
		HeaderFGSet: true,
	}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"fg=none", "bg=none", "header=none"} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("missing %s in:\n%s", field, raw)
		}
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 {
		t.Fatalf("parsed slides = %#v", parsed.Slides)
	}
	slide := parsed.Slides[0]
	if !slide.FGSet || slide.FG != "" || !slide.BGSet || slide.BG != "" || !slide.HeaderFGSet || slide.HeaderFG != "" {
		t.Fatalf("parsed explicit none colors = %#v", slide)
	}
}

func TestEmptySlideWithOnlyVisualOverridesSurvivesParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-visual.md")
	deck := Deck{Slides: []Slide{{FGSet: true, EffectSet: true}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || !parsed.Slides[0].FGSet || !parsed.Slides[0].EffectSet {
		t.Fatalf("empty visual slide was dropped or changed: %#v", parsed.Slides)
	}
}

func TestMasterEditorPageNumberToggleWorksForBaseAndLayouts(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck()}
	base := masterEditorSlide(&deck, 0)
	if got := toggleMasterEditorPageNumber(&deck, 0, &base); got != "hide" {
		t.Fatalf("Base editor toggle = %q", got)
	}
	if _, ok := pageNumberElement(base); ok {
		t.Fatal("Base editor retained hidden page number")
	}
	if got := toggleMasterEditorPageNumber(&deck, 0, &base); got != "show" {
		t.Fatalf("Base editor second toggle = %q", got)
	}
	number, ok := pageNumberElement(base)
	if !ok || number.Inherited {
		t.Fatalf("Base editor page number = %#v ok=%v", number, ok)
	}

	layout := masterEditorSlide(&deck, 1)
	if inherited, ok := pageNumberElement(layout); !ok || !inherited.Inherited {
		t.Fatalf("inherited layout page number = %#v ok=%v", inherited, ok)
	}
	if got := toggleMasterEditorPageNumber(&deck, 1, &layout); got != "show" {
		t.Fatalf("layout editor show toggle = %q", got)
	}
	if own, ok := pageNumberElement(layout); !ok || own.Inherited {
		t.Fatalf("layout-owned page number = %#v ok=%v", own, ok)
	}
	if got := toggleMasterEditorPageNumber(&deck, 1, &layout); got != "hide" {
		t.Fatalf("layout editor hide toggle = %q", got)
	}
	if _, ok := pageNumberElement(layout); ok {
		t.Fatal("hidden layout retained page number")
	}
	if got := toggleMasterEditorPageNumber(&deck, 1, &layout); got != "inherit" {
		t.Fatalf("layout editor inherit toggle = %q", got)
	}
	if inherited, ok := pageNumberElement(layout); !ok || !inherited.Inherited {
		t.Fatalf("restored inherited page number = %#v ok=%v", inherited, ok)
	}
}

func TestMasterEditorContextAllowsPageNumberAndVisualProperties(t *testing.T) {
	master := interactionContext{Mode: editorModeSelect, Selection: selectionNone, Master: true}
	if !actionAllowedInContext(master, "page-number") || !actionAllowedInContext(master, "visual-properties") {
		t.Fatalf("master actions unavailable: %#v", editActionSpecs(master))
	}
	normal := interactionContext{Mode: editorModeSelect, Selection: selectionNone}
	if actionAllowedInContext(normal, "page-number") || actionAllowedInContext(normal, "visual-properties") {
		t.Fatalf("master-only actions leaked into ordinary element editing: %#v", editActionSpecs(normal))
	}
	if !actionSpecsAllow(mainActionSpecs(), "visual-properties") {
		t.Fatal("visual properties missing from normal slide shortcuts")
	}
}

func TestMasterEditorExtractionPreservesRawLayoutVisualOverrides(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck()}
	target := &deck.Masters.Layouts[0].Slide
	target.FG, target.FGSet = "31", true
	target.Background, target.BackgroundSet = "mesh", true
	working := masterEditorSlide(&deck, 1)
	extracted := extractEditedMasterSlide(*target, working, true)
	if extracted.FG != "31" || !extracted.FGSet || extracted.Background != "mesh" || !extracted.BackgroundSet {
		t.Fatalf("layout visual overrides were materialized or lost: %#v", extracted)
	}
}
