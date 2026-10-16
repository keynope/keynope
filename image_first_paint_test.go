package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Real native HTTP transport, without launching or activating the desktop app.
func TestImageFirstPaintBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_IMAGE_FIRST_PAINT_BROWSER") != "1" {
		t.Skip("set KEYNOPE_IMAGE_FIRST_PAINT_BROWSER=1 for the cold image paint regression")
	}
	useTestAuthoredSize(t, 245, 56)
	img := image.NewNRGBA(image.Rect(0, 0, 120, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 245, G: 40, B: 120, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	id, asset, imagePath, err := embeddedImageAssetContext(t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "cold-image-slide", BG: "#112233", BGSet: true, Elements: []Element{
		{ID: "cold-image", Kind: "image", AssetID: id, Path: imagePath, Query: "left=30&top=10&width=90&height=30&image-style=retro"},
	}}}}
	s := newNativeEditorSession("Cold image.md", d)
	server := newGeometryBrowserServer(t, s)
	runGeometryBrowser(t, "tools/test_image_first_paint.cjs", server.URL)
}

func newGeometryBrowserServer(t *testing.T, s *nativeEditorSession) *httptest.Server {
	t.Helper()
	p := &presenterCompanion{}
	t.Cleanup(p.Close)
	if err := p.Refresh(filepath.Join(t.TempDir(), "Cold image.md"), s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	s.companion = p
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	p.Update(0, 0, false, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := p.htmlSnapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/state", p.handleState)
	mux.HandleFunc("/slides", p.handlePageUpdate)
	mux.HandleFunc("/api/editor/state", s.handleState)
	mux.HandleFunc("/api/editor/action", s.handleAction)
	mux.HandleFunc("/api/editor/workspace", s.handleWorkspace)
	mux.HandleFunc("/api/editor/scene", s.handleScene)
	mux.HandleFunc("/api/editor/scene-fonts", s.handleSceneFonts)
	mux.HandleFunc("/api/editor/typeface", s.handleTypeface)
	mux.HandleFunc("/api/editor/font-export", s.handleFontExport)
	mux.HandleFunc("/api/editor/document", s.handleDocument)
	mux.HandleFunc("/api/editor/export-document", s.handleExportDocument)
	mux.HandleFunc("/api/editor/export-pptx", s.handleExportPPTX)
	mux.HandleFunc("/api/editor/export-pdf", s.handleExportPDF)
	mux.HandleFunc("/api/editor/upload", s.handleUpload)
	mux.HandleFunc("/api/editor/preview", s.handlePreview)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func runGeometryBrowser(t *testing.T, script, serverURL string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "node", script, serverURL+"/?keynopeSurface=app")
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}
