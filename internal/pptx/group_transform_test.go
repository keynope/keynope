package pptx

import (
	"fmt"
	"math"
	"testing"
)

func groupFixture(t *testing.T, rotation int, width, height int) *node {
	t.Helper()
	n, err := parseXML([]byte(fmt.Sprintf(`<p:grpSp xmlns:p="p" xmlns:a="a"><p:grpSpPr><a:xfrm rot="%d"><a:off x="635000" y="1270000"/><a:ext cx="%d" cy="%d"/><a:chOff x="63500" y="127000"/><a:chExt cx="635000" cy="635000"/></a:xfrm></p:grpSpPr></p:grpSp>`, rotation, width*6350, height*6350)), 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGroupTransformGeometryAndContent(t *testing.T) {
	objects := []Object{{ID: "shape", Kind: "shape", X: 20, Y: 30, Width: 30, Height: 40, OpacitySet: true, FontSize: 12, StrokeWidth: 2, Paragraphs: []TextParagraph{{Runs: []TextRun{{FontSize: 10}}}}, Table: &Table{Rows: []float64{40}, Columns: []float64{30}, Cells: [][]TableCell{{{FontSize: 10, Padding: 2, BorderWidth: 1}}}}}}
	warnings, err := applyGroupTransform(groupFixture(t, 0, 200, 200), objects)
	if err != nil || len(warnings) != 0 {
		t.Fatal(err, warnings)
	}
	o := objects[0]
	if o.X != 120 || o.Y != 220 || o.Width != 60 || o.Height != 80 || o.FontSize != 24 || o.StrokeWidth != 4 || o.Paragraphs[0].Runs[0].FontSize != 20 || o.Table.Rows[0] != 80 || o.Table.Columns[0] != 60 || o.Table.Cells[0][0].Padding != 4 || !o.OpacitySet || o.Opacity != 0 {
		t.Fatalf("transform lost content: %+v", o)
	}
	warnings, err = applyGroupTransform(groupFixture(t, 90*60000, 100, 100), objects)
	if err != nil || len(warnings) != 0 {
		t.Fatal(err, warnings)
	}
	if objects[0].Rotation != 90 {
		t.Fatal(objects[0])
	}
}

func TestGroupTransformsPolylineVerticesExactly(t *testing.T) {
	for _, test := range []struct {
		name        string
		rotation    int
		ownRotation float64
		flip        bool
		want        []LinePoint
	}{
		{"nonuniform-rotation", 90 * 60000, 0, false, []LinePoint{{240, 170}, {240, 230}, {200, 230}}},
		{"own-reflection", 0, 0, true, []LinePoint{{180, 210}, {120, 210}, {120, 250}}},
		{"own-rotation-skew", 0, 90, false, []LinePoint{{190, 215}, {190, 245}, {110, 245}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := []LinePoint{{20, 30}, {50, 30}, {50, 70}}
			objects := []Object{{ID: "route", Kind: "line", X: 20, Y: 30, Width: 30, Height: 40, LinePoints: original, Rotation: test.ownRotation, FlipH: test.flip, StrokeWidth: 2}}
			warnings, err := applyGroupTransform(groupFixture(t, test.rotation, 200, 100), objects)
			if err != nil || len(warnings) != 0 {
				t.Fatal(err, warnings)
			}
			got := objects[0]
			if got.Rotation != 0 || got.FlipH || got.FlipV || math.Abs(got.StrokeWidth-2*math.Sqrt(2)) > 1e-8 {
				t.Fatal(got)
			}
			for i, p := range got.LinePoints {
				if math.Abs(p.X-test.want[i].X) > 1e-8 || math.Abs(p.Y-test.want[i].Y) > 1e-8 {
					t.Fatal(i, p, test.want[i])
				}
				if original[i] != ([]LinePoint{{20, 30}, {50, 30}, {50, 70}})[i] {
					t.Fatal("mutated route input")
				}
			}
			// An identity outer transform must not repeat the baked rotation.
			n, err := parseXML([]byte(`<p:grpSp xmlns:p="p" xmlns:a="a"><p:grpSpPr><a:xfrm/></p:grpSpPr></p:grpSp>`), 64)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = applyGroupTransform(n, objects); err != nil {
				t.Fatal(err)
			}
			for i, p := range objects[0].LinePoints {
				if math.Abs(p.X-test.want[i].X) > 1e-8 || math.Abs(p.Y-test.want[i].Y) > 1e-8 {
					t.Fatal("nested transform repeated", p)
				}
			}
		})
	}
}

func TestNestedGroupParsingAppliesEachTransform(t *testing.T) {
	transform := `<p:grpSpPr><a:xfrm><a:off x="63500" y="127000"/><a:ext cx="1270000" cy="1270000"/><a:chOff x="0" y="0"/><a:chExt cx="635000" cy="635000"/></a:xfrm></p:grpSpPr>`
	shape := `<p:sp><p:nvSpPr><p:cNvPr id="7"/></p:nvSpPr><p:spPr><a:xfrm><a:off x="6350" y="12700"/><a:ext cx="19050" cy="25400"/></a:xfrm><a:solidFill><a:srgbClr val="ffffff"><a:alpha val="0"/></a:srgbClr></a:solidFill></p:spPr></p:sp>`
	n, err := parseXML([]byte(`<p:grpSp xmlns:p="p" xmlns:a="a">`+transform+`<p:grpSp>`+transform+shape+`</p:grpSp></p:grpSp>`), 64)
	if err != nil {
		t.Fatal(err)
	}
	objects, warnings, err := parseGroup(n, nil, nil, nil, nil, DefaultLimits(), colourResolver{})
	if err != nil || len(objects) != 1 || len(warnings) != 0 {
		t.Fatal(err, objects, warnings)
	}
	o := objects[0]
	if o.X != 34 || o.Y != 68 || o.Width != 12 || o.Height != 16 || !o.OpacitySet || o.Opacity != 0 {
		t.Fatal(o)
	}
}

func TestGroupSkewIsReported(t *testing.T) {
	objects := []Object{{ID: "rotated", Kind: "shape", X: 10, Y: 20, Width: 10, Height: 20, Rotation: 45}}
	warnings, err := applyGroupTransform(groupFixture(t, 0, 200, 100), objects)
	if err != nil || len(warnings) != 1 || warnings[0].Code != "group-skew-approximated" {
		t.Fatal(err, warnings)
	}
	if math.Abs(objects[0].Width-10*math.Sqrt(2.5)) > 1e-8 {
		t.Fatal(objects[0])
	}
}

func TestGroupRotationUsesGroupCenter(t *testing.T) {
	objects := []Object{{X: 20, Y: 30, Width: 30, Height: 40}}
	warnings, err := applyGroupTransform(groupFixture(t, 90*60000, 200, 200), objects)
	if err != nil || len(warnings) != 0 {
		t.Fatal(err, warnings)
	}
	o := objects[0]
	if math.Abs(o.X-210) > 1e-8 || math.Abs(o.Y-210) > 1e-8 || o.Width != 60 || o.Height != 80 || o.Rotation != 90 {
		t.Fatal(o)
	}
}

func TestGroupTransformRejectsInvalidExtents(t *testing.T) {
	for _, width := range []int{-1} {
		if _, err := applyGroupTransform(groupFixture(t, 0, width, 100), []Object{{Width: 1, Height: 1}}); err == nil {
			t.Fatal("invalid extents accepted")
		}
	}
}

func TestGroupOptionalAndDegenerateTransforms(t *testing.T) {
	for _, body := range []string{
		`<a:xfrm/>`,
		`<a:xfrm><a:ext cx="635000" cy="635000"/></a:xfrm>`,
		`<a:xfrm><a:chExt cx="635000" cy="635000"/></a:xfrm>`,
		`<a:xfrm><a:ext cx="635000" cy="0"/><a:chExt cx="635000" cy="0"/></a:xfrm>`,
	} {
		n, err := parseXML([]byte(`<p:grpSp xmlns:p="p" xmlns:a="a"><p:grpSpPr>`+body+`</p:grpSpPr></p:grpSp>`), 64)
		if err != nil {
			t.Fatal(err)
		}
		objects := []Object{{X: 10, Y: 20, Width: 30, Height: 0, Kind: "line"}}
		warnings, err := applyGroupTransform(n, objects)
		if err != nil || len(warnings) != 0 {
			t.Fatal(body, err, warnings)
		}
		if objects[0].X != 10 || objects[0].Y != 20 || objects[0].Width != 30 || objects[0].Height != 0 {
			t.Fatal("identity group altered child", body, objects[0])
		}
	}
}
