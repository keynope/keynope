package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func textAlignmentFixture(t testing.TB, kind, font string) Deck {
	t.Helper()
	d := textMetricInheritanceFixture(t, kind, font)
	m := &d.Masters.Layouts[0].Slide.Elements[0]
	m.Query = setQueryValue(setQueryValue(setQueryValue(m.Query, "text-align", "center"), "text-valign", "middle"), "text-box", "1")
	return d
}

func alignmentFrame(t testing.TB, d Deck) sceneRect {
	t.Helper()
	scene, err := buildMixedSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		if object.ID == "child-text" {
			return object.Bounds
		}
	}
	t.Fatal("missing text frame")
	return sceneRect{}
}

func TestAlignmentKeepsInheritedFrameAndIndependentRestore(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		for _, font := range []string{"c64", "sans", "mono"} {
			for _, property := range []string{"text.align", "text.vertical"} {
				t.Run(fmt.Sprintf("%s/%s/%s", kind, font, property), func(t *testing.T) {
					s := newNativeEditorSession("Alignment.md", textAlignmentFixture(t, kind, font))
					before, frame, revision := cloneDeck(s.deck), alignmentFrame(t, s.deck), s.version
					value, other, inherited := "right", "text.vertical", "center"
					if property == "text.vertical" {
						value, other, inherited = "bottom", "text.align", "middle"
					}
					raw, _ := json.Marshal(value)
					a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: raw}
					if err := s.apply(a); err != nil {
						t.Fatal(err)
					}
					if got := alignmentFrame(t, s.deck); got != frame {
						t.Fatal("alignment moved/resized inherited frame", frame, got)
					}
					q := mustQuery(s.deck.Slides[0].Elements[0].Query)
					if q.Has("width") || q.Has("height") || q.Has("top") || q.Has("left") || q.Has(objectStyleKeys[other]) {
						t.Fatal("alignment froze unrelated inherited properties", q)
					}
					if got := metricSelectionState(s.deck, property); got.Value != value || got.Provenance != "explicit" {
						t.Fatal("alignment was not explicit", got)
					}
					if len(s.undo) != 1 || s.version != revision+1 || !reflect.DeepEqual(before.Masters, s.deck.Masters) {
						t.Fatal("alignment not one isolated history transaction")
					}
					accepted := cloneDeck(s.deck)
					changed := cloneDeck(s.deck)
					master := &changed.Masters.Layouts[0].Slide.Elements[0]
					master.Query = setQueryValue(setQueryValue(master.Query, "width", "130.5"), "left", "30")
					if next := alignmentFrame(t, changed); next == frame {
						t.Fatal("later master frame edits no longer propagate")
					}
					revision = s.version
					if err := s.apply(a); err != nil || s.version != revision {
						t.Fatal("repeated alignment was not a no-op", err)
					}
					if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
						t.Fatal("alignment Undo changed unrelated data", err)
					}
					if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
						t.Fatal("alignment Redo changed unrelated data", err)
					}
					data, err := serializeDeck("Alignment.md", s.deck)
					if err != nil {
						t.Fatal(err)
					}
					reopened, err := parseDeckData("Alignment.md", data)
					if err != nil || metricSelectionState(reopened, property).Value != value || alignmentFrame(t, reopened) != frame {
						t.Fatal("alignment/frame changed after reopen", err)
					}
					revision, a.PropertyValue = s.version, json.RawMessage(`""`)
					if err := s.apply(a); err != nil {
						t.Fatal("independent alignment inherit rejected", err)
					}
					if got := metricSelectionState(s.deck, property); got.Value != inherited || got.Provenance != "inherited" {
						t.Fatal("alignment did not return to master", got)
					}
					if !reflect.DeepEqual(s.deck, before) {
						t.Fatal("Inherit left frozen frame/formatting behind")
					}
				})
			}
		}
	}
}

func TestAlignmentPinsEqualLabelValuesAndRestores(t *testing.T) {
	for _, test := range []struct{ property, value string }{{"label.align", "center"}, {"label.vertical", "middle"}} {
		t.Run(test.property, func(t *testing.T) {
			d := labelledSelectionFixture(t)
			s := newNativeEditorSession("Label alignment.md", d)
			before, revision := cloneDeck(s.deck), s.version
			raw, _ := json.Marshal(test.value)
			a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"label-a"}, Property: test.property, PropertyValue: raw}
			if err := s.apply(a); err != nil {
				t.Fatal(err)
			}
			caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), []string{"label-a"}, "", 0)
			if got := caps.Properties[test.property]; got.Provenance != "explicit" || got.Value != test.value {
				t.Fatal("equal label alignment choice did not pin", got)
			}
			revision, a.PropertyValue = s.version, json.RawMessage(`""`)
			if err := s.apply(a); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("label inherit changed content/shape/nested defaults", err)
			}
		})
	}
}

