package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeEditorMutationsPersistAndUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "heading", Level: 1, Text: "Original"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "text"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if len(state.Slides[0].Elements) != 2 || state.Selected != 1 {
		t.Fatalf("state after insert = %#v", state)
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if len(state.Slides[0].Elements) != 1 || state.Slides[0].Elements[0].Text != "Original" {
		t.Fatalf("state after undo = %#v", state)
	}
	if state.Selected != -1 || len(state.Selection) != 0 {
		t.Fatalf("selection after undo = selected %d, selection %v", state.Selected, state.Selection)
	}
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if len(state.Slides[0].Elements) != 2 || state.Selected != -1 || len(state.Selection) != 0 {
		t.Fatalf("state after redo = %#v", state)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestNativeEditorPastesCopiedElementsWithFreshIDs(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Existing", ID: "existing"}}}}})
	copied := []Element{{Kind: "text", Text: "Copied", ID: "old-copy"}, {Kind: "shape", Query: "shape=diamond", ID: "old-shape"}}
	if err := session.apply(nativeEditorAction{Action: "paste-elements", ElementsData: copied}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if len(state.Slides[0].Elements) != 3 || state.Selected != 2 {
		t.Fatalf("state after paste = %#v", state)
	}
	if state.Slides[0].Elements[1].Text != "Copied" || state.Slides[0].Elements[2].Query != "shape=diamond" {
		t.Fatalf("pasted elements = %#v", state.Slides[0].Elements)
	}
	if state.Slides[0].Elements[1].ID == "old-copy" || state.Slides[0].Elements[2].ID == "old-shape" {
		t.Fatalf("pasted elements kept source IDs: %#v", state.Slides[0].Elements)
	}
}

func TestNativeEditorRejectsInvalidSelection(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{}}})
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: 4}); err == nil {
		t.Fatal("expected invalid slide selection to fail")
	}
}

func TestNativeEditorAddsTitleAndSubtitleHeadings(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{}}})
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "heading", Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "heading", Level: 2}); err != nil {
		t.Fatal(err)
	}
	elements := session.state().Slides[0].Elements
	if len(elements) != 2 || elements[0].Level != 1 || elements[0].Text != "Title" || elements[1].Level != 2 || elements[1].Text != "Subtitle" {
		t.Fatalf("added heading elements = %#v", elements)
	}
}

func TestNativeEditorAddsChosenShape(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{}}})
	for _, shape := range []string{"circle", "square", "triangle", "diamond"} {
		if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "shape", Name: shape}); err != nil {
			t.Fatal(err)
		}
	}
	elements := session.state().Slides[0].Elements
	if len(elements) != 4 {
		t.Fatalf("shape count = %d, want 4", len(elements))
	}
	for index, want := range []string{"circle", "square", "triangle", "diamond"} {
		if got := shapeName(elements[index]); got != want {
			t.Fatalf("shape %d = %q, want %q", index, got, want)
		}
	}
}

func TestNativeEditorCanClearSelection(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Selected"}}}}})
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: -1}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Selected != -1 || len(state.Selection) != 0 {
		t.Fatalf("selection was not cleared: selected %d, selection %v", state.Selected, state.Selection)
	}
}

func TestNativeEditorPresentationNavigationUpdatesCurrentSlide(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{
		{Elements: []Element{{Kind: "text", Text: "First"}}},
		{Elements: []Element{{Kind: "text", Text: "Second"}}},
	}})
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "navigate-presentation", Slide: 1, Page: 2}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 1 || state.Selected != -1 || len(state.Selection) != 0 {
		t.Fatalf("navigation state = current %d, selected %d, selection %v", state.Current, state.Selected, state.Selection)
	}
}

