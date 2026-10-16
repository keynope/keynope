package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func TestChartPPTXUsesResolvedLabelContrast(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"column-stacked", "bar-stacked", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			chart := newChartData(strings.TrimSuffix(kind, "-stacked"))
			chart.Options.Stacked, chart.Options.DataLabels = strings.HasSuffix(kind, "-stacked"), true
			deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"chart": chart}}, Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "chart", Kind: "chart", ObjectID: "chart", Query: "left=10&top=4&width=220&height=48"}}}}}
			scene, err := buildSlideScene(deck, 0, 245, 56)
			if err != nil || len(scene.Objects) != 1 {
				t.Fatal(err)
			}
			resolved := scene.Objects[0].Chart
			data, _, err := exportPPTXDocument(deck)
			if err != nil {
				t.Fatal(err)
			}
			presentation, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var actual *pptx.Chart
			for _, object := range presentation.Slides[0].Objects {
				if object.Chart != nil {
					actual = object.Chart
				}
			}
			if actual == nil {
				t.Fatal("editable chart missing")
			}
			if chart.Options.Stacked {
				if len(resolved.SeriesLabelColors) == 0 || !strings.EqualFold(actual.Series[0].LabelColor, resolved.SeriesLabelColors[0]) {
					t.Fatal("stacked contrast lost in native package", actual.Series[0].LabelColor, resolved.SeriesLabelColors)
				}
			} else {
				if len(actual.CategoryLabelColors) != len(resolved.CategoryLabelColors) || len(actual.CategoryLabelColors) == 0 {
					t.Fatal("slice label palette missing")
				}
				for i, color := range actual.CategoryLabelColors {
					if !strings.EqualFold(color, resolved.CategoryLabelColors[i]) {
						t.Fatal("slice contrast lost in native package", i, color, resolved.CategoryLabelColors[i])
					}
				}
			}
		})
	}
}
