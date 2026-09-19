package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRichTextLivePreviewDoesNotMutateDeck(t *testing.T) {
	base := withConvertedRuns(Element{Kind: "text", ID: "preview-rich", Query: "render=truetype&top=2&left=2"}, []sceneRun{{Text: "AB", Bold: true}, {Text: "CD", Italic: true}})
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{base}
	deck.Slides[0].Engagement = nil
	deck.Slides[0].EngagementResult = nil
	s := newNativeEditorSession("Preview.md", deck)
	for _, master := range []bool{false, true} {
		if master {
			s.deck.Masters.Base.Slide.Elements = []Element{base}
			s.masterMode = true
			s.currentMaster = 0
		}
		before, revision := cloneDeck(s.deck), s.version
		edit := base
		edit.Text = "ABxCD"
		data, _ := json.Marshal(nativeEditorAction{Name: "inline-edit", Element: 0, ElementData: &edit, Cols: 245, Rows: 56})
		w := httptest.NewRecorder()
		s.handlePreview(w, httptest.NewRequest("POST", "/api/editor/preview", bytes.NewReader(data)))
		if w.Code != 200 {
			t.Fatalf("preview failed: %s", w.Body.String())
		}
		var preview nativeEditorInlinePreview
		if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, page := range preview.Pages {
			if page.Scene != nil {
				for _, object := range page.Scene.Objects {
					if object.ID != base.ID || object.Text == nil {
						continue
					}
					runs := object.Text.Runs
					found = len(runs) == 2 && runs[0].Text == "ABx" && runs[0].Bold && runs[1].Text == "CD" && runs[1].Italic
				}
			}
			for _, line := range page.Lines {
				if line.TrueType != nil && line.TrueType.Text == edit.Text {
					runs := line.TrueType.RichRuns
					found = len(runs) == 2 && runs[0].Text == "ABx" && runs[0].Bold && runs[1].Text == "CD" && runs[1].Italic
				}
			}
		}
		if !found {
			t.Fatalf("master=%v: preview lost style", master)
		}
		if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
			t.Fatal("preview mutated deck/history")
		}
	}
}

func TestModernRunStylesRoundTripAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Runs.md", deck)
	before := cloneDeck(s.deck)
	runs := []sceneRun{{Text: "Normal "}, {Text: "bold", Bold: true, Underline: true}, {Text: " italic", Italic: true, Color: "#55aaff"}}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "title", TextRuns: runs}); err != nil {
		t.Fatal(err)
	}
	element := s.deck.Slides[0].Elements[0]
	if !reflect.DeepEqual(sceneTextFor(element).Runs, runs) {
		t.Fatalf("run styles not projected: %+v", sceneTextFor(element).Runs)
	}
	if !reflect.DeepEqual(exportTrueTypeElement(element, 100, 20).RichRuns, runs) {
		t.Fatal("Retro export lost rich runs")
	}
	path := filepath.Join(t.TempDir(), "Runs.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.Text == element.Text {
			found = reflect.DeepEqual(sceneTextFor(e).Runs, runs)
		}
	}
	if !found {
		t.Fatal("mixed styles lost on save/reopen")
	}
	element.Text = "External edit"
	if exportTrueTypeElement(element, 100, 20).RichRuns != nil {
		t.Fatal("Retro export retained stale run styles")
	}
	if got := sceneTextFor(element).Runs; len(got) != 1 || got[0].Text != "External edit" || got[0].Bold || got[0].Italic {
		t.Fatalf("stale styles survived: %+v", got)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("undo did not restore exact deck")
	}
}

