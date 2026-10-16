package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func tinyPPTXPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPPTXDeckRoundTripKeepsEditableSubset(t *testing.T) {
	imageData := tinyPPTXPNG(t)
	id, asset, path, err := embeddedImageAsset(imageData)
	if err != nil {
		t.Fatal(err)
	}
	deck := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{
		BG: "48;2;1;2;3", BGSet: true,
		Elements: []Element{
			{ID: "title", Kind: "text", Text: "Hello PowerPoint", Query: "render=truetype&left=10&top=5&width=120&height=8&modern-font=sans&modern-size=80&fg=%2355aaff&link=https%3A%2F%2Fkeynope.sh"},
			{ID: "shape", Kind: "shape", Text: "[shape:circle]", Query: "shape=circle&left=15&top=22&width=30&height=10&fg=%23ff55aa"},
			{ID: "image", Kind: "image", AssetID: id, Path: path, Query: "image-style=modern&left=170&top=20&width=24&height=16&stretch=1"},
		},
	}}}
	data, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatal("not a pptx zip")
	}
	_ = report
	imported, _, err := importPPTXDocument(data, "example.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Slides) != 1 || len(imported.Slides[0].Elements) < 3 {
		t.Fatalf("lost editable objects: %#v", imported.Slides)
	}
	var foundText, foundImage bool
	for _, element := range imported.Slides[0].Elements {
		if element.Kind == "text" && element.Text == "Hello PowerPoint" {
			values, _ := url.ParseQuery(element.Query)
			foundText = values.Get("modern-size") == "80"
		}
		if element.Kind == "image" && imported.Assets[element.AssetID].MIME != "" {
			foundImage = true
		}
	}
	if !foundText || !foundImage {
		t.Fatalf("unexpected imported deck: %#v", imported)
	}
}

func TestPPTXExportSanitisesActivityData(t *testing.T) {
	private := "KEYNOPE_PRIVATE_ACTIVITY_SENTINEL"
	deck := Deck{Slides: []Slide{{
		Notes:            private,
		Engagement:       &EngagementDefinition{ID: "activity", Kind: "storm", Prompt: private},
		EngagementResult: &EngagementResult{Version: 1, ActivityID: "activity", Kind: "storm", State: map[string]json.RawMessage{"ideas": json.RawMessage(`["` + private + `"]`)}},
		Extra:            &jsonExtensions{"authoring": json.RawMessage(`"` + private + `"`)},
		Elements:         []Element{{ID: "visible", Kind: "text", Text: "Visible", Query: "render=truetype&top=2&left=2"}},
	}}}
	deck.Slides = append(deck.Slides, Slide{ID: "private-tab", TabID: "private-tab", Elements: []Element{{ID: "private-tab-text", Kind: "text", Text: private}}})
	orphan, err := newTableData(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	orphan.Cells[0][0].Text = private
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"unused-private-table": orphan}}
	deck.Extra = &jsonExtensions{"private": json.RawMessage(`"` + private + `"`)}
	before := cloneDeck(deck)
	data, _, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	// ZIP entries are usually compressed. Searching only the outer archive
	// bytes could pass even if a slide/notes/workbook XML part leaked content.
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, part := range archive.File {
		reader, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(decoded, []byte(private)) {
			t.Fatal("private activity/tab/orphan/extension data leaked into package part", part.Name)
		}
		if strings.HasPrefix(part.Name, "ppt/slides/") && bytes.Contains(decoded, []byte("Visible")) {
			visible = true
		}
	}
	if !visible {
		t.Fatal("public slide text was lost while filtering private metadata")
	}
	if !reflect.DeepEqual(deck, before) {
		t.Fatal("public export changed private source archive")
	}
}

func TestPPTXSuggestedName(t *testing.T) {
	if got := pptxSuggestedName("/tmp/Name.pptx"); got != "Name.md" {
		t.Fatalf("got %q", got)
	}
}

func TestPPTXUsesBundledC64FontFamilyName(t *testing.T) {
	if got := pptxFontFace(&sceneText{FontID: "c64"}); got != "Keynope C64" {
		t.Fatalf("C64 export font = %q, want Keynope's embedded font family", got)
	}
}

