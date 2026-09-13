package main

import (
	"net/url"
	"strings"
	"testing"
)

func useTestAuthoredSize(t *testing.T, w, h int) {
	t.Helper()
	oldW, oldH := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = w, h
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = oldW, oldH })
}

func TestStandardTextInsertionAndLoad(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}}, true)
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		if err := s.apply(nativeEditorAction{Action: "add-element", Kind: kind}); err != nil {
			t.Fatal(err)
		}
		state := s.state()
		e := state.Slides[0].Elements[state.Selected]
		if !isTrueType(e) {
			t.Fatalf("%s did not use standard font: %+v", kind, e)
		}
		if kind == "text" && e.Text != "Text" {
			t.Fatal(e.Text)
		}
	}
	for _, style := range []string{"", "blocks", "braille", "ascii", "dense"} {
		e := standardTextElement(Element{Kind: "text", Text: "Test\nsecond line", Query: "ttf-size=123&ttf-width=67&text-box=1&width=100&height=20&glyph=" + style})
		b, err := serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
		if err != nil {
			t.Fatal(err)
		}
		d, err := parseDeckData("test.md", b)
		if err != nil {
			t.Fatal(err)
		}
		got := d.Slides[0].Elements[0]
		q, _ := url.ParseQuery(got.Query)
		if !isTrueType(got) || got.Text != e.Text || q.Get("glyph") != style || trueTypeSize(got) != 123 || trueTypeWidthPercent(got) != 67 {
			t.Fatalf("lost standard text: %+v", got)
		}
	}
	d, err := parseDeckData("legacy.md", []byte("# Heading\n\nText\n\n- Item\n\n```\ncode\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Slides[0].Elements {
		if !isTrueType(e) {
			t.Fatalf("ordinary Markdown not upgraded: %+v", e)
		}
	}
	if strings.Contains(exportHTMLSuffix(), "Add TrueType text (experimental)") {
		t.Fatal("retired insert button remains")
	}
}

func TestStandardTextLiteralMarkdownRoundTrip(t *testing.T) {
	for _, text := range []string{"---", "![image](missing.png)", "[shape:circle]", "", " Text ", "[color=#ff0000]red[/color]", "**** some text ****", "*text*", "**text**"} {
		e := standardTextElement(Element{Kind: "text", Text: text})
		data, err := serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
		if err != nil {
			t.Fatal(err)
		}
		deck, err := parseDeckData("test.md", data)
		if err != nil {
			t.Fatal(err)
		}
		if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Text != text || deck.Slides[0].Elements[0].Kind != "text" {
			t.Fatalf("literal text %q changed: %+v", text, deck.Slides)
		}
	}
}

func TestStandardTextPreservesArtwork(t *testing.T) {
	for _, e := range []Element{
		{Kind: "text", Text: "QR", Query: "qr=1&render=text-image"},
		{Kind: "text", Text: "Custom", Query: "font=my-font&render=text-image"},
	} {
		if got := standardTextElement(e); got.Query != e.Query {
			t.Fatalf("artwork changed: %+v", got)
		}
	}
}
