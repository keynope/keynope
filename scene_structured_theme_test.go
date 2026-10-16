package main

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func structuredThemeFixture(kind string, explicit bool) Deck {
	ptrFloat := func(v float64) *float64 { return &v }
	d := themeDependencyFixture(kind, false)
	e := &d.Slides[0].Elements[0]
	e.ObjectID = "data"
	e.Query = "top=4&left=10&width=180&height=40"
	d.Objects = &DeckObjects{Version: 1}
	if kind == "table" {
		data, _ := newTableData(3, 2)
		data.Style.Header, data.Style.Banded = true, true
		data.Cells[0][0].Runs = []ObjectTextRun{{Text: "Inline", Color: "#aabbcc", FontID: "mono", FontSize: 60}}
		data.Cells[1][0].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"left": {Color: "theme:accent-2", Width: ptrFloat(2)}}}
		if explicit {
			data.Style.HeaderFill, data.Style.BandFill = "#ddbb88", "#eeeedd"
			data.Style.Cell = TableCellStyle{FontID: "mono", FontSize: ptrFloat(64), Color: "#112233", Fill: "#eeeedd", BorderColor: "#334455"}
			data.Cells[1][0].Style = nil
		}
		d.Objects.Tables = map[string]TableData{"data": data}
	} else {
		data := newChartData(kind)
		if kind != "pie" && kind != "doughnut" {
			data.Series = append(data.Series, ChartSeries{Name: "Second series", Values: []float64{6, 3, 8, 5}})
		}
		e.Kind = "chart"
		data.Options.DataLabels = true
		if explicit {
			for i := range data.Series {
				data.Series[i].Color = "#445566"
			}
			e.Query += "&modern-font=mono&fg=%23112233"
		}
		d.Objects.Charts = map[string]ChartData{"data": data}
	}
	d.EnsureDefaultMasters()
	return d
}

func structuredColdScene(t *testing.T, d Deck, master bool) []byte {
	t.Helper()
	// Bypass both caches: parity must not be accidentally proved against the
	// same retained chart geometry that this comparison is deciding to reuse.
	sharedChartGeometry.Lock()
	sharedChartGeometry.entries, sharedChartGeometry.bytes = nil, 0
	sharedChartGeometry.Unlock()
	s := newNativeEditorSession("Structured.md", d)
	s.masterMode = master
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func TestStructuredThemeDependencyColdParity(t *testing.T) {
	edits := []struct {
		name string
		edit func(*DeckTheme)
	}{
		{"name", func(t *DeckTheme) { t.Name = "Renamed" }},
		{"surface", func(t *DeckTheme) { t.Colors.Surface = "#55ffaa" }},
		{"text", func(t *DeckTheme) { t.Colors.Text = "#ff00aa" }},
		{"background", func(t *DeckTheme) { t.Colors.Background = "#221144" }},
		{"heading", func(t *DeckTheme) { t.Colors.Heading = "#55aaff" }},
		{"muted", func(t *DeckTheme) { t.Colors.Muted = "#ffaa55" }},
		{"line", func(t *DeckTheme) { t.Colors.Line = "#aa55ff" }},
		{"body-family", func(t *DeckTheme) { t.Fonts.Body.FontID = "c64" }},
		{"body-size", func(t *DeckTheme) { t.Fonts.Body.Size += 20 }},
		{"body-width", func(t *DeckTheme) { t.Fonts.Body.Width = 70 }},
		{"body-height", func(t *DeckTheme) { t.Fonts.Body.LineHeight = 2 }},
		{"mono", func(t *DeckTheme) { t.Fonts.Mono.FontID = "c64"; t.Fonts.Mono.Size += 10 }},
		{"heading-font", func(t *DeckTheme) { t.Fonts.Heading.Width = 120 }},
		{"shape-default", func(t *DeckTheme) { t.Defaults.LineWidth = 9; t.Defaults.ShapeFill = "#aa55ff" }},
	}
	for i := 0; i < 6; i++ {
		i := i
		edits = append(edits, struct {
			name string
			edit func(*DeckTheme)
		}{fmt.Sprint("accent-", i), func(t *DeckTheme) { t.Colors.Accent[i] = "#ff00aa" }})
	}
	retained, invalidated := 0, 0
	for _, kind := range []string{"table", "column", "bar", "line", "area", "pie", "doughnut"} {
		for _, explicit := range []bool{false, true} {
			for _, master := range []bool{false, true} {
				d := structuredThemeFixture(kind, explicit)
				if master {
					d.Masters.Base.Slide = cloneSlide(d.Slides[0])
					d.Slides[0].Elements = nil
				}
				baseline := cloneDeck(d)
				old := structuredColdScene(t, d, master)
				for _, edit := range edits {
					after := cloneDeck(d)
					edit.edit(after.Theme)
					unchanged := cloneDeck(after)
					equal := sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0, cols: 245, rows: 56, master: master})
					if equal {
						retained++
						if !bytes.Equal(old, structuredColdScene(t, after, master)) {
							t.Fatalf("stale scene %s explicit=%v master=%v change=%s", kind, explicit, master, edit.name)
						}
					} else {
						invalidated++
					}
					if !reflect.DeepEqual(d, baseline) || !reflect.DeepEqual(after, unchanged) {
						t.Fatal("theme consumers mutated authored inputs")
					}
				}
			}
		}
	}
	if retained < 100 || invalidated < 100 {
		t.Fatal("insufficient retained/invalidated cases", retained, invalidated)
	}
	t.Log("retained", retained, "invalidated", invalidated)
}

