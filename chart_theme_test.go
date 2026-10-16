package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func chartThemeFixture(kind string) Deck {
	data := newChartData(kind)
	data.Series[0].Color = "#112233"
	data.Series[0].ColorToken = "accent-2"
	if kind == "pie" || kind == "doughnut" {
		data.CategoryColors = []ChartColorStyle{{Color: "#112233", ColorToken: "accent-2"}, {Color: "#ff55aa"}}
	} else {
		data.Series = append(data.Series, ChartSeries{Name: "Fixed", Values: []float64{3, 2, 5, 4}, Color: "#ff55aa"})
	}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "chart-theme-slide", Elements: []Element{{ID: "chart-theme", Kind: "chart", ObjectID: "chart-theme-data", Query: "left=20&top=8&width=200&height=40&fg=%23263445"}}}}, Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"chart-theme-data": data}}}
}

func TestStackedChartThemeConsumerDependencies(t *testing.T) {
	for _, kind := range []string{"column", "bar"} {
		for _, labels := range []bool{false, true} {
			for _, axes := range []bool{false, true} {
				for _, edit := range []struct {
					name   string
					reuse  bool
					mutate func(*DeckTheme)
				}{
					{"unused-accent", true, func(t *DeckTheme) { t.Colors.Accent[5] = "#abcdef" }},
					{"used-accent", false, func(t *DeckTheme) { t.Colors.Accent[1] = "#abcdef" }},
					{"grid", !axes, func(t *DeckTheme) { t.Colors.Line = "#abcdef" }},
					{"body-width", false, func(t *DeckTheme) { t.Fonts.Body.Width += 10 }},
					{"unused-mono", true, func(t *DeckTheme) { t.Fonts.Mono.Width += 10 }},
				} {
					t.Run(fmt.Sprintf("%s/labels=%t/axes=%t/%s", kind, labels, axes, edit.name), func(t *testing.T) {
						before := chartThemeFixture(kind)
						data := before.Objects.Charts["chart-theme-data"]
						data.Options.Stacked, data.Options.DataLabels, data.Options.HideAxes = true, labels, !axes
						before.Objects.Charts["chart-theme-data"] = data
						after := cloneDeck(before)
						edit.mutate(after.Theme)
						if reuse := sceneProjectionInputsEqual(before, after, sceneResponseKey{index: 0}); reuse != edit.reuse {
							t.Fatalf("reuse=%t, want %t", reuse, edit.reuse)
						}
						old, err := buildSlideScene(before, 0, 245, 56)
						if err != nil {
							t.Fatal(err)
						}
						next, err := buildSlideScene(after, 0, 245, 56)
						if err != nil {
							t.Fatal(err)
						}
						if equal := reflect.DeepEqual(old.Objects[0].Chart, next.Objects[0].Chart); equal != edit.reuse {
							t.Fatalf("cold chart equality=%t, reuse=%t", equal, edit.reuse)
						}
						session := newNativeEditorSession("Stacked.md", before)
						get := func(cold bool) []byte {
							uri := "/api/editor/scene?slide=0"
							if cold {
								uri += "&timing=1"
							}
							response := httptest.NewRecorder()
							session.handleScene(response, httptest.NewRequest("GET", uri, nil))
							if response.Code != 200 {
								t.Fatal(response.Code, response.Body.String())
							}
							return response.Body.Bytes()
						}
						get(false)
						cached := session.sceneResponse
						if err := session.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: after.Theme}); err != nil {
							t.Fatal(err)
						}
						retained := session.sceneResponse != nil && session.sceneResponse.key.revision == session.version && &session.sceneResponse.data[0] == &cached.data[0]
						if retained != edit.reuse {
							t.Fatalf("response retained=%t, want %t", retained, edit.reuse)
						}
						for _, action := range []string{"", "undo", "redo"} {
							if action != "" {
								if err := session.apply(nativeEditorAction{Action: action}); err != nil {
									t.Fatal(err)
								}
							}
							if !bytes.Equal(get(false), get(true)) {
								t.Fatalf("%s cached response differs from cold render", action)
							}
						}
					})
				}
			}
		}
	}
}

