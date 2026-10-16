package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func clipboardChartThemeFixture(kind string, stacked bool) Deck {
	deck := chartThemeFixture(kind)
	deck.Slides[0].Elements[0].Query = "left=20&top=8&width=200&height=40"
	deck.Theme = builtinDeckTheme("clean-dark")
	deck.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 64, Width: 72, LineHeight: 1.6}
	deck.Theme.Colors.Text = "#d0f0ff"
	deck.Theme.Colors.Accent[0], deck.Theme.Colors.Accent[1] = "#075530", "#403090"
	data := deck.Objects.Charts["chart-theme-data"]
	data.Series[0].Color, data.Series[0].ColorToken = "", ""
	data.Series[0].LabelColor = "theme:accent-6"
	if kind == "pie" || kind == "doughnut" {
		data.CategoryColors[0].LabelColor = "theme:accent-6"
	}
	data.Options.DataLabels, data.Options.Stacked = true, stacked
	data.Options.GridColor = "theme:accent-4"
	data.Series[0].Extra = extensionFixture("future-series", `{"keep":true}`)
	deck.Objects.Charts["chart-theme-data"] = data
	return deck
}

func chartRenderedEqual(a, b *sceneChart) bool {
	x, y := *a, *b
	x.Data, y.Data = ChartData{}, ChartData{}
	return reflect.DeepEqual(x, y)
}

func TestClipboardChartThemeAppearanceAcrossTypes(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut", "column-stacked", "bar-stacked"} {
		t.Run(kind, func(t *testing.T) {
			baseKind := strings.TrimSuffix(kind, "-stacked")
			source := clipboardChartThemeFixture(baseKind, kind != baseKind)
			before := cloneDeck(source)
			want, err := buildSlideScene(source, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			var wire ObjectClipboard
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			fallback, _ := json.Marshal(wire.ChartThemeFallbacks)
			if strings.Contains(string(fallback), "categories\":[\"") || strings.Contains(string(fallback), "Series 1") || strings.Contains(string(fallback), "future-series") {
				t.Fatal("formatting fallback duplicated content/metadata")
			}
			for _, themed := range []bool{false, true} {
				destination := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}}
				if themed {
					destination.Theme = builtinDeckTheme("clean-light")
					destination.Theme.Colors.Accent[0] = "#ff8822"
					destination.Theme.Colors.Accent[1] = "#2255dd"
				}
				target := cloneDeck(destination)
				elements, assets, objects, err := prepareObjectClipboardPaste(destination, wire)
				if err != nil {
					t.Fatal(err)
				}
				destination.Slides[0].Elements, destination.Assets, destination.Objects = elements, assets, objects
				actual, err := buildSlideScene(destination, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				if themed {
					if !reflect.DeepEqual(objects.Charts[elements[0].ObjectID], wire.Objects.Charts["chart-theme-data"]) {
						t.Fatal("themed paste baked source defaults")
					}
					if actual.Objects[0].Chart.Labels[0].Text.FontID != "sans" {
						t.Fatal("target font not adopted")
					}
				} else if !chartRenderedEqual(want.Objects[0].Chart, actual.Objects[0].Chart) {
					a, _ := json.Marshal(want.Objects[0].Chart)
					b, _ := json.Marshal(actual.Objects[0].Chart)
					t.Fatalf("plain chart appearance mismatch\nsource=%s\nactual=%s", a, b)
				}
				md, err := serializeDeck("pasted.md", destination)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("pasted.md", md)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(reopened.Objects.Charts[elements[0].ObjectID], objects.Charts[elements[0].ObjectID]) {
					t.Fatal("chart styles lost on save/reopen")
				}
				public, err := participantSlideMarkdown(destination, 0)
				if err != nil {
					t.Fatal(err)
				}
				audience, err := parseDeckData("public.md", public)
				if err != nil {
					t.Fatal(err)
				}
				publicScene, err := buildSlideScene(audience, 0, 245, 56)
				if err != nil || !chartRenderedEqual(actual.Objects[0].Chart, publicScene.Objects[0].Chart) {
					t.Fatal("public chart style parity", err)
				}
				if output := os.Getenv("KEYNOPE_CLIPBOARD_CHART_EVIDENCE"); output != "" {
					if err := os.MkdirAll(output, 0755); err != nil {
						t.Fatal(err)
					}
					name := "plain"
					if themed {
						name = "themed"
					}
					for filename, deck := range map[string]Deck{kind + "-source.md": source, kind + "-" + name + ".md": target} {
						if err := saveDeck(filepath.Join(output, filename), deck); err != nil {
							t.Fatal(err)
						}
					}
					ppt, _, err := exportPPTXDocument(destination)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(output, kind+"-"+name+".pptx"), ppt, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !reflect.DeepEqual(source, before) {
				t.Fatal("clipboard changed source")
			}
		})
	}
}

func TestClipboardChartThemeValidationAtomic(t *testing.T) {
	source := clipboardChartThemeFixture("pie", false)
	for i, mutate := range []func(*ObjectClipboard){
		func(b *ObjectClipboard) { b.ThemeSignature = "" },
		func(b *ObjectClipboard) { b.ChartThemeFallbacks["unknown"] = b.ChartThemeFallbacks["chart-theme"] },
		func(b *ObjectClipboard) {
			f := b.ChartThemeFallbacks["chart-theme"]
			f.FontWidth = 201
			b.ChartThemeFallbacks["chart-theme"] = f
		},
		func(b *ObjectClipboard) {
			f := b.ChartThemeFallbacks["chart-theme"]
			f.Series = nil
			b.ChartThemeFallbacks["chart-theme"] = f
		},
		func(b *ObjectClipboard) {
			f := b.ChartThemeFallbacks["chart-theme"]
			f.Categories = append(f.Categories, f.Categories[0])
			b.ChartThemeFallbacks["chart-theme"] = f
		},
		func(b *ObjectClipboard) {
			f := b.ChartThemeFallbacks["chart-theme"]
			f.Categories[0].LabelColor = "theme:heading"
			b.ChartThemeFallbacks["chart-theme"] = f
		},
		func(b *ObjectClipboard) {
			f := b.ChartThemeFallbacks["chart-theme"]
			f.TextColor = ""
			b.ChartThemeFallbacks["chart-theme"] = f
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&bundle)
			session := newNativeEditorSession("target.md", Deck{Slides: []Slide{{ID: "target"}}}, false, false)
			before := cloneDeck(session.deck)
			if err := session.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err == nil || !reflect.DeepEqual(session.deck, before) || len(session.undo) != 0 {
				t.Fatal("invalid chart fallback was not atomic", err)
			}
		})
	}
}
