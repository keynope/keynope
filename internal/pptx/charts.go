package pptx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Chart is the editable, cached-data subset shared with the slide editor.
// Workbooks are local OPC parts; imports never resolve an external workbook.
type Chart struct {
	ColorThemeSlot, ColorThemeSource string
	FontThemeRole, FontThemeSource   string
	GridThemeSlot, GridThemeSource   string
	CategoryColorSources             []ThemeColourReference
	CategoryLabelSources             []ThemeColourReference
	Type, Title, FontFace, Color     string
	GridColor                        string
	FontSize                         float64
	Categories                       []string
	Series                           []ChartSeries
	Options                          ChartOptions
	CategoryColors                   []string
	CategoryLabelColors              []string
}
type ChartSeries struct {
	ColorThemeSlot, ColorThemeSource string
	LabelThemeSlot, LabelThemeSource string
	Name, Color                      string
	LabelColor                       string
	Values                           []float64
}
type ChartOptions struct {
	Stacked                      bool
	HideAxes, Legend, DataLabels bool
	ValueFormat, XTitle, YTitle  string
	Minimum, Maximum             *float64
}

func validateChart(c *Chart) error {
	if c == nil {
		return fmt.Errorf("missing chart data")
	}
	switch c.Type {
	case "column", "bar", "line", "area", "pie", "doughnut":
	default:
		return fmt.Errorf("unsupported chart type %q", c.Type)
	}
	if len(c.Categories) < 1 || len(c.Categories) > 2000 || len(c.Series) < 1 || len(c.Series) > 64 {
		return fmt.Errorf("chart exceeds category/series limits")
	}
	circular := c.Type == "pie" || c.Type == "doughnut"
	if c.Options.Stacked && c.Type != "column" && c.Type != "bar" {
		return fmt.Errorf("stacking requires a column or bar chart")
	}
	if circular && len(c.Series) != 1 {
		return fmt.Errorf("circular chart needs one series")
	}
	n := len(c.Title) + len(c.Options.XTitle) + len(c.Options.YTitle) + len(c.FontFace)
	texts := []string{c.Title, c.Options.XTitle, c.Options.YTitle, c.FontFace}
	for _, v := range c.Categories {
		n += len(v)
		texts = append(texts, v)
	}
	for _, s := range c.Series {
		n += len(s.Name)
		texts = append(texts, s.Name)
		if len(s.Values) != len(c.Categories) || !tableColorValid(s.Color) || !tableColorValid(s.LabelColor) {
			return fmt.Errorf("invalid chart series")
		}
		for _, v := range s.Values {
			if math.IsNaN(v) || math.IsInf(v, 0) || circular && v < 0 {
				return fmt.Errorf("invalid chart value")
			}
		}
	}
	if n > 1<<20 {
		return fmt.Errorf("chart text exceeds limit")
	}
	for _, text := range texts {
		if !utf8.ValidString(text) {
			return fmt.Errorf("invalid chart text encoding")
		}
		for _, r := range text {
			if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0xfffe || r == 0xffff) {
				return fmt.Errorf("invalid chart XML character")
			}
		}
	}
	if !tableColorValid(c.Color) || !tableColorValid(c.GridColor) || c.FontSize < 0 || c.FontSize > 1e5 || math.IsNaN(c.FontSize) || math.IsInf(c.FontSize, 0) {
		return fmt.Errorf("invalid chart label style")
	}
	for _, color := range c.CategoryColors {
		if !tableColorValid(color) {
			return fmt.Errorf("invalid chart category color")
		}
	}
	if len(c.CategoryLabelColors) > len(c.Categories) {
		return fmt.Errorf("too many chart label colours")
	}
	for _, color := range c.CategoryLabelColors {
		if !tableColorValid(color) {
			return fmt.Errorf("invalid chart label colour")
		}
	}
	if c.Options.ValueFormat != "" && c.Options.ValueFormat != "number" && c.Options.ValueFormat != "percent" {
		return fmt.Errorf("unsupported chart value format")
	}
	for _, v := range []*float64{c.Options.Minimum, c.Options.Maximum} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return fmt.Errorf("invalid chart axis bound")
		}
	}
	if c.Options.Minimum != nil && c.Options.Maximum != nil && *c.Options.Minimum >= *c.Options.Maximum {
		return fmt.Errorf("invalid chart axis range")
	}
	return nil
}