func TestChartThemeColoursProjectPortableAndScoped(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			d := chartThemeFixture(kind)
			before := cloneDeck(d)
			scene, err := buildSlideScene(d, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			chart := scene.Objects[0].Chart
			colours := chart.SeriesColors
			if kind == "pie" || kind == "doughnut" {
				colours = chart.CategoryColors
			}
			if colours[0] != d.Theme.Colors.Accent[1] || colours[1] != "#ff55aa" {
				t.Fatal("chart role/fixed colours not resolved", colours)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("projection baked chart colours")
			}
			md, err := serializeDeck("Chart.md", d)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("Chart.md", md)
			if err != nil || !reflect.DeepEqual(loaded.Objects, d.Objects) {
				t.Fatal("chart roles lost on reopen", err)
			}
		})
	}
}

func TestChartColourValidationOwnershipAndExtensions(t *testing.T) {
	base := chartThemeFixture("pie").Objects.Charts["chart-theme-data"]
	for _, invalid := range []func(*ChartData){
		func(c *ChartData) { c.Series[0].ColorToken = "future" },
		func(c *ChartData) { c.Series[0].Color = "none" },
		func(c *ChartData) { c.CategoryColors[0].ColorToken = "accent-7" },
		func(c *ChartData) { c.CategoryColors[0].Color = "#gggggg" },
		func(c *ChartData) { c.CategoryColors = make([]ChartColorStyle, 5) },
	} {
		bad := cloneChart(base)
		invalid(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid chart colour accepted", bad)
		}
	}
	extra := &jsonExtensions{"future": json.RawMessage(`{"value":1}`)}
	base.CategoryColors[0].Extra = extra
	raw, err := json.Marshal(base)
	var loaded ChartData
	if err != nil || json.Unmarshal(raw, &loaded) != nil || !reflect.DeepEqual(base, loaded) {
		t.Fatal("chart style extensions lost", err)
	}
	owned := cloneChart(base)
	(*owned.CategoryColors[0].Extra)["future"][0] = '['
	owned.CategoryColors[0].Color = "#ffffff"
	if base.CategoryColors[0].Color != "#112233" || string((*extra)["future"]) != `{"value":1}` {
		t.Fatal("chart style clone aliases source")
	}
}

func TestChartColourRoleCommandsAtomicHistoryAndFork(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := chartThemeFixture("column")
		d.Slides = append(d.Slides, cloneSlide(d.Slides[0]))
		d.Slides[1].ID = "other-slide"
		d.EnsureDefaultMasters()
		if master {
			d.Masters.Base.Slide = cloneSlide(d.Slides[0])
		}
		s := newNativeEditorSession("chart.md", d)
		s.masterMode, s.currentMaster = master, 0
		before := cloneDeck(s.deck)
		index, token, rev := 0, "accent-3", s.version
		a := nativeEditorAction{Action: "set-chart-series-style", Slide: 0, SceneMaster: master, SceneRevision: &rev, ObjectID: "chart-theme", ChartSeriesIndex: &index, ChartSeriesColorToken: &token}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		target := s.deck.Slides[0]
		if master {
			target = *masterSlideAt(&s.deck, 0)
		}
		id := target.Elements[0].ObjectID
		if id == "chart-theme-data" || s.deck.Objects.Charts[id].Series[0].ColorToken != token || s.deck.Objects.Charts[id].Series[0].Color != d.Theme.Colors.Accent[2] || !reflect.DeepEqual(before.Objects.Charts["chart-theme-data"], s.deck.Objects.Charts["chart-theme-data"]) || len(s.undo) != 1 {
			t.Fatal("role command failed fork/history/fallback")
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("role undo", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("role redo", err)
		}
		rev = s.version
		token = "invalid"
		if err := s.apply(a); err == nil || !reflect.DeepEqual(after, s.deck) || len(s.undo) != 1 {
			t.Fatal("invalid role command mutated state", err)
		}
		token = "accent-3"
		rgb := "#abcdef"
		a.ChartSeriesColor = &rgb
		if err := s.apply(a); err == nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("ambiguous RGB/role command accepted")
		}
		a.ChartSeriesColorToken = nil
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		if s.deck.Objects.Charts[id].Series[0].ColorToken != "" || s.deck.Objects.Charts[id].Series[0].Color != rgb {
			t.Fatal("custom RGB did not unlink role")
		}
		rev = s.version
		a.ChartSeriesColor = nil
		token = ""
		a.ChartSeriesColorToken = &token
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		if s.deck.Objects.Charts[id].Series[0].ColorToken != "" || s.deck.Objects.Charts[id].Series[0].Color != "" {
			t.Fatal("inherit did not clear role/RGB")
		}
	}
}

