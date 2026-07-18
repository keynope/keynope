package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestNativeEditorUploadUsesVisibleImagePlacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{Kind: "text", Text: "Before"},
		{Kind: "text", Text: "After"},
	}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("image bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	session.handleUpload(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", response.Code, response.Body.String())
	}

	state := session.state()
	if len(state.Slides[0].Elements) != 3 || state.Selected != 1 {
		t.Fatalf("uploaded image placement in element list = %#v", state)
	}
	image := state.Slides[0].Elements[1]
	if image.Kind != "image" || filepath.Base(image.Path) != "photo.png" {
		t.Fatalf("uploaded element = %#v", image)
	}
	if image.Query != "left=1&scale=1.0&top=1" {
		t.Fatalf("uploaded image query = %q", image.Query)
	}
	if _, err := os.Stat(image.Path); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Elements; len(got) != 2 {
		t.Fatalf("single undo did not remove upload: %#v", got)
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{}}})
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Selected"}}}}})
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{}}})
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Notes: "first"}, {Notes: "second"}}})
	if err := session.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 1, Notes: "updated"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 0 || state.Slides[0].Notes != "first" || state.Slides[1].Notes != "updated" {
		t.Fatalf("unexpected notes state: current=%d notes=%q/%q", state.Current, state.Slides[0].Notes, state.Slides[1].Notes)
	}
}

func TestNativeEditorPreviewRendersWithoutMutatingDeck(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Original"}}}}})
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

func TestNativeEditorInlinePreviewReturnsExactBulletCaret(t *testing.T) {
	element := Element{Kind: "bullet", Text: "First\nSecond  "}
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{element}}}})
	action := nativeEditorAction{Name: "inline-edit", Element: 0, ElementData: &element, Cursor: len([]rune(element.Text)), Cols: 80, Rows: 40}
	body, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/preview", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	session.handlePreview(recorder, request)
	var preview nativeEditorInlinePreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || len(preview.Pages) == 0 {
		t.Fatalf("inline preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if preview.Caret.Row != 10 || preview.Caret.Col != 40 || preview.Caret.Cells != 4 {
		t.Fatalf("inline bullet caret = %#v, want row 10 col 40 cells 4", preview.Caret)
	}
}

func TestNativeEditorInlinePreviewReturnsSelectionBackgroundRows(t *testing.T) {
	element := Element{Kind: "text", Text: "Select me"}
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{element}}}})
	action := nativeEditorAction{Name: "inline-edit", Element: 0, ElementData: &element, Cursor: 6, SelectionStart: 0, SelectionEnd: 6, Cols: 80, Rows: 40}
	body, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/preview", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	session.handlePreview(recorder, request)
	var preview nativeEditorInlinePreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.SelectionRows) != 4 {
		t.Fatalf("selection background rows=%#v, want four glyph rows", preview.SelectionRows)
	}
	for _, row := range preview.SelectionRows {
		if row.Cells != 24 {
			t.Fatalf("selection row=%#v, want six four-cell glyphs", row)
		}
	}
}

func TestNativeEditorSelectionBackgroundUsesBoldGlyphWidth(t *testing.T) {
	element := Element{Kind: "text", Text: "A**BB**C"}
	selectionStart := len([]rune("A**"))
	selectionEnd := len([]rune("A**BB"))
	slide := Slide{Elements: []Element{element}}
	start := editorCaretForElement(slide, 0, selectionStart, 80, 40, 0)
	end := editorCaretForElement(slide, 0, selectionEnd, 80, 40, 0)
	rows := editorSelectionRows(slide, 0, start, end, 80, 40, 0)
	if len(rows) != 4 {
		t.Fatalf("bold selection background rows=%#v, want four glyph rows", rows)
	}
	for _, row := range rows {
		if row.Cells != 10 {
			t.Fatalf("bold selection row=%#v, want two five-cell bold glyphs", row)
		}
	}
}