func TestPPTXKeepsC64TextEditable(t *testing.T) {
	object := sceneObject{
		ID:     "c64",
		Kind:   "text",
		Bounds: sceneRect{X: 10, Y: 20, Width: 480, Height: 120},
		Paint:  scenePaint{Color: "#55AAFF"},
		Text:   &sceneText{FontID: "c64", Size: 48, WidthScale: 1, Align: "start", Vertical: "top", Runs: []sceneRun{{Text: "READY."}}},
	}
	converted, warnings := sceneObjectToPPTX(object)
	if converted == nil || converted.Kind != "text" || converted.FontFace != "Keynope C64" || converted.Text != "READY." {
		t.Fatalf("C64 text was not kept editable: %#v", converted)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected C64 export warnings: %#v", warnings)
	}
}

func TestPPTXC64TextUsesItsAuthoredWidthInstance(t *testing.T) {
	object := sceneObject{
		ID:     "c64-fit",
		Kind:   "text",
		Bounds: sceneRect{X: 10, Y: 20, Width: 360, Height: 90},
		Paint:  scenePaint{Color: "#55AAFF"},
		Text:   &sceneText{FontID: "c64", Size: 135, WidthScale: .5, LineHeight: 1.25, Align: "start", Vertical: "top", Runs: []sceneRun{{Text: "THIS IS A LONG C64 TITLE"}}},
	}
	converted, _ := sceneObjectToPPTX(object)
	if converted == nil {
		t.Fatal("C64 text was omitted")
	}
	if converted.FontFace != "Keynope C64 W50" {
		t.Fatalf("C64 face = %q, want width-specific embedded face", converted.FontFace)
	}
	if converted.FontSize != 67.5 {
		t.Fatalf("C64 font size = %.2fpt, want unchanged authored size 67.5pt", converted.FontSize)
	}
}

func TestPPTXExportsTextBoxAlignment(t *testing.T) {
	object := sceneObject{
		ID:     "aligned",
		Kind:   "text",
		Bounds: sceneRect{X: 10, Y: 20, Width: 480, Height: 120},
		Paint:  scenePaint{Color: "#55AAFF"},
		Text:   &sceneText{FontID: "c64", Size: 48, WidthScale: 1, Align: "justify", Vertical: "bottom", Runs: []sceneRun{{Text: "ALIGNED"}}},
	}
	converted, warnings := sceneObjectToPPTX(object)
	if converted == nil || len(warnings) != 0 {
		t.Fatalf("text was not converted cleanly: %#v %#v", converted, warnings)
	}
	if converted.TextAlign != "justify" || converted.VerticalAlign != "bottom" {
		t.Fatalf("alignment lost before PPTX writing: %#v", converted)
	}
}

func TestPPTXC64WidthInstanceCanBePrepared(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(trueTypeFontBase64))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := pptx.ScaleTrueTypeWidth(data, .5)
	if err != nil {
		t.Fatal(err)
	}
	if len(instance) == 0 || bytes.Equal(instance, data) {
		t.Fatal("C64 width instance was not transformed")
	}
}

func TestPPTXEmbedsEveryUsedC64Width(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "narrow", Kind: "text", Text: "NARROW", Query: "render=truetype&modern-font=c64&modern-size=135&modern-width=50&left=10&top=10&width=80&height=10&text-box=1"},
		{ID: "wide", Kind: "text", Text: "WIDE", Query: "render=truetype&modern-font=c64&modern-size=135&modern-width=140&left=10&top=30&width=80&height=10&text-box=1"},
	}}}}
	// Compare against the unified editor model, where 100% is the C64 optical
	// baseline, not the retired absolute-width interpretation of raw legacy data.
	opened := preparePPTXEditorModel(deck)
	scene, err := buildDocumentSlideScene(opened, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	families := map[string]bool{}
	for _, object := range scene.Objects {
		if object.Text != nil {
			families[pptxC64FontFamily(object.Text.WidthScale)] = true
		}
	}
	if !families["Keynope C64 W20.835"] || !families["Keynope C64 W58.338"] {
		t.Fatal("fixture did not use normalized editor widths", families)
	}
	data, _, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		parts[file.Name] = body
	}
	for family := range families {
		if !bytes.Contains(parts["ppt/presentation.xml"], []byte(`typeface="`+family+`"`)) {
			t.Fatalf("displayed width instance missing from font list: %s", parts["ppt/presentation.xml"])
		}
	}
	if len(parts["ppt/fonts/font1.fntdata"]) == 0 || len(parts["ppt/fonts/font2.fntdata"]) == 0 {
		t.Fatal("each used C64 width must have an embedded editable font payload")
	}
}

