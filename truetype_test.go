package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrueTypeRoundTripAndExport(t *testing.T) {
	e := Element{Kind: "text", Text: "TrueType\nLiteral **stars**", Query: "render=truetype&ttf-size=120&width=100&height=12&top=5&left_pct=0.1&transparent=1&outline=dark&gradient-start=%23ff0055&gradient-end=%23ffffaa&shadow=solid&shadow-color=%23ffffff"}
	d := Deck{Slides: []Slide{{Elements: []Element{e}}}}
	b, err := serializeDeck("test.md", d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeckData("test.md", b)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Slides[0].Elements[0]
	q, _ := url.ParseQuery(got.Query)
	if !isTrueType(got) || q.Get("ttf-size") != "120" || q.Get("shadow") != "solid" || got.Text != e.Text {
		t.Fatalf("lost TrueType metadata: %s\n%#v", b, got)
	}
	pages := exportSlidePages(parsed.Slides[0], 0, 1, 245, 56)
	count := 0
	for _, p := range pages {
		for _, l := range p.Lines {
			if l.TrueType != nil {
				count++
				if l.TrueType.Size != 120 || l.TrueType.Width != 100 || l.TrueType.Height != 12 {
					t.Fatalf("bad TrueType payload: %#v", l.TrueType)
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("want one TrueType object, got %d", count)
	}
	if trueTypeSize(Element{Query: "ttf-size=9999"}) != 512 || trueTypeSize(Element{Query: "ttf-size=-1"}) != 1 {
		t.Fatal("font size bounds")
	}
	normal := exportSlidePages(Slide{Elements: []Element{{Kind: "text", Text: "Unchanged"}}}, 0, 1, 245, 56)
	for _, l := range normal[0].Lines {
		if l.TrueType != nil {
			t.Fatal("regular text migrated unexpectedly")
		}
	}
}

func TestTrueTypeBlockConversion(t *testing.T) {
	for _, kind := range []string{"bullet", "code"} {
		source := Element{Kind: "text", Text: "One two\nThree four", Query: "render=truetype&ttf-size=96&ttf-width=50&width=100&height=20&align=justify&orientation=cw"}
		converted := convertedTextKindElement(source, kind, 0)
		if !isTrueType(converted) || trueTypeSize(converted) != 96 || trueTypeWidthPercent(converted) != 50 || converted.Kind != kind || converted.Text != source.Text {
			t.Fatalf("lost renderer on conversion: %#v", converted)
		}
		data, err := serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{converted}}}})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parseDeckData("test.md", data)
		if err != nil {
			t.Fatal(err)
		}
		got := parsed.Slides[0].Elements[0]
		if got.Kind != kind || got.Text != source.Text || !isTrueType(got) || textOrientation(got) != "cw" || trueTypeWidthPercent(got) != 50 {
			t.Fatalf("block round trip: %#v", got)
		}
	}
}

func TestTrueTypePresetSizes(t *testing.T) {
	for _, tc := range []struct {
		kind        string
		level, want int
	}{{"text", 0, 97}, {"heading", 1, 386}, {"heading", 2, 193}, {"bullet", 0, 97}, {"code", 0, 97}} {
		e := Element{Kind: tc.kind, Level: tc.level, Text: "Test", Query: "render=truetype"}
		if got := trueTypeSize(e); got != tc.want {
			t.Fatalf("%s H%d: got %d want %d", tc.kind, tc.level, got, tc.want)
		}
		e.Query += "&ttf-size=120"
		if trueTypeSize(e) != 120 {
			t.Fatal("explicit size must override preset")
		}
	}
}

func TestTrueTypeHeadingColour(t *testing.T) {
	e := Element{Kind: "heading", Level: 1, Text: "Title", Query: "render=truetype&header=%23ff5500&width=200&height=30"}
	pages := exportSlidePages(Slide{Elements: []Element{e}}, 0, 1, 245, 56)
	for _, page := range pages {
		for _, line := range page.Lines {
			if line.TrueType != nil {
				if line.TrueType.Size != 386 || line.Parts[0].Color != "rgb(255,85,0)" {
					t.Fatalf("wrong heading export: %#v", line)
				}
				return
			}
		}
	}
	t.Fatal("missing TrueType heading")
}

