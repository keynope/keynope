package pptx

import (
	"bytes"
	"context"
	"math"
	"reflect"
	"testing"
)

func TestTableDistinctEdgesNativeRoundTripAndCoveredPerimeter(t *testing.T) {
	table := tableFixture()
	edges := map[string]TableBorder{"left": {Color: "none"}, "right": {Color: "#FF0055", Width: 4}, "top": {Color: "#00FFAA", Width: 2}, "bottom": {Color: "#4455FF", Width: 1}}
	table.Cells[1][0].Borders = edges
	input := Presentation{Slides: []Slide{{Objects: []Object{{ID: "table", Kind: "table", Width: 480, Height: 370, Table: table}}}}}
	var encoded bytes.Buffer
	report, err := Write(context.Background(), &encoded, input)
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	output, report, err := Read(context.Background(), bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	got := output.Slides[0].Objects[0].Table
	if !reflect.DeepEqual(got.Cells[1][0].Borders, edges) {
		t.Fatal("distinct borders collapsed", got.Cells[1][0].Borders)
	}
	if got.Cells[1][1].Borders["right"] != edges["right"] || got.Cells[2][1].Borders["bottom"] != edges["bottom"] || got.Cells[2][0].Borders["left"] != edges["left"] {
		t.Fatal("covered perimeter missing", got.Cells)
	}
	if got.Cells[1][1].Borders["left"].Color != "none" || got.Cells[2][0].Borders["top"].Color != "none" {
		t.Fatal("merged interior is painted")
	}
	// A foreign file can assign a continuation-specific perimeter style.
	n, err := parseXML([]byte(tableXML(input.Slides[0].Objects[0], 2, "")), 128)
	if err != nil {
		t.Fatal(err)
	}
	rows := children(findFirst(n, "tbl"), "tr")
	cell := children(rows[2], "tc")[1]
	line := findChild(findChild(cell, "tcPr"), "lnB")
	color := findChild(findChild(line, "solidFill"), "srgbClr")
	color.Attr["val"] = "FFAABB"
	parsed, warnings, err := parseTableFrame(n, colourResolver{})
	if err != nil || len(warnings) != 0 || parsed.Table.Cells[2][1].Borders["bottom"].Color != "#FFAABB" {
		t.Fatal("foreign continuation paint lost", err, warnings)
	}
}

func TestTableInvalidEdgeStylesWriteNoBytes(t *testing.T) {
	for _, edge := range []TableBorder{{Color: "#ff0000", Width: -1}, {Color: "#ff0000", Width: math.NaN()}, {Color: "#ff0000", Width: math.Inf(1)}, {Color: "blue", Width: 1}} {
		for _, rc := range [][2]int{{0, 0}, {2, 1}} {
			table := tableFixture()
			table.Cells[rc[0]][rc[1]].Borders = map[string]TableBorder{"left": edge}
			var encoded bytes.Buffer
			_, err := Write(context.Background(), &encoded, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "table", Width: 480, Height: 370, Table: table}}}}})
			if err == nil || encoded.Len() != 0 {
				t.Fatal("invalid edge wrote output", rc, edge, err)
			}
		}
	}
}

func TestTableInvalidEdgeWidthsRejectImportEvenWhenOff(t *testing.T) {
	for _, width := range []string{"-1", "NaN", "+Inf", "not-a-number"} {
		for _, rc := range [][2]int{{0, 0}, {2, 1}} {
			for _, off := range []bool{false, true} {
				table := tableFixture()
				if off {
					table.Cells[rc[0]][rc[1]].Borders = map[string]TableBorder{"left": {Color: "none"}}
				}
				n, err := parseXML([]byte(tableXML(Object{ID: "table", Kind: "table", Width: 480, Height: 370, Table: table}, 2, "")), 128)
				if err != nil {
					t.Fatal(err)
				}
				row := children(findFirst(n, "tbl"), "tr")[rc[0]]
				cell := children(row, "tc")[rc[1]]
				findChild(findChild(cell, "tcPr"), "lnL").Attr["w"] = width
				if _, _, err := parseTableFrame(n, colourResolver{}); err == nil {
					t.Fatal("invalid border width accepted", rc, width, "off", off)
				}
			}
		}
	}
}
