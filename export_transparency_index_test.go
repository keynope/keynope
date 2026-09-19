package main

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestExportTransparencyIndexMatchesSuffixScan(t *testing.T) {
	slide := Slide{Elements: []Element{
		{Kind: "shape", Query: "transparent=1&fg=%23ff5500"},
		{Kind: "code", Query: "transparent=1&bg=%230055ff"},
		{Kind: "image", Query: "transparent=1&fg=%23aaff55"},
		{Kind: "text", Query: "fg=%23ffffff"},
	}}
	rng := rand.New(rand.NewSource(71))
	var lines []Line
	for i := 0; i < 250; i++ {
		e := rng.Intn(4)
		text := "█ ▀ ▄█"
		if e == 2 {
			text = "\033[38;2;255;0;128m█ ▄\033[0m █"
		}
		lines = append(lines, Line{Row: rng.Intn(15) - 2, Col: rng.Intn(25) - 3, Text: text, Element: e, Role: []string{"shape", "code", "image", "body"}[e], Query: slide.Elements[e].Query})
	}
	before := append([]Line(nil), lines...)
	index := indexExportTransparency(lines, 20, 10, slide)
	for i := -1; i < len(lines); i++ {
		want := transparentShapeCellsFrom(lines, i+1, 20, 10, slide)
		for row := -2; row < 13; row++ {
			got := index.after(i, row)
			if !reflect.DeepEqual(got[row], want[row]) {
				t.Fatalf("line %d row %d: %v != %v", i, row, got[row], want[row])
			}
		}
	}
	if !reflect.DeepEqual(lines, before) {
		t.Fatal("indexing mutated source rows")
	}
}

func BenchmarkExportTransparencySuffixes(b *testing.B) {
	slide := Slide{Elements: []Element{{Kind: "shape", Query: "transparent=1&fg=%23ff5500"}}}
	lines := make([]Line, 300)
	for i := range lines {
		lines[i] = Line{Row: i % 56, Col: i % 200, Element: 0, Role: "shape", Text: "████████"}
	}
	for _, indexed := range []bool{false, true} {
		name := "scan"
		if indexed {
			name = "indexed"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				var index exportTransparencyIndex
				if indexed {
					index = indexExportTransparency(lines, 245, 56, slide)
				}
				for i, line := range lines {
					var cells map[int]map[int]string
					if indexed {
						cells = index.after(i, line.Row)
					} else {
						cells = transparentShapeCellsFrom(lines, i+1, 245, 56, slide)
					}
					if i == len(lines)-1 && len(cells) != 0 {
						b.Fatal("unexpected final mask")
					}
				}
			}
		})
	}
}
