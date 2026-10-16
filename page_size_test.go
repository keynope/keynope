package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPageSizeMetadataOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []*DeckPageSize{nil, {1, 1920, 1080}, {1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		d := Deck{PageSize: size, Slides: []Slide{{Elements: []Element{{ID: "body", Kind: "text", Text: "Page size", Query: "render=truetype"}}}}}
		before := cloneDeck(d)
		data, err := serializeDeck("Page.md", d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Page.md", data)
		if err != nil || !reflect.DeepEqual(loaded.PageSize, size) {
			t.Fatal("page size lost", err, loaded.PageSize)
		}
		if authoredTerminalWidth != 245 || authoredTerminalHeight != 56 {
			t.Fatal("page size changed text grid")
		}
		second, err := serializeDeck("Page.md", loaded)
		if err != nil || !bytes.Equal(data, second) {
			t.Fatal("page metadata is not deterministic", err)
		}
		if !reflect.DeepEqual(d, before) {
			t.Fatal("save mutated source")
		}
		if size == nil && bytes.Contains(data, []byte("keynope-page-size")) {
			t.Fatal("legacy default gained metadata")
		}
		copy := cloneDeck(d)
		if size != nil {
			copy.PageSize.Width++
			if copy.PageSize.Width == d.PageSize.Width {
				t.Fatal("clone aliases page size")
			}
		}
	}
}

func TestPageSizeRejectsAmbiguousMalformedAndOversizedMetadata(t *testing.T) {
	good := "<!-- keynope-page-size version=1 width=1440 height=1080 -->"
	for _, metadata := range []string{
		good + "\n" + good,
		strings.Replace(good, "version=1", "version=2", 1),
		strings.Replace(good, "width=1440", "width=0", 1),
		strings.Replace(good, "width=1440", "width=NaN", 1),
		strings.Replace(good, "width=1440", "width=-1440", 1),
		strings.Replace(good, "width=1440", "width=99999999999999999999999999", 1),
		"<!-- keynope-page-size version=1 width=7680 height=320 -->", // Excessive aspect ratio.
		"<!-- keynope-page-size version=1 width=7680 height=7680 -->",
		"<!-- keynope-page-size version=1 width=1440 width=1920 height=1080 -->",
	} {
		w, h := authoredTerminalWidth, authoredTerminalHeight
		if _, err := parseDeckData("Invalid.md", []byte(metadata+"\n\n# Text")); err == nil {
			t.Fatal("accepted invalid page metadata", metadata)
		}
		if authoredTerminalWidth != w || authoredTerminalHeight != h {
			t.Fatal("failed load changed grid")
		}
	}
	loaded, err := parseDeckData("Fenced.md", []byte("```html\n"+good+"\n```\n"))
	if err != nil || loaded.PageSize != nil {
		t.Fatal("interpreted fenced example as page metadata", err)
	}
	for _, size := range []*DeckPageSize{{1, 0, 1080}, {2, 1920, 1080}, {1, 7680, 7680}} {
		data, err := serializeDeck("Invalid.md", Deck{PageSize: size})
		if err == nil || data != nil {
			t.Fatal("save accepted invalid page size", err)
		}
	}
}

func TestPageSizeJSONRejectsDuplicateAliasesWithoutReplacingReceiver(t *testing.T) {
	for _, data := range []string{`{"version":1,"width":1440,"width":1920,"height":1080}`, `{"version":1,"width":1440,"Width":1920,"height":1080}`, `{"version":1,"width":1.5,"height":1080}`} {
		size := DeckPageSize{1, 1920, 1080}
		if err := json.Unmarshal([]byte(data), &size); err == nil || size != (DeckPageSize{1, 1920, 1080}) {
			t.Fatal("invalid JSON changed page size", err, size)
		}
	}
}

func TestPageSizeCommandHistoryAndInputOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("Page.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "body", Kind: "text", Text: "Unchanged"}}}}})
		s.masterMode = master
		before := cloneDeck(s.deck)
		size := &DeckPageSize{1, 1440, 1080}
		if err := s.apply(nativeEditorAction{Action: "set-page-size", PageSize: size}); err != nil {
			t.Fatal(err)
		}
		if len(s.undo) != 1 || !reflect.DeepEqual(s.deck.PageSize, size) {
			t.Fatal("page size was not one transaction")
		}
		size.Width = 1920
		if s.deck.PageSize.Width != 1440 {
			t.Fatal("command retained caller pointer")
		}
		if err := s.apply(nativeEditorAction{Action: "set-page-size", PageSize: s.deck.PageSize.Clone()}); err != nil || len(s.undo) != 1 {
			t.Fatal("no-op added history", err)
		}
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-page-size", PageSize: &DeckPageSize{1, 0, 1080}})
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("undo changed content", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || s.deck.PageSize.Width != 1440 {
			t.Fatal("redo lost size", err)
		}
		if err := s.apply(nativeEditorAction{Action: "set-page-size"}); err != nil || s.deck.PageSize != nil {
			t.Fatal("reset failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || s.deck.PageSize.Width != 1440 {
			t.Fatal("reset undo failed", err)
		}
	}
}
