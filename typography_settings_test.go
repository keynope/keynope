package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestSlideTrueTypeDefaults(t *testing.T) {
	d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank", Elements: []Element{{Kind: "text", Text: "Default", Query: "render=truetype"}, {Kind: "heading", Level: 2, Text: "Header", Query: "render=truetype"}, {Kind: "text", Text: "Override", Query: "render=truetype&ttf-size=77&ttf-width=80"}}}}}
	d.Masters.Base.Slide.TTFSize = 194
	d.Masters.Base.Slide.TTFWidth = 50
	find := func(s Slide, text string) Element {
		for _, e := range s.Elements {
			if e.Text == text {
				return e
			}
		}
		t.Fatalf("missing %s", text)
		return Element{}
	}
	check := func(wantSize int, wantWidth float64) {
		t.Helper()
		resolved := d.ResolveSlide(0, false)
		rendered := withTrueTypeDefaults(resolved)
		if trueTypeSize(find(rendered, "Default")) != wantSize || trueTypeWidthPercent(find(rendered, "Default")) != wantWidth {
			t.Fatalf("defaults not resolved: %#v", rendered)
		}
		if trueTypeSize(find(rendered, "Override")) != 77 || trueTypeWidthPercent(find(rendered, "Override")) != 80 {
			t.Fatal("override lost")
		}
		if strings.Contains(find(resolved, "Default").Query, "ttf-size") || strings.Contains(find(d.Slides[0], "Default").Query, "ttf-width") {
			t.Fatal("render defaults leaked into source")
		}
	}
	check(194, 50)
	if trueTypeSize(find(withTrueTypeDefaults(d.ResolveSlide(0, false)), "Header")) != 386 {
		t.Fatal("heading default did not scale")
	}
	d.Slides[0].TTFSize = 97
	d.Slides[0].TTFWidth = 125
	check(97, 125)
	data, err := serializeDeck("test.md", d)
	if err != nil {
		t.Fatal(err)
	}
	d, err = parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	check(97, 125)
	if find(d.ResolveSlide(0, false), "Header").Level != 2 {
		t.Fatal("TTF H2 level lost on reload")
	}
	d.Slides[0].TTFSize = 0
	d.Slides[0].TTFWidth = 0
	check(194, 50)
}

func TestTextAndBoxAlignmentAreIndependent(t *testing.T) {
	e := Element{Kind: "text", Text: "AB", Query: "text-box=1&width=40&height=20&left=5&top=2&text-align=right&text-valign=bottom"}
	base := e
	base.Query = "width=40"
	plain := renderElementRows(base, 245)
	aligned := renderElementRows(e, 245)
	if len(aligned) != 20 || maxLineDisplayWidth(aligned) != 40 {
		t.Fatal("box dimensions changed")
	}
	dy := 20 - len(plain)
	dx := 40 - maxLineDisplayWidth(plain)
	for i, row := range plain {
		if !strings.HasPrefix(aligned[dy+i], strings.Repeat(" ", dx)+row) {
			t.Fatalf("glyph distorted on row %d", i)
		}
	}
	r, c := renderedOffsetForCursor(base, 245, 1)
	ar, ac := renderedOffsetForCursor(e, 245, 1)
	if ar != r+dy || ac != c+dx {
		t.Fatalf("caret mismatch: %d,%d vs %d,%d", ar, ac, r+dy, c+dx)
	}
	data, err := serializeDeck("test.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(d.Slides[0].Elements[0].Query)
	if q.Get("text-align") != "right" || q.Get("text-valign") != "bottom" || q.Get("left") != "5" || q.Get("top") != "2" {
		t.Fatal("lost independent positioning")
	}
}

func TestTrueTypeHeadingKeepsExplicitStyle(t *testing.T) {
	for _, colour := range []string{"", "&fg=%23ff5500"} {
		e := Element{Kind: "text", Text: "Title", Query: "render=truetype&ttf-width=73&ttf-weight=bold&gradient-start=%23ffffff&gradient-end=%230000ff&shadow=soft&outline=dark" + colour}
		for _, level := range []int{1, 2} {
			converted := convertedTextKindElement(e, "heading", level)
			q, _ := url.ParseQuery(converted.Query)
			for _, key := range []string{"ttf-width", "ttf-weight", "gradient-start", "gradient-end", "shadow", "outline"} {
				original, _ := url.ParseQuery(e.Query)
				if q.Get(key) != original.Get(key) {
					t.Fatalf("lost %s", key)
				}
			}
			if (q.Get("header") == "") != (colour == "") {
				t.Fatal("changed colour inheritance")
			}
		}
	}
}
