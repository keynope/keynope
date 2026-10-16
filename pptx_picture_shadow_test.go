package main

import (
	"bytes"
	"context"
	"keynope/internal/pptx"
	"math"
	"reflect"
	"testing"
)

func TestPPTXBackgroundPicturePaint(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := retroBackgroundDeck(t, "contain", false)
	deck.Slides[0].BackgroundMedia.Query = "image-style=modern&shadow=soft&shadow-color=%2355aaff&shadow-x=-1&shadow-y=1&gradient-start=%23ff0055&gradient-end=%23ffffaa"
	before := cloneDeck(deck)
	presentation, report, err := deckToPPTX(deck)
	if err != nil {
		t.Fatal(err)
	}
	found, warned := false, false
	for _, object := range presentation.Slides[0].Objects {
		if object.ID != "keynope-background-image-0" {
			continue
		}
		found = true
		if object.Shadow == nil || object.Shadow.Blur != 8 || object.Shadow.Opacity == nil || *object.Shadow.Opacity != .6 || object.FillTop+object.FillLeft == 0 {
			t.Fatal("lost background shadow/opacity/contain", object)
		}
	}
	for _, warning := range report.Warnings {
		if warning.Code == "image-paint-approximated" && warning.Object == "keynope-background-image-0" && warning.Slide == 0 {
			warned = true
		}
	}
	if !found || !warned || !reflect.DeepEqual(before, deck) {
		t.Fatal(found, warned, report)
	}
}

func TestPPTXPictureRejectsInvalidPaintBeforeWriting(t *testing.T) {
	for _, object := range []pptx.Object{
		{Kind: "image", Shadow: &pptx.ShapeShadow{Color: "#ffffff", Blur: math.NaN()}},
		{Kind: "image", Gradient: &pptx.ShapeGradient{Start: "#000000", End: "#ffffff", Direction: "horizontal"}},
	} {
		var out bytes.Buffer
		if _, err := pptx.Write(context.Background(), &out, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{object}}}}); err == nil || out.Len() != 0 {
			t.Fatal("invalid picture paint wrote output", err, out.Len())
		}
	}
}

func TestPPTXPictureShadowRoundTrip(t *testing.T) {
	for _, alpha := range []float64{0, .4, 1} {
		for _, blur := range []float64{0, 8} {
			scene := sceneObject{ID: "picture", Kind: "image", Bounds: sceneRect{X: 100, Y: 80, Width: 300, Height: 200}, Media: &sceneMedia{Source: "data:image/png;base64,iVBORw0KGgo=", Mask: "ellipse"}, Paint: scenePaint{Opacity: &alpha, ShadowColor: "#55aaff", ShadowX: -1920.0 / 245, ShadowY: 1080.0 / 56, ShadowBlur: blur}}
			object, warnings := sceneObjectToPPTX(scene)
			if object == nil || object.Shadow == nil || len(warnings) != 0 {
				t.Fatal(object, warnings)
			}
			var out bytes.Buffer
			if _, err := pptx.Write(context.Background(), &out, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*object}}}}); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := pptx.Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			got := loaded.Slides[0].Objects[0]
			if got.Shadow == nil || math.Abs(got.Shadow.X-object.Shadow.X) > .001 || math.Abs(got.Shadow.Y-object.Shadow.Y) > .001 || got.Shadow.Blur != blur || got.Shadow.Opacity == nil || math.Abs(*got.Shadow.Opacity-alpha) > 1e-6 || got.Shape != "ellipse" || !bytes.Equal(got.Image, object.Image) {
				t.Fatalf("picture changed: %+v", got)
			}
			q := mustQuery("")
			if warnings := importPPTXPictureShadow(got, q, 1); len(warnings) != 0 || q.Get("shadow-color") != "#55AAFF" || q.Get("shadow-x") != "-1" || q.Get("shadow-y") != "1" {
				t.Fatal(q, warnings)
			}
		}
	}
}

func TestPPTXPictureReportsUnsupportedPaint(t *testing.T) {
	for _, paint := range []scenePaint{{GradientStart: "#ff0055", GradientEnd: "#ffffaa"}, {Stroke: "#ffffff", StrokeWidth: 2}} {
		object, warnings := sceneObjectToPPTX(sceneObject{ID: "paint", Kind: "image", Bounds: sceneRect{Width: 100, Height: 100}, Media: &sceneMedia{Source: "data:image/png;base64,iVBORw0KGgo="}, Paint: paint})
		if object == nil || len(warnings) != 1 || warnings[0].Code != "image-paint-approximated" || warnings[0].Object != "paint" {
			t.Fatal(object, warnings)
		}
	}
}
