package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSharedTextEmojiTint(t *testing.T) {
	for _, tint := range []string{"#55aaff", "#FFFFFF", "", "red", "#xyzxyz", "#12345"} {
		q := url.Values{"tint": {tint}}
		got := sceneTextFromValues(Element{Kind: "text", Text: "D😍D"}, q)
		want := ""
		if tint == "#55aaff" || tint == "#FFFFFF" {
			want = tint
		}
		if got.EmojiTint != want {
			t.Fatalf("tint %q: got %q, want %q", tint, got.EmojiTint, want)
		}
	}
}

func TestEmojiTextFontsEndpoint(t *testing.T) {
	s := &nativeEditorSession{}
	for _, tc := range []struct {
		method, text string
		status       int
	}{
		{"GET", "D👩🏽‍🚀D 👩🏽‍🚀", 200}, {"POST", "😀", 405}, {"GET", strings.Repeat("a", 8193), 413},
	} {
		w := httptest.NewRecorder()
		s.handleEmojiTextFonts(w, httptest.NewRequest(tc.method, "/api/editor/emoji-text-fonts?text="+url.QueryEscape(tc.text), nil))
		if w.Code != tc.status {
			t.Fatalf("status %d, want %d", w.Code, tc.status)
		}
		if w.Code == 200 {
			var fonts map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &fonts); err != nil {
				t.Fatal(err)
			}
			if len(fonts) != 1 || fonts["👩🏽‍🚀"] == "" {
				t.Fatal("expected only the unique Unicode emoji font")
			}
		}
	}
}

func TestSharedRetroTextCommitPreservesModernProfile(t *testing.T) {
	for _, label := range []bool{false, true} {
		deck := sceneFixture(t)
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "retro"}
		query := "render=truetype&ttf-size=97&ttf-width=75&modern-size=31&modern-width=160&modern-font=sans&modern-line-height=2"
		owner := Element{ID: "target", Kind: "text", Text: "Before", Query: query}
		if label {
			payload, _ := json.Marshal(shapeLabelData{Text: "Before", Query: query + "&element-style=retro"})
			owner = Element{ID: "target", Kind: "shape", Query: "element-style=modern&shape=square&shape-label=" + url.QueryEscape(base64.StdEncoding.EncodeToString(payload))}
		}
		deck.Slides[0].Elements = []Element{owner}
		size, width, spacing, font := 120.0, .4167*90, 1.2, "c64"
		action := nativeEditorAction{ObjectID: "target", TextRuns: []sceneRun{{Text: "After"}}, ModernSize: &size, ModernWidth: &width, ModernLineHeight: &spacing, ModernFont: &font}
		changed, err := applySceneText(&deck, action)
		if err != nil || !changed {
			t.Fatalf("label=%v: %v, %v", label, changed, err)
		}
		element := deck.Slides[0].Elements[0]
		if label {
			element, _ = shapeLabel(element)
		}
		q, _ := url.ParseQuery(element.Query)
		if element.Text != "After" || trueTypeSize(element) != 120 || math.Abs(trueTypeWidthPercent(element)-90) > 1e-9 || q.Get("modern-size") != "31" || q.Get("modern-width") != "160" || q.Get("modern-font") != "sans" || q.Get("modern-line-height") != "2" {
			t.Fatalf("wrong active profile or lost inactive profile: %+v", element)
		}
		if changed, err = applySceneText(&deck, action); err != nil || changed {
			t.Fatalf("unchanged edit created a mutation: %v %v", changed, err)
		}
		before := cloneDeck(deck)
		bad := 513.0
		action.ModernSize = &bad
		if _, err := applySceneText(&deck, action); err == nil || !reflect.DeepEqual(before, cloneDeck(deck)) {
			t.Fatalf("label=%v: invalid Retro size accepted or partially committed: %v", label, err)
		}
	}
}

func TestResolvedRetroTextProfile(t *testing.T) {
	deck := sceneFixture(t)
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		t.Run(kind, func(t *testing.T) {
			element := Element{Kind: kind, Level: 2, Text: "C64 text", Query: "render=truetype&ttf-size=97&ttf-width=75&modern-size=31&modern-width=160&modern-font=sans&modern-line-height=2&modern-paragraph-before=1&modern-paragraph-after=2"}
			original := element
			modern := deck.sceneTextForStyle(element, "modern")
			retro := deck.sceneTextForStyle(element, "retro")
			if retro.FontID != "c64" || retro.Family != "KeynopeC64, monospace" || retro.Size != 97 || math.Abs(retro.WidthScale-.4167*.75) > 1e-9 || retro.LineHeight != 1.2 || retro.ParagraphBefore != 0 || retro.ParagraphAfter != 0 {
				t.Fatalf("incorrect Retro profile: %+v", retro)
			}
			if modern.Size != 31 || modern.WidthScale != 1.6 || modern.FontID != "sans" || modern.LineHeight != 2 || modern.ParagraphBefore != 1 || modern.ParagraphAfter != 2 {
				t.Fatalf("Modern profile lost: %+v", modern)
			}
			if !reflect.DeepEqual(modern.Runs, retro.Runs) || element != original {
				t.Fatal("style resolution changed semantic content or authored element")
			}
		})
	}
}