func chartPartName(slide, object int) string { return fmt.Sprintf("chart-s%d-o%d", slide+1, object+1) }
func chartFrameXML(o Object, id int, rid string) string {
	return fmt.Sprintf(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="%s"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm rot="%d"><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="%s"/></a:graphicData></a:graphic></p:graphicFrame>`, id, escapeAttr(objectDisplayName(o)), int64(math.Round(o.Rotation*60000)), emu(o.X), emu(o.Y), emu(o.Width), emu(o.Height), rid)
}
func chartNumberFormat(c *Chart) string {
	if c.Options.ValueFormat == "percent" {
		return "0%"
	}
	return "General"
}
func chartTextProperties(c *Chart, text string) string {
	size := c.FontSize
	if size <= 0 {
		size = 18
	}
	face := c.FontFace
	if face == "" {
		face = "Arial"
	}
	color := c.Color
	if color == "" {
		color = "#000000"
	}
	return fmt.Sprintf(`<a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="%d">%s<a:latin typeface="%s"/></a:defRPr></a:pPr><a:r><a:rPr lang="en-US"/> <a:t>%s</a:t></a:r><a:endParaRPr lang="en-US"/></a:p>`, int64(math.Round(size*100)), tablePaint(color), escapeAttr(face), escapeText(text))
}
func chartTitleXML(c *Chart, text string) string {
	if text == "" {
		return ""
	}
	return `<c:title><c:tx><c:rich>` + chartTextProperties(c, text) + `</c:rich></c:tx><c:overlay val="0"/></c:title>`
}
func chartStringCache(values []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<c:ptCount val="%d"/>`, len(values))
	for i, v := range values {
		fmt.Fprintf(&b, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, i, escapeText(v))
	}
	return b.String()
}

// PowerPoint treats omitted series/point label flags as enabled when that
// scope has its own formatting, rather than inheriting every plot-level flag.
// Be explicit at each emitted scope; circular charts label shares, not raw data.
func chartDataLabelFlags(c *Chart) string {
	circular := c.Type == "pie" || c.Type == "doughnut"
	position := ""
	// Office prohibits dLblPos for doughnut/area/radar even though the shared
	// schema permits it; doughnut's automatic placement is already centred.
	if c.Type == "pie" {
		position = `<c:dLblPos val="ctr"/>`
	}
	return fmt.Sprintf(`%s<c:showLegendKey val="0"/><c:showVal val="%d"/><c:showCatName val="0"/><c:showSerName val="0"/><c:showPercent val="%d"/><c:showBubbleSize val="0"/>`, position, boolNumber(c.Options.DataLabels && !circular), boolNumber(c.Options.DataLabels && circular))
}

func chartDataLabelNumberFormat(c *Chart) string {
	if c.Type == "pie" || c.Type == "doughnut" {
		return "0%"
	}
	return chartNumberFormat(c)
}

