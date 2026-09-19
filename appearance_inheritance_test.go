package main

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func roundTripAppearanceDeck(t *testing.T, deck Deck) Deck {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inheritance.md")
	data, err := serializeDeck(path, deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func assertElementStyle(t *testing.T, deck Deck, id, want string) {
	t.Helper()
	slide := deck.Slides[0]
	for _, element := range slide.Elements {
		if element.ID == id {
			if got := deck.elementStyle(slide, element); got != want {
				t.Fatalf("%s style = %s, want %s", id, got, want)
			}
			return
		}
	}
	t.Fatalf("missing element %s", id)
}

func TestAppearanceInheritanceRoundTripsEveryAuthoredScope(t *testing.T) {
	deck := Deck{
		Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro", Extra: map[string]json.RawMessage{"future": json.RawMessage(`{"keep":true}`)}},
		Masters: MasterDeck{Version: 1,
			Base:    MasterLayout{ID: "base", Name: "Base", Slide: Slide{DefaultStyle: "modern"}},
			Layouts: []MasterLayout{{ID: "content", Name: "Content", Slide: Slide{DefaultStyle: "retro"}}},
		},
		Slides: []Slide{{LayoutID: "content", DefaultStyle: "modern", Elements: []Element{
			{ID: "group-a", Kind: "text", Text: "A", Query: "group=pair"},
			{ID: "group-b", Kind: "shape", Query: "group=pair&shape=square"},
			{ID: "explicit", Kind: "text", Text: "Explicit", Query: "element-style=modern"},
		}}},
	}

	// Styling one collapsed group member writes one atomic element-level
	// override to every member. Clearing it restores, rather than copies, the
	// receiving slide's inherited value.
	changed, err := applyElementStyle(&deck, nativeEditorAction{Slide: 0, ObjectIDs: []string{"group-a"}, Name: "retro"}, "")
	if err != nil || !changed {
		t.Fatalf("group style: changed=%v err=%v", changed, err)
	}
	deck = roundTripAppearanceDeck(t, deck)
	assertElementStyle(t, deck, "group-a", "retro")
	assertElementStyle(t, deck, "group-b", "retro")
	assertElementStyle(t, deck, "explicit", "modern")

	changed, err = applyElementStyle(&deck, nativeEditorAction{Slide: 0, ObjectIDs: []string{"group-a"}, Name: "inherit"}, "")
	if err != nil || !changed {
		t.Fatalf("clear group style: changed=%v err=%v", changed, err)
	}
	deck = roundTripAppearanceDeck(t, deck)
	assertElementStyle(t, deck, "group-a", "modern") // slide
	assertElementStyle(t, deck, "group-b", "modern")

	deck.Slides[0].DefaultStyle = ""
	deck = roundTripAppearanceDeck(t, deck)
	assertElementStyle(t, deck, "group-a", "retro") // layout

	deck.Masters.Layouts[0].Slide.DefaultStyle = ""
	deck = roundTripAppearanceDeck(t, deck)
	assertElementStyle(t, deck, "group-a", "modern") // base master

	deck.Masters.Base.Slide.DefaultStyle = ""
	deck = roundTripAppearanceDeck(t, deck)
	assertElementStyle(t, deck, "group-a", "retro") // deck
	assertElementStyle(t, deck, "explicit", "modern")
	if !strings.Contains(string(deck.Appearance.Extra["future"]), `"keep":true`) {
		t.Fatal("compatible unknown appearance metadata was lost")
	}
}

func TestDocumentOpenRecoversInvalidOptionalAppearance(t *testing.T) {
	valid, err := encodeDeckAppearance(&DeckAppearance{Version: 2, DefaultStyle: "modern"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"invalid-base64":     "<!-- keynope-appearance base64:%%% -->\n# Kept\n",
		"malformed-envelope": "<!-- keynope-appearance nope -->\n# Kept\n",
		"duplicate":          valid + "\n" + valid + "\n# Kept\n",
		"oversized":          "<!-- keynope-appearance base64:" + strings.Repeat("A", (64<<10)+1) + " -->\n# Kept\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			deck, err := parseDeckData(name+".md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if deck.Appearance != nil || len(deck.Slides) != 1 || deck.Slides[0].Elements[0].Text != "Kept" {
				t.Fatalf("valid content not recovered: %#v", deck)
			}
			if len(deck.Diagnostics) != 1 || deck.Diagnostics[0].Code != "appearance-recovered" {
				t.Fatalf("missing bounded diagnostic: %#v", deck.Diagnostics)
			}
			state := newNativeEditorSession(name+".md", deck).state()
			if !reflect.DeepEqual(state.Diagnostics, deck.Diagnostics) || state.Dirty {
				t.Fatal("recovery diagnostic not exposed as clean editor state")
			}
			encoded, err := serializeDeck(name+".md", deck)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "keynope-appearance") || strings.Contains(string(encoded), "appearance-recovered") {
				t.Fatal("invalid envelope or diagnostic was persisted")
			}
		})
	}
}

func TestDocumentOpenDiagnosesRetiredAndMalformedTextMetadata(t *testing.T) {
	malformedText := "<!-- truetype-text=base64:" + base64.StdEncoding.EncodeToString([]byte("unused")) + " kind=broken -->\n"
	deck, err := parseDeckData("legacy.md", []byte("<!-- keynope-fonts version=1 base64:YWJj -->\n"+malformedText+"<!-- glyph=blocks render=text-image source=bitmap scale=2 text-size=10 -->\nKept\n"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, diagnostic := range deck.Diagnostics {
		seen[diagnostic.Code] = true
		if len(diagnostic.Message) > 512 {
			t.Fatal("unbounded diagnostic")
		}
	}
	if !seen["retired-font-data"] || !seen["text-payload-recovered"] {
		t.Fatalf("missing migration diagnostics: %#v", deck.Diagnostics)
	}
	if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 2 || deck.Slides[0].Elements[0].Text != "unused" || deck.Slides[0].Elements[1].Text != "Kept" || !isTrueType(deck.Slides[0].Elements[0]) || !isTrueType(deck.Slides[0].Elements[1]) {
		t.Fatalf("valid text was not recovered and migrated: %#v", deck.Slides)
	}

	standalone, err := parseDeckData("retired.md", []byte("<!-- glyph=blocks render=text-image source=bitmap scale=2 text-size=10 -->\nKept\n"))
	if err != nil || len(standalone.Diagnostics) != 1 || standalone.Diagnostics[0].Code != "retired-text-data" {
		t.Fatalf("standalone retired settings were not diagnosed: %#v, %v", standalone.Diagnostics, err)
	}
	malformedFont, err := parseDeckData("font.md", []byte("<!-- keynope-fonts version=broken payload -->\n# Kept\n"))
	if err != nil || len(malformedFont.Slides) != 1 || malformedFont.Slides[0].Elements[0].Text != "Kept" || len(malformedFont.Diagnostics) != 1 || malformedFont.Diagnostics[0].Code != "retired-font-data" {
		t.Fatalf("malformed retired font envelope was not recovered: %#v, %#v, %v", malformedFont.Slides, malformedFont.Diagnostics, err)
	}
}
