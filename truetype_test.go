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
		source := Element{Kind: "text", Text: "One two\nThree four", Query: "render=truetype&ttf-size=96&width=100&height=20&align=justify&orientation=cw"}
		converted := convertedTextKindElement(source, kind, 0)
		if !isTrueType(converted) || trueTypeSize(converted) != 96 || converted.Kind != kind || converted.Text != source.Text {
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
		if got.Kind != kind || got.Text != source.Text || !isTrueType(got) || textOrientation(got) != "cw" {
			t.Fatalf("block round trip: %#v", got)
		}
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
}
