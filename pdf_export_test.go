package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Owned production-renderer fixture; generated only for explicit local QA.
func TestPDFQualityFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_PDF_EVIDENCE")
	if output == "" {
		t.Skip("set KEYNOPE_PDF_EVIDENCE to generate the owned PDF proof fixture")
	}
	table, _ := newTableData(3, 3)
	table.Cells[0][0].Text = "Quality"
	table.Cells[1][1].Text = "Selectable cell"
	size := 28.0
	table.Style.Cell.FontSize = &size
	chart := newChartData("column")
	chart.Title = "Selectable chart labels"
	chart.Options.DataLabels = true
	d := Deck{Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table": table}, Charts: map[string]ChartData{"chart": chart}}, Slides: []Slide{
		{BG: "48;2;238;243;248", Elements: []Element{{ID: "heading", Kind: "heading", Level: 1, Text: "Modern quality proof", Query: "render=truetype&modern-font=sans&modern-size=90&left=10&top=4&width=220&height=8"}, {ID: "text", Kind: "text", Text: "Selectable text at the edge", Query: "render=truetype&modern-font=sans&modern-size=45&left=5&top=45&width=200&height=5&fg=%23263445"}, {ID: "shape", Kind: "shape", Text: "[shape:circle]", Query: "shape=circle&left=168&top=14&width=68&height=27&fg=%230e7490&outline=dark"}}},
		{BG: "48;2;16;24;40", Elements: []Element{{ID: "retro-title", Kind: "heading", Level: 1, Text: "C64 FONT PROOF", Query: "render=truetype&modern-font=c64&modern-size=100&left=10&top=4&width=220&height=10&fg=%23e6f0ff"}, {ID: "retro-text", Kind: "text", Text: "Retro text remains text.", Query: "render=truetype&modern-font=c64&modern-size=70&left=10&top=20&width=220&height=10&fg=%2355ffff"}}},
		{BG: "48;2;255;255;255", Elements: []Element{{ID: "table", Kind: "table", ObjectID: "table", Query: "left=5&top=2&width=90&height=18"}, {ID: "chart", Kind: "chart", ObjectID: "chart", Query: "left=15&top=24&width=210&height=30"}}},
		{TabID: "private-tab", Notes: "PRIVATE NOTE", Elements: []Element{{ID: "private-tab-text", Kind: "text", Text: "TAB ONLY DO NOT EXPORT"}}},
	}}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 8), 180, 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	id, asset, assetPath, err := embeddedImageAsset(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	d.Assets = map[string]DeckAsset{id: asset}
	d.Slides = append(d.Slides[:3], Slide{Background: "constellation", Effect: "warp", BG: "48;2;16;24;40", Elements: []Element{{ID: "retro-image", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=retro&glyph=blocks&left=40&top=8&width=120&height=36"}, {ID: "retro-fallback-title", Kind: "text", Text: "Exact Retro fallback", Query: "render=truetype&modern-font=c64&modern-size=60&left=10&top=1&width=220&height=6&fg=%23e6f0ff"}}}, d.Slides[3])
	document, err := makePDFExportDocument("Quality proof.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "quality-proof.html"), []byte(document.Content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "raster-proof.html"), []byte(document.FallbackContent), 0644); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(document.Manifest)
	if err := os.WriteFile(filepath.Join(output, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	md, err := serializeDeck("quality-proof.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "quality-proof.md"), md, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPDFManifestScopedAndImmutable(t *testing.T) {
	d := Deck{Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "visible", Notes: "PRIVATE NOTES", Engagement: &EngagementDefinition{Kind: "impostor", ID: "secret", Code: "SECRETCODE"}, Elements: []Element{{ID: "text", Kind: "text", Text: "Public text", Query: "modern-font=sans&render=truetype"}, {ID: "qr", Kind: "text", Text: "PRIVATE QR", PlaceholderRole: activityQRCodeRole}, {ID: "url", Kind: "text", Text: "PRIVATE URL", PlaceholderRole: activityURLRole}}}, {ID: "tab", TabID: "tab", Elements: []Element{{Kind: "text", Text: "PRIVATE TAB"}}}, {ID: "second", Elements: []Element{{ID: "second-text", Kind: "text", Text: "Another page", Query: "modern-font=c64&render=truetype"}}}}}
	before := cloneDeck(d)
	doc, err := makePDFExportDocument("/private/path/Public.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("PDF projection mutated authored document")
	}
	if len(doc.Manifest.Pages) != 2 || doc.Manifest.Pages[0].Label != "1" || doc.Manifest.Pages[1].Label != "3" || doc.Manifest.Pages[1].Index != 1 {
		t.Fatal("tab exclusion/source slide labels", doc.Manifest)
	}
	if !doc.Manifest.Pages[0].Quality || !doc.Manifest.Pages[1].Quality {
		t.Fatal("TTF text should retain vector eligibility")
	}
	for _, body := range []string{doc.Content, doc.FallbackContent} {
		for _, secret := range []string{"PRIVATE NOTES", "PRIVATE QR", "PRIVATE URL", "PRIVATE TAB", "SECRETCODE", "/private/path"} {
			if strings.Contains(body, secret) {
				t.Fatal("PDF disclosed private content", secret)
			}
		}
	}
	for _, p := range doc.Manifest.Pages {
		if p.Width != 1920 || p.Height != 1080 || p.Page.Engagement != nil {
			t.Fatal("invalid export page scope", p)
		}
	}
}
func TestPDFFallbackClassification(t *testing.T) {
	for _, p := range []exportPage{{}, {Scene: &slideScene{}, Effect: "warp"}, {Scene: &slideScene{BackgroundMedia: &sceneBackgroundMedia{RetroLines: &sceneRetroLines{}}}}, {Scene: &slideScene{Objects: []sceneObject{{RetroLines: &sceneRetroLines{}}}}}, {Scene: &slideScene{Objects: []sceneObject{{Media: &sceneMedia{Frames: []sceneMediaFrame{{}, {}}}}}}}, {Scene: &slideScene{Objects: []sceneObject{{Text: &sceneText{}, Paint: scenePaint{GradientStart: "#ff0000"}}}}}} {
		if len(pdfPageFallbackReasons(p)) == 0 {
			t.Fatal("unsupported vector output declared quality eligible")
		}
	}
}
func TestPDFExportEndpointAndNoVisibleSlides(t *testing.T) {
	s := newNativeEditorSession("Public.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Endpoint proof"}}}}}, false, false)
	w := httptest.NewRecorder()
	s.handleExportPDF(w, httptest.NewRequest(http.MethodGet, "/api/editor/export-pdf", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.handleExportPDF(w, httptest.NewRequest(http.MethodPost, "/api/editor/export-pdf", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var doc pdfExportDocument
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil || len(doc.Manifest.Pages) != 1 || !strings.Contains(doc.Content, "keynope-print-data") {
		t.Fatal("manifest transport", err)
	}
	_, err := makePDFExportDocument("Tabs.md", Deck{Slides: []Slide{{TabID: "private"}}}, 245, 56)
	if err == nil {
		t.Fatal("empty PDF accepted")
	}
}
