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
		if !isTrueType(got) || got.Text != e.Text || q.Has("glyph") || trueTypeSize(got) != 123 || trueTypeWidthPercent(got) != 67 {
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
	for _, retired := range []string{"function openFontEditor", "function canvasStyleSelect", "function canvasFontSelect", "keynope-font-library-v1", ".keynope-font-editor {", "choose('Text rendering'", "Text rendering style"} {
		if strings.Contains(exportHTMLSuffix(), retired) {
			t.Fatalf("retired glyph editor remains: %s", retired)
		}
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
		{Kind: "image", Text: "Artwork", Query: "glyph=blocks&scale=2"},
	} {
		if got := standardTextElement(e); got.Query != e.Query {
			t.Fatalf("artwork changed: %+v", got)
		}
	}
}

func TestRetiredGlyphTextMigratesWithoutLosingContentOrPlacement(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code", "text-image"} {
		original := Element{ID: "stable", Kind: kind, Level: 2, Text: "Literal ****\n[color=#55aa00]content[/color]", Query: "font=custom&glyph=blocks&render=text-image&source=bitmap&scale=2&text-size=10&ttf-size=123&top=4&width=77&fg=%2355aa00&group=g"}
		got := standardTextElement(original)
		q, _ := url.ParseQuery(got.Query)
		for _, retired := range []string{"font", "glyph", "source", "scale", "text-size"} {
			if q.Has(retired) {
				t.Fatalf("%s retained %s", kind, retired)
			}
		}
		if got.ID != original.ID || got.Text != original.Text || got.Level != original.Level || !isTrueType(got) || q.Get("top") != "4" || q.Get("width") != "77" || q.Get("group") != "g" || q.Get("fg") != "#55aa00" || trueTypeSize(got) != 123 {
			t.Fatalf("migration changed content or independent metadata: %+v", got)
		}
		if standardTextElement(got) != got {
			t.Fatal("migration is not idempotent")
		}
	}
}

func TestRetiredFontPayloadIsDiscardedWithoutDecoding(t *testing.T) {
	// Valid base64 but deliberately not a gzip stream. Retired font artwork
	// must not prevent the actual document text from opening.
	deck, err := parseDeckData("legacy.md", []byte("<!-- keynope-fonts version=1 base64:YWJj -->\n\n<!-- font=old glyph=blocks -->\nMy preserved text\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Text != "My preserved text" || !isTrueType(deck.Slides[0].Elements[0]) {
		t.Fatalf("retired payload interfered with migration: %+v", deck.Slides)
	}
	if len(deck.Diagnostics) != 1 || deck.Diagnostics[0].Code != "retired-font-data" {
		t.Fatalf("retired payload was not diagnosed: %#v", deck.Diagnostics)
	}
	encoded, err := serializeDeck("legacy.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "keynope-fonts") || strings.Contains(string(encoded), "font=old") || strings.Contains(string(encoded), "glyph=blocks") {
		t.Fatal("retired font data returned during save")
	}
}

func TestLegacyEmojiMigrationKeepsExplicitTrueTypeSize(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, value := range []string{"1", "97", "193", "512"} {
		original := Element{Kind: "text-image", Text: "😍", ID: "emoji", Query: "render=text-image&source=bitmap&scale=5&ttf-size=" + value}
		got := standardTextElement(original)
		q, _ := url.ParseQuery(got.Query)
		if q.Get("ttf-size") != value {
			t.Fatalf("explicit size %s replaced with %s", value, q.Get("ttf-size"))
		}
		if standardTextElement(got) != got {
			t.Fatal("repeat migration changed the emoji")
		}
		loaded, err := parseDeckData("Legacy.md", []byte("<!-- render=text-image source=bitmap scale=5 ttf-size="+value+" -->\n😍\n"))
		if err != nil {
			t.Fatal(err)
		}
		session := newNativeEditorSession("Legacy.md", loaded)
		if trueTypeSize(session.deck.Slides[0].Elements[0]) != trueTypeSize(got) {
			t.Fatal("opening legacy deck replaced explicit emoji size")
		}
		data, err := serializeDeck("Emoji.md", Deck{Slides: []Slide{{Elements: []Element{got}}}})
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseDeckData("Emoji.md", data)
		if err != nil {
			t.Fatal(err)
		}
		if trueTypeSize(reopened.Slides[0].Elements[0]) != trueTypeSize(got) {
			t.Fatal("saved emoji size changed")
		}
	}
}

func TestLegacyBitmapTextScaleMigration(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, tc := range []struct{ scale, size string }{
		{"1", "96"}, {"2", "193"}, {"4", "386"}, {"5", "482"}, {"1e300", "512"},
	} {
		for _, kind := range []string{"text", "heading", "bullet", "code", "text-image"} {
			e := Element{Kind: kind, Level: 1, Text: "Sample", Query: "render=text-image&scale=" + tc.scale + "&top=4&fg=%23ff5500"}
			got := standardTextElement(e)
			q, _ := url.ParseQuery(got.Query)
			if q.Get("ttf-size") != tc.size || q.Get("top") != "4" || q.Get("fg") != "#ff5500" {
				t.Fatalf("%s scale=%s: %s", kind, tc.scale, got.Query)
			}
			if standardTextElement(got) != got {
				t.Fatal("non-idempotent scale migration")
			}
			data, err := serializeDeck("Scaled.md", Deck{Slides: []Slide{{Elements: []Element{got}}}})
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Scaled.md", data)
			if err != nil {
				t.Fatal(err)
			}
			rq, _ := url.ParseQuery(reopened.Slides[0].Elements[0].Query)
			if rq.Get("ttf-size") != tc.size {
				t.Fatalf("saved size changed: %s", rq.Encode())
			}
		}
	}
	for _, query := range []string{"render=text-image", "render=text-image&scale=NaN", "render=text-image&scale=+Inf", "render=text-image&scale=-1", "render=truetype&scale=4"} {
		got := standardTextElement(Element{Kind: "heading", Level: 2, Text: "Inherited", Query: query})
		q, _ := url.ParseQuery(got.Query)
		if q.Has("ttf-size") {
			t.Fatalf("implicit or invalid scale became explicit: %s", got.Query)
		}
	}
}

func TestLegacyScaleUsesLoadedDeckDimensions(t *testing.T) {
	useTestAuthoredSize(t, 999, 999)
	for _, tc := range []struct {
		header string
		size   int
	}{
		{"<!-- keynope width=245 height=56 -->", 193},
		{"<!-- keynope width=245 height=112 -->", 96},
		{"", 193}, // No header must not inherit the preceding deck's dimensions.
		{"<!-- keynope width=80 height=25 -->", 432},
	} {
		deck, err := parseDeckData("Scale.md", []byte(tc.header+"\n<!-- render=text-image scale=2 -->\nExample\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := trueTypeSize(deck.Slides[0].Elements[0]); got != tc.size {
			t.Fatalf("header %q: size %d, want %d", tc.header, got, tc.size)
		}
	}
}

func TestLegacyEmojiScaleCannotOverflow(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, tc := range []struct {
		scale string
		size  int
	}{
		{"1e300", 512}, {"NaN", 96}, {"%2BInf", 96}, {"-1", 96},
	} {
		e := standardTextElement(Element{Kind: "text-image", Text: "😍", Query: "render=text-image&scale=" + tc.scale})
		if got := trueTypeSize(e); got != tc.size {
			t.Fatalf("scale %s: %d, want %d", tc.scale, got, tc.size)
		}
	}
}