func chartXML(c *Chart) string {
	var b strings.Builder
	circular := c.Type == "pie" || c.Type == "doughnut"
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><c:date1904 val="0"/><c:lang val="en-US"/><c:chart>`)
	b.WriteString(chartTitleXML(c, c.Title))
	b.WriteString(`<c:autoTitleDeleted val="1"/><c:plotArea><c:layout/>`)
	kind := map[string]string{"column": "barChart", "bar": "barChart", "line": "lineChart", "area": "areaChart", "pie": "pieChart", "doughnut": "doughnutChart"}[c.Type]
	b.WriteString("<c:" + kind + ">")
	switch c.Type {
	case "column", "bar":
		dir := "col"
		if c.Type == "bar" {
			dir = "bar"
		}
		grouping := "clustered"
		if c.Options.Stacked {
			grouping = "stacked"
		}
		fmt.Fprintf(&b, `<c:barDir val="%s"/><c:grouping val="%s"/>`, dir, grouping)
	case "line", "area":
		b.WriteString(`<c:grouping val="standard"/>`)
	}
	b.WriteString(`<c:varyColors val="0"/>`)
	for i, s := range c.Series {
		column := xlsxColumn(i + 2)
		last := len(c.Categories) + 1
		fmt.Fprintf(&b, `<c:ser><c:idx val="%d"/><c:order val="%d"/><c:tx><c:strRef><c:f>Data!$%s$1</c:f><c:strCache>%s</c:strCache></c:strRef></c:tx>`, i, i, column, chartStringCache([]string{s.Name}))
		fill := tablePaint(s.Color)
		if c.Type == "area" && strings.Contains(fill, `<a:srgbClr `) {
			fill = strings.Replace(fill, `/></a:solidFill>`, `><a:alpha val="35000"/></a:srgbClr></a:solidFill>`, 1)
		}
		b.WriteString(`<c:spPr>` + fill)
		if c.Type == "line" {
			b.WriteString(`<a:ln w="28575">` + tablePaint(s.Color) + `</a:ln>`)
		} else {
			b.WriteString(`<a:ln><a:noFill/></a:ln>`)
		}
		b.WriteString(`</c:spPr>`)
		if c.Type == "column" || c.Type == "bar" {
			b.WriteString(`<c:invertIfNegative val="0"/>`)
		}
		if c.Type == "line" {
			b.WriteString(`<c:marker><c:symbol val="none"/></c:marker>`)
		}
		if c.Type == "pie" || c.Type == "doughnut" {
			for index, color := range c.CategoryColors {
				if index >= len(c.Categories) {
					break
				}
				fmt.Fprintf(&b, `<c:dPt><c:idx val="%d"/><c:spPr>%s<a:ln><a:noFill/></a:ln></c:spPr></c:dPt>`, index, tablePaint(color))
			}
		}
		if s.LabelColor != "" || circular && len(c.CategoryLabelColors) > 0 {
			b.WriteString(`<c:dLbls>`)
			if circular {
				for i, color := range c.CategoryLabelColors {
					if color == "" {
						continue
					}
					style := *c
					style.Color = color
					fmt.Fprintf(&b, `<c:dLbl><c:idx val="%d"/><c:numFmt formatCode="%s" sourceLinked="0"/><c:txPr>%s</c:txPr>%s</c:dLbl>`, i, chartDataLabelNumberFormat(c), chartTextProperties(&style, ""), chartDataLabelFlags(c))
				}
			}
			fmt.Fprintf(&b, `<c:numFmt formatCode="%s" sourceLinked="0"/>`, chartDataLabelNumberFormat(c))
			if s.LabelColor != "" {
				style := *c
				style.Color = s.LabelColor
				b.WriteString(`<c:txPr>` + chartTextProperties(&style, "") + `</c:txPr>`)
			}
			b.WriteString(chartDataLabelFlags(c) + `<c:showLeaderLines val="0"/></c:dLbls>`)
		}
		fmt.Fprintf(&b, `<c:cat><c:strRef><c:f>Data!$A$2:$A$%d</c:f><c:strCache>%s</c:strCache></c:strRef></c:cat><c:val><c:numRef><c:f>Data!$%s$2:$%s$%d</c:f><c:numCache><c:formatCode>%s</c:formatCode><c:ptCount val="%d"/>`, last, chartStringCache(c.Categories), column, column, last, chartNumberFormat(c), len(s.Values))
		for index, v := range s.Values {
			fmt.Fprintf(&b, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, index, strconv.FormatFloat(v, 'g', -1, 64))
		}
		b.WriteString(`</c:numCache></c:numRef></c:val>`)
		if c.Type == "line" {
			b.WriteString(`<c:smooth val="0"/>`)
		}
		b.WriteString(`</c:ser>`)
	}
	if c.Options.DataLabels {
		fmt.Fprintf(&b, `<c:dLbls><c:numFmt formatCode="%s" sourceLinked="0"/>%s<c:showLeaderLines val="0"/></c:dLbls>`, chartDataLabelNumberFormat(c), chartDataLabelFlags(c))
	}
	if circular {
		b.WriteString(`<c:firstSliceAng val="270"/>`)
		if c.Type == "doughnut" {
			b.WriteString(`<c:holeSize val="56"/>`)
		}
	} else {
		if kind == "barChart" {
			overlap := 0
			if c.Options.Stacked {
				overlap = 100
			}
			fmt.Fprintf(&b, `<c:gapWidth val="150"/><c:overlap val="%d"/>`, overlap)
		}
		b.WriteString(`<c:axId val="100"/><c:axId val="200"/>`)
	}
	b.WriteString("</c:" + kind + ">")
	if !circular {
		catPos, valPos := "b", "l"
		if c.Type == "bar" {
			catPos, valPos = "l", "b"
		}
		fmt.Fprintf(&b, `<c:catAx><c:axId val="100"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="%d"/><c:axPos val="%s"/>`, boolNumber(c.Options.HideAxes), catPos)
		b.WriteString(chartTitleXML(c, c.Options.XTitle))
		b.WriteString(`<c:tickLblPos val="nextTo"/><c:crossAx val="200"/><c:crosses val="autoZero"/><c:auto val="1"/><c:lblAlgn val="ctr"/><c:lblOffset val="100"/></c:catAx>`)
		b.WriteString(`<c:valAx><c:axId val="200"/><c:scaling><c:orientation val="minMax"/>`)
		if c.Options.Maximum != nil {
			fmt.Fprintf(&b, `<c:max val="%s"/>`, strconv.FormatFloat(*c.Options.Maximum, 'g', -1, 64))
		}
		if c.Options.Minimum != nil {
			fmt.Fprintf(&b, `<c:min val="%s"/>`, strconv.FormatFloat(*c.Options.Minimum, 'g', -1, 64))
		}
		fmt.Fprintf(&b, `</c:scaling><c:delete val="%d"/><c:axPos val="%s"/>`, boolNumber(c.Options.HideAxes), valPos)
		if c.GridColor != "" {
			b.WriteString(`<c:majorGridlines><c:spPr><a:ln>` + tablePaint(c.GridColor) + `</a:ln></c:spPr></c:majorGridlines>`)
		}
		b.WriteString(chartTitleXML(c, c.Options.YTitle))
		fmt.Fprintf(&b, `<c:numFmt formatCode="%s" sourceLinked="0"/><c:tickLblPos val="nextTo"/><c:crossAx val="100"/><c:crosses val="autoZero"/><c:crossBetween val="between"/></c:valAx>`, chartNumberFormat(c))
	}
	b.WriteString(`</c:plotArea>`)
	if c.Options.Legend {
		b.WriteString(`<c:legend><c:legendPos val="b"/><c:overlay val="0"/></c:legend>`)
	}
	b.WriteString(`<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/><c:showDLblsOverMax val="0"/></c:chart><c:spPr><a:noFill/><a:ln><a:noFill/></a:ln></c:spPr><c:txPr>` + chartTextProperties(c, "") + `</c:txPr><c:externalData r:id="rId1"><c:autoUpdate val="0"/></c:externalData></c:chartSpace>`)
	return b.String()
}
func chartRelationships(name string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/package" Target="../embeddings/` + name + `.xlsx"/></Relationships>`
}
func xlsxColumn(index int) string {
	s := ""
	for index > 0 {
		index--
		s = string(rune('A'+index%26)) + s
		index /= 26
	}
	return s
}
func chartWorkbook(c *Chart) ([]byte, error) {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	parts := [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets><calcPr calcId="0"/></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font><sz val="11"/><name val="Arial"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="9" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`},
	}
	var sheet strings.Builder
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1">`)
	inline := func(ref, text string) {
		fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escapeText(text))
	}
	inline("A1", "Category")
	for i, s := range c.Series {
		inline(xlsxColumn(i+2)+"1", s.Name)
	}
	sheet.WriteString(`</row>`)
	for row, cat := range c.Categories {
		fmt.Fprintf(&sheet, `<row r="%d">`, row+2)
		inline("A"+strconv.Itoa(row+2), cat)
		for i, s := range c.Series {
			style := 0
			if c.Options.ValueFormat == "percent" {
				style = 1
			}
			fmt.Fprintf(&sheet, `<c r="%s%d" s="%d"><v>%s</v></c>`, xlsxColumn(i+2), row+2, style, strconv.FormatFloat(s.Values[row], 'g', -1, 64))
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	parts = append(parts, [2]string{"xl/worksheets/sheet1.xml", sheet.String()})
	for _, p := range parts {
		w, err := z.CreateHeader(&zip.FileHeader{Name: p[0], Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + p[1])); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Route graphic frames without ever fetching a workbook or remote chart part.
func parseGraphicFrame(n *node, rels map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) (*Object, []Warning, error) {
	if findFirst(n, "tbl") != nil {
		if err := loadTableStyles(files, limits, resolver.parts); err != nil {
			return nil, nil, err
		}
		return parseTableFrame(n, resolver)
	}
	ref := findFirst(n, "chart")
	if ref == nil {
		return nil, []Warning{{Code: "unsupported-graphic-frame", Message: "Unsupported graphic frame was not imported."}}, nil
	}
	o := &Object{ID: attr(findFirst(n, "cNvPr"), "id"), Kind: "chart"}
	o.Name = attr(findFirst(n, "cNvPr"), "name")
	o.X, o.Y, o.Width, o.Height, o.Rotation = transform(n)
	for _, v := range []float64{o.X, o.Y, o.Width, o.Height, o.Rotation} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e8 {
			return nil, nil, fmt.Errorf("invalid chart bounds")
		}
	}
	if o.Width <= 0 || o.Height <= 0 {
		return nil, nil, fmt.Errorf("empty chart bounds")
	}
	part := rels[attr(ref, "id")]
	if part == "" {
		o.Kind = "text"
		o.Text = "Unavailable PowerPoint chart"
		o.FontFace = "Arial"
		o.FontSize = 18
		o.Color = "#555555"
		return o, []Warning{{Object: o.ID, Code: "chart-part-missing", Message: "External or missing chart part was not loaded; a labelled placeholder was inserted."}}, nil
	}
	data := cache["chart-xml:"+part]
	if data == nil {
		var err error
		data, err = readPart(files, part, limits.MaxXMLPart)
		if err != nil {
			return nil, nil, err
		}
		if cache != nil {
			cache["chart-xml:"+part] = data
		}
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return nil, nil, err
	}
	resolver, err = applyThemeOverride(files, part, limits, resolver)
	if err != nil {
		return nil, nil, err
	}
	c, warnings, err := parseChartData(root, resolver)
	if err != nil {
		return nil, nil, err
	}
	for i := range warnings {
		warnings[i].Object = o.ID
	}
	if c == nil {
		o.Kind = "text"
		o.Text = "Unsupported PowerPoint chart"
		o.FontFace = "Arial"
		o.FontSize = 18
		o.Color = "#555555"
		if len(data) <= 1<<20 {
			o.ChartSource = data // Immutable source bytes shared across reused chart parts.
		} else {
			warnings = append(warnings, Warning{Object: o.ID, Code: "chart-source-too-large", Message: "Unsupported chart source exceeds the 1 MiB recovery limit and was not retained."})
		}
	} else {
		o.Chart = c
	}
	if ext := findChild(root, "externalData"); ext != nil {
		local, e := readRelationships(files, part, limits.MaxXMLPart)
		if e != nil {
			return nil, nil, e
		}
		if local[attr(ext, "id")] == "" {
			warnings = append(warnings, Warning{Object: o.ID, Code: "chart-external-data-ignored", Message: "External chart workbook was not accessed; cached values were used."})
		}
	}
	return o, warnings, nil
}
func chartCachedStrings(n *node, max int) ([]string, error) {
	if n == nil {
		return nil, fmt.Errorf("chart has no cached values")
	}
	cache := findFirst(n, "strCache")
	if cache == nil {
		cache = findFirst(n, "numCache")
	}
	if cache == nil {
		cache = findFirst(n, "strLit")
	}
	if cache == nil {
		cache = findFirst(n, "numLit")
	}
	if cache == nil {
		return nil, fmt.Errorf("chart requires local cached values")
	}
	count, err := strconv.Atoi(attr(findChild(cache, "ptCount"), "val"))
	if err != nil || count < 1 || count > max {
		return nil, fmt.Errorf("invalid chart cache count")
	}
	result := make([]string, count)
	seen := make([]bool, count)
	for _, p := range children(cache, "pt") {
		i, err := strconv.Atoi(attr(p, "idx"))
		if err != nil || i < 0 || i >= count || seen[i] {
			return nil, fmt.Errorf("invalid chart cache index")
		}
		seen[i] = true
		result[i] = nodeText(findChild(p, "v"))
	}
	for _, v := range seen {
		if !v {
			return nil, fmt.Errorf("sparse chart caches are not supported")
		}
	}
	return result, nil
}
func chartRichText(n *node) string {
	var b strings.Builder
	for _, t := range descendants(n, "t") {
		b.WriteString(t.Text)
	}
	return b.String()
}
func nodeText(n *node) string {
	if n == nil {
		return ""
	}
	return n.Text
}
func chartResolvedPaint(n *node, resolver colourResolver) string {
	if findChild(n, "noFill") != nil {
		return "none"
	}
	color, _ := resolvedPaint(findChild(n, "solidFill"), resolver)
	return color
}

func chartPaintSource(n *node, resolver colourResolver) (string, ThemeColourReference) {
	color := chartResolvedPaint(n, resolver)
	if color == "" || color == "none" {
		return color, ThemeColourReference{}
	}
	slot := resolvedThemeColourSlot(findChild(n, "solidFill"), resolver)
	return color, ThemeColourReference{Slot: slot, SourcePart: themeReferenceSource(slot, resolver.colourPart)}
}

func setChartColourSource(sources *[]ThemeColourReference, index, count int, ref ThemeColourReference) {
	if ref.Slot != "" && *sources == nil {
		*sources = make([]ThemeColourReference, count)
	}
	if index < len(*sources) {
		(*sources)[index] = ref
	}
}
func parseChartData(root *node, resolver colourResolver) (*Chart, []Warning, error) {
	plot := findFirst(root, "plotArea")
	var kind *node
	kindName := ""
	warnings := []Warning{}
	if plot != nil {
		for _, child := range plot.Children {
			if strings.HasSuffix(child.Name, "Chart") {
				if kind != nil {
					return nil, []Warning{{Code: "chart-combination-unsupported", Message: "Combination chart was retained as a labelled placeholder."}}, nil
				}
				kind = child
				kindName = child.Name
			}
		}
	}
	typ := map[string]string{"barChart": "column", "lineChart": "line", "areaChart": "area", "pieChart": "pie", "doughnutChart": "doughnut"}[kindName]
	if typ == "" {
		return nil, []Warning{{Code: "chart-type-unsupported", Message: "Unsupported chart type was retained as a labelled placeholder with its source XML."}}, nil
	}
	if kindName == "barChart" && attr(findChild(kind, "barDir"), "val") == "bar" {
		typ = "bar"
	}
	group := attr(findChild(kind, "grouping"), "val")
	if group == "percentStacked" || (group == "stacked" && typ != "column" && typ != "bar") {
		return nil, []Warning{{Code: "chart-stacked-unsupported", Message: "Stacked chart was retained as a labelled placeholder with its source XML."}}, nil
	}
	c := &Chart{Type: typ, Title: chartRichText(findChild(findFirst(root, "chart"), "title")), Color: "#000000", FontFace: "Arial", FontSize: 18}
	c.Options.Stacked = group == "stacked"
	if len(children(plot, "catAx")) > 1 || len(children(plot, "valAx")) > 1 {
		warnings = append(warnings, Warning{Code: "chart-secondary-axes-approximated", Message: "Secondary chart axes were reduced to the first category and value axis."})
	}
	for _, feature := range []string{"trendline", "errBars", "pictureOptions", "multiLvlStrRef"} {
		if findFirst(kind, feature) != nil {
			warnings = append(warnings, Warning{Code: "chart-option-approximated", Message: "Chart " + feature + " is not supported and was omitted."})
		}
	}
	for _, s := range children(kind, "ser") {
		if len(c.Series) >= 64 {
			return nil, nil, fmt.Errorf("too many chart series")
		}
		cats, err := chartCachedStrings(findChild(s, "cat"), 2000)
		if err != nil {
			return nil, nil, err
		}
		if c.Categories == nil {
			c.Categories = cats
		} else {
			if len(cats) != len(c.Categories) {
				return nil, nil, fmt.Errorf("inconsistent chart categories")
			}
			for i, v := range cats {
				if v != c.Categories[i] {
					return nil, nil, fmt.Errorf("inconsistent chart categories")
				}
			}
		}
		values, err := chartCachedStrings(findChild(s, "val"), 2000)
		if err != nil {
			return nil, nil, err
		}
		series := ChartSeries{Values: make([]float64, len(values))}
		tx := findChild(s, "tx")
		if findFirst(tx, "strCache") != nil {
			names, e := chartCachedStrings(tx, 1)
			if e != nil {
				return nil, nil, e
			}
			series.Name = names[0]
		} else {
			series.Name = nodeText(findChild(tx, "v"))
		}
		for i, v := range values {
			number, e := strconv.ParseFloat(v, 64)
			if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, nil, fmt.Errorf("invalid chart numeric cache")
			}
			series.Values[i] = number
		}
		paint := findChild(s, "spPr")
		if typ == "line" && findChild(paint, "ln") != nil {
			paint = findChild(paint, "ln")
		}
		color, reference := chartPaintSource(paint, resolver)
		series.Color, series.ColorThemeSlot, series.ColorThemeSource = color, reference.Slot, reference.SourcePart
		labels := findChild(s, "dLbls")
		color, reference = chartPaintSource(findFirst(findChild(labels, "txPr"), "defRPr"), resolver)
		series.LabelColor, series.LabelThemeSlot, series.LabelThemeSource = color, reference.Slot, reference.SourcePart
		if typ == "pie" || typ == "doughnut" {
			seen := map[int]bool{}
			for _, label := range children(labels, "dLbl") {
				idx, err := strconv.Atoi(attr(findChild(label, "idx"), "val"))
				if err != nil || idx < 0 || idx >= len(c.Categories) || seen[idx] {
					return nil, nil, fmt.Errorf("invalid chart data-label index")
				}
				seen[idx] = true
				color, reference := chartPaintSource(findFirst(findChild(label, "txPr"), "defRPr"), resolver)
				if color == "" {
					continue
				}
				for len(c.CategoryLabelColors) <= idx {
					c.CategoryLabelColors = append(c.CategoryLabelColors, "")
				}
				c.CategoryLabelColors[idx] = color
				setChartColourSource(&c.CategoryLabelSources, idx, len(c.Categories), reference)
			}
		}
		c.Series = append(c.Series, series)
		if typ == "pie" || typ == "doughnut" {
			for _, p := range children(s, "dPt") {
				idx, e := strconv.Atoi(attr(findChild(p, "idx"), "val"))
				if e != nil || idx < 0 || idx >= len(c.Categories) {
					return nil, nil, fmt.Errorf("invalid chart category color index")
				}
				for len(c.CategoryColors) < len(c.Categories) {
					c.CategoryColors = append(c.CategoryColors, "")
				}
				color, reference := chartPaintSource(findChild(p, "spPr"), resolver)
				c.CategoryColors[idx] = color
				setChartColourSource(&c.CategoryColorSources, idx, len(c.Categories), reference)
			}
		}
	}
	c.Options.Legend = findChild(findFirst(root, "chart"), "legend") != nil
	labels := findChild(kind, "dLbls")
	c.Options.DataLabels = drawingMLBool(attr(findChild(labels, "showVal"), "val")) || (typ == "pie" || typ == "doughnut") && drawingMLBool(attr(findChild(labels, "showPercent"), "val"))
	catAxis, valAxis := findChild(plot, "catAx"), findChild(plot, "valAx")
	grid, reference := chartPaintSource(findChild(findChild(findChild(valAxis, "majorGridlines"), "spPr"), "ln"), resolver)
	c.GridColor, c.GridThemeSlot, c.GridThemeSource = grid, reference.Slot, reference.SourcePart
	c.Options.HideAxes = drawingMLBool(attr(findChild(catAxis, "delete"), "val"))
	c.Options.XTitle = chartRichText(findChild(catAxis, "title"))
	c.Options.YTitle = chartRichText(findChild(valAxis, "title"))
	format := attr(findChild(valAxis, "numFmt"), "formatCode")
	if format == "" {
		format = nodeText(findFirst(kind, "formatCode"))
	}
	if strings.Contains(format, "%") {
		c.Options.ValueFormat = "percent"
	} else {
		c.Options.ValueFormat = "number"
	}
	for _, bound := range []struct {
		name string
		dest **float64
	}{{"min", &c.Options.Minimum}, {"max", &c.Options.Maximum}} {
		if n := findChild(findChild(valAxis, "scaling"), bound.name); n != nil {
			v, e := strconv.ParseFloat(attr(n, "val"), 64)
			if e != nil {
				return nil, nil, fmt.Errorf("invalid chart axis bound")
			}
			*bound.dest = &v
		}
	}
	if pr := findFirst(findChild(root, "txPr"), "defRPr"); pr != nil {
		style := overlayTextRun(TextRun{}, pr, resolver)
		c.FontFace, c.FontSize, c.Color = style.FontFace, style.FontSize, style.Color
		c.FontThemeRole, c.FontThemeSource = style.FontThemeRole, style.FontThemeSource
		c.ColorThemeSlot, c.ColorThemeSource = style.ColorThemeSlot, style.ColorThemeSource
	}
	if err := validateChart(c); err != nil {
		return nil, nil, err
	}
	return c, warnings, nil
}
