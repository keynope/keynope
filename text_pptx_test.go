package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func richTextPPTXFixture(t *testing.T) Deck {
	t.Helper()
	runs := []sceneRun{
		{Text: "Mono ", FontID: "mono", FontSize: 80, Bold: true, Color: "#ff0055"},
		{Text: "C64 ", FontID: "c64", FontSize: 72, Italic: true, Color: "#55aaff"},
		{Text: "Underlined", Underline: true},
	}
	base := "top=12&left=20&width=200&height=24&render=truetype&modern-font=sans&modern-size=60&modern-width=100&fg=%23ffffff&text-align=center&text-valign=middle&modern-line-height=1.25&modern-paragraph-before=0.5&modern-paragraph-after=0.75"
	text := withConvertedRuns(Element{ID: "body", Kind: "text", Query: base + "&link=https%3A%2F%2Fkeynope.sh&object-rotation=5"}, runs)
	label := withConvertedRuns(Element{Kind: "text", Query: base}, runs)
	encoded, err := json.Marshal(shapeLabelData{Text: label.Text, Query: label.Query})
	if err != nil {
		t.Fatal(err)
	}
	shape := Element{ID: "label", Kind: "shape", Text: "[shape:square]", Query: "top=12&left=20&width=200&height=24&shape=square&fg=%23143458&shape-stroke=none"}
	shape.Query = setQueryValue(shape.Query, "shape-label", base64.StdEncoding.EncodeToString(encoded))
	bullet := withConvertedRuns(Element{ID: "list", Kind: "bullet", Query: base}, []sceneRun{
		{Text: "First", FontID: "mono", FontSize: 80, Bold: true},
		{Text: "\n  continuation\nSecond", FontID: "c64", FontSize: 72, Underline: true},
	})
	code := withConvertedRuns(Element{ID: "code", Kind: "code", Query: base}, []sceneRun{
		{Text: "print", FontID: "mono", FontSize: 80, Color: "#ff0055"},
		{Text: "(42)\n", FontID: "c64", FontSize: 72, Italic: true},
		{Text: "return 0", Bold: true, Underline: true, Color: "#aaff55"},
	})
	return Deck{Slides: []Slide{
		{ID: "rich-body", BG: "48;2;12;22;32", BGSet: true, Elements: []Element{text}},
		{ID: "rich-label", BG: "48;2;12;22;32", BGSet: true, Elements: []Element{shape}},
		{ID: "rich-list", BG: "48;2;12;22;32", BGSet: true, Elements: []Element{bullet}},
		{ID: "rich-code", BG: "48;2;12;22;32", BGSet: true, Elements: []Element{code}},
	}}
}

// Compare effective word properties rather than run segmentation or the
// spelling of an inherited property. A break may have a different style but
// every visible rune must keep its font, size, emphasis and concrete colour.
func effectiveTextRunes(text *sceneText, color string) []sceneRun {
	if text == nil {
		return nil
	}
	runs := text.Runs
	if len(text.Paragraphs) > 0 {
		runs = nil
		for i, p := range text.Paragraphs {
			if i > 0 {
				runs = append(runs, sceneRun{Text: "\n"})
			}
			runs = append(runs, p.Runs...)
		}
	}
	var result []sceneRun
	for _, run := range runs {
		if run.FontID == "" {
			run.FontID = text.FontID
		}
		if run.FontSize == 0 {
			run.FontSize = text.Size
		}
		if run.Color == "" {
			run.Color = color
		}
		run.Color = strings.ToLower(run.Color)
		for _, r := range run.Text {
			if r == '\n' {
				result = append(result, sceneRun{Text: "\n"})
				continue
			}
			run.Text = string(r)
			result = append(result, run)
		}
	}
	return result
}

