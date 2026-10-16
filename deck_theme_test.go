package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDeckThemePresetsAndPortableMetadata(t *testing.T) {
	for _, theme := range builtinDeckThemes() {
		if err := theme.Validate(); err != nil {
			t.Fatalf("%s: %v", theme.Name, err)
		}
		encoded, err := encodeDeckTheme(theme)
		if err != nil {
			t.Fatal(err)
		}
		loaded, remaining, err := decodeDeckTheme(encoded + "\n# Hello")
		if err != nil || remaining != "# Hello" || !reflect.DeepEqual(theme, loaded) {
			t.Fatalf("%s round trip: %v", theme.Name, err)
		}
		second, err := encodeDeckTheme(loaded)
		if err != nil || encoded != second {
			t.Fatal("theme encoding is not deterministic")
		}
	}
	if builtinDeckTheme("unknown") != nil {
		t.Fatal("unknown preset accepted")
	}
	theme := builtinDeckTheme("clean-dark")
	raw, _ := json.Marshal(theme)
	raw = bytes.Replace(raw, []byte(`"background":`), []byte(`"future-color":{"nested":[1,2]},"background":`), 1)
	raw = bytes.Replace(raw, []byte(`"fontId":`), []byte(`"future-font":true,"fontId":`), 1)
	raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"future-theme":"kept"`), 1)
	if err := json.Unmarshal(raw, theme); err != nil {
		t.Fatal(err)
	}
	copy := theme.Clone()
	copy.Colors.Accent[0] = "#010203"
	(*copy.Colors.Extra)["future-color"] = json.RawMessage(`false`)
	if theme.Colors.Accent[0] == copy.Colors.Accent[0] || reflect.DeepEqual(theme.Colors.Extra, copy.Colors.Extra) {
		t.Fatal("theme clone aliases mutable metadata")
	}
	encoded, _ := encodeDeckTheme(theme)
	loaded, _, err := decodeDeckTheme(encoded)
	if err != nil || !reflect.DeepEqual(theme, loaded) {
		t.Fatalf("unknown extensions lost: %v", err)
	}
}

func TestDeckThemeRejectsMalformedAndOversized(t *testing.T) {
	encoded, _ := encodeDeckTheme(builtinDeckTheme("clean-light"))
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	_, _ = w.Write(bytes.Repeat([]byte("x"), maxThemeMetadataBytes+1))
	_ = w.Close()
	bomb := "<!-- keynope-theme version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->"
	for _, text := range []string{encoded + "\n" + encoded, strings.Replace(encoded, "version=1", "version=2", 1), "<!-- keynope-theme broken -->", "<!-- keynope-theme version=1 base64:AAAA -->", bomb} {
		if _, _, err := decodeDeckTheme(text); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	for _, mutate := range []func(*DeckTheme){
		func(v *DeckTheme) { v.Colors.Accent = nil }, func(v *DeckTheme) { v.Fonts.Body.Size = math.NaN() },
		func(v *DeckTheme) { v.Fonts.Body.FontID = "local/path.ttf" }, func(v *DeckTheme) { v.Defaults.LineWidth = math.Inf(1) },
		func(v *DeckTheme) { v.Colors.Text = "red" }, func(v *DeckTheme) { v.Fonts.Body.LineHeight = 0 },
	} {
		theme := builtinDeckTheme("clean-light")
		mutate(theme)
		if _, err := encodeDeckTheme(theme); err == nil {
			t.Fatal("invalid theme accepted")
		}
	}
}

func TestThemeTypographyIndependentInheritance(t *testing.T) {
	d := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, test := range []struct {
		e                 Element
		font              string
		size, width, line float64
	}{
		{Element{Kind: "text", Text: "Body"}, "sans", 64, 100, 1.25},
		{Element{Kind: "heading", Level: 1, Text: "H1"}, "sans", 160, 100, 1.1},
		{Element{Kind: "heading", Level: 2, Text: "H2"}, "sans", 80, 100, 1.1},
		{Element{Kind: "code", Text: "Code"}, "mono", 54, 100, 1.3},
		{Element{Kind: "text", Text: "Explicit", Query: "modern-font=c64&ttf-size=120&ttf-width=75&modern-line-height=1.8"}, "c64", 120, 75, 1.8},
		{Element{Kind: "text", Text: "Family only", Query: "modern-font=mono"}, "mono", 64, 100, 1.25},
	} {
		q, _ := url.ParseQuery(test.e.Query)
		before := cloneQueryValues(q)
		resolved := d.resolveThemeTextValues(test.e, q)
		if !reflect.DeepEqual(q, before) {
			t.Fatal("resolution changed authored query")
		}
		text := d.modernSceneText(test.e)
		width, _ := strconvThemeFloat(resolved, "modern-width", "ttf-width")
		if text.FontID != test.font || text.Size != test.size || text.LineHeight != test.line || width != test.width {
			t.Fatalf("%s resolved incorrectly: %+v width %v", test.e.Text, text, width)
		}
	}
}

func strconvThemeFloat(q url.Values, first, second string) (float64, error) {
	value := q.Get(first)
	if value == "" {
		value = q.Get(second)
	}
	var number float64
	err := json.Unmarshal([]byte(value), &number)
	return number, err
}

func TestThemeSceneSourceAndTokenRoundTrip(t *testing.T) {
	d := Deck{Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{Elements: []Element{
		{ID: "title", Kind: "heading", Level: 1, Text: "Theme title", Query: "top=2&width=200&height=10"},
		{ID: "token", Kind: "text", Text: "Linked", Query: "top=16&theme-fg=accent-2"},
		{ID: "explicit", Kind: "text", Text: "Explicit", Query: "top=24&theme-fg=accent-2&fg=%23112233&modern-font=c64&ttf-size=88"},
		{ID: "shape", Kind: "shape", Query: "top=32&shape=square&width=40&height=10&theme-fg=accent-3"},
	}}}}
	d = cloneDeck(newNativeEditorSession("theme.md", d).deck)
	before := cloneDeck(d)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("projection changed authored deck")
	}
	objects := map[string]sceneObject{}
	for _, o := range scene.Objects {
		objects[o.ID] = o
	}
	if objects["title"].Text.Size != 160 || objects["title"].Text.FontID != "sans" {
		t.Fatalf("title typography: %+v", objects["title"].Text)
	}
	for id, want := range map[string]string{"title": d.Theme.Colors.Heading, "token": d.Theme.Colors.Accent[1], "explicit": "#112233", "shape": d.Theme.Colors.Accent[2]} {
		if objects[id].Paint.Color != want {
			t.Fatalf("%s paint=%s want %s", id, objects[id].Paint.Color, want)
		}
	}
	data, err := serializeDeck("theme.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("theme.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Theme, loaded.Theme) {
		t.Fatal("embedded theme lost")
	}
	for _, e := range loaded.Slides[0].Elements {
		if e.ID == "token" {
			q, _ := url.ParseQuery(e.Query)
			if q.Get("theme-fg") != "accent-2" || q.Has("fg") {
				t.Fatal("token was flattened or lost")
			}
		}
	}
	loaded.Theme.Colors.Accent[1] = "#010203"
	scene, err = buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.ID == "token" && o.Paint.Color != "#010203" {
			t.Fatal("token did not update")
		}
		if o.ID == "explicit" && o.Paint.Color != "#112233" {
			t.Fatal("explicit colour was restyled")
		}
	}
}

func TestPortableThemeTransactionAndMaster(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("theme.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Keep content", Query: "modern-font=mono&fg=%23abcdef"}}}}})
		s.masterMode = master
		s.currentMaster = 0
		before := cloneDeck(s.deck)
		theme := builtinDeckTheme("bold")
		if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
			t.Fatal(err)
		}
		updated := cloneDeck(s.deck)
		content := cloneDeck(updated)
		content.Theme = before.Theme
		if !reflect.DeepEqual(content, before) || len(s.undo) != 1 {
			t.Fatal("theme changed content or made extra history")
		}
		theme.Colors.Text = "#010203"
		if s.deck.Theme.Colors.Text == theme.Colors.Text {
			t.Fatal("command aliases caller draft")
		}
		bad := s.deck.Theme.Clone()
		bad.Version = 3
		if s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: bad}) == nil || !reflect.DeepEqual(s.deck, updated) || len(s.undo) != 1 {
			t.Fatal("invalid command was not atomic")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.deck, before) {
			t.Fatal("undo not exact")
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.deck, updated) {
			t.Fatal("redo not exact")
		}
	}
}