func TestChartColourRoleClipboardAndConsumerDependencies(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		d := chartThemeFixture(kind)
		bundle, err := makeObjectClipboard(d, d.Slides[0].Elements)
		if err != nil {
			t.Fatal(err)
		}
		captured := bundle.Objects.Charts["chart-theme-data"]
		if captured.Series[0].Color != d.Theme.Colors.Accent[1] {
			t.Fatal("clipboard did not capture current role fallback")
		}
		for _, portable := range []bool{false, true} {
			dest := Deck{Slides: []Slide{{}}}
			if portable {
				dest.Theme = builtinDeckTheme("clean-light")
				dest.Theme.Colors.Accent[1] = "#aa0055"
				dest.Appearance = &DeckAppearance{Version: 3}
			}
			elements, _, objects, err := prepareObjectClipboardPaste(dest, bundle)
			if err != nil {
				t.Fatal(err)
			}
			pasted := objects.Charts[elements[0].ObjectID]
			if portable && pasted.Series[0].ColorToken != "accent-2" || !portable && (pasted.Series[0].ColorToken != "" || pasted.Series[0].Color != captured.Series[0].Color) {
				t.Fatal("clipboard role adaptation", portable, pasted)
			}
			if len(pasted.CategoryColors) > 0 && (!portable && pasted.CategoryColors[0].ColorToken != "" || pasted.CategoryColors[1].Color != "#ff55aa") {
				t.Fatal("slice colour clipboard adaptation")
			}
		}
		for _, accent := range []int{1, 5} {
			after := cloneDeck(d)
			after.Theme.Colors.Accent[accent] = "#abcdef"
			if equal := sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0}); equal != (accent == 5) {
				t.Fatal("chart role consumer invalidation", kind, accent, equal)
			}
			if accent == 1 {
				scene, _ := buildSlideScene(after, 0, 245, 56)
				colors := scene.Objects[0].Chart.SeriesColors
				if kind == "pie" || kind == "doughnut" {
					colors = scene.Objects[0].Chart.CategoryColors
				}
				if colors[0] != "#abcdef" || colors[1] != "#ff55aa" {
					t.Fatal("role change lost fixed colour", colors)
				}
			}
		}
	}
}

func TestChartColourPPTXRGBAndPublicPrivacy(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		d := chartThemeFixture(kind)
		before := cloneDeck(d)
		data, report, err := exportPPTXDocument(d)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warning := range report.Warnings {
			if warning.Code == "chart-theme-colours-frozen" {
				found = true
			}
		}
		if !found {
			t.Fatal("export omitted role freeze diagnostic")
		}
		loaded, _, err := importPPTXDocument(data, "chart.pptx")
		if err != nil {
			t.Fatal(err)
		}
		for _, chart := range loaded.Objects.Charts {
			if chartHasColorTokens(chart) {
				t.Fatal("PPTX import claimed authored role")
			}
			if kind == "pie" || kind == "doughnut" {
				if len(chart.CategoryColors) != 4 || !strings.EqualFold(chart.CategoryColors[0].Color, d.Theme.Colors.Accent[1]) || !strings.EqualFold(chart.CategoryColors[1].Color, "#ff55aa") {
					t.Fatal("native PPTX lost slice RGB", chart)
				}
			} else if !strings.EqualFold(chart.Series[0].Color, d.Theme.Colors.Accent[1]) || !strings.EqualFold(chart.Series[1].Color, "#ff55aa") {
				t.Fatal("native PPTX lost series RGB", chart)
			}
		}
		if !reflect.DeepEqual(before, d) {
			t.Fatal("PPTX export mutated roles")
		}
		chart := d.Objects.Charts["chart-theme-data"]
		chart.CategoryColors = []ChartColorStyle{{Color: "#112233", ColorToken: "accent-2", Extra: &jsonExtensions{"private": json.RawMessage(`"PRIVATE SLICE"`)}}}
		d.Objects.Charts["chart-theme-data"] = chart
		scene, err := buildSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		md, err := participantSlideMarkdown(d, 0)
		if err != nil {
			t.Fatal(err)
		}
		public, err := parseDeckData("public.md", md)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []any{scene, public} {
			raw, _ := json.Marshal(value)
			if bytes.Contains(raw, []byte("PRIVATE SLICE")) {
				t.Fatal("slice extension leaked")
			}
		}
		if root := os.Getenv("KEYNOPE_CHART_TOKEN_EVIDENCE"); root != "" {
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, kind+".pptx"), data, 0600); err != nil {
				t.Fatal(err)
			}
			md, err := serializeDeck("chart.md", before)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, kind+".md"), md, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
