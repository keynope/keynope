package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func objectProvenanceResolver() colourResolver {
	r := defaultColourResolver()
	r.colours["accent1"], r.colours["accent2"] = "#123456", "#abcdef"
	r.mapping["tx1"] = "accent1"
	r.majorFont, r.minorFont = "Major", "Minor"
	r.colourPart, r.fontPart = "ppt/theme/palette.xml", "ppt/theme/typography.xml"
	return r
}

func TestObjectTextDefaultsKeepProvenanceSeparateFromFirstWord(t *testing.T) {
	r := objectProvenanceResolver()
	source := provenanceNode(t, `<sp><spPr/><txBody><p><pPr><defRPr sz="2400"><solidFill><schemeClr val="tx1"/></solidFill><latin typeface="+mn-lt"/></defRPr></pPr><r><rPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Minor"/></rPr><t>Explicit first word</t></r><r><t> inherited</t></r></p></txBody></sp>`)
	o, err := parseShape(source, nil, r, nil, false)
	if err != nil || o == nil {
		t.Fatal(err)
	}
	if o.Color != "#123456" || o.ColorThemeSlot != "accent1" || o.FontFace != "Minor" || o.FontThemeRole != "minor" {
		t.Fatal("object defaults lost source role", o)
	}
	if o.ColorThemeSource != r.colourPart || o.FontThemeSource != r.fontPart {
		t.Fatal("object default owner lost", o)
	}
	if run := o.Paragraphs[0].Runs[0]; run.ColorThemeSlot != "" || run.FontThemeRole != "" {
		t.Fatal("object defaults replaced explicit first word source", run)
	}
	if run := o.Paragraphs[0].Runs[1]; run.ColorThemeSlot != "accent1" || run.FontThemeRole != "minor" {
		t.Fatal("later word lost inherited role", run)
	}
	// Reverse the property ownership: a live first word does not relink the
	// explicitly concrete paragraph default, even when both values coincide.
	source = provenanceNode(t, `<sp><spPr/><txBody><p><pPr><defRPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Minor"/></defRPr></pPr><r><rPr><solidFill><schemeClr val="accent1"/></solidFill><latin typeface="+mn-lt"/></rPr><t>Live word</t></r></p></txBody></sp>`)
	o, err = parseShape(source, nil, r, nil, false)
	if err != nil || o == nil || o.ColorThemeSlot != "" || o.FontThemeRole != "" || o.Paragraphs[0].Runs[0].ColorThemeSlot != "accent1" || o.Paragraphs[0].Runs[0].FontThemeRole != "minor" {
		t.Fatal("first word incorrectly relinked object default", o, err)
	}
}

func TestShapePaintProvenanceRequiresActualUnmodifiedScheme(t *testing.T) {
	r := objectProvenanceResolver()
	parse := func(xml string, parent *inheritedShape) *Object {
		t.Helper()
		o, err := parseShape(provenanceNode(t, xml), nil, r, parent, true)
		if err != nil || o == nil {
			t.Fatal(err)
		}
		return o
	}
	parent := parse(`<sp><spPr><prstGeom prst="rect"/><solidFill><schemeClr val="tx1"/></solidFill><ln w="12700"><solidFill><schemeClr val="accent2"/></solidFill></ln></spPr></sp>`, nil)
	if parent.Fill != "#123456" || parent.FillThemeSlot != "accent1" || !strings.EqualFold(parent.Stroke, "#abcdef") || parent.StrokeThemeSlot != "accent2" {
		t.Fatal("shape paint references lost", parent)
	}
	if parent.FillThemeSource != r.colourPart || parent.StrokeThemeSource != r.colourPart {
		t.Fatal("shape paint source lost", parent)
	}
	donor := &inheritedShape{object: *parent}
	child := parse(`<sp><spPr/></sp>`, donor)
	if child.Fill != parent.Fill || child.FillThemeSlot != "accent1" {
		t.Fatal("inherited fill source lost", child)
	}
	if child.FillThemeSource != parent.FillThemeSource {
		t.Fatal("inherited fill owner changed")
	}
	for _, fill := range []string{`<solidFill><srgbClr val="123456"/></solidFill>`, `<solidFill><schemeClr val="accent1"><shade val="100000"/></schemeClr></solidFill>`, `<noFill/>`} {
		child = parse(`<sp><spPr>`+fill+`<ln><solidFill><srgbClr val="abcdef"/></solidFill></ln></spPr></sp>`, donor)
		if child.FillThemeSlot != "" || child.StrokeThemeSlot != "" {
			t.Fatal("concrete/transformed/None paint kept donor role", child)
		}
		if child.FillThemeSource != "" || child.StrokeThemeSource != "" {
			t.Fatal("concrete paint retained donor source", child)
		}
	}
}