func TestRetroSceneDescribesItsPaintedFont(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{{ID: "retro", Kind: "text", Text: "C64 text", Query: "element-style=retro&render=truetype&ttf-size=97&ttf-width=75&modern-size=31&modern-font=sans&top=2&left=2&width=150&height=12"}}
	scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		if object.ID != "retro" {
			continue
		}
		if object.RetroText != nil || object.RetroLines != nil || object.Text == nil || object.Text.FontID != "c64" || object.Text.Size != 97 {
			t.Fatalf("semantic typography differs from active painter: %+v", object)
		}
		return
	}
	t.Fatal("missing Retro text object")
}

func TestSharedRetroTextIgnoresRetiredArtMetadata(t *testing.T) {
	for _, query := range []string{"glyph=blocks", "glyph=braille", "glyph=ascii", "font=custom"} {
		if !sharedRetroText(Element{Kind: "text", Text: "Artwork", Query: "render=truetype&" + query}) {
			t.Fatalf("retired metadata reactivated sampled text: %s", query)
		}
	}
	if !sharedRetroText(Element{Kind: "text", Text: "Emoji 😀", Query: "render=truetype"}) {
		t.Fatal("Retro emoji text must use shared shaped typography")
	}
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		if !sharedRetroText(Element{Kind: kind, Text: "Ordinary text", Query: "render=truetype"}) {
			t.Fatalf("ordinary %s did not use shared typography", kind)
		}
		transparent := Element{Kind: kind, Text: "See through", Query: "render=truetype&transparent=1"}
		if !sharedRetroText(transparent) || !sceneTextFor(transparent).SeeThrough {
			t.Fatalf("see-through %s must use shared typography and retain paint", kind)
		}
	}
}

func TestSceneTextEmbedsOnlyUsedEmojiFonts(t *testing.T) {
	retro := (Deck{}).sceneTextForStyle(Element{Kind: "text", Text: "D😀D", Query: "render=truetype&ttf-width=50"}, "retro")
	if math.Abs(retro.EmojiWidthScale*retro.WidthScale-.5) > 1e-9 {
		t.Fatal("Retro emoji must remove baseline squeeze but retain explicit font width")
	}
	for _, kind := range []string{"text", "bullet", "code"} {
		text := sceneTextFor(Element{Kind: kind, Text: "D👩🏽‍🚀D 😀"})
		if len(text.EmojiFonts) != 2 || text.EmojiFonts["👩🏽‍🚀"] == "" || text.EmojiFonts["😀"] == "" {
			t.Fatalf("%s: missing exact Unicode font keys", kind)
		}
	}
	if len(sceneTextFor(Element{Kind: "text", Text: "No emoji"}).EmojiFonts) != 0 {
		t.Fatal("ordinary text includes unused emoji fonts")
	}
}

func TestSharedC64Font(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{{ID: "text", Kind: "text", Text: "Shared C64", Query: "element-style=modern&render=truetype&ttf-size=97"}}
	font := "c64"
	changed, err := applyModernTextStyle(&deck, nativeEditorAction{ObjectIDs: []string{"text"}, ModernFont: &font}, "")
	if err != nil || !changed {
		t.Fatalf("font command: %v %v", changed, err)
	}
	text := deck.modernSceneText(deck.Slides[0].Elements[0])
	if text.FontID != "c64" || text.Family != "KeynopeC64, monospace" {
		t.Fatalf("font resolution: %+v", text)
	}
	changed, err = applySceneText(&deck, nativeEditorAction{Slide: 0, ObjectID: "text", ModernFont: &font, TextRuns: []sceneRun{{Text: "Edited C64", Bold: true}}})
	if err != nil || !changed {
		t.Fatalf("shared editor commit: %v %v", changed, err)
	}
	if !strings.Contains(modernFontsCSS(), "font-family:KeynopeC64") || !strings.Contains(modernFontsCSS(), strings.TrimSpace(trueTypeFontBase64)) {
		t.Fatal("shared font CSS omitted embedded C64 face")
	}
}
