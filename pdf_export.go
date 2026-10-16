package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed web/print.js
var printRendererJS string

// PDF export is a resolved, audience-safe snapshot, never an authoring archive.
// The manifest also explains pages which require exact-appearance fallback.
type pdfExportManifest struct {
	Version int             `json:"version"`
	Title   string          `json:"title"`
	Cols    int             `json:"cols"`
	Rows    int             `json:"rows"`
	Pages   []pdfExportPage `json:"pages"`
}
type pdfExportPage struct {
	Index   int        `json:"index"`
	Label   string     `json:"label"`
	Width   float64    `json:"width"`
	Height  float64    `json:"height"`
	Quality bool       `json:"quality"`
	Reasons []string   `json:"reasons,omitempty"`
	Page    exportPage `json:"page"`
}

type pdfExportDocument struct {
	Content         string            `json:"content"`
	FallbackContent string            `json:"fallbackContent"`
	Manifest        pdfExportManifest `json:"manifest"`
}

func pdfPageFallbackReasons(p exportPage) []string {
	reasons := []string{}
	if p.Scene == nil {
		return []string{"Legacy canvas artwork"}
	}
	if p.Effect != "" && p.Effect != "none" || p.Background != "" && p.Background != "none" {
		reasons = append(reasons, "Animated slide background or effect")
	}
	if b := p.Scene.BackgroundMedia; b != nil && (b.RetroLines != nil || len(b.Frames) > 1) {
		reasons = append(reasons, "Sampled or animated background image")
	}
	if b := p.Scene.BackgroundMedia; b != nil && (len(b.ColourMatrices) > 0 || b.Sharpness != 0 && b.Sharpness != 1 || b.Paint != nil && b.Paint.GradientStart != "") {
		reasons = append(reasons, "Background image adjustments needing exact-appearance capture")
	}
	for _, o := range p.Scene.Objects {
		if o.RetroLines != nil || o.RetroText != nil || o.RetroLabel != nil {
			reasons = append(reasons, "Sampled Retro artwork")
			break
		}
	}
	for _, o := range p.Scene.Objects {
		if o.Media != nil && len(o.Media.Frames) > 1 {
			reasons = append(reasons, "Animated image, frozen at its first frame")
			break
		}
	}
	for _, o := range p.Scene.Objects {
		if o.Media != nil && (len(o.Media.ColourMatrices) > 0 || o.Media.Sharpness != 0 && o.Media.Sharpness != 1 || o.Paint.GradientStart != "") {
			reasons = append(reasons, "Image adjustments needing exact-appearance capture")
			break
		}
	}
	for _, o := range p.Scene.Objects {
		if (o.Text != nil || o.Label != nil) && (o.Paint.GradientStart != "" || o.Paint.ShadowColor != "" || o.LabelPaint != nil && (o.LabelPaint.GradientStart != "" || o.LabelPaint.ShadowColor != "")) {
			reasons = append(reasons, "Text effects needing exact-appearance capture")
			break
		}
	}
	return reasons
}
func projectPDFExport(deckPath string, deck Deck, cols, rows int) (pdfExportManifest, exportDeck, error) {
	projected := projectExportDeck(deckPath, deck.ResolvedSlides(), cols, rows, false)
	visible := make([]exportPage, 0, len(projected.Pages))
	manifest := pdfExportManifest{Version: 1, Title: strings.TrimSuffix(filepath.Base(deckPath), filepath.Ext(deckPath)), Cols: projected.Cols, Rows: projected.Rows, Pages: []pdfExportPage{}}
	if manifest.Title == "" || manifest.Title == "." {
		manifest.Title = "Keynope Presentation"
	}
	for _, p := range projected.Pages {
		if p.TabOnly {
			continue
		}
		width, height := deck.pageDimensions()
		if p.Scene != nil {
			width, height = p.Scene.Width, p.Scene.Height
		}
		if width <= 0 || height <= 0 || width > 20000 || height > 20000 || math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
			return manifest, exportDeck{}, fmt.Errorf("invalid PDF slide dimensions")
		}
		label := fmt.Sprint(p.Slide + 1)
		if p.Page > 0 {
			label += string(rune('a' + p.Page))
		}
		reasons := pdfPageFallbackReasons(p)
		manifest.Pages = append(manifest.Pages, pdfExportPage{Index: len(visible), Label: label, Width: width, Height: height, Quality: len(reasons) == 0, Reasons: reasons, Page: p})
		visible = append(visible, p)
		if len(visible) > 1000 {
			return manifest, exportDeck{}, fmt.Errorf("PDF export exceeds 1000 pages")
		}
	}
	if len(visible) == 0 {
		return manifest, exportDeck{}, fmt.Errorf("there are no presentation slides to export")
	}
	projected.Pages = visible
	return manifest, projected, nil
}
func makePDFExportDocument(deckPath string, deck Deck, cols, rows int) (pdfExportDocument, error) {
	manifest, projected, err := projectPDFExport(deckPath, deck, cols, rows)
	if err != nil {
		return pdfExportDocument{}, err
	}
	fallback, err := exportProjectedHTML(projected, preservedExportHead{}, false)
	if err != nil {
		return pdfExportDocument{}, err
	}
	payload, err := json.Marshal(struct {
		Manifest pdfExportManifest `json:"manifest"`
		Fallback string            `json:"fallback"`
	}{manifest, fallback})
	if err != nil {
		return pdfExportDocument{}, err
	}
	if len(payload) > 128<<20 {
		return pdfExportDocument{}, fmt.Errorf("PDF export resources exceed 128 MiB")
	}
	content := `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(manifest.Title) + `</title><style data-keynope-modern-fonts>` + modernFontsCSS() + `
html,body{margin:0;padding:0;background:#111;color:#fff;font:16px system-ui}#keynope-print-pages{margin:0;padding:0}.keynope-print-page{position:relative;overflow:hidden;break-after:page;page-break-after:always}.keynope-print-page:last-child{break-after:auto;page-break-after:auto}.keynope-chart-data-access,dialog{display:none!important}#keynope-print-status{position:fixed;inset:0;display:grid;place-content:center;background:#111;z-index:1000;text-align:center}#keynope-print-status button{padding:10px;margin:8px}#keynope-print-status[hidden]{display:none}@media print{html,body{background:transparent;-webkit-print-color-adjust:exact;print-color-adjust:exact}#keynope-print-status{display:none!important}}
</style></head><body><div id="keynope-print-status" role="status">Preparing PDF…</div><div id="keynope-print-pages"></div><script id="keynope-print-data" type="application/json">` + string(payload) + `</script><script>const keynopeTTFFontData='` + strings.TrimSpace(trueTypeFontBase64) + `';
` + trueTypeJS + shapedTextJS + sceneJS + printRendererJS + `</script></body></html>`
	return pdfExportDocument{Content: content, FallbackContent: fallback, Manifest: manifest}, nil
}
func (s *nativeEditorSession) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	deck, path := cloneDeckForRender(s.deck), s.deckPath
	s.mu.RUnlock()
	cols, rows := authoredRenderSize(245, 56)
	document, err := makePDFExportDocument(path, deck, cols, rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(document)
}
