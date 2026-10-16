package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func tableRunFixture(t *testing.T) Deck {
	t.Helper()
	data, err := newTableData(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	size := 40.0
	data.Style.Header = false
	data.Cells[0][0].Style = &TableCellStyle{FontID: "sans", FontSize: &size}
	data.Cells[0][0].Text = ""
	data.Cells[0][0].Runs = []ObjectTextRun{
		{Text: "Plain ", Color: "#ffffff"},
		{Text: "Mono ", FontID: "mono", FontSize: 64, Bold: true, Color: "#55aaff"},
		{Text: "C64", FontID: "c64", FontSize: 72, Italic: true},
	}
	return Deck{Slides: []Slide{{ID: "run-slide", Elements: []Element{{ID: "run-table", Kind: "table", ObjectID: "run-data", Query: "top=8&left=24&width=160&height=20"}}}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"run-data": data}}}
}

func TestTableRunTypographyPortableAndOwned(t *testing.T) {
	deck := tableRunFixture(t)
	want := cloneTable(deck.Objects.Tables["run-data"])
	text, err := serializeDeck("runs.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeckData("runs.md", text)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Objects.Tables["run-data"], want) {
		t.Fatal("run typography did not survive Markdown", parsed.Objects.Tables)
	}
	scene, err := buildSlideScene(parsed, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 || scene.Objects[0].Table == nil {
		t.Fatal("table projection missing")
	}
	runs := scene.Objects[0].Table.Cells[0].Text.Runs
	if len(runs) != 3 || runs[1].FontID != "mono" || runs[1].FontSize != 64 || runs[2].FontID != "c64" || runs[2].FontSize != 72 {
		t.Fatal("run typography was not projected", runs)
	}
	copy := cloneDeck(parsed)
	data := copy.Objects.Tables["run-data"]
	data.Cells[0][0].Runs[1].FontSize = 128
	copy.Objects.Tables["run-data"] = data
	if parsed.Objects.Tables["run-data"].Cells[0][0].Runs[1].FontSize != 64 {
		t.Fatal("clone aliases font runs")
	}
}

func TestTableRunTypographyValidationAndExtensions(t *testing.T) {
	deck := tableRunFixture(t)
	for _, size := range []float64{-1, 1025, math.NaN(), math.Inf(1)} {
		data := cloneTable(deck.Objects.Tables["run-data"])
		data.Cells[0][0].Runs[0].FontSize = size
		if data.Validate() == nil {
			t.Fatalf("invalid run size accepted: %v", size)
		}
	}
	data := cloneTable(deck.Objects.Tables["run-data"])
	data.Cells[0][0].Runs[0].FontID = "url(evil)"
	if data.Validate() == nil {
		t.Fatal("invalid font ID accepted")
	}
	var run ObjectTextRun
	if err := json.Unmarshal([]byte(`{"text":"Portable","fontId":"mono","fontSize":64,"future":{"value":7}}`), &run); err != nil {
		t.Fatal(err)
	}
	run.FontSize = 72
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"fontSize":72`) || !strings.Contains(string(encoded), `"future":{"value":7}`) {
		t.Fatal("known font fields or unknown extensions lost", string(encoded))
	}
}

func TestTableRunTypographyPPTXRoundTrip(t *testing.T) {
	deck := tableRunFixture(t)
	// Styling the first word must not rewrite the inherited cell defaults.
	payload := deck.Objects.Tables["run-data"]
	payload.Cells[0][0].Runs[0].FontID = "mono"
	payload.Cells[0][0].Runs[0].FontSize = 70
	payload.Cells[0][0].Runs[0].Color = "#ff0055"
	deck.Objects.Tables["run-data"] = payload
	data, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatal("mixed table font export warns", report)
	}
	presentation, readReport, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(readReport.Warnings) != 0 {
		t.Fatal("mixed table fonts failed native package read", err, readReport)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	fontParts := 0
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "ppt/fonts/") && file.UncompressedSize64 > 0 {
			fontParts++
		}
	}
	if fontParts == 0 {
		t.Fatal("C64 in a Sans cell was not embedded")
	}
	var raw *pptx.Table
	for _, object := range presentation.Slides[0].Objects {
		if object.Table != nil {
			raw = object.Table
			break
		}
	}
	if raw == nil || len(raw.Cells[0][0].Runs) != 3 {
		t.Fatal("mixed cell flattened")
	}
	if raw.Cells[0][0].FontFace != "Arial" || raw.Cells[0][0].FontSize != 20 || raw.Cells[0][0].Runs[0].FontFace != "Courier New" || raw.Cells[0][0].Runs[0].FontSize != 35 {
		t.Fatal("first run replaced cell defaults", raw.Cells[0][0])
	}
	if raw.Cells[0][0].Runs[1].FontFace != "Courier New" || raw.Cells[0][0].Runs[1].FontSize != 32 || !strings.Contains(raw.Cells[0][0].Runs[2].FontFace, "C64") || raw.Cells[0][0].Runs[2].FontSize != 36 {
		t.Fatal("mixed font faces or sizes lost", raw.Cells[0][0].Runs)
	}
	imported, _, err := importPPTXDocument(data, "runs.pptx")
	if err != nil {
		t.Fatal(err)
	}
	var table TableData
	for _, payload := range imported.Objects.Tables {
		table = payload
		break
	}
	runs := table.Cells[0][0].Runs
	if len(runs) != 3 || runs[1].FontID != "mono" || runs[1].FontSize != 64 || runs[1].Color != "#55AAFF" || !runs[1].Bold || runs[2].FontID != "c64" || runs[2].FontSize != 72 || !runs[2].Italic {
		t.Fatal("editable run typography lost in import", runs)
	}
}
