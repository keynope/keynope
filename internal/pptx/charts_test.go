package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func chartFixture(kind string) *Chart {
	min, max := -5.0, 20.0
	c := &Chart{Type: kind, Title: "Editable <chart>", FontFace: "Arial", FontSize: 16, Color: "#223344", Categories: []string{"=2+2", " B & C ", "Third"}, Series: []ChartSeries{{Name: "Outcome", Color: "#55AAFF", Values: []float64{0, 5.25, 12}}}, Options: ChartOptions{Legend: true, DataLabels: true, ValueFormat: "percent", XTitle: "Teams", YTitle: "Scores", Minimum: &min, Maximum: &max}}
	if kind == "pie" || kind == "doughnut" {
		c.Options.Minimum = nil
		c.Options.Maximum = nil
		c.Options.XTitle = ""
		c.Options.YTitle = ""
		c.CategoryColors = []string{"#55AAFF", "#FF5500", "#AAFF00"}
		c.CategoryLabelColors = []string{"#FF00AA", "none"}
	} else {
		c.GridColor = "#6688AA"
		c.Series[0].LabelColor = "#00AAFF"
		c.Series = append(c.Series, ChartSeries{Name: "Comparison", Color: "#FF5500", Values: []float64{-2, 4, 11}})
	}
	return c
}

