package app

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func capabilityFixture() *nativeEditorSession {
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "cap-slide", Elements: []Element{
		{ID: "text-a", Kind: "text", Text: "First text", Query: "modern-size=120&modern-font=c64&width=50&height=8&top=4"},
		{ID: "text-b", Kind: "text", Text: "Second text", Query: "modern-size=160&modern-font=mono&width=50&height=8&top=20"},
		{ID: "shape-a", Kind: "shape", Query: "shape=square&width=12&height=6&top=35"},
	}}}}
	s := newNativeEditorSession("", deck, false)
	s.selected = 0
	s.selection = map[int]bool{0: true, 1: true, 2: true}
	return s
}

func TestSelectionCapabilitiesMixedAndSubset(t *testing.T) {
	s := capabilityFixture()
	before := cloneDeck(s.deck)
	state := s.actionTransportState("select-element")
	caps := state.SelectionCapabilities
	if caps == nil || caps.Version != 1 || caps.Revision != s.version || len(caps.TargetIDs) != 3 {
		t.Fatal("missing selection contract")
	}
	size := caps.Properties["text.size"]
	if size.State != "mixed" || size.Value != nil || size.TotalCount != 3 || !reflect.DeepEqual(size.EligibleIDs, []string{"text-a", "text-b"}) || !size.Editable {
		t.Fatalf("wrong mixed subset: %+v", size)
	}
	if opacity := caps.Properties["paint.opacity"]; opacity.State != "value" || opacity.Value != 1.0 || len(opacity.EligibleIDs) != 3 || opacity.Provenance != "inherited" {
		t.Fatalf("wrong shared opacity: %+v", opacity)
	}
	if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
		t.Fatal("capability projection mutated document/history")
	}
}

func TestSelectionCapabilitiesEqualValuesKeepProvenance(t *testing.T) {
	s := capabilityFixture()
	s.deck.Slides[0].Elements[0].Query += "&modern-opacity=1"
	caps := s.transportState().SelectionCapabilities
	opacity := caps.Properties["paint.opacity"]
	if opacity.State != "value" || opacity.Value != 1.0 || opacity.Provenance != "mixed" {
		t.Fatalf("equal values lost explicit/inherited distinction: %+v", opacity)
	}
}

func TestSelectionCapabilitiesMixedEmphasisWithinOneObject(t *testing.T) {
	s := capabilityFixture()
	s.selection = map[int]bool{0: true}
	element := &s.deck.Slides[0].Elements[0]
	element.Text = "First text"
	encoded, err := encodeModernRuns(element.Text, "", "text", []sceneRun{
		{Text: "First", Bold: true, Italic: true, Underline: true},
		{Text: " text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	element.Query = setQueryValue(element.Query, "modern-runs", encoded)
	caps := s.transportState().SelectionCapabilities
	for _, property := range []string{"text.bold", "text.italic", "text.underline"} {
		state := caps.Properties[property]
		if state.State != "mixed" || state.Value != nil || state.Provenance != "explicit" || !state.Editable {
			t.Fatalf("%s lost within-object mixed runs: %+v", property, state)
		}
	}
	data, err := json.Marshal(caps)
	if err != nil || !json.Valid(data) {
		t.Fatal("mixed contract did not serialize", err)
	}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: "text.bold", PropertyValue: json.RawMessage("true"), SceneRevision: &revision, Slide: 0, ObjectIDs: []string{element.ID}}); err != nil {
		t.Fatal(err)
	}
	if bold := s.transportState().SelectionCapabilities.Properties["text.bold"]; bold.State != "value" || bold.Value != true {
		t.Fatalf("applying emphasis did not unify the selected object: %+v", bold)
	}
}

func TestSelectionCapabilitiesGroupAndLockedSubset(t *testing.T) {
	s := capabilityFixture()
	for i := range s.deck.Slides[0].Elements {
		s.deck.Slides[0].Elements[i].Query += "&group=cap-group"
	}
	s.selection = map[int]bool{0: true}
	s.deck.Slides[0].Elements[2].Query += "&object-locked=1"
	caps := s.transportState().SelectionCapabilities
	if len(caps.TargetIDs) != 3 || caps.Properties["text.size"].Editable || caps.Properties["text.size"].Reason == "" {
		t.Fatal("collapsed group ignored locked non-text member")
	}
	s.editingGroup = "cap-group"
	caps = s.transportState().SelectionCapabilities
	if caps.Context != "group-child" || len(caps.TargetIDs) != 1 || !caps.Properties["text.size"].Editable {
		t.Fatal("entered child inherited unrelated group lock")
	}
}

func TestSelectionPropertyCommandAtomicHistoryAndAcceptedValue(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "master"}[master], func(t *testing.T) {
			s := capabilityFixture()
			if master {
				s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
				s.masterMode = true
				s.currentMaster = 0
			}
			before := cloneDeck(s.deck)
			revision := s.version
			action := nativeEditorAction{Action: "set-selection-property", Property: "text.size", PropertyValue: json.RawMessage("180"), SceneRevision: &revision, SceneMaster: master, Slide: 0, ObjectIDs: []string{"text-a", "text-b", "shape-a"}}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			state := s.actionTransportState(action.Action)
			if size := state.SelectionCapabilities.Properties["text.size"]; size.State != "value" || size.Value != 180.0 {
				t.Fatalf("response did not return accepted value: %+v", size)
			}
			if len(s.undo) != 1 || s.version != revision+1 {
				t.Fatal("property command was not one transaction")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatalf("undo: %v", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			current := s.version
			action.SceneRevision = &current
			depth := len(s.undo)
			if err := s.apply(action); err != nil || len(s.undo) != depth || s.version != current {
				t.Fatal("exact repeated value dirtied history", err)
			}
		})
	}
}

