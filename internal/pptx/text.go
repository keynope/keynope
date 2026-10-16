package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Paragraphs retain editable word formatting. Sizes are points; spacing uses
// a multiple of the object default font size, matching Keynope's text model.
type TextParagraph struct {
	Runs                    []TextRun
	Align                   string
	Bullet                  bool
	LineHeight              float64
	SpaceBefore, SpaceAfter float64
}

func validXMLText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0xfffe || r == 0xffff) {
			return false
		}
	}
	return true
}

func validateObjectText(object Object) error {
	if object.Kind != "text" && object.Kind != "shape" {
		return nil
	}
	if !validXMLText(object.Text) || len(object.Text) > 100000 || len(object.Paragraphs) > 10000 || !validXMLText(object.FontFace) || len(object.FontFace) > 128 || !validTextPointSize(object.FontSize) || !tableColorValid(object.Color) || object.Color == "none" {
		return fmt.Errorf("PowerPoint text exceeds supported content/style bounds")
	}
	count, bytes := 0, 0
	for _, p := range object.Paragraphs {
		if p.Align != "" && p.Align != "start" && p.Align != "left" && p.Align != "center" && p.Align != "end" && p.Align != "right" && p.Align != "justify" {
			return fmt.Errorf("unsupported text paragraph alignment")
		}
		for _, v := range []float64{p.LineHeight, p.SpaceBefore, p.SpaceAfter} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 4 {
				return fmt.Errorf("invalid text paragraph spacing")
			}
		}
		size := object.FontSize
		if size == 0 {
			size = 18
		}
		if math.Round(p.SpaceBefore*size*100) > 158400 || math.Round(p.SpaceAfter*size*100) > 158400 {
			return fmt.Errorf("text paragraph spacing exceeds DrawingML's 1584-point limit")
		}
		for _, run := range p.Runs {
			count++
			bytes += len(run.Text)
			if count > 10000 || bytes > 100000 || !validXMLText(run.Text) || !validXMLText(run.FontFace) || len(run.FontFace) > 128 || !validTextPointSize(run.FontSize) || !tableColorValid(run.Color) || run.Color == "none" {
				return fmt.Errorf("invalid or oversized PowerPoint text runs")
			}
		}
	}
	if len(object.Paragraphs) > 0 && paragraphPlainText(object.Paragraphs) != object.Text {
		return fmt.Errorf("PowerPoint paragraph runs do not match plain text")
	}
	return nil
}

func validTextPointSize(size float64) bool {
	// Zero means inherit. DrawingML's explicit font-size range is 1–4000pt.
	return size == 0 || !math.IsNaN(size) && !math.IsInf(size, 0) && size >= 1 && size <= 4000
}

func paragraphPlainText(paragraphs []TextParagraph) string {
	var b strings.Builder
	for i, p := range paragraphs {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, run := range p.Runs {
			b.WriteString(run.Text)
		}
	}
	return b.String()
}

