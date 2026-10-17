package app

import (
	"fmt"
	"reflect"
)

type editorChartData struct {
	ObjectID string    `json:"objectId"`
	Data     ChartData `json:"data"`
}

func isChartCommand(action string) bool {
	switch action {
	case "insert-chart", "set-chart-data", "set-chart-options", "set-chart-series-style":
		return true
	}
	return false
}

func newChartData(kind string) ChartData {
	if kind == "" {
		kind = "column"
	}
	return ChartData{Version: 1, Type: kind, Title: "Chart", Categories: []string{"First", "Second", "Third", "Fourth"}, Series: []ChartSeries{{Name: "Series 1", Values: []float64{4, 7, 5, 9}}}, Options: ChartOptions{Legend: true}}
}

// Every host uses this authored transaction. Shared instances fork their data
// before edits; no mutation touches an inherited preview or the caller's buffer.
func applyChartCommand(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	var slide *Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			return false, errInvalidEditorAction
		}
		slide = masterSlideAt(deck, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			return false, errInvalidEditorAction
		}
		slide = &deck.Slides[action.Slide]
	}
	if action.Action == "insert-chart" {
		data := newChartData(action.Kind)
		if action.ChartData != nil {
			data = cloneChart(*action.ChartData)
		}
		if err := data.Validate(); err != nil {
			return false, err
		}
		objects := deck.Objects.cloneForPayloadReplacement()
		if objects == nil {
			objects = &DeckObjects{Version: 1}
		}
		if objects.Charts == nil {
			objects.Charts = map[string]ChartData{}
		}
		id := newStableID("chart-data")
		objects.Charts[id] = data
		slide.Elements = append(append([]Element(nil), slide.Elements...), Element{Kind: "chart", ID: newStableID("chart"), ObjectID: id, Query: "left=24&top=8&width=170&height=36"})
		deck.Objects = objects
		return true, nil
	}
	if err := requireSelectionObjectCommand(*deck, *slide, action.ObjectID, editingGroup, "chart.edit"); err != nil {
		return false, err
	}
	for index, element := range slide.Elements {
		if element.ID != action.ObjectID {
			continue
		}
		if element.Kind != "chart" || deck.Objects == nil {
			return false, editorFailure("missing-reference", "", action.ObjectID, fmt.Errorf("chart no longer exists"))
		}
		previous, ok := deck.Objects.Charts[element.ObjectID]
		if !ok {
			return false, editorFailure("missing-reference", "", action.ObjectID, fmt.Errorf("chart data is missing"))
		}
		data := cloneChart(previous)
		switch action.Action {
		case "set-chart-data":
			if action.ChartData == nil {
				return false, errInvalidEditorAction
			}
			data = cloneChart(*action.ChartData)
		case "set-chart-options":
			if action.ChartOptions == nil {
				return false, errInvalidEditorAction
			}
			data.Options = *action.ChartOptions
			data = cloneChart(data)
		case "set-chart-series-style":
			if action.ChartSeriesIndex == nil || *action.ChartSeriesIndex < 0 || *action.ChartSeriesIndex >= len(data.Series) || action.ChartSeriesColor == nil && action.ChartSeriesColorToken == nil || action.ChartSeriesColor != nil && action.ChartSeriesColorToken != nil {
				return false, errInvalidEditorAction
			}
			series := &data.Series[*action.ChartSeriesIndex]
			if action.ChartSeriesColorToken != nil {
				series.ColorToken = *action.ChartSeriesColorToken
				series.Color = ""
				if series.ColorToken != "" {
					if !validThemeColorToken(series.ColorToken) {
						return false, fmt.Errorf("invalid chart colour role")
					}
					series.Color = deck.themeTokenColor(series.ColorToken)
				}
			} else {
				series.Color, series.ColorToken = *action.ChartSeriesColor, ""
			}
		default:
			return false, errInvalidEditorAction
		}
		if err := data.Validate(); err != nil {
			return false, err
		}
		if reflect.DeepEqual(previous, data) {
			return false, nil
		}
		consumers := 0
		visit := func(s Slide) {
			for _, e := range s.Elements {
				if e.Kind == "chart" && e.ObjectID == element.ObjectID {
					consumers++
				}
			}
		}
		for _, s := range deck.Slides {
			visit(s)
		}
		visit(deck.Masters.Base.Slide)
		for _, l := range deck.Masters.Layouts {
			visit(l.Slide)
		}
		objects := deck.Objects.cloneForPayloadReplacement()
		id := element.ObjectID
		if consumers > 1 {
			id = newStableID("chart-data")
		}
		objects.Charts[id] = data
		slide.Elements = append([]Element(nil), slide.Elements...)
		slide.Elements[index].ObjectID = id
		deck.Objects = objects
		return true, nil
	}
	return false, fmt.Errorf("chart no longer exists")
}
