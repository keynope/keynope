package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"keynope/internal/pptx"
)

func pptxTimingDeck(t *testing.T) Deck {
	t.Helper()
	s := buildEditorFixture(false)
	d := cloneDeck(s.deck)
	table, err := newTableData(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	table.Cells[0][0].Text = "Header"
	d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"table-data": table}, Charts: map[string]ChartData{"chart-data": newChartData("column")}, Builds: map[string]SlideBuild{}}
	imageID, asset, imagePath, err := embeddedImageAsset(tinyPPTXPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	d.Assets = map[string]DeckAsset{imageID: asset}
	d.Slides[0].Elements = append(d.Slides[0].Elements,
		Element{ID: "table-target", Kind: "table", ObjectID: "table-data", Query: "left=10&top=30&width=70&height=20"},
		Element{ID: "chart-target", Kind: "chart", ObjectID: "chart-data", Query: "left=100&top=30&width=90&height=20"},
		Element{ID: "image-target", Kind: "image", AssetID: imageID, Path: imagePath, Query: "image-style=modern&left=200&top=10&width=30&height=15"})
	d.Objects.Builds[d.Slides[0].ID] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "fade", DurationMS: 1200}, Steps: []BuildStep{
		{TargetIDs: []string{"heading"}, Effect: "fade", DurationMS: 500, Advance: "click"},
		{TargetIDs: []string{"first", "table-target"}, Effect: "appear", DurationMS: 100, Advance: "click"},
		{TargetIDs: []string{"chart-target"}, Effect: "wipe", Direction: "down", DurationMS: 400, Advance: "after", DelayMS: 200},
		{TargetIDs: []string{"image-target"}, Effect: "zoom", DurationMS: 700, Advance: "after", DelayMS: 150},
	}}
	return d
}
func TestPPTXTimelinePortableEditableRoundTrip(t *testing.T) {
	d := pptxTimingDeck(t)
	before, _ := json.Marshal(d)
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range report.Warnings {
		if warning.Code == "timeline-omitted" || warning.Code == "timing-unsupported" || warning.Code == "build-target-omitted" {
			t.Fatal(warning)
		}
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("export mutated authored timeline")
	}
	p, report, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	if len(p.Slides[0].Timeline.Steps) != 4 {
		t.Fatal("lost native timeline")
	}
	imported, report, err := importPPTXDocument(data, "Timeline.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) > 0 {
		for _, warning := range report.Warnings {
			if warning.Code != "chart-label-style-approximated" {
				t.Fatal(report)
			}
		}
	}
	if err := validateDeckObjectReferences(imported); err != nil {
		t.Fatal(err)
	}
	build := imported.Objects.Builds[imported.Slides[0].ID]
	if len(build.Steps) != 4 || build.Transition.Effect != "fade" || build.Transition.DurationMS != 1200 {
		t.Fatal(build)
	}
	kinds := map[string]string{}
	for _, element := range imported.Slides[0].Elements {
		kinds[element.ID] = element.Kind
	}
	want := [][]string{{"text"}, {"shape", "table"}, {"chart"}, {"image"}}
	for i, step := range build.Steps {
		got := []string{}
		for _, id := range step.TargetIDs {
			got = append(got, kinds[id])
		}
		if !reflect.DeepEqual(got, want[i]) {
			t.Fatal("wrong target after remap", got, want[i])
		}
	}
	md, err := serializeDeck("Timeline.md", imported)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Timeline.md", md)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Objects.Builds[reopened.Slides[0].ID], build) {
		t.Fatal("timeline not portable in Markdown")
	}
	scene, err := buildDocumentSlideScene(reopened, 0, 245, 56)
	if err != nil || scene.Build == nil || len(scene.Build.Steps) != 4 {
		t.Fatal("reopened timeline did not reach presentation", err)
	}
	if output := os.Getenv("KEYNOPE_PPTX_TIMING_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string][]byte{"timing-editable.pptx": data, "timing-imported.md": md} {
			if err := os.WriteFile(filepath.Join(output, name), content, 0644); err != nil {
				t.Fatal(err)
			}
		}
		html, err := exportHTMLDocument("Timeline.md", reopened.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "timing-imported.html"), []byte(html), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