func TestAlignmentEqualChoicePinsOnlySelectedAxis(t *testing.T) {
	for _, property := range []string{"text.align", "text.vertical"} {
		s := newNativeEditorSession("Pin alignment.md", textAlignmentFixture(t, "text", "sans"))
		before, revision := cloneDeck(s.deck), s.version
		value := metricSelectionState(s.deck, property).Value
		raw, _ := json.Marshal(value)
		a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: raw}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		want := before.Slides[0].Elements[0]
		want.Query = setQueryValue(want.Query, objectStyleKeys[property], value.(string))
		if !reflect.DeepEqual(want, s.deck.Slides[0].Elements[0]) {
			t.Fatal("equal alignment choice changed unrelated fields")
		}
		master := &s.deck.Masters.Layouts[0].Slide.Elements[0]
		master.Query = setQueryValue(setQueryValue(master.Query, "text-align", "left"), "text-valign", "top")
		if got := metricSelectionState(s.deck, property); got.Value != value || got.Provenance != "explicit" {
			t.Fatal("equal choice did not pin master value", got)
		}
		other, inherited := "text.align", "left"
		if property == "text.align" {
			other, inherited = "text.vertical", "top"
		}
		if got := metricSelectionState(s.deck, other); got.Value != inherited || got.Provenance != "inherited" {
			t.Fatal("equal choice froze other alignment axis", got)
		}
	}
}

func TestAlignmentKeepsUnrelatedAutoDimension(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, property := range []string{"text.align", "text.vertical"} {
		for _, fixed := range []string{"neither", "width", "height", "both"} {
			t.Run(property+"/"+fixed, func(t *testing.T) {
				d := textAlignmentFixture(t, "text", "sans")
				q := mustQuery(d.Masters.Layouts[0].Slide.Elements[0].Query)
				if fixed != "width" && fixed != "both" {
					q.Del("width")
				}
				if fixed != "height" && fixed != "both" {
					q.Del("height")
				}
				d.Masters.Layouts[0].Slide.Elements[0].Query = q.Encode()
				s := newNativeEditorSession("Natural alignment.md", d)
				frame, revision := alignmentFrame(t, s.deck), s.version
				value, unrelated := `"right"`, "height"
				if property == "text.vertical" {
					value, unrelated = `"bottom"`, "width"
				}
				if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage(value)}); err != nil {
					t.Fatal(err)
				}
				if query := mustQuery(s.deck.Slides[0].Elements[0].Query); query.Has(unrelated) {
					t.Fatal("alignment froze unrelated auto/inherited dimension", query)
				}
				got := alignmentFrame(t, s.deck)
				for _, axis := range [][2]float64{{frame.X, got.X}, {frame.Y, got.Y}, {frame.Width, got.Width}, {frame.Height, got.Height}} {
					if math.Abs(axis[0]-axis[1]) > 1e-9 {
						t.Fatal("single-axis alignment changed its visible frame", frame, got)
					}
				}
			})
		}
	}
}

func TestAlignmentNormalTabBaseLayoutTransactions(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, scope := range []string{"normal", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			d := textAlignmentFixture(t, "text", "sans")
			if scope == "tab" {
				d.Slides[0].TabID = "alignment-tab"
				syncSlideTabs(&d)
			}
			s := newNativeEditorSession("Scopes.md", d)
			if scope == "base" || scope == "layout" {
				owner := cloneSlide(d.Masters.Layouts[0].Slide)
				owner.ID, owner.Elements[0].ID = "alignment-owner", "child-text"
				s.masterMode = true
				if scope == "layout" {
					s.currentMaster = 1
				}
				*masterSlideAt(&s.deck, s.currentMaster) = owner
			}
			s.selected, s.selection = 0, map[int]bool{0: true}
			for _, property := range []string{"text.align", "text.vertical"} {
				before, revision, count := cloneDeck(s.deck), s.version, len(s.undo)
				raw := json.RawMessage(`"right"`)
				if property == "text.vertical" {
					raw = json.RawMessage(`"bottom"`)
				}
				a := nativeEditorAction{Action: "set-selection-property", SceneMaster: s.masterMode, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: raw}
				if s.masterMode {
					a.Slide = s.currentMaster
				}
				if err := s.apply(a); err != nil {
					t.Fatal(err)
				}
				accepted := cloneDeck(s.deck)
				if s.version != revision+1 || len(s.undo) != count+1 {
					t.Fatal("alignment did not commit once")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
					t.Fatal("scoped Undo not exact", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
					t.Fatal("scoped Redo not exact", err)
				}
				revision, a.PropertyValue = s.version, json.RawMessage(`""`)
				if err := s.apply(a); err != nil {
					t.Fatal("scoped Inherit rejected", err)
				}
				if got := s.transportState().SelectionCapabilities.Properties[property]; got.Provenance != "inherited" {
					t.Fatal("scoped Inherit retained explicitness", got)
				}
			}
		})
	}
}

