package app

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestChartLabelStyleProjectionAndThemeDependencies(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		deck := chartThemeFixture(kind)
		data := deck.Objects.Charts["chart-theme-data"]
		width := 72.0
		data.Options.FontID = "mono"
		data.Options.FontWidth = &width
		data.Options.GridColor = "theme:accent-4"
		before := cloneChart(data)
		inputs := deck.chartInputs(data, 1000, 600, "#123456", "")
		if inputs.Text.FontID != "mono" || inputs.Text.WidthScale != .72 {
			t.Fatal(kind, inputs.Text)
		}
		if kind != "pie" && kind != "doughnut" && inputs.GridColor != deck.Theme.Colors.Accent[3] {
			t.Fatal("grid role not resolved", kind, inputs.GridColor)
		}
		other := cloneDeck(deck)
		other.Theme.Fonts.Body.FontID = "sans"
		other.Theme.Fonts.Body.Width = 150
		other.Theme.Colors.Line = "#abcdef"
		if next := other.chartInputs(data, 1000, 600, "#123456", ""); !reflect.DeepEqual(inputs, next) {
			t.Fatal("explicit chart styles retained irrelevant theme dependencies", kind)
		}
		other.Theme.Colors.Accent[3] = "#abcdef"
		if next := other.chartInputs(data, 1000, 600, "#123456", ""); kind != "pie" && kind != "doughnut" && reflect.DeepEqual(inputs, next) {
			t.Fatal("grid role was not a theme dependency")
		}
		if !reflect.DeepEqual(data, before) {
			t.Fatal("projection mutated authored data")
		}
		deck.Objects.Charts["chart-theme-data"] = data
		md, err := serializeDeck("style.md", deck)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseDeckData("style.md", md)
		if err != nil || !reflect.DeepEqual(reopened.Objects.Charts["chart-theme-data"], data) {
			t.Fatal("chart style Markdown round trip", err)
		}
		public, err := participantSlideMarkdown(deck, 0)
		if err != nil {
			t.Fatal(err)
		}
		audience, err := parseDeckData("public.md", public)
		if err != nil {
			t.Fatal(err)
		}
		sourceScene, err := buildSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		publicScene, err := buildSlideScene(audience, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sourceScene.Objects[0].Chart, publicScene.Objects[0].Chart) {
			t.Fatal("public chart style differs", kind)
		}
		copy := cloneChart(data)
		*copy.Options.FontWidth = 91
		if *data.Options.FontWidth != 72 {
			t.Fatal("chart clone aliases width")
		}
	}
}

func TestChartLabelStyleInvalidInputAtomic(t *testing.T) {
	for _, mutate := range []func(*ChartOptions){
		func(o *ChartOptions) { o.FontID = "foreign" },
		func(o *ChartOptions) { v := 201.0; o.FontWidth = &v },
		func(o *ChartOptions) { v := math.NaN(); o.FontWidth = &v },
		func(o *ChartOptions) { o.GridColor = "theme:unknown" },
	} {
		deck := chartThemeFixture("column")
		session := newNativeEditorSession("style.md", deck, false, false)
		before := cloneDeck(session.deck)
		options := session.deck.Objects.Charts["chart-theme-data"].Options
		mutate(&options)
		if err := session.apply(nativeEditorAction{Action: "set-chart-options", ObjectID: "chart-theme", ChartOptions: &options}); err == nil || !reflect.DeepEqual(session.deck, before) || len(session.undo) != 0 {
			t.Fatal("invalid chart style accepted or mutated history", err)
		}
	}
	var options ChartOptions
	if err := json.Unmarshal([]byte(`{"fontId":"mono","fontWidth":72,"gridColor":"#123456","future":true}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.Extra == nil || len(*options.Extra) != 1 {
		t.Fatal("known styles treated as opaque metadata")
	}
}

func TestChartDataLabelOffUsesTransparentTextPaint(t *testing.T) {
	for _, kind := range []string{"column", "pie"} {
		deck := chartThemeFixture(kind)
		data := deck.Objects.Charts["chart-theme-data"]
		data.Options.DataLabels = true
		if kind == "pie" {
			data.CategoryColors[0].LabelColor = "none"
		} else {
			data.Series[0].LabelColor = "none"
		}
		deck.Objects.Charts["chart-theme-data"] = data
		scene, err := buildSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, label := range scene.Objects[0].Chart.Labels {
			if label.Paint.Color == "none" {
				t.Fatal("invalid CSS colour used for label Off")
			}
			found = found || label.Paint.Color == "transparent"
		}
		if !found {
			t.Fatal("no transparent data label", kind)
		}
	}
}
