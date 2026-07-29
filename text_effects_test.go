package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestTextGradientUsesAuthoredBounds(t *testing.T) {
	slide := Slide{Elements: []Element{{
		Kind:  "text",
		Query: "gradient-start=%23ff0000&gradient-end=%230000ff&gradient-dir=horizontal",
	}}}
	lines := []Line{{Text: "A B", Role: "body", Row: 3, Col: 5, Element: 0, Query: slide.Elements[0].Query}}
	got := applyTextElementEffects(lines, slide)
	if len(got) != 1 {
		t.Fatalf("line count = %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Text, "\033[38;2;255;0;0mA") {
		t.Fatalf("missing red gradient start: %q", got[0].Text)
	}
	if !strings.Contains(got[0].Text, "\033[38;2;0;0;255mB") {
		t.Fatalf("missing blue gradient end: %q", got[0].Text)
	}
}

func TestTextShadowIsDerivedWithoutCoveringForeground(t *testing.T) {
	query := "shadow=soft&shadow-color=%23112233&shadow-x=1&shadow-y=0"
	slide := Slide{Elements: []Element{{Kind: "text", Query: query}}}
	lines := []Line{{Text: "██", Role: "body", Row: 2, Col: 4, Element: 0, Query: query}}
	got := applyTextElementEffects(lines, slide)
	if len(got) != 2 {
		t.Fatalf("line count = %d, want shadow plus foreground: %#v", len(got), got)
	}
	if got[0].Role != "shadow" || got[1].Role != "body" {
		t.Fatalf("unexpected layer order: %#v", got)
	}
	if !strings.Contains(got[0].Text, "\033[38;2;17;34;51m") {
		t.Fatalf("shadow color missing: %#v", got)
	}
	if stripANSI(got[0].Text) != "░" || got[0].Col != 6 {
		t.Fatalf("shadow should not cover occupied foreground cells: %#v", got)
	}
}

func TestTextShadowDefaultsToWhite(t *testing.T) {
	shadow, ok := parseElementTextShadow("shadow=soft")
	if !ok {
		t.Fatal("shadow was not parsed")
	}
	if shadow.colour != (rgbColour{r: 255, g: 255, b: 255}) {
		t.Fatalf("default shadow colour = %#v, want white", shadow.colour)
	}
}

func TestTextEffectsIgnoreShapes(t *testing.T) {
	query := "shadow=solid&gradient-start=%23ff0000&gradient-end=%230000ff"
	slide := Slide{Elements: []Element{{Kind: "shape", Query: query}}}
	lines := []Line{{Text: "██", Role: "shape", Element: 0, Query: query}}
	got := applyTextElementEffects(lines, slide)
	if len(got) != len(lines) || got[0].Text != lines[0].Text {
		t.Fatalf("shape changed: %#v", got)
	}
}

func TestImageBlocksReceiveGradientAndShadow(t *testing.T) {
	query := "gradient-start=%23ff0000&gradient-end=%230000ff&shadow=solid&shadow-color=%23ffffff&shadow-x=1&shadow-y=1"
	slide := Slide{Elements: []Element{{Kind: "image", Query: query}}}
	lines := []Line{{Text: "\033[38;2;20;30;40m██", Role: "image", Row: 2, Col: 3, Element: 0, Query: query}}
	got := applyTextElementEffects(lines, slide)
	if len(got) != 2 || got[0].Role != "shadow" || got[1].Role != "image" {
		t.Fatalf("image effects were not layered: %#v", got)
	}
	if !strings.Contains(got[0].Text, "\033[38;2;255;255;255m█") {
		t.Fatalf("image shadow is not white: %#v", got)
	}
	if !strings.Contains(got[1].Text, "\033[38;2;255;0;0m█") ||
		!strings.Contains(got[1].Text, "\033[38;2;0;0;255m█") {
		t.Fatalf("image gradient did not replace source colours: %#v", got)
	}
}

func TestTextShadowPaddingReservesFlowSpace(t *testing.T) {
	element := Element{Kind: "text", Query: "shadow=solid&shadow-x=2&shadow-y=1"}
	got := padTextRowsForShadow(element, []string{"AB"})
	if len(got) != 2 || got[0] != "AB  " || got[1] != "    " {
		t.Fatalf("padding = %#v", got)
	}
}

func TestTextEffectsSurviveHTMLExportParts(t *testing.T) {
	query := "gradient-start=%23ff0000&gradient-end=%230000ff&shadow=soft&shadow-color=%23112233&shadow-x=1&shadow-y=1&top=2&left=3"
	slide := Slide{
		FG: "37",
		Elements: []Element{{
			Kind:  "text",
			Text:  "AB",
			Query: query,
		}},
	}
	lines := layout(slide, 40, 12)
	exported := exportLines(lines, slide, 40, 12, 1)

	var sawStart, sawEnd, sawShadow bool
	for _, line := range exported {
		for _, part := range line.Parts {
			if part.Color == "rgb(255,0,0)" {
				sawStart = true
			}
			if part.Color == "rgb(0,0,255)" {
				sawEnd = true
			}
			if line.Role == "shadow" && part.Color == "rgb(17,34,51)" && strings.Contains(part.Text, "░") {
				sawShadow = true
			}
		}
	}
	if !sawStart || !sawEnd || !sawShadow {
		t.Fatalf("export lost text effects: start=%v end=%v shadow=%v lines=%#v", sawStart, sawEnd, sawShadow, exported)
	}
}

func TestTextEffectMetadataRoundTripsThroughMarkdownComments(t *testing.T) {
	comment := "<!-- gradient-start=#ff0000 gradient-end=#0000ff gradient-dir=diagonal shadow=solid shadow-color=#112233 shadow-x=-2 shadow-y=3 -->"
	query, ok := textPlacementComment(comment)
	if !ok {
		t.Fatal("text effect metadata was not recognized")
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"gradient-start": "#ff0000",
		"gradient-end":   "#0000ff",
		"gradient-dir":   "diagonal",
		"shadow":         "solid",
		"shadow-color":   "#112233",
		"shadow-x":       "-2",
		"shadow-y":       "3",
	} {
		if got := values.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	serialized := placementCommentText(query)
	for _, marker := range []string{
		"gradient-start=#ff0000",
		"gradient-end=#0000ff",
		"gradient-dir=diagonal",
		"shadow=solid",
		"shadow-color=#112233",
		"shadow-x=-2",
		"shadow-y=3",
	} {
		if !strings.Contains(serialized, marker) {
			t.Fatalf("serialized metadata lost %q: %q", marker, serialized)
		}
	}
}
