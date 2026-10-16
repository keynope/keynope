package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"keynope/internal/pptx"
)

func labelledSelectionFixture(t *testing.T) Deck {
	t.Helper()
	data, _ := json.Marshal(shapeLabelData{Text: "Shared label", Query: "render=truetype&modern-size=40&modern-font=mono", Extra: &jsonExtensions{"future": json.RawMessage(`{"keep":true}`)}})
	shape := Element{ID: "label-a", Kind: "shape", Query: "left=12&top=7&width=70&height=20&shape=square&fg=%230055aa&shape-label=" + url.QueryEscape(base64.StdEncoding.EncodeToString(data))}
	second := shape
	second.ID = "label-b"
	second.Query = setQueryValue(second.Query, "left", "120")
	return Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "labels", Elements: []Element{shape, second, {ID: "plain", Kind: "text", Text: "Unrelated", Query: "render=truetype&left=10&top=36&width=220&height=10"}}}}}
}

func TestSelectionLabelPropertiesAtomicOwnedHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := labelledSelectionFixture(t)
		s := newNativeEditorSession("Labels.md", d)
		if master {
			s.masterMode = true
			s.currentMaster = 0
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "base-labels"
		}
		before := cloneDeck(s.deck)
		for _, property := range []struct {
			id    string
			value json.RawMessage
		}{{"label.size", json.RawMessage(`80`)}, {"label.colorToken", json.RawMessage(`"accent-2"`)}, {"label.font", json.RawMessage(`"sans"`)}, {"label.align", json.RawMessage(`"right"`)}, {"label.bold", json.RawMessage(`true`)}} {
			version := s.version
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"label-a", "label-b", "plain"}, Property: property.id, PropertyValue: property.value}); err != nil {
				t.Fatal(property.id, err)
			}
		}
		after := cloneDeck(s.deck)
		target := s.deck.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
		}
		for _, owner := range target.Elements[:2] {
			q := mustQuery(owner.Query)
			original := mustQuery(before.Slides[0].Elements[0].Query)
			if q.Get("shape-label") != original.Get("shape-label") || q.Get("fg") != original.Get("fg") || q.Get("width") != "70" || q.Get("height") != "20" {
				t.Fatal("label style changed content/body/frame", owner)
			}
			if q.Get("label-modern-size") != "80" || q.Get("label-theme-fg") != "accent-2" {
				t.Fatal("missing sparse label override", q)
			}
		}
		if target.Elements[2].Query != before.Slides[0].Elements[2].Query {
			t.Fatal("label subset changed unrelated text")
		}
		for i := 0; i < 5; i++ {
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(before, s.deck) {
			t.Fatal("label history did not restore original")
		}
		for i := 0; i < 5; i++ {
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(after, s.deck) {
			t.Fatal("label redo lost sparse metadata")
		}
		file := filepath.Join(t.TempDir(), "Labels.md")
		if err := saveDeck(file, s.deck); err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeck(file)
		if err != nil {
			t.Fatal(err)
		}
		want := s.deck.Slides[0].Elements[0]
		got := loaded.Slides[0].Elements[0]
		if master {
			want = s.deck.Masters.Base.Slide.Elements[0]
			got = loaded.Masters.Base.Slide.Elements[0]
		}
		if mustQuery(got.Query).Get("label-modern-size") != mustQuery(want.Query).Get("label-modern-size") {
			t.Fatal("Markdown lost label styles", master, got.Query, want.Query)
		}
	}
}

