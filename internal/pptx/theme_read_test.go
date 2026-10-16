package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestThemePartCacheIsBoundedAndRequestOwned(t *testing.T) {
	parts := map[string]string{}
	for i := 0; i < themePartCacheEntries+2; i++ {
		parts[fmt.Sprintf("ppt/masters/m%d.xml", i)] = `<master/>`
	}
	data := testPPTX(t, parts)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	limits := normaliseLimits(Limits{})
	cache := &themePartCache{}
	first, _, err := readThemePart(files, "ppt/masters/m0.xml", limits, cache)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := readThemePart(files, "ppt/masters/m0.xml", limits, cache)
	if err != nil || first != again {
		t.Fatal("shared part parsed again", err)
	}
	other, _, err := readThemePart(files, "ppt/masters/m0.xml", limits, &themePartCache{})
	if err != nil || first == other {
		t.Fatal("parse tree escaped its request", err)
	}
	for i := 1; i < themePartCacheEntries+2; i++ {
		if _, _, err := readThemePart(files, fmt.Sprintf("ppt/masters/m%d.xml", i), limits, cache); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != themePartCacheEntries || cache.bytes > themePartCacheBytes {
		t.Fatal("unbounded cache", cache.bytes, len(cache.entries))
	}
	full := &themePartCache{bytes: themePartCacheBytes - 1}
	if _, _, err := readThemePart(files, "ppt/masters/m0.xml", limits, full); err != nil || len(full.entries) != 0 {
		t.Fatal("byte limit ignored", err)
	}
	if _, _, err := readThemePart(files, "missing.xml", limits, cache); err == nil {
		t.Fatal("missing part cached as success")
	}
	base := defaultColourResolver()
	base.mapping["accent1"] = "accent1"
	n, err := parseXML([]byte(`<overrideClrMapping accent1="accent2"/>`), 16)
	if err != nil {
		t.Fatal(err)
	}
	override := applyColourMap(base, n)
	if override.mapping["accent1"] != "accent2" || base.mapping["accent1"] != "accent1" {
		t.Fatal("slide override polluted shared mapping")
	}
}

func TestThemeRelationshipsAndPerSlideFontsAreDeterministic(t *testing.T) {
	theme1 := &Theme{Name: "Red", Dark1: "#111111", Light1: "#ffffff", Dark2: "#222222", Light2: "#eeeeee", Accent: [6]string{"#aa1100", "#bb2200", "#cc3300", "#dd4400", "#ee5500", "#ff6600"}, Hyperlink: "#123456", FollowedHyperlink: "#654321", HeadingFont: "Arial", BodyFont: "Courier New"}
	theme2 := *theme1
	theme2.Name = "Green"
	theme2.Accent[0] = "#00bb11"
	theme2.Accent[1] = "#0000cc"
	theme2.HeadingFont = "Keynope C64 W80"
	var base bytes.Buffer
	o := Object{ID: "title", Kind: "text", Width: 200, Height: 100, Text: "Theme reference", Color: "#111111", FontFace: "+mj-lt", FontSize: 24}
	if _, err := Write(context.Background(), &base, Presentation{Theme: theme1, Slides: []Slide{{Objects: []Object{o}}, {Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for key, value := range packageParts(t, base.Bytes()) {
		parts[key] = string(value)
	}
	parts["ppt/theme/000-decoy.xml"] = themeXML(&Theme{Name: "Decoy", Dark1: "#000000", Light1: "#ffffff", Dark2: "#000000", Light2: "#ffffff", Accent: [6]string{"#ffffff", "#ffffff", "#ffffff", "#ffffff", "#ffffff", "#ffffff"}, Hyperlink: "#ffffff", FollowedHyperlink: "#ffffff", HeadingFont: "Wrong face", BodyFont: "Wrong face"})
	parts["ppt/theme/zzz-selected.xml"] = themeXML(&theme2)
	parts["ppt/presentation.xml"] = strings.Replace(parts["ppt/presentation.xml"], `xmlns:r="`+officeRelationshipNamespace+`"`, `xmlns:r="`+officeRelationshipNamespace+`" xmlns:rel="`+officeRelationshipNamespace+`"`, 1)
	parts["ppt/presentation.xml"] = strings.Replace(parts["ppt/presentation.xml"], `<p:sldMasterId id="2147483648" r:id="rId1"/>`, `<p:sldMasterId rel:id="selected" id="2147483649"/><p:sldMasterId id="2147483648" r:id="rId1"/>`, 1)
	parts["ppt/presentation.xml"] = strings.ReplaceAll(parts["ppt/presentation.xml"], `id="256" r:id="rId2"`, `rel:id="rId2" id="256"`)
	parts["ppt/_rels/presentation.xml.rels"] = strings.Replace(parts["ppt/_rels/presentation.xml.rels"], `</Relationships>`, `<Relationship Id="selected" Type="`+officeRelationshipNamespace+`/slideMaster" Target="masters/custom.xml"/></Relationships>`, 1)
	parts["ppt/masters/custom.xml"] = masterXML()
	parts["ppt/masters/_rels/custom.xml.rels"] = `<Relationships><Relationship Id="theme" Type="` + officeRelationshipNamespace + `/theme" Target="../theme/zzz-selected.xml"/></Relationships>`
	parts["ppt/layouts/custom.xml"] = layoutXML()
	parts["ppt/layouts/_rels/custom.xml.rels"] = `<Relationships><Relationship Id="master" Type="` + officeRelationshipNamespace + `/slideMaster" Target="../masters/custom.xml"/></Relationships>`
	parts["ppt/slides/_rels/slide2.xml.rels"] = `<Relationships><Relationship Id="layout" Type="` + officeRelationshipNamespace + `/slideLayout" Target="../layouts/custom.xml"/></Relationships>`
	for _, part := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml"} {
		parts[part] = strings.ReplaceAll(parts[part], `<a:srgbClr val="111111"/>`, `<a:schemeClr val="accent1"/>`)
	}
	// First slide uses the shape style, second an explicit Latin theme token.
	if !strings.Contains(parts["ppt/slides/slide1.xml"], `<a:latin typeface="+mj-lt"/>`) {
		t.Fatal("fixture font not found")
	}
	parts["ppt/slides/slide1.xml"] = strings.ReplaceAll(parts["ppt/slides/slide1.xml"], `<a:latin typeface="+mj-lt"/>`, "")
	parts["ppt/slides/slide1.xml"] = strings.Replace(parts["ppt/slides/slide1.xml"], `<p:txBody>`, `<p:style><a:fontRef idx="major"/></p:style><p:txBody>`, 1)
	parts["ppt/slides/slide2.xml"] = strings.Replace(parts["ppt/slides/slide2.xml"], `</p:sld>`, `<p:clrMapOvr><a:overrideClrMapping accent1="accent2"/></p:clrMapOvr></p:sld>`, 1)
	data := testPPTX(t, parts)
	for range 20 {
		p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Theme == nil || p.Theme.Name != "Green" {
			t.Fatal("presentation theme ignored declared master order", p.Theme)
		}
		if p.Theme.SourcePart != "ppt/theme/zzz-selected.xml" {
			t.Fatal("primary theme owner lost", p.Theme.SourcePart)
		}
		for i, want := range []struct{ colour, face string }{{"#AA1100", "Arial"}, {"#0000CC", "Keynope C64 W80"}} {
			got := p.Slides[i].Objects[0]
			if got.Color != want.colour || got.FontFace != want.face || got.Paragraphs[0].Runs[0].FontFace != want.face {
				t.Fatal("wrong per-slide palette/font", i, got.Color, got.FontFace)
			}
			owner := []string{"ppt/theme/theme1.xml", "ppt/theme/zzz-selected.xml"}[i]
			run := got.Paragraphs[0].Runs[0]
			if got.ColorThemeSource != owner || got.FontThemeSource != owner || run.ColorThemeSource != owner || run.FontThemeSource != owner {
				t.Fatal("foreign master reference rebound to primary owner", i, got, run)
			}
		}
	}
}

func TestThemeFontTokensKeepExplicitDeviations(t *testing.T) {
	r := defaultColourResolver()
	r.majorFont = "Major"
	r.minorFont = "Minor"
	for _, tc := range []struct{ source, want string }{{"+mj-lt", "Major"}, {"+mn-lt", "Minor"}, {"Concrete face", "Concrete face"}} {
		n, err := parseXML([]byte(`<rPr><latin typeface="`+tc.source+`"/></rPr>`), 16)
		if err != nil {
			t.Fatal(err)
		}
		face, _, _, _, _, _ := propertiesFromRun(n, r)
		if face != tc.want {
			t.Fatal(face, tc.want)
		}
	}
	if _, err := parseXML([]byte(`<x id="a" id="b"/>`), 16); err == nil {
		t.Fatal("duplicate XML attribute accepted")
	}
	n, err := parseXML([]byte(`<x xmlns:q="`+officeRelationshipNamespace+`" q:id="relationship" id="numeric"/>`), 16)
	if err != nil || relationshipID(n) != "relationship" || attr(n, "id") != "numeric" {
		t.Fatal("namespaced and plain IDs collapsed", err)
	}
}

func TestShapeThemeFontReferencePriority(t *testing.T) {
	r := defaultColourResolver()
	r.majorFont, r.minorFont = "Major", "Minor"
	for _, tc := range []struct{ name, index, defaults, run, want string }{
		{"major", "major", "", "", "Major"},
		{"minor", "minor", "", "", "Minor"},
		{"list override", "major", `<lvl1pPr><defRPr><latin typeface="Concrete"/></defRPr></lvl1pPr>`, "", "Concrete"},
		{"run override", "major", "", `<rPr><latin typeface="Concrete"/></rPr>`, "Concrete"},
		{"run theme override", "major", "", `<rPr><latin typeface="+mn-lt"/></rPr>`, "Minor"},
		{"none retains inheritance", "none", "", "", "Inherited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parseXML([]byte(`<sp><style><fontRef idx="`+tc.index+`"/></style><txBody><lstStyle>`+tc.defaults+`</lstStyle><p><r>`+tc.run+`<t>Text</t></r></p></txBody></sp>`), 32)
			if err != nil {
				t.Fatal(err)
			}
			paragraphs, err := parseTextParagraphs(n, r, &inheritedShape{object: Object{FontFace: "Inherited", FontSize: 24}})
			if err != nil || paragraphs[0].Runs[0].FontFace != tc.want {
				t.Fatal("incorrect effective run family", paragraphs, err)
			}
			face, _, _, _, _, _ := textProperties(n, r)
			if tc.index != "none" && face != tc.want {
				t.Fatal("object/run font disagreement", face, tc.want)
			}
		})
	}
}

func TestThemeOverridesComposeWithoutCrossSlideMutation(t *testing.T) {
	theme := Theme{Name: "Base", Dark1: "#111111", Light1: "#ffffff", Dark2: "#222222", Light2: "#eeeeee", Accent: [6]string{"#aa1100", "#bb2200", "#cc3300", "#dd4400", "#ee5500", "#ff6600"}, Hyperlink: "#123456", FollowedHyperlink: "#654321", HeadingFont: "Arial", BodyFont: "Courier New"}
	var out bytes.Buffer
	o := Object{ID: "t", Kind: "text", Width: 200, Height: 100, Text: "Text", FontFace: "+mj-lt", FontSize: 24, Color: "#111111"}
	if _, err := Write(context.Background(), &out, Presentation{Theme: &theme, Slides: []Slide{{Objects: []Object{o}}, {Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for key, value := range packageParts(t, out.Bytes()) {
		parts[key] = string(value)
	}
	for _, name := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml"} {
		parts[name] = strings.ReplaceAll(parts[name], `<a:srgbClr val="111111"/>`, `<a:schemeClr val="accent1"/>`)
	}
	section := func(xml, tag string) string {
		start, end := strings.Index(xml, "<a:"+tag+" "), strings.Index(xml, "</a:"+tag+">")
		if start < 0 || end < 0 {
			t.Fatal("missing fixture section", tag)
		}
		return xml[start : end+len("</a:"+tag+">")]
	}
	changed := theme
	changed.HeadingFont, changed.Accent[0] = "Go Mono", "#00bb11"
	changedXML := themeXML(&changed)
	wrap := func(content string) string {
		return `<a:themeOverride xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` + content + `</a:themeOverride>`
	}
	parts["ppt/theme/layoutOverride.xml"] = wrap(section(changedXML, "fontScheme"))
	parts["ppt/theme/slideOverride.xml"] = wrap(section(changedXML, "clrScheme"))
	addRel := func(owner, target string) {
		parts[owner] = strings.Replace(parts[owner], `</Relationships>`, `<Relationship Id="override" Type="`+officeRelationshipNamespace+`/themeOverride" Target="`+target+`"/></Relationships>`, 1)
	}
	addRel("ppt/slideLayouts/_rels/slideLayout1.xml.rels", "../theme/layoutOverride.xml")
	addRel("ppt/slides/_rels/slide2.xml.rels", "../theme/slideOverride.xml")
	read := func(parts map[string]string) (Presentation, error) {
		data := testPPTX(t, parts)
		p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
		return p, err
	}
	for range 5 {
		p, err := read(parts)
		if err != nil {
			t.Fatal(err)
		}
		if p.Theme == nil || p.Theme.Name != "Base" || p.Theme.HeadingFont != "Arial" {
			t.Fatal("local override changed document theme", p.Theme)
		}
		if p.Theme.SourcePart != "ppt/theme/theme1.xml" {
			t.Fatal("override replaced declared theme owner")
		}
		for i, colour := range []string{"#AA1100", "#00BB11"} {
			got := p.Slides[i].Objects[0]
			if got.Color != colour || got.FontFace != "Go Mono" || got.Paragraphs[0].Runs[0].FontFace != "Go Mono" {
				t.Fatal("incorrect composed override", i, got)
			}
			colourOwner := []string{"ppt/theme/theme1.xml", "ppt/theme/slideOverride.xml"}[i]
			run := got.Paragraphs[0].Runs[0]
			if got.ColorThemeSource != colourOwner || run.ColorThemeSource != colourOwner || got.FontThemeSource != "ppt/theme/layoutOverride.xml" || run.FontThemeSource != "ppt/theme/layoutOverride.xml" {
				t.Fatal("independent scheme owner lost", i, got, run)
			}
		}
	}
	valid := parts["ppt/theme/slideOverride.xml"]
	// Reset the mapping alone, retaining independent local font/colour schemes.
	layoutSource := parts["ppt/slideLayouts/slideLayout1.xml"]
	parts["ppt/slideLayouts/slideLayout1.xml"] = strings.Replace(layoutSource, `<a:masterClrMapping/>`, `<a:overrideClrMapping accent1="accent2"/>`, 1)
	if parts["ppt/slideLayouts/slideLayout1.xml"] == layoutSource {
		t.Fatal("fixture layout map not found")
	}
	parts["ppt/slides/slide2.xml"] = strings.Replace(parts["ppt/slides/slide2.xml"], `</p:sld>`, `<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`, 1)
	p, err := read(parts)
	if err != nil {
		t.Fatal(err)
	}
	if p.Slides[0].Objects[0].Color != "#BB2200" || p.Slides[1].Objects[0].Color != "#00BB11" || p.Slides[1].Objects[0].FontFace != "Go Mono" {
		t.Fatal("master map reset lost scope or local theme scheme", p.Slides)
	}
	validSlide := parts["ppt/slides/slide2.xml"]
	for _, badMap := range []string{`<a:masterClrMapping/><a:overrideClrMapping accent1="accent2"/>`, ``, `<a:masterClrMapping/><a:masterClrMapping/>`} {
		parts["ppt/slides/slide2.xml"] = strings.Replace(validSlide, `<a:masterClrMapping/>`, badMap, 1)
		if _, err := read(parts); err == nil {
			t.Fatal("ambiguous/empty colour map accepted")
		}
	}
	parts["ppt/slides/slide2.xml"] = validSlide
	for _, invalid := range []string{`<theme/>`, wrap(section(changedXML, "clrScheme") + section(changedXML, "clrScheme"))} {
		parts["ppt/theme/slideOverride.xml"] = invalid
		if _, err := read(parts); err == nil {
			t.Fatal("invalid override accepted")
		}
	}
	parts["ppt/theme/slideOverride.xml"] = valid
	addRel("ppt/slides/_rels/slide2.xml.rels", "../theme/slideOverride.xml")
	if _, err := read(parts); err == nil {
		t.Fatal("ambiguous theme override accepted")
	}
}
