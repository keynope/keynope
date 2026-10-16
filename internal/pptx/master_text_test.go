package pptx

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestMasterTextLevelsAndPlaceholderCascade(t *testing.T) {
	parse := func(source string) *node {
		n, err := parseXML([]byte(source), 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	r := defaultColourResolver()
	r.majorFont, r.minorFont = "Major", "Minor"
	r.colours["accent1"] = "#123456"
	r.textStyles = parse(`<txStyles><titleStyle><lvl1pPr><defRPr sz="4400"><latin typeface="+mj-lt"/></defRPr></lvl1pPr></titleStyle><bodyStyle><lvl1pPr><defRPr sz="2400" b="1"><solidFill><schemeClr val="accent1"/></solidFill><latin typeface="+mn-lt"/></defRPr></lvl1pPr><lvl2pPr><defRPr sz="1800" b="0"><latin typeface="+mn-lt"/></defRPr></lvl2pPr></bodyStyle><otherStyle><lvl1pPr><defRPr sz="2000"/></lvl1pPr></otherStyle></txStyles>`)
	master := parse(`<sp><txBody><lstStyle><lvl1pPr><defRPr i="1"/></lvl1pPr><lvl2pPr><defRPr sz="1700"/></lvl2pPr></lstStyle></txBody></sp>`)
	layout := parse(`<sp><txBody><lstStyle><lvl2pPr><defRPr sz="1600"/></lvl2pPr></lstStyle></txBody></sp>`)
	n := parse(`<sp><nvSpPr><nvPr><ph type="body"/></nvPr></nvSpPr><txBody><lstStyle><lvl1pPr><defRPr sz="2200"/></lvl1pPr></lstStyle><p><r><t>One</t></r></p><p><pPr lvl="1"/><r><t>Two</t></r></p><p><pPr lvl="1"><defRPr sz="1500"/></pPr><r><rPr sz="1400" b="1"/><t>Three</t></r></p></txBody></sp>`)
	parent := &inheritedShape{textSources: []*node{master, layout}}
	paragraphs, err := parseTextParagraphs(n, r, parent)
	if err != nil {
		t.Fatal(err)
	}
	for i, size := range []float64{22, 16, 14} {
		got := paragraphs[i].Runs[0]
		if got.FontSize != size || got.FontFace != "Minor" {
			t.Fatal("wrong text cascade", i, got)
		}
	}
	if !paragraphs[0].Runs[0].Bold || !paragraphs[0].Runs[0].Italic || paragraphs[0].Runs[0].Color != "#123456" || paragraphs[1].Runs[0].Bold || !paragraphs[2].Runs[0].Bold {
		t.Fatal("present/absent emphasis precedence lost", paragraphs)
	}
	for _, tc := range []struct {
		kind, face string
		size       float64
	}{{"title", "Major", 44}, {"ctrTitle", "Major", 44}, {"body", "Minor", 24}, {"subTitle", "Minor", 24}, {"obj", "Arial", 20}} {
		source := parse(fmt.Sprintf(`<sp><nvSpPr><nvPr><ph type="%s"/></nvPr></nvSpPr><txBody><p><r><t>Text</t></r></p></txBody></sp>`, tc.kind))
		p, err := parseTextParagraphs(source, r, nil)
		if err != nil || p[0].Runs[0].FontFace != tc.face || p[0].Runs[0].FontSize != tc.size {
			t.Fatal("wrong master style role", tc, p, err)
		}
	}
	for _, level := range []string{"-1", "9", "NaN"} {
		source := parse(`<sp><txBody><p><pPr lvl="` + level + `"/><r><t>X</t></r></p></txBody></sp>`)
		if _, err := parseTextParagraphs(source, r, nil); err == nil {
			t.Fatal("invalid paragraph level accepted", level)
		}
	}
	parent.object.Bold, parent.object.Italic = true, true
	off := parse(`<sp><nvSpPr><nvPr><ph type="body"/></nvPr></nvSpPr><txBody><p><pPr lvl="1"><defRPr b="0" i="0"/></pPr><r><t>Off</t></r></p></txBody></sp>`)
	object, err := parseShape(off, nil, r, parent, false)
	if err != nil || object == nil || object.Bold || object.Italic || object.FontSize != 16 || object.Paragraphs[0].Runs[0].Bold || object.Paragraphs[0].Runs[0].Italic {
		t.Fatal("object fallback defeated explicit Off", object, err)
	}
}

func TestReadMasterTextDefaultsInProductionPackage(t *testing.T) {
	var out bytes.Buffer
	o := Object{ID: "text", Kind: "text", Width: 300, Height: 100, Text: "Master text", FontFace: "Arial", FontSize: 24, Color: "#111111"}
	if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, value := range packageParts(t, out.Bytes()) {
		parts[name] = string(value)
	}
	parts["ppt/slideMasters/slideMaster1.xml"] = strings.Replace(parts["ppt/slideMasters/slideMaster1.xml"], `</p:sldMaster>`, `<p:txStyles><p:otherStyle><a:lvl1pPr algn="ctr"><a:lnSpc><a:spcPct val="125000"/></a:lnSpc><a:spcBef><a:spcPts val="1650"/></a:spcBef><a:buChar char="•"/><a:defRPr sz="3300" b="1"><a:solidFill><a:srgbClr val="AABBCC"/></a:solidFill><a:latin typeface="Courier New"/></a:defRPr></a:lvl1pPr></p:otherStyle></p:txStyles></p:sldMaster>`, 1)
	slide := parts["ppt/slides/slide1.xml"]
	for _, explicit := range []string{`<a:latin typeface="Arial"/>`, ` sz="2400"`, ` b="0"`, ` algn="l"`, `<a:buNone/>`, `<a:solidFill><a:srgbClr val="111111"/></a:solidFill>`} {
		slide = strings.ReplaceAll(slide, explicit, "")
	}
	parts["ppt/slides/slide1.xml"] = slide
	data := testPPTX(t, parts)
	p, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := p.Slides[0].Objects[0]
	if got.FontSize != 33 || got.FontFace != "Courier New" || got.Color != "#AABBCC" || !got.Bold || got.Paragraphs[0].Runs[0].FontFace != "Courier New" || got.Paragraphs[0].Runs[0].FontSize != 33 {
		t.Fatal("master defaults absent from editable object", got)
	}
	paragraph := got.Paragraphs[0]
	if got.TextAlign != "center" || paragraph.Align != "center" || !paragraph.Bullet || paragraph.LineHeight != 1.25 || paragraph.SpaceBefore != .5 {
		t.Fatal("master paragraph defaults lost", got)
	}
	var again bytes.Buffer
	if _, err := Write(context.Background(), &again, p); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := Read(context.Background(), bytes.NewReader(again.Bytes()), int64(again.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := reopened.Slides[0].Objects[0].Paragraphs[0]
	if roundTrip.Align != paragraph.Align || roundTrip.Bullet != paragraph.Bullet || roundTrip.LineHeight != paragraph.LineHeight || roundTrip.SpaceBefore != paragraph.SpaceBefore {
		t.Fatal("resolved paragraph style not portable", roundTrip)
	}
}

func TestParagraphDefaultsOverlayIndependently(t *testing.T) {
	parse := func(xml string) *node {
		n, err := parseXML([]byte(xml), 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	r := defaultColourResolver()
	r.textStyles = parse(`<txStyles><bodyStyle><lvl1pPr algn="ctr"><lnSpc><spcPct val="150000"/></lnSpc><spcBef><spcPct val="50000"/></spcBef><buChar char="•"/></lvl1pPr><lvl2pPr algn="r"><buNone/></lvl2pPr></bodyStyle></txStyles>`)
	parent := &inheritedShape{textSources: []*node{parse(`<sp><txBody><lstStyle><lvl1pPr><spcAft><spcPct val="75000"/></spcAft></lvl1pPr></lstStyle></txBody></sp>`)}}
	source := parse(`<sp><nvSpPr><nvPr><ph type="body"/></nvPr></nvSpPr><txBody><lstStyle><lvl1pPr><lnSpc><spcPct val="125000"/></lnSpc></lvl1pPr></lstStyle><p><r><t>Inherited</t></r></p><p><pPr algn="l"><spcBef><spcPts val="0"/></spcBef><spcAft><spcPct val="0"/></spcAft><buNone/></pPr><r><t>Explicit</t></r></p><p><pPr lvl="1"/><r><t>Level two</t></r></p></txBody></sp>`)
	p, err := parseTextParagraphs(source, r, parent)
	if err != nil {
		t.Fatal(err)
	}
	if p[0].Align != "center" || !p[0].Bullet || p[0].LineHeight != 1.25 || p[0].SpaceBefore != .5 || p[0].SpaceAfter != .75 {
		t.Fatal("cascade missing inherited properties", p[0])
	}
	if p[1].Align != "start" || p[1].Bullet || p[1].SpaceBefore != 0 || p[1].SpaceAfter != 0 || p[1].LineHeight != 1.25 {
		t.Fatal("explicit reset did not beat inheritance", p[1])
	}
	if p[2].Align != "end" || p[2].Bullet || p[2].SpaceBefore != 0 {
		t.Fatal("levels bled into one another", p[2])
	}
	bad := parse(`<sp><txBody><p><pPr><buNone/><buChar char="•"/></pPr><r><t>X</t></r></p></txBody></sp>`)
	if _, err := parseTextParagraphs(bad, r, nil); err == nil {
		t.Fatal("ambiguous bullet choices accepted")
	}
}
