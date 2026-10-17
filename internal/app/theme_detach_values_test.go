package app

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func concreteThemeTestText(text *sceneText) {
	if text == nil {
		return
	}
	text.FontRoles, text.ColorTokens = nil, nil
	clear := func(runs []sceneRun) {
		for i := range runs {
			runs[i].ColorToken, runs[i].FontRole = "", ""
		}
	}
	clear(text.Runs)
	for i := range text.Paragraphs {
		clear(text.Paragraphs[i].Runs)
	}
}

func TestConcreteThemeTextValuesPreserveSourceAndRoles(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("editorial")}
	styles := modernRunStyles{Version: 1, Source: modernRunFingerprint("literal ** text", ""), Runs: []sceneRun{{Text: "literal ** text", Color: "#010203", ColorToken: "accent-2", FontID: "c64", FontRole: "mono"}}}
	data, _ := json.Marshal(styles)
	q := url.Values{"render": {"truetype"}, "theme-fg": {"accent-1"}, "modern-font": {"theme"}, "modern-runs": {base64.StdEncoding.EncodeToString(data)}, "future": {"first", "second"}, "shadow": {"solid"}}
	before := cloneQueryValues(q)
	for _, kind := range []string{"text", "heading", "code", "bullet"} {
		e := Element{Kind: kind, Level: 2, Text: "literal ** text"}
		got, err := concreteThemeTextValues(deck, e, q)
		if err != nil || !reflect.DeepEqual(q, before) || got.Has("theme-fg") || got.Get("fg") != deck.Theme.Colors.Accent[0] || got.Get("modern-font") == "theme" || !reflect.DeepEqual(got["future"], q["future"]) {
			t.Fatal("text fallback changed source or lost concrete style", kind, got, err)
		}
		runs := decodeModernRuns(got.Get("modern-runs"))
		if runs == nil || runs.Source != styles.Source || runs.Runs[0].Text != styles.Runs[0].Text || runs.Runs[0].ColorToken != "" || runs.Runs[0].FontRole != "" || runs.Runs[0].Color != deck.Theme.Colors.Accent[1] || runs.Runs[0].FontID != deck.Theme.Fonts.Mono.FontID {
			t.Fatal("rich source/fingerprint or concrete roles changed", runs)
		}
		got["future"][0] = "owned"
		if !reflect.DeepEqual(q, before) {
			t.Fatal("text fallback aliases caller")
		}
	}
	// Dormant stale styles must not be revived by recomputing their source hash.
	styles.Source = modernRunFingerprint("old text", "")
	data, _ = json.Marshal(styles)
	q.Set("modern-runs", base64.StdEncoding.EncodeToString(data))
	got, err := concreteThemeTextValues(deck, Element{Kind: "text", Text: "changed"}, q)
	stale := decodeModernRuns(got.Get("modern-runs"))
	if err != nil || stale == nil || stale.Source != styles.Source {
		t.Fatal("stale styles revived", err)
	}
}

func TestConcreteThemeTableRenderParity(t *testing.T) {
	for _, theme := range []string{"clean-light", "clean-dark", "editorial", "bold", "workshop", "c64"} {
		for _, color := range []string{"", "#00aa55"} {
			t.Run(theme+color, func(t *testing.T) {
				deck := portableMergedTableFixture(t)
				deck.Theme = builtinDeckTheme(theme)
				table := cloneTable(deck.Objects.Tables["portable-data"])
				table.Extra = &jsonExtensions{"future-table": json.RawMessage(`{"keep":[1,2,3]}`)}
				table.Style.Cell = TableCellStyle{Fill: "theme:background", BorderColor: "theme:line"}
				table.Style.HeaderFill, table.Style.BandFill = "theme:accent-1", "theme:surface"
				table.Cells[1][1].Runs[0].ColorToken = "accent-2"
				table.Cells[1][1].Runs[0].Color = "#010203"
				table.Cells[1][1].Runs[0].FontRole = "heading"
				before := cloneTable(table)
				owner := deck.Slides[0].Elements[0]
				slide := Slide{}
				if color != "" {
					slide.FG, _ = ansiFG(color)
				}
				inherited := deck.tableResolvedOwnerColor(slide, owner)
				bounds := sceneRect{Width: 1200, Height: 600}
				want, err := deck.projectTable(table, bounds, inherited)
				if err != nil {
					t.Fatal(err)
				}
				concrete, err := concreteThemeTable(deck, table)
				if err != nil {
					t.Fatal(err)
				}
				plain := deck
				plain.Theme = nil
				got, err := plain.projectTable(concrete, bounds, inherited)
				if err != nil {
					t.Fatal(err)
				}
				for i := range want.Cells {
					concreteThemeTestText(want.Cells[i].Text)
					concreteThemeTestText(got.Cells[i].Text)
				}
				if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(before, table) {
					t.Fatal("table paint/geometry/text parity or source ownership changed")
				}
				if !reflect.DeepEqual(concrete.Extra, table.Extra) {
					t.Fatal("table conversion dropped future metadata")
				}
				encoded, _ := json.Marshal(concrete)
				if strings.Contains(string(encoded), `"colorToken"`) || strings.Contains(string(encoded), `"fontRole"`) || strings.Contains(string(encoded), "theme:") {
					t.Fatal("table retained live roles")
				}
			})
		}
	}
}

func TestConcreteThemeChartRenderParity(t *testing.T) {
	for _, theme := range []string{"clean-light", "clean-dark", "editorial", "bold", "workshop", "c64"} {
		for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut", "column-stacked", "bar-stacked"} {
			t.Run(theme+"/"+kind, func(t *testing.T) {
				deck := Deck{Theme: builtinDeckTheme(theme), Appearance: &DeckAppearance{Version: 3}}
				data := newChartData(strings.TrimSuffix(kind, "-stacked"))
				data.Options.Stacked = strings.HasSuffix(kind, "-stacked")
				data.Extra = &jsonExtensions{"future-chart": json.RawMessage(`{"keep":true}`)}
				data.Options.DataLabels = true
				data.Series[0].Color, data.Series[0].ColorToken = "#010203", "accent-3"
				data.Options.GridColor = "theme:line"
				if kind == "pie" || kind == "doughnut" {
					data.CategoryColors = []ChartColorStyle{{Color: "#010203", ColorToken: "accent-2"}}
				}
				before := cloneChart(data)
				owner := Element{ID: "chart", Kind: "chart", Query: "theme-fg=heading"}
				bounds := sceneRect{Width: 1000, Height: 600}
				want, err := deck.projectChart(data, bounds, deck.tableResolvedOwnerColor(Slide{}, owner))
				if err != nil {
					t.Fatal(err)
				}
				concrete, updated, err := concreteThemeChart(deck, owner, data)
				if err != nil {
					t.Fatal(err)
				}
				plain := deck
				plain.Theme = nil
				got, err := plain.projectChart(concrete, bounds, plain.tableResolvedOwnerColor(Slide{}, updated))
				if err != nil {
					t.Fatal(err)
				}
				// Data intentionally changes from linked styles to concrete styles;
				// geometry, labels, contrast and palette must remain equivalent.
				w, g := *want, *got
				w.Data, g.Data = ChartData{}, ChartData{}
				if !reflect.DeepEqual(w, g) || !reflect.DeepEqual(data, before) || updated.ID != owner.ID || updated.Kind != owner.Kind {
					t.Fatal("chart render parity or source/owner identity changed")
				}
				if !reflect.DeepEqual(concrete.Extra, data.Extra) {
					t.Fatal("chart conversion dropped future metadata")
				}
			})
		}
	}
}
