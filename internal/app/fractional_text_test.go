package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fractionalTextFixture() Deck {
	theme := builtinDeckTheme("clean-light")
	theme.Fonts.Heading = ThemeFont{FontID: "sans", Size: 96.25, Width: 98.625, LineHeight: 1.02}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: theme, Slides: []Slide{{ID: "fractional-slide", Elements: []Element{{
		ID: "fractional-title", Kind: "heading", Level: 1, Text: "Agentic Delivery Platform",
		Query: "render=truetype&modern-font=theme&modern-paragraph-before=.02&modern-paragraph-after=.07&left=10&top=8&width=220&height=18",
	}}}}}
}

func fractionalTitleModel(t *testing.T, d Deck) *sceneText {
	t.Helper()
	scene, err := buildMixedSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		if object.ID == "fractional-title" {
			return object.Text
		}
	}
	t.Fatal("fractional title missing")
	return nil
}

func checkFractionalTitle(t *testing.T, d Deck) {
	t.Helper()
	model := fractionalTitleModel(t, d)
	if model.Size != 96.25 || model.WidthScale != .98625 || model.LineHeight != 1.02 || model.ParagraphBefore != .02 || model.ParagraphAfter != .07 {
		t.Fatalf("fractional metrics changed: %+v", model)
	}
	if sceneTextPlain(model) != "Agentic Delivery Platformd" {
		t.Fatalf("edited title was not saved: %q", sceneTextPlain(model))
	}
}

func TestFractionalTextSaveRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession(filepath.Join(t.TempDir(), "Fractional.md"), fractionalTextFixture())
	before := cloneDeck(s.deck)
	model := fractionalTitleModel(t, s.deck)
	runs := append([]sceneRun(nil), model.Runs...)
	runs[len(runs)-1].Text += "d"
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", Slide: 0, ObjectID: "fractional-title", SceneRevision: &revision,
		TextRuns: runs, ModernLineHeight: &model.LineHeight, ModernParagraphBefore: &model.ParagraphBefore, ModernParagraphAfter: &model.ParagraphAfter}); err != nil {
		t.Fatal(err)
	}
	checkFractionalTitle(t, s.deck)
	if !reflect.DeepEqual(s.deck.Theme, before.Theme) {
		t.Fatal("typing changed the inherited theme")
	}
	path := filepath.Join(t.TempDir(), "Saved.md")
	if err := saveDeck(path, s.deck); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	checkFractionalTitle(t, loaded)
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("undo did not restore the original title and inherited metrics")
	}
}

func TestFractionalTextBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_FRACTIONAL_TEXT_BROWSER") != "1" {
		t.Skip("set KEYNOPE_FRACTIONAL_TEXT_BROWSER=1 for fractional typography validation")
	}
	useTestAuthoredSize(t, 245, 56)
	path := filepath.Join(t.TempDir(), "Saved.md")
	t.Setenv("KEYNOPE_FRACTIONAL_SAVED_PATH", path)
	s := newNativeEditorSession("Fractional.md", fractionalTextFixture())
	server := newGeometryBrowserServer(t, s)
	runGeometryBrowser(t, "internal/app/testdata/fractional_text.cjs", server.URL)
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	checkFractionalTitle(t, loaded)
}
