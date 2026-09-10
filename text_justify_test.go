package main

import "testing"

func TestJustifyUsesBoxAndKeepsShortLines(t *testing.T) {
	e := Element{Kind: "text", Text: "AAA AAA AA\nAA A AA\nA A AA", Query: "align=justify&width=60"}
	rows := renderElementRows(e, 245)
	h := len(renderQuadStyled([]styledTextSpan{{Text: "M"}}))
	if len(rows) != 3*h {
		t.Fatalf("expected three lines, got %d rows", len(rows))
	}
	for i, want := range []int{60, 60, 24} {
		if got := maxLineDisplayWidth(rows[i*h : (i+1)*h]); got != want {
			t.Errorf("line %d width=%d want=%d", i, got, want)
		}
	}
}

func TestJustifyWrapsAndPreservesFontMetadata(t *testing.T) {
	e := Element{Kind: "text", Text: "one two three four five six seven eight", Query: "align=justify&width=50&render=text-image&source=bitmap&scale=1&text-size=5"}
	original := e
	rows := renderElementRows(e, 245)
	if maxLineDisplayWidth(rows) != 50 {
		t.Fatalf("justification did not reach the box width: %d", maxLineDisplayWidth(rows))
	}
	if e.Text != original.Text || e.Query != original.Query {
		t.Fatal("rendering modified authored text or font size")
	}
}
