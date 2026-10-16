package pptx

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func customGeometryFixture() *CustomGeometry {
	return &CustomGeometry{TextRect: [4]int64{100000, 200000, 900000, 800000}, Paths: []GeometryPath{{Fill: true, Commands: []GeometryCommand{{Kind: "move", Points: []GeometryPoint{{0, 0}}}, {Kind: "line", Points: []GeometryPoint{{1000000, 0}}}, {Kind: "cubic", Points: []GeometryPoint{{1000000, 1000000}, {0, 1000000}, {0, 0}}}, {Kind: "close"}}}}}
}

func TestCustomGeometryPackageAndXMLRoundTrip(t *testing.T) {
	g := customGeometryFixture()
	o := Object{ID: "custom", Kind: "shape", Shape: "custom", Width: 400, Height: 200, Fill: "#123456", Text: "Native curves", FontSize: 24, CustomGeometry: g, ShapeAdjustments: map[string]int64{"knVersion": 1, "knKind": 12345}}
	var data bytes.Buffer
	if _, err := Write(context.Background(), &data, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	p, report, err := Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	got := p.Slides[0].Objects[0]
	if got.Shape != "custom" || !reflect.DeepEqual(got.CustomGeometry, g) || !reflect.DeepEqual(got.ShapeAdjustments, o.ShapeAdjustments) || got.Text != "Native curves" {
		t.Fatal("native custom geometry lost", got)
	}
	xml := shapeXML(o, 2, "")
	if !strings.Contains(xml, "<a:cubicBezTo>") || !strings.Contains(xml, `fmla="*/ w 100000 1000000"`) || strings.Contains(xml, "<a:prstGeom") {
		t.Fatal("custom shape flattened/substituted", xml)
	}
}

func TestCustomGeometryRejectsInvalidInputBeforeZIP(t *testing.T) {
	for _, mutate := range []func(*CustomGeometry){
		func(g *CustomGeometry) { g.Paths = nil },
		func(g *CustomGeometry) { g.Paths = make([]GeometryPath, 17) },
		func(g *CustomGeometry) { g.TextRect[2] = g.TextRect[0] },
		func(g *CustomGeometry) { g.Paths[0].Commands[1].Kind = "arc" },
		func(g *CustomGeometry) { g.Paths[0].Commands[1].Points = nil },
		func(g *CustomGeometry) { g.Paths[0].Commands[0].Points[0].X = 4*GeometryUnits + 1 },
		func(g *CustomGeometry) { g.Paths[0].Commands = g.Paths[0].Commands[1:] },
		func(g *CustomGeometry) { g.Paths[0].Commands = g.Paths[0].Commands[:3] },
		func(g *CustomGeometry) { g.Paths[0].Commands[1] = g.Paths[0].Commands[0] },
		func(g *CustomGeometry) { g.Paths[0].Commands = make([]GeometryCommand, 2049) },
	} {
		g := customGeometryFixture()
		mutate(g)
		var out bytes.Buffer
		if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "shape", Width: 200, Height: 100, CustomGeometry: g}}}}}); err == nil || out.Len() != 0 {
			t.Fatal("invalid custom path partially written")
		}
	}
}

func TestCustomGeometryParserRejectsFormulasAndCommands(t *testing.T) {
	base := customGeometryXML(customGeometryFixture(), nil)
	for _, xml := range []string{
		strings.Replace(base, `x="1000000"`, `x="w"`, 1),
		strings.Replace(base, "cubicBezTo", "quadBezTo", 2),
		strings.Replace(base, `fill="norm"`, `fill="darken"`, 1),
		strings.Replace(base, `stroke="1"`, `stroke="invalid"`, 1),
		strings.Replace(base, `fmla="*/ w 100000 1000000"`, `fmla="val 5"`, 1),
		strings.Replace(base, `w="1000000"`, `w="0"`, 1),
		strings.Replace(base, `<a:close/>`, "", 1),
		strings.Replace(base, `l="knTxL"`, `l="r"`, 1),
	} {
		n, err := parseXML([]byte(`<root xmlns:a="a">`+xml+`</root>`), 32)
		if err != nil {
			t.Fatal(err)
		}
		if geometry, loss := parseCustomGeometry(findFirst(n, "custGeom")); geometry != nil || !loss {
			t.Fatal("unsafe/unsupported path accepted", xml)
		}
	}
}

func TestCustomGeometryInheritanceOwnsCommands(t *testing.T) {
	base := Object{Shape: "custom", hasShapeGeometry: true, CustomGeometry: customGeometryFixture()}
	got := mergeInheritedObject(base, Object{})
	got.CustomGeometry.Paths[0].Commands[0].Points[0].X = 123
	if base.CustomGeometry.Paths[0].Commands[0].Points[0].X != 0 {
		t.Fatal("inherited custom geometry aliases master")
	}
	explicit := mergeInheritedObject(base, Object{Shape: "rect", hasShapeGeometry: true})
	if explicit.CustomGeometry != nil {
		t.Fatal("explicit preset retained inherited custom path")
	}
}

func TestCustomGeometryUnpaintedLabelKeepsShapeKind(t *testing.T) {
	var data bytes.Buffer
	o := Object{Kind: "shape", Shape: "custom", Width: 400, Height: 200, Text: "Label only", CustomGeometry: customGeometryFixture()}
	if _, err := Write(context.Background(), &data, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	p, _, err := Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Slides[0].Objects[0]; got.Kind != "shape" || got.CustomGeometry == nil {
		t.Fatal("unpainted custom label lost its shape", got)
	}
}

func TestCustomGeometryIndependentPathStrokeRoundTrip(t *testing.T) {
	g := customGeometryFixture()
	g.Paths[0].NoStroke = true
	g.Paths = append(g.Paths, GeometryPath{Commands: []GeometryCommand{
		{Kind: "move", Points: []GeometryPoint{{0, 200000}}},
		{Kind: "cubic", Points: []GeometryPoint{{0, 400000}, {1000000, 400000}, {1000000, 200000}}},
	}})
	var data bytes.Buffer
	o := Object{Kind: "shape", Shape: "custom", Width: 400, Height: 200, Fill: "#123456", Stroke: "#202833", StrokeWidth: .75, CustomGeometry: g}
	if _, err := Write(context.Background(), &data, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	p, report, err := Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	got := p.Slides[0].Objects[0]
	if !reflect.DeepEqual(got.CustomGeometry, g) || got.Stroke != o.Stroke || got.StrokeWidth != o.StrokeWidth {
		t.Fatal("open detail path/independent body stroke lost", got)
	}
}
