package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestShapeHalfCellEdges(t *testing.T) {
	for _, test := range []struct {
		query string
		rows  []string
	}{
		{"width=2&height=2", []string{"██", "██"}},
		{"width=2.5&height=1.5", []string{"██▌", "▀▀▘"}},
		{"width=2&height=1&shape-offset-x=0.5&shape-offset-y=0.5", []string{"▗▄▖", "▝▀▘"}},
		{"width=0.5&height=0.5", []string{"▘"}},
	} {
		e := Element{Kind: "shape", Query: "shape=square&" + test.query}
		got := renderShapeRows(e, 100)
		if !reflect.DeepEqual(got, test.rows) {
			t.Fatalf("%s: %q != %q", test.query, got, test.rows)
		}
	}
}

func TestShapeFractionalSizeRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	e := Element{Kind: "shape", Query: "shape=circle&top=4&left=3&width=20.5&height=10.5&shape-offset-x=0.5&shape-offset-y=0.5&transparent=1&outline=dark"}
	data, err := serializeDeck("shape.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
	if err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeckData("shape.md", data)
	if err != nil {
		t.Fatal(err)
	}
	got := deck.Slides[0].Elements[0]
	before, _ := url.ParseQuery(e.Query)
	after, _ := url.ParseQuery(got.Query)
	for key := range before {
		if before.Get(key) != after.Get(key) {
			t.Fatalf("%s lost: %s", key, got.Query)
		}
	}
	if !reflect.DeepEqual(renderShapeRows(e, 245), renderShapeRows(got, 245)) {
		t.Fatal("render changed after reload")
	}
}

func TestCurvedShapesUsePartialBlocks(t *testing.T) {
	for _, shape := range []string{"circle", "triangle", "diamond"} {
		rows := renderShapeRows(Element{Kind: "shape", Query: "shape=" + shape + "&width=10.5&height=6.5"}, 100)
		if !strings.ContainsAny(strings.Join(rows, ""), "▀▄▌▐▘▝▖▗▙▚▛▜▞▟") {
			t.Fatalf("%s has no partial edges: %q", shape, rows)
		}
	}
}

func TestShapeDimensionsScaleOnce(t *testing.T) {
	for _, query := range []string{"width=20&height=10", "width=21&height=11", "width=20.5&height=10.5"} {
		e := Element{Kind: "shape", Query: "shape=square&" + query}
		got, _ := url.ParseQuery(scaleElementForTerminal(e, .5, .5).Query)
		original, _ := url.ParseQuery(e.Query)
		for _, key := range []string{"width", "height"} {
			if shapeHalfCells(got, key, 1) != (shapeHalfCells(original, key, 1)+1)/2 {
				t.Fatalf("%s scaled twice: %v", query, got)
			}
		}
	}
}

func TestTransparentShapeKeepsPartialMask(t *testing.T) {
	e := Element{Kind: "shape", Query: "shape=square&width=2.5&height=1.5&transparent=1&fg=%2355aaff"}
	slide := Slide{Elements: []Element{e}}
	lines := roleLines(renderShapeRows(e, 100), "shape", 0, e.Query)
	exported := transparentShapeExportLines(lines, 100, 25, slide)
	var rows []string
	for _, line := range exported {
		var row string
		for _, part := range line.Parts {
			row += part.Text
		}
		rows = append(rows, row)
	}
	if !reflect.DeepEqual(rows, []string{"██▌", "▀▀▘"}) {
		t.Fatalf("transparent mask expanded partial edges: %q", rows)
	}
}