func TestSelectionPropertyRejectsMalformedStaleAndLockedAtomically(t *testing.T) {
	for _, scenario := range []string{"stale", "locked-nontext", "unknown-property", "wrong-value-type", "null", "missing-id", "opacity-range"} {
		t.Run(scenario, func(t *testing.T) {
			s := capabilityFixture()
			revision := s.version
			action := nativeEditorAction{Action: "set-selection-property", Property: "text.size", PropertyValue: json.RawMessage("180"), SceneRevision: &revision, Slide: 0, ObjectIDs: []string{"text-a", "text-b", "shape-a"}}
			switch scenario {
			case "stale":
				revision--
			case "locked-nontext":
				s.deck.Slides[0].Elements[2].Query += "&object-locked=1"
			case "unknown-property":
				action.Property = "unknown"
			case "wrong-value-type":
				action.PropertyValue = json.RawMessage(`"180"`)
			case "null":
				action.PropertyValue = json.RawMessage("null")
			case "missing-id":
				action.ObjectIDs = append(action.ObjectIDs, "missing")
			case "opacity-range":
				action.Property = "paint.opacity"
				action.PropertyValue = json.RawMessage("2")
			}
			before := s.state()
			if err := s.apply(action); err == nil {
				t.Fatal("invalid property command accepted")
			}
			if !reflect.DeepEqual(s.state(), before) || len(s.undo) != 0 {
				t.Fatal("failed command changed document/view/history")
			}
		})
	}
}

func TestSelectionPropertyFontAlignmentAndOpacity(t *testing.T) {
	s := capabilityFixture()
	for _, edit := range []struct{ property, value string }{{"text.font", `"sans"`}, {"text.align", `"justify"`}, {"text.vertical", `"middle"`}, {"text.bold", "true"}, {"paint.opacity", "0.4"}} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, Slide: 0, ObjectIDs: []string{"text-a", "text-b", "shape-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}); err != nil {
			t.Fatalf("%s: %v", edit.property, err)
		}
		property := s.transportState().SelectionCapabilities.Properties[edit.property]
		if property.State != "value" {
			t.Fatalf("accepted property still mixed: %s %+v", edit.property, property)
		}
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[2].Query)
	if q.Get("modern-opacity") != "0.4" || q.Has("modern-font") || q.Has("text-align") || q.Has("ttf-weight") {
		t.Fatal("text commands edited unrelated shape semantics", q)
	}
}
