package main

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShapeLabelStyleTransferHistoryAndOwnership(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := labelledSelectionFixture(t)
		for _, edit := range []struct{ property, value string }{{"label.size", "80"}, {"label.font", `"sans"`}, {"label.colorToken", `"accent-3"`}, {"label.bold", "true"}, {"label.shadow", `"solid"`}, {"label.paragraphBefore", "0"}} {
			if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}, ""); err != nil {
				t.Fatal(err)
			}
		}
		s := newNativeEditorSession("Label style.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(d.Slides[0])
			s.deck.Masters.Base.Slide.ID = "label-master-base"
		}
		before := cloneDeck(s.deck)
		version := s.version
		command := nativeEditorAction{Action: "copy-object-style", StyleContext: "shape-label", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"label-a"}}
		if err := s.apply(command); err != nil || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("label copy was not read-only", err)
		}
		snapshot := s.transportState().StyleClipboard
		raw, _ := json.Marshal(snapshot)
		if snapshot.Context != "shape-label" || snapshot.Fields["paint.color"].Token != "accent-3" || snapshot.Fields["text.size"].Value != "80" || strings.Contains(string(raw), "Shared label") || len(raw) > 8192 {
			t.Fatal("label capture lost context/role or included content", string(raw))
		}
		command.Action, command.ObjectIDs, command.StyleData = "paste-object-style", []string{"label-b", "plain"}, snapshot
		if err := s.apply(command); err != nil || s.version != version+1 || len(s.undo) != 1 {
			t.Fatal("label paste not one atomic edit", err)
		}
		after := cloneDeck(s.deck)
		target := s.deck.Slides[0]
		original := before.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
			original = before.Masters.Base.Slide
		}
		q, old := mustQuery(target.Elements[1].Query), mustQuery(original.Elements[1].Query)
		for _, key := range []string{"shape-label", "fg", "left", "top", "width", "height"} {
			if q.Get(key) != old.Get(key) {
				t.Fatal("label paste changed body/content/frame", key)
			}
		}
		if q.Get("label-modern-size") != "80" || q.Get("label-theme-fg") != "accent-3" || q.Get("label-shadow") != "solid" || target.Elements[2].Query != original.Elements[2].Query {
			t.Fatal("label subset failed", q)
		}
		version = s.version
		if err := s.apply(command); err != nil || s.version != version || len(s.undo) != 1 {
			t.Fatal("identical label paste not a no-op", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("label paste Undo", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("label paste Redo", err)
		}
		version = s.version
		command.Action = "reset-object-style"
		if err := s.apply(command); err != nil {
			t.Fatal(err)
		}
		target = s.deck.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
		}
		q = mustQuery(target.Elements[1].Query)
		label, data, err := sceneEditableShapeLabel(target.Elements[1])
		if err != nil || label.Text != "Shared label" || data.Extra == nil || q.Get("fg") != old.Get("fg") || q.Has("label-modern-size") || mustQuery(data.Query).Has("modern-font") {
			t.Fatal("label Reset did not preserve content/body or remove old formatting", err, q)
		}
		if q.Has("label-ttf-weight") || q.Has("label-modern-runs") || q.Has("label-text-italic") {
			t.Fatal("Reset retained whole-label emphasis")
		}
		file := filepath.Join(t.TempDir(), "Label style.md")
		if err := saveDeck(file, s.deck); err != nil {
			t.Fatal(err)
		}
		if _, err := parseDeck(file); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShapeLabelStyleFlagsYieldToInlineMixedEmphasis(t *testing.T) {
	d := labelledSelectionFixture(t)
	for _, property := range []string{"label.bold", "label.italic", "label.underline"} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: property, PropertyValue: json.RawMessage(`true`)}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := applySceneText(&d, nativeEditorAction{Slide: 0, ObjectID: "label-a", TextRuns: []sceneRun{{Text: "A", Bold: true}, {Text: "B", Italic: true}}}); err != nil {
		t.Fatal(err)
	}
	label, _ := shapeLabel(d.Slides[0].Elements[0])
	runs := sceneTextFor(label).Runs
	if len(runs) != 2 || !runs[0].Bold || runs[0].Italic || runs[0].Underline || runs[1].Bold || !runs[1].Italic || runs[1].Underline {
		t.Fatal("whole-label flags overrode inline mixed emphasis", runs)
	}
}

