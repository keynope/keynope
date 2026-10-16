package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func namedPPTXFixture(t *testing.T) Deck {
	d := connectedPPTXDeck(t)
	d.Slides = d.Slides[:1]
	for i := range d.Slides[0].Elements {
		name := "Diagram <node> & 中文 🚀"
		if d.Slides[0].Elements[i].Kind == "connector" {
			name = "The connection"
		}
		d.Slides[0].Elements[i].Query = setQueryValue(d.Slides[0].Elements[i].Query, "object-name", name)
	}
	structured := structuredFixture()
	d.Objects = structured.Objects.Clone()
	table, chart := structured.Slides[0].Elements[0], structured.Slides[0].Elements[1]
	table.Query = "left=5&top=12&width=100&height=18&object-name=Workshop+plan"
	chart.Query = "left=135&top=12&width=100&height=18&object-name=Our+outcomes"
	d.Assets = map[string]DeckAsset{"picture": clipboardTestAsset(t)}
	d.Slides = append(d.Slides, Slide{ID: "named-primitives", Elements: []Element{
		{ID: "text", Kind: "text", Text: "Named objects remain editable", Query: "render=truetype&modern-font=mono&modern-size=38&left=5&top=1&width=230&height=8&object-name=Overview"}, table, chart,
		{ID: "picture", Kind: "image", AssetID: "picture", Path: embeddedAssetPath("picture", d.Assets["picture"]), Query: "left=5&top=35&width=30&height=15&image-fit=contain&image-style=modern&object-name=The+picture"},
	}})
	return d
}

func TestForeignOversizedObjectNameWarnsWithoutDroppingObject(t *testing.T) {
	data, _, err := exportPPTXDocument(namedPPTXFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var changed bytes.Buffer
	w := zip.NewWriter(&changed)
	replaced := false
	for _, file := range r.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "ppt/slides/slide2.xml" {
			replaced = bytes.Contains(body, []byte(`name="Overview"`))
			body = bytes.ReplaceAll(body, []byte(`name="Overview"`), []byte(`name="`+strings.Repeat("x", 129)+`"`))
		}
		entry, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil || !replaced {
		t.Fatal("foreign-name fixture not changed", err)
	}
	d, report, err := importPPTXDocument(changed.Bytes(), "Foreign.pptx")
	if err != nil {
		t.Fatal(err)
	}
	warned, kept := false, false
	for _, warning := range report.Warnings {
		warned = warned || warning.Code == "object-name-omitted"
	}
	for _, e := range d.Slides[1].Elements {
		kept = kept || e.Text == "Named objects remain editable" && objectName(e) == ""
	}
	if !warned || !kept {
		t.Fatal("invalid foreign name silently lost or removed the object", report)
	}
}

func TestCustomObjectNamesPPTXAllPrimitivesAndReferences(t *testing.T) {
	d := namedPPTXFixture(t)
	before := cloneDeck(d)
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{"Diagram <node> & 中文 🚀": 2, "The connection": 1, "Workshop plan": 1, "Our outcomes": 1, "Overview": 1, "The picture": 1}
	actual := map[string]int{}
	for _, slide := range p.Slides {
		for _, o := range slide.Objects {
			if expected[o.Name] > 0 {
				actual[o.Name]++
			}
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("export lost display names", actual)
	}
	reopened, _, err := importPPTXDocument(data, "Names.pptx")
	if err != nil {
		t.Fatal(err)
	}
	actual = map[string]int{}
	for _, slide := range reopened.Slides {
		for _, e := range slide.Elements {
			if name := objectName(e); expected[name] > 0 {
				actual[name]++
			}
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("import lost primitive/connector names", actual)
	}
	if err := validateDeckObjectReferences(reopened); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("export changed authored names/IDs")
	}
	if dir := os.Getenv("KEYNOPE_OBJECT_NAMES_PPTX_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(dir, "source.md"), d); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "named.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
