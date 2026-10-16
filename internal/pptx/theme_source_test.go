package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestEqualThemeValuesKeepDistinctSourceOwners(t *testing.T) {
	var output bytes.Buffer
	object := Object{ID: "text", Kind: "text", Text: "Same values, independent themes", FontFace: "Arial", FontSize: 24, Color: "#112233", Width: 500, Height: 100}
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{object}}, {Objects: []Object{object}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, data := range packageParts(t, output.Bytes()) {
		parts[name] = string(data)
	}
	parts["ppt/theme/independent.xml"] = parts["ppt/theme/theme1.xml"]
	parts["ppt/slideMasters/independent.xml"] = masterXML()
	parts["ppt/slideMasters/_rels/independent.xml.rels"] = `<Relationships><Relationship Id="theme" Type="` + officeRelationshipNamespace + `/theme" Target="../theme/independent.xml"/></Relationships>`
	parts["ppt/slideLayouts/independent.xml"] = layoutXML()
	parts["ppt/slideLayouts/_rels/independent.xml.rels"] = `<Relationships><Relationship Id="master" Type="` + officeRelationshipNamespace + `/slideMaster" Target="../slideMasters/independent.xml"/></Relationships>`
	parts["ppt/slides/_rels/slide2.xml.rels"] = `<Relationships><Relationship Id="layout" Type="` + officeRelationshipNamespace + `/slideLayout" Target="../slideLayouts/independent.xml"/></Relationships>`
	for _, part := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml"} {
		parts[part] = strings.ReplaceAll(parts[part], `<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent1"/>`)
		parts[part] = strings.ReplaceAll(parts[part], `typeface="Arial"`, `typeface="+mn-lt"`)
	}
	data := testPPTX(t, parts)
	p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Theme == nil || p.Theme.SourcePart != "ppt/theme/theme1.xml" {
		t.Fatal("primary source owner missing", p.Theme)
	}
	a, b := p.Slides[0].Objects[0], p.Slides[1].Objects[0]
	if a.Color != b.Color || a.FontFace != b.FontFace || a.ColorThemeSlot != b.ColorThemeSlot || a.FontThemeRole != b.FontThemeRole {
		t.Fatal("fixture does not use coincident theme values", a, b)
	}
	for i, owner := range []string{"ppt/theme/theme1.xml", "ppt/theme/independent.xml"} {
		o := p.Slides[i].Objects[0]
		r := o.Paragraphs[0].Runs[0]
		if o.ColorThemeSource != owner || o.FontThemeSource != owner || r.ColorThemeSource != owner || r.FontThemeSource != owner {
			t.Fatal("equal values collapsed independent ownership", i, o, r)
		}
	}
	// Source locations are request-local read metadata. Writing this model
	// creates its own normal package; old relationship paths never enter XML.
	var again bytes.Buffer
	if _, err := Write(context.Background(), &again, p); err != nil {
		t.Fatal(err)
	}
	for name, value := range packageParts(t, again.Bytes()) {
		if strings.Contains(string(value), "independent.xml") {
			t.Fatal("read-time owner leaked into written part", name)
		}
	}
}