func TestPPTXImportRendersLineShapesInsteadOfBoxes(t *testing.T) {
	element, warnings, _, err := pptxObjectToElement(pptx.Object{
		ID:          "rule",
		Kind:        "shape",
		Shape:       "line",
		X:           200,
		Y:           300,
		Width:       800,
		Height:      0,
		Stroke:      "#55AAFF",
		StrokeWidth: 2,
	}, 1, 0, 0)
	if err != nil || len(warnings) != 0 || element.Kind != "shape" {
		t.Fatalf("line was not converted: %#v %#v %v", element, warnings, err)
	}
	values, _ := url.ParseQuery(element.Query)
	if values.Get("fg") != "#55AAFF" || values.Get("shape") != "square" || values.Get("height") == "0" {
		t.Fatalf("line paint or bounds were lost: %q", element.Query)
	}
}

func TestPPTXSceneBoundsKeepFractionalPowerPointPositionsAnchored(t *testing.T) {
	query := pptxSceneBoundsQuery(960.5, 540.5, 480, 270)
	if got := query.Get("left"); got != "122" {
		t.Fatalf("left anchor = %q, want integer floor", got)
	}
	if got := query.Get("top"); got != "28" {
		t.Fatalf("top anchor = %q, want integer floor", got)
	}
	if query.Get("object-offset-x") == "" || query.Get("object-offset-y") == "" {
		t.Fatalf("missing precise offsets: %q", query.Encode())
	}
	if got, want := query.Get("width"), "61.25"; got != want {
		t.Fatalf("width = %q, want %q", got, want)
	}
	if got, want := query.Get("height"), "14"; got != want {
		t.Fatalf("height = %q, want %q", got, want)
	}
}

func TestPPTXImportPreservesModernSceneBackgroundAndPictureBounds(t *testing.T) {
	source := pptx.Presentation{Width: pptx.SlideWidthEMU, Height: pptx.SlideHeightEMU, Slides: []pptx.Slide{{
		Background: "#150027",
		Objects:    []pptx.Object{{ID: "picture", Kind: "image", X: 480, Y: 270, Width: 960, Height: 540, Image: tinyPPTXPNG(t), ImageMIME: "image/png"}},
	}}}
	var encoded bytes.Buffer
	if _, err := pptx.Write(context.Background(), &encoded, source); err != nil {
		t.Fatal(err)
	}
	deck, _, err := importPPTXDocument(encoded.Bytes(), "scene.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if !deck.Slides[0].BGSet {
		t.Fatal("source background was not imported as an authored slide property")
	}
	if got := sceneColor(slideBG(deck.Slides[0])); got != "#150027" {
		t.Fatalf("scene background = %q, want source colour", got)
	}
	q, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
	if q.Get("stretch") != "1" {
		t.Fatalf("imported image must retain its explicit bounds: %q", deck.Slides[0].Elements[0].Query)
	}
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 {
		t.Fatalf("scene objects = %d", len(scene.Objects))
	}
	object := scene.Objects[0]
	if math.Abs(object.Bounds.X-480) > 2 || math.Abs(object.Bounds.Y-270) > 2 || math.Abs(object.Bounds.Width-960) > 2 || math.Abs(object.Bounds.Height-540) > 2 {
		t.Fatalf("picture bounds = %#v, want 480,270 960×540", object.Bounds)
	}
	exported, _, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _, err := importPPTXDocument(exported, "reopened.pptx")
	if err != nil || len(reopened.Slides) != 1 || len(reopened.Slides[0].Elements) != 1 || sceneColor(slideBG(reopened.Slides[0])) != "#150027" {
		t.Fatal("background became a selectable rectangle or changed after export/reimport", err, reopened.Slides)
	}
}

