package app

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestChartCommandsHistoryOwnedNormalAndMaster(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := capabilityFixture()
		s.masterMode = master
		s.currentMaster = 0
		before := cloneDeck(s.deck)
		rev := s.version
		if err := s.apply(nativeEditorAction{Action: "insert-chart", Slide: 0, SceneMaster: master, SceneRevision: &rev}); err != nil {
			t.Fatal(err)
		}
		state := s.state()
		if len(s.undo) != 1 || state.SelectedChart == nil {
			t.Fatal("chart not inserted/selected")
		}
		data := state.SelectedChart.Data
		data.Series[0].Values[1] = -12.5
		rev = s.version
		a := nativeEditorAction{Action: "set-chart-data", Slide: 0, SceneMaster: master, SceneRevision: &rev, ObjectID: state.SelectedChart.ObjectID, ChartData: &data}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		data.Series[0].Values[1] = 999
		if !reflect.DeepEqual(after, s.deck) || len(s.undo) != 2 {
			t.Fatal("chart edit aliases caller or history")
		}
		if err := s.apply(a); err == nil {
			t.Fatal("stale chart edit accepted")
		}
		data = s.state().SelectedChart.Data
		rev = s.version
		a.SceneRevision = &rev
		a.ChartData = &data
		if err := s.apply(a); err != nil || s.version != rev {
			t.Fatal("no-op changes revision", err)
		}
		for i := 0; i < 2; i++ {
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(before, s.deck) {
			t.Fatal("chart undo lost original")
		}
		for i := 0; i < 2; i++ {
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(after, s.deck) {
			t.Fatal("chart redo lost data")
		}
	}
}

func TestChartSharedPayloadForkAndInvalidAtomic(t *testing.T) {
	data := newChartData("column")
	deck := Deck{Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"data": data}}, Slides: []Slide{{Elements: []Element{{ID: "a", Kind: "chart", ObjectID: "data", Query: "left=1&top=1&width=150&height=30"}}}, {Elements: []Element{{ID: "a", Kind: "chart", ObjectID: "data"}}}}}
	deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
	before := cloneDeck(deck)
	bad := cloneChart(data)
	bad.Series[0].Values[0] = math.Inf(1)
	if _, err := applyChartCommand(&deck, nativeEditorAction{Action: "set-chart-data", ObjectID: "a", ChartData: &bad}, ""); err == nil || !reflect.DeepEqual(before, deck) {
		t.Fatal("invalid chart changed document")
	}
	color := "#123456"
	index := 0
	if changed, err := applyChartCommand(&deck, nativeEditorAction{Action: "set-chart-series-style", ObjectID: "a", ChartSeriesIndex: &index, ChartSeriesColor: &color}, ""); err != nil || !changed {
		t.Fatal(err)
	}
	id := deck.Slides[0].Elements[0].ObjectID
	if id == "data" || deck.Objects.Charts["data"].Series[0].Color != "" || deck.Objects.Charts[id].Series[0].Color != color {
		t.Fatal("shared chart mutated")
	}
	options := ChartOptions{Legend: true, DataLabels: true}
	if _, err := applyChartCommand(&deck, nativeEditorAction{Action: "set-chart-options", ObjectID: "a", ChartOptions: &options}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestChartGeometrySixTypesDeterministicFinite(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		for _, values := range [][]float64{{0, 0, 0, 0}, {4, 4, 4, 4}, {math.SmallestNonzeroFloat64, 1e-300, 1e-310, 0}, {1e308, 1e307, 0, 1e308}} {
			data := newChartData(kind)
			data.Series[0].Values = values
			data.Options.DataLabels = true
			data.Categories[0] = strings.Repeat("Long category ", 30)
			for _, bounds := range []sceneRect{{Width: 900, Height: 500}, {Width: 80, Height: 50}} {
				first, err := deck.projectChart(data, bounds, "")
				if err != nil {
					t.Fatal(kind, err)
				}
				next, _ := deck.projectChart(data, sceneRect{X: 30, Y: 40, Width: bounds.Width, Height: bounds.Height}, "")
				if first != next {
					t.Fatal("translation bypassed geometry cache")
				}
				bytes, err := json.Marshal(first)
				if err != nil || strings.Contains(string(bytes), "NaN") || strings.Contains(string(bytes), "+Inf") {
					t.Fatal(kind, "non-finite geometry", err, string(bytes))
				}
				if (kind == "pie" || kind == "doughnut") && values[0] == 0 && !first.Empty {
					t.Fatal("zero circular dataset has invented angle")
				}
				for _, label := range first.Labels {
					b := label.Bounds
					if b.X < 0 || b.Y < 0 || b.X+b.Width > bounds.Width+.001 || b.Y+b.Height > bounds.Height+.001 {
						t.Fatal("label outside chart", kind, b, bounds)
					}
				}
			}
		}
	}
	for _, kind := range []string{"column", "bar", "line", "area"} {
		data := newChartData(kind)
		data.Series[0].Values = []float64{-10, 5, -3, 0}
		data.Series = append(data.Series, ChartSeries{Name: "B", Values: []float64{2, -7, 0, 9}})
		c, err := deck.projectChart(data, sceneRect{Width: 900, Height: 500}, "")
		if err != nil || len(c.Paths) < 2 {
			t.Fatal(kind, err)
		}
	}
}

