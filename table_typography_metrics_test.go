package main

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestTableTypographyMetricsCommandsAndProjection(t *testing.T) {
	for _, metric := range []struct {
		name         string
		value, delta float64
	}{{"fontWidth", 72, 1}, {"lineHeight", 1.6, .1}} {
		t.Run(metric.name, func(t *testing.T) {
			deck, action := tableCellPropertyFixture(t)
			data := deck.Objects.Tables["cell-data"]
			data.Cells[0][0].Style = nil
			deck.Objects.Tables["cell-data"] = data
			before := cloneDeck(deck)
			action.Property = "cell." + metric.name
			action.PropertyValue, _ = json.Marshal(metric.value)
			changed, err := applyTableCellPropertyCommand(&deck, action, "")
			if err != nil || !changed {
				t.Fatal(changed, err)
			}
			caps, err := tableCellStyleCapabilities(deck, action, "", 1)
			if err != nil || caps.Properties[action.Property].Value != metric.value || caps.Properties[action.Property].Provenance != "explicit" {
				t.Fatal(caps, err)
			}
			action.PropertyValue = nil
			action.PropertyDelta = &metric.delta
			if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil || !changed {
				t.Fatal(changed, err)
			}
			caps, err = tableCellStyleCapabilities(deck, action, "", 2)
			want := metric.value + metric.delta
			if err != nil || math.Abs(caps.Properties[action.Property].Value.(float64)-want) > 1e-9 {
				t.Fatal(caps, err)
			}
			scene, err := buildSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			text := scene.Objects[0].Table.Cells[0].Text
			if metric.name == "lineHeight" && math.Abs(text.LineHeight-want) > 1e-9 {
				t.Fatal(text)
			}
			if metric.name == "fontWidth" {
				base := 1.0
				if text.FontID == "c64" {
					base = .4167
				}
				if math.Abs(text.WidthScale-base*want/100) > 1e-9 {
					t.Fatal(text)
				}
			}
			if !reflect.DeepEqual(before.Objects.Tables["cell-data"], data) {
				t.Fatal("command mutated source")
			}
			md, err := serializeDeck("metrics.md", deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("metrics.md", md)
			if err != nil || !reflect.DeepEqual(reopened.Objects, deck.Objects) {
				t.Fatal("metric Markdown round trip", err)
			}
			public, err := participantSlideMarkdown(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			audience, err := parseDeckData("public.md", public)
			if err != nil {
				t.Fatal(err)
			}
			publicScene, err := buildSlideScene(audience, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			actual := publicScene.Objects[0].Table.Cells[0].Text
			if actual.WidthScale != text.WidthScale || actual.LineHeight != text.LineHeight {
				t.Fatal("audience metric parity", actual, text)
			}
			action.PropertyDelta = nil
			action.PropertyValue = json.RawMessage(`""`)
			if changed, err := applyTableCellPropertyCommand(&deck, action, ""); err != nil || !changed {
				t.Fatal(changed, err)
			}
			current := deck.Objects.Tables[deck.Slides[0].Elements[0].ObjectID]
			if _, explicit := tableCellAuthoredValue(current.Cells[0][0].Style, metric.name); explicit {
				t.Fatal("reset retained explicit metric")
			}
		})
	}
}

func TestTableTypographyMetricsValidationAndOwnedJSON(t *testing.T) {
	width, spacing := 72.0, 1.6
	style := TableCellStyle{FontWidth: &width, LineHeight: &spacing}
	if err := style.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := cloneCellStyle(style)
	*copy.FontWidth = 90
	*copy.LineHeight = 2
	if *style.FontWidth != 72 || *style.LineHeight != 1.6 {
		t.Fatal("aliased metric pointers")
	}
	raw, err := json.Marshal(style)
	if err != nil {
		t.Fatal(err)
	}
	var restored TableCellStyle
	if err := json.Unmarshal(raw, &restored); err != nil || !reflect.DeepEqual(style, restored) {
		t.Fatal(string(raw), restored, err)
	}
	for _, name := range []string{"fontWidth", "lineHeight"} {
		low, high, _ := tableCellMetricRange(name)
		for _, value := range []float64{low - .01, high + .01, math.NaN(), math.Inf(1)} {
			invalid := cloneCellStyle(style)
			if name == "fontWidth" {
				invalid.FontWidth = &value
			} else {
				invalid.LineHeight = &value
			}
			if invalid.Validate() == nil {
				t.Fatalf("accepted %s=%v", name, value)
			}
		}
		deck, action := tableCellPropertyFixture(t)
		before := cloneDeck(deck)
		action.Property = "cell." + name
		action.PropertyValue = json.RawMessage(`"1.5"`)
		if _, err := applyTableCellPropertyCommand(&deck, action, ""); err == nil || !reflect.DeepEqual(deck, before) {
			t.Fatal("numeric string accepted or rejected input mutated deck")
		}
	}
}
