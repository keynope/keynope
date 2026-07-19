package main

import (
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoredEmojiGlyphCatalogCoversUnicodeEmoji17(t *testing.T) {
	ensureEmojiData()
	missing := make([]string, 0)
	fullyQualified := 0
	for _, rawLine := range strings.Split(unicodeEmojiTestData, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "#", 2)
		left := strings.SplitN(fields[0], ";", 2)
		if len(left) != 2 || strings.TrimSpace(left[1]) != "fully-qualified" {
			continue
		}
		sequence := emojiSequenceFromHexFields(strings.Fields(strings.TrimSpace(left[0])))
		fullyQualified++
		if emojiData.assets[emojiAssetKey(sequence)] == nil {
			missing = append(missing, strings.TrimSpace(left[0]))
		}
	}
	if fullyQualified < 3000 {
		t.Fatalf("parsed only %d fully-qualified emoji", fullyQualified)
	}
	if len(missing) != 0 {
		limit := min(12, len(missing))
		t.Fatalf("Noto archive is missing %d Unicode emoji assets; first: %v", len(missing), missing[:limit])
	}
}

func TestStoredEmojiGlyphsUseCanonicalMaximumGrid(t *testing.T) {
	ensureEmojiData()
	if len(emojiData.assets) < 3900 {
		t.Fatalf("stored emoji glyph count = %d, want at least 3900", len(emojiData.assets))
	}
	for key, cells := range emojiData.assets {
		if len(cells) != storedEmojiGlyphWidth*storedEmojiGlyphHeight*storedEmojiCellBytes {
			t.Fatalf("stored emoji %q has %d cell bytes", key, len(cells))
		}
	}
	rows := renderEmojiGlyph(emojiAssetKey("🏁"), storedEmojiGlyphHeight)
	if len(rows) != storedEmojiGlyphHeight || maxLineDisplayWidth(rows) != storedEmojiGlyphWidth {
		t.Fatalf("maximum stored emoji renders at %dx%d, want %dx%d", maxLineDisplayWidth(rows), len(rows), storedEmojiGlyphWidth, storedEmojiGlyphHeight)
	}
}

func TestEmojiSequencesRenderAsOneColoredBlockGlyph(t *testing.T) {
	for _, sequence := range []string{"😀", "🇳🇱", "👨‍👩‍👧‍👦", "👍🏽"} {
		tokens := splitEmojiText("A" + sequence + "B")
		if len(tokens) != 3 || tokens[1].assetKey == "" || tokens[1].text != sequence {
			t.Fatalf("tokens for %q = %#v", sequence, tokens)
		}
		rows := renderQuadRaw(sequence)
		if len(rows) != 4 || !strings.Contains(strings.Join(rows, ""), "\033[38;2;") {
			t.Fatalf("emoji %q did not render as a four-row true-colour glyph: %#v", sequence, rows)
		}
	}
}

func TestNormalTextElementUsesEmojiAwareRenderer(t *testing.T) {
	rows := renderElementRows(Element{Kind: "text", Text: "😀"}, 80)
	joined := strings.Join(rows, "")
	if len(rows) != 4 || !strings.Contains(joined, "\033[38;2;") {
		t.Fatalf("normal text emoji fell through to font fallback: %#v", rows)
	}
	if joined == strings.Join(renderQuad("?"), "") {
		t.Fatal("normal text emoji rendered as a question mark")
	}
}

func TestEmojiGlyphUsesTextSizeScale(t *testing.T) {
	rows := renderBitmapTextImage("😀", 4.0)
	if len(rows) != 16 {
		t.Fatalf("large emoji rows=%d, want 16", len(rows))
	}
	if width := maxLineDisplayWidth(rows); width != 34 {
		t.Fatalf("large emoji width=%d, want 34 including side spacers", width)
	}
}

func TestEmojiTextRenderingReservesOneCellSpacerOnEachSide(t *testing.T) {
	rows := renderQuadRaw("D😀X")
	if width := maxLineDisplayWidth(rows); width != 18 {
		t.Fatalf("D + emoji + X width=%d, want 18 (4 + 1 + 8 + 1 + 4)", width)
	}
	parts := exportANSITextParts(rows[0], 0, "#ffffff", 80)
	firstEmojiCol := -1
	for _, part := range parts {
		if strings.HasPrefix(part.Color, "rgb(") {
			firstEmojiCol = part.Col
			break
		}
	}
	if firstEmojiCol != 5 {
		t.Fatalf("emoji starts at col=%d, want 5 after the D glyph and one spacer", firstEmojiCol)
	}
}