func TestStructuredThemeWorkspaceDeltas(t *testing.T) {
	d := structuredThemeFixture("table", false)
	other := structuredThemeFixture("column", false)
	other.Slides[0].ID = "chart-slide"
	other.Slides[0].Elements[0].ID = "chart"
	other.Slides[0].Elements[0].ObjectID = "chart-data"
	d.Objects.Charts = map[string]ChartData{"chart-data": other.Objects.Charts["data"]}
	d.Slides = append(d.Slides, other.Slides[0])
	var cache workspacePageCache
	if _, err := cache.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []struct {
		name   string
		change func(*DeckTheme)
		want   []int
	}{
		{"unused-accent", func(t *DeckTheme) { t.Colors.Accent[5] = "#ff00aa" }, nil},
		{"unused-mono", func(t *DeckTheme) { t.Fonts.Mono.Size += 10 }, nil},
		{"table-band", func(t *DeckTheme) { t.Colors.Surface = "#55ffaa" }, []int{0}},
		{"chart-series", func(t *DeckTheme) { t.Colors.Accent[1] = "#ffaa55" }, []int{0, 1}},
		{"table-size", func(t *DeckTheme) { t.Fonts.Body.Size += 10 }, []int{0}},
		{"both-width", func(t *DeckTheme) { t.Fonts.Body.Width = 80 }, []int{0, 1}},
	} {
		edit.change(d.Theme)
		pages, err := cache.render(d, false, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := editorWorkspacePages(d, false, 0, 245, 56)
		if err != nil || !reflect.DeepEqual(pages, fresh) || !reflect.DeepEqual(cache.changed, edit.want) {
			t.Fatalf("%s delta=%v want=%v parity=%v err=%v", edit.name, cache.changed, edit.want, reflect.DeepEqual(pages, fresh), err)
		}
	}
}

func TestChartGeometryResolvedThemeCache(t *testing.T) {
	d := structuredThemeFixture("column", false)
	data := d.Objects.Charts["data"]
	bounds := sceneRect{Width: 900, Height: 500}
	old, err := d.projectChart(data, bounds, "")
	if err != nil {
		t.Fatal(err)
	}
	after := cloneDeck(d)
	after.Theme.Name = "Renamed"
	after.Theme.Colors.Accent[5] = "#ff00aa"
	after.Theme.Fonts.Body.Size += 10     // Chart size is geometry-owned.
	after.Theme.Fonts.Body.LineHeight = 2 // Compact labels own line-height.
	next, err := after.projectChart(data, bounds, "")
	if err != nil || old != next {
		t.Fatal("unused theme inputs rebuilt chart geometry", err)
	}
	after.Theme.Colors.Accent[0] = "#ff00aa"
	changed, err := after.projectChart(data, bounds, "")
	if err != nil || changed == old || changed.SeriesColors[0] != "#ff00aa" {
		t.Fatal("used chart palette stale", err)
	}
}

func TestStructuredThemeTransactionsRetainImmutableResponse(t *testing.T) {
	for _, kind := range []string{"table", "column"} {
		for _, master := range []bool{false, true} {
			d := structuredThemeFixture(kind, false)
			if master {
				d.Masters.Base.Slide = cloneSlide(d.Slides[0])
				d.Slides[0].Elements = nil
			}
			s := newNativeEditorSession("Retained.md", d)
			s.masterMode = master
			get := func(cold bool) []byte {
				url := "/api/editor/scene?slide=0"
				if cold {
					url += "&timing=1"
				}
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", url, nil))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				return append([]byte(nil), w.Body.Bytes()...)
			}
			original := get(false)
			entry, initial := s.sceneResponse, cloneDeck(s.deck)
			theme := s.deck.Theme.Clone()
			theme.Colors.Accent[5] = "#ff00aa"
			if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
				t.Fatal(err)
			}
			if s.sceneResponse == nil || &entry.data[0] != &s.sceneResponse.data[0] || entry.key.revision == s.version {
				t.Fatal("immutable response not retained", kind, master)
			}
			if !bytes.Equal(get(false), get(true)) {
				t.Fatal("retained revision response differs", kind, master)
			}
			var old bytes.Buffer
			entry.write(&old)
			if !bytes.Equal(original, old.Bytes()) {
				t.Fatal("old reader response mutated")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck, initial) || !bytes.Equal(get(false), get(true)) {
				t.Fatal("history lost theme/payload or reused stale data")
			}
		}
	}
}

