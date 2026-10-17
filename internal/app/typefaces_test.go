package app

import (
	"bytes"
	"encoding/binary"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func testTypeface(t *testing.T, data []byte) DeckTypeface {
	t.Helper()
	face, err := prepareTypeface(DeckTypeface{Name: "Test font", PostScriptName: "TestFont", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func typefaceDeck(t *testing.T) Deck {
	t.Helper()
	face := testTypeface(t, gomono.TTF)
	return Deck{Appearance: &DeckAppearance{Version: 3}, Typefaces: map[string]DeckTypeface{face.ID(): face}, Slides: []Slide{{ID: "font-slide", BG: "#f5f7fa", BGSet: true, Elements: []Element{
		{ID: "font-text", Kind: "text", Text: "System fonts. Your own voice.", Query: "render=truetype&modern-font=" + face.ID() + "&ttf-size=97&top=10&left=10&width=220&height=15&fg=%23155e75"},
	}}}}
}

func TestTypefaceRoundTripAndMetrics(t *testing.T) {
	deck := typefaceDeck(t)
	encoded, err := serializeDeck("Fonts.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Fonts.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	face := testTypeface(t, gomono.TTF)
	if got := reopened.Typefaces[face.ID()]; !bytes.Equal(got.Data, face.Data) || got.metric == nil || got.PostScriptName != face.PostScriptName {
		t.Fatal("lost embedded face or its metrics")
	}
	scene, err := buildDocumentSlideScene(reopened, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Typefaces) != 1 || scene.Objects[0].Text.FontID != face.ID() || scene.Objects[0].Text.TypefaceMetrics[face.ID()].metric == nil {
		t.Fatal("scene lost custom font")
	}
	style := scene.Objects[0].Text
	if a, b := chartTextWidth("iiiiiiii", style), chartTextWidth("WWWWWWWW", style); a != b {
		t.Fatal("custom monospace metrics used fallback", a, b)
	}
	style.FontID = "sans"
	if a, b := chartTextWidth("iiiiiiii", style), chartTextWidth("WWWWWWWW", style); a >= b {
		t.Fatal("control metrics not proportional")
	}
	public, err := participantSlideMarkdown(reopened, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(public, []byte("keynope-typefaces")) {
		t.Fatal("audience document lost font")
	}
	presentation, _, err := deckToPPTX(reopened)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, font := range presentation.EmbeddedFonts {
		if font.Family == face.PostScriptName {
			found = true
			if font.ForceEditable || !bytes.Equal(font.Data, face.Data) {
				t.Fatal("font permissions or bytes modified")
			}
		}
	}
	if !found {
		t.Fatal("editable custom font missing from PPTX")
	}
}

func TestTypefacePermissionAndArchiveValidation(t *testing.T) {
	for _, flags := range []uint16{0, 8, 0x100, 0x108, 2, 4, 0x200, 0x208} {
		data := bytes.Clone(goregular.TTF)
		binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], flags)
		face, err := prepareTypeface(DeckTypeface{Name: "Test", PostScriptName: "Test", Data: data})
		if err != nil {
			t.Fatal(err)
		}
		if flags == 2 || typefaceEmbeddingFlags(data)&0x200 != 0 {
			if !face.LocalOnly || len(face.Data) != 0 || face.metric == nil {
				t.Fatalf("restricted font %x retained outlines or lost runtime metrics", flags)
			}
		} else if face.LocalOnly || !bytes.Equal(face.Data, data) || face.previewOnly() != (flags == 4) {
			t.Fatalf("permission %x modified", flags)
		}
	}
	for _, face := range []DeckTypeface{{Name: "Bad", PostScriptName: "Bad", Data: []byte("not a font")}, {Name: "Bad\nname", PostScriptName: "Bad", LocalOnly: true}, {Name: "Local", PostScriptName: "Local", LocalOnly: true, Data: goregular.TTF}} {
		if _, err := prepareTypeface(face); err == nil {
			t.Fatal("accepted invalid font")
		}
	}
	face := testTypeface(t, goregular.TTF)
	archive, err := encodeTypefaces(map[string]DeckTypeface{face.ID(): face})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeTypefaces(archive + "\n" + archive); err == nil {
		t.Fatal("accepted duplicate font metadata")
	}
	tampered := strings.Replace(archive, "version=1", "version=99", 1)
	if _, _, err := decodeTypefaces(tampered); err == nil {
		t.Fatal("accepted unsupported font version")
	}
	face.LocalOnly = true
	if _, err := encodeTypefaces(map[string]DeckTypeface{"font-" + strings.Repeat("0", 64): face}); err == nil {
		t.Fatal("accepted wrong content identity")
	}
}

func TestTypefaceRestrictedFontIsReferenceOnly(t *testing.T) {
	data := bytes.Clone(gomono.TTF)
	binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], 2)
	face := testTypeface(t, data)
	deck := typefaceDeck(t)
	s := newNativeEditorSession("Licensed fonts.md", deck)
	if err := s.apply(nativeEditorAction{Action: "register-typeface", TypefaceData: &face}); err != nil {
		t.Fatal(err)
	}
	id, revision := face.ID(), s.version
	if err := s.apply(nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"font-text"}, ModernFont: &id, SceneRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	encoded, err := serializeDeck("Licensed fonts.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Licensed fonts.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if f := reopened.Typefaces[id]; !f.LocalOnly || len(f.Data) != 0 || f.EmbeddingFlags != 2 {
		t.Fatal("restricted font outlines persisted", f)
	}
	presentation, report, err := deckToPPTX(reopened)
	if err != nil {
		t.Fatal(err)
	}
	for _, font := range presentation.EmbeddedFonts {
		if font.Family == face.PostScriptName {
			t.Fatal("PPTX embedded restricted outlines")
		}
	}
	warned := false
	for _, warning := range report.Warnings {
		if warning.Code == "font-not-embedded" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("PPTX should warn about external reader restrictions")
	}
	if _, _, err := exportPPTXDocument(reopened); err != nil {
		t.Fatal(err)
	}
}

func TestTypefaceAdmissionUndoAndLocalOnly(t *testing.T) {
	deck := typefaceDeck(t)
	deck.Typefaces = nil
	deck.Slides[0].Elements[0].Query = setQueryValue(deck.Slides[0].Elements[0].Query, "modern-font", "sans")
	s := newNativeEditorSession("Fonts.md", deck)
	face := testTypeface(t, gomono.TTF)
	before := s.state()
	if err := s.apply(nativeEditorAction{Action: "register-typeface", TypefaceData: &face}); err != nil {
		t.Fatal(err)
	}
	after := s.state()
	if before.Version != after.Version || before.Dirty != after.Dirty || len(s.deck.Typefaces) != 0 {
		t.Fatal("resource admission edited document")
	}
	id := face.ID()
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"font-text"}, ModernFont: &id, SceneRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	// Change the existing reference to a real edit and ensure its registry is owned.
	size := 123.0
	revision = s.version
	if err := s.apply(nativeEditorAction{Action: "set-text-size", ObjectIDs: []string{"font-text"}, TextSize: &size, SceneRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Typefaces) != 1 {
		t.Fatal("content edit did not retain admitted font")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if s.deck.Typefaces[id].metric == nil {
		t.Fatal("history lost font")
	}
	local, err := prepareTypeface(DeckTypeface{Name: "Local installed font", PostScriptName: "LocalTestFont", LocalOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	s.deck.Typefaces[local.ID()] = local
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	q.Set("modern-font", local.ID())
	s.deck.Slides[0].Elements[0].Query = q.Encode()
	_, report, err := deckToPPTX(s.deck)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range report.Warnings {
		if w.Code == "font-not-embedded" {
			found = true
		}
	}
	if !found {
		t.Fatal("local-only export should warn")
	}
}

func TestTypefaceBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_TYPEFACE_BROWSER") != "1" {
		t.Skip("set KEYNOPE_TYPEFACE_BROWSER=1 for font picker and rendering checks")
	}
	useTestAuthoredSize(t, 245, 56)
	fixture := filepath.Join(t.TempDir(), "Import.ttf")
	if err := os.WriteFile(fixture, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYNOPE_FONT_FIXTURE", fixture)
	deckFile := filepath.Join(t.TempDir(), "Fonts.md")
	content, err := serializeDeck(deckFile, typefaceDeck(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deckFile, content, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYNOPE_FONT_DECK_FIXTURE", deckFile)
	for _, engine := range []string{"webkit", "chromium"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("KEYNOPE_BROWSER_ENGINE", engine)
			s := newNativeEditorSession("Fonts.md", typefaceDeck(t))
			server := newGeometryBrowserServer(t, s)
			runGeometryBrowser(t, "tools/test_system_fonts.cjs", server.URL)
		})
	}
}

func TestTypefaceClipboardAndShapeMetrics(t *testing.T) {
	deck := typefaceDeck(t)
	unused := testTypeface(t, goregular.TTF)
	deck.Typefaces[unused.ID()] = unused
	bundle, err := makeObjectClipboard(deck, deck.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Typefaces) != 1 || bundle.Typefaces[unused.ID()].Name != "" {
		t.Fatal("clipboard copied unrelated fonts")
	}
	s := newNativeEditorSession("Destination.md", Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}})
	if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
		t.Fatal(err)
	}
	for id, face := range bundle.Typefaces {
		if !bytes.Equal(face.Data, s.deck.Typefaces[id].Data) || s.deck.Typefaces[id].metric == nil {
			t.Fatal("paste lost font bytes/metrics")
		}
	}
	face := testTypeface(t, gomono.TTF)
	q := url.Values{"modern-font": {face.ID()}, "ttf-size": {"97"}}
	label := Element{Kind: "text", Text: "iiiiiiii", Query: q.Encode()}
	a, _ := shapeLabelSizeWithFonts(label, 245, 56, deck.Typefaces)
	label.Text = "WWWWWWWW"
	b, _ := shapeLabelSizeWithFonts(label, 245, 56, deck.Typefaces)
	if a != b {
		t.Fatal("shape labels did not use custom monospace advances", a, b)
	}
}
