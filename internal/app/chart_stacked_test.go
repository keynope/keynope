package app

import (
	"bytes"
	"context"
	"fmt"
	"keynope/internal/pptx"
	"math"
	"strings"
	"testing"
)

func TestStackedChartGeometryAndPPTX(t *testing.T) {
	for _, kind := range []string{"column", "bar"} {
		t.Run(kind, func(t *testing.T) {
			low, high := -5.0, 5.0
			data := ChartData{Version: 1, Type: kind, Categories: []string{"One"}, Options: ChartOptions{Stacked: true, HideAxes: true, Minimum: &low, Maximum: &high}}
			for i, v := range []float64{3, 2, -4, -1} {
				data.Series = append(data.Series, ChartSeries{Name: fmt.Sprint(i), Values: []float64{v}})
			}
			chart, err := (Deck{}).projectChart(data, sceneRect{Width: 800, Height: 500}, "")
			if err != nil {
				t.Fatal(err)
			}
			type rect struct{ x, y, w, h float64 }
			rects := []rect{}
			for _, p := range chart.Paths {
				if p.Series < 0 {
					continue
				}
				var r rect
				var back float64
				if n, err := fmt.Sscanf(p.D, "M%g %gh%gv%gh%gZ", &r.x, &r.y, &r.w, &r.h, &back); err != nil || n != 5 {
					t.Fatalf("bad bar %q: %v", p.D, err)
				}
				rects = append(rects, r)
			}
			if len(rects) != 4 {
				t.Fatal(rects)
			}
			near := func(a, b float64) {
				t.Helper()
				if math.Abs(a-b) > 1e-7 {
					t.Fatalf("%g != %g", a, b)
				}
			}
			if kind == "column" {
				near(rects[0].y, rects[1].y+rects[1].h)
				near(rects[2].y+rects[2].h, rects[3].y)
				near(rects[0].y+rects[0].h, rects[2].y)
				for _, r := range rects {
					near(r.x, rects[0].x)
					near(r.w, rects[0].w)
				}
			} else {
				near(rects[0].x+rects[0].w, rects[1].x)
				near(rects[3].x+rects[3].w, rects[2].x)
				near(rects[2].x+rects[2].w, rects[0].x)
				for _, r := range rects {
					near(r.y, rects[0].y)
					near(r.h, rects[0].h)
				}
			}
			var out bytes.Buffer
			_, err = pptx.Write(context.Background(), &out, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{{ID: "chart", Kind: "chart", Width: 800, Height: 500, Chart: sceneChartToPPTX(chart)}}}}})
			if err != nil {
				t.Fatal(err)
			}
			imported, report, err := pptx.Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), pptx.Options{})
			if err != nil || len(report.Warnings) > 0 {
				t.Fatal(err, report)
			}
			restored, _, err := pptxChartToData(imported.Slides[0].Objects[0].Chart)
			if err != nil || !restored.Options.Stacked {
				t.Fatal(err, restored)
			}
			for i, s := range restored.Series {
				near(s.Values[0], data.Series[i].Values[0])
			}
			data.Options.Minimum = nil
			data.Options.Maximum = nil
			for i := range data.Series {
				data.Series[i].Values[0] = math.MaxFloat64
			}
			huge, err := (Deck{}).projectChart(data, sceneRect{Width: 800, Height: 500}, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range huge.Paths {
				if strings.Contains(p.D, "Inf") || strings.Contains(p.D, "NaN") {
					t.Fatal(p.D)
				}
			}
		})
	}
}

func TestStackedChartLabelsFitSegmentsAndContrast(t *testing.T) {
	for _, kind := range []string{"column", "bar"} {
		data := ChartData{Version: 1, Type: kind, Categories: []string{"One"}, Options: ChartOptions{Stacked: true, HideAxes: true, DataLabels: true}, Series: []ChartSeries{
			{Name: "Large", Color: "#ffff00", Values: []float64{90}},
			{Name: "Tiny", Color: "#ffffff", Values: []float64{0.01}},
			{Name: "Zero", Values: []float64{0}},
		}}
		chart, err := (Deck{}).projectChart(data, sceneRect{Width: 800, Height: 500}, "#ffffff")
		if err != nil {
			t.Fatal(err)
		}
		if len(chart.Labels) != 1 || chart.Labels[0].FullText != "90" {
			t.Fatalf("%s: labels don't fit visible segments: %+v", kind, chart.Labels)
		}
		if tableColorContrast(chart.Labels[0].Paint.Color, "#ffff00") < 4.5 {
			t.Fatalf("%s: unreadable label colour %s", kind, chart.Labels[0].Paint.Color)
		}
	}
}

func TestUnsupportedChartStackingRejectsAtomicEdits(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, kind := range []string{"line", "area", "pie", "doughnut"} {
			s := capabilityFixture()
			s.masterMode, s.currentMaster = master, 0
			data := newChartData(kind)
			data.Options.Stacked = true
			revision := s.version
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "insert-chart", SceneMaster: master, SceneRevision: &revision, ChartData: &data})
			valid := newChartData(kind)
			if err := s.apply(nativeEditorAction{Action: "insert-chart", SceneMaster: master, SceneRevision: &revision, ChartData: &valid}); err != nil {
				t.Fatal(err)
			}
			target := s.state().SelectedChart.ObjectID
			revision = s.version
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-chart-data", SceneMaster: master, SceneRevision: &revision, ObjectID: target, ChartData: &data})
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-chart-options", SceneMaster: master, SceneRevision: &revision, ObjectID: target, ChartOptions: &data.Options})
		}
	}
}
