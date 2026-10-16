package pptx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tableStyleFrameXML(rows, cols int, properties string) string {
	var b strings.Builder
	b.WriteString(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="7" name="Styled table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="635000" y="635000"/><a:ext cx="10160000" cy="5715000"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr ` + properties + `</a:tblPr><a:tblGrid>`)
	for c := range cols {
		w := 10160000 / cols
		if c == cols-1 {
			w += 10160000 % cols
		}
		fmt.Fprintf(&b, `<a:gridCol w="%d"/>`, w)
	}
	b.WriteString(`</a:tblGrid>`)
	for r := range rows {
		h := 5715000 / rows
		if r == rows-1 {
			h += 5715000 % rows
		}
		fmt.Fprintf(&b, `<a:tr h="%d">`, h)
		for c := range cols {
			fmt.Fprintf(&b, `<a:tc><a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>Cell %d,%d</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc>`, r, c)
		}
		b.WriteString(`</a:tr>`)
	}
	b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return b.String()
}

const tableStylePartsXML = `<a:tblStyle styleId="{36CE7E52-4504-4F78-9B80-9D4F42113728}" styleName="Owned style">
<a:wholeTbl><a:tcTxStyle><a:fontRef idx="minor"><a:srgbClr val="000000"/></a:fontRef><a:schemeClr val="accent1"/></a:tcTxStyle><a:tcStyle><a:tcBdr><a:left><a:ln w="12700"><a:solidFill><a:srgbClr val="111111"/></a:solidFill></a:ln></a:left><a:insideV><a:ln w="6350"><a:solidFill><a:srgbClr val="222222"/></a:solidFill></a:ln></a:insideV></a:tcBdr><a:fill><a:solidFill><a:schemeClr val="accent2"/></a:solidFill></a:fill></a:tcStyle></a:wholeTbl>
<a:band1H><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="333333"/></a:solidFill></a:fill></a:tcStyle></a:band1H>
<a:band2H><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="444444"/></a:solidFill></a:fill></a:tcStyle></a:band2H>
<a:firstCol><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="555555"/></a:solidFill></a:fill></a:tcStyle></a:firstCol>
<a:firstRow><a:tcTxStyle b="on"><a:schemeClr val="accent2"/></a:tcTxStyle><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="666666"/></a:solidFill></a:fill></a:tcStyle></a:firstRow>
<a:nwCell><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="777777"/></a:solidFill></a:fill></a:tcStyle></a:nwCell>
</a:tblStyle>`

func TestTableStyleCascadeAndExplicitOverrides(t *testing.T) {
	style := provenanceNode(t, tableStylePartsXML)
	r := objectProvenanceResolver()
	r.parts = &themePartCache{tableStyles: map[string]*node{"{36CE7E52-4504-4F78-9B80-9D4F42113728}": style}}
	n := provenanceNode(t, tableStyleFrameXML(3, 3, `firstRow="1" firstCol="1" bandRow="1"><a:tableStyleId>{36ce7e52-4504-4f78-9b80-9d4f42113728}</a:tableStyleId>`))
	before, _ := json.Marshal(n)
	o, warnings, err := parseTableFrame(n, r)
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	want := [][]string{{"#777777", "#666666", "#666666"}, {"#555555", "#444444", "#444444"}, {"#555555", "#333333", "#333333"}}
	for y, row := range o.Table.Cells {
		for x, cell := range row {
			if cell.Fill != want[y][x] || cell.FontFace != "Minor" || cell.FontThemeRole != "minor" || cell.FontThemeSource != r.fontPart {
				t.Fatal("conditional style/Latin role lost", y, x, cell)
			}
			if y == 0 && (!cell.Runs[0].Bold || !strings.EqualFold(cell.Color, "#abcdef")) {
				t.Fatal("header text style lost", cell)
			}
		}
	}
	if o.Table.Cells[1][0].Borders["left"].Color != "#111111" || o.Table.Cells[1][1].Borders["left"].Color != "#222222" {
		t.Fatal("outer/interior border style lost")
	}
	after, _ := json.Marshal(n)
	if !bytes.Equal(before, after) {
		t.Fatal("style cascade mutated source cell XML")
	}
	styleBefore, _ := json.Marshal(style)
	// Direct noFill and a literal equal-value word must clear the inherited link.
	x := tableStyleFrameXML(1, 1, `firstRow="1"><a:tableStyleId>{36CE7E52-4504-4F78-9B80-9D4F42113728}</a:tableStyleId>`)
	x = strings.Replace(x, `<a:tcPr/>`, `<a:tcPr><a:noFill/><a:lnL><a:noFill/></a:lnL></a:tcPr>`, 1)
	x = strings.Replace(x, `<a:r><a:t>`, `<a:r><a:rPr b="0"><a:solidFill><a:srgbClr val="ABCDEF"/></a:solidFill><a:latin typeface="Minor"/></a:rPr><a:t>`, 1)
	o, warnings, err = parseTableFrame(provenanceNode(t, x), r)
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	cell := o.Table.Cells[0][0]
	if cell.Fill != "none" || cell.FillThemeSlot != "" || cell.Borders["left"].Color != "none" || cell.Runs[0].Bold || cell.Runs[0].ColorThemeSlot != "" || cell.Runs[0].FontThemeRole != "" {
		t.Fatal("explicit style did not override source", cell)
	}
	styleAfter, _ := json.Marshal(style)
	if !bytes.Equal(styleBefore, styleAfter) {
		t.Fatal("shared style mutated")
	}
	// One cached definition resolves separately for another effective palette.
	r.colours["accent2"] = "#aabbcc"
	o, _, err = parseTableFrame(provenanceNode(t, tableStyleFrameXML(1, 1, `><a:tableStyleId>{36CE7E52-4504-4F78-9B80-9D4F42113728}</a:tableStyleId>`)), r)
	if err != nil || !strings.EqualFold(o.Table.Cells[0][0].Fill, "#aabbcc") || o.Table.Cells[0][0].FillThemeSource != r.colourPart {
		t.Fatal("shared style froze first palette", err)
	}
}

func tableStylePackageParts(t *testing.T) map[string]string {
	t.Helper()
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{ID: "table", Kind: "table", Width: 400, Height: 300, Table: tableFixture()}}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, data := range packageParts(t, out.Bytes()) {
		parts[name] = string(data)
	}
	frame := tableStyleFrameXML(3, 3, `firstRow="1" firstCol="1" bandRow="1"><a:tableStyleId>{36CE7E52-4504-4F78-9B80-9D4F42113728}</a:tableStyleId>`)
	x := parts["ppt/slides/slide1.xml"]
	a, b := strings.Index(x, "<p:graphicFrame>"), strings.Index(x, "</p:graphicFrame>")
	if a < 0 || b < 0 {
		t.Fatal("generated table missing")
	}
	parts["ppt/slides/slide1.xml"] = x[:a] + frame + x[b+len("</p:graphicFrame>"):]
	parts["ppt/_rels/presentation.xml.rels"] = strings.Replace(parts["ppt/_rels/presentation.xml.rels"], `</Relationships>`, `<Relationship Id="styles" Type="`+officeRelationshipNamespace+`/tableStyles" Target="styles/custom.xml"/></Relationships>`, 1)
	parts["ppt/styles/custom.xml"] = `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{36CE7E52-4504-4F78-9B80-9D4F42113728}">` + tableStylePartsXML + `</a:tblStyleLst>`
	parts["[Content_Types].xml"] = strings.Replace(parts["[Content_Types].xml"], `</Types>`, `<Override PartName="/ppt/styles/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml"/></Types>`, 1)
	return parts
}