func TestSelectionBackgroundKeepsOffsetsAcrossBoldMarkers(t *testing.T) {
	element := Element{Kind: "text", Text: "A **BB** C"}
	slide := Slide{Elements: []Element{element}}
	assertSelectionWidth := func(startText, endText string, want int) {
		t.Helper()
		start := editorCaretForElement(slide, 0, len([]rune(startText)), 80, 40, 0)
		end := editorCaretForElement(slide, 0, len([]rune(endText)), 80, 40, 0)
		if got := end.Col - start.Col; got != want {
			t.Fatalf("selection %q..%q width=%d, want %d", startText, endText, got, want)
		}
		for _, row := range editorSelectionRows(slide, 0, start, end, 80, 40, 0) {
			if row.Cells != want {
				t.Fatalf("selection %q..%q row=%#v, want %d cells", startText, endText, row, want)
			}
		}
	}
	assertSelectionWidth("A **", "A **BB", 10)
	assertSelectionWidth("A **", "A **BB** C", 18)
}

func TestSelectionBackgroundStartsAfterUnselectedWordSpace(t *testing.T) {
	element := Element{Kind: "text", Text: "Dit is een test"}
	startCursor := len([]rune("Dit is een "))
	endCursor := len([]rune(element.Text))
	slide := Slide{Elements: []Element{element}}
	start := editorCaretForElement(slide, 0, startCursor, 80, 40, 0)
	end := editorCaretForElement(slide, 0, endCursor, 80, 40, 0)
	if got := end.Col - start.Col; got != 16 {
		t.Fatalf("selected word width=%d, want four four-cell glyphs without the preceding space", got)
	}
	for _, row := range editorSelectionRows(slide, 0, start, end, 80, 40, 0) {
		if row.Cells != 16 {
			t.Fatalf("selection row=%#v includes the preceding unselected space", row)
		}
	}
}

func TestResizedStyledTextCaretStaysAtInsertionPointInsideMarkers(t *testing.T) {
	element := Element{Kind: "text", Text: "**Large text**", Query: "render=text-image&source=bitmap&scale=2.00&text-size=10"}
	cursor := len([]rune("**Large text"))
	caret := editorCaretForElement(Slide{Elements: []Element{element}}, 0, cursor, 120, 50, 0)
	lines := displayLines(Slide{Elements: []Element{element}}, 120, 50, 0)
	if len(lines) == 0 {
		t.Fatal("resized styled text rendered no lines")
	}
	if caret.Col <= lines[0].Col {
		t.Fatalf("resized styled caret jumped to element start: caret=%#v first line col=%d", caret, lines[0].Col)
	}
}

func TestResizedEmojiCaretIncludesSideSpacers(t *testing.T) {
	element := Element{Kind: "text", Text: "D😀", Query: "render=text-image&source=bitmap&scale=2.00&text-size=10"}
	caret := editorCaretForElement(Slide{Elements: []Element{element}}, 0, len([]rune(element.Text)), 120, 50, 0)
	lines := displayLines(Slide{Elements: []Element{element}}, 120, 50, 0)
	if len(lines) == 0 {
		t.Fatal("resized emoji text rendered no lines")
	}
	if advance := caret.Col - lines[0].Col; advance != 26 {
		t.Fatalf("caret advance after D + emoji=%d, want 26 (8 + 1 + 16 + 1)", advance)
	}
	beforeEmoji := editorCaretForElement(Slide{Elements: []Element{element}}, 0, 1, 120, 50, 0)
	if beforeEmoji.Cells != 18 {
		t.Fatalf("emoji caret width=%d, want 18 including both side spacers", beforeEmoji.Cells)
	}
}

