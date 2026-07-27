package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDeckFont(id string) DeckFont {
	font := defaultEditableDeckFont()
	font.ID = id
	font.Name = "Test Face"
	font.Normal["A"] = []string{"###", "#.#", "#.#", "###", "#.#", "#.#", "#.#", "..."}
	font.Bold["A"] = []string{"#####", "##.##", "##.##", "#####", "##.##", "##.##", "##.##", "....."}
	font.Normal["I"] = []string{"#", "#", "#", "#", "#", "#", "#", "."}
	font.Normal["W"] = []string{"#####", "#####", "#####", "#####", "#####", "#####", "#####", "....."}
	return font
}

func TestDeckFontsMetadataRoundTrip(t *testing.T) {
	font := testDeckFont("arcade")
	metadata, err := encodeDeckFonts(map[string]DeckFont{font.ID: font})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(metadata, "<!-- keynope-fonts version=1 base64:") {
		t.Fatalf("unexpected metadata: %q", metadata)
	}
	fonts, remaining, err := decodeDeckFonts(metadata + "\n\n# Slide\n")
	if err != nil {
		t.Fatal(err)
	}
	if remaining != "# Slide\n" {
		t.Fatalf("metadata was not removed cleanly: %q", remaining)
	}
	if got := fonts["arcade"].Normal["A"][0]; got != "###" {
		t.Fatalf("normal A was %q", got)
	}
	if got := fonts["arcade"].Bold["A"][0]; got != "#####" {
		t.Fatalf("bold A was %q", got)
	}
}

func TestCustomFontUsesVariableGlyphWidths(t *testing.T) {
	font, err := normalizeDeckFont(testDeckFont("variable"))
	if err != nil {
		t.Fatal(err)
	}
	face := compileDeckFontFace(font.Normal)
	mask := deckFontTextMask("IW", face)
	if len(mask) != 8 || len(mask[0]) != 6 {
		t.Fatalf("mask dimensions were %dx%d, want 6x8", len(mask[0]), len(mask))
	}
}

func TestElementFontRendersThroughSharedTextImagePath(t *testing.T) {
	font := testDeckFont("arcade")
	registerDeckFonts(map[string]DeckFont{font.ID: font})
	element := Element{Kind: "text", Text: "A", Query: "font=arcade"}
	rows := renderElementRows(element, 40)
	if len(rows) == 0 || maxLineDisplayWidth(rows) != 2 {
		t.Fatalf("custom three-pixel A rendered as %#v", rows)
	}
}

func TestDeckFontLibraryUsesCompressedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
	font := testDeckFont("library-face")
	if err := storeDeckFontInLibrary(font); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".keynope", "fonts", "library-face.json.gz")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("compressed library font was not written: %v", err)
	}
	loaded := loadDeckFontLibrary()
	if got := loaded["library-face"].Name; got != font.Name {
		t.Fatalf("loaded library font name = %q, want %q", got, font.Name)
	}
	if err := removeDeckFontFromLibrary("library-face"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("library font still exists: %v", err)
	}
}
