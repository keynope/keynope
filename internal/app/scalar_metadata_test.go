package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestCanvasMetadataBoundaries(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {245, 56}, {4096, 256}, {256, 4096}, {1024, 1024}} {
		text := fmt.Sprintf("<!-- keynope width=%d height=%d -->\nText", size[0], size[1])
		width, height, remaining, err := decodeCanvasMetadata(text)
		if err != nil || width != size[0] || height != size[1] || remaining != "\nText" {
			t.Fatal("valid canvas rejected", size, remaining, err)
		}
	}
	for _, text := range []string{
		"<!-- keynope width=0 height=56 -->", "<!-- keynope width=245 height=0 -->",
		"<!-- keynope width=4097 height=1 -->", "<!-- keynope width=4096 height=257 -->",
		"<!-- keynope width=-1 height=56 -->", "<!-- keynope width=no height=56 -->",
		"<!-- keynope width=" + strings.Repeat("9", 100) + " height=56 -->",
		"<!-- keynope width=" + strings.Repeat("9", 300) + " height=56 -->",
		"<!-- keynope width=245 -->", "<!-- keynope -->",
		"<!-- keynope width=245 height=56 -->\n<!-- keynope width=100 height=80 -->",
	} {
		if _, _, remaining, err := decodeCanvasMetadata(text); err == nil || remaining != text {
			t.Fatal("bad canvas accepted or consumed", text, err)
		}
	}
	unknown := "<!-- keynope-canvas-extra future=true -->\nText"
	if width, height, remaining, err := decodeCanvasMetadata(unknown); err != nil || width != 0 || height != 0 || remaining != unknown {
		t.Fatal("unrelated namespace changed", err)
	}
}

func TestScalarMetadataRejectedLoadPreservesRuntime(t *testing.T) {
	oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
	}()
	for _, metadata := range []string{
		"<!-- keynope width=4097 height=56 -->",
		"<!-- keynope width=0 height=56 -->",
		"<!-- keynope width=245 height=56 -->\n<!-- keynope width=245 height=56 -->",
		"<!-- keynope-activity-qr=on -->\n<!-- keynope-activity-qr=off -->",
		"<!-- keynope-activity-qr=maybe -->",
		"<!-- keynope-activity-qr -->",
		"<!-- keynope-slide-id=one -->\n<!-- keynope-slide-id=two -->",
		"<!-- keynope-slide-id=invalid space -->",
		"<!-- layout=one -->\n<!-- layout=two -->",
		"<!-- layout=invalid space -->",
		"<!-- keynope-tab=one -->\n<!-- keynope-tab=two -->",
		"<!-- keynope-tab=invalid space -->",
		"<!-- page-number=show -->\n<!-- page-number=hide -->",
		"<!-- page-number=maybe -->",
	} {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
		if _, err := parseEditorDeckData("bad.md", []byte(metadata+"\nText")); err == nil || authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged {
			t.Fatal("rejected scalar changed active state", metadata, err)
		}
	}
	authoredTerminalWidth, authoredTerminalHeight = 4097, 56
	if _, err := serializeDeck("bad.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Keep"}}}}}); err == nil {
		t.Fatal("save emitted an unloadable canvas")
	}
}

func TestScalarSettingsDistinctOwnersAndCode(t *testing.T) {
	oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
	}()
	code := "<!-- keynope width=999999 height=999999 -->\n<!-- keynope-activity-qr=maybe -->\n<!-- keynope-slide-id=bad space -->\n<!-- layout=invalid space -->\n<!-- keynope-tab=bad space -->\n<!-- page-number=maybe -->"
	text := "<!-- keynope width=245 height=56 -->\n<!-- keynope-activity-qr=off -->\n" + strings.Repeat("\n", 300) +
		"<!-- keynope-slide-id=first -->\n<!-- layout=blank -->\n<!-- page-number=hide -->\n```\n" + code + "\n```\n---\n" +
		"<!-- keynope-slide-id=second -->\n<!-- layout=blank -->\n<!-- page-number=show -->\nText"
	deck, err := parseDeckData("scalars.md", []byte(text))
	if err != nil || len(deck.Slides) != 2 || !deck.HideActivityQR || deck.Slides[0].ID != "first" || deck.Slides[1].ID != "second" || deck.Slides[0].Elements[0].Text != code {
		t.Fatal("distinct scalar owners or fenced literals changed", err)
	}
	encoded, err := serializeDeck("scalars.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("scalars.md", encoded)
	if err != nil || len(opened.Slides) != 2 || !opened.HideActivityQR || opened.Slides[0].Elements[0].Text != code {
		t.Fatal("scalar metadata round trip", err)
	}
}
