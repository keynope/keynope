package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSceneLineIndexPreservesObjectArtwork(t *testing.T) {
	deck := sceneFixture(t)
	slide := withTrueTypeDefaults(deck.ResolveSlide(0, false))
	lines := layout(slide, 245, 56)
	indexed := sceneLinesByElement(lines)
	for i := range slide.Elements {
		want := retroObjectLines(deck, slide, lines, i, 245, 56)
		got := retroObjectLines(deck, slide, indexed[i], i, 245, 56)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("element %d artwork differs", i)
		}
	}
	if len(indexed[-999]) != 0 {
		t.Fatal("missing element returned rows")
	}
	interleaved := []Line{{Element: 2, Text: "first"}, {Element: 1, Text: "other"}, {Element: 2, Text: "second", Role: "shape-label"}}
	if got := sceneLinesByElement(interleaved)[2]; !reflect.DeepEqual(got, []Line{interleaved[0], interleaved[2]}) {
		t.Fatal("index changed row order or label membership")
	}
}

func BenchmarkSceneLineOwnership(b *testing.B) {
	for _, count := range []int{100, 500, 1000} {
		lines := make([]Line, count*20)
		for i := range lines {
			lines[i] = Line{Element: i / 20, Text: "sampled artwork"}
		}
		for _, indexed := range []bool{false, true} {
			b.Run(fmt.Sprintf("objects=%d/indexed=%t", count, indexed), func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					seen := 0
					if indexed {
						groups := sceneLinesByElement(lines)
						for i := 0; i < count; i++ {
							seen += len(groups[i])
						}
					} else {
						for i := 0; i < count; i++ {
							var own []Line
							for _, line := range lines {
								if line.Element == i {
									own = append(own, line)
								}
							}
							seen += len(own)
						}
					}
					if seen != len(lines) {
						b.Fatal("lost rows")
					}
				}
			})
		}
	}
}
