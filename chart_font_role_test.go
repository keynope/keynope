package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"keynope/internal/pptx"
)

func TestChartFontRolesLifecycleAndDependencies(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		for _, role := range []string{"heading", "body", "mono"} {
			t.Run(kind+"/"+role, func(t *testing.T) {
				deck := chartThemeFixture(kind)
				deck.Theme.Fonts.Heading.FontID, deck.Theme.Fonts.Body.FontID, deck.Theme.Fonts.Mono.FontID = "c64", "sans", "mono"
				data := deck.Objects.Charts["chart-theme-data"]
				width := 72.0
				data.Options.FontRole, data.Options.FontID, data.Options.FontWidth = role, "sans", &width
				data.Options.Extra = extensionFixture("future", `{"kept":true}`)
				deck.Objects.Charts["chart-theme-data"] = data
				if err := data.Validate(); err != nil {
					t.Fatal(err)
				}
				before := cloneDeck(deck)
				inputs := deck.chartInputs(data, 1000, 600, "#123456", "mono")
				want := deck.wordFontID(role, "sans")
				if inputs.Text.FontID != want {
					t.Fatal("chart role did not override owner/fallback", inputs.Text.FontID, want)
				}
				updated := cloneDeck(deck)
				nextFont := "mono"
				if want == nextFont {
					nextFont = "c64"
				}
				switch role {
				case "heading":
					updated.Theme.Fonts.Heading.FontID = nextFont
				case "body":
					updated.Theme.Fonts.Body.FontID = nextFont
				case "mono":
					updated.Theme.Fonts.Mono.FontID = nextFont
				}
				if sceneProjectionInputsEqual(deck, updated, sceneResponseKey{index: 0}) {
					t.Fatal("font-role edit reused stale scene")
				}
				if updated.chartInputs(data, 1000, 600, "#123456", "mono").Text.FontID != nextFont {
					t.Fatal("changed role not resolved")
				}
				withoutTheme := cloneDeck(deck)
				withoutTheme.Theme = nil
				if withoutTheme.chartInputs(data, 1000, 600, "#123456", "mono").Text.FontID != "sans" {
					t.Fatal("portable fallback ignored")
				}
				owner := deck.Slides[0].Elements[0]
				frozen, _, err := concreteThemeChart(deck, owner, data)
				if err != nil || frozen.Options.FontRole != "" || frozen.Options.FontID != want {
					t.Fatal("detach role/family lost", frozen.Options, err)
				}
				bundle, err := makeObjectClipboard(deck, []Element{owner})
				if err != nil {
					t.Fatal(err)
				}
				copied := bundle.Objects.Charts[owner.ObjectID]
				if copied.Options.FontRole != role || copied.Options.FontID != want {
					t.Fatal("clipboard fallback was stale", copied.Options)
				}
				for _, themed := range []bool{false, true} {
					destination := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}}
					if themed {
						destination.Theme = updated.Theme.Clone()
					}
					elements, assets, objects, err := prepareObjectClipboardPaste(destination, bundle)
					if err != nil {
						t.Fatal(err)
					}
					destination.Slides[0].Elements, destination.Assets, destination.Objects = elements, assets, objects
					actual := objects.Charts[elements[0].ObjectID]
					font := destination.chartInputs(actual, 1000, 600, "#123456", "").Text.FontID
					if themed && (font != nextFont || actual.Options.FontRole != role) || !themed && (font != want || actual.Options.FontRole != "") {
						t.Fatal("cross-deck font role lost", themed, font, actual.Options)
					}
				}
				encoded, err := serializeDeck("Font roles.md", deck)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("Font roles.md", encoded)
				if err != nil || !reflect.DeepEqual(reopened.Objects.Charts[owner.ObjectID], data) {
					t.Fatal("font role/extension not portable", err)
				}
				public, err := participantSlideMarkdown(deck, 0)
				if err != nil {
					t.Fatal(err)
				}
				audience, err := parseDeckData("Public.md", public)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := buildSlideScene(deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				audienceScene, err := buildSlideScene(audience, 0, 245, 56)
				if err != nil || !reflect.DeepEqual(scene.Objects[0].Chart, audienceScene.Objects[0].Chart) {
					t.Fatal("audience chart font differs", err)
				}
				nativeBytes, report, err := exportPPTXDocument(deck)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, warning := range report.Warnings {
					found = found || warning.Code == "chart-theme-font-frozen"
				}
				if !found {
					t.Fatal("native role freeze not reported")
				}
				native, _, err := pptx.Read(context.Background(), bytes.NewReader(nativeBytes), int64(len(nativeBytes)), pptx.Options{})
				if err != nil {
					t.Fatal(err)
				}
				found = false
				for _, object := range native.Slides[0].Objects {
					if object.Chart != nil {
						found = importFont(object.Chart.FontFace) == want
					}
				}
				if !found {
					t.Fatal("native chart did not retain resolved font family", want)
				}
				if !reflect.DeepEqual(deck, before) {
					t.Fatal("role projection mutated source")
				}
			})
		}
	}
}

func TestChartFontRoleValidationAndExtensions(t *testing.T) {
	for _, options := range []ChartOptions{{FontRole: "unknown", FontID: "sans"}, {FontRole: "heading"}, {FontRole: "heading", FontID: "foreign"}} {
		deck := chartThemeFixture("column")
		data := cloneChart(deck.Objects.Charts["chart-theme-data"])
		data.Options = options
		session := newNativeEditorSession("Invalid.md", deck)
		before, revision := cloneDeck(session.deck), session.version
		if err := session.apply(nativeEditorAction{Action: "set-chart-data", ObjectID: "chart-theme", ChartData: &data}); err == nil || !reflect.DeepEqual(before, session.deck) || len(session.undo) != 0 || revision != session.version {
			t.Fatal("invalid role mutated document/history", err)
		}
	}
	var options ChartOptions
	if err := json.Unmarshal([]byte(`{"fontRole":"heading","fontId":"sans","future":true}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.FontRole != "heading" || options.Extra == nil || len(*options.Extra) != 1 {
		t.Fatal("font role treated as opaque extension", options)
	}
}