func TestNativeEditorControlsSharedPresenterTimer(t *testing.T) {
	companion := &presenterCompanion{}
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{}}})
	session.companion = companion
	before := time.Now().UnixMilli()
	if err := session.apply(nativeEditorAction{Action: "start-timer", Value: 90}); err != nil {
		t.Fatal(err)
	}
	companion.mu.RLock()
	mode, end := companion.state.TimerMode, companion.state.TimerEndMS
	companion.mu.RUnlock()
	if mode != "running" || end < before+89_000 {
		t.Fatalf("timer mode=%q end=%d before=%d", mode, end, before)
	}
	if err := session.apply(nativeEditorAction{Action: "stop-timer"}); err != nil {
		t.Fatal(err)
	}
	companion.mu.RLock()
	mode, end = companion.state.TimerMode, companion.state.TimerEndMS
	companion.mu.RUnlock()
	if mode != "" || end != 0 {
		t.Fatalf("stopped timer mode=%q end=%d", mode, end)
	}
}

func TestNativeEditorUpdatesNotesOnSpecifiedSlide(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{Notes: "first"}, {Notes: "second"}}})
	if err := session.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 1, Notes: "updated"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 0 || state.Slides[0].Notes != "first" || state.Slides[1].Notes != "updated" {
		t.Fatalf("unexpected notes state: current=%d notes=%q/%q", state.Current, state.Slides[0].Notes, state.Slides[1].Notes)
	}
}

func TestNativeEditorPreviewRendersWithoutMutatingDeck(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Original"}}}}})
	preview := nativeEditorAction{Element: 0, ElementData: &Element{Kind: "text", Text: "Preview glyphs"}, Cols: 80, Rows: 25}
	body, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/preview", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	session.handlePreview(recorder, request)
	var pages []exportPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &pages); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || len(pages) == 0 || len(pages[0].Lines) == 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := session.state().Slides[0].Elements[0].Text; got != "Original" {
		t.Fatalf("preview mutated deck text to %q", got)
	}
}

func TestNativeEditorFitsLargestTextSizeInsideDragBox(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Fit"}}}}})
	fit := func(width, height int) nativeEditorTextFit {
		action := nativeEditorAction{Element: 0, ElementData: &Element{Kind: "text", Text: "Fit"}, BoxWidth: width, BoxHeight: height, Cols: 120, Rows: 50}
		body, err := json.Marshal(action)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/editor/fit-text", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		session.handleFitText(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("fit status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var result nativeEditorTextFit
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Pages) == 0 {
			t.Fatal("fit returned no preview pages")
		}
		return result
	}
	small, large := fit(8, 2), fit(100, 40)
	if smallSize, largeSize := textSize(small.Element), textSize(large.Element); largeSize <= smallSize {
		t.Fatalf("fitted sizes small=%d large=%d", smallSize, largeSize)
	}
	if got := session.state().Slides[0].Elements[0].Query; got != "" {
		t.Fatalf("fit preview mutated deck query to %q", got)
	}
}

