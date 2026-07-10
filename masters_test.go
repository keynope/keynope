package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyDeckParsesWithoutMasters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.md")
	if err := os.WriteFile(path, []byte("# Legacy\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 1 || deck.Slides[0].Elements[0].Text != "Legacy" {
		t.Fatalf("unexpected slides: %#v", deck.Slides)
	}
	if deckHasMasterData(deck.Masters) {
		t.Fatalf("legacy deck unexpectedly gained masters: %#v", deck.Masters)
	}
}

func TestStarterDeckSeedsDefaultMastersAndBoundTitleSlots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "starter.md")
	if err := os.WriteFile(path, []byte(starterDeckMarkdown(120, 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Masters.Layouts) != 4 || len(deck.Slides) != 1 || deck.Slides[0].LayoutID != "title-subtitle" {
		t.Fatalf("starter deck = %#v", deck)
	}
	if len(deck.Slides[0].Elements) < 2 || deck.Slides[0].Elements[0].MasterSlotID != "title-subtitle-title" || deck.Slides[0].Elements[1].MasterSlotID != "title-subtitle-subtitle" {
		t.Fatalf("starter bindings = %#v", deck.Slides[0].Elements)
	}
}

func TestMasterDeckRoundTripDoesNotCreateSlides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mastered.md")
	deck := Deck{Masters: defaultMasterDeck()}
	deck.Slides = []Slide{{
		LayoutID: "title-body",
		Elements: []Element{{
			Kind: "heading", Level: 1, Text: "Actual title", MasterSlotID: "title-body-title",
		}},
	}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 {
		t.Fatalf("master layouts leaked into slide count: %d", len(parsed.Slides))
	}
	if len(parsed.Masters.Layouts) != 4 {
		t.Fatalf("layout count = %d", len(parsed.Masters.Layouts))
	}
	resolved := parsed.ResolveSlide(0, false)
	if len(resolved.Elements) != 2 || resolved.Elements[0].Text != "Actual title" || resolved.Elements[1].Kind != "page-number" || resolved.Elements[1].Text != "1" {
		t.Fatalf("resolved elements = %#v", resolved.Elements)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "keynope-masters version=1 base64:") || !strings.Contains(string(raw), "<!-- layout=title-body -->") {
		t.Fatalf("missing master metadata:\n%s", raw)
	}
}

func TestUnsupportedMasterDeckVersionFails(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"version":2}`))
	path := filepath.Join(t.TempDir(), "future.md")
	text := "<!-- keynope-masters version=2 base64:" + payload + " -->\n\n# Slide\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDeck(path); err == nil || !strings.Contains(err.Error(), "unsupported master deck version") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveSlideInheritsStaticElementsAndSparseSlotOverrides(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.Elements = []Element{{Kind: "shape", ID: "brand", Query: "shape=square&color=blue"}}
	masters.Layouts = []MasterLayout{{
		ID: "article", Name: "Article", Slide: Slide{
			Background: "blueprint", BackgroundSet: true,
			Elements: []Element{masterPlaceholder("article-title", placeholderTitle, "Title", "heading", 1)},
		},
	}}
	masters.Layouts[0].Slide.Elements[0].Query = "left=4&fg=white"
	deck := Deck{Masters: masters, Slides: []Slide{{
		LayoutID: "article",
		Elements: []Element{{Kind: "heading", Level: 1, Text: "Hello", MasterSlotID: "article-title", Query: "left=9"}},
	}}}
	resolved := deck.ResolveSlide(0, false)
	if resolved.Background != "blueprint" || len(resolved.Elements) != 3 {
		t.Fatalf("resolved slide = %#v", resolved)
	}
	if !resolved.Elements[0].Inherited || resolved.Elements[0].ID != "brand" {
		t.Fatalf("base element = %#v", resolved.Elements[0])
	}
	if got := resolved.Elements[1].Query; got != "fg=white&left=9" {
		t.Fatalf("merged query = %q", got)
	}
	resolved.Elements[1].Query = "fg=white&left=12"
	deck.StoreResolvedSlide(0, resolved)
	if got := deck.Slides[0].Elements[0].Query; got != "left=12" {
		t.Fatalf("stored sparse query = %q", got)
	}
}

func TestSparseQueryOverrideCanRemoveInheritedProperty(t *testing.T) {
	override := queryDifference("left=4&fg=white", "right=7&fg=white")
	if override != "master-clear=left&right=7" {
		t.Fatalf("override = %q", override)
	}
	if merged := mergeQueries("left=4&fg=white", override); merged != "fg=white&right=7" {
		t.Fatalf("merged query = %q", merged)
	}
	line := "<!-- " + placementCommentText(override) + " -->"
	parsed, ok := textPlacementComment(line)
	if !ok || parsed != override {
		t.Fatalf("round-trip query = %q ok=%v line=%q", parsed, ok, line)
	}
}

func TestResolveSlideShowsEmptySlotsOnlyForEditing(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "title-subtitle"}}}
	for _, element := range deck.ResolveSlide(0, false).Elements {
		if element.Placeholder {
			t.Fatalf("presentation contains empty placeholder: %#v", element)
		}
	}
	editing := deck.ResolveSlide(0, true)
	if len(editing.Elements) != 3 || !editing.Elements[0].Placeholder || editing.Elements[0].MasterSlotID == "" || editing.Elements[2].Kind != "page-number" {
		t.Fatalf("editing placeholders = %#v", editing.Elements)
	}
}

func TestRebindLayoutPreservesMatchedAndUnmatchedContent(t *testing.T) {
	masters := defaultMasterDeck()
	deck := Deck{Masters: masters, Slides: []Slide{{
		LayoutID: "title-body",
		Elements: []Element{
			{Kind: "heading", Level: 1, Text: "Title", MasterSlotID: "title-body-title"},
			{Kind: "text", Text: "Body", MasterSlotID: "title-body-body"},
		},
	}}}
	if !deck.RebindSlideLayout(0, "title") {
		t.Fatal("rebind failed")
	}
	slide := deck.Slides[0]
	if slide.LayoutID != "title" || len(slide.Elements) != 2 {
		t.Fatalf("rebound slide = %#v", slide)
	}
	if slide.Elements[0].Text != "Title" || slide.Elements[0].MasterSlotID != "title-title" {
		t.Fatalf("title was not rebound: %#v", slide.Elements[0])
	}
	if slide.Elements[1].Text != "Body" || slide.Elements[1].MasterSlotID != "" {
		t.Fatalf("unmatched body was not preserved locally: %#v", slide.Elements[1])
	}
}

func TestInheritedMasterElementsAreNotSelectable(t *testing.T) {
	element := Element{Kind: "shape", Inherited: true}
	if isSelectableElement(element) {
		t.Fatal("inherited master element is selectable")
	}
}

func TestInheritedMasterLayersRenderBelowLocalLayers(t *testing.T) {
	slide := Slide{Elements: []Element{
		{Kind: "text", Text: "MASTER", Query: "top=0", Inherited: true},
		{Kind: "shape", Query: "shape=square&top=0&width=4&height=2"},
	}}
	lines := layout(slide, 40, 20)
	firstMaster, firstLocal := -1, -1
	for index, line := range lines {
		switch line.Element {
		case 0:
			if firstMaster < 0 {
				firstMaster = index
			}
		case 1:
			if firstLocal < 0 {
				firstLocal = index
			}
		}
	}
	if firstMaster < 0 || firstLocal < 0 || firstMaster >= firstLocal {
		t.Fatalf("master lines must render before local lines: master=%d local=%d lines=%#v", firstMaster, firstLocal, lines)
	}
}

func TestExplicitNoneOverridesInheritedBackground(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.Background = "blueprint"
	masters.Base.Slide.BackgroundSet = true
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank", BackgroundSet: true}}}
	resolved := deck.ResolveSlide(0, false)
	if resolved.Background != "" || !resolved.BackgroundSet {
		t.Fatalf("resolved background = %q set=%v", resolved.Background, resolved.BackgroundSet)
	}
	path := filepath.Join(t.TempDir(), "none.md")
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "<!-- background=none -->") {
		t.Fatalf("explicit none was not persisted:\n%s", raw)
	}
}

func TestMasterImagePathsRoundTripRelativeToDeck(t *testing.T) {
	directory := t.TempDir()
	asset := filepath.Join(directory, "logo.png")
	if err := os.WriteFile(asset, []byte("not decoded during parsing"), 0o644); err != nil {
		t.Fatal(err)
	}
	masters := defaultMasterDeck()
	masters.Base.Slide.Elements = []Element{{Kind: "image", ID: "logo", Path: asset}}
	path := filepath.Join(directory, "deck.md")
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank"}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	decoded, _, err := decodeMasterDeckMetadata(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Base.Slide.Elements[0].Path; got != "logo.png" {
		t.Fatalf("stored image path = %q", got)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Masters.Base.Slide.Elements[0].Path; got != asset {
		t.Fatalf("resolved image path = %q", got)
	}
}

func TestPlaceholderRoleAssignment(t *testing.T) {
	element := Element{Kind: "text", Text: ""}
	applyPlaceholderRole(&element, placeholderTitle)
	if element.PlaceholderRole != placeholderTitle || element.SlotID == "" || element.Kind != "heading" || element.Level != 1 || element.Text != "Title" {
		t.Fatalf("title placeholder = %#v", element)
	}
	applyPlaceholderRole(&element, placeholderFixed)
	if element.PlaceholderRole != "" || element.SlotID != "" {
		t.Fatalf("fixed element retained slot metadata: %#v", element)
	}
}

func TestCloneMasterLayoutGetsFreshStableIDs(t *testing.T) {
	source := defaultMasterDeck().Layouts[2]
	clone := cloneMasterLayoutFresh(source)
	if clone.ID == source.ID || clone.Name == source.Name {
		t.Fatalf("clone identity was not refreshed: %#v", clone)
	}
	for index := range source.Slide.Elements {
		if clone.Slide.Elements[index].ID == source.Slide.Elements[index].ID || clone.Slide.Elements[index].SlotID == source.Slide.Elements[index].SlotID {
			t.Fatalf("element %d retained master identity", index)
		}
	}
}

func TestFullDeckUndoRestoresMastersAndSlides(t *testing.T) {
	before := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
	after := cloneDeck(before)
	after.Masters.Layouts[0].Name = "Empty"
	after.Slides = append(after.Slides, Slide{LayoutID: "title"})
	state := &EditState{}
	if !commitFullDeckSnapshot(state, before, after, 0, 1) {
		t.Fatal("snapshot not committed")
	}
	current := cloneDeck(after)
	index, ok := undoDeckSnapshot(state, &current)
	if !ok || index != 0 || len(current.Slides) != 1 || current.Masters.Layouts[0].Name != "Blank" {
		t.Fatalf("undo result index=%d deck=%#v", index, current)
	}
	index, ok = redoDeckSnapshot(state, &current)
	if !ok || index != 1 || len(current.Slides) != 2 || current.Masters.Layouts[0].Name != "Empty" {
		t.Fatalf("redo result index=%d deck=%#v", index, current)
	}
}

func TestMasterLayoutExtractionDropsBaseOverlay(t *testing.T) {
	original := Slide{Background: "waves", BackgroundSet: true, Elements: []Element{{Kind: "text", ID: "layout", Text: "Layout"}}}
	working := Slide{Background: "blueprint", BackgroundSet: true, Elements: []Element{
		{Kind: "shape", ID: "base", Inherited: true},
		{Kind: "text", ID: "layout", Text: "Edited"},
	}}
	extracted := extractEditedMasterSlide(original, working, true)
	if extracted.Background != "waves" || len(extracted.Elements) != 1 || extracted.Elements[0].ID != "layout" || extracted.Elements[0].Text != "Edited" {
		t.Fatalf("extracted layout = %#v", extracted)
	}
}

func TestStoreUnmasteredResolvedSlideDropsInheritedRuntimeElements(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Local"}}}}}
	effective := Slide{Elements: []Element{
		{Kind: "shape", ID: "base", Inherited: true},
		{Kind: "text", Text: "Local"},
	}}
	deck.StoreResolvedSlide(0, effective)
	if len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Text != "Local" {
		t.Fatalf("stored slide = %#v", deck.Slides[0])
	}
}

func TestEmptyPlaceholderPositionOverrideRoundTripsWithoutPresentingSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "placeholder.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "title"}}}
	editing := deck.ResolveSlide(0, true)
	editing.Elements[0].Query = "left=12"
	deck.StoreResolvedSlide(0, editing)
	if len(deck.Slides[0].Elements) != 1 || !deck.Slides[0].Elements[0].Placeholder {
		t.Fatalf("placeholder override not stored: %#v", deck.Slides[0])
	}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range parsed.ResolveSlide(0, false).Elements {
		if element.Placeholder {
			t.Fatalf("presentation contains placeholder element: %#v", element)
		}
	}
	resolvedEditing := parsed.ResolveSlide(0, true)
	if len(resolvedEditing.Elements) != 2 || resolvedEditing.Elements[0].Text != "Title" || resolvedEditing.Elements[0].Query != "left=12" || resolvedEditing.Elements[1].Kind != "page-number" {
		t.Fatalf("editing placeholder = %#v", resolvedEditing.Elements)
	}
}

func TestResolvedMasterContentFeedsExportWithoutAddingSlides(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.Elements = []Element{{Kind: "shape", ID: "base-shape", Query: "shape=square&width=4&height=2"}}
	deck := Deck{Masters: masters, Slides: []Slide{{
		LayoutID: "title",
		Elements: []Element{{Kind: "heading", Level: 1, Text: "Exported", MasterSlotID: "title-title"}},
	}}}
	resolved := deck.ResolvedSlides()
	pages := exportSlidePages(resolved[0], 0, len(resolved), 80, 25)
	if len(pages) == 0 || pages[0].SlideCount != 1 {
		t.Fatalf("export pages = %#v", pages)
	}
	roles := map[string]bool{}
	for _, page := range pages {
		if page.SlideCount != 1 || page.Slide != 0 {
			t.Fatalf("master leaked into export numbering: %#v", page)
		}
		for _, line := range page.Lines {
			roles[line.Role] = true
		}
	}
	if !roles["shape"] || !roles["heading"] {
		t.Fatalf("resolved export roles = %#v", roles)
	}
}

func TestPresenterFullRefreshIncludesEveryResolvedSlide(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{
		{LayoutID: "title", Elements: []Element{{Kind: "heading", Level: 1, Text: "One", MasterSlotID: "title-title"}}},
		{LayoutID: "title", Elements: []Element{{Kind: "heading", Level: 1, Text: "Two", MasterSlotID: "title-title"}}},
	}}
	presenter := &presenterCompanion{pages: map[int][]exportPage{}}
	presenter.RefreshAllAsync(filepath.Join(t.TempDir(), "deck.md"), deck.ResolvedSlides(), 80, 25)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		presenter.mu.RLock()
		count := len(presenter.pages)
		version := presenter.state.DeckVersion
		presenter.mu.RUnlock()
		if count == 2 && version > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("presenter did not receive every resolved slide")
}

func TestMasteredSlideUndoStoresOnlySourceOverrides(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.Elements = []Element{{Kind: "shape", ID: "base", Query: "shape=square"}}
	deck := Deck{Masters: masters, Slides: []Slide{{
		LayoutID: "title",
		Elements: []Element{{Kind: "heading", Level: 1, Text: "Before", MasterSlotID: "title-title"}},
	}}}
	before := deck.ResolveSlide(0, true)
	after := cloneSlide(before)
	after.Elements[1].Text = "After"
	state := &EditState{}
	commitSlideSnapshot(state, before, after)
	deck.StoreResolvedSlide(0, after)
	working := deck.ResolveSlide(0, true)
	if !undoSlideSnapshot(state, &working) {
		t.Fatal("undo failed")
	}
	deck.StoreResolvedSlide(0, working)
	if len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Text != "Before" || deck.Slides[0].Elements[0].Inherited {
		t.Fatalf("source slide contains resolved master data: %#v", deck.Slides[0])
	}
}

func TestEmptyImagePlaceholderIsVisibleOnlyInEditor(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Layouts = append(masters.Layouts, MasterLayout{ID: "image", Name: "Image", Slide: Slide{Elements: []Element{
		masterPlaceholder("image-slot", placeholderImage, "", "image", 0),
	}}})
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: "image"}}}
	editing := deck.ResolveSlide(0, true)
	lines := displayLines(editing, 80, 25, 0)
	hasImage := false
	for _, line := range lines {
		if line.Role == "image" && line.Element == 0 {
			hasImage = true
			break
		}
	}
	if !hasImage || !isSelectableElement(editing.Elements[0]) {
		t.Fatalf("image placeholder lines=%#v element=%#v", lines, editing.Elements[0])
	}
	for _, element := range deck.ResolveSlide(0, false).Elements {
		if element.Kind == "image" {
			t.Fatal("empty image placeholder leaked into presentation")
		}
	}
}

func TestDefaultBaseMasterShowsBottomRightDynamicPageNumbers(t *testing.T) {
	masters := defaultMasterDeck()
	if masters.Base.Slide.PageNumber != pageNumberShow {
		t.Fatalf("base page-number mode = %q", masters.Base.Slide.PageNumber)
	}
	template, ok := pageNumberElement(masters.Base.Slide)
	if !ok || !strings.Contains(template.Query, "bottom=1") || !strings.Contains(template.Query, "right=2") {
		t.Fatalf("base page-number element = %#v", template)
	}
	if !isSelectableElement(template) {
		t.Fatal("raw Base Master page number is not positionable")
	}
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank"}, {LayoutID: "title"}}}
	for index, want := range []string{"1", "2"} {
		resolved := deck.ResolveSlide(index, false)
		number, ok := pageNumberElement(resolved)
		if !ok || number.Text != want || !number.Inherited || isSelectableElement(number) {
			t.Fatalf("resolved page number %d = %#v", index, number)
		}
	}
}

func TestPageNumberInheritanceAndLocalOverrides(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Layouts[0].Slide.PageNumber = pageNumberHide
	deck := Deck{Masters: masters, Slides: []Slide{
		{LayoutID: "blank"},
		{LayoutID: "blank", PageNumber: pageNumberShow},
		{LayoutID: "title", PageNumber: pageNumberHide},
	}}
	if _, ok := pageNumberElement(deck.ResolveSlide(0, false)); ok {
		t.Fatal("layout hide did not suppress Base Master page number")
	}
	if number, ok := pageNumberElement(deck.ResolveSlide(1, false)); !ok || number.Text != "2" {
		t.Fatalf("slide show override = %#v ok=%v", number, ok)
	}
	if _, ok := pageNumberElement(deck.ResolveSlide(2, false)); ok {
		t.Fatal("slide hide override did not suppress page number")
	}
}

func TestMasterPageNumberPolicyReplacesOnlyMasteredExportChrome(t *testing.T) {
	legacyPages := exportSlidePages(Slide{Elements: []Element{{Kind: "text", Text: "Legacy"}}}, 0, 1, 80, 25)
	if len(legacyPages) == 0 || legacyPages[0].HideChromePageNumber {
		t.Fatalf("legacy export chrome unexpectedly hidden: %#v", legacyPages)
	}
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}, {LayoutID: "blank", PageNumber: pageNumberHide}}}
	for index, slide := range deck.ResolvedSlides() {
		pages := exportSlidePages(slide, index, len(deck.Slides), 80, 25)
		if len(pages) == 0 || !pages[0].HideChromePageNumber {
			t.Fatalf("mastered export %d retained legacy chrome: %#v", index, pages)
		}
	}
}

func TestDynamicPageNumberRepeatsOnOverflowSubpages(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{
		LayoutID: "blank",
		Elements: []Element{{Kind: "text", Text: "Second page", Query: "top=30"}},
	}}}
	resolved := deck.ResolveSlide(0, false)
	pages := displayPages(resolved, 80, 25)
	if len(pages) < 2 {
		t.Fatalf("expected overflow pages, got %d", len(pages))
	}
	for pageIndex, lines := range pages {
		hasPageNumber := false
		for _, line := range lines {
			if line.Element >= 0 && line.Element < len(resolved.Elements) && resolved.Elements[line.Element].Kind == "page-number" {
				hasPageNumber = true
				break
			}
		}
		if !hasPageNumber {
			t.Fatalf("overflow page %d has no page number: %#v", pageIndex, lines)
		}
	}
}

func TestLayoutPageNumberUsesItsOwnMasterPosition(t *testing.T) {
	masters := defaultMasterDeck()
	layout := &masters.Layouts[1]
	layout.Slide.PageNumber = pageNumberShow
	layout.Slide.Elements = append(layout.Slide.Elements, Element{
		Kind: "page-number", Text: "1", ID: "title-page-number", Query: "bottom=3&left=4&fg=%23ffffff",
	})
	deck := Deck{Masters: masters, Slides: []Slide{{LayoutID: layout.ID}}}
	number, ok := pageNumberElement(deck.ResolveSlide(0, false))
	if !ok || number.Query != "bottom=3&left=4&fg=%23ffffff" {
		t.Fatalf("layout page-number position = %#v ok=%v", number, ok)
	}
}

func TestPageNumberSlideOverrideRoundTripsMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbers.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank", PageNumber: pageNumberHide}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "<!-- page-number=hide -->") {
		t.Fatalf("missing page-number override:\n%s", raw)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || parsed.Slides[0].PageNumber != pageNumberHide {
		t.Fatalf("parsed page-number override = %#v", parsed.Slides)
	}
}

func TestMasterPageNumberToggleCyclesPoliciesAndElements(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck()}
	if got := toggleMasterPageNumber(&deck, 0); got != "hide" {
		t.Fatalf("Base toggle = %q", got)
	}
	if _, ok := pageNumberElement(deck.Masters.Base.Slide); ok {
		t.Fatal("hidden Base Master retained page-number element")
	}
	if got := toggleMasterPageNumber(&deck, 0); got != "show" {
		t.Fatalf("Base second toggle = %q", got)
	}
	if _, ok := pageNumberElement(deck.Masters.Base.Slide); !ok {
		t.Fatal("shown Base Master has no page-number element")
	}
	if got := toggleMasterPageNumber(&deck, 1); got != "show" {
		t.Fatalf("layout first toggle = %q", got)
	}
	if _, ok := pageNumberElement(deck.Masters.Layouts[0].Slide); !ok {
		t.Fatal("shown layout has no page-number element")
	}
	if got := toggleMasterPageNumber(&deck, 1); got != "hide" {
		t.Fatalf("layout second toggle = %q", got)
	}
	if got := toggleMasterPageNumber(&deck, 1); got != "inherit" || deck.Masters.Layouts[0].Slide.PageNumber != "" {
		t.Fatalf("layout third toggle = %q slide=%#v", got, deck.Masters.Layouts[0].Slide)
	}
}

func TestDetachPreservesDynamicPageNumberWithoutMasterElement(t *testing.T) {
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
	if !deck.DetachSlide(0) {
		t.Fatal("detach failed")
	}
	if deck.Slides[0].LayoutID != "" || deck.Slides[0].PageNumber != pageNumberShow {
		t.Fatalf("detached source = %#v", deck.Slides[0])
	}
	number, ok := pageNumberElement(deck.ResolveSlide(0, false))
	if !ok || number.Text != "1" {
		t.Fatalf("detached page number = %#v ok=%v", number, ok)
	}
}
