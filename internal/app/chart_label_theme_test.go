package app

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestChartLabelThemeLifecycleAndNativeOutput(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			deck := chartThemeFixture(kind)
			data := deck.Objects.Charts["chart-theme-data"]
			data.Options.DataLabels = true
			// Isolate label dependencies from mark-paint theme roles.
			for i := range data.Series {
				data.Series[i].ColorToken = ""
			}
			for i := range data.CategoryColors {
				data.CategoryColors[i].ColorToken = ""
			}
			circular := kind == "pie" || kind == "doughnut"
			if circular {
				data.CategoryColors[0].LabelColor = "theme:accent-6"
				data.CategoryColors[1].LabelColor = deck.Theme.Colors.Accent[5]
			} else {
				data.Series[0].LabelColor = "theme:accent-6"
				data.Series[1].LabelColor = deck.Theme.Colors.Accent[5]
			}
			deck.Objects.Charts["chart-theme-data"] = data
			if err := data.Validate(); err != nil || !chartHasColorTokens(data) {
				t.Fatal("label role rejected", err)
			}
			before := cloneDeck(deck)
			edited := cloneDeck(deck)
			edited.Theme.Colors.Accent[5] = "#bd128f"
			if sceneProjectionInputsEqual(deck, edited, sceneResponseKey{index: 0}) {
				t.Fatal("label-only palette edit reused stale scene")
			}
			hidden := cloneDeck(deck)
			hiddenData := hidden.Objects.Charts["chart-theme-data"]
			hiddenData.Options.DataLabels = false
			hidden.Objects.Charts["chart-theme-data"] = hiddenData
			hiddenEdited := cloneDeck(hidden)
			hiddenEdited.Theme.Colors.Accent[5] = "#bd128f"
			if !sceneProjectionInputsEqual(hidden, hiddenEdited, sceneResponseKey{index: 0}) {
				t.Fatal("inactive labels invalidated scene")
			}
			scene, err := buildSlideScene(edited, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			labels := scene.Objects[0].Chart.SeriesLabelColors
			if circular {
				labels = scene.Objects[0].Chart.CategoryLabelColors
			}
			if labels[0] != "#bd128f" || labels[1] != deck.Theme.Colors.Accent[5] {
				t.Fatal("linked/literal label paints conflated", labels)
			}
			owner := edited.Slides[0].Elements[0]
			fallback := captureClipboardChartTheme(edited, owner, data)
			if err := fallback.validate(data); err != nil {
				t.Fatal(err)
			}
			plain := applyClipboardChartTheme(data, &owner, fallback)
			concrete, _, err := concreteThemeChart(edited, edited.Slides[0].Elements[0], data)
			if err != nil {
				t.Fatal(err)
			}
			for _, frozen := range []ChartData{plain, concrete} {
				if chartHasColorTokens(frozen) {
					t.Fatal("frozen chart retained theme role")
				}
				actual := frozen.Series[0].LabelColor
				if circular {
					actual = frozen.CategoryColors[0].LabelColor
				}
				if actual != "#bd128f" {
					t.Fatal("label fallback stale", actual)
				}
			}
			md, err := serializeDeck("Chart labels.md", edited)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Chart labels.md", md)
			if err != nil || !reflect.DeepEqual(reopened.Objects.Charts["chart-theme-data"], data) {
				t.Fatal("label role not portable", err)
			}
			for _, visible := range []bool{true, false} {
				d := cloneDeck(edited)
				c := d.Objects.Charts["chart-theme-data"]
				c.Options.DataLabels = visible
				d.Objects.Charts["chart-theme-data"] = c
				encoded, _, err := exportPPTXDocument(d)
				if err != nil {
					t.Fatal("native output rejected label role", visible, err)
				}
				native, _, err := pptx.Read(context.Background(), bytes.NewReader(encoded), int64(len(encoded)), pptx.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if visible {
					found := false
					for _, o := range native.Slides[0].Objects {
						if o.Chart == nil {
							continue
						}
						actual := o.Chart.Series[0].LabelColor
						if circular {
							actual = o.Chart.CategoryLabelColors[0]
						}
						found = strings.EqualFold(actual, "#bd128f")
					}
					if !found {
						t.Fatal("native output lost resolved label colour")
					}
				}
			}
			if !reflect.DeepEqual(deck, before) {
				t.Fatal("projection/freeze mutated authored chart")
			}
		})
	}
}

func TestChartLabelThemeInvalidRoleRejected(t *testing.T) {
	for _, value := range []string{"theme:unknown", "theme:", "#12345", "theme:accent-0"} {
		deck := chartThemeFixture("column")
		data := cloneChart(deck.Objects.Charts["chart-theme-data"])
		data.Series[0].LabelColor = value
		if data.Validate() == nil {
			t.Fatal("accepted invalid label role", value)
		}
		session := newNativeEditorSession("Labels.md", deck)
		before, revision := cloneDeck(session.deck), session.version
		if err := session.apply(nativeEditorAction{Action: "set-chart-data", ObjectID: "chart-theme", ChartData: &data}); err == nil || session.version != revision || len(session.undo) != 0 || !reflect.DeepEqual(session.deck, before) {
			t.Fatal("invalid label role changed document/history", value, err)
		}
	}
}

func TestPPTXChartLabelBindingScope(t *testing.T) {
	source := pptxThemeImportFixture(t)
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(source), int64(len(source)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	binding := newPPTXThemeBinding(native.Theme)
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			c := &pptx.Chart{Type: kind, Categories: []string{"Linked", "Foreign", "Literal"}, Series: []pptx.ChartSeries{{Name: "Linked", Values: []float64{3, 2, 1}, LabelColor: "#112233", LabelThemeSlot: "accent5", LabelThemeSource: native.Theme.SourcePart}}, Options: pptx.ChartOptions{DataLabels: true}}
			if kind == "pie" || kind == "doughnut" {
				c.CategoryLabelColors = []string{"#112233", "#112233", "#112233"}
				c.CategoryLabelSources = []pptx.ThemeColourReference{{Slot: "accent5", SourcePart: native.Theme.SourcePart}, {Slot: "accent5", SourcePart: "ppt/theme/foreign.xml"}, {}}
			} else {
				foreign := c.Series[0]
				foreign.Name, foreign.LabelThemeSource = "Foreign", "ppt/theme/foreign.xml"
				literal := c.Series[0]
				literal.Name, literal.LabelThemeSlot, literal.LabelThemeSource = "Literal", "", ""
				c.Series = append(c.Series, foreign, literal)
			}
			data, _, err := pptxChartToData(c)
			if err != nil {
				t.Fatal(err)
			}
			owner := Element{Kind: "chart"}
			result, err := binding.chart(Deck{}, c, &owner, data)
			if err != nil {
				t.Fatal(err)
			}
			paints := []string{}
			if kind == "pie" || kind == "doughnut" {
				for _, s := range result.CategoryColors {
					paints = append(paints, s.LabelColor)
				}
			} else {
				for _, s := range result.Series {
					paints = append(paints, s.LabelColor)
				}
			}
			if !reflect.DeepEqual(paints, []string{"theme:accent-5", "#112233", "#112233"}) {
				t.Fatal("label source ownership lost", paints)
			}
		})
	}
}
