package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNativeSolidBackgroundWriteRead(t *testing.T) {
	p := Presentation{Slides: []Slide{{Background: "#101820"}, {Background: "#Aa55Cc"}, {}}}
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, p); err != nil {
		t.Fatal(err)
	}
	loaded, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	for i, slide := range loaded.Slides {
		if !strings.EqualFold(slide.Background, p.Slides[i].Background) || len(slide.Objects) != 0 {
			t.Fatal("background became missing/selectable artwork", i, slide)
		}
	}
	parts := packageParts(t, out.Bytes())
	root, err := parseXML(parts["ppt/slides/slide1.xml"], 64)
	if err != nil {
		t.Fatal(err)
	}
	common := findChild(root, "cSld")
	if len(common.Children) != 2 || common.Children[0].Name != "bg" || common.Children[1].Name != "spTree" || findChild(findChild(common.Children[0], "bgPr"), "solidFill") == nil {
		t.Fatal("background must precede the shape tree as a native slide property")
	}
	blank, err := parseXML(parts["ppt/slides/slide3.xml"], 64)
	if err != nil || findFirst(blank, "bg") != nil {
		t.Fatal("unspecified background became an explicit fill", err)
	}
}

func TestInvalidSolidBackgroundRejectsBeforeWrite(t *testing.T) {
	for _, fill := range []string{"not-a-colour", "#12345", `#ffffff"/>`} {
		var out bytes.Buffer
		if _, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Background: fill}}}); err == nil || out.Len() != 0 {
			t.Fatal("invalid background produced package bytes", fill, err)
		}
	}
}
