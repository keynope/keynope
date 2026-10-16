package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"keynope/internal/pptx"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const ownedSVGLogo = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180" viewBox="0 0 320 180">
<metadata><private>This must not leave the importer</private></metadata>
<defs><linearGradient id="ink" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#00aacc"/><stop offset="100%" stop-color="#7733bb"/></linearGradient><path id="tick" d="M 0 12 L 10 22 L 28 0"/></defs>
<rect x="8" y="8" width="304" height="164" rx="24" fill="url(#ink)"/>
<g color="white" style="fill:none;stroke:currentColor;stroke-width:8;stroke-linecap:round;stroke-linejoin:round"><use href="#tick" x="38" y="45" transform="scale(2)"/></g>
<circle cx="248" cy="90" r="32" fill="#ffbb33"/>
</svg>`

func TestSVGSanitizerCanonicalPassiveReferences(t *testing.T) {
	data, w, h, err := sanitizeSVG(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	if w != 320 || h != 180 {
		t.Fatal("wrong viewport", w, h)
	}
	for _, removed := range []string{"metadata", "private", "<use", "href=", "style=", "currentColor"} {
		if bytes.Contains(data, []byte(removed)) {
			t.Fatal("unresolved/authoring data retained", removed)
		}
	}
	if !bytes.Contains(data, []byte(`stroke="white"`)) || !bytes.Contains(data, []byte(`url(#ink)`)) {
		t.Fatal("lost inherited paint or gradient", string(data))
	}
	canonical, w2, h2, err := sanitizeSVG(context.Background(), data)
	if err != nil || !bytes.Equal(data, canonical) || w2 != w || h2 != h {
		t.Fatal("canonicalization is not deterministic", err)
	}
	pixels, err := rasterizeSVG(context.Background(), data, w, h)
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(pixels))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha := image.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatal("lost transparent background")
	}
	r, g, b, a := image.At(248, 90).RGBA()
	if r < 50000 || g < 30000 || b > 20000 || a < 60000 {
		t.Fatal("SVG fallback lost the orange circle", r, g, b, a)
	}
	// These are the regressions caught by the browser comparison: rx implies
	// ry, and scale(2) scales both axes, including the stroke width.
	_, _, _, corner := image.At(9, 9).RGBA()
	tr, tg, tb, ta := image.At(96, 134).RGBA()
	if corner != 0 || tr < 60000 || tg < 60000 || tb < 60000 || ta < 60000 {
		t.Fatal("rounded corner or translated/scaled stroke lost", corner, tr, tg, tb, ta)
	}
	if output := os.Getenv("KEYNOPE_SVG_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "passive-logo.svg"), data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "passive-logo.png"), pixels, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSVGViewportInheritanceAndExpansion(t *testing.T) {
	input := "\ufeff" + `<svg width="100%" height="100%" viewBox="10 20 40 20"><g fill-opacity=".3"><rect x="10" y="20" width="20" height="20" fill="red"/><rect x="30" y="20" width="20" height="20" fill="blue" fill-opacity="1"/></g></svg>`
	canonical, w, h, err := sanitizeSVG(context.Background(), []byte(input))
	if err != nil || w != 40 || h != 20 {
		t.Fatal(w, h, err)
	}
	pixels, err := rasterizeSVG(context.Background(), canonical, w, h)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pixels))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := img.At(5, 10).RGBA()
	_, _, _, b := img.At(35, 10).RGBA()
	if a < 18000 || a > 21000 || b != 65535 {
		t.Fatal("viewBox offset or opacity inheritance changed", a, b)
	}
	for _, source := range []string{
		`<svg width="0%" height="100%" viewBox="0 0 100 100"/>`,
		`<svg viewBox="0 0 100 100"><defs><linearGradient id="a" gradientTransform="scale(1000) scale(1000)"/></defs></svg>`,
		`<svg viewBox="0 0 100 100"><defs><g id="x"><linearGradient id="paint"/><rect width="10" height="10" fill="url(#paint)"/></g></defs><use href="#x"/><use href="#x"/></svg>`,
	} {
		if _, _, _, err := sanitizeSVG(context.Background(), []byte(source)); err == nil {
			t.Fatal("accepted invalid expanded source", source)
		}
	}
	// Compact aliases must not multiply into an unbounded document.
	var expansion strings.Builder
	expansion.WriteString(`<svg viewBox="0 0 100 100"><defs><path id="a0" d="M0 0L10 10"/>`)
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&expansion, `<g id="a%d"><use href="#a%d"/><use href="#a%d"/></g>`, i, i-1, i-1)
	}
	expansion.WriteString(`</defs><use href="#a12"/></svg>`)
	if _, _, _, err := sanitizeSVG(context.Background(), []byte(expansion.String())); err == nil {
		t.Fatal("unbounded reference expansion")
	}
}

