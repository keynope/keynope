package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func chartProvenanceXML(kind string) string {
	xml := chartXML(chartFixture(kind))
	// This fixture exercises simple scheme paint. The native area's alpha
	// transform is covered separately as a deliberately concrete fallback.
	xml = strings.ReplaceAll(xml, `><a:alpha val="35000"/></a:srgbClr>`, `/>`)
	for _, p := range [][2]string{
		{`<a:srgbClr val="55AAFF"/>`, `<a:schemeClr val="accent1"/>`},
		{`<a:srgbClr val="223344"/>`, `<a:schemeClr val="tx1"/>`},
		{`<a:srgbClr val="6688AA"/>`, `<a:schemeClr val="accent1"/>`},
		{`<a:srgbClr val="00AAFF"/>`, `<a:schemeClr val="accent2"/>`},
		{`<a:srgbClr val="FF00AA"/>`, `<a:schemeClr val="accent2"/>`},
		{`<a:srgbClr val="FF5500"/>`, `<a:srgbClr val="123456"/>`},
		{`<a:srgbClr val="AAFF00"/>`, `<a:schemeClr val="accent1"><a:shade val="100000"/></a:schemeClr>`},
		{`typeface="Arial"`, `typeface="+mn-lt"`},
	} {
		xml = strings.ReplaceAll(xml, p[0], p[1])
	}
	return xml
}

func TestChartThemeProvenanceAcrossSupportedTypes(t *testing.T) {
	r := objectProvenanceResolver()
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			c, _, err := parseChartData(provenanceNode(t, chartProvenanceXML(kind)), r)
			if err != nil || c == nil {
				t.Fatal(err)
			}
			if c.ColorThemeSlot != "accent1" || c.ColorThemeSource != r.colourPart || c.FontThemeRole != "minor" || c.FontThemeSource != r.fontPart {
				t.Fatal("chart typography source missing", c)
			}
			if c.Series[0].Color != "#123456" || c.Series[0].ColorThemeSlot != "accent1" || c.Series[0].ColorThemeSource != r.colourPart {
				t.Fatal("series paint source missing", c.Series)
			}
			if kind == "pie" || kind == "doughnut" {
				if len(c.CategoryColorSources) != 3 || c.CategoryColorSources[0].Slot != "accent1" || c.CategoryColorSources[0].SourcePart != r.colourPart || c.CategoryColorSources[1] != (ThemeColourReference{}) || c.CategoryColorSources[2] != (ThemeColourReference{}) {
					t.Fatal("slice source references collapsed literal or transformed values", c.CategoryColorSources)
				}
				if c.CategoryColors[0] != c.CategoryColors[1] || c.CategoryColors[0] != c.CategoryColors[2] {
					t.Fatal("fixture must have equal resolved slice colours", c.CategoryColors)
				}
				if len(c.CategoryLabelSources) != 3 || c.CategoryLabelSources[0].Slot != "accent2" || c.CategoryLabelSources[0].SourcePart != r.colourPart || c.CategoryLabelSources[1] != (ThemeColourReference{}) {
					t.Fatal("slice-label source lost", c.CategoryLabelSources)
				}
			} else {
				if c.Series[1].Color != c.Series[0].Color || c.Series[1].ColorThemeSlot != "" || c.Series[1].ColorThemeSource != "" {
					t.Fatal("equal literal series became theme linked", c.Series)
				}
				if c.Series[0].LabelThemeSlot != "accent2" || c.Series[0].LabelThemeSource != r.colourPart || c.GridThemeSlot != "accent1" || c.GridThemeSource != r.colourPart {
					t.Fatal("grid/series-label ownership lost", c)
				}
			}
		})
	}
}

func TestChartPointSourceReplacementClearsOldReference(t *testing.T) {
	r := objectProvenanceResolver()
	xml := chartProvenanceXML("pie")
	// Existing reader policy is last-wins for duplicate point colour entries.
	// Its parallel source metadata must follow the same selected value.
	xml = strings.Replace(xml, `</c:ser>`, `<c:dPt><c:idx val="0"/><c:spPr><a:solidFill><a:srgbClr val="123456"/></a:solidFill></c:spPr></c:dPt></c:ser>`, 1)
	c, _, err := parseChartData(provenanceNode(t, xml), r)
	if err != nil || c == nil || c.CategoryColorSources[0] != (ThemeColourReference{}) {
		t.Fatal("literal replacement retained old slot", c, err)
	}
	_, ref := chartPaintSource(provenanceNode(t, `<spPr><noFill/></spPr>`), r)
	if ref != (ThemeColourReference{}) {
		t.Fatal("None has theme source", ref)
	}
	color, ref := chartPaintSource(provenanceNode(t, `<spPr><solidFill><schemeClr val="accent1"><alpha val="35000"/></schemeClr></solidFill></spPr>`), r)
	if color != "#123456" || ref != (ThemeColourReference{}) {
		t.Fatal("transformed area paint claimed unmodified theme reference", color, ref)
	}
}

