package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func paragraphInheritanceFixture(t testing.TB, kind string) Deck {
	t.Helper()
	d := inheritedEmphasisFixture(t, false)
	m := &d.Masters.Layouts[0].Slide.Elements[0]
	m.Kind, m.Text = kind, "First paragraph\nSecond paragraph"
	m.Query = setQueryValue(setQueryValue(setQueryValue(m.Query, "modern-paragraph-before", "0.4"), "modern-paragraph-after", "0.7"), "modern-line-height", "1.8")
	d.Slides[0].Elements[0].Kind, d.Slides[0].Elements[0].Text = kind, m.Text
	return d
}

func checkParagraphInheritance(t testing.TB, d Deck, before, after, line float64) {
	t.Helper()
	for _, e := range d.ResolveSlide(0, false).Elements {
		if e.ID != "child-text" {
			continue
		}
		text := sceneTextFor(e)
		if text.ParagraphBefore != before || text.ParagraphAfter != after || text.LineHeight != line {
			t.Fatalf("spacing = %g/%g/%g; want %g/%g/%g", text.ParagraphBefore, text.ParagraphAfter, text.LineHeight, before, after, line)
		}
		return
	}
	t.Fatal("missing child text")
}

func TestParagraphZeroOverridesMasterAndClearInherits(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		for _, tab := range []bool{false, true} {
			for _, mode := range []string{"inspector", "inline", "style-paste"} {
				t.Run(fmt.Sprintf("%s/tab=%t/%s", kind, tab, mode), func(t *testing.T) {
					d := paragraphInheritanceFixture(t, kind)
					if tab {
						d.Slides[0].TabID = "paragraph-tab"
						syncSlideTabs(&d)
					}
					s := newNativeEditorSession("Paragraphs.md", d)
					before := cloneDeck(s.deck)
					zero, revision := 0.0, s.version
					a := nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"child-text"}, SceneRevision: &revision, ModernParagraphBefore: &zero, ModernParagraphAfter: &zero}
					if mode == "inline" {
						a.Action, a.ObjectID, a.TextRuns = "set-scene-text", "child-text", []sceneRun{{Text: d.Slides[0].Elements[0].Text, Bold: true}}
					} else if mode == "style-paste" {
						a.Action = "paste-object-style"
						a.StyleData = &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{"text.paragraphBefore": {Present: true, Value: "0"}, "text.paragraphAfter": {Present: true, Value: "0"}}}
					}
					if err := s.apply(a); err != nil {
						t.Fatal(err)
					}
					checkParagraphInheritance(t, s.deck, 0, 0, 1.8)
					changedMaster := cloneDeck(s.deck)
					m := &changedMaster.Masters.Layouts[0].Slide.Elements[0]
					m.Query = setQueryValue(setQueryValue(m.Query, "modern-paragraph-before", "0.9"), "modern-line-height", "2")
					checkParagraphInheritance(t, changedMaster, 0, 0, 2)
					if s.version != revision+1 || len(s.undo) != 1 || !reflect.DeepEqual(s.deck.Masters, before.Masters) {
						t.Fatal("spacing was not one owned transaction")
					}
					accepted := cloneDeck(s.deck)
					data, err := serializeDeck("Paragraphs.md", s.deck)
					if err != nil {
						t.Fatal(err)
					}
					reopened, err := parseDeckData("Paragraphs.md", data)
					if err != nil {
						t.Fatal(err)
					}
					checkParagraphInheritance(t, reopened, 0, 0, 1.8)
					revision = s.version
					if err := s.apply(a); err != nil || s.version != revision {
						t.Fatalf("repeated spacing was not a no-op: %v\nbefore: %+v\nafter: %+v", err, accepted.Slides[0].Elements[0], s.deck.Slides[0].Elements[0])
					}
					if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
						t.Fatal("inexact Undo", err)
					}
					if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
						t.Fatal("inexact Redo", err)
					}
					for _, property := range []string{"text.paragraphBefore", "text.paragraphAfter"} {
						revision = s.version
						if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage(`""`)}); err != nil {
							t.Fatal(err)
						}
					}
					checkParagraphInheritance(t, s.deck, .4, .7, 1.8)
					if q := mustQuery(s.deck.Slides[0].Elements[0].Query); q.Has("modern-paragraph-before") || q.Has("modern-paragraph-after") {
						t.Fatal("clear materialized master spacing")
					}
				})
			}
		}
	}
}

