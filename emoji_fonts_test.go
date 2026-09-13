package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryEmojiHasColourFont(t *testing.T) {
	ensureEmojiData()
	archive, err := zip.NewReader(bytes.NewReader(emojiFontArchive), int64(len(emojiFontArchive)))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(archive.File) - 1; got != len(emojiData.assets) {
		t.Fatalf("fonts=%d, artworks=%d", got, len(emojiData.assets))
	}
	for key := range emojiData.assets {
		data, err := base64.StdEncoding.DecodeString(emojiFontData(key))
		if err != nil || len(data) < 48 || string(data[:4]) != "wOF2" {
			t.Fatalf("missing/invalid colour font %s: %v", key, err)
		}
	}
}

func TestColourFontSequencesAndMetrics(t *testing.T) {
	text := "D😍 👩🏽‍🚀 🇳🇱😍!"
	runs, fonts := trueTypeEmojiData(text)
	if len(runs) != 4 || len(fonts) != 3 {
		t.Fatalf("runs=%+v fonts=%d", runs, len(fonts))
	}
	for i, want := range []string{"😍", "👩🏽‍🚀", "🇳🇱", "😍"} {
		run := runs[i]
		if got := string([]rune(text)[run.Start : run.Start+run.Length]); got != want {
			t.Fatalf("run %d=%q want %q", i, got, want)
		}
	}
	e := Element{Kind: "text", Text: "😍", Query: "render=truetype&ttf-size=100"}
	w, _ := trueTypeBounds(e, 1920, 1080)
	if w != 85 {
		t.Fatalf("square 80px emoji plus 2.5px spacers: got %d", w)
	}
	e.Query += "&ttf-width=50"
	w, _ = trueTypeBounds(e, 1920, 1080)
	if w != 45 {
		t.Fatalf("explicit half-width emoji plus spacers: got %d", w)
	}
	payload := exportTrueTypeElement(e, 45, 120)
	if len(payload.Emojis) != 1 || len(payload.EmojiFonts) != 1 {
		t.Fatal("export missing embedded colour font")
	}
}

func TestLegacyEmojiMigratesToColourFont(t *testing.T) {
	e := standardTextElement(Element{Kind: "text", Text: "😍", Query: "render=text-image&source=bitmap&scale=5&text-size=25&glyph=braille"})
	if !isTrueType(e) || trueTypeSize(e) < 400 || !strings.Contains(e.Query, "glyph=braille") {
		t.Fatalf("migration lost size or treatment: %+v", e)
	}
}

func TestEmojiTintRoundTrip(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "D😍", Query: "render=truetype&tint=%2355aaff"}}}}}
	path := filepath.Join(t.TempDir(), "emoji.md")
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	e := parsed.Slides[0].Elements[0]
	q, _ := url.ParseQuery(e.Query)
	if q.Get("tint") != "#55aaff" || e.Text != "D😍" {
		t.Fatalf("lost emoji tint: %+v", e)
	}
}