func TestShapeLabelStyleContextValidationAndLocks(t *testing.T) {
	d := labelledSelectionFixture(t)
	snapshot, err := captureShapeLabelStyle(d, d.Slides[0], d.ResolveSlide(0, false), "label-a", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"context", "whole-target", "object-source", "foreign-property", "excluded-lock", "group"} {
		deck := cloneDeck(d)
		copy := snapshot.Clone()
		command := nativeEditorAction{Action: "paste-object-style", StyleContext: "shape-label", Slide: 0, ObjectIDs: []string{"label-b", "plain"}, StyleData: copy}
		switch scenario {
		case "context":
			copy.Context = "unknown"
		case "whole-target":
			command.StyleContext = ""
		case "object-source":
			copy.Context = ""
		case "foreign-property":
			copy.Fields["paint.opacity"] = ObjectStyleField{Present: true, Value: ".5"}
		case "excluded-lock":
			deck.Slides[0].Elements[2].Query = setQueryValue(deck.Slides[0].Elements[2].Query, "object-locked", "1")
		case "group":
			deck.Slides[0].Elements[1].Query = setQueryValue(deck.Slides[0].Elements[1].Query, "group", "closed")
		}
		before := cloneDeck(deck)
		if changed, err := applyObjectStyle(&deck, command, ""); err == nil || changed || !reflect.DeepEqual(before, deck) {
			t.Fatal("invalid label context partially applied", scenario, err)
		}
	}
	clone := snapshot.Clone()
	clone.Fields["text.size"] = ObjectStyleField{Present: true, Value: "99"}
	if snapshot.Fields["text.size"].Value == "99" || clone.Context != "shape-label" {
		t.Fatal("style clone aliases or lost context")
	}
}