func TestSelectionLabelCapabilityLocksValidationAndNoOp(t *testing.T) {
	d := labelledSelectionFixture(t)
	s := newNativeEditorSession("Labels.md", d)
	ids := []string{"label-a", "plain"}
	caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), ids, "", s.version)
	if !reflect.DeepEqual(caps.Contexts["shape-label"], []string{"label-a"}) || len(caps.Properties["label.size"].EligibleIDs) != 1 || !caps.Properties["label.size"].Editable {
		t.Fatal("label subset/context missing")
	}
	for _, value := range []json.RawMessage{json.RawMessage(`"bad"`), json.RawMessage(`0`), json.RawMessage(`99999`)} {
		before := cloneDeck(s.deck)
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: ids, Property: "label.size", PropertyValue: value}); err == nil || !reflect.DeepEqual(before, s.deck) || s.version != version {
			t.Fatal("invalid label command not atomic")
		}
	}
	version := s.version
	command := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: ids, Property: "label.size", PropertyValue: json.RawMessage(`70`)}
	if err := s.apply(command); err != nil {
		t.Fatal(err)
	}
	version = s.version
	beforeNoOp := cloneDeck(s.deck)
	command.SceneRevision = &version
	if err := s.apply(command); err != nil || version != s.version || !reflect.DeepEqual(beforeNoOp, s.deck) {
		t.Fatal("repeated accepted label style changed revision/history", err)
	}
	s.deck.Slides[0].Elements[2].Query = setQueryValue(s.deck.Slides[0].Elements[2].Query, "object-locked", "1")
	version = s.version
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: ids, Property: "label.size", PropertyValue: json.RawMessage(`70`)}); err == nil || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("locked excluded member allowed partial label edit")
	}
}

func TestSelectionLabelInheritedContentAndPaletteProjection(t *testing.T) {
	d := labelledSelectionFixture(t)
	shape := d.Slides[0].Elements[0]
	shape.SlotID = "shape-slot"
	shape.PlaceholderRole = "body"
	shape.ID = "master-shape"
	d.EnsureDefaultMasters()
	d.Masters.Layouts = append(d.Masters.Layouts, MasterLayout{ID: "label-layout", Name: "Labels", Slide: Slide{Elements: []Element{shape}}})
	d.Slides[0] = Slide{ID: "slide", LayoutID: "label-layout", Elements: []Element{{ID: "instance", Kind: "shape", MasterSlotID: "shape-slot", Query: "left=20&top=8&master-clear=shadow"}}}
	s := newNativeEditorSession("Inherited.md", d)
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"instance"}, Property: "label.colorToken", PropertyValue: json.RawMessage(`"accent-3"`)}); err != nil {
		t.Fatal(err)
	}
	q := mustQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("shape-label") || q.Get("master-clear") != "label-fg,shadow" {
		t.Fatal("inherited label content/shape suppression baked or lost", q)
	}
	caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), []string{"instance"}, "", s.version)
	if state := caps.Properties["label.colorToken"]; state.Provenance != "explicit" || state.Value != "accent-3" {
		t.Fatal("inherited label's sparse override reported as inherited", state)
	}
	scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, object := range scene.Objects {
		if object.ID == "instance" {
			found = object.LabelPaint != nil && object.LabelPaint.Color == s.deck.Theme.Colors.Accent[2]
		}
	}
	if !found {
		t.Fatal("label role not projected")
	}
	changed := cloneDeck(s.deck)
	changed.Theme.Colors.Accent[2] = "#ff00aa"
	if sceneProjectionInputsEqual(s.deck, changed, sceneResponseKey{index: 0}) {
		t.Fatal("linked label theme role did not invalidate its consumer")
	}
	// Label content remains inherited after a style-only mutation.
	layout := s.deck.Masters.LayoutIndex("label-layout")
	raw := mustQuery(s.deck.Masters.Layouts[layout].Slide.Elements[0].Query)
	bytes, _ := base64.StdEncoding.DecodeString(raw.Get("shape-label"))
	var label shapeLabelData
	_ = json.Unmarshal(bytes, &label)
	label.Text = "Master text changed"
	bytes, _ = json.Marshal(label)
	raw.Set("shape-label", base64.StdEncoding.EncodeToString(bytes))
	s.deck.Masters.Layouts[layout].Slide.Elements[0].Query = raw.Encode()
	resolved := s.deck.ResolveSlide(0, false)
	projected, _ := shapeLabel(resolved.Elements[0])
	if projected.Text != "Master text changed" {
		t.Fatal("label content no longer inherited")
	}
	// Content editing intentionally owns the label text, but starts from the
	// resolved master label and retains both its extensions and sparse styles.
	if _, err := applySceneText(&s.deck, nativeEditorAction{Slide: 0, ObjectID: "instance", TextRuns: []sceneRun{{Text: "Slide text"}}}); err != nil {
		t.Fatal(err)
	}
	editedLabel, extra, err := sceneEditableShapeLabel(s.deck.Slides[0].Elements[0])
	if err != nil || editedLabel.Text != "Slide text" || extra.Extra == nil || mustQuery(editedLabel.Query).Get("theme-fg") != "accent-3" {
		t.Fatal("inherited inline edit lost its master style/extensions", err, editedLabel, extra)
	}
}

