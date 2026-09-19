package main

import (
	"math"
	"net/url"
	"reflect"
	"testing"
)

func TestShapePortProjectionReusesReadOnlyMetadata(t *testing.T) {
	for _, query := range []string{
		"shape=diamond&width=20.25&height=9.75&object-rotation=37.5&object-offset-x=0.25",
		"shape=triangle&width=20&height=10&object-rotation=90&bad=%zz",
		"shape=unknown&object-rotation=NaN",
	} {
		e := Element{ID: "shape", Kind: "shape", Query: query}
		q, _ := url.ParseQuery(query)
		before := q.Encode()
		want := rotateShapePorts(shapePorts(e, 30, 10), e, 30, 10, 245, 56)
		for range 3 {
			got := rotateShapePortsFromValues(shapePortsFromValues(e.ID, q, 30, 10), q, 30, 10, 245, 56)
			if !reflect.DeepEqual(got, want) || q.Encode() != before {
				t.Fatalf("shared query changed projection or metadata: %q", query)
			}
		}
	}
}

func TestRotatedShapePortsUsePhysicalAxes(t *testing.T) {
	e := Element{ID: "a", Kind: "shape", Query: "shape=square&width=20&height=10&object-rotation=90"}
	ports := rotateShapePorts(shapePorts(e, 30, 10), e, 30, 10, 245, 56)
	for _, p := range ports {
		if p.Side == "right" {
			if math.Abs(p.X-40) > 1e-9 || math.Abs(p.Y-(15+10*(1920.0/245)/(1080.0/56))) > 1e-9 || p.Direction != "bottom" {
				t.Fatalf("right port: %+v", p)
			}
		}
		if p.Side == "top" && p.Direction != "right" {
			t.Fatalf("top direction: %+v", p)
		}
	}
	ports = append(ports, shapePort{ID: "b", Side: "left", X: 80, Y: 15})
	connector := Element{Kind: "connector", Query: "connector-from=a&connector-from-side=right&connector-to=b&connector-to-side=left&connector-mode=elbow"}
	a, b, ok := connectorEndpoints(connector, ports)
	if !ok || a.Side != "bottom" {
		t.Fatal("authored side failed to resolve after rotation")
	}
	path := connectorPath(connector, a, b)
	if path[0].X != a.X || path[0].Y != a.Y || path[1].X != a.X || path[1].Y <= a.Y {
		t.Fatal("connector does not leave rotated edge", path)
	}
}

func TestFractionalShapePortRotation(t *testing.T) {
	e := Element{ID: "a", Kind: "shape", Query: "shape=circle&width=20&height=10&object-rotation=-37.5"}
	before := shapePorts(e, 30, 10)
	after := rotateShapePorts(append([]shapePort(nil), before...), e, 30, 10, 245, 56)
	for i, p := range after {
		a := before[i]
		sx, sy := 1920.0/245, 1080.0/56
		original := math.Hypot((a.X-40)*sx, (a.Y-15)*sy)
		rotated := math.Hypot((p.X-40)*sx, (p.Y-15)*sy)
		if math.Abs(original-rotated) > 1e-8 || p.Side != a.Side {
			t.Fatal("rotation changed radius or attachment identity")
		}
	}
}

func TestSceneConnectorFollowsRotatedAttachment(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := sceneFixture(t)
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		for i, e := range deck.Slides[0].Elements {
			if e.ID == "box" {
				deck.Slides[0].Elements[i].Query = setQueryValue(e.Query, "object-rotation", "37.5")
			}
		}
		scene, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		slide := withTrueTypeDefaults(deck.ResolveSlide(0, false))
		ports := slideShapePorts(slide, layout(slide, 245, 56), 245, 56)
		var anchor shapePort
		for _, p := range ports {
			if p.ID == "box" && p.Side == "right" {
				anchor = p
			}
		}
		found := false
		for _, o := range scene.Objects {
			if o.Kind == "connector" {
				found = true
				if len(o.Points) < 2 || math.Abs(o.Points[0].X-anchor.X*1920/245) > 1e-8 || math.Abs(o.Points[0].Y-anchor.Y*1080/56) > 1e-8 {
					t.Fatalf("%s connector missed rotated anchor", style)
				}
			}
		}
		if !found {
			t.Fatal("connector missing")
		}
	}
}
