package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTableThemeProvenanceKeepsEqualConcreteWordOverrides(t *testing.T) {
	r := objectProvenanceResolver()
	cell := parseTableCell(provenanceNode(t, `<tc><txBody><p><pPr><defRPr sz="2400"><solidFill><schemeClr val="tx1"/></solidFill><latin typeface="+mn-lt"/></defRPr></pPr>
<r><t>Inherited</t></r>
<r><rPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Minor"/></rPr><t>Concrete same values</t></r>
<r><rPr><solidFill><schemeClr val="accent1"><shade val="100000"/></schemeClr></solidFill><latin typeface="Minor"/></rPr><t>Transformed same values</t></r>
<r><rPr><solidFill><schemeClr val="accent1"/></solidFill><latin typeface="+mn-lt"/></rPr><t>Same live references</t></r>
</p></txBody><tcPr><solidFill><schemeClr val="accent2"/></solidFill><lnL><solidFill><srgbClr val="123456"/></solidFill></lnL><lnT><solidFill><schemeClr val="accent1"/></solidFill></lnT></tcPr></tc>`), r, func(string, string) {})
	if cell.ColorThemeSlot != "accent1" || cell.ColorThemeSource != r.colourPart || cell.FontThemeRole != "minor" || cell.FontThemeSource != r.fontPart || cell.FillThemeSlot != "accent2" || cell.FillThemeSource != r.colourPart {
		t.Fatal("cell default/fill ownership lost", cell)
	}
	for _, i := range []int{0, 3} {
		run := cell.Runs[i]
		if run.Color != "" || run.FontFace != "" || run.ColorThemeSlot != "accent1" || run.ColorThemeSource != r.colourPart || run.FontThemeRole != "minor" || run.FontThemeSource != r.fontPart {
			t.Fatal("matching inherited source did not remain sparse", i, run)
		}
	}
	for _, i := range []int{1, 2} {
		run := cell.Runs[i]
		if run.Color != "#123456" || run.FontFace != "Minor" || run.ColorThemeSlot != "" || run.ColorThemeSource != "" || run.FontThemeRole != "" || run.FontThemeSource != "" {
			t.Fatal("equal explicit/transformed word lost independent formatting", i, run)
		}
	}
	if cell.BorderThemeSlot != "" || cell.Borders["left"].ColorThemeSlot != "" || cell.Borders["top"].ColorThemeSlot != "accent1" || cell.Borders["top"].ColorThemeSource != r.colourPart {
		t.Fatal("independent edge references collapsed", cell.Borders)
	}
}

func TestTableFirstWordFallbackDoesNotInventCellThemeOwnership(t *testing.T) {
	r := objectProvenanceResolver()
	cell := parseTableCell(provenanceNode(t, `<tc><txBody><p><r><rPr><solidFill><schemeClr val="accent1"/></solidFill><latin typeface="+mj-lt"/></rPr><t>First word</t></r><r><t>Other word</t></r></p></txBody><tcPr/></tc>`), r, func(string, string) {})
	if cell.Color != "#123456" || cell.FontFace != "Major" || cell.ColorThemeSlot != "" || cell.FontThemeRole != "" || cell.ColorThemeSource != "" || cell.FontThemeSource != "" {
		t.Fatal("legacy concrete first-word fallback claimed cell ownership", cell)
	}
	if run := cell.Runs[0]; run.Color != "#123456" || run.FontFace != "Major" || run.ColorThemeSlot != "accent1" || run.FontThemeRole != "major" {
		t.Fatal("source word lost explicit live formatting", run)
	}
	if run := cell.Runs[1]; run.ColorThemeSlot != "" || run.FontThemeRole != "" {
		t.Fatal("unstyled word rebound from first word", run)
	}
}

func TestTableParagraphOverrideAndNoFillClearDonorSource(t *testing.T) {
	r := objectProvenanceResolver()
	cell := parseTableCell(provenanceNode(t, `<tc><txBody><p><pPr><defRPr><solidFill><schemeClr val="accent1"/></solidFill><latin typeface="+mn-lt"/></defRPr></pPr><r><t>Default</t></r></p>
<p><pPr><defRPr><solidFill><srgbClr val="123456"/></solidFill><latin typeface="Minor"/></defRPr></pPr><r><t>Concrete paragraph</t></r></p>
<p><r><t>No paragraph default</t></r></p>
<p><r><rPr><noFill/></rPr><t>No fill</t></r></p>
</txBody><tcPr><noFill/><lnL><noFill/></lnL></tcPr></tc>`), r, func(string, string) {})
	var concrete, none, absent TextRun
	for _, run := range cell.Runs {
		if run.Text == "Concrete paragraph" {
			concrete = run
		}
		if run.Text == "No fill" {
			none = run
		}
		if run.Text == "No paragraph default" {
			absent = run
		}
	}
	if concrete.Color != "#123456" || concrete.FontFace != "Minor" || concrete.ColorThemeSlot != "" || concrete.FontThemeRole != "" {
		t.Fatal("paragraph override inherited cell source", concrete)
	}
	if absent.Color != "#123456" || absent.FontFace != "Minor" || absent.ColorThemeSlot != "" || absent.FontThemeRole != "" {
		t.Fatal("missing paragraph properties invented first-paragraph source ownership", absent)
	}
	if none.Color != "none" || none.ColorThemeSlot != "" || none.ColorThemeSource != "" || cell.FillThemeSlot != "" || cell.Borders["left"].ColorThemeSlot != "" {
		t.Fatal("None paint retained source reference", none, cell)
	}
}

