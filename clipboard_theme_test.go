package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipboardThemeFallbacksAcrossDecks(t *testing.T) {
	source := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-dark"), Slides: []Slide{{Elements: []Element{
		{ID: "text", Kind: "text", Text: "Theme linked", Query: "render=truetype&modern-font=theme&theme-fg=accent-1&left=5&top=5&width=100&height=15&text-box=1"},
		{ID: "shape", Kind: "shape", Query: "shape=square&shape-stroke=theme:accent-2&left=120&top=5&width=40&height=20"},
	}}}}
	source.Theme.Fonts.Body.FontID = "mono"
	source.Theme.Fonts.Body.Size, source.Theme.Fonts.Body.Width = 88, 80
	source.Theme.Colors.Accent[0] = "#ff66cc"
	if dir := os.Getenv("KEYNOPE_CLIPBOARD_THEME_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(dir, "source.md"), source); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(dir, "destination.md"), Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{Elements: []Element{{ID: "caption", Kind: "text", Text: "Themed destination", Query: "top=30&render=truetype&modern-font=theme"}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	before := cloneDeck(source)
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(bundle)
	var wire ObjectClipboard
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.ThemeFallbacks) != 2 {
		t.Fatal("missing source style fallbacks")
	}
	for _, themed := range []bool{false, true} {
		target := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}}
		if themed {
			target.Theme = builtinDeckTheme("clean-light")
		}
		elements, assets, objects, err := prepareObjectClipboardPaste(target, wire)
		if err != nil {
			t.Fatal(err)
		}
		target.Slides[0].Elements, target.Assets, target.Objects = elements, assets, objects
		got, err := buildSlideScene(target, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if themed {
			q, _ := url.ParseQuery(elements[0].Query)
			var color string
			for _, object := range got.Objects {
				if object.ID == elements[0].ID {
					color = object.Paint.Color
				}
			}
			if q.Get("theme-fg") != "accent-1" || q.Get("modern-font") != "theme" || color != target.Theme.Colors.Accent[0] {
				t.Fatal("themed destination lost role association", q, color)
			}
		} else {
			want, err := buildSlideScene(source, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			for i := range got.Objects {
				if !reflect.DeepEqual(got.Objects[i].Paint, want.Objects[i].Paint) || !reflect.DeepEqual(got.Objects[i].Text, want.Objects[i].Text) || !reflect.DeepEqual(got.Objects[i].Bounds, want.Objects[i].Bounds) {
					t.Fatalf("unthemed destination lost source appearance for %s\ngot %#v\nwant %#v", elements[i].Kind, got.Objects[i], want.Objects[i])
				}
			}
			data, err := serializeDeck("Pasted.md", target)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Pasted.md", data)
			if err != nil || !reflect.DeepEqual(reopened.Slides[0].Elements[0].Query, elements[0].Query) {
				t.Fatal("fallback query lost on save/reopen", err)
			}
		}
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("capture or paste mutated source")
	}
}

func TestClipboardThemeFallbackValidation(t *testing.T) {
	bundle, err := makeObjectClipboard(Deck{Theme: builtinDeckTheme("clean-dark")}, []Element{{ID: "text", Kind: "text", Text: "Theme"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"connector-from=hidden", "top=50", "shape-label=secret", "modern-font=unknown", "modern-size=NaN", "modern-width=300", "fg=%23ffffff&fg=%23000000", "theme-fg=accent-1"} {
		copy := bundle
		copy.ThemeFallbacks = map[string]string{"text": query}
		if err := copy.Validate(); err == nil {
			t.Fatal("accepted non-format/bad fallback", query)
		}
	}
	bundle.ThemeFallbacks = map[string]string{"missing": "fg=%23ffffff"}
	if err := bundle.Validate(); err == nil {
		t.Fatal("accepted unrelated fallback")
	}
}

func TestClipboardThemeStrokeFallbackPreservesPresetAndZeroWidth(t *testing.T) {
	for _, query := range []string{"", "outline=light&shape-stroke=theme:accent-2", "outline=none&shape-stroke=theme:accent-2", "shape-stroke=theme:accent-2&shape-stroke-width=0", "shape-stroke=none"} {
		source := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-dark"), Slides: []Slide{{Elements: []Element{{ID: "shape", Kind: "shape", Query: "shape=square&left=5&top=5&width=40&height=20&" + query}}}}}
		bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
		if err != nil {
			t.Fatal(err)
		}
		target := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}}
		elements, _, _, err := prepareObjectClipboardPaste(target, bundle)
		if err != nil {
			t.Fatal(err)
		}
		target.Slides[0].Elements = elements
		want, err := buildSlideScene(source, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		got, err := buildSlideScene(target, 0, 245, 56)
		if err != nil || !reflect.DeepEqual(got.Objects[0].Paint, want.Objects[0].Paint) {
			t.Fatal("stroke fallback changed source paint", query, got.Objects[0].Paint, want.Objects[0].Paint, err)
		}
	}
}