func TestNativeEditorSlideColorsOverrideMasterColors(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.BG, masters.Base.Slide.BGSet = "44", true
	masters.Base.Slide.FG, masters.Base.Slide.FGSet = "37", true
	session := newNativeEditorSession("deck.md", Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank"}}})
	slide := session.state().Slides[0]
	slide.BG, slide.BGSet = "#123456", true
	slide.FG, slide.FGSet = "#abcdef", true
	if err := session.apply(nativeEditorAction{Action: "update-slide", SlideData: &slide}); err != nil {
		t.Fatal(err)
	}
	resolved := session.state().Resolved[0]
	if resolved.BG != "48;2;18;52;86" || resolved.FG != "38;2;171;205;239" {
		t.Fatalf("resolved editor colours = bg %q fg %q", resolved.BG, resolved.FG)
	}
}

func TestNativeEditorMasterModeUsesSlideEditingActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if !state.MasterMode || state.Current != 0 || len(state.Slides) != len(deck.Masters.Layouts)+1 {
		t.Fatalf("master state = mode %v current %d slides %d", state.MasterMode, state.Current, len(state.Slides))
	}
	if err := session.apply(nativeEditorAction{Action: "add-slide"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "text"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if state.Current != len(state.Slides)-1 || len(state.Slides[state.Current].Elements) != 1 || state.Slides[state.Current].Elements[0].Text != "Text" {
		t.Fatalf("new master workspace = %#v", state)
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if session.state().MasterMode {
		t.Fatal("master mode did not exit")
	}
}

func TestNativeEditorLayoutsLayersAndMultiSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{Kind: "text", Text: "one", ID: "one"},
		{Kind: "text", Text: "two", ID: "two"},
		{Kind: "text", Text: "three", ID: "three"},
	}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "move-element", Element: 0, Kind: "front"}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Elements[2].ID; got != "one" {
		t.Fatalf("front element = %q", got)
	}
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 0, Name: "toggle"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 1, Name: "toggle"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "delete-selection"}); err != nil {
		t.Fatal(err)
	}
	if got := len(session.state().Slides[0].Elements); got != 1 {
		t.Fatalf("elements after bulk delete = %d", got)
	}
	if err := session.apply(nativeEditorAction{Action: "add-layout", Name: "Custom"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if got := state.Masters.Layouts[len(state.Masters.Layouts)-1].Name; got != "Custom" {
		t.Fatalf("layout name = %q", got)
	}
}

func TestNativeEditorDuplicatesElementWithNewIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Duplicate me", ID: "original"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "duplicate-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if len(state.Slides[0].Elements) != 2 {
		t.Fatalf("elements after duplicate = %d", len(state.Slides[0].Elements))
	}
	if state.Selected != 1 || len(state.Selection) != 1 || state.Selection[0] != 1 {
		t.Fatalf("selection after duplicate = selected %d, selection %v", state.Selected, state.Selection)
	}
	if got := state.Slides[0].Elements[1]; got.Text != "Duplicate me" || got.ID == "" || got.ID == "original" {
		t.Fatalf("duplicated element = %#v", got)
	}
}

func TestNormalizedTextKindPlacementKeepsLargerHeadingOnCurrentPage(t *testing.T) {
	element := Element{Kind: "heading", Level: 1, Text: "Hi", ID: "edge", Query: "left_pct=0.950000&top=20"}
	normalized := normalizedTextKindPlacement(Slide{Elements: []Element{element}}, element, 0, 80, 25, 0)
	preview := Slide{Elements: []Element{normalized}}
	top, bottom, left, right, ok := elementFullBox(preview, 0, 80, 25)
	if !ok {
		t.Fatal("normalized heading did not render")
	}
	if top < 0 || bottom >= 25 || left < 0 || right >= 80 {
		t.Fatalf("normalized bounds = top:%d bottom:%d left:%d right:%d query=%q", top, bottom, left, right, normalized.Query)
	}
	if placement := parseImagePlacement(normalized.Query); placement.top == nil || *placement.top >= 20 {
		t.Fatalf("normalized query did not move heading up: %q", normalized.Query)
	} else if placement.leftPct == nil || *placement.leftPct >= 0.95 {
		t.Fatalf("normalization did not retain a safe relative horizontal placement: %q", normalized.Query)
	}
}

func TestNormalizedTextKindPlacementPreservesAlignmentAndRelativeOffsets(t *testing.T) {
	tests := []struct {
		name  string
		query string
		check func(imagePlacement) bool
	}{
		{name: "alignment", query: "align=center&row_delta=2", check: func(placement imagePlacement) bool {
			return placement.align == "center" && placement.rowDelta != nil && *placement.rowDelta == 2
		}},
		{name: "relative offset", query: "left_pct=0.350000&top=2", check: func(placement imagePlacement) bool {
			return placement.leftPct != nil && *placement.leftPct == 0.35 && placement.top != nil && *placement.top == 2
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			element := Element{Kind: "heading", Level: 2, Text: "Hi", ID: "placed", Query: test.query}
			normalized := normalizedTextKindPlacement(Slide{Elements: []Element{element}}, element, 0, 80, 25, 0)
			if placement := parseImagePlacement(normalized.Query); !test.check(placement) {
				t.Fatalf("placement changed from %q to %q", test.query, normalized.Query)
			}
		})
	}
}