func TestTableStylePackageRelationshipAndRecovery(t *testing.T) {
	base := tableStylePackageParts(t)
	if root := os.Getenv("KEYNOPE_TABLE_STYLE_IMPORT_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "table-style.pptx"), testPPTX(t, base), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"valid", "default", "missing", "external", "duplicate", "malformed", "unsafe"} {
		t.Run(mode, func(t *testing.T) {
			parts := map[string]string{}
			for k, v := range base {
				parts[k] = v
			}
			switch mode {
			case "default":
				parts["ppt/slides/slide1.xml"] = strings.ReplaceAll(parts["ppt/slides/slide1.xml"], `<a:tableStyleId>{36CE7E52-4504-4F78-9B80-9D4F42113728}</a:tableStyleId>`, "")
			case "missing":
				delete(parts, "ppt/styles/custom.xml")
			case "external":
				parts["ppt/_rels/presentation.xml.rels"] = strings.Replace(parts["ppt/_rels/presentation.xml.rels"], `Target="styles/custom.xml"`, `Target="https://example.invalid/tableStyles.xml" TargetMode="External"`, 1)
			case "duplicate":
				parts["ppt/styles/custom.xml"] = strings.Replace(parts["ppt/styles/custom.xml"], `</a:tblStyleLst>`, tableStylePartsXML+`</a:tblStyleLst>`, 1)
			case "malformed":
				parts["ppt/styles/custom.xml"] = `<a:wrong/>`
			case "unsafe":
				parts["ppt/_rels/presentation.xml.rels"] = strings.Replace(parts["ppt/_rels/presentation.xml.rels"], `Target="styles/custom.xml"`, `Target="../../../styles.xml"`, 1)
			}
			data := testPPTX(t, parts)
			p, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
			if mode == "missing" || mode == "duplicate" || mode == "malformed" || mode == "unsafe" {
				if err == nil {
					t.Fatal("invalid style package accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "external" {
				found := false
				for _, w := range report.Warnings {
					found = found || w.Code == "table-style-approximated"
				}
				if !found {
					t.Fatal("external style fallback not reported")
				}
				return
			}
			if p.Slides[0].Objects[0].Table.Cells[0][0].Fill != "#777777" {
				t.Fatal("style relationship/default not applied", report)
			}
			var encoded bytes.Buffer
			if _, err := Write(context.Background(), &encoded, p); err != nil {
				t.Fatal(err)
			}
			reopened, _, err := Read(context.Background(), bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), Options{})
			if err != nil || reopened.Slides[0].Objects[0].Table.Cells[0][0].Fill != "#777777" {
				t.Fatal("concrete native roundtrip lost style", err)
			}
		})
	}
}

func TestTableStyleExpansionBoundAndInlineFallback(t *testing.T) {
	style := `<a:tblStyle styleId="{36CE7E52-4504-4F78-9B80-9D4F42113728}"><a:wholeTbl>` + strings.Repeat(`<a:extra/>`, 513) + `</a:wholeTbl></a:tblStyle>`
	parts := tableStylePackageParts(t)
	parts["ppt/styles/custom.xml"] = `<a:tblStyleLst>` + style + `</a:tblStyleLst>`
	data := testPPTX(t, parts)
	if _, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{}); err == nil {
		t.Fatal("unbounded referenced style accepted")
	}
	frame := tableStyleFrameXML(1, 1, `>`+strings.ReplaceAll(strings.ReplaceAll(style, "a:tblStyle", "a:tableStyle"), ` styleId="{36CE7E52-4504-4F78-9B80-9D4F42113728}"`, ""))
	o, warnings, err := parseTableFrame(provenanceNode(t, frame), objectProvenanceResolver())
	if err != nil || o == nil || len(warnings) != 1 || warnings[0].Code != "table-style-approximated" {
		t.Fatal("inline expansion fallback not bounded/reported", err, warnings)
	}
}

