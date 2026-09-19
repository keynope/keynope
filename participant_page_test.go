package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParticipantMarkdownPreservesWelcomePageRendering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldWidth, oldHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldWidth, oldHeight }()
	source, err := os.ReadFile("app/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeckData("app/Welcome.md", source)
	if err != nil {
		t.Fatal(err)
	}
	for index := range deck.Slides {
		// This test covers the legacy Markdown transport adapter specifically.
		// Shared-scene participant projection is tested independently below.
		slide := deck.ResolveSlide(index, false)
		want := exportSlidePages(slide, index, len(deck.Slides), 245, 56)
		data, err := participantSlideMarkdown(deck, index)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parseDeckData("Presentation.md", data)
		if err != nil {
			t.Fatal(err)
		}
		participantSlide := parsed.ResolveSlide(0, false)
		restoreParticipantLayers(&participantSlide)
		got := exportSlidePages(participantSlide, index, len(deck.Slides), 245, 56)
		// Saving canonicalizes element indices; participants cannot select them.
		for _, pages := range [][]exportPage{want, got} {
			for i := range pages {
				for j := range pages[i].Lines {
					pages[i].Lines[j].Element = 0
				}
			}
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got)
		if string(a) != string(b) {
			for pos := 0; pos < min(len(a), len(b)); pos++ {
				if a[pos] != b[pos] {
					t.Fatalf("slide %d render changed at %d: want %s; got %s", index, pos, a[max(0, pos-80):min(len(a), pos+180)], b[max(0, pos-80):min(len(b), pos+180)])
				}
			}
			t.Fatalf("slide %d render length changed", index)
		}
	}
}

func TestParticipantJustifiedTextBox(t *testing.T) {
	oldWidth, oldHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldWidth, oldHeight }()
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	for _, kind := range []string{"text", "heading", "bullet"} {
		t.Run(kind, func(t *testing.T) {
			e := Element{Kind: kind, Level: 2, Text: "AAA AAA AA\nAA A AA\nA A AA", Query: "align=justify&text-box=1&width=60&height=12&top=3&left_pct=0.100000"}
			if kind == "heading" {
				e.Text = "AAA AAA AA AA A AA A A AA"
			}
			e = standardTextElement(e)
			deck := Deck{Slides: []Slide{{Elements: []Element{e}}}}
			data, err := participantSlideMarkdown(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parseDeckData("Presentation.md", data)
			if err != nil {
				t.Fatal(err)
			}
			gotElement := parsed.Slides[0].Elements[0]
			wantRows, _ := json.Marshal(renderElementRows(e, 245))
			gotRows, _ := json.Marshal(renderElementRows(gotElement, 245))
			if string(wantRows) != string(gotRows) {
				t.Fatalf("justified box changed after Markdown roundtrip: %+v\nMD: %s\nwant:%s\ngot:%s", gotElement, data, wantRows, gotRows)
			}
			got, err := participantRenderedDeck(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := exportSlidePages(deck.slideRenderPreview(0, 245, 56), 0, 1, 245, 56)
			wantLines, _ := json.Marshal(want[0].Scene)
			gotLines, _ := json.Marshal(got.Pages[0].Scene)
			if string(wantLines) != string(gotLines) {
				t.Fatalf("participant glyphs/positions differ from presenter's justified box\nwant %s\ngot %s", wantLines, gotLines)
			}
		})
	}
}

func TestParticipantMarkdownContainsOnlyCurrentSlide(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldWidth, oldHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldWidth, oldHeight }()
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	deck := Deck{
		Slides: []Slide{
			{Elements: []Element{{Kind: "text", Text: "PRIVATE OTHER SLIDE"}}},
			{LayoutID: "blank", Notes: "PRIVATE NOTES", Engagement: &EngagementDefinition{Kind: "true-false", Prompt: "PRIVATE QUESTION"}, Elements: []Element{
				{Kind: "text", Text: "A", Query: "font=participant-font"},
				{Kind: "image", Path: "/private/personal/photo.png"},
			}},
		},
		Assets:  map[string]DeckAsset{"unused": {MIME: "image/png", Data: []byte("PRIVATE ASSET")}},
		Masters: MasterDeck{Base: MasterLayout{ID: "base", Slide: Slide{BG: "48;2;12;34;56", BGSet: true}}, Layouts: []MasterLayout{{ID: "blank"}}},
	}
	data, err := participantSlideMarkdown(deck, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "/private/personal", "keynope-masters", "keynope-assets", "keynope-fonts"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("disclosed %q", secret)
		}
	}
	parsed, err := parseDeckData("Presentation.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || !isTrueType(parsed.Slides[0].Elements[0]) || parsed.Slides[0].BG != "48;2;12;34;56" {
		t.Fatalf("lost current slide appearance/font: %+v", parsed)
	}
	if !strings.Contains(string(data), "[IMG]") {
		t.Fatal("missing image placeholder")
	}
	if deck.Slides[1].Notes == "" || deck.Slides[1].Elements[1].Kind != "image" {
		t.Fatal("mutated source deck")
	}
	if _, err := participantSlideMarkdown(deck, 2); err == nil {
		t.Fatal("accepted invalid slide")
	}
	rendered, err := participantRenderedDeck(deck, 1)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(rendered)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "/private/personal", "PRIVATE QUESTION"} {
		if strings.Contains(string(wire), secret) {
			t.Fatalf("rendered payload disclosed %q", secret)
		}
	}
	if len(rendered.Pages) == 0 || rendered.Pages[0].Engagement != nil || rendered.Source != "" {
		t.Fatal("rendered payload contains private data or has no pages")
	}
}