func BenchmarkStructuredThemeUnusedRoleComparison(b *testing.B) {
	for _, kind := range []string{"table", "column"} {
		b.Run(kind, func(b *testing.B) {
			d := structuredThemeFixture(kind, false)
			if kind == "table" {
				data, _ := newTableData(20, 20)
				data.Style.Header, data.Style.Banded = true, true
				d.Objects.Tables["data"] = data
			}
			after := cloneDeck(d)
			after.Theme.Colors.Accent[5] = "#ff00aa"
			key := sceneResponseKey{index: 0, cols: 245, rows: 56}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !sceneProjectionInputsEqual(d, after, key) {
					b.Fatal("unused role invalidated")
				}
			}
		})
	}
}

func TestStructuredThemeSparseCellAndChartVisibilityDependencies(t *testing.T) {
	d := structuredThemeFixture("table", false)
	data, _ := newTableData(20, 20)
	data.Style.Header, data.Style.Banded = true, true
	for i := 0; i < 6; i++ {
		data.Cells[10+i][19].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"bottom": {Color: fmt.Sprintf("theme:accent-%d", i+1)}}}
	}
	d.Objects.Tables["data"] = data
	for i := 0; i < 6; i++ {
		after := cloneDeck(d)
		after.Theme.Colors.Accent[i] = "#ff00aa"
		if sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0}) {
			t.Fatal("sparse far-cell role ignored", i)
		}
	}
	for _, kind := range []string{"column", "pie", "doughnut"} {
		for _, small := range []bool{false, true} {
			d := structuredThemeFixture(kind, false)
			data := d.Objects.Charts["data"]
			data.Options.HideAxes = true
			d.Objects.Charts["data"] = data
			if small {
				d.Slides[0].Elements[0].Query = "width=10&height=2"
			}
			after := cloneDeck(d)
			after.Theme.Colors.Line = "#ff00aa"
			if !sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0}) || !bytes.Equal(structuredColdScene(t, d, false), structuredColdScene(t, after, false)) {
				t.Fatal("nonexistent chart grid invalidated/stale", kind, small)
			}
		}
	}
}

func TestStructuredThemeBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_STRUCTURED_THEME_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	d := structuredThemeFixture("table", false)
	chart := structuredThemeFixture("column", false)
	d.Objects.Charts = map[string]ChartData{"chart-data": chart.Objects.Charts["data"]}
	e := chart.Slides[0].Elements[0]
	e.ObjectID = "chart-data"
	e.ID, e.Query = "chart", "top=6&left=130&width=100&height=35"
	d.Slides[0].Elements[0].ID, d.Slides[0].Elements[0].Query = "table", "top=6&left=6&width=110&height=35"
	d.Slides[0].Elements = append(d.Slides[0].Elements, e)
	d.Slides = append(d.Slides, Slide{ID: "unseen", Elements: []Element{{ID: "role", Kind: "text", Text: "Unseen accent consumer", Query: "render=truetype&theme-fg=accent-6"}}})
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "structured.md"), d); err != nil {
		t.Fatal(err)
	}
}