func TestPPTXBlankSlideBackgroundRemainsAProperty(t *testing.T) {
	for _, colour := range []string{"#000000", "#ffffff", "#101820", "#aaff55"} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{{ID: "blank", LayoutID: "blank", PageNumber: pageNumberHide}}}
		d.Masters.Base.Slide.Elements = nil
		d.Slides[0].BG, _ = ansiBG(colour)
		d.Slides[0].BGSet = true
		before := cloneDeck(d)
		raw, _, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		p, _, err := pptx.Read(context.Background(), bytes.NewReader(raw), int64(len(raw)), pptx.Options{})
		if err != nil || len(p.Slides) != 1 || len(p.Slides[0].Objects) != 0 || !strings.EqualFold(p.Slides[0].Background, colour) {
			t.Fatal("blank export lost native colour or added background artwork", err, p)
		}
		loaded, _, err := importPPTXDocument(raw, "blank.pptx")
		if err != nil || len(loaded.Slides[0].Elements) != 0 || !loaded.Slides[0].BGSet || !strings.EqualFold(sceneColor(slideBG(loaded.Slides[0])), colour) || !reflect.DeepEqual(d, before) {
			t.Fatal("blank background was not portable/source-immutable", err, loaded.Slides)
		}
	}
}

func TestPPTXImportMakesPowerPointObjectIDsDocumentUnique(t *testing.T) {
	source := pptx.Presentation{Width: pptx.SlideWidthEMU, Height: pptx.SlideHeightEMU, Slides: []pptx.Slide{
		{Objects: []pptx.Object{{ID: "3", Kind: "text", X: 100, Y: 100, Width: 200, Height: 100, Text: "First"}}},
		{Objects: []pptx.Object{{ID: "3", Kind: "text", X: 100, Y: 100, Width: 200, Height: 100, Text: "Second"}}},
	}}
	var encoded bytes.Buffer
	if _, err := pptx.Write(context.Background(), &encoded, source); err != nil {
		t.Fatal(err)
	}
	deck, _, err := importPPTXDocument(encoded.Bytes(), "duplicate-ids.pptx")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for slideIndex, slide := range deck.Slides {
		for _, element := range slide.Elements {
			if seen[element.ID] {
				t.Fatalf("slide %d repeated imported ID %q", slideIndex+1, element.ID)
			}
			seen[element.ID] = true
		}
	}
	if !seen["pptx-s1-o1"] || !seen["pptx-s2-o1"] {
		t.Fatalf("unexpected imported IDs: %#v", seen)
	}
}

func TestPPTXImportSerializesMediaAndShapesAsTheirOwnKinds(t *testing.T) {
	data := tinyPPTXPNG(t)
	assetID, asset, path, err := embeddedImageAsset(data)
	if err != nil {
		t.Fatal(err)
	}
	deck := Deck{Assets: map[string]DeckAsset{assetID: asset}, Slides: []Slide{{Elements: []Element{
		{ID: "image", Kind: "image", AssetID: assetID, Path: path, Query: "image-style=modern&left=2&top=3&width=10&height=8"},
		{ID: "shape", Kind: "shape", Text: "[shape:square]", Query: "shape=square&fg=%2355aaff&left=20&top=3&width=10&height=8"},
	}}}}
	markdown, err := serializeDeck("imported.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(markdown), "kind=image") || strings.Contains(string(markdown), "kind=shape") {
		t.Fatalf("non-text elements were incorrectly encoded as TrueType text: %s", markdown)
	}
	parsed, err := parseDeckData("imported.md", markdown)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || len(parsed.Slides[0].Elements) != 2 || parsed.Slides[0].Elements[0].Kind != "image" || parsed.Slides[0].Elements[1].Kind != "shape" {
		t.Fatalf("media/shape import did not survive Markdown transport: %#v", parsed.Slides)
	}
}