func TestInlineDefaultLineHeightOverridesMaster(t *testing.T) {
	s := newNativeEditorSession("Paragraphs.md", paragraphInheritanceFixture(t, "text"))
	line, revision := 1.25, s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "child-text", TextRuns: []sceneRun{{Text: s.deck.Slides[0].Elements[0].Text, Bold: true}}, ModernLineHeight: &line}); err != nil {
		t.Fatal(err)
	}
	checkParagraphInheritance(t, s.deck, .4, .7, 1.25)
	revision = s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: "text.lineHeight", PropertyValue: json.RawMessage(`""`)}); err != nil {
		t.Fatal(err)
	}
	checkParagraphInheritance(t, s.deck, .4, .7, 1.8)
}

func TestParagraphInheritanceClearMixedGroup(t *testing.T) {
	d := paragraphInheritanceFixture(t, "text")
	_, table := cellStyleTransferFixture(t)
	d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"group-table-data": table}}
	d.Slides[0].Elements[0].Query = "group=mixed&modern-paragraph-after=0"
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "group-table", Kind: "table", ObjectID: "group-table-data", Query: "group=mixed&top=30&left=20&width=100&height=20"})
	s := newNativeEditorSession("Group.md", d)
	before, revision := cloneDeck(s.deck), s.version
	a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: "text.paragraphAfter", PropertyValue: json.RawMessage(`""`)}
	if err := s.apply(a); err != nil {
		t.Fatal(err)
	}
	checkParagraphInheritance(t, s.deck, .4, .7, 1.8)
	if s.version != revision+1 || len(s.undo) != 1 || !reflect.DeepEqual(before.Objects, s.deck.Objects) || !reflect.DeepEqual(before.Slides[0].Elements[1], s.deck.Slides[0].Elements[1]) {
		t.Fatal("group spacing touched the unrelated table or history")
	}
	revision = s.version
	if err := s.apply(a); err != nil || s.version != revision {
		t.Fatal("repeated inheritance clear changed revision", err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("group clear Undo was not exact", err)
	}
}

func TestLabelParagraphZeroAndInherit(t *testing.T) {
	d := labelledSelectionFixture(t)
	e := &d.Slides[0].Elements[0]
	label, data, err := sceneEditableShapeLabel(*e)
	if err != nil {
		t.Fatal(err)
	}
	data.Query = setQueryValue(setQueryValue(label.Query, "modern-paragraph-before", "0.4"), "modern-paragraph-after", "0.7")
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	e.Query = setQueryValue(e.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
	s := newNativeEditorSession("Labels.md", d)
	for _, property := range []string{"label.paragraphBefore", "label.paragraphAfter"} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"label-a"}, Property: property, PropertyValue: json.RawMessage("0")}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(before, after float64) {
		t.Helper()
		effective := s.deck.ResolveSlide(0, false).Elements[0]
		l, ok := shapeLabel(effective)
		if !ok {
			t.Fatal("missing label")
		}
		text := sceneTextFor(l)
		if text.ParagraphBefore != before || text.ParagraphAfter != after {
			t.Fatal("incorrect label spacing", text)
		}
	}
	check(0, 0)
	for _, property := range []string{"label.paragraphBefore", "label.paragraphAfter"} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"label-a"}, Property: property, PropertyValue: json.RawMessage(`""`)}); err != nil {
			t.Fatal(err)
		}
	}
	check(.4, .7)
}

func TestParagraphBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_PARAGRAPH_PROOF")
	if root == "" {
		t.Skip("browser fixture not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "paragraphs.md"), paragraphInheritanceFixture(t, "text")); err != nil {
		t.Fatal(err)
	}
	d := labelledSelectionFixture(t)
	e := &d.Slides[0].Elements[0]
	_, data, err := sceneEditableShapeLabel(*e)
	if err != nil {
		t.Fatal(err)
	}
	data.Query = setQueryValue(setQueryValue(data.Query, "modern-paragraph-before", "0.4"), "modern-paragraph-after", "0.7")
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	e.Query = setQueryValue(e.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
	if err := saveDeck(filepath.Join(root, "labels.md"), d); err != nil {
		t.Fatal(err)
	}
}
