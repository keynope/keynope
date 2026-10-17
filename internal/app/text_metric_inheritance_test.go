package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func textMetricInheritanceFixture(t testing.TB, kind, font string) Deck {
	t.Helper()
	d := inheritedEmphasisFixture(t, false)
	m := &d.Masters.Layouts[0].Slide.Elements[0]
	m.Kind = kind
	m.Query = setQueryValue(setQueryValue(m.Query, "modern-font", font), "modern-width", "75")
	d.Slides[0].Elements[0].Kind = kind
	return d
}

func metricSelectionState(d Deck, property string) selectionPropertyState {
	return buildSelectionCapabilities(d, d.Slides[0], d.ResolveSlide(0, false), []string{"child-text"}, "", 0).Properties[property]
}

func TestTextMetricExplicitInheritedValueAndRestore(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		for _, font := range []string{"c64", "mono", "sans"} {
			for _, property := range []string{"text.size", "text.width"} {
				t.Run(fmt.Sprintf("%s/%s/%s", kind, font, property), func(t *testing.T) {
					s := newNativeEditorSession("Metrics.md", textMetricInheritanceFixture(t, kind, font))
					before, revision := cloneDeck(s.deck), s.version
					initial := metricSelectionState(s.deck, property)
					if initial.Provenance != "inherited" {
						t.Fatal("fixture not inherited", initial)
					}
					raw, err := json.Marshal(initial.Value)
					if err != nil {
						t.Fatal(err)
					}
					a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: raw}
					if err := s.apply(a); err != nil {
						t.Fatal(err)
					}
					state := metricSelectionState(s.deck, property)
					if state.Provenance != "explicit" || state.Value != initial.Value || len(s.undo) != 1 || s.version != revision+1 {
						t.Fatal("absolute choice did not pin inherited value", state)
					}
					if !reflect.DeepEqual(before.Masters, s.deck.Masters) {
						t.Fatal("child metric mutated master")
					}
					accepted := cloneDeck(s.deck)
					want := before.Slides[0].Elements[0]
					want.Query = setQueryValue(want.Query, objectStyleKeys[property], string(raw))
					if !reflect.DeepEqual(want, accepted.Slides[0].Elements[0]) {
						t.Fatal("pin changed unrelated content, frame or typography")
					}
					changed := cloneDeck(s.deck)
					master := &changed.Masters.Layouts[0].Slide.Elements[0]
					master.Query = setQueryValue(setQueryValue(master.Query, "modern-size", "84"), "modern-width", "85")
					if got := metricSelectionState(changed, property); got.Value != initial.Value {
						t.Fatal("explicit metric changed with master", got)
					}
					other, otherValue := "text.width", float64(85)
					if property == "text.width" {
						other, otherValue = "text.size", 84
					}
					if got := metricSelectionState(changed, other); got.Value != otherValue || got.Provenance != "inherited" {
						t.Fatal("other metric froze", got)
					}
					revision = s.version
					if err := s.apply(a); err != nil || s.version != revision {
						t.Fatal("repeated absolute value was not a no-op", err)
					}
					if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
						t.Fatal("inexact Undo", err)
					}
					if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
						t.Fatal("inexact Redo", err)
					}
					bytes, err := serializeDeck("Metrics.md", s.deck)
					if err != nil {
						t.Fatal(err)
					}
					reopened, err := parseDeckData("Metrics.md", bytes)
					if err != nil {
						t.Fatal(err)
					}
					if got := metricSelectionState(reopened, property); got.Value != initial.Value || got.Provenance != "explicit" {
						t.Fatal("reopen lost metric", got)
					}
					revision, a.PropertyValue = s.version, json.RawMessage(`""`)
					if err := s.apply(a); err != nil {
						t.Fatal(err)
					}
					if got := metricSelectionState(s.deck, property); got.Value != initial.Value || got.Provenance != "inherited" {
						t.Fatal("clear did not restore inheritance", got)
					}
				})
			}
		}
	}
}