func TestEmojiSpacingParticipatesInWrapping(t *testing.T) {
	chunks := wrapWords("D😀X", 4)
	if len(chunks) != 2 || chunks[0] != "D😀" || chunks[1] != "X" {
		t.Fatalf("space-aware emoji wrapping = %#v, want [D😀 X]", chunks)
	}
}

func TestEmojiSequencesStayAtomicWhileWrappingAndEditing(t *testing.T) {
	family := "👨‍👩‍👧‍👦"
	chunks := fixedRuneChunks(family+family, 2)
	if len(chunks) != 2 || chunks[0] != family || chunks[1] != family {
		t.Fatalf("emoji sequence was split while wrapping: %#v", chunks)
	}
	element := Element{Kind: "code", Text: family + "X"}
	line, col := codeCursorVisualLineCol(element.Text, 12, len([]rune(family)))
	if line != 0 || col != 2 {
		t.Fatalf("caret after family emoji=(%d,%d), want (0,2)", line, col)
	}
	if index := codeCursorIndexForVisualLineCol(element.Text, 12, 0, 2); index != len([]rune(family)) {
		t.Fatalf("visual caret mapped to rune %d, want %d", index, len([]rune(family)))
	}
}

func TestNativeEditorEmojiPickerReturnsASCIIBlockPreviews(t *testing.T) {
	session := newNativeEditorSession("deck.md", Deck{Slides: []Slide{{}}})
	request := httptest.NewRequest(http.MethodGet, "/api/editor/emojis?q=family&limit=8", nil)
	response := httptest.NewRecorder()
	session.handleEmojiCatalog(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("emoji catalog status=%d: %s", response.Code, response.Body.String())
	}
	var payload nativeEditorEmojiCatalog
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Groups) < 8 || len(payload.Items) == 0 {
		t.Fatalf("picker payload groups=%d items=%d", len(payload.Groups), len(payload.Items))
	}
	for _, item := range payload.Items {
		if len(item.Lines) != 8 {
			t.Fatalf("picker item %q rows=%d, want each of the 8 source rows exactly once", item.Name, len(item.Lines))
		}
		for _, line := range item.Lines {
			for _, part := range line.Parts {
				if strings.Contains(part.Text, item.Emoji) {
					t.Fatalf("picker preview leaked Unicode emoji instead of block glyphs: %#v", part)
				}
			}
		}
	}
}

func TestLanczosResamplingIgnoresRGBInTransparentPixels(t *testing.T) {
	makeSource := func(hidden color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 6, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 6; x++ {
				img.SetNRGBA(x, y, hidden)
			}
		}
		for y := 1; y < 5; y++ {
			for x := 1; x < 4; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 210, B: 80, A: 255})
			}
		}
		return img
	}
	black := resizeLanczosRGBA(makeSource(color.NRGBA{}), image.Rect(0, 0, 6, 6), 16, 16)
	magenta := resizeLanczosRGBA(makeSource(color.NRGBA{R: 255, B: 255}), image.Rect(0, 0, 6, 6), 16, 16)
	if len(black) != len(magenta) {
		t.Fatal("resampled image sizes differ")
	}
	for index := range black {
		if black[index] != magenta[index] {
			t.Fatalf("transparent RGB leaked into resampled pixel %d: black=%#v magenta=%#v", index, black[index], magenta[index])
		}
	}
}

func TestTransparentEmojiUsesItsPerCellColours(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "text", Text: "😀", Query: "transparent=1"}}}
	lines := layout(slide, 80, 25)
	cells := transparentShapeCells(lines, 80, 25, slide)
	colours := map[string]bool{}
	for _, row := range cells {
		for _, background := range row {
			colours[background] = true
		}
	}
	if len(colours) < 2 {
		t.Fatalf("transparent emoji collapsed to one text colour: %#v", colours)
	}
	exported := transparentShapeExportLines(lines, 80, 25, slide)
	foundMaskedEdge := false
	for _, line := range exported {
		for _, part := range line.Parts {
			for _, glyph := range part.Text {
				if strings.ContainsRune("▘▝▖▗▀▄▌▐▞▚▛▜▙▟", glyph) {
					foundMaskedEdge = true
				}
			}
		}
	}
	if !foundMaskedEdge {
		t.Fatal("transparent emoji export lost its quarter-block edge mask")
	}
}