func TestRichTextPPTXPortableEditableRoundTrip(t *testing.T) {
	deck := richTextPPTXFixture(t)
	before := cloneDeck(deck)
	data, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck, before) {
		t.Fatal("export mutated authored rich text")
	}
	for _, warning := range report.Warnings {
		if strings.Contains(warning.Code, "rich-text") || warning.Code == "shape-label-text-approximated" {
			t.Fatal("supported editable word properties were approximated", warning)
		}
	}
	var codePanelLoss bool
	for _, warning := range report.Warnings {
		codePanelLoss = codePanelLoss || warning.Code == "code-container-omitted"
	}
	if !codePanelLoss {
		t.Fatal("code panel omission was not disclosed")
	}
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, slide := range native.Slides {
		var found *pptx.Object
		for j := range slide.Objects {
			if len(slide.Objects[j].Paragraphs) > 0 {
				found = &slide.Objects[j]
				break
			}
		}
		if found == nil || found.FontFace != "Arial" || found.FontSize != 30 || len(found.Paragraphs[0].Runs) < 2 || found.Paragraphs[0].Runs[0].FontFace != "Courier New" || found.Paragraphs[0].Runs[0].FontSize != 40 {
			t.Fatalf("slide %d defaults replaced or text flattened: %#v", i, found)
		}
	}
	imported, _, err := importPPTXDocument(data, "Words.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Slides) != len(deck.Slides) {
		t.Fatal("slide count changed")
	}
	for i := range deck.Slides {
		source, err := buildSlideScene(deck, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := buildSlideScene(imported, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		original := source.Objects[0]
		var rebuilt *sceneObject
		for j := range restored.Objects {
			o := &restored.Objects[j]
			if o.Text != nil || o.Label != nil {
				rebuilt = o
			}
		}
		if rebuilt == nil {
			t.Fatal("missing imported editable object", i)
		}
		originalText, rebuiltText, color, recoveredColor := original.Text, rebuilt.Text, original.Paint.Color, rebuilt.Paint.Color
		if original.Label != nil {
			originalText, rebuiltText = original.Label, rebuilt.Label
			color, recoveredColor = original.LabelPaint.Color, rebuilt.LabelPaint.Color
		}
		want, got := effectiveTextRunes(originalText, color), effectiveTextRunes(rebuiltText, recoveredColor)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("slide %d word styles lost:\nwant %#v\ngot  %#v", i, want, got)
		}
		if rebuiltText.Align != originalText.Align || rebuiltText.Vertical != originalText.Vertical || rebuiltText.Size != originalText.Size || rebuiltText.FontID != originalText.FontID || math.Abs(rebuiltText.LineHeight-originalText.LineHeight) > .001 || math.Abs(rebuiltText.ParagraphBefore-originalText.ParagraphBefore) > .001 || math.Abs(rebuiltText.ParagraphAfter-originalText.ParagraphAfter) > .001 {
			t.Fatal("text box defaults/paragraph settings lost", i, originalText, rebuiltText)
		}
		if original.Link != rebuilt.Link || math.Abs(original.Rotation-rebuilt.Rotation) > .001 || math.Abs(original.Bounds.Width-rebuilt.Bounds.Width) > .001 {
			t.Fatal("formatting changed bounds, rotation or link", i)
		}
	}
	if dir := os.Getenv("KEYNOPE_RICH_PPTX_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "words.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
		for name, d := range map[string]Deck{"source.md": deck, "imported.md": imported} {
			md, err := serializeDeck(name, d)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), md, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRichTextPPTXReportsParagraphVariation(t *testing.T) {
	object := pptx.Object{ID: "mixed", Kind: "text", FontFace: "Arial", FontSize: 30, Color: "#ffffff", TextAlign: "center", Paragraphs: []pptx.TextParagraph{
		{Align: "center", LineHeight: 1.25, Runs: []pptx.TextRun{{Text: "First", Color: "#FFFFFF"}}},
		{Align: "end", Bullet: true, LineHeight: 2, Runs: []pptx.TextRun{{Text: "Second", Color: "#ffffff"}}},
	}}
	element, warnings, err := importPPTXTextRuns(object, Element{Kind: "text", Query: "render=truetype&modern-font=sans&modern-size=60"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || element.Kind != "text" || element.Text != "First\n• Second" {
		t.Fatal("mixed paragraphs lost silently", element, warnings)
	}
}

func TestRichTextPPTXReportsInheritedAlignmentVariation(t *testing.T) {
	for _, tc := range []struct {
		first, second, object string
		warning               bool
	}{
		{"", "center", "", true},
		{"", "start", "center", true},
		{"", "center", "center", false},
		{"left", "start", "", false},
		{"right", "end", "", false},
	} {
		object := pptx.Object{ID: "align", Kind: "text", FontFace: "Arial", FontSize: 24, TextAlign: tc.object, Paragraphs: []pptx.TextParagraph{{Align: tc.first, Runs: []pptx.TextRun{{Text: "First"}}}, {Align: tc.second, Runs: []pptx.TextRun{{Text: "Second"}}}}}
		_, warnings, err := importPPTXTextRuns(object, Element{Kind: "text", Query: "render=truetype&modern-font=sans&modern-size=48"}, 1)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warning := range warnings {
			if warning.Code == "paragraph-style-approximated" {
				found = true
			}
		}
		if found != tc.warning {
			t.Fatal("inherited alignment diagnostic mismatch", tc, warnings)
		}
	}
}

func TestRichTextPPTXMinimumPointSizeDiagnostic(t *testing.T) {
	o, warnings := sceneObjectToPPTX(sceneObject{ID: "tiny", Kind: "text", Bounds: sceneRect{Width: 400, Height: 100}, Paint: scenePaint{Color: "#ffffff"}, Text: &sceneText{Size: 1, FontID: "c64", WidthScale: 1, Runs: []sceneRun{{Text: "Tiny", FontSize: 1}}}})
	if o == nil || o.FontSize != 1 || o.Paragraphs[0].Runs[0].FontSize != 1 || len(warnings) != 1 || warnings[0].Code != "text-size-clamped" {
		t.Fatal("sub-point export not normalized/disclosed", o, warnings)
	}
	var output bytes.Buffer
	if _, err := pptx.Write(context.Background(), &output, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*o}}}}); err != nil {
		t.Fatal("normalized export still invalid", err)
	}
}

func TestRichTextPPTXMaximumParagraphSpacingDiagnostic(t *testing.T) {
	o := pptx.Object{Kind: "text", FontSize: 2000, Text: "Large", Paragraphs: []pptx.TextParagraph{{SpaceBefore: 4, SpaceAfter: 4, Runs: []pptx.TextRun{{Text: "Large"}}}}}
	warnings := normalizePPTXTextSizes(&o)
	if len(warnings) != 1 || warnings[0].Code != "paragraph-spacing-clamped" || o.Paragraphs[0].SpaceBefore != .792 || o.Paragraphs[0].SpaceAfter != .792 {
		t.Fatal("excessive point spacing not normalized/disclosed", o, warnings)
	}
	var output bytes.Buffer
	if _, err := pptx.Write(context.Background(), &output, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{o}}}}); err != nil {
		t.Fatal("normalized spacing still invalid", err)
	}
}
