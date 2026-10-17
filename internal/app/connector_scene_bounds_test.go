package app

import (
	"math"
	"testing"
)

func TestVectorConnectorBoundsTrackContinuousRoute(t *testing.T) {
	deck := powerupProofCorpus(t)["diagrams.md"]
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.Kind != "connector" {
			continue
		}
		found = true
		padding := math.Max(object.Paint.StrokeWidth/2, object.ArrowWidth/2)
		left, right, top, bottom := object.Points[0].X, object.Points[0].X, object.Points[0].Y, object.Points[0].Y
		for _, p := range object.Points {
			left = math.Min(left, p.X)
			right = math.Max(right, p.X)
			top = math.Min(top, p.Y)
			bottom = math.Max(bottom, p.Y)
		}
		expected := sceneRect{X: left - padding, Y: top - padding, Width: right - left + 2*padding, Height: bottom - top + 2*padding}
		if object.Bounds != expected {
			t.Fatal("connector uses legacy layout instead of route bounds", object.Bounds, expected)
		}
		for _, diagnostic := range scene.Diagnostics {
			if diagnostic.ObjectID == object.ID && diagnostic.Code == "bounds-overflow" {
				t.Fatal("in-canvas vector route falsely reported overflow", diagnostic)
			}
		}
	}
	if !found {
		t.Fatal("fixture missing connector")
	}
}