func TestNormalizedLongHeadingRemainsVisibleOnConversionPage(t *testing.T) {
	for _, test := range []struct {
		name  string
		text  string
		query string
	}{
		{name: "bottom", text: "Bottom", query: "left_pct=0.100000&top=22"},
		{name: "long from middle", text: "This is a longer sentence converted into a large heading", query: "left_pct=0.100000&top=12"},
	} {
		t.Run(test.name, func(t *testing.T) {
			element := Element{Kind: "heading", Level: 1, Text: test.text, ID: "converted", Query: test.query}
			normalized := normalizedTextKindPlacement(Slide{Elements: []Element{element}}, element, 0, 80, 25, 0)
			pages := exportSlidePages(Slide{Elements: []Element{normalized}}, 0, 1, 80, 25)
			if len(pages) == 0 {
				t.Fatal("conversion rendered no pages")
			}
			visible := false
			for _, line := range pages[0].Lines {
				if line.Element == 0 && len(line.Parts) > 0 {
					visible = true
					break
				}
			}
			if !visible {
				t.Fatalf("converted heading absent from first page: query=%q pages=%d", normalized.Query, len(pages))
			}
		})
	}
}

func TestNormalizeTextKindFindsUnidentifiedLocalElementAfterMasterContent(t *testing.T) {
	deck := Deck{
		Masters: defaultMasterDeck(),
		Slides:  []Slide{{LayoutID: "title", Elements: []Element{{Kind: "text", Text: "Bottom", Query: "left_pct=0.100000&top=22"}}}},
	}
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), deck)
	action := nativeEditorAction{
		Element: 0, Cols: 80, Rows: 25,
		ElementData: &Element{Kind: "heading", Level: 1, Text: "Bottom", Query: "left_pct=0.100000&top=22"},
	}
	body, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/normalize-text-kind", bytes.NewReader(body))
	response := httptest.NewRecorder()
	session.handleNormalizeTextKind(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("normalization status = %d: %s", response.Code, response.Body.String())
	}
	var result nativeEditorTextFit
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Element.ID == "" {
		t.Fatal("normalization did not assign a stable element identity")
	}
	placement := parseImagePlacement(result.Element.Query)
	if placement.top == nil || *placement.top >= 22 {
		t.Fatalf("master-resolved heading was not moved onto the current page: %q", result.Element.Query)
	}
	visible := false
	for _, line := range result.Pages[0].Lines {
		if line.Element > 0 && len(line.Parts) > 0 {
			visible = true
			break
		}
	}
	if !visible {
		t.Fatal("normalized local heading is absent behind master content")
	}
}

func TestNativeEditorRefreshScopeAvoidsFullDeckReloadForElementMutations(t *testing.T) {
	for _, action := range []string{"add-element", "duplicate-element", "paste-elements", "update-element", "delete-element", "delete-selection", "move-element", "update-slide", "set-layout"} {
		if got := nativeEditorRefreshScope(action); got != "slide" {
			t.Fatalf("refresh scope for %q = %q, want slide", action, got)
		}
	}
	if got := nativeEditorRefreshScope("update-slide-notes"); got != "" {
		t.Fatalf("notes refresh scope = %q, want none", got)
	}
	for _, action := range []string{"add-slide", "delete-slide", "undo", "redo", "update-layout"} {
		if got := nativeEditorRefreshScope(action); got != "deck" {
			t.Fatalf("refresh scope for %q = %q, want deck", action, got)
		}
	}
}
