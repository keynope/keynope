package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func archivedLayoutFixture() Slide {
	return Slide{TTFSize: 110, TTFWidth: 90, Elements: []Element{
		{ID: "text", Kind: "text", Text: "Hello", Query: "render=truetype"},
		{ID: "shape", Kind: "shape", Query: "shape=square&width=20&height=10&top=12"},
	}, EngagementResult: &EngagementResult{State: map[string]json.RawMessage{"ideas": json.RawMessage(`"` + strings.Repeat("x", 900000) + `"`)}}}
}

func TestLayoutLeavesArchiveAndAuthoredElementsUntouched(t *testing.T) {
	slide := archivedLayoutFixture()
	before := cloneSlide(slide)
	projected := withTrueTypeDefaults(slide)
	if projected.Elements[0].Query == slide.Elements[0].Query {
		t.Fatal("fixture did not exercise default projection")
	}
	if projected.EngagementResult != slide.EngagementResult {
		t.Fatal("read-only archive was copied during typography projection")
	}
	lines := layout(slide, 245, 56)
	if len(slideShapePorts(slide, lines, 245, 56)) != 4 {
		t.Fatal("fixture lost shape ports")
	}
	if !reflect.DeepEqual(slide, before) {
		t.Fatal("projection mutated authored slide")
	}
}

func BenchmarkLayoutActivityArchive(b *testing.B) {
	for _, archived := range []bool{false, true} {
		name := "without"
		if archived {
			name = "with"
		}
		b.Run(name, func(b *testing.B) {
			slide := archivedLayoutFixture()
			if !archived {
				slide.EngagementResult = nil
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := layout(slide, 245, 56)
				slideShapePorts(slide, lines, 245, 56)
			}
		})
	}
}
