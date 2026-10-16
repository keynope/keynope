package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestShapeAdjustmentLossIsReported(t *testing.T) {
	for _, guides := range []string{
		`<a:gd name="adj" fmla="*/ w 1 2"/>`,
		`<a:gd name="adj" fmla="val 2147483648"/>`,
		`<a:gd name="adj" fmla="val 1"/><a:gd name="adj" fmla="val 2"/>`,
		strings.Repeat(`<a:gd name="adj" fmla="val 1"/>`, 33),
	} {
		xml := `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2"/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="1270000"/></a:xfrm><a:prstGeom prst="roundRect"><a:avLst>` + guides + `</a:avLst></a:prstGeom><a:solidFill><a:srgbClr val="ff0000"/></a:solidFill></p:spPr></p:sp></p:spTree></p:cSld></p:sld>`
		data := testPPTX(t, map[string]string{"ppt/slides/slide1.xml": xml})
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]*zip.File{}
		for _, file := range archive.File {
			files[file.Name] = file
		}
		_, warnings, err := readSlide(context.Background(), files, "ppt/slides/slide1.xml", DefaultLimits(), colourResolver{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range warnings {
			if w.Code == "shape-adjustment-formula-unsupported" && w.Object == "2" {
				found = true
			}
		}
		if !found {
			t.Fatal("unsupported adjustment disappeared silently", warnings)
		}
	}
}

func TestShapeAdjustmentLayoutInheritance(t *testing.T) {
	base := Object{Shape: "rightArrow", hasShapeGeometry: true, shapeAdjustmentLoss: true, ShapeAdjustments: map[string]int64{"adj1": 27000}}
	layout := mergeInheritedObject(base, Object{Shape: "rect"})
	if layout.Shape != "rightArrow" || !layout.shapeAdjustmentLoss || layout.ShapeAdjustments["adj1"] != 27000 {
		t.Fatal("missing master geometry")
	}
	layout.ShapeAdjustments["adj1"] = 1
	if base.ShapeAdjustments["adj1"] != 27000 {
		t.Fatal("layout aliases master")
	}
	explicit := mergeInheritedObject(base, Object{Shape: "rect", hasShapeGeometry: true})
	if explicit.Shape != "rect" || explicit.shapeAdjustmentLoss || len(explicit.ShapeAdjustments) != 0 {
		t.Fatal("explicit child geometry did not win")
	}
}

func TestShapeAdjustmentPackageRoundTrip(t *testing.T) {
	object := Object{ID: "arrow", Kind: "shape", Shape: "rightArrow", Width: 400, Height: 100, Fill: "#ff0000", ShapeAdjustments: map[string]int64{"adj1": 27000, "adj2": 172000}}
	var data bytes.Buffer
	if _, err := Write(context.Background(), &data, Presentation{Slides: []Slide{{Objects: []Object{object}}}}); err != nil {
		t.Fatal(err)
	}
	result, _, err := Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Slides[0].Objects[0].ShapeAdjustments, object.ShapeAdjustments) {
		t.Fatal("adjustments lost")
	}
	xml := shapeXML(object, 2, "")
	if !strings.Contains(xml, `<a:avLst><a:gd name="adj1" fmla="val 27000"/><a:gd name="adj2" fmla="val 172000"/></a:avLst>`) {
		t.Fatal(xml)
	}
}

func TestShapeAdjustmentRejectsBeforeWriting(t *testing.T) {
	for _, values := range []map[string]int64{{"bad\"name": 1}, {"adj": 2147483648}, {"adj": -2147483649}} {
		var data bytes.Buffer
		_, err := Write(context.Background(), &data, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "shape", ShapeAdjustments: values}}}}})
		if err == nil || data.Len() != 0 {
			t.Fatal("invalid adjustment emitted package")
		}
	}
}

func TestShapeAdjustmentInheritanceOwnsGuides(t *testing.T) {
	root, err := parseXML([]byte(`<p:sp xmlns:p="p" xmlns:a="a"><p:nvSpPr><p:cNvPr id="2"/></p:nvSpPr><p:spPr/></p:sp>`), 32)
	if err != nil {
		t.Fatal(err)
	}
	parent := &inheritedShape{object: Object{Shape: "rightArrow", Width: 400, Height: 100, Fill: "#ff0000", ShapeAdjustments: map[string]int64{"adj1": 27000, "adj2": 172000}}, hasGeometry: true}
	object, err := parseShape(root, nil, colourResolver{}, parent, true)
	if err != nil || object == nil {
		t.Fatal(err)
	}
	if object.Shape != "rightArrow" || object.ShapeAdjustments["adj2"] != 172000 {
		t.Fatal("inherited geometry lost")
	}
	object.ShapeAdjustments["adj2"] = 1
	if parent.object.ShapeAdjustments["adj2"] != 172000 {
		t.Fatal("inherited guides aliased")
	}
}