func TestLegacyObjectDonorRetainsKnownTextProvenance(t *testing.T) {
	r := objectProvenanceResolver()
	donor := &inheritedShape{object: Object{Kind: "text", FontFace: "Major", FontThemeRole: "major", FontSize: 24, Color: "#123456", ColorThemeSlot: "accent1"}}
	source := provenanceNode(t, `<sp><txBody><lstStyle/><p><r><t>Inherited</t></r></p></txBody></sp>`)
	o, err := parseShape(source, nil, r, donor, false)
	if err != nil || o == nil || o.ColorThemeSlot != "accent1" || o.FontThemeRole != "major" || o.Paragraphs[0].Runs[0].ColorThemeSlot != "accent1" || o.Paragraphs[0].Runs[0].FontThemeRole != "major" {
		t.Fatal("known object donor provenance lost", o, err)
	}
	concrete := provenanceNode(t, `<sp><txBody><p><pPr><defRPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Major"/></defRPr></pPr><r><t>Concrete</t></r></p></txBody></sp>`)
	o, err = parseShape(concrete, nil, r, donor, false)
	if err != nil || o == nil || o.ColorThemeSlot != "" || o.FontThemeRole != "" {
		t.Fatal("concrete donor override did not clear provenance", o, err)
	}
}

func TestConnectorPaintRetainsMappedThemeSlot(t *testing.T) {
	r := objectProvenanceResolver()
	for _, tc := range []struct{ paint, slot string }{{`<schemeClr val="tx1"/>`, "accent1"}, {`<srgbClr val="123456"/>`, ""}, {`<schemeClr val="tx1"><tint val="0"/></schemeClr>`, ""}} {
		o := parseConnector(provenanceNode(t, `<cxnSp><spPr><ln><solidFill>`+tc.paint+`</solidFill></ln></spPr></cxnSp>`), r)
		if o.Stroke != "#123456" || o.StrokeThemeSlot != tc.slot {
			t.Fatal("connector reference differs from actual paint", o)
		}
		if o.StrokeThemeSource != themeReferenceSource(tc.slot, r.colourPart) {
			t.Fatal("connector source owner lost", o)
		}
	}
}

func TestReadShapeThemeProvenanceInProductionPackage(t *testing.T) {
	var output bytes.Buffer
	objects := []Object{{ID: "shape", Kind: "shape", Shape: "rect", Width: 300, Height: 200, Fill: "#112233", Stroke: "#445566", StrokeWidth: 2}, {ID: "line", Kind: "line", X: 400, Y: 300, Width: 200, Height: 100, Stroke: "#445566", StrokeWidth: 2}}
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: objects}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, data := range packageParts(t, output.Bytes()) {
		parts[name] = string(data)
	}
	xml := parts["ppt/slides/slide1.xml"]
	xml = strings.ReplaceAll(xml, `<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent1"/>`)
	xml = strings.ReplaceAll(xml, `<a:srgbClr val="445566"/>`, `<a:schemeClr val="accent2"/>`)
	parts["ppt/slides/slide1.xml"] = xml
	data := testPPTX(t, parts)
	p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slides[0].Objects) != 2 {
		t.Fatal("fixture lost shape/line")
	}
	shape, line := p.Slides[0].Objects[0], p.Slides[0].Objects[1]
	if shape.FillThemeSlot != "accent1" || shape.StrokeThemeSlot != "accent2" || line.StrokeThemeSlot != "accent2" {
		t.Fatal("package shape/line roles lost", shape, line)
	}
	var concrete bytes.Buffer
	if _, err := Write(context.Background(), &concrete, p); err != nil {
		t.Fatal(err)
	}
	got, _, err := Read(context.Background(), bytes.NewReader(concrete.Bytes()), int64(concrete.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range got.Slides[0].Objects {
		before := p.Slides[0].Objects[i]
		if o.Fill != before.Fill || o.Stroke != before.Stroke || o.FillThemeSlot != "" || o.StrokeThemeSlot != "" {
			t.Fatal("concrete writer changed paint or fabricated source links", o)
		}
	}
}
