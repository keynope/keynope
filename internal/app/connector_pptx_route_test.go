package app

import (
	"bytes"
	"context"
	"keynope/internal/pptx"
	"math"
	"os"
	"testing"
)

func TestSceneConnectorExportsAllRouteVertices(t *testing.T) {
	points := []connectorPoint{{X: 100, Y: 200}, {X: 350, Y: 200}, {X: 350, Y: 500}, {X: 250, Y: 500}, {X: 250, Y: 400}}
	opacity := .4
	object, warnings := sceneObjectToPPTX(sceneObject{ID: "elbow", Kind: "connector", Points: points, Arrows: "both", ArrowWidth: 6, Paint: scenePaint{Stroke: "#55aaff", StrokeWidth: 2, Opacity: &opacity}})
	if object == nil || len(object.LinePoints) != len(points) {
		t.Fatal(object, warnings)
	}
	for _, warning := range warnings {
		if warning.Code == "connector-route-approximated" {
			t.Fatal(warning)
		}
	}
	var buffer bytes.Buffer
	if _, err := pptx.Write(context.Background(), &buffer, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{*object}}}}); err != nil {
		t.Fatal(err)
	}
	if file := os.Getenv("KEYNOPE_CONNECTOR_PPTX_FIXTURE"); file != "" {
		if err := os.WriteFile(file, buffer.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	restored, _, err := pptx.Read(context.Background(), bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	route := restored.Slides[0].Objects[0]
	if len(route.LinePoints) != len(points) || route.LineHead.Type != "triangle" || route.LineTail.Type != "triangle" {
		t.Fatal(route)
	}
	for i, p := range route.LinePoints {
		if math.Abs(p.X-points[i].X) > 1.0/6350 || math.Abs(p.Y-points[i].Y) > 1.0/6350 {
			t.Fatal(i, p, points[i])
		}
	}
	_, loss, _, err := pptxLineToElement(route, route.X, route.Y, route.Width, route.Height, 1)
	if err != nil {
		t.Fatal(err)
	}
	reported := false
	for _, warning := range loss {
		reported = reported || warning.Code == "line-route-approximated"
	}
	if !reported {
		t.Fatal("import route loss was not reported")
	}
}