func TestSVGOfflineFallbackBounds(t *testing.T) {
	for _, source := range []string{
		`<svg viewBox="0 0 100 100"><path d="M0 0 L"/></svg>`,
		`<svg viewBox="0 0 100 100"><g transform="scale(2 1)" stroke="white" fill="none"><path d="M0 0L10 10"/></g></svg>`,
		`<svg width="20000" height="20000" viewBox="0 0 1 1"><path d="M0 0L1000000 1000000"/></svg>`,
		`<svg width="20000" height="20000" viewBox="0 0 1 1"><path d="M0 0L1 1" stroke="white" stroke-width="256"/></svg>`,
	} {
		if _, _, _, err := embeddedSVGAsset(context.Background(), []byte(source)); err == nil {
			t.Fatal("accepted unsupported/overflowing raster geometry", source)
		}
	}
}

func TestSVGPPTXCompanionRoundTrip(t *testing.T) {
	id, asset, assetPath, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: pageNumberHide, Elements: []Element{{ID: "vector", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=modern&left=20&top=6&width=130&height=25&stretch=1"}}}}}
	data, report, err := exportPPTXDocument(d)
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	p, report, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	var o pptx.Object
	for _, candidate := range p.Slides[0].Objects {
		if candidate.Kind == "image" {
			o = candidate
		}
	}
	if o.Kind != "image" || o.ImageMIME != "image/png" || !bytes.Equal(o.ImageSVG, asset.Vector.Data) {
		t.Fatalf("lost SVG or PNG companion: kind=%s mime=%s png=%d svg=%d objects=%d", o.Kind, o.ImageMIME, len(o.Image), len(o.ImageSVG), len(p.Slides[0].Objects))
	}
	if _, err := png.Decode(bytes.NewReader(o.Image)); err != nil {
		t.Fatal("invalid PNG fallback", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range archive.File {
		if part.Name != "ppt/slides/slide1.xml" && part.Name != "[Content_Types].xml" && part.Name != "ppt/slides/_rels/slide1.xml.rels" {
			continue
		}
		reader, _ := part.Open()
		raw, _ := io.ReadAll(reader)
		reader.Close()
		if part.Name == "ppt/slides/slide1.xml" && !bytes.Contains(raw, []byte("asvg:svgBlip")) {
			t.Fatal("no SVG DrawingML extension")
		}
		if part.Name == "[Content_Types].xml" && bytes.Count(raw, []byte(`Extension="svg"`)) != 1 {
			t.Fatal("duplicate/missing SVG content type")
		}
		if bytes.Contains(raw, []byte(`TargetMode="External"`)) {
			t.Fatal("external media relationship")
		}
	}
	reopened, report, err := importPPTXDocument(data, "vector.pptx")
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	var reimported DeckAsset
	for _, candidate := range reopened.Slides[0].Elements {
		if candidate.Kind == "image" {
			reimported = reopened.Assets[candidate.AssetID]
		}
	}
	if reimported.Vector == nil || !bytes.Equal(reimported.Vector.Data, asset.Vector.Data) {
		t.Fatal("lost vector after PowerPoint round trip")
	}
	o.ImageSVG = []byte(`<svg width="10" height="10"><script/></svg>`)
	_, warnings, fallback, err := pptxObjectToElement(o, 1, 0, 0)
	if err != nil || fallback == nil || fallback.Vector != nil || len(warnings) != 1 || warnings[0].Code != "svg-source-refused" {
		t.Fatal("unsafe SVG did not fall back", err, warnings)
	}
	if output := os.Getenv("KEYNOPE_SVG_EVIDENCE"); output != "" {
		if err := os.WriteFile(filepath.Join(output, "vector.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSVGUploadCancellationAndDestination(t *testing.T) {
	id, asset, assetPath, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	for _, master := range []bool{false, true} {
		for _, failure := range []string{"cancel", "revision", "mode", "slide"} {
			s := atomicEditorFixture(master)
			revision := s.version
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := nativeEditorAction{Action: "add-element", Kind: "image", ElementData: &Element{Kind: "image", AssetID: id, Path: assetPath}, AssetData: &asset, SceneRevision: &revision, SceneMaster: master, uploadContext: ctx}
			switch failure {
			case "cancel":
				cancel()
			case "revision":
				revision--
			case "mode":
				a.SceneMaster = !master
			case "slide":
				a.Slide = 10
			}
			assertEditorFailureUnchanged(t, s, a)
		}
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("image", "logo.svg")
	file.Write([]byte(ownedSVGLogo))
	form.Close()
	s := newNativeEditorSession("SVG.md", Deck{Slides: []Slide{{PageNumber: pageNumberHide}}})
	r := httptest.NewRequest("POST", "/api/editor/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	s.handleUpload(w, r)
	if w.Code != 200 || len(s.deck.Assets) != 1 || len(s.deck.Slides[0].Elements) != 1 || len(s.undo) != 1 {
		t.Fatal("native SVG upload failed", w.Code, w.Body.String())
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || len(s.deck.Assets) != 0 {
		t.Fatal("SVG upload is not atomic", err)
	}
}

func TestSVGVisualFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_SVG_EVIDENCE")
	if output == "" {
		t.Skip("set KEYNOPE_SVG_EVIDENCE for owned renderer fixtures")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	id, asset, assetPath, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: pageNumberHide, BG: "48;2;20;28;40", Elements: []Element{
		{ID: "vector", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=modern&left=15&top=8&width=100&height=20&stretch=1"},
		{ID: "retro", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=retro&glyph=blocks&left=132&top=8&width=100&height=20&stretch=1"},
		{ID: "masked", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=modern&modern-mask=ellipse&modern-crop=0.1,0,0.1,0&left=20&top=33&width=75&height=18&stretch=1&object-rotation=12"},
		{ID: "adjusted", Kind: "image", AssetID: id, Path: assetPath, Query: "image-style=modern&left=132&top=33&width=100&height=20&stretch=1&brightness=0.6"},
	}}}}
	md, err := serializeDeck("SVG proof.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "vector.md"), md, 0644); err != nil {
		t.Fatal(err)
	}
	// Parse through the actual saved deck path to set media/render dependencies
	// exactly as native and site generation do, not a hand-built scene stub.
	loaded, err := parseDeckData("SVG proof.md", md)
	if err != nil {
		t.Fatal(err)
	}
	html, err := exportHTMLDocument("SVG proof.md", loaded.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "vector.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSVGSanitizerRejectsActiveUnsupportedAndExcessiveContent(t *testing.T) {
	items := map[string]string{
		"script": `<script>alert(1)</script>`, "event": `<rect onload="alert(1)"/>`, "foreign": `<foreignObject/>`,
		"animate": `<animate attributeName="x"/>`, "external-use": `<use href="https://example.invalid/a.svg#x"/>`,
		"file-use": `<use href="file:///etc/passwd"/>`, "data-use": `<use href="data:image/svg+xml,bad"/>`,
		"paint-url": `<rect fill="url(https://example.invalid/x)"/>`, "css-url": `<rect style="fill:url(//example.invalid/x)"/>`,
		"escaped-css": `<rect style="fill:u\72l(#x)"/>`, "stylesheet": `<style>.x{fill:red}</style>`,
		"filters": `<filter id="f"><feGaussianBlur/></filter>`, "text": `<text>Hello</text>`, "image": `<image href="x.png"/>`,
		"missing": `<use href="#missing"/>`, "self": `<defs><g id="a"><use href="#a"/></g></defs><use href="#a"/>`,
		"cycle":          `<defs><g id="a"><use href="#b"/></g><g id="b"><use href="#a"/></g></defs><use href="#a"/>`,
		"gradient-cycle": `<defs><linearGradient id="a" href="#b"/><linearGradient id="b" href="#a"/></defs>`,
		"duplicate-id":   `<rect id="x"/><circle id="x"/>`, "namespace": `<rect xmlns="https://example.invalid/foreign"/>`,
		"huge-coordinate": `<path d="M0 0 L1e100 10"/>`, "invalid-path": `<path d="M NaN 1"/>`,
		"huge-scale": `<g transform="scale(1000)"><g transform="scale(1000)"><rect width="10" height="10"/></g></g>`,
		"huge-skew":  `<g transform="skewX(90)"><rect/></g>`, "zero-dash": `<path d="M0 0L10 10" stroke-dasharray="0 0"/>`,
		"invalid-opacity": `<rect opacity="2"/>`, "nested-svg": `<svg viewBox="0 0 10 10"/>`,
		"group-opacity": `<g opacity=".5"><rect width="10" height="10"/></g>`,
	}
	for name, inside := range items {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := sanitizeSVG(context.Background(), []byte(`<svg width="100" height="100">`+inside+`</svg>`))
			if err == nil {
				t.Fatal("accepted unsafe or unsupported SVG")
			}
		})
	}
	for _, prefix := range []string{`<!DOCTYPE svg [<!ENTITY x "boom">]>`, `<?xml-stylesheet href="https://example.invalid/style.css"?>`} {
		if _, _, _, err := sanitizeSVG(context.Background(), []byte(prefix+`<svg width="100" height="100"/>`)); err == nil {
			t.Fatal("accepted DTD/processing instruction")
		}
	}
	for _, box := range []string{"0 0 0 10", "0 0 .00001 10", "0 0 1000000 10"} {
		if _, _, _, err := sanitizeSVG(context.Background(), []byte(`<svg viewBox="`+box+`"/>`)); err == nil {
			t.Fatal("accepted invalid viewport")
		}
	}
	deep := `<svg width="100" height="100">` + strings.Repeat(`<g>`, 40) + strings.Repeat(`</g>`, 40) + `</svg>`
	if _, _, _, err := sanitizeSVG(context.Background(), []byte(deep)); err == nil {
		t.Fatal("accepted deep SVG")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := sanitizeSVG(ctx, []byte(ownedSVGLogo)); err != context.Canceled {
		t.Fatal("cancelled parse did not stop", err)
	}
}

func TestSVGRejectsAmbiguousAttributesAndAggregateTransforms(t *testing.T) {
	invalid := []string{
		`<svg width="100" height="100"><rect style="fill:url(https://example.invalid/PRIVATE)" style="fill:red"/></svg>`,
		`<svg width="100" height="100" version="1" version="2"/>`,
		`<svg width="100" height="100"><metadata data-private="one" data-private="two"/></svg>`,
		`<svg width="100" height="100"><rect xmlns:a="http://www.w3.org/1999/xlink" xmlns:b="http://www.w3.org/1999/xlink" a:href="#a" b:href="#b"/></svg>`,
	}
	identity := strings.Repeat("matrix(1 0 0 1 0 0) ", 16)
	// Individually legal transforms must still share the document-wide budget.
	invalid = append(invalid, `<svg width="100" height="100">`+strings.Repeat(`<g transform="`+identity+`"/>`, 690)+`</svg>`)
	for i, source := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if data, _, _, err := sanitizeSVG(context.Background(), []byte(source)); err == nil || len(data) != 0 {
				t.Fatal("ambiguous/oversized SVG admitted", err)
			}
			for _, master := range []bool{false, true} {
				s := atomicEditorFixture(master)
				before, version, history := cloneDeck(s.deck), s.version, len(s.undo)
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				file, err := form.CreateFormFile("image", "unsafe.svg")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write([]byte(source)); err != nil {
					t.Fatal(err)
				}
				if err := form.Close(); err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest("POST", "/api/editor/upload", &body)
				r.Header.Set("Content-Type", form.FormDataContentType())
				w := httptest.NewRecorder()
				s.handleUpload(w, r)
				if w.Code != 400 || !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != history {
					t.Fatal("rejected SVG changed editor", master, w.Code)
				}
				if strings.Contains(w.Body.String(), "PRIVATE") {
					t.Fatal("SVG diagnostic exposed original external URL")
				}
			}
		})
	}
	// Stay below the aggregate limit with the same per-element complexity.
	valid := `<svg width="100" height="100">` + strings.Repeat(`<g transform="`+identity+`"/>`, 680) + `</svg>`
	if _, _, _, err := sanitizeSVG(context.Background(), []byte(valid)); err != nil {
		t.Fatal("valid bounded transforms rejected", err)
	}
}

func TestSVGAssetPortabilityAndRendering(t *testing.T) {
	id, asset, path, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateClipboardAsset(asset); err != nil {
		t.Fatal(err)
	}
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: pageNumberHide, Elements: []Element{{ID: "logo", Kind: "image", AssetID: id, Path: path, Query: "top=3&left=15&width=100&height=20&stretch=1&image-style=modern"}}}}}
	owned := cloneDeck(d)
	owned.Assets[id].Vector.Data[0] ^= 1
	if owned.Assets[id].Vector.Data[0] == d.Assets[id].Vector.Data[0] {
		t.Fatal("owned clone shares vector bytes")
	}
	render := cloneDeckForRender(d)
	if &render.Assets[id].Vector.Data[0] != &d.Assets[id].Vector.Data[0] {
		t.Fatal("render snapshot needlessly copies immutable SVG bytes")
	}
	data, err := serializeDeck("SVG.md", d)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := parseDeckData("SVG.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reloaded.Assets[id].Vector.Data, asset.Vector.Data) {
		t.Fatal("SVG source lost on save/reopen")
	}
	scene, err := buildDocumentSlideScene(reloaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 || !strings.HasPrefix(scene.Objects[0].Media.Source, "data:image/svg+xml;base64,") {
		t.Fatal("modern scene did not retain vector source")
	}
	encoded := strings.TrimPrefix(scene.Objects[0].Media.Source, "data:image/svg+xml;base64,")
	source, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.Equal(source, asset.Vector.Data) {
		t.Fatal("scene vector changed", err)
	}
	reloaded.Slides[0].Elements[0].Query = setQueryValue(reloaded.Slides[0].Elements[0].Query, "image-style", "retro")
	reloaded.Appearance = &DeckAppearance{Version: 3}
	scene, err = buildDocumentSlideScene(reloaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if scene.Objects[0].RetroLines == nil || len(scene.Objects[0].RetroLines.Lines) == 0 {
		t.Fatal("Retro rendering lost SVG fallback")
	}
	forged := asset
	v := *asset.Vector
	v.Data = []byte(`<svg width="100" height="100"><script/></svg>`)
	forged.Vector = &v
	if err := validateClipboardAsset(forged); err == nil {
		t.Fatal("clipboard accepted active SVG")
	}
	if _, err := encodeDeckAssets(map[string]DeckAsset{id: forged}); err == nil {
		t.Fatal("saved active SVG")
	}
	// Canonical metadata must remain ordinary deterministic JSON, not renderer state.
	if _, err := json.Marshal(asset); err != nil {
		t.Fatal(err)
	}
	// Cross-deck paste carries both forms, not the source cache pathname.
	bundle, err := makeObjectClipboard(d, d.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	elements, assets, _, err := prepareObjectClipboardPaste(Deck{}, bundle)
	if err != nil {
		t.Fatal(err)
	}
	pasted := assets[elements[0].AssetID]
	if pasted.Vector == nil || !bytes.Equal(pasted.Vector.Data, asset.Vector.Data) || elements[0].ID == d.Slides[0].Elements[0].ID {
		t.Fatal("clipboard lost owned SVG or fresh identity")
	}
	pasted.Vector.Data[0] ^= 1
	if bundle.Assets[id].Vector.Data[0] != asset.Vector.Data[0] {
		t.Fatal("clipboard paste shares vector source ownership")
	}
}

func TestSVGMasterBackgroundAndPDFProjection(t *testing.T) {
	id, asset, _, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	masters := defaultMasterDeck()
	masters.Base.Slide.BackgroundMedia = &slideBackgroundMedia{AssetID: id, Fit: "contain", Opacity: .6, Query: "image-style=modern&brightness=0.8&modern-crop=0.1,0,0.1,0&modern-mask=ellipse"}
	masters.Base.Slide.BackgroundMediaSet = true
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: masters, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{PageNumber: pageNumberHide, LayoutID: "blank"}}}
	md, err := serializeDeck("SVG background.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("SVG background.md", md)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := buildDocumentSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	b := scene.BackgroundMedia
	if b == nil || !strings.HasPrefix(b.Source, "data:image/svg+xml;base64,") || b.Fit != "contain" || b.Opacity != .6 || b.Mask != "ellipse" || b.Crop == nil || len(b.ColourMatrices) == 0 {
		t.Fatal("SVG background lost source or adjustments", b)
	}
	loaded.Masters.Base.Slide.BackgroundMedia.Query = "image-style=retro&glyph=braille"
	scene, err = buildDocumentSlideScene(loaded, 0, 245, 56)
	if err != nil || scene.BackgroundMedia.RetroLines == nil || len(scene.BackgroundMedia.RetroLines.Lines) == 0 {
		t.Fatal("Retro SVG background lost samples", err)
	}
	d.Masters.Base.Slide.BackgroundMedia.Query = "image-style=modern"
	manifest, projected, err := projectPDFExport("SVG background.md", d, 245, 56)
	if err != nil || len(manifest.Pages) != 1 || !manifest.Pages[0].Quality || !strings.HasPrefix(projected.Pages[0].Scene.BackgroundMedia.Source, "data:image/svg+xml;base64,") {
		t.Fatal("vector PDF scene or quality route missing", err)
	}
}