func TestDefaultTextCaretIncludesEveryEmojiAndSpacer(t *testing.T) {
	element := Element{Kind: "text", Text: "D😀🥰X"}
	slide := Slide{Elements: []Element{element}}
	lines := displayLines(slide, 120, 50, 0)
	if len(lines) == 0 {
		t.Fatal("emoji text rendered no lines")
	}
	assertAdvance := func(cursor, want int) {
		t.Helper()
		caret := editorCaretForElement(slide, 0, cursor, 120, 50, 0)
		if got := caret.Col - lines[0].Col; got != want {
			t.Fatalf("caret at rune %d advances %d cells, want %d", cursor, got, want)
		}
	}
	assertAdvance(1, 4)
	assertAdvance(2, 14)
	assertAdvance(3, 24)
	assertAdvance(4, 28)
	beforeEmoji := editorCaretForElement(slide, 0, 1, 120, 50, 0)
	if beforeEmoji.Cells != 10 {
		t.Fatalf("default emoji caret width=%d, want 10 including side spacers", beforeEmoji.Cells)
	}
}

func TestBulletEmojiCaretAndSelectionUseRenderedWidth(t *testing.T) {
	element := Element{Kind: "bullet", Text: "A[color=#55aaff]😀[/color]B"}
	slide := Slide{Elements: []Element{element}}
	startCursor := len([]rune("A[color=#55aaff]"))
	endCursor := startCursor + len([]rune("😀"))
	start := editorCaretForElement(slide, 0, startCursor, 80, 40, 0)
	end := editorCaretForElement(slide, 0, endCursor, 80, 40, 0)
	if got := end.Col - start.Col; got != 10 {
		t.Fatalf("bullet emoji caret advance=%d, want rendered emoji width 10", got)
	}
	if start.Cells != 10 {
		t.Fatalf("bullet emoji caret underline=%d, want rendered emoji width 10", start.Cells)
	}
	selection := editorSelectionRows(slide, 0, start, end, 80, 40, 0)
	if len(selection) != 4 {
		t.Fatalf("bullet emoji selection rows=%#v, want four glyph rows", selection)
	}
	for _, row := range selection {
		if row.Cells != 10 {
			t.Fatalf("bullet emoji selection row=%#v, want rendered emoji width 10", row)
		}
	}
}

func TestResizedColouredTextCaretIgnoresMarkupWidth(t *testing.T) {
	element := Element{Kind: "text", Text: "[color=#55aaff]Blue[/color]", Query: "render=text-image&source=bitmap&scale=2.00&text-size=10"}
	cursor := len([]rune("[color=#55aaff]Blue"))
	caret := editorCaretForElement(Slide{Elements: []Element{element}}, 0, cursor, 120, 50, 0)
	lines := displayLines(Slide{Elements: []Element{element}}, 120, 50, 0)
	if len(lines) == 0 || caret.Col-lines[0].Col != 32 {
		t.Fatalf("coloured resized caret=%#v lines=%#v, want four eight-cell glyphs", caret, lines)
	}
}

func TestNativeEditorFitsLargestTextSizeInsideDragBox(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Fit"}}}}})
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
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Masters: masters, Slides: []Slide{{LayoutID: "blank"}}})
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

func TestNativeEditorClonesMasterImmediatelyBelowSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "title"}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: 2}); err != nil {
		t.Fatal(err)
	}
	sourceID := session.state().Masters.Layouts[1].ID
	if err := session.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 3 {
		t.Fatalf("selected master after clone = %d, want 3", state.Current)
	}
	if len(state.Masters.Layouts) != 5 || state.Masters.Layouts[1].ID != sourceID {
		t.Fatalf("master order after clone = %#v", state.Masters.Layouts)
	}
	clone := state.Masters.Layouts[2]
	if clone.ID == sourceID || clone.Name != "Title Copy" {
		t.Fatalf("master inserted below source = %#v", clone)
	}
	if state.Masters.Layouts[3].Name != "Title + Subtitle" {
		t.Fatalf("following master was displaced incorrectly: %#v", state.Masters.Layouts)
	}
}

