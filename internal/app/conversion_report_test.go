package app

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"keynope/internal/pptx"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestConversionReportPoliciesCoverEmittedCodes(t *testing.T) {
	for _, dir := range []string{".", testRepositoryPath("internal/pptx")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no source files found in %s", dir)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			tree, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(tree, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				warning := false
				switch typ := literal.Type.(type) {
				case *ast.Ident:
					warning = dir == testRepositoryPath("internal/pptx") && typ.Name == "Warning"
				case *ast.SelectorExpr:
					alias, ok := typ.X.(*ast.Ident)
					warning = ok && alias.Name == "pptx" && typ.Sel.Name == "Warning"
				}
				if !warning {
					return true
				}
				for _, entry := range literal.Elts {
					kv, ok := entry.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Code" {
						continue
					}
					value, ok := kv.Value.(*ast.BasicLit)
					if !ok || value.Kind != token.STRING {
						continue
					}
					code, _ := strconv.Unquote(value.Value)
					report := (pptx.Report{Warnings: []pptx.Warning{{Code: code, Message: "Actual fallback detail"}}}).Normalized()
					w := report.Warnings[0]
					if w.Fallback == "unclassified" || w.Feature == "" || w.Severity == "" || w.Remediation == "" {
						t.Errorf("%s: no conversion policy for %s", file, code)
					}
				}
				return true
			})
		}
	}
}

func TestConversionReportOwnershipAndDeduplication(t *testing.T) {
	a := pptx.Warning{Slide: 0, Object: "a", Code: "image-paint-rasterized", Message: "Framing baked"}
	b, c, d := a, a, a
	b.Object = "b"
	c.Slide = 1
	d.Message = "Different fallback detail"
	input := pptx.Report{Warnings: []pptx.Warning{a, a, b, c, d}}
	before := append([]pptx.Warning(nil), input.Warnings...)
	result := input.Normalized()
	if len(result.Warnings) != 4 || !reflect.DeepEqual(input.Warnings, before) || !reflect.DeepEqual(result, result.Normalized()) {
		t.Fatal("lost owners, mutated caller or normalization is unstable", result)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"slide":0`)) {
		t.Fatal("first slide location missing", string(data))
	}
	custom := (pptx.Report{Warnings: []pptx.Warning{{Code: "foreign", Severity: "info", Fallback: "source-kept", Remediation: "Custom guidance"}}}).Normalized().Warnings[0]
	if custom.Severity != "info" || custom.Fallback != "source-kept" || custom.Remediation != "Custom guidance" {
		t.Fatal("explicit policy overwritten", custom)
	}
}

func TestConversionWriterSlideLocationsAndPrivateTabs(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	var output bytes.Buffer
	report, err := pptx.Write(context.Background(), &output, pptx.Presentation{Slides: []pptx.Slide{{Objects: []pptx.Object{{ID: "a", Kind: "shape", Width: 100, Height: 100, Shape: "rect", Link: "unsupported:target"}}}, {Objects: []pptx.Object{{ID: "b", Kind: "shape", Width: 100, Height: 100, Shape: "rect", Link: "unsupported:target"}}}}})
	if err != nil || len(report.Warnings) != 2 || report.Warnings[0].Slide != 0 || report.Warnings[1].Slide != 1 {
		t.Fatal("package warnings lost slide scope", err, report)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"chart": newChartData("column")}}, Slides: []Slide{
		{ID: "first", Elements: []Element{{ID: "first-chart", Kind: "chart", ObjectID: "chart", Query: "left=5&top=5&width=100&height=30&link=https%3A%2F%2Fexample.com"}}},
		{ID: "private", TabID: "private-tab"},
		{ID: "last", Elements: []Element{{ID: "last-chart", Kind: "chart", ObjectID: "chart", Query: "left=5&top=5&width=100&height=30&link=https%3A%2F%2Fexample.com"}}},
	}}
	before := cloneDeck(d)
	_, report, err = exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	var locations []int
	for _, warning := range report.Warnings {
		if warning.Code == "chart-link-omitted" {
			locations = append(locations, warning.Slide)
		}
	}
	if !reflect.DeepEqual(locations, []int{0, 2}) || !reflect.DeepEqual(before, d) {
		t.Fatal("writer source locations or source ownership lost", locations, report)
	}
	if root := os.Getenv("KEYNOPE_CONVERSION_REPORT_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "report.md"), d); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(report)
		if err := os.WriteFile(filepath.Join(root, "report.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeImportConversionRuntimeScope(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	var source bytes.Buffer
	if _, err := pptx.Write(context.Background(), &source, pptx.Presentation{Slides: []pptx.Slide{{Hidden: true, Objects: []pptx.Object{{ID: "first", Kind: "shape", Shape: "rect", Width: 200, Height: 100, Fill: "#ff0055"}}}, {Hidden: true, Objects: []pptx.Object{{ID: "second", Kind: "shape", Shape: "rect", Width: 200, Height: 100, Fill: "#55aaff"}}}}}); err != nil {
		t.Fatal(err)
	}
	d, report, err := importPPTXDocument(source.Bytes(), "Imported.pptx")
	if err != nil {
		t.Fatal(err)
	}
	d.Diagnostics = append(d.Diagnostics, documentConversionDiagnostics(report)...)
	if len(d.Diagnostics) < 2 {
		t.Fatal("import notices missing", report)
	}
	session := newNativeEditorSession("Imported.md", d, true)
	state := session.state()
	var locations []int
	for _, issue := range state.Diagnostics {
		if issue.Conversion {
			if issue.Code == "hidden-slide-visible" {
				locations = append(locations, issue.Slide)
			}
			if issue.Feature == "" || issue.Severity == "" || issue.Fallback == "" || issue.Remediation == "" {
				t.Fatal("structured import report lost", issue)
			}
		}
	}
	if !reflect.DeepEqual(locations, []int{0, 1}) {
		t.Fatal("same-message reports lost distinct slides", locations)
	}
	state.Diagnostics[0].Message = "mutated snapshot"
	if session.state().Diagnostics[0].Message == "mutated snapshot" {
		t.Fatal("report aliased editor state")
	}
	markdown, err := serializeDeck("Imported.md", session.deck)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(markdown, []byte("hidden-slide-visible")) || bytes.Contains(markdown, []byte("ordinary-visible-slide")) {
		t.Fatal("conversion report persisted to authored deck")
	}
	scene, err := buildDocumentSlideScene(session.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(scene)
	if bytes.Contains(public, []byte("hidden-slide-visible")) || bytes.Contains(public, []byte(`"conversion":true`)) {
		t.Fatal("editor report leaked to audience scene")
	}
	if root := os.Getenv("KEYNOPE_CONVERSION_REPORT_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "native-import.md"), d); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(session.state().Diagnostics)
		if err := os.WriteFile(filepath.Join(root, "import-diagnostics.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