func TestTableStyleConditionalRegions(t *testing.T) {
	pr := provenanceNode(t, `<tblPr firstRow="on" lastRow="true" firstCol="1" lastCol="1" bandRow="1" bandCol="1"/>`)
	for _, test := range []struct {
		row, col int
		names    []string
	}{
		{0, 0, []string{"wholeTbl", "band1H", "band1V", "firstCol", "firstRow", "nwCell"}},
		{0, 2, []string{"wholeTbl", "band1H", "band1V", "lastCol", "firstRow", "neCell"}},
		{2, 0, []string{"wholeTbl", "band1H", "band1V", "firstCol", "lastRow", "swCell"}},
		{2, 2, []string{"wholeTbl", "band1H", "band1V", "lastCol", "lastRow", "seCell"}},
		{1, 1, []string{"wholeTbl", "band2H", "band2V"}},
	} {
		names := []string{}
		for _, region := range tableStyleRegions(pr, test.row, test.col, 3, 3) {
			names = append(names, region.name)
		}
		if !reflect.DeepEqual(names, test.names) {
			t.Fatal("conditional activation/ordering", test.row, test.col, names)
		}
	}
	if regions := tableStyleRegions(nil, 0, 0, 3, 3); len(regions) != 1 || regions[0].name != "wholeTbl" {
		t.Fatal("disabled conditional style applied")
	}
}

func TestTableStyleDefaultToggleRetainsBroaderEmphasis(t *testing.T) {
	style := provenanceNode(t, `<tableStyle><wholeTbl><tcTxStyle b="on" i="on"/></wholeTbl><firstRow><tcTxStyle b="def" i="off"/></firstRow></tableStyle>`)
	cache := &themePartCache{tableStyles: map[string]*node{"STYLE": style}}
	r := objectProvenanceResolver()
	r.parts = cache
	o, _, err := parseTableFrame(provenanceNode(t, tableStyleFrameXML(1, 1, `firstRow="1"><a:tableStyleId>STYLE</a:tableStyleId>`)), r)
	if err != nil || !o.Table.Cells[0][0].Runs[0].Bold || o.Table.Cells[0][0].Runs[0].Italic {
		t.Fatal("def/off style toggles did not independently inherit/reset", err)
	}
}
