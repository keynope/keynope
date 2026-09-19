package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestTrueTypeMeasurementSharedQuery(t *testing.T) {
	for _, query := range []string{
		"render=truetype&width=30&height=3&top=4&left=8",
		"render=truetype&ttf-size=120&ttf-width=50&align=center",
		"render=truetype&text-box=1&width=30.5&height=5.25&right=2&bottom=3",
		"render=truetype&width=bad&height=bad&fg=%2355aaff",
	} {
		e := Element{Kind: "text", Text: "Hello 😍\nSecond line", Query: query}
		q, _ := url.ParseQuery(query)
		before := q.Encode()
		wantW, wantH := trueTypeBounds(e, 245, 56)
		w, h := trueTypeBoundsFromValues(e, 245, 56, q)
		if w != wantW || h != wantH || !reflect.DeepEqual(trueTypeLayoutRows(e, 245, 56), trueTypeLayoutRowsFromValues(e, 245, 56, q)) {
			t.Fatalf("measurement differs for %q", query)
		}
		if q.Encode() != before {
			t.Fatal("measurement mutated shared placement metadata")
		}
	}
}

func TestPlacementFromValuesRetainsAnchorPrecedence(t *testing.T) {
	q, err := url.ParseQuery("align=center&valign=middle&top=-2&bottom=3&left=10&left_pct=0.25&right=8&width=40&height=7")
	if err != nil {
		t.Fatal(err)
	}
	before := q.Encode()
	p := imagePlacementFromValues(q)
	if !p.hasVerticalOffset() || !p.hasHorizontalOffset() || placementTopRow(p, 56, 7, 10) != -2 || placementLeftCol(p, 101, 40) != 25 {
		t.Fatal("explicit anchors lost precedence", p)
	}
	if q.Encode() != before {
		t.Fatal("placement modified shared metadata")
	}
}

func TestLayoutMalformedPlacementRejectsAnchors(t *testing.T) {
	// Historically placement rejects malformed queries as a whole, while
	// text-box dimensions still consume the valid portion. Preserve both.
	query := "top=20&left=30&text-box=1&width=12&height=4&bad=%zz"
	p := parseImagePlacement(query)
	if p.hasHorizontalOffset() || p.hasVerticalOffset() {
		t.Fatal("malformed placement accepted anchors")
	}
	lines := layout(Slide{Elements: []Element{{Kind: "text", Text: "X", Query: query}}}, 80, 56)
	if len(lines) == 0 || lines[0].Row != 0 || lines[0].Col != 0 {
		t.Fatal("layout accepted invalid placement", lines)
	}
}
