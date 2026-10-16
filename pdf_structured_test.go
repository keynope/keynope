package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Shared semantic output corpus. Assets/content are owned, not customer decks.
func structuredPDFDeck(t *testing.T) (Deck, []string) {
	t.Helper()
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{}, Charts: map[string]ChartData{}}}
	var expected []string
	add := func(title string, element Element) {
		d.Slides = append(d.Slides, Slide{ID: fmt.Sprintf("proof-%d", len(d.Slides)), PageNumber: pageNumberHide, Notes: "PRIVATE STRUCTURED NOTES", Elements: []Element{{ID: fmt.Sprintf("heading-%d", len(d.Slides)), Kind: "heading", Level: 1, Text: title, Query: "left=10&top=2&width=225&height=5&render=truetype&modern-font=sans&modern-size=56&fg=%23172e46"}, element}})
		expected = append(expected, title)
	}
	merged := fragmentDeck(t).Objects.Tables["data"]
	merged.Cells[0][0].Text = "Merged perimeter proof"
	merged.Cells[0][0].Style.Align = "center"
	merged.Cells[0][0].Style.Vertical = "middle"
	merged.Cells[0][2].Text = "Top neighbour"
	merged.Cells[1][2].Text = "Bottom neighbour"
	d.Objects.Tables["merged"] = merged
	add("MERGED TABLE PERIMETER", Element{ID: "table", Kind: "table", ObjectID: "merged", Query: "left=10&top=10&width=225&height=36"})
	styled, _ := newTableData(4, 4)
	styled.Style.Header = true
	styled.Style.Banded = true
	size, padding := 38.0, 16.0
	styled.Style.Cell.FontSize = &size
	styled.Style.Cell.Padding = &padding
	styled.Style.Cell.FontID = "sans"
	styled.Style.Cell.Vertical = "middle"
	texts := [][]string{{"Workshop", "Owner", "Status", "Time"}, {"Explore", "Group A", "Complete", "15 min"}, {"Build", "Group B", "In progress", "30 min"}, {"Review", "Group C", "Ready", "10 min"}}
	for r, row := range texts {
		for c, text := range row {
			styled.Cells[r][c].Text = text
		}
	}
	styled.Cells[1][2] = TableCell{Runs: []ObjectTextRun{{Text: "Complete", Bold: true, Color: "#007755"}}}
	styled.Cells[2][2] = TableCell{Runs: []ObjectTextRun{{Text: "In ", Italic: true}, {Text: "progress", Bold: true, Underline: true, FontID: "c64", Color: "#713fd0"}}}
	styled.Cells[3][2].Style = &TableCellStyle{Align: "center", Fill: "theme:accent-4", Color: "#ffffff"}
	d.Objects.Tables["styled"] = styled
	add("RICH ROTATED TABLE", Element{ID: "table", Kind: "table", ObjectID: "styled", Query: "left=18&top=13&width=209&height=28&object-rotation=5"})
	dense, _ := newTableData(20, 20)
	dense.Style.Header = false
	dense.Style.Banded = false
	small, inset := 13.0, 3.0
	dense.Style.Cell.FontSize = &small
	dense.Style.Cell.Padding = &inset
	dense.Style.Cell.Color = "#172e46"
	for r := range dense.Cells {
		for c := range dense.Cells[r] {
			dense.Cells[r][c].Text = fmt.Sprintf("%d:%d", r+1, c+1)
		}
	}
	d.Objects.Tables["dense"] = dense
	add("DENSE TABLE EDGE CHECK", Element{ID: "table", Kind: "table", ObjectID: "dense", Query: "left=10&top=8&width=225&height=45"})
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		chart := newChartData(kind)
		chart.Title = "Quarterly workshop outcomes"
		chart.Categories = []string{"Discovery", "Prototyping", "Validation", "Delivery"}
		chart.Series = []ChartSeries{{Name: "Team A", Values: []float64{5, 9, 3, 7}}, {Name: "Team B", Values: []float64{7, 4, 8, 6}}}
		if kind == "line" || kind == "area" || kind == "bar" || kind == "column" {
			chart.Series[0].Values[2] = -3
			chart.Options.XTitle = "Workshop stage"
			chart.Options.YTitle = "Change in score"
		}
		if kind == "pie" || kind == "doughnut" {
			chart.Series = chart.Series[:1]
		}
		chart.Options.Legend = true
		chart.Options.DataLabels = true
		d.Objects.Charts[kind] = chart
		query := "left=10&top=9&width=225&height=44"
		if os.Getenv("KEYNOPE_PDF_ROTATED") == "1" {
			angle := 6
			if len(d.Slides)%2 == 0 {
				angle = -6
			}
			query = fmt.Sprintf("left=20&top=13&width=205&height=36&object-rotation=%d", angle)
		}
		add(strings.ToUpper(kind)+" CHART", Element{ID: "chart", Kind: "chart", ObjectID: kind, Query: query})
	}
	clipped, _ := newTableData(2, 2)
	clipped.Style.Header = false
	clipped.Style.Banded = false
	normal, clipInset := 34.0, 20.0
	clipped.Style.Cell.FontSize = &normal
	clipped.Style.Cell.Padding = &clipInset
	large := 120.0
	clipped.Cells[0][0] = TableCell{Text: "Visible text stays inside this cell.\nOVERFLOW MUST NOT CROSS THE CELL BORDER.\nExtra lines remain clipped.", Style: &TableCellStyle{FontSize: &large, Fill: "#943838", Color: "#ffffff"}}
	clipped.Cells[0][1].Text = "Clear neighbour"
	clipped.Cells[1][0].Text = "Clean lower row"
	clipped.Cells[1][1].Text = "Contents stay in their cell."
	d.Objects.Tables["clipped"] = clipped
	add("CLIPPED CELL BOUNDARIES", Element{ID: "table", Kind: "table", ObjectID: "clipped", Query: "left=10&top=12&width=225&height=26"})
	d.Slides = append(d.Slides, Slide{ID: "private-tab", TabID: "private-tab", Elements: []Element{{Kind: "text", Text: "PRIVATE STRUCTURED TAB"}}})
	return d, expected
}

func TestStructuredPDFManifestAndFixture(t *testing.T) {
	d, expected := structuredPDFDeck(t)
	before := cloneDeck(d)
	doc, err := makePDFExportDocument("Structured output.md", d, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, before) || len(doc.Manifest.Pages) != len(expected) {
		t.Fatal("manifest mutated deck or included tab")
	}
	for i, page := range doc.Manifest.Pages {
		if !page.Quality || page.Width/page.Height != 16.0/9 || page.Page.Scene == nil {
			t.Fatal("semantic page lost quality eligibility", i, page.Reasons)
		}
		if len(page.Page.Scene.Objects) != 2 {
			t.Fatal("unexpected page objects", i)
		}
	}
	for _, body := range []string{doc.Content, doc.FallbackContent} {
		if strings.Contains(body, "PRIVATE STRUCTURED") {
			t.Fatal("PDF leaked authoring metadata")
		}
	}
	if root := os.Getenv("KEYNOPE_STRUCTURED_PDF_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{"quality.html": doc.Content, "raster.html": doc.FallbackContent} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(expected)
		if err := os.WriteFile(filepath.Join(root, "expected.json"), raw, 0644); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "structured.md"), d); err != nil {
			t.Fatal(err)
		}
	}
}
