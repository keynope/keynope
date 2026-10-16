package pptx

import "testing"

func TestExplicitZeroOpacityPaintSelection(t *testing.T) {
	for _, tc := range []struct {
		name, paint, text string
		set               bool
		opacity           float64
	}{
		{"fill-zero", `<a:solidFill><a:srgbClr val="ff0000"><a:alpha val="0"/></a:srgbClr></a:solidFill><a:ln><a:solidFill><a:srgbClr val="ffffff"><a:alpha val="50000"/></a:srgbClr></a:solidFill></a:ln>`, "", true, 0},
		{"stroke-zero", `<a:ln><a:solidFill><a:srgbClr val="ffffff"><a:alpha val="0"/></a:srgbClr></a:solidFill></a:ln>`, "", true, 0},
		{"text-zero", "", `<a:p><a:r><a:rPr><a:solidFill><a:srgbClr val="ffffff"><a:alpha val="0"/></a:srgbClr></a:solidFill></a:rPr><a:t>Text</a:t></a:r></a:p>`, true, 0},
		{"unspecified", `<a:solidFill><a:srgbClr val="ff0000"/></a:solidFill>`, "", false, 0},
		{"explicit-opaque", `<a:solidFill><a:srgbClr val="ff0000"><a:alpha val="100000"/></a:srgbClr></a:solidFill>`, "", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parseXML([]byte(`<p:sp xmlns:p="p" xmlns:a="a"><p:spPr>`+tc.paint+`</p:spPr><p:txBody>`+tc.text+`</p:txBody></p:sp>`), 64)
			if err != nil {
				t.Fatal(err)
			}
			o, err := parseShape(n, nil, colourResolver{}, nil, true)
			if err != nil || o == nil {
				t.Fatal(err)
			}
			if o.OpacitySet != tc.set || o.Opacity != tc.opacity {
				t.Fatalf("opacity %v, set %v", o.Opacity, o.OpacitySet)
			}
		})
	}
	base := Object{Color: "#ffffff", Opacity: .5, OpacitySet: true}
	o := mergeInheritedObject(base, Object{Opacity: 0, OpacitySet: true})
	if !o.OpacitySet || o.Opacity != 0 {
		t.Fatal("explicit zero lost during inheritance")
	}
}
