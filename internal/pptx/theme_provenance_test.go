package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func provenanceNode(t *testing.T, xml string) *node {
	t.Helper()
	n, err := parseXML([]byte(xml), 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTextThemeProvenanceFollowsPropertyCascadeNotMatchingValues(t *testing.T) {
	r := defaultColourResolver()
	r.colours["accent2"] = "#123456"
	r.mapping["tx1"] = "accent2"
	r.majorFont, r.minorFont = "Same Face", "Same Face"
	r.colourPart, r.fontPart = "ppt/theme/colours.xml", "ppt/theme/fonts.xml"
	r.textStyles = provenanceNode(t, `<txStyles><bodyStyle><lvl1pPr><defRPr><solidFill><schemeClr val="tx1"/></solidFill><latin typeface="+mn-lt"/></defRPr></lvl1pPr></bodyStyle></txStyles>`)
	source := provenanceNode(t, `<sp><nvSpPr><nvPr><ph type="body"/></nvPr></nvSpPr><txBody><p>
<r><t>Inherited</t></r>
<r><rPr b="1"/><t>Emphasis only</t></r>
<r><rPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Same Face"/></rPr><t>Identical concrete values</t></r>
<r><rPr><solidFill><schemeClr val="accent2"><shade val="100000"/></schemeClr></solidFill><latin typeface="+mj-lt"/></rPr><t>Transformed scheme</t></r>
<r><rPr><noFill/></rPr><t>No paint</t></r>
<r><rPr><solidFill><schemeClr val="missing"/></solidFill></rPr><t>Unresolved paint</t></r>
</p></txBody></sp>`)
	paragraphs, err := parseTextParagraphs(source, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	runs := paragraphs[0].Runs
	for i, run := range runs {
		if run.Color != "#123456" || run.FontFace != "Same Face" {
			t.Fatal("provenance changed concrete output", i, run)
		}
	}
	for _, i := range []int{0, 1} {
		if runs[i].ColorThemeSlot != "accent2" || runs[i].FontThemeRole != "minor" {
			t.Fatal("inherited source reference lost", i, runs[i])
		}
		if runs[i].ColorThemeSource != r.colourPart || runs[i].FontThemeSource != r.fontPart {
			t.Fatal("inherited independent owners lost", runs[i])
		}
	}
	if runs[2].ColorThemeSlot != "" || runs[2].FontThemeRole != "" {
		t.Fatal("guessed inheritance from equal explicit values", runs[2])
	}
	if runs[2].ColorThemeSource != "" || runs[2].FontThemeSource != "" {
		t.Fatal("explicit values retained stale theme owner", runs[2])
	}
	if runs[3].ColorThemeSlot != "" || runs[3].FontThemeRole != "major" {
		t.Fatal("transformed colour rebound or major role lost", runs[3])
	}
	for _, i := range []int{4, 5} {
		if runs[i].ColorThemeSlot != "" {
			t.Fatal("explicit unrepresented paint kept inherited provenance", runs[i])
		}
	}
}

func TestTextThemeProvenanceFromMasterLayoutAndShapeStyle(t *testing.T) {
	r := defaultColourResolver()
	r.colours["accent1"] = "#abcdef"
	r.colours["accent2"] = "#fedcba"
	r.majorFont = "Major"
	r.minorFont = "Minor"
	master := provenanceNode(t, `<sp><style><fontRef idx="major"/></style><txBody><lstStyle><lvl1pPr><defRPr><solidFill><schemeClr val="accent1"/></solidFill></defRPr></lvl1pPr></lstStyle></txBody></sp>`)
	layout := provenanceNode(t, `<sp><txBody><p><pPr><defRPr><solidFill><schemeClr val="accent2"/></solidFill></defRPr></pPr></p></txBody></sp>`)
	source := provenanceNode(t, `<sp><txBody><p><r><t>Inherited</t></r></p><p><pPr><defRPr><latin typeface="+mn-lt"/></defRPr></pPr><r><t>Local paragraph</t></r></p></txBody></sp>`)
	p, err := parseTextParagraphs(source, r, &inheritedShape{textSources: []*node{master, layout}})
	if err != nil {
		t.Fatal(err)
	}
	if p[0].Runs[0].ColorThemeSlot != "accent2" || p[0].Runs[0].FontThemeRole != "major" || p[1].Runs[0].FontThemeRole != "minor" {
		t.Fatal("source cascade provenance lost", p)
	}
	// A concrete donor font overrides the shape-style role even when identical.
	layout = provenanceNode(t, `<sp><txBody><p><pPr><defRPr><latin typeface="Major"/></defRPr></pPr></p></txBody></sp>`)
	p, err = parseTextParagraphs(source, r, &inheritedShape{textSources: []*node{master, layout}})
	if err != nil || p[0].Runs[0].FontThemeRole != "" || p[0].Runs[0].FontFace != "Major" {
		t.Fatal("concrete layout font did not clear role", p, err)
	}
}

func TestThemeProvenanceRejectsUnresolvedAndTransformedSlots(t *testing.T) {
	r := defaultColourResolver()
	r.colours["accent1"] = "#112233"
	for _, xml := range []string{`<solidFill><srgbClr val="112233"/></solidFill>`, `<solidFill><schemeClr val="missing"/></solidFill>`, `<solidFill><schemeClr val="accent1"><tint val="0"/></schemeClr></solidFill>`, `<solidFill><schemeClr val="accent1"><alpha val="100000"/></schemeClr></solidFill>`} {
		if slot := resolvedThemeColourSlot(provenanceNode(t, xml), r); slot != "" {
			t.Fatal("unsafe live slot", slot)
		}
	}
	if latinThemeRole("+mj-lt", r) != "" || latinThemeRole("Arial", r) != "" {
		t.Fatal("unresolved/literal font claimed live role")
	}
}

func TestReadThemeProvenanceInRealPackageRetainsConcreteExport(t *testing.T) {
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{{ID: "text", Kind: "text", Text: "Theme text", FontFace: "Arial", FontSize: 24, Color: "#112233", Width: 400, Height: 100}}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, data := range packageParts(t, output.Bytes()) {
		parts[name] = string(data)
	}
	parts["ppt/slides/slide1.xml"] = strings.ReplaceAll(parts["ppt/slides/slide1.xml"], `<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent1"/>`)
	parts["ppt/slides/slide1.xml"] = strings.ReplaceAll(parts["ppt/slides/slide1.xml"], `typeface="Arial"`, `typeface="+mj-lt"`)
	data := testPPTX(t, parts)
	read, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	run := read.Slides[0].Objects[0].Paragraphs[0].Runs[0]
	if run.ColorThemeSlot != "accent1" || run.FontThemeRole != "major" || run.Color != "#55AAFF" || run.FontFace != "Arial" {
		t.Fatal("package source reference lost", run)
	}
	object := read.Slides[0].Objects[0]
	if object.ColorThemeSlot != "accent1" || object.FontThemeRole != "major" {
		t.Fatal("package object defaults lost provenance", object)
	}
	var again bytes.Buffer
	if _, err := Write(context.Background(), &again, read); err != nil {
		t.Fatal(err)
	}
	concrete, _, err := Read(context.Background(), bytes.NewReader(again.Bytes()), int64(again.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := concrete.Slides[0].Objects[0].Paragraphs[0].Runs[0]
	if got.Color != run.Color || got.FontFace != run.FontFace || got.ColorThemeSlot != "" || got.FontThemeRole != "" {
		t.Fatal("read-only provenance changed concrete writer behaviour", got)
	}
}