func TestAlignmentLegacyJustifyWarmTextCache(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := textAlignmentFixture(t, "text", "sans")
	q := mustQuery(d.Masters.Layouts[0].Slide.Elements[0].Query)
	q.Del("text-align")
	d.Masters.Layouts[0].Slide.Elements[0].Query = q.Encode()
	key := func(deck Deck) sceneTextObjectKey {
		e := deck.ResolveSlide(0, false).Elements[0]
		return sceneTextDependencyKey(deck, e, "modern", mustQuery(e.Query))
	}
	plainKey := key(d)
	build := func(deck Deck) string {
		t.Helper()
		scene, err := buildMixedSlideScene(deck, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range scene.Objects {
			if o.ID == "child-text" {
				return o.Text.Align
			}
		}
		t.Fatal("missing text")
		return ""
	}
	if got := build(d); got != "start" {
		t.Fatal("fixture not left aligned", got)
	}
	d.Slides[0].Elements[0].Query = "align=justify"
	legacyKey := key(d)
	if legacyKey == plainKey || build(d) != "justify" {
		t.Fatal("legacy paragraph change reused stale left-aligned text")
	}
	d.Slides[0].Elements[0].Query = "text-align=justify"
	if key(d) != legacyKey || build(d) != "justify" {
		t.Fatal("equivalent legacy/modern paragraph alignment does not share text projection")
	}
}

func TestAlignmentLegacyJustifyAndMixedGroupReset(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"inherit", "style-paste", "reset-style"} {
		t.Run(mode, func(t *testing.T) {
			d := textAlignmentFixture(t, "text", "sans")
			_, table := cellStyleTransferFixture(t)
			d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"table-data": table}}
			d.Slides[0].Elements[0].Query = "group=mixed&align=justify&master-clear=text-valign"
			d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "table", Kind: "table", ObjectID: "table-data", Query: "group=mixed&left=20&top=30&width=100&height=20"})
			s := newNativeEditorSession("Legacy alignment.md", d)
			before, revision := cloneDeck(s.deck), s.version
			if got := metricSelectionState(s.deck, "text.align"); got.Value != "justify" || got.Provenance != "explicit" {
				t.Fatal("legacy paragraph alias lost precedence/provenance", got)
			}
			if mode != "inherit" {
				if err := s.apply(nativeEditorAction{Action: "enter-group", ElementData: &Element{ID: "child-text"}}); err != nil {
					t.Fatal(err)
				}
				revision = s.version
			}
			a := nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: "text.align", PropertyValue: json.RawMessage(`""`)}
			if mode == "style-paste" {
				a.Action, a.StyleData = "paste-object-style", &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{"text.align": {}}}
			} else if mode == "reset-style" {
				// Whole-style Reset intentionally does not support table groups.
				a.Action = "reset-object-style"
			}
			if err := s.apply(a); err != nil {
				t.Fatal(err)
			}
			if got := metricSelectionState(s.deck, "text.align"); got.Value != "center" || got.Provenance != "inherited" {
				t.Fatal("legacy alias remained after reset", got)
			}
			if !reflect.DeepEqual(before.Objects, s.deck.Objects) || !reflect.DeepEqual(before.Slides[0].Elements[1], s.deck.Slides[0].Elements[1]) {
				t.Fatal("text inheritance reset changed table/group data")
			}
		})
	}
}

func TestAlignmentBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_ALIGNMENT_PROOF")
	if root == "" {
		t.Skip("alignment browser fixture not requested")
	}
	useTestAuthoredSize(t, 245, 56)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := textAlignmentFixture(t, "text", "sans")
	legacy.Slides[0].Elements[0].Query = "align=justify"
	for name, d := range map[string]Deck{"alignment.md": textAlignmentFixture(t, "text", "sans"), "legacy.md": legacy, "labels.md": labelledSelectionFixture(t), "legacy-label.md": legacyJustifiedLabelFixture(t)} {
		if err := saveDeck(filepath.Join(root, name), d); err != nil {
			t.Fatal(err)
		}
	}
}