func TestChartLocalThemeOverridesRetainIndependentOwners(t *testing.T) {
	var output bytes.Buffer
	object := Object{ID: "chart", Kind: "chart", Width: 500, Height: 350, Chart: chartFixture("column")}
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{object}}, {Objects: []Object{object}}}}); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{}
	for name, data := range packageParts(t, output.Bytes()) {
		base[name] = string(data)
	}
	section := func(tag string) string {
		source := base["ppt/theme/theme1.xml"]
		start, end := strings.Index(source, "<a:"+tag+" "), strings.Index(source, "</a:"+tag+">")
		if start < 0 || end < 0 {
			t.Fatal("fixture scheme missing", tag)
		}
		return source[start : end+len("</a:"+tag+">")]
	}
	for _, mode := range []string{"font", "colour", "both", "different"} {
		t.Run(mode, func(t *testing.T) {
			parts := map[string]string{}
			for name, data := range base {
				parts[name] = data
			}
			for _, name := range []string{"ppt/charts/chart-s1-o1.xml", "ppt/charts/chart-s2-o1.xml"} {
				xml := parts[name]
				xml = strings.ReplaceAll(xml, `<a:srgbClr val="55AAFF"/>`, `<a:schemeClr val="accent1"/>`)
				xml = strings.ReplaceAll(xml, `typeface="Arial"`, `typeface="+mn-lt"`)
				parts[name] = xml
			}
			content := ""
			if mode != "font" {
				content += section("clrScheme")
			}
			if mode != "colour" {
				content += section("fontScheme")
			}
			if mode == "different" {
				content = strings.ReplaceAll(content, `val="55AAFF"`, `val="112233"`)
				content = strings.ReplaceAll(content, `typeface="Arial"`, `typeface="Courier New"`)
			}
			parts["ppt/theme/chartOverride.xml"] = `<a:themeOverride xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` + content + `</a:themeOverride>`
			rels := "ppt/charts/_rels/chart-s1-o1.xml.rels"
			parts[rels] = strings.Replace(parts[rels], `</Relationships>`, `<Relationship Id="themeOverride" Type="`+officeRelationshipNamespace+`/themeOverride" Target="../theme/chartOverride.xml"/></Relationships>`, 1)
			data := testPPTX(t, parts)
			p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
			if err != nil {
				t.Fatal(err)
			}
			a, b := p.Slides[0].Objects[0].Chart, p.Slides[1].Objects[0].Chart
			colorOwner, fontOwner := "ppt/theme/theme1.xml", "ppt/theme/theme1.xml"
			if mode != "font" {
				colorOwner = "ppt/theme/chartOverride.xml"
			}
			if mode != "colour" {
				fontOwner = "ppt/theme/chartOverride.xml"
			}
			if a.Series[0].ColorThemeSource != colorOwner || a.FontThemeSource != fontOwner || b.Series[0].ColorThemeSource != "ppt/theme/theme1.xml" || b.FontThemeSource != "ppt/theme/theme1.xml" {
				t.Fatal("chart-local equal-value override lost ownership or changed values", a, b)
			}
			if mode == "different" {
				if a.Series[0].Color != "#112233" || a.FontFace != "Courier New" || b.Series[0].Color != "#55AAFF" || b.FontFace != "Arial" {
					t.Fatal("chart-local override values ignored or escaped scope", a, b)
				}
			} else if a.Series[0].Color != b.Series[0].Color || a.FontFace != b.FontFace {
				t.Fatal("equal-value override changed paint", a, b)
			}
			if p.Theme == nil || p.Theme.SourcePart != "ppt/theme/theme1.xml" {
				t.Fatal("chart override changed presentation theme")
			}
			var again bytes.Buffer
			if _, err := Write(context.Background(), &again, p); err != nil {
				t.Fatal(err)
			}
			for name, data := range packageParts(t, again.Bytes()) {
				if strings.Contains(string(data), "chartOverride.xml") {
					t.Fatal("read-only chart owner leaked into writer", name)
				}
			}
			if mode == "font" {
				valid := parts["ppt/theme/chartOverride.xml"]
				parts["ppt/theme/chartOverride.xml"] = `<theme/>`
				bad := testPPTX(t, parts)
				if partial, _, err := Read(context.Background(), bytes.NewReader(bad), int64(len(bad)), Options{}); err == nil || len(partial.Slides) != 0 {
					t.Fatal("invalid chart override did not reject atomically", err)
				}
				parts["ppt/theme/chartOverride.xml"] = valid
				parts[rels] = strings.Replace(parts[rels], `Id="themeOverride"`, `Id="themeOverride" TargetMode="External"`, 1)
				external := testPPTX(t, parts)
				ignored, _, err := Read(context.Background(), bytes.NewReader(external), int64(len(external)), Options{})
				if err != nil || ignored.Slides[0].Objects[0].Chart.FontThemeSource != "ppt/theme/theme1.xml" {
					t.Fatal("external chart override was loaded", err)
				}
			}
		})
	}
}
