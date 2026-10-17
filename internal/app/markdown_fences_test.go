package app

import (
	"regexp"
	"strings"
	"testing"
)

func TestDocumentMetadataUnfencedOffsetsAndRewrite(t *testing.T) {
	pattern := regexp.MustCompile(`(?m)^<!-- owned=([a-z]+) -->\s*`)
	code := "````md\r\n<!-- owned=hidden -->\r\n```\r\n~~~\r\n<!-- owned=alsohidden -->\r\n````\r\n"
	text := "<!-- owned=first -->\r\n" + code + "<!-- owned=last -->\r\nUnicode: 🙂\r\n"
	for _, count := range []int{1, 2, -1} {
		matches := findDocumentMetadata(text, pattern, count)
		want := 2
		if count == 1 {
			want = 1
		}
		if len(matches) != want || matches[0][1] != "first" || want == 2 && matches[1][1] != "last" {
			t.Fatal("fenced matches affected count or capture", count, matches)
		}
	}
	if got := rewriteDocumentMetadata(text, pattern, nil); got != code+"Unicode: 🙂\r\n" {
		t.Fatalf("rewrite damaged literal source: %q", got)
	}
	if got := rewriteDocumentMetadata(code+"<!-- owned=hiddenagain -->\n", pattern, func(raw string) string { return "REPLACED\n" }); got != code+"REPLACED\n" {
		t.Fatal("rewrite offsets or raw spans wrong", got)
	}
	unterminated := "<!-- owned=first -->\n~~~\n<!-- owned=hidden -->\n"
	if got := rewriteDocumentMetadata(unterminated, pattern, nil); got != "~~~\n<!-- owned=hidden -->\n" {
		t.Fatal("unterminated code became metadata", got)
	}
}

func TestGlobalMetadataInCodeIsLiteralAcrossLoadSave(t *testing.T) {
	objects, err := encodeDeckObjects(&DeckObjects{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	theme, err := encodeDeckTheme(builtinDeckThemes()[0])
	if err != nil {
		t.Fatal(err)
	}
	appearance, err := encodeDeckAppearance(&DeckAppearance{Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	assets := compressedEmptyAssetComment(t)
	metadata := strings.Join([]string{
		plainMetadataComment("masters", `{"version":1}`), plainMetadataComment("tabs", `[]`),
		plainMetadataComment("document-extra", `{"future":"literal"}`), objects, theme, appearance, assets,
	}, "\n")
	code := metadata + "\n" + strings.Join([]string{
		"<!-- keynope width=100 height=80 -->", "<!-- keynope-activity-qr=off -->",
		"<!-- keynope-fonts version=9 base64:%%% -->", "<!-- truetype-text broken -->",
		"<!-- keynope-assets version=9 base64:%%% -->", "<!-- keynope-theme version=9 base64:%%% -->",
		"<!-- keynope-appearance broken -->", "---", "Literal 🙂 content",
	}, "\n")
	oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
	}()
	for _, kind := range []string{"```", "````", "~~~"} {
		t.Run(kind, func(t *testing.T) {
			literal := code
			if kind != "```" {
				literal += "\n```\n<!-- keynope-assets version=9 base64:%%% -->"
			}
			text := "<!-- keynope width=245 height=56 -->\n\n" + kind + "md\n" + literal + "\n" + kind + "\n"
			deck, err := parseDeckData("code.md", []byte(text))
			if err != nil {
				t.Fatal(err)
			}
			if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Kind != "code" || deck.Slides[0].Elements[0].Text != literal {
				t.Fatal("global metadata changed code or slide boundaries", deck.Slides)
			}
			if deck.Objects != nil || deck.Theme != nil || deck.Appearance != nil || len(deck.Assets) != 0 || len(deck.Tabs) != 0 || deck.Extra != nil || deck.Masters.Version != 0 || deck.HideActivityQR || len(deck.Diagnostics) != 0 || authoredTerminalWidth != 245 || authoredTerminalHeight != 56 {
				t.Fatal("literal code modified document settings or diagnostics", deck.Diagnostics)
			}
			encoded, err := serializeDeck("code.md", deck)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := parseDeckData("code.md", encoded)
			if err != nil || len(opened.Slides) != 1 || opened.Slides[0].Elements[0].Text != literal {
				t.Fatal("literal code changed on save/reopen", err)
			}
			// Real copies outside the fence must still be interpreted exactly once.
			mixed, err := parseDeckData("mixed.md", []byte(metadata+"\n"+text))
			if err != nil || mixed.Objects == nil || mixed.Theme == nil || mixed.Appearance == nil || mixed.Masters.Version != 1 || mixed.Extra == nil || mixed.Slides[0].Elements[0].Text != literal {
				t.Fatal("literal copies confused real document metadata", err)
			}
		})
	}
}

func compressedEmptyAssetComment(t *testing.T) string {
	t.Helper()
	// Reuse the archive test's gzip helper without generating/importing pixels.
	return strings.Replace(compressedActivityComment(t, []byte(`{}`)), "keynope-engagement-results", "keynope-assets", 1)
}

func TestCodeFenceWriterKeepsNestedExamples(t *testing.T) {
	code := "Example\n```md\n---\n<!-- keynope-element version=9 base64:%%% -->\n```\n~~~~\nDone"
	fence := codeFenceForText(code)
	if len(fence) <= 3 {
		t.Fatal("writer did not choose a longer fence")
	}
	slide, err := parseSlideChecked(fence+"\n"+code+"\n"+fence, ".")
	if err != nil || len(slide.Elements) != 1 || slide.Elements[0].Text != code {
		t.Fatal("nested example did not stay literal", err)
	}
	if err := validateCompatibleElementMetadata(fence + "\n" + code + "\n" + fence); err != nil {
		t.Fatal("compatibility preflight disagreed with parser fences", err)
	}
}

func BenchmarkDocumentMetadataFencedSearch(b *testing.B) {
	text := "<!-- owned=first -->\n" + strings.Repeat("x", 1<<20) + "\n```\n<!-- owned=literal -->\n```\n"
	pattern := regexp.MustCompile(`(?m)^<!-- owned=([a-z]+) -->`)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(findDocumentMetadataIndices(text, pattern, 1)) != 1 {
			b.Fatal("missing header")
		}
	}
}
