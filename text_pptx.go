package main

import (
	"fmt"
	"strings"

	"keynope/internal/pptx"
)

// DrawingML cannot express text below one point. Keep the package valid and
// disclose the minimum-size adjustment rather than emit a repairable export.
func normalizePPTXTextSizes(object *pptx.Object) []pptx.Warning {
	if object == nil {
		return nil
	}
	clamped, spacingClamped := false, false
	if object.FontSize > 0 && object.FontSize < 1 {
		object.FontSize = 1
		clamped = true
	}
	for i := range object.Paragraphs {
		size := object.FontSize
		if size <= 0 {
			size = 18
		}
		for _, spacing := range []*float64{&object.Paragraphs[i].SpaceBefore, &object.Paragraphs[i].SpaceAfter} {
			if *spacing*size > 1584 {
				*spacing = 1584 / size
				spacingClamped = true
			}
		}
		for j := range object.Paragraphs[i].Runs {
			run := &object.Paragraphs[i].Runs[j]
			if run.FontSize > 0 && run.FontSize < 1 {
				run.FontSize = 1
				clamped = true
			}
		}
	}
	var warnings []pptx.Warning
	if clamped {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "text-size-clamped", Message: "PowerPoint text has a one-point minimum; smaller text was raised to one point."})
	}
	if spacingClamped {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "paragraph-spacing-clamped", Message: "Paragraph spacing exceeded PowerPoint's 1584-point limit and was reduced."})
	}
	return warnings
}

func sceneTextToPPTXParagraphs(text *sceneText) []pptx.TextParagraph {
	if text == nil {
		return nil
	}
	convert := func(run sceneRun) pptx.TextRun {
		out := pptx.TextRun{Text: run.Text, Bold: run.Bold, Italic: run.Italic, Underline: run.Underline, Color: run.Color}
		if run.FontID != "" {
			font := *text
			font.FontID = run.FontID
			out.FontFace = pptxFontFace(&font)
		}
		if run.FontSize > 0 {
			out.FontSize = run.FontSize * pptxPointsPerSceneUnit
		}
		return out
	}
	paragraph := func(bullet bool) pptx.TextParagraph {
		return pptx.TextParagraph{Align: text.Align, Bullet: bullet, LineHeight: text.LineHeight, SpaceBefore: text.ParagraphBefore, SpaceAfter: text.ParagraphAfter}
	}
	out := []pptx.TextParagraph{}
	if len(text.Paragraphs) > 0 {
		for _, p := range text.Paragraphs {
			converted := paragraph(p.Bullet)
			for _, run := range p.Runs {
				converted.Runs = append(converted.Runs, convert(run))
			}
			out = append(out, converted)
		}
		return out
	}
	out = append(out, paragraph(false))
	for _, run := range text.Runs {
		for i, part := range strings.Split(run.Text, "\n") {
			if i > 0 {
				out = append(out, paragraph(false))
			}
			if part != "" {
				converted := convert(run)
				converted.Text = part
				out[len(out)-1].Runs = append(out[len(out)-1].Runs, converted)
			}
		}
	}
	return out
}

// Existing body/label query and geometry remain owned by the object. Import
// typed native runs with canonical defaults, and report unsupported paragraph
// variation rather than flattening word fonts silently.
func importPPTXTextRuns(object pptx.Object, element Element, scale float64, bindings ...*pptxThemeBinding) (Element, []pptx.Warning, error) {
	if len(object.Paragraphs) == 0 {
		return element, nil, nil
	}
	warnings := []pptx.Warning{}
	allBullets, anyBullets := true, false
	for _, p := range object.Paragraphs {
		allBullets = allBullets && p.Bullet
		anyBullets = anyBullets || p.Bullet
	}
	// A shape label is plain text: it cannot own the bullet element kind.
	if allBullets && element.Kind == "text" && object.Kind == "text" {
		element.Kind = "bullet"
	}
	nativeBullets := element.Kind == "bullet"
	q := mustQuery(element.Query)
	binding := pptxBinding(bindings)
	if binding != nil {
		freezePPTXTextDefaults(q)
	}
	first := object.Paragraphs[0]
	paragraphAlign := func(p pptx.TextParagraph) string {
		align := p.Align
		if align == "" {
			align = object.TextAlign
		}
		switch align {
		case "", "left":
			return "start"
		case "right":
			return "end"
		default:
			return align
		}
	}
	if first.LineHeight > 0 {
		q.Set("modern-line-height", decimal(first.LineHeight))
	}
	if first.SpaceBefore > 0 {
		q.Set("modern-paragraph-before", decimal(first.SpaceBefore))
	}
	if first.SpaceAfter > 0 {
		q.Set("modern-paragraph-after", decimal(first.SpaceAfter))
	}
	var runs []sceneRun
	var plainSource strings.Builder
	for index, p := range object.Paragraphs {
		if paragraphAlign(p) != paragraphAlign(first) || p.LineHeight != first.LineHeight || p.SpaceBefore != first.SpaceBefore || p.SpaceAfter != first.SpaceAfter {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "paragraph-style-approximated", Message: "Paragraph-specific alignment or spacing uses the first paragraph's text-box defaults."})
		}
		if index > 0 {
			runs = append(runs, sceneRun{Text: "\n"})
		}
		if anyBullets && !nativeBullets && p.Bullet {
			runs = append(runs, sceneRun{Text: "• "})
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "mixed-list-paragraphs-approximated", Message: "Mixed list/text paragraphs retain bullet markers as editable text."})
		}
		for _, run := range p.Runs {
			converted := sceneRun{Text: run.Text, Bold: run.Bold, Italic: run.Italic, Underline: run.Underline, Color: strings.ToLower(run.Color)}
			if strings.EqualFold(run.Color, object.Color) {
				converted.Color = ""
			}
			if run.FontFace != "" && run.FontFace != object.FontFace {
				converted.FontID = importFont(run.FontFace)
			}
			if run.FontSize > 0 && run.FontSize != object.FontSize {
				converted.FontSize = run.FontSize * 2 * scale
				if converted.FontSize > 1024 {
					converted.FontSize = 1024
					warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "run-font-size-clamped", Message: "A word font size exceeded the supported 1024 scene-pixel limit."})
				}
			}
			if nativeBullets {
				converted.Text = strings.ReplaceAll(converted.Text, "\n", "\n  ")
			}
			converted = binding.run(object, run, converted)
			runs = append(runs, converted)
		}
	}
	for _, run := range runs {
		plainSource.WriteString(run.Text)
	}
	element.Query = q.Encode()
	element = withConvertedRuns(element, runs)
	plainElement := element
	if plainElement.Kind == "bullet" {
		plainElement.Kind = "text"
	}
	var recovered strings.Builder
	for _, run := range sceneTextFor(plainElement).Runs {
		recovered.WriteString(run.Text)
	}
	// withConvertedRuns is also used by legacy conversion paths that may
	// omit unsupported metadata. Import must instead fail, never silently
	// discard a valid run override because the encoded document is too large.
	if _, err := encodeModernRuns(element.Text, q.Get("ttf-weight"), element.Kind, runs); err != nil {
		return Element{}, warnings, err
	}
	if recovered.String() != plainSource.String() {
		return Element{}, warnings, fmt.Errorf("PowerPoint text formatting could not be retained")
	}
	return element, warnings, nil
}
