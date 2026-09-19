package main

import "testing"

func TestConnectorFreeProjectionDoesNotAllocate(t *testing.T) {
	slide := Slide{Elements: []Element{{ID: "shape", Kind: "shape", Query: "shape=circle&width=20&height=10"}}}
	lines := []Line{{Role: "shape", Element: 0, Query: slide.Elements[0].Query}}
	allocations := testing.AllocsPerRun(100, func() {
		if got := slideShapeConnectors(slide, lines, 245, 56); got != nil {
			t.Fatal("connector-free slide produced routes")
		}
	})
	if allocations != 0 {
		t.Fatalf("connector-free projection allocated %g times", allocations)
	}
	// Adding a dangling reference must still be inert, not a fabricated route.
	slide.Elements = append(slide.Elements, Element{ID: "line", Kind: "connector", Query: "connector-from=shape&connector-from-side=right&connector-to=missing&connector-to-side=left"})
	if got := slideShapeConnectors(slide, lines, 245, 56); len(got) != 0 {
		t.Fatal("dangling connector produced a route", got)
	}
}
