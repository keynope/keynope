package pptx

import (
	"bytes"
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestNativeTextParagraphsRemainEditable(t *testing.T) {
	paragraphs := []TextParagraph{
		{Align: "center", LineHeight: 1.25, SpaceBefore: .5, SpaceAfter: .75, Runs: []TextRun{
			{Text: "Mono ", FontFace: "Courier New", FontSize: 40, Color: "#FF0055", Bold: true},
			{Text: "normal\nline", FontFace: "Arial", FontSize: 30, Color: "#123456", Italic: true, Underline: true},
		}},
		{Align: "center", LineHeight: 1.25},
		{Align: "end", Bullet: true, Runs: []TextRun{{Text: "C64", FontFace: "Keynope C64", FontSize: 36, Color: "#123456"}}},
	}
	object := Object{ID: "words", Kind: "text", X: 20, Y: 30, Width: 1500, Height: 900, FontFace: "Arial", FontSize: 30, Color: "#123456", TextAlign: "center", VerticalAlign: "bottom", Paragraphs: paragraphs, Text: paragraphPlainText(paragraphs), Bold: true}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{object}}}}); err != nil {
		t.Fatal(err)
	}
	xml := string(packageParts(t, output.Bytes())["ppt/slides/slide1.xml"])
	for _, want := range []string{`<a:br/>`, `<a:buChar char="•"/>`, `<a:lnSpc><a:spcPct val="125000"/>`, `<a:spcBef><a:spcPts val="1500"/>`, `<a:spcAft><a:spcPts val="2250"/>`, `u="sng"`, `typeface="Courier New"`} {
		if !strings.Contains(xml, want) {
			t.Fatal("missing editable native text", want, xml)
		}
	}
	got, _, err := Read(context.Background(), bytes.NewReader(output.Bytes()), int64(output.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	read := got.Slides[0].Objects[0]
	if read.Text != object.Text || read.FontFace != "Arial" || read.FontSize != 30 || read.Color != "#123456" || read.TextAlign != "center" || read.VerticalAlign != "bottom" {
		t.Fatal("first word overwrote the text box defaults", read)
	}
	if len(read.Paragraphs) != 3 || read.Paragraphs[0].SpaceBefore != .5 || read.Paragraphs[0].SpaceAfter != .75 || read.Paragraphs[0].LineHeight != 1.25 || !read.Paragraphs[2].Bullet {
		t.Fatal("paragraph structure lost", read.Paragraphs)
	}
	runs := read.Paragraphs[0].Runs
	if len(runs) != 4 || runs[0] != paragraphs[0].Runs[0] || runs[1].Text != "normal" || runs[1].Bold || !runs[1].Underline || !runs[1].Italic || runs[2].Text != "\n" || runs[3].Text != "line" {
		t.Fatal("run/line-break formatting lost (including explicit bold Off)", runs)
	}
	if !reflect.DeepEqual(read.Paragraphs[2], paragraphs[2]) {
		t.Fatal("bullet word typography lost", read.Paragraphs[2])
	}
}

func TestNativeTextRejectsInvalidStyleBeforeWriting(t *testing.T) {
	valid := Object{ID: "text", Kind: "text", Width: 400, Height: 80, Text: "Word", FontSize: 20, Paragraphs: []TextParagraph{{Runs: []TextRun{{Text: "Word"}}}}}
	for name, mutate := range map[string]func(*Object){
		"control":             func(o *Object) { o.Text = "bad\x00" },
		"utf8":                func(o *Object) { o.Text = string([]byte{0xff}) },
		"mismatch":            func(o *Object) { o.Text = "Different" },
		"face":                func(o *Object) { o.Paragraphs[0].Runs[0].FontFace = "bad\x01" },
		"size":                func(o *Object) { o.Paragraphs[0].Runs[0].FontSize = math.Inf(1) },
		"subpoint":            func(o *Object) { o.Paragraphs[0].Runs[0].FontSize = .5 },
		"spacing":             func(o *Object) { o.Paragraphs[0].LineHeight = math.NaN() },
		"point-spacing-limit": func(o *Object) { o.FontSize = 2000; o.Paragraphs[0].SpaceAfter = 4 },
		"align":               func(o *Object) { o.Paragraphs[0].Align = "upside-down" },
		"color":               func(o *Object) { o.Paragraphs[0].Runs[0].Color = "none" },
		"limit":               func(o *Object) { o.Paragraphs[0].Runs = make([]TextRun, 10001) },
	} {
		t.Run(name, func(t *testing.T) {
			o := valid
			o.Paragraphs = []TextParagraph{{Runs: []TextRun{{Text: "Word"}}}}
			mutate(&o)
			var output bytes.Buffer
			if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{valid, o}}}}); err == nil || output.Len() != 0 {
				t.Fatal("invalid rich text wrote partial output", err, output.Len())
			}
		})
	}
}

func TestNativeTextImportRejectsOversizedOrInvalidRuns(t *testing.T) {
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{ID: "body", Kind: "text", Width: 400, Height: 100, Text: "Word", FontSize: 30, Paragraphs: []TextParagraph{{Runs: []TextRun{{Text: "Word", FontSize: 40}}}}}}}}}); err != nil {
		t.Fatal(err)
	}
	parts := packageParts(t, out.Bytes())
	for name, transform := range map[string]func(string) string{
		"non-finite": func(s string) string { return strings.Replace(s, `sz="4000"`, `sz="+Inf"`, 1) },
		"sub-point":  func(s string) string { return strings.Replace(s, `sz="4000"`, `sz="50"`, 1) },
		"run-limit": func(s string) string {
			return strings.Replace(s, `</a:p>`, strings.Repeat(`<a:r><a:t/></a:r>`, 10001)+`</a:p>`, 1)
		},
		"content-limit": func(s string) string {
			return strings.Replace(s, `<a:t>Word</a:t>`, `<a:t>`+strings.Repeat("x", 100001)+`</a:t>`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			modified := map[string]string{}
			for part, data := range parts {
				modified[part] = string(data)
			}
			modified["ppt/slides/slide1.xml"] = transform(modified["ppt/slides/slide1.xml"])
			data := testPPTX(t, modified)
			if _, _, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{}); err == nil {
				t.Fatal("invalid runs silently flattened/imported")
			}
		})
	}
}

func TestNativeTextWordOffOverridesInheritedDefaults(t *testing.T) {
	root, err := parseXML([]byte(`<p:sp xmlns:p="p" xmlns:a="a"><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:rPr b="0" i="0"/><a:t>Not emphasized</a:t></a:r><a:r><a:t> Inherit</a:t></a:r></a:p></p:txBody></p:sp>`), 128)
	if err != nil {
		t.Fatal(err)
	}
	parent := &inheritedShape{object: Object{FontFace: "Courier New", FontSize: 30, Bold: true, Italic: true, Color: "#123456"}}
	o, err := parseShape(root, nil, colourResolver{}, parent, false)
	if err != nil {
		t.Fatal(err)
	}
	runs := o.Paragraphs[0].Runs
	if runs[0].Bold || runs[0].Italic || !runs[1].Bold || !runs[1].Italic || runs[0].FontFace != "Courier New" || runs[0].FontSize != 30 {
		t.Fatal("explicit Off lost to inherited emphasis", runs)
	}
}
