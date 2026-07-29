package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
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
	face := compileDeckFontFace(font.Normal, "")
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

func TestBlockCellFontRoundTripAndRendering(t *testing.T) {
	font := defaultEditableDeckCellFont()
	font.ID = "block-cells"
	font.Name = "Block Cells"
	font.Normal["A"] = []string{"▀▄", "▖▗", "░▓", "▌▐", "▔▕", "▘▝", "▚▞", "█."}
	metadata, err := encodeDeckFonts(map[string]DeckFont{font.ID: font})
	if err != nil {
		t.Fatal(err)
	}
	fonts, _, err := decodeDeckFonts(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded := fonts[font.ID]
	if decoded.Mode != deckFontModeCells {
		t.Fatalf("font mode = %q, want %q", decoded.Mode, deckFontModeCells)
	}
	if got := decoded.Normal["A"]; !reflect.DeepEqual(got, font.Normal["A"]) {
		t.Fatalf("block cells changed during round trip: %#v", got)
	}
	face := compileDeckFontFace(decoded.Normal, deckFontModeCells)
	nativeRows := append([]string(nil), font.Normal["A"]...)
	nativeRows[len(nativeRows)-1] = "█ "
	if got := renderScaledDeckCellFont("A", 1, face); !reflect.DeepEqual(got, nativeRows) {
		t.Fatalf("native block rendering = %#v, want %#v", got, nativeRows)
	}
	scaled := renderScaledDeckCellFont("A", 2, face)
	if len(scaled) != 16 || utf8.RuneCountInString(scaled[0]) != 4 {
		t.Fatalf("scaled block rendering is %dx%d, want 4x16", utf8.RuneCountInString(scaled[0]), len(scaled))
	}
	registerDeckFonts(fonts)
	elementRows := renderElementRows(Element{Kind: "text", Text: "A", Query: "font=block-cells"}, 40)
	if len(elementRows) != 4 || maxLineDisplayWidth(elementRows) != 1 {
		t.Fatalf("shared element rendering is %dx%d, want the original 2x8 grid packed to 1x4: %#v", maxLineDisplayWidth(elementRows), len(elementRows), elementRows)
	}
}

func TestParseFIGletFontPreservesLiteralCells(t *testing.T) {
	var source strings.Builder
	source.WriteString("flf2a$ 1 1 1 0 0 0 0\n")
	for code := 32; code <= 126; code++ {
		glyph := string(rune(code))
		if code == 32 {
			glyph = "$"
		}
		if code == 'A' {
			glyph = ".A."
		}
		source.WriteString(glyph + "@\n")
	}
	font, err := parseFIGletFont("Test Figlet", source.String())
	if err != nil {
		t.Fatal(err)
	}
	if font.ID != "test-figlet" || font.Mode != deckFontModeFiglet || font.Height != 1 {
		t.Fatalf("parsed FIGlet identity = %#v", font)
	}
	if got := font.Normal["A"]; !reflect.DeepEqual(got, []string{".A."}) {
		t.Fatalf("FIGlet A = %#v", got)
	}
	font.Bold = cloneDeckFontFace(font.Normal)
	normalized, err := normalizeDeckFont(font)
	if err != nil {
		t.Fatal(err)
	}
	if got := normalized.Normal["A"][0]; got != ".A." {
		t.Fatalf("FIGlet literal dots changed to %q", got)
	}
	face := compileDeckFontFace(normalized.Normal, deckFontModeFiglet)
	if got := renderScaledDeckFigletFont("A", 1, face, normalized.FigletLayout); !reflect.DeepEqual(got, []string{".A."}) {
		t.Fatalf("FIGlet render = %#v", got)
	}

	registerDeckFonts(map[string]DeckFont{normalized.ID: normalized})
	resized := Element{Kind: "text", Text: "A", Query: "font=test-figlet&render=text-image&source=bitmap&scale=2.00&text-size=10"}
	if got := renderElementRows(resized, 40); !reflect.DeepEqual(got, []string{".A."}) {
		t.Fatalf("terminal FIGlet resize changed native glyph size: %#v", got)
	}
	visual := visualFontScaledSlide(Slide{Elements: []Element{resized}})
	if got := renderElementRows(visual.Elements[0], 40); !reflect.DeepEqual(got, []string{"..AA..", "..AA.."}) {
		t.Fatalf("visual FIGlet resize = %#v", got)
	}
}

func TestFontLibraryHasNoBundledAdditionalFont(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
	fonts := loadDeckFontLibrary()
	if len(fonts) != 0 {
		t.Fatalf("font library unexpectedly contains bundled fonts: %#v", fonts)
	}
}