func TestNativeEditorReordersMasterLayoutsAndKeepsMovedMasterSelected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	movedID := session.state().Masters.Layouts[2].ID
	if err := session.apply(nativeEditorAction{Action: "reorder-master", Slide: 3, Value: 1}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 1 || state.Masters.Layouts[0].ID != movedID {
		t.Fatalf("master was not moved and selected: current=%d layouts=%#v", state.Current, state.Masters.Layouts)
	}
	if state.Masters.Layouts[1].Name != "Blank" || state.Masters.Layouts[2].Name != "Title" {
		t.Fatalf("other masters lost their relative order: %#v", state.Masters.Layouts)
	}
	if err := session.apply(nativeEditorAction{Action: "reorder-master", Slide: 0, Value: 2}); err == nil {
		t.Fatal("Base Master was allowed to move")
	}
}

func TestNativeEditorMasterModeTogglesInheritedPageNumberElement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: 1}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Slides[state.Current].PageNumber != "" {
		t.Fatalf("layout did not begin inherited: %#v", state.Slides[state.Current])
	}
	if _, ok := pageNumberElement(state.Resolved[state.Current]); !ok {
		t.Fatal("inherited page number is not active before toggling")
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-page-number"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	slide := state.Slides[state.Current]
	number, ok := pageNumberElement(slide)
	if !ok || slide.PageNumber != pageNumberShow || number.Text != "1" || number.Query != "bottom=1&fg=%23aaaaaa&right=2" {
		t.Fatalf("local page number = %#v, slide mode = %q", number, slide.PageNumber)
	}
	if state.Selected != -1 || len(state.Selection) != 0 {
		t.Fatalf("page-number toggle changed the selection: %#v", state)
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-page-number"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	slide = state.Slides[state.Current]
	if slide.PageNumber != pageNumberHide {
		t.Fatalf("local page number was not turned off: %#v", slide)
	}
	if _, ok := pageNumberElement(state.Resolved[state.Current]); ok {
		t.Fatal("off page number remains in resolved master")
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-page-number"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	slide = state.Slides[state.Current]
	if slide.PageNumber != "" {
		t.Fatalf("off page number did not return to inherited: %#v", slide)
	}
	if _, ok := pageNumberElement(slide); ok {
		t.Fatal("inherited page number was stored as a local element")
	}
	if number, ok := pageNumberElement(state.Resolved[state.Current]); !ok || !number.Inherited {
		t.Fatalf("inherited page number was not restored: %#v", number)
	}
}

func TestNativeEditorBaseMasterPageNumberToggleRemainsBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-page-number"}); err != nil {
		t.Fatal(err)
	}
	if slide := session.state().Slides[0]; slide.PageNumber != pageNumberHide {
		t.Fatalf("Base Master did not turn off: %#v", slide)
	}
	if err := session.apply(nativeEditorAction{Action: "toggle-page-number"}); err != nil {
		t.Fatal(err)
	}
	if slide := session.state().Slides[0]; slide.PageNumber != pageNumberShow {
		t.Fatalf("Base Master did not turn back on: %#v", slide)
	} else if _, ok := pageNumberElement(slide); !ok {
		t.Fatal("Base Master turned on without a page-number element")
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
	front := session.state().Slides[0].Elements[0]
	if front.ID != "one" || elementLayer(front) != "front" {
		t.Fatalf("front element = %#v", front)
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
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: len(state.Masters.Layouts)}); err != nil {
		t.Fatal(err)
	}
	const renamed = "Dennis's_Master - 2"
	if err := session.apply(nativeEditorAction{Action: "rename-master", Name: renamed}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if got := state.Masters.Layouts[len(state.Masters.Layouts)-1].Name; got != renamed {
		t.Fatalf("renamed layout name = %q, want %q", got, renamed)
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
		{name: "vertical alignment", query: "align=right&valign=middle", check: func(placement imagePlacement) bool {
			return placement.align == "right" && placement.verticalAlign == "middle"
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

func TestConvertCodeBlockToHeadingsSplitsLinesAtRenderedOffsets(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "code", Text: "first\n  second", Query: "fg=%2355aaff&left=7&top=3", ID: "code-1"}}}
	selected, err := convertNativeEditorTextKind(&slide, 0, "heading", 1, 80, 25)
	if err != nil {
		t.Fatal(err)
	}
	if selected != 0 || len(slide.Elements) != 2 {
		t.Fatalf("selected=%d elements=%d, want first of two headings", selected, len(slide.Elements))
	}
	for index, want := range []string{"first", "second"} {
		element := slide.Elements[index]
		if element.Kind != "heading" || element.Level != 1 || element.Text != want {
			t.Fatalf("element %d = %#v", index, element)
		}
		values, _ := url.ParseQuery(element.Query)
		if values.Get("header") != "#55aaff" {
			t.Fatalf("element %d lost colour: %q", index, element.Query)
		}
		placement := parseImagePlacement(element.Query)
		if placement.top == nil || placement.leftPct == nil {
			t.Fatalf("element %d lacks explicit line placement: %q", index, element.Query)
		}
	}
	first := parseImagePlacement(slide.Elements[0].Query)
	second := parseImagePlacement(slide.Elements[1].Query)
	if *second.top <= *first.top || *second.leftPct <= *first.leftPct {
		t.Fatalf("line offsets were not preserved: first=%q second=%q", slide.Elements[0].Query, slide.Elements[1].Query)
	}
}

func TestConvertBlockKindTogglesAndCrossConverts(t *testing.T) {
	for _, test := range []struct {
		name       string
		sourceKind string
		targetKind string
		wantKinds  []string
	}{
		{name: "bullet toggle", sourceKind: "bullet", targetKind: "bullet", wantKinds: []string{"text", "text"}},
		{name: "code toggle", sourceKind: "code", targetKind: "code", wantKinds: []string{"text", "text"}},
		{name: "bullet to code", sourceKind: "bullet", targetKind: "code", wantKinds: []string{"code"}},
		{name: "code to bullet", sourceKind: "code", targetKind: "bullet", wantKinds: []string{"bullet"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			slide := Slide{Elements: []Element{{Kind: test.sourceKind, Text: "one\ntwo", Query: "left=2&top=2"}}}
			if _, err := convertNativeEditorTextKind(&slide, 0, test.targetKind, 0, 80, 25); err != nil {
				t.Fatal(err)
			}
			if len(slide.Elements) != len(test.wantKinds) {
				t.Fatalf("elements=%d, want %d", len(slide.Elements), len(test.wantKinds))
			}
			for index, want := range test.wantKinds {
				if slide.Elements[index].Kind != want {
					t.Fatalf("element %d kind=%q, want %q", index, slide.Elements[index].Kind, want)
				}
			}
		})
	}
}

func TestConvertTextKindActionPersistsAsSingleUndoableMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "code", Text: "one\ntwo", Query: "left=2&top=2"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "convert-text-kind", Element: 0, Kind: "text", Cols: 80, Rows: 25}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if len(state.Slides[0].Elements) != 2 || state.Slides[0].Elements[0].Kind != "text" || state.Slides[0].Elements[1].Kind != "text" {
		t.Fatalf("converted state = %#v", state.Slides[0].Elements)
	}
	reloaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Slides[0].Elements) != 2 {
		t.Fatalf("persisted elements=%d, want 2", len(reloaded.Slides[0].Elements))
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if len(state.Slides[0].Elements) != 1 || state.Slides[0].Elements[0].Kind != "code" {
		t.Fatalf("undo state = %#v", state.Slides[0].Elements)
	}
}

