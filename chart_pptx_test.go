package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestChartPPTXEditableDeckRoundTrip(t *testing.T) {
	table, _ := newTableData(2, 2)
	d := Deck{Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table": table}, Charts: map[string]ChartData{}}}
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		c := newChartData(kind)
		c.Title = "Workshop outcomes"
		c.Options.DataLabels = true
		if kind != "pie" && kind != "doughnut" {
			c.Options.XTitle = "Teams"
			c.Options.YTitle = "Scores"
			c.Series[0].Values[0] = -3
		}
		c.Series[0].Color = "#ff5500"
		d.Objects.Charts[kind] = c
		d.Slides = append(d.Slides, Slide{ID: kind, Elements: []Element{{ID: kind, Kind: "chart", ObjectID: kind, Query: "left=20&top=8&width=180&height=36&object-rotation=12"}, {ID: "table-" + kind, Kind: "table", ObjectID: "table", Query: "left=10&top=1&width=30&height=5"}}})
	}
	before, _ := json.Marshal(d)
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) > 0 {
		t.Fatal("unexpected chart export warning", report)
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("export mutated document")
	}
	p, report, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	for _, slide := range p.Slides {
		found := false
		for _, o := range slide.Objects {
			if o.Kind == "chart" {
				found = true
				if o.Chart == nil {
					t.Fatal("no data")
				}
			}
		}
		if !found {
			t.Fatal("chart flattened")
		}
	}
	imported, report, err := importPPTXDocument(data, "charts.pptx")
	if err != nil {
		t.Fatal(err, report)
	}
	if imported.Objects == nil || len(imported.Objects.Charts) != 6 || len(imported.Objects.Tables) != 6 {
		t.Fatal("mixed object registries", imported.Objects)
	}
	if err := validateDeckObjectReferences(imported); err != nil {
		t.Fatal(err)
	}
	for i, slide := range imported.Slides {
		beforeScene, err := buildSlideScene(d, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		var wantedFont, wantedColor string
		for _, o := range beforeScene.Objects {
			if o.Chart != nil && len(o.Chart.Labels) > 0 {
				wantedFont = o.Chart.Labels[0].Text.FontID
				wantedColor = o.Chart.Labels[0].Paint.Color
			}
		}
		for _, e := range slide.Elements {
			if e.Kind != "chart" {
				continue
			}
			got := imported.Objects.Charts[e.ObjectID]
			want := d.Objects.Charts[d.Slides[i].ID]
			if got.Type != want.Type || got.Title != want.Title || !reflect.DeepEqual(got.Categories, want.Categories) || !reflect.DeepEqual(got.Series[0].Values, want.Series[0].Values) || got.Options.XTitle != want.Options.XTitle {
				t.Fatal("chart data lost", got, want)
			}
		}
		scene, err := buildSlideScene(imported, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range scene.Objects {
			if o.Kind == "chart" && o.Chart != nil && len(o.Chart.Paths) > 0 {
				found = true
				if len(o.Chart.Labels) == 0 || o.Chart.Labels[0].Text.FontID != wantedFont || !strings.EqualFold(o.Chart.Labels[0].Paint.Color, wantedColor) {
					t.Fatal("imported chart label family/color lost", o.Chart.Labels, wantedFont, wantedColor)
				}
			}
		}
		if !found {
			t.Fatal("imported chart does not render")
		}
	}
	md, err := serializeDeck("charts.md", imported)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("charts.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imported.Objects.Charts, reopened.Objects.Charts) {
		t.Fatal("chart metadata did not survive Markdown")
	}
	if output := os.Getenv("KEYNOPE_CHART_PPTX_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "editable-charts.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "imported-charts.md"), md, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func TestUnsupportedChartSourceSurvivesMarkdown(t *testing.T) {
	c := &pptx.Chart{Type: "column", Categories: []string{"A", "B"}, Series: []pptx.ChartSeries{{Name: "S", Values: []float64{1, 2}}}}
	var b bytes.Buffer
	_, err := pptx.Write(context.Background(), &b, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{{Kind: "chart", Width: 600, Height: 400, Chart: c}}}}})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var changed bytes.Buffer
	zw := zip.NewWriter(&changed)
	for _, f := range z.File {
		r, _ := f.Open()
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(f.Name, "ppt/charts/") && strings.HasSuffix(f.Name, ".xml") {
			data = bytes.ReplaceAll(data, []byte("barChart"), []byte("scatterChart"))
		}
		w, _ := zw.Create(f.Name)
		w.Write(data)
	}
	zw.Close()
	d, report, err := importPPTXDocument(changed.Bytes(), "unsupported.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "chart-type-unsupported" {
		t.Fatal(report)
	}
	if d.Objects == nil || d.Objects.Extra == nil || len(*d.Objects.Extra) != 1 {
		t.Fatal("recoverable source dropped")
	}
	md, err := serializeDeck("source.md", d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseDeckData("source.md", md)
	if err != nil || got.Objects == nil || !reflect.DeepEqual(got.Objects.Extra, d.Objects.Extra) {
		t.Fatal("source metadata lost", err)
	}
}

func TestChartPPTXColorsIncludeZeroValues(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, kind := range []string{"column", "pie", "doughnut"} {
		data := newChartData(kind)
		for i := range data.Series[0].Values {
			data.Series[0].Values[i] = 0
		}
		data.Options.Legend = false
		scene, err := deck.projectChart(data, sceneRect{Width: 900, Height: 600}, "")
		if err != nil {
			t.Fatal(err)
		}
		got := sceneChartToPPTX(scene)
		if got.Series[0].Color != deck.Theme.Colors.Accent[0] {
			t.Fatal("zero series lost its theme color", got)
		}
		if kind != "column" {
			for i, color := range got.CategoryColors {
				if color != deck.Theme.Colors.Accent[i%len(deck.Theme.Colors.Accent)] {
					t.Fatal("zero category shifted the palette")
				}
			}
			if len(got.CategoryColors) != len(data.Categories) {
				t.Fatal("zero categories lost")
			}
		}
	}
}

func TestModernChartSingleDigitDataLabels(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	data := newChartData("column")
	data.Options.DataLabels = true
	data.Series[0].Values = []float64{4, 9, 5, 7}
	chart, err := deck.projectChart(data, sceneRect{Width: 1400, Height: 700}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"4", "9", "5", "7"} {
		found := false
		for _, label := range chart.Labels {
			if label.FullText == value && label.Bounds.X >= chart.Plot.X && label.Bounds.Y >= chart.Plot.Y && label.Bounds.X+label.Bounds.Width <= chart.Plot.X+chart.Plot.Width {
				found = true
			}
		}
		if !found {
			t.Fatal("single-digit data label suppressed by ellipsis width", value)
		}
	}
}
