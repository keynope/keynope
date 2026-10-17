package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func pptxThemeImportFixture(t *testing.T) []byte {
	t.Helper()
	theme := &pptx.Theme{Name: "Editable import", Dark1: "#101010", Light1: "#eeeeee", Dark2: "#202020", Light2: "#dddddd", Accent: [6]string{"#d94a11", "#5c20c8", "#117755", "#0077dd", "#112233", "#884400"}, Hyperlink: "#0055aa", FollowedHyperlink: "#aa0055", HeadingFont: "Courier New", BodyFont: "Arial"}
	objects := []pptx.Object{
		{ID: "text", Kind: "text", X: 80, Y: 60, Width: 900, Height: 90, FontFace: "Arial", FontSize: 30, Color: "#101010", Text: "Linked Literal", Paragraphs: []pptx.TextParagraph{{Runs: []pptx.TextRun{{Text: "Linked ", Color: "#d94a11", FontFace: "Courier New"}, {Text: "Literal", Color: "#d94a11", FontFace: "Courier New"}}}}},
		{ID: "shape", Kind: "shape", Shape: "rect", X: 1050, Y: 60, Width: 350, Height: 170, Fill: "#5c20c8", Stroke: "#0077dd", StrokeWidth: 2},
		{ID: "table", Kind: "table", X: 80, Y: 270, Width: 650, Height: 160, Table: &pptx.Table{Rows: []float64{160}, Columns: []float64{325, 325}, Cells: [][]pptx.TableCell{{{Text: "Linked cell", FontFace: "Arial", FontSize: 24, Color: "#112233", Fill: "#117755", BorderColor: "none", Padding: 10}, {Text: "Literal cell", FontFace: "Arial", FontSize: 24, Color: "#112233", Fill: "#117755", BorderColor: "none", Padding: 10}}}}},
		{ID: "chart", Kind: "chart", X: 800, Y: 350, Width: 850, Height: 500, Chart: &pptx.Chart{Type: "column", FontFace: "Arial", FontSize: 16, Color: "#101010", Categories: []string{"A", "B"}, Series: []pptx.ChartSeries{{Name: "Linked", Color: "#884400", LabelColor: "#112233", Values: []float64{3, 7}}, {Name: "Literal", Color: "#884400", LabelColor: "#112233", Values: []float64{6, 4}}}, Options: pptx.ChartOptions{Legend: true, DataLabels: true}}},
	}
	var output bytes.Buffer
	if _, err := pptx.Write(context.Background(), &output, pptx.Presentation{Theme: theme, Slides: []pptx.Slide{{Background: "#eeeeee", Objects: objects}, {Background: "#eeeeee", Objects: objects}}}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(data)
	}
	for _, name := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml"} {
		x := parts[name]
		x = strings.Replace(x, `<a:srgbClr val="D94A11"/>`, `<a:schemeClr val="accent1"/>`, 1)
		x = strings.Replace(x, `typeface="Courier New"`, `typeface="+mj-lt"`, 1)
		x = strings.ReplaceAll(x, `<a:srgbClr val="5C20C8"/>`, `<a:schemeClr val="accent2"/>`)
		x = strings.ReplaceAll(x, `<a:srgbClr val="0077DD"/>`, `<a:schemeClr val="accent4"/>`)
		for range 2 {
			x = strings.Replace(x, `<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent5"/>`, 1)
		}
		x = strings.ReplaceAll(x, `<a:srgbClr val="117755"/>`, `<a:schemeClr val="accent3"/>`)
		parts[name] = x
	}
	for _, name := range []string{"ppt/charts/chart-s1-o4.xml", "ppt/charts/chart-s2-o4.xml"} {
		parts[name] = strings.Replace(parts[name], `<a:srgbClr val="884400"/>`, `<a:schemeClr val="accent6"/>`, 1)
		parts[name] = strings.Replace(parts[name], `<a:srgbClr val="112233"/>`, `<a:schemeClr val="accent5"/>`, 1)
		parts[name] = strings.ReplaceAll(parts[name], `typeface="Arial"`, `typeface="+mj-lt"`)
	}
	// Same palette/font values, genuinely independent second master.
	parts["ppt/theme/foreign.xml"] = parts["ppt/theme/theme1.xml"]
	parts["ppt/slideMasters/foreign.xml"] = parts["ppt/slideMasters/slideMaster1.xml"]
	parts["ppt/slideMasters/_rels/foreign.xml.rels"] = `<Relationships><Relationship Id="theme" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/foreign.xml"/></Relationships>`
	parts["ppt/slideLayouts/foreign.xml"] = parts["ppt/slideLayouts/slideLayout1.xml"]
	parts["ppt/slideLayouts/_rels/foreign.xml.rels"] = `<Relationships><Relationship Id="master" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/foreign.xml"/></Relationships>`
	parts["ppt/slides/_rels/slide2.xml.rels"] = strings.Replace(parts["ppt/slides/_rels/slide2.xml.rels"], `../slideLayouts/slideLayout1.xml`, `../slideLayouts/foreign.xml`, 1)
	var encoded bytes.Buffer
	writer := zip.NewWriter(&encoded)
	for name, data := range parts {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestPPTXImportedThemeIsEditableAndScopeOwned(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	source := pptxThemeImportFixture(t)
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(source), int64(len(source)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	beforeNative, _ := json.Marshal(native)
	legacy := native
	legacy.Theme = nil
	baseline, _, err := importPPTXPresentation(legacy, pptx.Report{})
	if err != nil {
		t.Fatal(err)
	}
	deck, _, err := importPPTXDocument(source, "Owned.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if deck.Theme == nil || deck.Theme.Name != "Editable import" || deck.Theme.Fonts.Heading.FontID != "mono" {
		t.Fatal("primary theme not attached", deck.Theme)
	}
	var initial, old, changed []slideScene
	for i := range deck.Slides {
		a, err := buildDocumentSlideScene(baseline, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		b, err := buildDocumentSlideScene(deck, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if a.Background != b.Background || len(a.Objects) != len(b.Objects) {
			t.Fatal("theme attachment changed slide topology")
		}
		for j := range a.Objects {
			if a.Objects[j].Bounds != b.Objects[j].Bounds {
				t.Fatal("theme attachment changed geometry", i, j, a.Objects[j].Bounds, b.Objects[j].Bounds)
			}
		}
		old = append(old, a)
		initial = append(initial, b)
	}
	var primaryText, foreignText Element
	for _, e := range deck.Slides[0].Elements {
		if e.Kind == "text" {
			primaryText = e
		}
	}
	for _, e := range deck.Slides[1].Elements {
		if e.Kind == "text" {
			foreignText = e
		}
	}
	runs := sceneTextFor(primaryText).Runs
	if len(runs) != 2 || runs[0].ColorToken != "accent-1" || runs[0].FontRole != "heading" || runs[1].ColorToken != "" || runs[1].FontRole != "" {
		t.Fatal("primary word references/explicit equal values lost", runs)
	}
	for _, r := range sceneTextFor(foreignText).Runs {
		if r.ColorToken != "" || r.FontRole != "" {
			t.Fatal("foreign master rebound to primary theme", r)
		}
	}
	for _, e := range deck.Slides[0].Elements {
		if e.Kind == "shape" && mustQuery(e.Query).Get("theme-fg") != "accent-2" {
			t.Fatal("shape fill source not bound", e)
		}
		if e.Kind == "chart" {
			data := deck.Objects.Charts[e.ObjectID]
			if data.Series[0].LabelColor != "theme:accent-5" || data.Series[1].LabelColor != "#112233" {
				t.Fatal("chart label source/literal paint lost", data.Series)
			}
			if data.Options.FontRole != "heading" || data.Options.FontID != "mono" {
				t.Fatal("chart major-font source lost", data.Options)
			}
		}
	}
	edited := cloneDeck(deck)
	for i := range edited.Theme.Colors.Accent {
		edited.Theme.Colors.Accent[i] = "#00ff88"
	}
	edited.Theme.Fonts.Heading.FontID = "c64"
	for i := range edited.Slides {
		scene, err := buildDocumentSlideScene(edited, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		changed = append(changed, scene)
	}
	if !reflect.DeepEqual(initial[1], changed[1]) {
		t.Fatal("primary edit changed foreign-master slide")
	}
	if reflect.DeepEqual(initial[0], changed[0]) {
		t.Fatal("primary theme edit has no consumers")
	}
	encoded, err := serializeDeck("Theme.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Theme.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck.Theme, reopened.Theme) {
		t.Fatal("saved theme changed")
	}
	for i := range reopened.Slides {
		scene, err := buildDocumentSlideScene(reopened, i, 245, 56)
		if err != nil || !reflect.DeepEqual(initial[i], scene) {
			t.Fatal("saved imported theme changed scene", i, err)
		}
	}
	afterNative, _ := json.Marshal(native)
	if !bytes.Equal(beforeNative, afterNative) {
		t.Fatal("adapter changed native source graph")
	}
	if dir := os.Getenv("KEYNOPE_PPTX_THEME_IMPORT_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]any{"baseline": old, "attached": initial, "changed": changed} {
			data, _ := json.Marshal(value)
			if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "source.pptx"), source, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "attached.md"), encoded, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