func TestSelectionLabelClosedAndEnteredGroup(t *testing.T) {
	d := labelledSelectionFixture(t)
	for i := 0; i < 2; i++ {
		d.Slides[0].Elements[i].Query = setQueryValue(d.Slides[0].Elements[i].Query, "group", "label-group")
	}
	ids := []string{"label-a", "label-b"}
	before := cloneDeck(d)
	command := nativeEditorAction{Slide: 0, ObjectIDs: ids, Property: "label.size", PropertyValue: json.RawMessage(`60`)}
	if changed, err := applySelectionProperty(&d, command, ""); err == nil || changed || !reflect.DeepEqual(before, d) {
		t.Fatal("closed group allowed label child mutation")
	}
	if changed, err := applySelectionProperty(&d, command, "label-group"); err != nil || !changed {
		t.Fatal("entered group blocked label child styling", err)
	}
}

func TestSelectionLabelVisualFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_LABEL_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "labels.md"), labelledSelectionFixture(t)); err != nil {
		t.Fatal(err)
	}
}

func TestSelectionLabelInlineEditRetainsSparseStyles(t *testing.T) {
	d := labelledSelectionFixture(t)
	for _, edit := range []struct{ property, value string }{{"label.size", "80"}, {"label.colorToken", `"accent-2"`}, {"label.align", `"right"`}, {"label.shadow", `"solid"`}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}, ""); err != nil {
			t.Fatal(err)
		}
	}
	before := mustQuery(d.Slides[0].Elements[0].Query)
	if _, err := applySceneText(&d, nativeEditorAction{Slide: 0, ObjectID: "label-a", TextRuns: []sceneRun{{Text: "Edited label"}}}); err != nil {
		t.Fatal(err)
	}
	q := mustQuery(d.Slides[0].Elements[0].Query)
	for _, key := range []string{"label-modern-size", "label-theme-fg", "label-text-align", "label-shadow"} {
		if q.Get(key) != before.Get(key) {
			t.Fatalf("inline content edit lost %s: %q -> %q", key, before.Get(key), q.Get(key))
		}
	}
	label, data, err := sceneEditableShapeLabel(d.Slides[0].Elements[0])
	if err != nil || label.Text != "Edited label" || data.Extra == nil {
		t.Fatal("inline edit lost content or extension", err, label, data)
	}
	size := 90.0
	if _, err := applySceneText(&d, nativeEditorAction{Slide: 0, ObjectID: "label-a", TextRuns: []sceneRun{{Text: "Edited label"}}, ModernSize: &size}); err != nil {
		t.Fatal(err)
	}
	label, _ = shapeLabel(d.Slides[0].Elements[0])
	if mustQuery(label.Query).Get("modern-size") != "90" {
		t.Fatal("sparse override masked inline size edit", label.Query)
	}
}