func textBody(o Object) string {
	if len(o.Paragraphs) == 0 {
		return legacyTextBody(o)
	}
	face, size, color := o.FontFace, o.FontSize, o.Color
	if face == "" {
		face = "Arial"
	}
	if size <= 0 {
		size = 18
	}
	if color == "" {
		color = "#000000"
	}
	anchor := "t"
	if o.VerticalAlign == "middle" {
		anchor = "ctr"
	} else if o.VerticalAlign == "bottom" {
		anchor = "b"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<p:txBody><a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0" anchor="%s"/><a:lstStyle/>`, anchor)
	for _, p := range o.Paragraphs {
		align := p.Align
		if align == "" {
			align = o.TextAlign
		}
		switch align {
		case "center":
			align = "ctr"
		case "right", "end":
			align = "r"
		case "justify":
			align = "just"
		default:
			align = "l"
		}
		indent := ""
		if p.Bullet {
			indent = ` marL="342900" indent="-285750"`
		}
		fmt.Fprintf(&b, `<a:p><a:pPr algn="%s"%s>`, align, indent)
		if p.LineHeight > 0 {
			fmt.Fprintf(&b, `<a:lnSpc><a:spcPct val="%d"/></a:lnSpc>`, int(math.Round(p.LineHeight*100000)))
		}
		for _, item := range []struct {
			tag   string
			value float64
		}{{"spcBef", p.SpaceBefore}, {"spcAft", p.SpaceAfter}} {
			if item.value > 0 {
				fmt.Fprintf(&b, `<a:%s><a:spcPts val="%d"/></a:%s>`, item.tag, int(math.Round(item.value*size*100)), item.tag)
			}
		}
		if p.Bullet {
			b.WriteString(`<a:buChar char="•"/>`)
		} else {
			b.WriteString(`<a:buNone/>`)
		}
		fmt.Fprintf(&b, `<a:defRPr sz="%d" b="%d" i="%d">%s<a:latin typeface="%s"/></a:defRPr></a:pPr>`, int(math.Round(size*100)), boolNumber(o.Bold), boolNumber(o.Italic), textRunPaintXML(color, o), escapeAttr(face))
		for _, run := range p.Runs {
			rf, rs, rc := run.FontFace, run.FontSize, run.Color
			if rf == "" {
				rf = face
			}
			if rs <= 0 {
				rs = size
			}
			if rc == "" {
				rc = color
			}
			underline := "none"
			if run.Underline {
				underline = "sng"
			}
			for i, part := range strings.Split(run.Text, "\n") {
				if i > 0 {
					b.WriteString(`<a:br/>`)
				}
				if part == "" {
					continue
				}
				fmt.Fprintf(&b, `<a:r><a:rPr lang="en-US" sz="%d" b="%d" i="%d" u="%s">%s<a:latin typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`, int(math.Round(rs*100)), boolNumber(run.Bold), boolNumber(run.Italic), underline, textRunPaintXML(rc, o), escapeAttr(rf), escapeText(part))
			}
		}
		b.WriteString(`</a:p>`)
	}
	b.WriteString(`</p:txBody>`)
	return b.String()
}

// Overlay only present attributes; explicit b="0" must beat inherited bold.
func overlayTextRun(base TextRun, pr *node, resolver colourResolver) TextRun {
	style, _, _ := overlayTextRunProperties(base, pr, resolver)
	return style
}

func overlayTextRunProperties(base TextRun, pr *node, resolver colourResolver) (TextRun, float64, bool) {
	face, size, color, opacity, bold, italic := propertiesFromRun(pr, resolver)
	if face != "" {
		base.FontFace = face
		base.FontThemeRole = latinThemeRole(attr(findChild(pr, "latin"), "typeface"), resolver)
		base.FontThemeSource = themeReferenceSource(base.FontThemeRole, resolver.fontPart)
	}
	if size > 0 {
		base.FontSize = size
	}
	if color != "" {
		base.Color = color
		base.ColorThemeSlot = resolvedThemeColourSlot(findChild(pr, "solidFill"), resolver)
		base.ColorThemeSource = themeReferenceSource(base.ColorThemeSlot, resolver.colourPart)
	} else if hasExplicitShapeFill(pr) {
		// An unsupported or empty explicit paint is not inherited scheme
		// provenance, even when the current concrete reader uses a fallback.
		base.ColorThemeSlot = ""
		base.ColorThemeSource = ""
	}
	if attr(pr, "b") != "" {
		base.Bold = bold
	}
	if attr(pr, "i") != "" {
		base.Italic = italic
	}
	if attr(pr, "u") != "" {
		base.Underline = attr(pr, "u") != "none"
	}
	return base, opacity, color != ""
}

func inheritedTextSources(inherited *inheritedShape) []*node {
	if inherited == nil {
		return nil
	}
	return inherited.textSources
}

func masterTextStyle(n *node, resolver colourResolver) *node {
	style := "otherStyle"
	switch attr(findFirst(n, "ph"), "type") {
	case "title", "ctrTitle":
		style = "titleStyle"
	case "body", "subTitle":
		style = "bodyStyle"
	}
	return findChild(resolver.textStyles, style)
}

func listParagraphDefaults(list *node, level int) []*node {
	return []*node{findChild(list, "defPPr"), findChild(list, fmt.Sprintf("lvl%dpPr", level+1))}
}

func paragraphDefaults(n *node, resolver colourResolver, inherited *inheritedShape, level int) []*node {
	result := listParagraphDefaults(masterTextStyle(n, resolver), level)
	for _, source := range inheritedTextSources(inherited) {
		body := findChild(source, "txBody")
		result = append(result, listParagraphDefaults(findChild(body, "lstStyle"), level)...)
		result = append(result, findChild(findChild(body, "p"), "pPr"))
	}
	return append(result, listParagraphDefaults(findChild(findChild(n, "txBody"), "lstStyle"), level)...)
}

func paragraphRunBase(n *node, resolver colourResolver, inherited *inheritedShape, level int) TextRun {
	base := TextRun{FontFace: "Arial", FontSize: 18, Color: "#000000"}
	applyList := func(base TextRun, list *node) TextRun {
		for _, properties := range listParagraphDefaults(list, level) {
			base = overlayTextRun(base, findChild(properties, "defRPr"), resolver)
		}
		return base
	}
	base = applyList(base, masterTextStyle(n, resolver))
	if inherited != nil && len(inherited.textSources) == 0 {
		old := inherited.object
		base.FontFace, base.FontSize, base.Color = old.FontFace, old.FontSize, old.Color
		base.FontThemeRole, base.ColorThemeSlot = old.FontThemeRole, old.ColorThemeSlot
		base.FontThemeSource, base.ColorThemeSource = old.FontThemeSource, old.ColorThemeSource
		base.Bold, base.Italic = old.Bold, old.Italic
		if base.FontFace == "" {
			base.FontFace = "Arial"
			base.FontThemeRole = ""
			base.FontThemeSource = ""
		}
		if base.FontSize <= 0 {
			base.FontSize = 18
		}
		if base.Color == "" {
			base.Color = "#000000"
			base.ColorThemeSlot = ""
			base.ColorThemeSource = ""
		}
	}
	applySource := func(base TextRun, source *node, donor bool) TextRun {
		if face := shapeThemeFont(source, resolver); face != "" {
			base.FontFace = face
			base.FontThemeRole = attr(findChild(findChild(source, "style"), "fontRef"), "idx")
			base.FontThemeSource = themeReferenceSource(base.FontThemeRole, resolver.fontPart)
		}
		body := findChild(source, "txBody")
		base = applyList(base, findChild(body, "lstStyle"))
		if donor {
			base = overlayTextRun(base, findChild(findChild(findChild(body, "p"), "pPr"), "defRPr"), resolver)
		}
		return base
	}
	for _, source := range inheritedTextSources(inherited) {
		base = applySource(base, source, true)
	}
	return applySource(base, n, false)
}

func parseTextParagraphs(n *node, resolver colourResolver, inherited *inheritedShape) ([]TextParagraph, error) {
	body := findFirst(n, "txBody")
	if body == nil {
		return nil, nil
	}
	paragraphs := []TextParagraph{}
	count, bytes := 0, 0
	for _, source := range children(body, "p") {
		pr := findChild(source, "pPr")
		level := 0
		if raw := attr(pr, "lvl"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || parsed > 8 {
				return nil, fmt.Errorf("invalid PowerPoint paragraph level")
			}
			level = parsed
		}
		base := paragraphRunBase(n, resolver, inherited, level)
		style := overlayTextRun(base, findChild(pr, "defRPr"), resolver)
		p := TextParagraph{}
		for _, properties := range append(paragraphDefaults(n, resolver, inherited, level), pr) {
			if err := overlayParagraphProperties(&p, properties, style.FontSize); err != nil {
				return nil, err
			}
		}
		for _, child := range source.Children {
			if child.Name != "r" && child.Name != "fld" && child.Name != "br" {
				continue
			}
			run := overlayTextRun(style, findChild(child, "rPr"), resolver)
			if child.Name == "br" {
				run.Text = "\n"
			} else if text := findChild(child, "t"); text != nil {
				run.Text = text.Text
			}
			count++
			bytes += len(run.Text)
			if count > 10000 || bytes > 100000 {
				return nil, fmt.Errorf("PowerPoint text exceeds supported run/content bounds")
			}
			p.Runs = append(p.Runs, run)
		}
		paragraphs = append(paragraphs, p)
		if len(paragraphs) > 10000 {
			return nil, fmt.Errorf("PowerPoint text exceeds supported paragraph bounds")
		}
	}
	return paragraphs, nil
}

func overlayParagraphProperties(p *TextParagraph, pr *node, fontSize float64) error {
	switch attr(pr, "algn") {
	case "l":
		p.Align = "start"
	case "ctr":
		p.Align = "center"
	case "r":
		p.Align = "end"
	case "just", "dist":
		p.Align = "justify"
	}
	bulletChoices := len(children(pr, "buChar")) + len(children(pr, "buAutoNum")) + len(children(pr, "buNone"))
	if bulletChoices > 1 {
		return fmt.Errorf("ambiguous paragraph bullet definition")
	}
	if bulletChoices == 1 {
		p.Bullet = findChild(pr, "buNone") == nil
	}
	if v, err := strconv.ParseFloat(attr(findChild(findChild(pr, "lnSpc"), "spcPct"), "val"), 64); err == nil && v > 0 && v <= 400000 {
		p.LineHeight = v / 100000
	}
	for _, item := range []struct {
		tag    string
		target *float64
	}{{"spcBef", &p.SpaceBefore}, {"spcAft", &p.SpaceAfter}} {
		spacing := findChild(pr, item.tag)
		if v, err := strconv.ParseFloat(attr(findChild(spacing, "spcPts"), "val"), 64); err == nil && v >= 0 && v <= 400*fontSize {
			*item.target = v / (100 * fontSize)
		} else if v, err := strconv.ParseFloat(attr(findChild(spacing, "spcPct"), "val"), 64); err == nil && v >= 0 && v <= 400000 {
			*item.target = v / 100000
		}
	}
	return nil
}