func TestModernRunsCodeAndInvalidMetadata(t *testing.T) {
	runs := []sceneRun{{Text: "literal * [color=x]", Bold: true, Italic: true, Color: "#ff0000"}}
	value, err := encodeModernRuns(runs[0].Text, "", "code", runs)
	if err != nil {
		t.Fatal(err)
	}
	e := Element{Kind: "code", Text: runs[0].Text, Query: url.Values{"modern-runs": {value}}.Encode()}
	if !reflect.DeepEqual(sceneTextFor(e).Runs, runs) {
		t.Fatal("styled code lost literal content")
	}
	for _, bad := range []string{"not base64", "e30="} {
		e.Query = url.Values{"modern-runs": {bad}}.Encode()
		if got := sceneTextFor(e).Runs; len(got) != 1 || got[0].Bold || got[0].Italic {
			t.Fatal("invalid metadata used")
		}
	}
	if _, err := encodeModernRuns("bad", "", "code", []sceneRun{{Text: "bad", Color: "red<script>"}}); err == nil {
		t.Fatal("invalid code color accepted")
	}
}

func TestLegacyTextEditsPreserveRunStyles(t *testing.T) {
	base := Element{Kind: "text", Text: "ABCD", ID: "styled", Query: "render=truetype"}
	value, err := encodeModernRuns(base.Text, "", base.Kind, []sceneRun{{Text: "A"}, {Text: "BC", Bold: true, Italic: true}, {Text: "D"}})
	if err != nil {
		t.Fatal(err)
	}
	base.Query = setQueryValue(base.Query, "modern-runs", value)
	for _, test := range []struct {
		text string
		bold string
	}{{"ABxCD", "BxC"}, {"ABD", "B"}, {"AxBCD", "BC"}, {"😀ABCD", "BC"}} {
		after := base
		after.Text = test.text
		after = preserveRunStyles(base, after)
		var bold string
		for _, run := range sceneTextFor(after).Runs {
			if run.Bold {
				bold += run.Text
				if !run.Italic {
					t.Fatal("italic lost")
				}
			}
		}
		if bold != test.bold {
			t.Fatalf("%q: styled %q, want %q", test.text, bold, test.bold)
		}
	}
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{base}
	s := newNativeEditorSession("Legacy.md", deck)
	before := cloneDeck(s.deck)
	after := base
	after.Text = "ABxCD"
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &after}); err != nil {
		t.Fatal(err)
	}
	if got := sceneTextFor(s.deck.Slides[0].Elements[0]).Runs; len(got) != 3 || got[1].Text != "BxC" || !got[1].Bold {
		t.Fatalf("legacy transaction dropped styles: %+v", got)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("legacy styled edit undo mismatch")
	}
	after = base
	after.Query = setQueryValue(after.Query, "ttf-weight", "bold")
	after = preserveRunStyles(base, after)
	for _, run := range sceneTextFor(after).Runs {
		if !run.Bold {
			t.Fatal("whole-element bold did not apply")
		}
	}
}

func TestRichTextKindConversion(t *testing.T) {
	runs := []sceneRun{{Text: "First", Bold: true, Color: "#ff0000"}, {Text: "\n"}, {Text: "Second", Italic: true}}
	base := withConvertedRuns(Element{Kind: "text", ID: "rich", Query: "render=truetype&top=2&left=2"}, runs)
	for _, kind := range []string{"heading", "bullet", "code"} {
		slide := Slide{Elements: []Element{base}}
		if _, err := convertNativeEditorTextKind(&slide, 0, kind, 1, 245, 56); err != nil {
			t.Fatal(err)
		}
		converted := slide.Elements[0]
		if !reflect.DeepEqual(exportedRichRuns(converted), runs) {
			t.Fatalf("%s conversion lost styles: %+v", kind, exportedRichRuns(converted))
		}
		if kind == "code" && converted.Text != "First\nSecond" {
			t.Fatal("code contains colour tag markup")
		}
		if kind == "heading" {
			continue
		}
		if _, err := convertNativeEditorTextKind(&slide, 0, "text", 0, 245, 56); err != nil {
			t.Fatal(err)
		}
		if len(slide.Elements) != 2 {
			t.Fatalf("%s did not split into two text items", kind)
		}
		for i, element := range slide.Elements {
			got := sceneTextFor(element).Runs
			want := runs[0]
			if i == 1 {
				want = runs[2]
			}
			if !reflect.DeepEqual(got, []sceneRun{want}) {
				t.Fatalf("%s line %d: %+v", kind, i, got)
			}
		}
	}
}
