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

func comparableDetachedObject(object sceneObject) sceneObject {
	object.Table = cloneSceneTable(object.Table)
	if object.Table != nil {
		for i := range object.Table.Cells {
			concreteThemeTestText(object.Table.Cells[i].Text)
		}
	}
	if object.Chart != nil {
		chart := *object.Chart
		chart.Data = ChartData{} // Payload formatting becomes concrete by design.
		object.Chart = &chart
	}
	return object
}

func TestConcreteThemeSharedMasterPayloads(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, palette := range []string{"clean-light", "clean-dark", "editorial", "bold", "workshop", "c64"} {
		for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut", "column-stacked", "bar-stacked"} {
			t.Run(palette+"/"+kind, func(t *testing.T) {
				table, _ := newTableData(3, 3)
				table.Style.Header, table.Style.Banded = true, true
				for r := range table.Cells {
					for c := range table.Cells[r] {
						table.Cells[r][c].Text = fmt.Sprintf("Cell %d:%d", r+1, c+1)
					}
				}
				table.Cells[1][1].Text = ""
				table.Cells[1][1].Runs = []ObjectTextRun{{Text: "Linked", Bold: true, Color: "#010203", ColorToken: "accent-2", FontID: "c64", FontRole: "mono"}}
				table.Cells[0][0].Style = &TableCellStyle{Fill: "#ffffaa"}
				table.Cells[1][1].Style = &TableCellStyle{Color: "#ff0055"}
				chart := newChartData(strings.TrimSuffix(kind, "-stacked"))
				chart.Options.Stacked, chart.Options.DataLabels = strings.HasSuffix(kind, "-stacked"), true
				deck := Deck{Theme: builtinDeckTheme(palette), Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"shared-table": table}, Charts: map[string]ChartData{"shared-chart": chart}}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{Elements: []Element{
					{ID: "master-table", Kind: "table", ObjectID: "shared-table", Query: "left=4&top=2&width=100&height=24"},
					{ID: "master-chart", Kind: "chart", ObjectID: "shared-chart", Query: "left=112&top=2&width=128&height=24"},
				}}}, Layouts: []MasterLayout{{ID: "layout", Name: "Layout"}}}}
				for i, color := range []string{"", "#ff5500", "#0055ff"} {
					slide := Slide{ID: fmt.Sprintf("slide-%d", i), LayoutID: "layout"}
					if color != "" {
						slide.FG, _ = ansiFG(color)
						slide.FGSet = true
					}
					font := "sans"
					if i == 1 {
						font = "mono"
					}
					if i == 2 {
						font = "c64"
					}
					slide.Elements = []Element{{ID: fmt.Sprintf("local-chart-%d", i), Kind: "chart", ObjectID: "shared-chart", Query: "left=4&top=30&width=128&height=24&modern-font=" + font}}
					deck.Slides = append(deck.Slides, slide)
				}
				before := cloneDeck(deck)
				plain := cloneDeck(deck)
				var err error
				plain.Objects.Tables["shared-table"], err = concreteThemeTable(deck, table)
				if err != nil {
					t.Fatal(err)
				}
				plain.Objects.Charts["shared-chart"], _, err = concreteThemeChart(deck, Element{Kind: "chart"}, chart)
				if err != nil {
					t.Fatal(err)
				}
				plain.Theme = nil
				encoded, err := serializeDeck("shared.md", plain)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("shared.md", encoded)
				if err != nil {
					t.Fatal(err)
				}
				for i := range deck.Slides {
					want, err := buildSlideScene(deck, i, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					got, err := buildSlideScene(plain, i, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					if len(want.Objects) != len(got.Objects) {
						t.Fatal("lost master instances")
					}
					for j := range want.Objects {
						w, g := comparableDetachedObject(want.Objects[j]), comparableDetachedObject(got.Objects[j])
						if !reflect.DeepEqual(w, g) {
							wb, _ := json.Marshal(w)
							gb, _ := json.Marshal(g)
							t.Fatalf("shared owner changed on slide %d object %s\nwant %s\ngot %s", i, w.ID, wb, gb)
						}
						if w.Table != nil && !reflect.DeepEqual(sceneTableToPPTX(w.Table), sceneTableToPPTX(g.Table)) {
							t.Fatal("editable PowerPoint table formatting changed", i, w.ID)
						}
						if w.Chart != nil && !reflect.DeepEqual(sceneChartToPPTX(want.Objects[j].Chart), sceneChartToPPTX(got.Objects[j].Chart)) {
							t.Fatal("editable PowerPoint chart formatting changed", i, w.ID)
						}
					}
					again, err := buildSlideScene(reopened, i, 245, 56)
					if err != nil || !reflect.DeepEqual(again.Objects, got.Objects) {
						t.Fatal("concrete defaults lost on save/reopen", i, err)
					}
				}
				if !reflect.DeepEqual(deck, before) || !reflect.DeepEqual(deck.Masters, plain.Masters) || !reflect.DeepEqual(deck.Slides, plain.Slides) || len(plain.Objects.Tables) != 1 || len(plain.Objects.Charts) != 1 {
					t.Fatal("shared source or master structure was flattened/forked")
				}
				if palette == "clean-dark" && kind == "column" {
					originalEdit, concreteEdit := cloneDeck(deck), cloneDeck(plain)
					for _, edit := range []*Deck{&originalEdit, &concreteEdit} {
						edit.Masters.Base.Slide.Elements[1].Query = setQueryValue(edit.Masters.Base.Slide.Elements[1].Query, "modern-font", "mono")
						changedTable := edit.Objects.Tables["shared-table"]
						changedTable.Cells[2][2].Text = "Updated"
						edit.Objects.Tables["shared-table"] = changedTable
						changedChart := edit.Objects.Charts["shared-chart"]
						changedChart.Title = "Updated title"
						edit.Objects.Charts["shared-chart"] = changedChart
					}
					for i := range originalEdit.Slides {
						w, we := buildSlideScene(originalEdit, i, 245, 56)
						g, ge := buildSlideScene(concreteEdit, i, 245, 56)
						if we != nil || ge != nil {
							t.Fatal(we, ge)
						}
						for j := range w.Objects {
							if !reflect.DeepEqual(comparableDetachedObject(w.Objects[j]), comparableDetachedObject(g.Objects[j])) {
								t.Fatal("master font or shared content edit stopped propagating", i, j)
							}
						}
					}
				}
				if directory := os.Getenv("KEYNOPE_SHARED_DETACH_FIXTURE"); directory != "" && palette == "clean-dark" && kind == "column" {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					for name, document := range map[string]Deck{"source": deck, "concrete": plain} {
						document = cloneDeck(document)
						// Isolate structured defaults from the still-unimplemented
						// deck background detachment; both proof files author it.
						for i := range document.Slides {
							document.Slides[i].BG, _ = ansiBG(deck.Theme.Colors.Background)
							document.Slides[i].BGSet = true
						}
						data, err := serializeDeck(name+".md", document)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(directory, name+".md"), data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestStructuredConcreteFallbackAdmission(t *testing.T) {
	for _, value := range []string{"none", "theme:text", "red", "#12345", "NaN"} {
		if (TableCellStyle{DefaultColor: value}).Validate() == nil {
			t.Fatal("bad table fallback accepted", value)
		}
		chart := newChartData("column")
		chart.Options.DefaultColor = value
		if chart.Validate() == nil {
			t.Fatal("bad chart fallback accepted", value)
		}
	}
	for _, value := range []string{"theme", "file:///private/font.ttf", "unknown"} {
		chart := newChartData("column")
		chart.Options.DefaultFontID = value
		if chart.Validate() == nil {
			t.Fatal("bad chart fallback font accepted", value)
		}
	}
	for _, raw := range []string{`{"defaultColor":"#112233","DefaultColor":"#445566"}`, `{"defaultFontId":"sans","defaultFontId":"mono"}`} {
		if strings.Contains(raw, "FontId") {
			var options ChartOptions
			if json.Unmarshal([]byte(raw), &options) == nil {
				t.Fatal("duplicate chart fallback accepted")
			}
		} else {
			var style TableCellStyle
			if json.Unmarshal([]byte(raw), &style) == nil {
				t.Fatal("duplicate cell fallback accepted")
			}
		}
	}
}
