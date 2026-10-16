package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math"
	"strings"
	"testing"
)

func tableFixture() *Table {
	t := &Table{Rows: []float64{100, 150, 120}, Columns: []float64{120, 200, 160}, Header: true, Banded: true, Cells: make([][]TableCell, 3)}
	for r := range t.Cells {
		t.Cells[r] = make([]TableCell, 3)
		for c := range t.Cells[r] {
			t.Cells[r][c] = TableCell{Text: "Editable <cell>", FontFace: "Arial", FontSize: 22, Color: "#FFFFFF", Fill: "#314765", BorderColor: "#55AAFF", BorderWidth: .5, Padding: 8, Align: "center", Vertical: "middle"}
		}
	}
	t.Cells[1][0].RowSpan, t.Cells[1][0].ColSpan = 2, 2
	t.Cells[1][0].Runs = []TextRun{{Text: "Bold & ", Bold: true}, {Text: "red\nnext", Color: "#FF0055", Italic: true, Underline: true}}
	t.Cells[1][0].Text = "Bold & red\nnext"
	for _, rc := range [][2]int{{1, 1}, {2, 0}, {2, 1}} {
		t.Cells[rc[0]][rc[1]] = TableCell{Hidden: true}
	}
	t.Cells[2][2].Fill, t.Cells[2][2].BorderColor, t.Cells[2][2].BorderWidth = "none", "none", 0
	return t
}

func TestTableDrawingMLWhitespaceWithoutWordAttribute(t *testing.T) {
	table := tableFixture()
	want := "  Leading  and trailing  "
	table.Cells[0][0].Text = want
	input := Presentation{Slides: []Slide{{Objects: []Object{{ID: "table", Kind: "table", X: 120, Y: 80, Width: 480, Height: 370, Table: table}}}}}
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, input); err != nil {
		t.Fatal(err)
	}
	for _, part := range packageParts(t, out.Bytes()) {
		if bytes.Contains(part, []byte(`<a:t xml:space=`)) {
			t.Fatal("DrawingML text used Word's unsupported attribute")
		}
	}
	got, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	if got.Slides[0].Objects[0].Table.Cells[0][0].Text != want {
		t.Fatal("table whitespace changed")
	}
}

func TestTableNativePackageRoundTrip(t *testing.T) {
	input := Presentation{Slides: []Slide{{Objects: []Object{{ID: "table", Kind: "table", X: 120, Y: 80, Width: 480, Height: 370, Rotation: 12, Table: tableFixture()}}}}}
	input.Slides[0].Objects[0].Table.Cells[0][0].LineHeight = 1.6
	var out bytes.Buffer
	report, err := Write(context.Background(), &out, input)
	if err != nil || len(report.Warnings) != 0 {
		t.Fatalf("write: %v %#v", err, report)
	}
	z, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name != "ppt/slides/slide1.xml" {
			continue
		}
		r, _ := f.Open()
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		x := string(data)
		for _, marker := range []string{"<p:graphicFrame>", "<a:tbl>", `gridSpan="2"`, `rowSpan="2"`, `hMerge="1"`, `vMerge="1"`, `marL="50800"`, `anchor="ctr"`, `u="sng"`} {
			if !strings.Contains(x, marker) {
				t.Errorf("missing %s", marker)
			}
		}
		if strings.Contains(x, "<p:pic>") || strings.Contains(x, "<p:sp>") {
			t.Fatal("table flattened")
		}
		if _, err := parseXML(data, 128); err != nil {
			t.Fatal(err)
		}
	}
	got, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatalf("read: %v %#v", err, report)
	}
	o := got.Slides[0].Objects[0]
	if o.Kind != "table" || o.Table == nil || math.Abs(o.Rotation-12) > .001 || o.Width != 480 {
		t.Fatalf("geometry: %#v", o)
	}
	table := o.Table
	if table.Cells[0][0].LineHeight != 1.6 {
		t.Fatal("table line spacing lost", table.Cells[0][0])
	}
	if table.Cells[1][0].RowSpan != 2 || table.Cells[1][0].ColSpan != 2 || !table.Cells[2][1].Hidden || table.Cells[1][0].Text != "Bold & red\nnext" {
		t.Fatalf("merge/text: %#v", table)
	}
	if len(table.Cells[1][0].Runs) < 2 || !table.Cells[1][0].Runs[0].Bold || table.Cells[1][0].Runs[1].Color != "#FF0055" || !table.Cells[1][0].Runs[1].Underline {
		t.Fatalf("runs lost: %#v", table.Cells[1][0].Runs)
	}
	if table.Cells[0][0].Padding != 8 || table.Cells[0][0].Vertical != "middle" || table.Cells[0][0].Align != "center" || table.Cells[0][0].BorderWidth != .5 || table.Cells[2][2].Fill != "none" {
		t.Fatalf("style: %#v", table.Cells)
	}
}

func TestTableMalformedSpansRejectedBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*Table){
		func(t *Table) { t.Cells[1][0].ColSpan = 4 },
		func(t *Table) { t.Cells[1][1].Hidden = false },
		func(t *Table) { t.Columns[0] = math.Inf(1) },
		func(t *Table) { t.Rows = nil },
	} {
		table := tableFixture()
		mutate(table)
		var out bytes.Buffer
		_, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "table", Width: 480, Height: 370, Table: table}}}}})
		if err == nil || out.Len() != 0 {
			t.Fatalf("invalid table wrote output: %v %d", err, out.Len())
		}
	}
}

func TestTableMalformedReaderAndStyleDiagnostics(t *testing.T) {
	o := Object{ID: "table", Kind: "table", Width: 480, Height: 370, Table: tableFixture()}
	x := tableXML(o, 2, "")
	for _, replacement := range []struct{ from, to string }{{`gridSpan="2"`, `gridSpan="4000000"`}, {`hMerge="1"`, `hMerge="0"`}, {`w="762000"`, `w="0"`}} {
		n, err := parseXML([]byte(strings.Replace(x, replacement.from, replacement.to, 1)), 128)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := parseTableFrame(n, colourResolver{}); err == nil {
			t.Fatalf("accepted malformed %s", replacement.from)
		}
	}
	n, err := parseXML([]byte(strings.Replace(x, "<a:tblPr", `<a:tblPr`, 1)), 128)
	if err != nil {
		t.Fatal(err)
	}
	tbl := findFirst(n, "tbl")
	pr := findChild(tbl, "tblPr")
	pr.Children = append(pr.Children, &node{Name: "tableStyleId", Text: "{foreign-style}"})
	_, warnings, err := parseTableFrame(n, colourResolver{})
	if err != nil || len(warnings) == 0 {
		t.Fatalf("missing style diagnostic: %v %#v", err, warnings)
	}
}

func TestTableGroupedAndInheritedFrames(t *testing.T) {
	x := tableXML(Object{ID: "table", Kind: "table", Width: 480, Height: 370, Table: tableFixture()}, 2, "")
	n, err := parseXML([]byte("<grpSp><grpSp>"+x+"</grpSp></grpSp>"), 128)
	if err != nil {
		t.Fatal(err)
	}
	objects, warnings, err := parseGroup(n, nil, nil, nil, nil, DefaultLimits(), colourResolver{})
	if err != nil || len(warnings) != 0 || len(objects) != 1 || objects[0].Table == nil {
		t.Fatalf("group table: %v %#v %#v", err, objects, warnings)
	}
	root, err := parseXML([]byte("<sldMaster><cSld><spTree>"+x+"</spTree></cSld></sldMaster>"), 128)
	if err != nil {
		t.Fatal(err)
	}
	_, fixed, warnings, err := parseInheritedShapeTree(root, nil, nil, nil, DefaultLimits(), colourResolver{})
	if err != nil || len(warnings) != 0 || len(fixed) != 1 || fixed[0].Table == nil {
		t.Fatalf("inherited table: %v %#v %#v", err, fixed, warnings)
	}
}

func TestTableRejectsUnrepresentableFrameWithoutPartialOutput(t *testing.T) {
	for _, width := range []float64{0, math.NaN(), math.Inf(1), 1e-10} {
		var out bytes.Buffer
		_, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "table", Width: width, Height: 370, Table: tableFixture()}}}}})
		if err == nil || out.Len() != 0 {
			t.Fatal("invalid frame wrote bytes", width, err)
		}
	}
}