func TestTrueTypeWidth(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  float64
	}{
		{"", 100}, {"ttf-width=41.67", 41.67}, {"ttf-width=100", 100}, {"ttf-width=50", 50}, {"ttf-width=0", 1}, {"ttf-width=999", 200}, {"ttf-width=bad", 100},
	} {
		if got := trueTypeWidthPercent(Element{Query: tc.query}); got != tc.want {
			t.Fatalf("%s: got %g want %g", tc.query, got, tc.want)
		}
	}
	e := Element{Kind: "text", Text: "ABCD", Query: "render=truetype&ttf-size=100"}
	w, h := trueTypeBounds(e, 1920, 1080)
	if w != 134 {
		t.Fatalf("default width: got %d want 134", w)
	}
	e.Query += "&ttf-width=100"
	w, h = trueTypeBounds(e, 1920, 1080)
	e.Query = "render=truetype&ttf-size=100"
	e.Query += "&ttf-width=50"
	narrowW, narrowH := trueTypeBounds(e, 1920, 1080)
	if narrowW != w/2 || narrowH != h {
		t.Fatalf("width scale changed height or wrong width: %dx%d -> %dx%d", w, h, narrowW, narrowH)
	}
	e.Query += "&width=500&height=200"
	w, h = trueTypeBounds(e, 1920, 1080)
	if w != 500 || h != 200 {
		t.Fatal("explicit bounds changed")
	}
	data, err := serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Slides[0].Elements[0]
	if trueTypeWidthPercent(got) != 50 {
		t.Fatal("lost saved width")
	}
	pages := exportSlidePages(parsed.Slides[0], 0, 1, 1920, 1080)
	if !strings.Contains(pages[0].Lines[0].TrueType.Query, "ttf-width=50") {
		t.Fatal("export lost width")
	}
	e.Query = "render=truetype&ttf-width=41.67"
	data, err = serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(parsed.Slides[0].Elements[0].Query)
	if q.Get("ttf-width") != "41.67" {
		t.Fatal("lost fractional saved width")
	}
}

// Opt in to the real native-editor HTTP API + Chromium integration test.
func TestTrueTypeBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_TTF_UI_TEST") != "1" {
		t.Skip("set KEYNOPE_TTF_UI_TEST=1 to run Playwright")
	}
	path := filepath.Join(t.TempDir(), "truetype.md")
	oldWidth, oldHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = oldWidth, oldHeight })
	d := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Original block text", Query: "top=2"}}}}}
	s := newNativeEditorSession(path, d)
	html, err := exportHTMLDocument(path, s.deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	html = strings.Replace(html, "<head>", "<head><script>window.KEYNOPE_APP_SURFACE='app';</script>", 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/editor/state", s.handleState)
	mux.HandleFunc("/api/editor/action", s.handleAction)
	mux.HandleFunc("/api/editor/workspace", s.handleWorkspace)
	mux.HandleFunc("/api/editor/preview", s.handlePreview)
	mux.HandleFunc("/api/editor/document", s.handleDocument)
	mux.HandleFunc("/api/editor/fonts/default", s.handleDefaultFont)
	mux.HandleFunc("/api/editor/fonts/library", s.handleFontLibrary)
	mux.HandleFunc("/api/editor/emojis", s.handleEmojiCatalog)
	mux.HandleFunc("/test/workspace", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		var pages []exportPage
		for index, slide := range s.deck.ResolvedSlides() {
			pages = append(pages, exportSlidePages(slide, index, len(s.deck.Slides), 245, 56)...)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pages": pages, "cols": 245, "rows": 56, "current": s.current})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.Command("node", "tools/test_truetype_ui.cjs", server.URL)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("node", "tools/test_emoji_editor.cjs", server.URL)
	out, err = cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
