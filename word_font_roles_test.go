package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func wordFontFixture(t *testing.T) Deck {
	d := wordThemeFixture(t)
	d.Theme.Fonts.Heading.FontID = "c64"
	for i := range d.Slides {
		d.Slides[i].Elements[0] = mapWordColorRuns(d.Slides[i].Elements[0], func(run sceneRun) sceneRun {
			run.FontRole, run.FontID, run.FontSize, run.Bold = "heading", "mono", 80, true
			return run
		})
	}
	table := tableWordThemeFixture(t)
	data := table.Objects.Tables["table-word-data"]
	data.Cells[0][1].Runs[0].FontRole, data.Cells[0][1].Runs[0].FontID = "heading", "mono"
	d.Objects = table.Objects
	d.Objects.Tables["table-word-data"] = data
	d.Slides = append(d.Slides, table.Slides[0])
	return d
}

func TestWordFontRolesProjectionConsumersAndPersistence(t *testing.T) {
	d := wordFontFixture(t)
	before := cloneDeck(d)
	for index := range d.Slides {
		for _, family := range []string{"c64", "sans"} {
			d.Theme.Fonts.Heading.FontID = family
			warm, err := buildSlideScene(d, index, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			text := warm.Objects[0].Text
			if text == nil {
				text = warm.Objects[0].Label
			}
			if text == nil {
				text = warm.Objects[0].Table.Cells[1].Text
			}
			run := firstWordRun(text)
			if run.FontRole != "heading" || run.FontID != family || text.FontRoles["heading"] != family {
				t.Fatal("stale font role projection", index, run, text.FontRoles)
			}
			if index < 4 {
				annotateSceneEditing(&warm, d, d.Slides[index].Elements)
				if warm.Objects[0].EditRuns[0].FontRole != "heading" || warm.Objects[0].EditRuns[0].FontID != "mono" {
					t.Fatal("projection baked fallback", index, warm.Objects[0].EditRuns)
				}
			}
			clearSceneTextObjects()
			cold, err := buildSlideScene(d, index, 245, 56)
			if err != nil || !reflect.DeepEqual(warm.Objects[0].Text, cold.Objects[0].Text) || !reflect.DeepEqual(warm.Objects[0].Table, cold.Objects[0].Table) || !reflect.DeepEqual(warm.Objects[0].Label, cold.Objects[0].Label) {
				t.Fatal("warm font projection differs from cold", index, err)
			}
		}
		unused := cloneDeck(d)
		unused.Theme.Fonts.Mono.FontID = "sans"
		if !sceneThemeInputsEqual(d, unused, d.Slides[index], sceneResponseKey{cols: 245, rows: 56}) {
			t.Fatal("unused word font invalidated scene", index)
		}
		used := cloneDeck(d)
		used.Theme.Fonts.Heading.FontID = "mono"
		if sceneThemeInputsEqual(d, used, d.Slides[index], sceneResponseKey{cols: 245, rows: 56}) {
			t.Fatal("word font role missing from consumers", index)
		}
	}
	if !reflect.DeepEqual(d.Slides, before.Slides) || !reflect.DeepEqual(d.Objects, before.Objects) {
		t.Fatal("projection mutated document")
	}
	d.Theme.Fonts.Heading.FontID = "c64"
	md, err := serializeDeck("Word fonts.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Word fonts.md", md)
	if err != nil || !reflect.DeepEqual(loaded.Objects, d.Objects) {
		t.Fatal("font roles lost on reopen", err)
	}
	for i := range d.Slides {
		canonical := func(elements []Element) []Element {
			copy := append([]Element(nil), elements...)
			for j := range copy {
				copy[j].Query = mustQuery(copy[j].Query).Encode()
			}
			return copy
		}
		if !reflect.DeepEqual(canonical(loaded.Slides[i].Elements), canonical(d.Slides[i].Elements)) {
			t.Fatal("font runs changed on reopen", i, loaded.Slides[i].Elements, d.Slides[i].Elements)
		}
	}
	if root := os.Getenv("KEYNOPE_WORD_FONT_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "font-roles.md"), md, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWordFontRoleSnapshotsTypedDraftAndFallback(t *testing.T) {
	d := wordFontFixture(t)
	runs := []sceneRun{{Text: "Role", FontRole: "heading", FontID: "mono", FontSize: 80, Italic: true}}
	base, err := captureTextRangeStyle(runs, &textStyleRange{0, 4})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := wordSnapshotInDeck(d, base)
	if snapshot.Fields["text.font"].Value != "theme:heading" || snapshot.Fields["text.font"].Fallback != "c64" || base.Fields["text.font"].Fallback != "mono" {
		t.Fatal("font role capture/fallback aliases source", snapshot, base)
	}
	for _, themed := range []bool{false, true} {
		destination := Deck{Appearance: &DeckAppearance{Version: 3}}
		if themed {
			destination.Theme = builtinDeckTheme("clean-dark")
			destination.Theme.Fonts.Heading.FontID = "sans"
		}
		adapted := wordSnapshotForDestination(destination, snapshot)
		got, _, err := applyTextRangeStyle([]sceneRun{{Text: "Target"}}, &textStyleRange{0, 6}, adapted, false, false)
		if err != nil || got[0].FontID != "c64" || themed && got[0].FontRole != "heading" || !themed && got[0].FontRole != "" {
			t.Fatal("word font fallback mapping", themed, got, err)
		}
	}
	for _, value := range []string{"theme:heading", "theme:body", "theme:mono", "sans", ""} {
		s := newNativeEditorSession("Fonts.md", d)
		version := s.version
		raw, _ := json.Marshal(value)
		a := nativeEditorAction{Action: "set-selection-property", StyleContext: "text-range", SceneRevision: &version, ObjectID: "word-text", TextRuns: runs, TextRange: &textStyleRange{0, 4}, Property: "text.font", PropertyValue: raw}
		w := tableWordStyleCall(t, s, a, nil)
		if w.Code != 200 {
			t.Fatal(value, w.Code, w.Body.String())
		}
		var result struct {
			Runs  []sceneRun                `json:"textRuns"`
			Caps  *selectionCapabilityState `json:"selectionCapabilities"`
			Fonts map[string]string         `json:"fontRoles"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Runs[0].FontRole != wordFontRole(value) || result.Runs[0].FontSize != 80 || !result.Runs[0].Italic || s.version != version || len(s.undo) != 0 {
			t.Fatal("font role draft changed scope", value, result)
		}
		if wordFontRole(value) != "" && (result.Caps.Properties["text.font"].Provenance != "theme" || !validBundledFont(result.Fonts[wordFontRole(value)])) {
			t.Fatal("font role capability/palette missing", result)
		}
	}
	fixed, _, err := applyTextRangeStyle(runs, &textStyleRange{0, 4}, &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{"text.font": {Present: true, Value: "sans"}}}, false, false)
	if err != nil || fixed[0].FontRole != "" || fixed[0].FontID != "sans" {
		t.Fatal("fixed font didn't unlink role", fixed, err)
	}
	reset, _, err := applyTextRangeStyle(runs, &textStyleRange{0, 4}, nil, false, true)
	if err != nil || reset[0].FontRole != "" || reset[0].FontID != "" {
		t.Fatal("reset retained font role", reset, err)
	}
	roleOnly := &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{"text.font": snapshot.Fields["text.font"]}}
	noop, changed, err := applyTextRangeFontRole(runs, &textStyleRange{0, 4}, roleOnly)
	if err != nil || changed || !reflect.DeepEqual(noop, runs) {
		t.Fatal("same role rewrote portable fallback", noop, changed, err)
	}
	varied := []sceneRun{{Text: "A", FontRole: "heading", FontID: "mono"}, {Text: "B", FontRole: "heading", FontID: "sans"}}
	uniform, err := captureTextRangeStyle(d.wordFontSnapshotRuns(varied), &textStyleRange{0, 2})
	if err != nil || uniform.Fields["text.font"].Value != "theme:heading" || uniform.Fields["text.font"].Fallback != "c64" || varied[0].FontID != "mono" {
		t.Fatal("fallback differences split equal linked roles or baked draft", uniform, err)
	}
	varied[1].FontRole = "body"
	mixed, err := captureTextRangeStyle(d.wordFontSnapshotRuns(varied), &textStyleRange{0, 2})
	if _, exists := mixed.Fields["text.font"]; err != nil || exists {
		t.Fatal("different roles flattened", mixed, err)
	}
	for _, run := range []sceneRun{{Text: "X", FontRole: "unknown", FontID: "sans"}, {Text: "X", FontRole: "heading"}, {Text: "X", FontRole: "body", FontID: "remote"}} {
		if validSceneRun(run) {
			t.Fatal("invalid font role accepted", run)
		}
	}
	bad := snapshot.Clone()
	f := bad.Fields["text.font"]
	f.Fallback = ""
	bad.Fields["text.font"] = f
	if bad.Validate() == nil {
		t.Fatal("role without fallback accepted")
	}
	bad = snapshot.Clone()
	bad.Context = "objects"
	if bad.Validate() == nil {
		t.Fatal("word role accepted as whole-box font")
	}
}

func TestWordFontRoleCachePaletteOwnershipAndKnownFieldClear(t *testing.T) {
	d := wordFontFixture(t)
	clearSceneTextObjects()
	defer clearSceneTextObjects()
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	scene.Objects[0].Text.FontRoles["heading"] = "sans"
	scene.Objects[0].Text.Runs[0].FontID = "sans"
	again, err := buildSlideScene(d, 0, 245, 56)
	if err != nil || again.Objects[0].Text.FontRoles["heading"] != "c64" || again.Objects[0].Text.Runs[0].FontID != "c64" {
		t.Fatal("cached font palette aliases response", err)
	}
	var run ObjectTextRun
	if err := json.Unmarshal([]byte(`{"text":"Role","fontId":"mono","fontRole":"heading","future":true}`), &run); err != nil {
		t.Fatal(err)
	}
	run.FontRole = ""
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["fontRole"]; ok || fields["future"] != true {
		t.Fatal("role resurrected from extension or future field lost", string(raw))
	}
	before := d.Slides[0].Elements[0]
	after := before
	after.Text += "!"
	after = preserveRunStyles(before, after)
	stored := decodeModernRuns(mustQuery(after.Query).Get("modern-runs"))
	if stored == nil || stored.Runs[0].FontRole != "heading" || stored.Runs[0].FontID != "mono" {
		t.Fatal("text mutation lost linked role", stored)
	}
	for _, bad := range []ObjectTextRun{{Text: "X", FontRole: "unknown", FontID: "sans"}, {Text: "X", FontRole: "body"}, {Text: "X", FontRole: "mono", FontID: "remote"}} {
		data := cloneTable(d.Objects.Tables["table-word-data"])
		data.Cells[0][1].Runs = []ObjectTextRun{bad}
		if data.Validate() == nil {
			t.Fatal("invalid table font role accepted", bad)
		}
	}
}

func TestWordFontRoleMasterDraftAndSafePresentationSurfaces(t *testing.T) {
	d := wordFontFixture(t)
	before := cloneDeck(d)
	for index := range d.Slides {
		local, err := buildDocumentSlideScene(d, index, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		participant, err := participantRenderedDeck(d, index)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(publicParityScene(*participant.Pages[0].Scene), publicParityScene(local)) {
			t.Fatal("participant font projection differs", index)
		}
		md, err := participantSlideMarkdown(d, index)
		if err != nil {
			t.Fatal(err)
		}
		public, err := parseDeckData("Public.md", md)
		if err != nil {
			t.Fatal(err)
		}
		rebuilt, err := buildDocumentSlideScene(public, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(local.Objects[0].Text, rebuilt.Objects[0].Text) || !reflect.DeepEqual(local.Objects[0].Table, rebuilt.Objects[0].Table) || !reflect.DeepEqual(local.Objects[0].Label, rebuilt.Objects[0].Label) {
			t.Fatal("public Markdown lost current font role", index)
		}
		private := cloneDeck(d)
		private.Slides[index].Notes = "PRIVATE NOTES"
		private.Slides[index].EngagementResult = &EngagementResult{}
		data, err := participantSlideMarkdown(private, index)
		if err != nil || bytes.Contains(data, []byte("PRIVATE")) {
			t.Fatal("font projection disclosed notes", err)
		}
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("public font projection mutated source")
	}
	master := cloneDeck(d)
	master.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: cloneSlide(master.Slides[0])}}
	s := newNativeEditorSession("Master.md", master)
	s.masterMode = true
	s.currentMaster = 0
	version := s.version
	masterBefore := cloneDeck(s.deck)
	var owner Element
	for _, element := range s.deck.Masters.Base.Slide.Elements {
		if element.Kind == "text" && hasWordFontRoles(sceneTextFor(element)) {
			owner = element
			break
		}
	}
	if owner.ID == "" {
		t.Fatal("master fixture lacks linked text", s.deck.Masters.Base.Slide.Elements)
	}
	runs := sceneTextFor(owner).Runs
	a := nativeEditorAction{Action: "set-selection-property", StyleContext: "text-range", SceneMaster: true, SceneRevision: &version, ObjectID: owner.ID, TextRuns: runs, TextRange: &textStyleRange{0, 4}, Property: "text.font", PropertyValue: json.RawMessage(`"theme:body"`)}
	w := tableWordStyleCall(t, s, a, nil)
	if w.Code != 200 || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, masterBefore) {
		t.Fatal("master font draft crossed ownership or committed early", w.Code, w.Body.String())
	}
}

func TestWordFontRoleClipboardAndEditablePPTX(t *testing.T) {
	d := wordFontFixture(t)
	for index := range d.Slides {
		bundle, err := makeObjectClipboard(d, d.Slides[index].Elements)
		if err != nil {
			t.Fatal(err)
		}
		for _, themed := range []bool{false, true} {
			target := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}}
			if themed {
				target.Theme = builtinDeckTheme("clean-dark")
			}
			elements, _, objects, err := prepareObjectClipboardPaste(target, bundle)
			if err != nil {
				t.Fatal(err)
			}
			target.Slides[0].Elements, target.Objects = elements, objects
			scene, err := buildSlideScene(target, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			text := scene.Objects[0].Text
			if text == nil {
				text = scene.Objects[0].Label
			}
			if text == nil {
				text = scene.Objects[0].Table.Cells[1].Text
			}
			run := firstWordRun(text)
			expected := "c64"
			if themed {
				expected = "sans"
			}
			if run.FontID != expected || !themed && run.FontRole != "" || themed && run.FontRole != "heading" {
				t.Fatal("object clipboard font role", index, themed, run)
			}
		}
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err, report)
	}
	found := false
	for _, w := range report.Warnings {
		if w.Code == "word-theme-fonts-frozen" {
			found = true
		}
	}
	if !found {
		t.Fatal("PPTX didn't disclose frozen role")
	}
	imported, _, err := importPPTXDocument(data, "Fonts.pptx")
	if err != nil {
		t.Fatal(err)
	}
	for index := range imported.Slides {
		scene, err := buildSlideScene(imported, index, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, object := range scene.Objects {
			texts := []*sceneText{object.Text, object.Label}
			if object.Table != nil {
				for _, cell := range object.Table.Cells {
					texts = append(texts, cell.Text)
				}
			}
			for _, text := range texts {
				if text == nil {
					continue
				}
				for _, run := range effectiveTextRunes(text, "#ffffff") {
					if run.Text == "R" {
						found = true
						if run.FontID != "c64" || run.FontRole != "" {
							t.Fatal("PPTX role became wrong font or retained unsupported link", index, run)
						}
					}
				}
			}
		}
		if !found {
			t.Fatal("PPTX flattened/omitted role text", index)
		}
	}
}