func TestTextMetricZeroDeltaDoesNotPinInheritance(t *testing.T) {
	for _, width := range []bool{false, true} {
		s := newNativeEditorSession("Metrics.md", textMetricInheritanceFixture(t, "text", "sans"))
		before, revision, zero := cloneDeck(s.deck), s.version, 0.0
		a := nativeEditorAction{Action: "set-text-size", ObjectIDs: []string{"child-text"}, SceneRevision: &revision, TextSizeDelta: &zero}
		if width {
			a.Action, a.TextSizeDelta, a.TextWidthDelta = "set-text-width", nil, &zero
		}
		if err := s.apply(a); err != nil || s.version != revision || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("zero delta created an override", err)
		}
	}
}

func TestTextMetricClearMixedGroupAndLegacyAlias(t *testing.T) {
	for _, property := range []string{"text.size", "text.width"} {
		d := textMetricInheritanceFixture(t, "text", "sans")
		_, table := cellStyleTransferFixture(t)
		d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"group-table-data": table}}
		legacyKey, legacyValue := "ttf-size", "120"
		if property == "text.width" {
			legacyKey, legacyValue = "ttf-width", "110"
		}
		d.Slides[0].Elements[0].Query = "group=mixed&fg=%23aa3355&" + legacyKey + "=" + legacyValue
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "table", Kind: "table", ObjectID: "group-table-data", Query: "group=mixed&top=30&left=20&width=100&height=20"})
		s := newNativeEditorSession("Group.md", d)
		before, revision := cloneDeck(s.deck), s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage(`""`)}); err != nil {
			t.Fatal(err)
		}
		if got := metricSelectionState(s.deck, property); got.Provenance != "inherited" {
			t.Fatal("legacy alias still masks inheritance", got)
		}
		if !reflect.DeepEqual(before.Objects, s.deck.Objects) || !reflect.DeepEqual(before.Slides[0].Elements[1], s.deck.Slides[0].Elements[1]) || mustQuery(s.deck.Slides[0].Elements[0].Query).Get("fg") != "#aa3355" {
			t.Fatal("metric clear touched unrelated group data/paint")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("group reset Undo not exact", err)
		}
	}
}

func TestInheritedFontWidthPreservesMasterFrame(t *testing.T) {
	for _, font := range []string{"c64", "mono", "sans"} {
		s := newNativeEditorSession("Frame.md", textMetricInheritanceFixture(t, "text", font))
		frame := func() sceneRect {
			t.Helper()
			scene, err := buildMixedSlideScene(s.deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range scene.Objects {
				if object.ID == "child-text" {
					return object.Bounds
				}
			}
			t.Fatal("missing text")
			return sceneRect{}
		}
		before, bounds := cloneDeck(s.deck), frame()
		revision, delta := s.version, 1.0
		if err := s.apply(nativeEditorAction{Action: "set-text-width", ObjectIDs: []string{"child-text"}, SceneRevision: &revision, TextWidthDelta: &delta}); err != nil {
			t.Fatal(err)
		}
		if after := frame(); after != bounds {
			t.Fatalf("%s font width resized master frame: before=%+v after=%+v", font, bounds, after)
		}
		q := mustQuery(s.deck.Slides[0].Elements[0].Query)
		if q.Has("width") || q.Has("height") {
			t.Fatal("font width froze inherited frame", q)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("frame edit Undo not exact", err)
		}
	}
}

func TestLabelMetricPinAndRestoreNestedDefaults(t *testing.T) {
	for _, test := range []struct {
		property string
		value    float64
	}{{"label.size", 40}, {"label.width", 100}} {
		s := newNativeEditorSession("Labels.md", labelledSelectionFixture(t))
		before, revision := cloneDeck(s.deck), s.version
		raw, _ := json.Marshal(test.value)
		a := nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{"label-a"}, SceneRevision: &revision, Property: test.property, PropertyValue: raw}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), []string{"label-a"}, "", 0)
		if got := caps.Properties[test.property]; got.Provenance != "explicit" || got.Value != test.value {
			t.Fatal("label metric did not become explicit", got)
		}
		revision, a.PropertyValue = s.version, json.RawMessage(`""`)
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.deck, before) {
			t.Fatal("label inherit changed content, nested style or body")
		}
	}
}

func TestTextMetricBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_METRIC_PROOF")
	if root == "" {
		t.Skip("browser fixture not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "metrics.md"), textMetricInheritanceFixture(t, "text", "sans")); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "labels.md"), labelledSelectionFixture(t)); err != nil {
		t.Fatal(err)
	}
}
