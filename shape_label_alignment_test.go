package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func legacyJustifiedLabelFixture(t *testing.T) Deck {
	t.Helper()
	d := labelledSelectionFixture(t)
	e := &d.Slides[0].Elements[0]
	q := mustQuery(e.Query)
	data, err := decodeShapeLabelData(q.Get("shape-label"))
	if err != nil {
		t.Fatal(err)
	}
	data.Query = setQueryValue(data.Query, "align", "justify")
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
	e.Query = q.Encode()
	return d
}

func TestLegacyShapeLabelJustificationLifecycle(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		d := legacyJustifiedLabelFixture(t)
		s := newNativeEditorSession("Legacy label.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "base-labels"
		}
		before := cloneDeck(s.deck)
		owner := func() Element {
			if master {
				return s.deck.Masters.Base.Slide.Elements[0]
			}
			return s.deck.Slides[0].Elements[0]
		}
		label, ok := shapeLabel(owner())
		if !ok || sceneTextFor(label).Align != "justify" {
			t.Fatal("legacy label justification replaced by default center", label)
		}
		revision := s.version
		copy := nativeEditorAction{Action: "copy-object-style", SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"label-a"}, StyleContext: "shape-label"}
		if err := s.copyObjectStyle(copy); err != nil || s.styleClipboard.Fields["text.align"].Value != "justify" || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("copy did not capture legacy justification without mutation", err)
		}
		a := nativeEditorAction{Action: "set-selection-property", SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"label-a"}, Property: "label.align", PropertyValue: json.RawMessage(`"right"`)}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		label, _ = shapeLabel(owner())
		if sceneTextFor(label).Align != "end" {
			t.Fatal("explicit primary alignment did not override legacy alias")
		}
		revision, a.PropertyValue = s.version, json.RawMessage(`""`)
		if err := s.apply(a); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("Inherit did not restore nested legacy style/content", err)
		}
		label, _ = shapeLabel(owner())
		if sceneTextFor(label).Align != "justify" {
			t.Fatal("Inherit lost legacy justification")
		}
		revision = s.version
		if err := s.apply(nativeEditorAction{Action: "reset-object-style", SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"label-a"}, StyleContext: "shape-label"}); err != nil {
			t.Fatal(err)
		}
		label, _ = shapeLabel(owner())
		if sceneTextFor(label).Align != "center" {
			t.Fatal("Reset retained the legacy paragraph alias", label.Query)
		}
		q := mustQuery(owner().Query)
		data, err := decodeShapeLabelData(q.Get("shape-label"))
		if err != nil || data.Text != "Shared label" || data.Extra == nil || q.Get("fg") != "#0055aa" {
			t.Fatal("Reset lost content/extensions or restyled the shape", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("Reset Undo did not restore legacy label", err)
		}
		wire, err := serializeDeck("Legacy.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Legacy.md", wire)
		if err != nil {
			t.Fatal(err)
		}
		e := loaded.Slides[0].Elements[0]
		if master {
			e = loaded.Masters.Base.Slide.Elements[0]
		}
		label, _ = shapeLabel(e)
		if sceneTextFor(label).Align != "justify" {
			t.Fatal("Markdown reopen changed legacy label alignment")
		}
	}
}

func TestShapeLabelAlignmentPrimaryAndPositionAliases(t *testing.T) {
	d := legacyJustifiedLabelFixture(t)
	e := d.Slides[0].Elements[0]
	q := mustQuery(e.Query)
	data, _ := decodeShapeLabelData(q.Get("shape-label"))
	for _, primary := range []string{"left", "center", "right", "justify"} {
		data.Query = setQueryValue(data.Query, "text-align", primary)
		encoded, _ := json.Marshal(data)
		q.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
		e.Query = q.Encode()
		label, _ := shapeLabel(e)
		if textBoxAlignment(label) != primary {
			t.Fatal("legacy alias replaced explicit primary alignment", primary, label.Query)
		}
	}
	for _, anchor := range []string{"left", "center", "right"} {
		data.Query = url.Values{"align": {anchor}}.Encode()
		encoded, _ := json.Marshal(data)
		q.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
		e.Query = q.Encode()
		label, _ := shapeLabel(e)
		if textBoxAlignment(label) != "center" {
			t.Fatal("positional alias became paragraph alignment", anchor)
		}
	}
}