func TestSelectionLabelExplicitOffAndInherit(t *testing.T) {
	d := labelledSelectionFixture(t)
	owner := &d.Slides[0].Elements[0]
	_, data, _ := sceneEditableShapeLabel(*owner)
	data.Query += "&outline=dark&shadow=soft&gradient-start=%2300ff00&gradient-end=%23ff0000&gradient-dir=vertical"
	raw, _ := json.Marshal(data)
	owner.Query = setQueryValue(owner.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
	for _, property := range []string{"label.outline", "label.shadow", "label.gradientDirection"} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: property, PropertyValue: json.RawMessage(`"none"`)}, ""); err != nil {
			t.Fatal(err)
		}
		caps := buildSelectionCapabilities(d, d.Slides[0], d.ResolveSlide(0, false), []string{"label-a"}, "", 1)
		if state := caps.Properties[property]; state.Value != "none" || state.Provenance != "explicit" {
			t.Fatal("effect Off did not mask nested style", property, state)
		}
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: property, PropertyValue: json.RawMessage(`""`)}, ""); err != nil {
			t.Fatal(err)
		}
		caps = buildSelectionCapabilities(d, d.Slides[0], d.ResolveSlide(0, false), []string{"label-a"}, "", 1)
		if caps.Properties[property].Value == "none" {
			t.Fatal("Inherit did not restore nested effect", property)
		}
	}
}

func TestSelectionLabelClearedEmphasisAndSpacingRoundTrip(t *testing.T) {
	d := labelledSelectionFixture(t)
	owner := &d.Slides[0].Elements[0]
	_, data, _ := sceneEditableShapeLabel(*owner)
	data.Query += "&ttf-weight=bold&modern-paragraph-before=2"
	runs, err := encodeModernRuns(data.Text, "bold", "text", []sceneRun{{Text: data.Text, Bold: true, Italic: true, Underline: true}})
	if err != nil {
		t.Fatal(err)
	}
	data.Query = setQueryValue(data.Query, "modern-runs", runs)
	raw, _ := json.Marshal(data)
	owner.Query = setQueryValue(owner.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
	for _, edit := range []struct{ property, value string }{{"label.bold", "false"}, {"label.italic", "false"}, {"label.underline", "false"}, {"label.paragraphBefore", "0"}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}, ""); err != nil {
			t.Fatal(err)
		}
	}
	check := func(deck Deck) {
		t.Helper()
		caps := buildSelectionCapabilities(deck, deck.Slides[0], deck.ResolveSlide(0, false), []string{"label-a"}, "", 1)
		for _, property := range []string{"label.bold", "label.italic", "label.underline"} {
			if state := caps.Properties[property]; state.Value != false || state.Provenance != "explicit" {
				t.Fatal("cleared nested emphasis returned", property, state)
			}
		}
		if state := caps.Properties["label.paragraphBefore"]; state.Value != float64(0) || state.Provenance != "explicit" {
			t.Fatal("zero paragraph spacing returned to nested value", state)
		}
	}
	check(d)
	file := filepath.Join(t.TempDir(), "Cleared.md")
	if err := saveDeck(file, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(file)
	if err != nil {
		t.Fatal(err)
	}
	check(loaded)
}

func TestSelectionLabelEditablePPTXAndLossWarnings(t *testing.T) {
	d := labelledSelectionFixture(t)
	for _, edit := range []struct{ property, value string }{{"label.size", "80"}, {"label.font", `"sans"`}, {"label.colorToken", `"accent-2"`}, {"label.align", `"right"`}, {"label.vertical", `"bottom"`}, {"label.bold", "true"}, {"label.italic", "true"}, {"label.outline", `"dark"`}, {"label.shadow", `"soft"`}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}, ""); err != nil {
			t.Fatal(err)
		}
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	foundWarning := false
	for _, warning := range report.Warnings {
		if warning.Object == "label-a" && warning.Code == "shape-label-effects-approximated" {
			foundWarning = true
		}
	}
	if foundWarning {
		t.Fatal("supported outline/shadow incorrectly reported as omitted")
	}
	deck, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, slide := range deck.Slides {
		for _, object := range slide.Objects {
			if object.Text == "Shared label" && object.Color == "#7C3AED" {
				if object.TextEffects == nil || object.TextEffects.Stroke == "" || object.TextEffects.Shadow == nil {
					t.Fatal("editable label effects missing", object)
				}
				if object.Kind != "shape" || object.FontFace != "Arial" || !object.Bold || !object.Italic || object.TextAlign != "end" || object.VerticalAlign != "bottom" || object.FontSize != 40 {
					t.Fatal("editable label mapping lost supported typography", object)
				}
				return
			}
		}
	}
	t.Fatal("styled label missing from editable PPTX")
}