func TestShapeLabelResetRestoresMasterStyleKeepsOwnText(t *testing.T) {
	d := labelledSelectionFixture(t)
	master := d.Slides[0].Elements[0]
	master.ID, master.SlotID, master.PlaceholderRole = "label-master", "label-slot", "body"
	d.EnsureDefaultMasters()
	d.Masters.Layouts = append(d.Masters.Layouts, MasterLayout{ID: "labels", Name: "Labels", Slide: Slide{Elements: []Element{master}}})
	_, labelData, _ := sceneEditableShapeLabel(master)
	labelData.Text = "My own text"
	labelData.Query = setQueryValue(labelData.Query, "modern-size", "100")
	raw, _ := json.Marshal(labelData)
	d.Slides[0] = Slide{LayoutID: "labels", Elements: []Element{{ID: "instance", MasterSlotID: "label-slot", Kind: "shape"}}}
	// Build the query with the normal percent-escaping helper.
	d.Slides[0].Elements[0].Query = setQueryValue("master-clear=label-modern-font,shadow", "shape-label", base64.StdEncoding.EncodeToString(raw))
	if _, err := applyObjectStyle(&d, nativeEditorAction{Action: "reset-object-style", StyleContext: "shape-label", Slide: 0, ObjectIDs: []string{"instance"}}, ""); err != nil {
		t.Fatal(err)
	}
	if mustQuery(d.Slides[0].Elements[0].Query).Get("master-clear") != "shadow" {
		t.Fatal("Reset removed shape clear or retained label suppression")
	}
	check := func(size string) {
		t.Helper()
		label, ok := shapeLabel(d.ResolveSlide(0, false).Elements[0])
		if !ok || label.Text != "My own text" || mustQuery(label.Query).Get("modern-size") != size || mustQuery(label.Query).Get("modern-font") != "mono" {
			t.Fatal("Reset failed independent master style/content inheritance", label)
		}
	}
	check("40")
	masterQ := mustQuery(d.Masters.Layouts[d.Masters.LayoutIndex("labels")].Slide.Elements[0].Query)
	data, _ := decodeShapeLabelData(masterQ.Get("shape-label"))
	data.Query = setQueryValue(data.Query, "modern-size", "50")
	raw, _ = json.Marshal(data)
	d.Masters.Layouts[d.Masters.LayoutIndex("labels")].Slide.Elements[0].Query = setQueryValue(masterQ.Encode(), "shape-label", base64.StdEncoding.EncodeToString(raw))
	check("50")
	snapshot, err := captureShapeLabelStyle(d, d.Slides[0], d.ResolveSlide(0, false), "instance", "")
	if err != nil || snapshot.Fields["text.font"].Value != "mono" {
		t.Fatal("master's explicit label font was captured as a theme font", err, snapshot)
	}
	file := filepath.Join(t.TempDir(), "Inherited labels.md")
	if err := saveDeck(file, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(file)
	if err != nil {
		t.Fatal(err)
	}
	d = loaded
	if mustQuery(d.Slides[0].Elements[0].Query).Get("label-style-inherit") != "1" {
		t.Fatal("Markdown lost independent label inheritance")
	}
	check("50")
}

func TestLegacyShapeLabelDoesNotAcquireMasterStyleImplicitly(t *testing.T) {
	baseData, _ := json.Marshal(shapeLabelData{Text: "Master", Query: "modern-font=mono&modern-size=40"})
	ownData, _ := json.Marshal(shapeLabelData{Text: "Existing text", Query: "render=truetype"})
	base := setQueryValue("", "shape-label", base64.StdEncoding.EncodeToString(baseData))
	owned := setQueryValue("", "shape-label", base64.StdEncoding.EncodeToString(ownData))
	merged := mergeQueries(base, owned)
	data, err := decodeShapeLabelData(mustQuery(merged).Get("shape-label"))
	if err != nil || data.Text != "Existing text" || mustQuery(data.Query).Has("modern-size") || mustQuery(data.Query).Has("modern-font") {
		t.Fatal("legacy nested label was silently restyled", err, data)
	}
	owned = setQueryValue(owned, "label-style-inherit", "1")
	data, err = decodeShapeLabelData(mustQuery(mergeQueries(base, owned)).Get("shape-label"))
	if err != nil || data.Text != "Existing text" || mustQuery(data.Query).Get("modern-size") != "40" {
		t.Fatal("explicit label inheritance did not opt in", err, data)
	}
}

func TestShapeLabelStyleCrossDeckFallbackAndIndependentAbsentFlags(t *testing.T) {
	source := labelledSelectionFixture(t)
	if _, err := applySelectionProperty(&source, nativeEditorAction{Slide: 0, ObjectIDs: []string{"label-a"}, Property: "label.colorToken", PropertyValue: json.RawMessage(`"accent-4"`)}, ""); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureShapeLabelStyle(source, source.Slides[0], source.ResolveSlide(0, false), "label-a", "")
	if err != nil {
		t.Fatal(err)
	}
	target := labelledSelectionFixture(t)
	target.Theme = nil
	if _, err := applyObjectStyle(&target, nativeEditorAction{Action: "paste-object-style", StyleContext: "shape-label", Slide: 0, ObjectIDs: []string{"label-b"}, StyleData: snapshot}, ""); err != nil {
		t.Fatal(err)
	}
	q := mustQuery(target.Slides[0].Elements[1].Query)
	if q.Has("label-theme-fg") || q.Get("label-fg") != source.Theme.Colors.Accent[3] {
		t.Fatal("unavailable label role didn't use captured RGB", q)
	}
	// An absent emphasis field restores that property's base independently;
	// a simultaneous explicit underline must not prevent inherited italic.
	owner := &target.Slides[0].Elements[1]
	data, _ := decodeShapeLabelData(q.Get("shape-label"))
	data.Query = setQueryValue(data.Query, "modern-runs", "")
	runs, err := encodeModernRuns(data.Text, "", "text", []sceneRun{{Text: data.Text, Italic: true}})
	if err != nil {
		t.Fatal(err)
	}
	data.Query = setQueryValue(data.Query, "modern-runs", runs)
	raw, _ := json.Marshal(data)
	owner.Query = setQueryValue(owner.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
	owner.Query = setQueryValue(owner.Query, "label-text-italic", "0")
	partial := &ObjectStyleSnapshot{Version: 1, Context: "shape-label", Fields: map[string]ObjectStyleField{"text.italic": {}, "text.underline": {Present: true, Value: "true"}}}
	if _, err := applyObjectStyle(&target, nativeEditorAction{Action: "paste-object-style", StyleContext: "shape-label", Slide: 0, ObjectIDs: []string{"label-b"}, StyleData: partial}, ""); err != nil {
		t.Fatal(err)
	}
	label, _ := shapeLabel(target.Slides[0].Elements[1])
	for _, run := range sceneTextFor(label).Runs {
		if !run.Italic || !run.Underline {
			t.Fatal("absent italic failed independent inheritance", run)
		}
	}
}
