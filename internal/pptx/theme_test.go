package pptx

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestNativeThemePaletteAndFonts(t *testing.T) {
	theme := &Theme{Name: `Research & "Design"`, Dark1: "#112233", Light1: "#fafbfc", Dark2: "#334455", Light2: "#eeeeee", Accent: [6]string{"#110000", "#220000", "#330000", "#440000", "#550000", "#660000"}, Hyperlink: "#123456", FollowedHyperlink: "#654321", HeadingFont: `Heading "Face"`, BodyFont: "Courier New"}
	before := *theme
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, Presentation{Theme: theme, Slides: []Slide{{Objects: []Object{{ID: "shape", Kind: "shape", Shape: "rect", Width: 100, Height: 100, Fill: "#00aa55"}}}}}); err != nil {
		t.Fatal(err)
	}
	parts := packageParts(t, out.Bytes())
	root, err := parseXML([]byte(parts["ppt/theme/theme1.xml"]), 128)
	if err != nil {
		t.Fatal(err)
	}
	if attr(root, "name") != theme.Name {
		t.Fatal("theme name not safely encoded", attr(root, "name"))
	}
	scheme := findFirst(root, "clrScheme")
	if len(scheme.Children) != 12 {
		t.Fatal("native palette must have 12 slots", len(scheme.Children))
	}
	for key, want := range map[string]string{"dk1": theme.Dark1, "lt1": theme.Light1, "dk2": theme.Dark2, "lt2": theme.Light2, "hlink": theme.Hyperlink, "folHlink": theme.FollowedHyperlink, "accent1": theme.Accent[0], "accent6": theme.Accent[5]} {
		got, _ := rawColourNode(firstColourChild(findChild(scheme, key)))
		if !strings.EqualFold(got, want) {
			t.Fatal(key, got, want)
		}
	}
	fonts := findFirst(root, "fontScheme")
	if attr(findFirst(findChild(fonts, "majorFont"), "latin"), "typeface") != theme.HeadingFont || attr(findFirst(findChild(fonts, "minorFont"), "latin"), "typeface") != theme.BodyFont {
		t.Fatal("font roles lost")
	}
	if !reflect.DeepEqual(before, *theme) || strings.Count(string(parts["ppt/theme/theme1.xml"]), "<a:effectStyle>") != 3 {
		t.Fatal("theme mutated or style-list schema damaged")
	}
}

func TestInvalidNativeThemeWritesNothing(t *testing.T) {
	valid := Theme{Name: "Valid", Dark1: "#000000", Light1: "#ffffff", Dark2: "#112233", Light2: "#eeeeee", Accent: [6]string{"#110000", "#220000", "#330000", "#440000", "#550000", "#660000"}, Hyperlink: "#123456", FollowedHyperlink: "#654321", HeadingFont: "Arial", BodyFont: "Arial"}
	for _, mutate := range []func(*Theme){func(v *Theme) { v.Name = "bad\x00name" }, func(v *Theme) { v.Name = strings.Repeat("x", 101) }, func(v *Theme) { v.Dark1 = "red" }, func(v *Theme) { v.Accent[5] = "#xxxxx0" }, func(v *Theme) { v.Hyperlink = "" }, func(v *Theme) { v.BodyFont = "" }, func(v *Theme) { v.HeadingFont = "bad\nface" }} {
		copy := valid
		mutate(&copy)
		var out bytes.Buffer
		if _, err := Write(context.Background(), &out, Presentation{Theme: &copy, Slides: []Slide{{}}}); err == nil || out.Len() != 0 {
			t.Fatal("invalid theme produced output", err, out.Len())
		}
	}
}