func TestStackedBarChartXML(t *testing.T) {
	for _, kind := range []string{"bar", "column"} {
		c := chartFixture(kind)
		c.Options.Stacked = true
		if err := validateChart(c); err != nil {
			t.Fatal(err)
		}
		xml := chartXML(c)
		for _, part := range []string{`<c:grouping val="stacked"/>`, `<c:overlap val="100"/>`} {
			if !strings.Contains(xml, part) {
				t.Fatalf("missing %s", part)
			}
		}
	}
	c := chartFixture("area")
	c.Options.Stacked = true
	if validateChart(c) == nil {
		t.Fatal("unsupported stacking accepted")
	}
}
func packageParts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		out[f.Name] = b
	}
	return out
}
func TestChartNativePackageRoundTrip(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			c := chartFixture(kind)
			p := Presentation{Slides: []Slide{{Objects: []Object{{ID: "chart", Kind: "chart", X: 120, Y: 80, Width: 900, Height: 600, Rotation: 12, Chart: c}}}}}
			var out bytes.Buffer
			report, err := Write(context.Background(), &out, p)
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			parts := packageParts(t, out.Bytes())
			slide := string(parts["ppt/slides/slide1.xml"])
			if !strings.Contains(slide, "<c:chart") || strings.Contains(slide, "<p:pic>") {
				t.Fatal("chart flattened", slide)
			}
			chart := parts["ppt/charts/chart-s1-o1.xml"]
			root, e := parseXML(chart, 128)
			if e != nil {
				t.Fatal(e)
			}
			if len(descendants(root, "ser")) != len(c.Series) {
				t.Fatal("series lost")
			}
			if findChild(root, "externalData") == nil {
				t.Fatal("workbook not linked")
			}
			workbook := packageParts(t, parts["ppt/embeddings/chart-s1-o1.xlsx"])
			sheet, e := parseXML(workbook["xl/worksheets/sheet1.xml"], 128)
			if e != nil {
				t.Fatal(e)
			}
			if len(descendants(sheet, "f")) != 0 {
				t.Fatal("category formula injection")
			}
			rows := children(findChild(sheet, "sheetData"), "row")
			if len(rows) != 4 {
				t.Fatal("workbook row count")
			}
			for r, row := range rows {
				cells := children(row, "c")
				if len(cells) != len(c.Series)+1 {
					t.Fatal("workbook columns")
				}
				if r == 0 {
					continue
				}
				if nodeText(findFirst(cells[0], "t")) != c.Categories[r-1] {
					t.Fatal("workbook category")
				}
				for s := range c.Series {
					if nodeText(findChild(cells[s+1], "v")) != strconvNumber(c.Series[s].Values[r-1]) {
						t.Fatal("workbook/cache mismatch")
					}
				}
			}
			got, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			o := got.Slides[0].Objects[0]
			if o.Kind != "chart" || o.Rotation != 12 || o.Width != 900 {
				t.Fatal("chart geometry", o)
			}
			if !reflect.DeepEqual(o.Chart, c) {
				t.Fatalf("data/style mismatch\ngot %#v\nwant %#v", o.Chart, c)
			}
			var twice bytes.Buffer
			_, err = Write(context.Background(), &twice, p)
			if err != nil || !bytes.Equal(out.Bytes(), twice.Bytes()) {
				t.Fatal("nondeterministic chart package", err)
			}
		})
	}
}
func strconvNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
func TestChartMalformedRejectedBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*Chart){func(c *Chart) { c.Type = "scatter" }, func(c *Chart) { c.Series[0].Values[0] = math.NaN() }, func(c *Chart) { c.Series[0].Values = nil }, func(c *Chart) { c.Categories = nil }, func(c *Chart) { c.Series[0].Color = "red" }, func(c *Chart) { c.FontSize = math.Inf(1) }, func(c *Chart) { c.Title = "Invalid\x00 title" }} {
		c := chartFixture("column")
		mutate(c)
		var b bytes.Buffer
		_, err := Write(context.Background(), &b, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "chart", Width: 100, Height: 100, Chart: c}}}}})
		if err == nil || b.Len() != 0 {
			t.Fatal("malformed chart accepted/partial ZIP", err, b.Len())
		}
	}
}
func TestChartCacheRejectsAmbiguousOrNonfiniteValues(t *testing.T) {
	base := chartXML(chartFixture("column"))
	for _, replace := range [][2]string{{`<c:v>5.25</c:v>`, `<c:v>NaN</c:v>`}, {`<c:pt idx="1">`, `<c:pt idx="0">`}, {`<c:ptCount val="3"/>`, `<c:ptCount val="2001"/>`}, {`<c:pt idx="1"><c:v>5.25</c:v></c:pt>`, ``}} {
		root, e := parseXML([]byte(strings.ReplaceAll(base, replace[0], replace[1])), 128)
		if e != nil {
			t.Fatal(e)
		}
		_, _, e = parseChartData(root, colourResolver{})
		if e == nil {
			t.Fatal("invalid cache accepted", replace)
		}
	}
}
func TestChartUnsupportedAndExternalData(t *testing.T) {
	c := chartFixture("column")
	source := strings.ReplaceAll(chartXML(c), "barChart", "scatterChart")
	root, e := parseXML([]byte(source), 128)
	if e != nil {
		t.Fatal(e)
	}
	got, w, e := parseChartData(root, colourResolver{})
	if e != nil || got != nil || len(w) != 1 || w[0].Code != "chart-type-unsupported" {
		t.Fatal("unsupported chart not diagnosed", got, w, e)
	}
	var out bytes.Buffer
	_, e = Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "chart", Width: 400, Height: 300, Chart: c}}}}})
	if e != nil {
		t.Fatal(e)
	}
	parts := packageParts(t, out.Bytes())
	parts["ppt/charts/_rels/chart-s1-o1.xml.rels"] = []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/package" Target="https://invalid.example/workbook.xlsx" TargetMode="External"/></Relationships>`)
	var edited bytes.Buffer
	z := zip.NewWriter(&edited)
	for name, data := range parts {
		w, _ := z.Create(name)
		w.Write(data)
	}
	z.Close()
	gotP, report, e := Read(context.Background(), bytes.NewReader(edited.Bytes()), int64(edited.Len()), Options{})
	if e != nil || gotP.Slides[0].Objects[0].Chart == nil || len(report.Warnings) != 1 || report.Warnings[0].Code != "chart-external-data-ignored" {
		t.Fatal("external workbook/cache policy", report, e)
	}
}