func TestChartThemeOverridesPrivacyAndPersistence(t *testing.T) {
	data := newChartData("line")
	data.Series = append(data.Series, ChartSeries{Name: "Explicit", Color: "#123456", Values: []float64{7, 9, 6, 3}})
	extra := jsonExtensions{"private": json.RawMessage(`"secret"`)}
	data.Extra = &extra
	data.Series[0].Extra = &extra
	data.Options.Extra = &extra
	deck := Deck{Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"data": data}}, Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "chart", Kind: "chart", ObjectID: "data", Query: "left=12&top=8&width=180&height=36"}}}}}
	c, err := deck.projectChart(data, sceneRect{Width: 900, Height: 500}, "")
	if err != nil {
		t.Fatal(err)
	}
	color := func(c *sceneChart, i int) string {
		for _, p := range c.Paths {
			if p.Series == i {
				return p.Stroke
			}
		}
		return ""
	}
	first := color(c, 0)
	if color(c, 1) != "#123456" {
		t.Fatal("explicit colour lost")
	}
	deck.Theme = builtinDeckTheme("workshop")
	deck.Theme.Colors.Accent[0] = "#ff1100"
	second, _ := deck.projectChart(data, sceneRect{Width: 900, Height: 500}, "")
	if first == color(second, 0) || color(second, 1) != "#123456" {
		t.Fatal("theme did not honor colour provenance")
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("chart leaked extension metadata")
	}
	path := t.TempDir() + "/charts.md"
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Objects.Charts["data"], data) {
		t.Fatal("chart data did not survive Markdown")
	}
	projected, err := buildMixedSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range projected.Objects {
		if o.ID == "chart" {
			found = o.Chart != nil
		}
	}
	if !found {
		t.Fatal("semantic chart missing from production scene")
	}
}

func BenchmarkChartGeometryRetained(b *testing.B) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	data := newChartData("column")
	data.Categories = make([]string, 2000)
	data.Series = make([]ChartSeries, 8)
	for i := range data.Series {
		data.Series[i] = ChartSeries{Name: "Series", Values: make([]float64, 2000)}
		for j := range data.Series[i].Values {
			data.Series[i].Values[j] = float64(j % 30)
		}
	}
	bounds := sceneRect{Width: 1200, Height: 700}
	deck.projectChart(data, bounds, "")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bounds.X = float64(i)
		if _, err := deck.projectChart(data, bounds, ""); err != nil {
			b.Fatal(err)
		}
	}
}

func TestChartLargeGeometryRetainsWithinBoundedCache(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	data := newChartData("column")
	data.Categories = make([]string, 2000)
	data.Series = make([]ChartSeries, 8)
	for i := range data.Series {
		data.Series[i] = ChartSeries{Name: "Series", Values: make([]float64, 2000)}
		for j := range data.Series[i].Values {
			data.Series[i].Values[j] = float64(j % 30)
		}
	}
	first, err := deck.projectChart(data, sceneRect{Width: 1200, Height: 700}, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := deck.projectChart(data, sceneRect{X: 300, Y: 100, Width: 1200, Height: 700}, "")
	if err != nil || first != next {
		t.Fatal("large translation recomputed chart geometry", err)
	}
	sharedChartGeometry.Lock()
	defer sharedChartGeometry.Unlock()
	if sharedChartGeometry.bytes > 4<<20 || len(sharedChartGeometry.entries) > 32 {
		t.Fatal("cache exceeds resource budget")
	}
}