func TestReadTableThemeProvenanceInProductionPackage(t *testing.T) {
	data := &Table{Rows: []float64{100}, Columns: []float64{300}, Cells: [][]TableCell{{{Text: "Editable", FontFace: "Arial", FontSize: 24, Color: "#112233", Fill: "#445566", BorderColor: "#112233", BorderWidth: 2, Padding: 10}}}}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{{ID: "table", Kind: "table", Width: 300, Height: 100, Table: data}}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, value := range packageParts(t, output.Bytes()) {
		parts[name] = string(value)
	}
	xml := parts["ppt/slides/slide1.xml"]
	for _, pair := range [][2]string{{`<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent1"/>`}, {`<a:srgbClr val="445566"/>`, `<a:schemeClr val="accent2"/>`}, {`typeface="Arial"`, `typeface="+mn-lt"`}} {
		xml = strings.ReplaceAll(xml, pair[0], pair[1])
	}
	parts["ppt/slides/slide1.xml"] = xml
	encoded := testPPTX(t, parts)
	p, _, err := Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	cell := p.Slides[0].Objects[0].Table.Cells[0][0]
	owner := "ppt/theme/theme1.xml"
	if cell.ColorThemeSlot != "accent1" || cell.ColorThemeSource != owner || cell.FontThemeRole != "minor" || cell.FontThemeSource != owner || cell.FillThemeSlot != "accent2" || cell.FillThemeSource != owner || cell.BorderThemeSlot != "accent1" || cell.BorderThemeSource != owner {
		t.Fatal("package cell source references lost", cell)
	}
	if cell.Runs[0].ColorThemeSlot != "accent1" || cell.Runs[0].FontThemeRole != "minor" {
		t.Fatal("package word source references lost", cell.Runs)
	}
	var again bytes.Buffer
	if _, err := Write(context.Background(), &again, p); err != nil {
		t.Fatal(err)
	}
	read, _, err := Read(context.Background(), bytes.NewReader(again.Bytes()), int64(again.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := read.Slides[0].Objects[0].Table.Cells[0][0]
	if got.Text != cell.Text || got.Color != cell.Color || got.Fill != cell.Fill || got.FontFace != cell.FontFace || got.BorderColor != cell.BorderColor || got.ColorThemeSlot != "" || got.FontThemeRole != "" || got.FillThemeSlot != "" || got.BorderThemeSlot != "" {
		t.Fatal("concrete table writer changed paint or fabricated references", got)
	}
}

func TestMergedTablePerimeterRetainsIndependentThemeSources(t *testing.T) {
	r := objectProvenanceResolver()
	table := tableFixture()
	xml := tableXML(Object{ID: "table", Kind: "table", Width: 480, Height: 370, Table: table}, 2, "")
	xml = strings.ReplaceAll(xml, `<a:srgbClr val="55AAFF"/>`, `<a:schemeClr val="accent1"/>`)
	n := provenanceNode(t, xml)
	parse := func() *Table {
		t.Helper()
		o, _, err := parseTableFrame(n, r)
		if err != nil {
			t.Fatal(err)
		}
		return o.Table
	}
	got := parse()
	for _, rc := range [][2]int{{1, 0}, {2, 1}} {
		border := got.Cells[rc[0]][rc[1]].Borders["bottom"]
		if border.ColorThemeSlot != "accent1" || border.ColorThemeSource != r.colourPart {
			t.Fatal("merged perimeter owner missing", rc, border)
		}
	}
	if edge := got.Cells[1][1].Borders["left"]; edge.Color != "none" || edge.ColorThemeSlot != "" || edge.ColorThemeSource != "" {
		t.Fatal("merged interior invented paint ownership", edge)
	}
	rows := children(findFirst(n, "tbl"), "tr")
	covered := children(rows[2], "tc")[1]
	colour := findChild(findChild(findChild(findChild(covered, "tcPr"), "lnB"), "solidFill"), "schemeClr")
	if colour == nil {
		t.Fatal("missing continuation fixture edge")
	}
	*colour = *provenanceNode(t, `<srgbClr val="123456"/>`)
	got = parse()
	concrete, live := got.Cells[2][1].Borders["bottom"], got.Cells[1][0].Borders["bottom"]
	if concrete.Color != live.Color || concrete.ColorThemeSlot != "" || concrete.ColorThemeSource != "" || live.ColorThemeSlot != "accent1" {
		t.Fatal("equal continuation paint collapsed into owner theme reference", concrete, live)
	}
}
