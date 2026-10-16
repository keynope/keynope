package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"

	"keynope/internal/pptx"
)

func TestPPTXExportRetainsObjectOpacity(t *testing.T) {
	for _, opacity := range []float64{0, .5, 1} {
		for _, kind := range []string{"shape", "text", "connector"} {
			o := sceneObject{ID: "alpha", Kind: kind, Shape: "square", Paint: scenePaint{Color: "#123456", Stroke: "#ffffff", StrokeWidth: 2, Opacity: &opacity}}
			if kind == "text" {
				o.Text = &sceneText{FontID: "c64", WidthScale: 1}
				o.Paint.Stroke = ""
			}
			if kind == "connector" {
				o.Points = []connectorPoint{{X: 10, Y: 10}, {X: 100, Y: 10}}
			}
			got, warnings := sceneObjectToPPTX(o)
			if got == nil || !got.OpacitySet || got.Opacity != opacity {
				t.Fatalf("%s alpha %g not exported: %+v", kind, opacity, got)
			}
			if kind == "connector" && got.StrokeWidth != 1 {
				t.Fatal("connector scene-to-point width conversion lost", got.StrokeWidth)
			}
			for _, warning := range warnings {
				if warning.Code == "text-effects-approximated" || warning.Code == "shape-effects-approximated" {
					t.Fatal("opacity alone caused an approximation warning", warning)
				}
			}
		}
	}
}

func TestPPTXConnectorArrowExport(t *testing.T) {
	for _, arrows := range []string{"none", "start", "end", "both"} {
		o := sceneObject{ID: "arrow", Kind: "connector", Arrows: arrows, ArrowWidth: 6, Points: []connectorPoint{{X: 100}, {X: 20, Y: 40}}, Paint: scenePaint{Stroke: "#ffffff", StrokeWidth: 1}}
		got, warnings := sceneObjectToPPTX(o)
		if (got.LineHead.Type == "triangle") != (arrows == "start" || arrows == "both") || (got.LineTail.Type == "triangle") != (arrows == "end" || arrows == "both") {
			t.Fatal(arrows, got)
		}
		warned := false
		for _, warning := range warnings {
			if warning.Code == "connector-arrow-size-approximated" {
				warned = true
			}
		}
		if warned != (arrows != "none") {
			t.Fatal("wrong approximation diagnostic", arrows, warnings)
		}
	}
}

func TestPPTXExplicitZeroOpacityTextAndLine(t *testing.T) {
	for _, kind := range []string{"text", "line"} {
		object := pptx.Object{ID: "zero", Kind: kind, Width: 200, Height: 40, Text: "Hidden", Color: "#ffffff", Stroke: "#ffffff", OpacitySet: true}
		e, _, _, err := pptxObjectToElement(object, 1, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(e.Query)
		if q.Get("modern-opacity") != "0" {
			t.Fatal(kind, "lost explicit zero", e.Query)
		}
		object.OpacitySet = false
		e, _, _, err = pptxObjectToElement(object, 1, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		q, _ = url.ParseQuery(e.Query)
		if q.Has("modern-opacity") {
			t.Fatal(kind, "invented zero opacity")
		}
	}
}

func TestPPTXShapeLabelDoesNotMultiplyObjectOpacity(t *testing.T) {
	for _, opacity := range []float64{0, .1, .5, .9, 1} {
		object := pptx.Object{ID: "labelled", Kind: "shape", Shape: "rect", Width: 300, Height: 120, Fill: "#123456", Text: "Visible label", Color: "#ffffff", FontSize: 24, Opacity: opacity}
		object.OpacitySet = true
		element, _, _, err := pptxObjectToElement(object, 1, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		query, err := url.ParseQuery(element.Query)
		if err != nil {
			t.Fatal(err)
		}
		if opacity < 1 && query.Get("modern-opacity") != decimal(opacity) {
			t.Fatal("shape opacity lost", element.Query)
		}
		encoded, err := base64.StdEncoding.DecodeString(query.Get("shape-label"))
		if err != nil {
			t.Fatal(err)
		}
		var label shapeLabelData
		if err := json.Unmarshal(encoded, &label); err != nil {
			t.Fatal(err)
		}
		labelQuery, err := url.ParseQuery(label.Query)
		if err != nil {
			t.Fatal(err)
		}
		if labelQuery.Has("modern-opacity") {
			t.Fatal("label multiplies parent opacity", label.Query)
		}
		if label.Text != object.Text || labelQuery.Get("fg") != object.Color {
			t.Fatal("label content/style changed", label)
		}
	}
}
