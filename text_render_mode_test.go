package main

import (
	"net/url"
	"testing"
)

func TestTextRenderModeMatchesQueryParser(t *testing.T) {
	queries := []string{
		"", "render=truetype", "top=4&render=truetype&width=32", "render=truetype&render=text-image",
		"render=&render=truetype", "render&render=truetype", "&&render=truetype&&", "render=truetype=extra",
		"%72ender=truetype", "render=true%74ype", "render=true+type", "render=truetype&bad=%xx",
		"render=truetype&bad=a;b", "bad=%ff&render=truetype", "RENDER=truetype", "notrender=truetype",
		"render=truetype&x=☃", "render=truetype&\x00=x",
	}
	for _, query := range queries {
		want := ""
		if values, err := url.ParseQuery(query); err == nil {
			want = values.Get("render")
		}
		if got := textRenderMode(query); got != want {
			t.Errorf("query %q: got %q, want %q", query, got, want)
		}
	}
}

func TestTextRenderModePlainQueryDoesNotAllocate(t *testing.T) {
	query := "element-style=retro&render=truetype&ttf-size=32&left=12&top=5&width=30&height=3"
	if allocations := testing.AllocsPerRun(100, func() { _ = textRenderMode(query) }); allocations != 0 {
		t.Fatalf("plain render lookup allocated %g times", allocations)
	}
}

func FuzzTextRenderModeMatchesQueryParser(f *testing.F) {
	for _, query := range []string{"render=truetype", "render=&render=text-image", "x=%zz", "render=truetype&bad=a;b"} {
		f.Add(query)
	}
	f.Fuzz(func(t *testing.T, query string) {
		want := ""
		if values, err := url.ParseQuery(query); err == nil {
			want = values.Get("render")
		}
		if got := textRenderMode(query); got != want {
			t.Fatalf("%q: got %q, want %q", query, got, want)
		}
	})
}
