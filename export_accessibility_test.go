package main

import (
	"os"
	"strings"
	"testing"
)

func TestExportProvidesDocumentAndSlideSemantics(t *testing.T) {
	deck := exportDeck{
		Cols:   245,
		Rows:   56,
		Source: `Roadmap & Review.md`,
		Pages: []exportPage{{
			Slide:      0,
			SlideCount: 1,
			PageCount:  1,
			FG:         "#ffffff",
			BG:         "#000000",
			Lines: []exportLine{{
				Role:     "truetype",
				TrueType: &exportTrueType{Text: "Accessible heading"},
			}},
		}},
	}
	document, err := exportProjectedHTML(deck, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		`<title>Roadmap &amp; Review</title>`,
		`<meta name="generator" content="Keynope">`,
		`id="stage" role="region" aria-label="Presentation slide"`,
		`id="presenter-canvas" aria-hidden="true"`,
		`id="slide-semantics" class="keynope-visually-hidden" aria-label="Presentation slide content"`,
		`id="slide-status" class="keynope-visually-hidden" role="status" aria-live="polite" aria-atomic="true"`,
		`function accessiblePageText(page)`,
		`function updateSlideSemantics(page, pageName)`,
		`appendSemanticText(slideSemantics, object.text, object.link);`,
		`appendSemanticImage(slideSemantics, connectorSemanticLabel(object), object.link);`,
		`updateAccessibleSlide(page);`,
	} {
		if !strings.Contains(document, marker) {
			t.Fatalf("export is missing semantic marker %q", marker)
		}
	}
	if strings.Contains(document, `.replace(/[*_\x60]+/g, '')`) {
		t.Fatal("accessible text still strips authored literal asterisks")
	}
}

func TestExportPreservesExplicitTitleWithoutDuplicatingGeneratorMetadata(t *testing.T) {
	preserved := preservedExportHead{
		Title: `<title>Custom title</title>`,
		Metas: []string{`<meta name="description" content="Custom deck">`},
	}
	document, err := exportProjectedHTML(exportDeck{Source: "Ignored.md"}, preserved, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document, `<title>Custom title</title>`) || strings.Contains(document, `<title>Ignored</title>`) {
		t.Fatal("explicit export title was not preserved")
	}
	if strings.Count(document, `name="generator"`) != 1 {
		t.Fatalf("expected one generated metadata tag, got %d", strings.Count(document, `name="generator"`))
	}
	if !strings.Contains(document, `<meta name="description" content="Custom deck">`) {
		t.Fatal("custom metadata was not preserved")
	}
}

func TestGeneratedHeadMetadataIsNotPreservedAsCustomMetadata(t *testing.T) {
	preserved := preservedExportHeadFromHTML(`<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta name="generator" content="Keynope"><meta name="description" content="Keep"></head></html>`)
	if len(preserved.Metas) != 1 || !strings.Contains(preserved.Metas[0], `name="description"`) {
		t.Fatalf("unexpected preserved metadata: %#v", preserved.Metas)
	}
}

func TestExportStructuredAccessibilityFixture(t *testing.T) {
	paint := scenePaint{Color: "#ffffff"}
	text := func(role, value string, level int) *sceneText {
		return &sceneText{Role: role, Level: level, Runs: []sceneRun{{Text: value}}, Family: "KeynopeModern, sans-serif", Size: 48, WidthScale: 1, LineHeight: 1.25}
	}
	page := exportPage{Slide: 0, SlideCount: 1, PageCount: 1, Scene: &slideScene{Version: 1, Width: 1920, Height: 1080, Background: "#000000", Objects: []sceneObject{
		{ID: "heading", Kind: "text", Bounds: sceneRect{Width: 600, Height: 100}, Paint: paint, Text: text("heading", "Overview ****", 2)},
		{ID: "list", Kind: "text", Bounds: sceneRect{Y: 120, Width: 600, Height: 200}, Paint: paint, Text: &sceneText{Role: "bullet", Paragraphs: []sceneParagraph{{Bullet: true, Runs: []sceneRun{{Text: "First item"}}}, {Bullet: true, Runs: []sceneRun{{Text: "Second item"}}}}, Family: "KeynopeModern", Size: 40, WidthScale: 1, LineHeight: 1.25}},
		{ID: "code", Kind: "text", Bounds: sceneRect{Y: 340, Width: 600, Height: 100}, Paint: paint, Text: text("code", "print(42)", 0)},
		{ID: "link", Kind: "text", Bounds: sceneRect{Y: 460, Width: 600, Height: 100}, Paint: paint, Text: text("text", "Documentation", 0), Link: "https://keynope.sh/"},
		{ID: "shape", Kind: "shape", Shape: "triangle", Bounds: sceneRect{X: 800, Width: 200, Height: 200}, Paint: paint},
		{ID: "connector", Kind: "connector", Bounds: sceneRect{X: 800, Y: 220, Width: 300, Height: 40}, Paint: paint, Arrows: "both", Points: []connectorPoint{{X: 800, Y: 240}, {X: 1100, Y: 240}}},
		{ID: "image", Kind: "image", Bounds: sceneRect{X: 1200, Width: 200, Height: 200}, Paint: paint, Media: &sceneMedia{Alt: "Architecture diagram", Width: 1, Height: 1}},
		{ID: "decorative", Kind: "image", Bounds: sceneRect{X: 1450, Width: 200, Height: 200}, Paint: paint, Media: &sceneMedia{Alt: "Decorative secret", Decorative: true, Width: 1, Height: 1}},
	}}}
	document, err := exportProjectedHTML(exportDeck{Cols: 245, Rows: 56, Source: "Accessible.md", Pages: []exportPage{page}}, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"Overview ****", "First item", "Second item", "print(42)", "Architecture diagram", "Decorative secret"} {
		if !strings.Contains(document, marker) {
			t.Fatalf("fixture lost %q", marker)
		}
	}
	if path := os.Getenv("KEYNOPE_ACCESSIBILITY_FIXTURE"); path != "" {
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
