package main

import (
	"strings"
	"testing"
)

func TestChartAxisZeroTickDoesNotAccumulateFloatingError(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area"} {
		for _, magnitude := range []float64{1, 1e-12, 1e12} {
			data := ChartData{Version: 1, Type: kind, Categories: []string{"First", "Second"}, Series: []ChartSeries{{Name: "Values", Values: []float64{-4 * magnitude, 8 * magnitude}}}}
			chart, err := (Deck{Theme: builtinDeckTheme("clean-light")}).projectChart(data, sceneRect{Width: 1700, Height: 926}, "")
			if err != nil {
				t.Fatal(err)
			}
			zero, numeric := 0, 0
			for _, label := range chart.Labels {
				if label.FullText == "0" {
					zero++
				}
				if strings.Contains(label.FullText, "e-") || strings.Contains(label.FullText, "e+") {
					numeric++
				}
				if strings.Contains(label.FullText, "e-16") && magnitude == 1 {
					t.Fatal("zero residue formatted as data", kind, label.FullText)
				}
			}
			if zero != 1 {
				t.Fatal("missing/duplicate exact zero tick", kind, magnitude, zero)
			}
			if magnitude != 1 && numeric == 0 {
				t.Fatal("small/large real values were flattened", kind, magnitude)
			}
		}
	}
	if chartNumber(1e-16, "") == "0" {
		t.Fatal("data number formatter rounded away a real tiny value")
	}
}