func TestNativeEditorBulkModifiersFilterIncompatibleElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{Kind: "text", Text: "First", Query: "top=2&left=2", ID: "first"},
		{Kind: "image", Path: "missing.png", Query: "top=4&left=2", ID: "image"},
		{Kind: "text", Text: "Second", Query: "top=6&left=2", ID: "second"},
	}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	session.selection = map[int]bool{0: true, 1: true, 2: true}
	session.selected = 2
	if err := session.apply(nativeEditorAction{Action: "convert-selected-text-kind", Kind: "heading", Level: 1, Cols: 80, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	byID := map[string]Element{}
	for _, element := range state.Slides[0].Elements {
		byID[element.ID] = element
	}
	if byID["first"].Kind != "heading" || byID["second"].Kind != "heading" {
		t.Fatalf("compatible text elements were not converted: %#v", byID)
	}
	if byID["image"].Kind != "image" {
		t.Fatalf("incompatible image was converted: %#v", byID["image"])
	}
	if len(state.Selection) != 3 {
		t.Fatalf("bulk conversion selection=%v, want all originally selected elements to remain selected", state.Selection)
	}

	indices := make([]int, 0, len(state.Slides[0].Elements))
	updates := make([]Element, 0, len(state.Slides[0].Elements))
	for index, element := range state.Slides[0].Elements {
		indices = append(indices, index)
		values, _ := url.ParseQuery(element.Query)
		values.Set("align", "center")
		element.Query = values.Encode()
		updates = append(updates, element)
	}
	if err := session.apply(nativeEditorAction{Action: "update-elements", ElementIndices: indices, ElementsData: updates}); err != nil {
		t.Fatal(err)
	}
	for _, element := range session.state().Slides[0].Elements {
		values, _ := url.ParseQuery(element.Query)
		if values.Get("align") != "center" {
			t.Fatalf("bulk alignment missed element %#v", element)
		}
	}
}

func TestNativeEditorMutationCanonicalizesOrderAndRemapsSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{Kind: "text", Text: "Lower", Query: "top=12&left=2", ID: "lower"},
		{Kind: "text", Text: "Upper", Query: "top=2&left=2", ID: "upper"},
	}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	session := newNativeEditorSession(path, deck)
	session.selected = 0
	session.selection = map[int]bool{0: true}
	updated := deck.Slides[0].Elements[0]
	updated.Text = "Lower edited"
	if err := session.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &updated}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Slides[0].Elements[0].ID != "upper" || state.Slides[0].Elements[1].ID != "lower" {
		t.Fatalf("mutation did not canonicalize element order: %#v", state.Slides[0].Elements)
	}
	if state.Selected != 1 || !reflect.DeepEqual(state.Selection, []int{1}) {
		t.Fatalf("selection was not remapped after ordering: selected=%d selection=%v", state.Selected, state.Selection)
	}
}

func TestNativeEditorRefreshScopeAvoidsFullDeckReloadForElementMutations(t *testing.T) {
	for _, action := range []string{"add-element", "duplicate-element", "paste-elements", "update-element", "update-elements", "convert-text-kind", "convert-selected-text-kind", "delete-element", "delete-selection", "move-element", "update-slide", "set-layout"} {
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

func TestNativeEditorReportsSlideSaveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "deck.md")
	session := newNativeEditorSession(path, Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Original"}}}}})
	err := session.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: "unsaved"})
	if err == nil || !strings.Contains(err.Error(), "save presentation") {
		t.Fatalf("save error = %v, want surfaced persistence failure", err)
	}
}

func TestNativeEditorReportsMasterSaveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "deck.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Slide"}}}}, Masters: defaultMasterDeck()}
	session := newNativeEditorSession(path, deck)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	err := session.apply(nativeEditorAction{Action: "update-slide", SlideData: &Slide{Elements: []Element{{Kind: "text", Text: "Master"}}}})
	if err == nil || !strings.Contains(err.Error(), "save presentation") {
		t.Fatalf("save error = %v, want surfaced persistence failure", err)
	}
}
