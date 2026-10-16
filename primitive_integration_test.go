package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Cross feature boundaries with the same production commands used by the
// editor, not synthetic registry mutation. All artwork is project-owned.
func TestMixedPrimitiveClipboardGroupHistoryAndReopen(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	source := powerupDenseEditingDeck(t)
	chartData := source.Objects.Charts["dense-chart"]
	chartData.Options.FontRole, chartData.Options.FontID = "heading", "mono"
	chartData.Series[0].LabelColor = "theme:accent-6"
	source.Objects.Charts["dense-chart"] = chartData
	tableData := source.Objects.Tables["table-dense"]
	tableData.Cells[0][0].Text = "A"
	tableData.Cells[0][0].Runs = []ObjectTextRun{{Text: "A", FontRole: "body", FontID: "sans", ColorToken: "accent-5", Color: "#123456"}}
	source.Objects.Tables["table-dense"] = tableData
	source.Slides[0].Elements = append(source.Slides[0].Elements,
		Element{ID: "heading", Kind: "heading", Level: 2, Text: "Heading", Query: "top=1&left=5&width=60&height=8&render=truetype"},
		Element{ID: "bullet", Kind: "bullet", Text: "A list item", Query: "top=9&left=5&width=60&height=8&render=truetype"},
		Element{ID: "code", Kind: "code", Text: "print(42)", Query: "top=17&left=5&width=60&height=8&render=truetype"})
	for i := range source.Slides[0].Elements {
		source.Slides[0].Elements[i].Query = setQueryValue(source.Slides[0].Elements[i].Query, "group", "source-group")
	}
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"slide", "tab", "base", "layout"} {
		t.Run(mode, func(t *testing.T) {
			dest := Deck{Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "unused-a"}, {ID: "unused-b"}, {ID: "destination"}}, Masters: defaultMasterDeck()}
			if mode == "tab" {
				dest.Slides[2].TabID = "destination-tab"
				syncSlideTabs(&dest)
			}
			s := newNativeEditorSession("Integration.md", dest)
			s.current = 2
			if mode == "base" || mode == "layout" {
				s.masterMode = true
				if mode == "layout" {
					s.currentMaster = 3
				}
				*masterSlideAt(&s.deck, s.currentMaster) = Slide{}
			}
			owner := func() *Slide {
				if s.masterMode {
					return masterSlideAt(&s.deck, s.currentMaster)
				}
				return &s.deck.Slides[s.current]
			}
			command := func(a nativeEditorAction) {
				t.Helper()
				revision := s.version
				a.SceneRevision, a.SceneMaster, a.Slide = &revision, s.masterMode, s.current
				if s.masterMode {
					a.Slide = s.currentMaster
				}
				if err := s.apply(a); err != nil {
					t.Fatalf("%s: %v", a.Action, err)
				}
			}
			command(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle})
			if len(owner().Elements) != len(bundle.Elements) || len(s.undo) != 1 {
				t.Fatal("paste was not one complete transaction")
			}
			ids := []string{}
			group := elementGroup(owner().Elements[0])
			if group == "" || group == "source-group" {
				t.Fatal("group identity shared with source")
			}
			for _, e := range owner().Elements {
				if elementGroup(e) != group {
					t.Fatal("mixed paste lost group membership")
				}
				ids = append(ids, e.ID)
			}
			beforeStyle := cloneDeck(s.deck)
			command(nativeEditorAction{Action: "set-selection-property", ObjectIDs: ids, Property: "paint.opacity", PropertyValue: json.RawMessage(`0.625`)})
			if len(s.undo) != 2 {
				t.Fatal("group style was not one transaction")
			}
			for _, e := range owner().Elements {
				q, _ := url.ParseQuery(e.Query)
				if q.Get("modern-opacity") != "0.625" {
					t.Fatal("mixed group opacity omitted", e.Kind, e.Query)
				}
			}
			styled := cloneDeck(s.deck)
			command(nativeEditorAction{Action: "undo"})
			if !reflect.DeepEqual(s.deck, beforeStyle) {
				t.Fatal("style Undo lost structured/media/group state")
			}
			command(nativeEditorAction{Action: "redo"})
			if !reflect.DeepEqual(s.deck, styled) {
				t.Fatal("style Redo drifted")
			}
			first := owner().Elements[0]
			command(nativeEditorAction{Action: "enter-group", ElementData: &first})
			var table, chart, text Element
			for _, e := range owner().Elements {
				switch e.Kind {
				case "table":
					table = e
				case "chart":
					chart = e
				case "text":
					text = e
				}
			}
			command(nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{text.ID}, Property: "text.size", PropertyValue: json.RawMessage(`37`)})
			for _, e := range []Element{table, chart} {
				command(nativeEditorAction{Action: "set-selection-geometry", ObjectIDs: []string{e.ID}, Geometry: &sceneRect{X: 10.125, Y: 12.25, Width: 100.5, Height: 30.75}, GeometryResize: true})
			}
			beforeSave := cloneDeck(s.deck)
			data, err := serializeDeck("Integration.md", s.deck)
			if err != nil {
				t.Fatal("save", err)
			}
			if root := os.Getenv("KEYNOPE_PRIMITIVE_EVIDENCE"); root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, mode+".md"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := parseDeckData("Integration.md", data)
			if err != nil {
				t.Fatal("reopen", err)
			}
			if !reflect.DeepEqual(s.deck, beforeSave) {
				t.Fatal("save mutated live deck")
			}
			if err := validateDeckObjectReferences(loaded); err != nil {
				t.Fatal("reopen references", err)
			}
			if mode == "tab" && (loaded.Slides[2].TabID != "destination-tab" || len(loaded.Tabs) != 1) {
				t.Fatal("tab ownership/order lost")
			}
			projection := func(deck Deck) slideScene {
				t.Helper()
				index := s.current
				visible := deck.ResolveSlide(index, false)
				if s.masterMode {
					index = s.currentMaster
					visible = masterViewPreview(deck.Masters, index)
				}
				scene, err := buildStyledSlideScene(deck, visible, index, 245, 56, true)
				if err != nil {
					t.Fatal(err)
				}
				return scene
			}
			originalScene, reopenedScene := projection(s.deck), projection(loaded)
			if !reflect.DeepEqual(originalScene, reopenedScene) {
				t.Fatal("save/reopen changed mixed scene", mode, fmt.Sprint(len(originalScene.Objects), len(reopenedScene.Objects)))
			}
			command(nativeEditorAction{Action: "undo"})
			command(nativeEditorAction{Action: "redo"})
			if !reflect.DeepEqual(s.deck, beforeSave) {
				t.Fatal("geometry Undo/Redo lost primitive ownership")
			}
			var roundtripOwner Slide
			if s.masterMode {
				roundtripOwner = *masterSlideAt(&loaded, s.currentMaster)
			} else {
				roundtripOwner = loaded.Slides[s.current]
			}
			secondBundle, err := makeObjectClipboard(loaded, roundtripOwner.Elements)
			if err != nil || len(secondBundle.Elements) != len(bundle.Elements) {
				t.Fatal("reopened objects cannot be copied", err)
			}
			if err := secondBundle.Validate(); err != nil {
				t.Fatal("reopened clipboard invalid", err)
			}
			// Reopen into a fresh editor and actually mutate each semantic owner,
			// rather than equating a visible scene with continued editability.
			s = newNativeEditorSession("Reopened.md", loaded)
			s.current = 2
			if mode == "base" || mode == "layout" {
				s.masterMode = true
				if mode == "layout" {
					s.currentMaster = 3
				}
			}
			member := owner().Elements[0]
			command(nativeEditorAction{Action: "enter-group", ElementData: &member})
			reopenedBefore := cloneDeck(s.deck)
			var edits []nativeEditorAction
			for index, e := range owner().Elements {
				switch e.Kind {
				case "table":
					table := cloneTable(s.deck.Objects.Tables[e.ObjectID])
					table.Cells[0][0].Text = "Reopened table"
					table.Cells[0][0].Runs = nil
					edits = append(edits, nativeEditorAction{Action: "set-table", ObjectID: e.ID, TableData: &table})
				case "chart":
					chart := cloneChart(s.deck.Objects.Charts[e.ObjectID])
					chart.Series[0].Values[0] = 42
					edits = append(edits, nativeEditorAction{Action: "set-chart-data", ObjectID: e.ID, ChartData: &chart})
				case "image":
					edits = append(edits, nativeEditorAction{Action: "set-scene-crop", ObjectID: e.ID, Crop: &sceneCrop{Left: .1}})
				case "text", "heading", "bullet", "code":
					edits = append(edits, nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{e.ID}, Property: "text.size", PropertyValue: json.RawMessage(`41`)})
				case "shape":
					edits = append(edits, nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{e.ID}, Property: "paint.strokeWidth", PropertyValue: json.RawMessage(`1.375`)})
					if _, _, err := sceneEditableShapeLabel(e); err == nil {
						edits = append(edits, nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{e.ID}, Property: "label.size", PropertyValue: json.RawMessage(`43`)})
					}
					q := mustQuery(e.Query)
					if definition, ok := shapeDefinitionFor(shapeSpecFromValues(q).Kind); ok && len(definition.Adjustments) > 0 {
						a := definition.Adjustments[0]
						value := a.Min + (a.Max-a.Min)*.63
						edits = append(edits, nativeEditorAction{Action: "set-shape-adjustment", ObjectID: e.ID, ShapeAdjustment: &shapeAdjustmentEdit{Key: a.Key, Value: value}})
					}
				case "connector":
					updated := cloneElement(e)
					q := mustQuery(updated.Query)
					q.Set("connector-width", "2.25")
					q.Set("connector-arrow-width", "7.5")
					q.Set("connector-arrows", "both")
					updated.Query = q.Encode()
					edits = append(edits, nativeEditorAction{Action: "update-elements", ElementIndices: []int{index}, ElementsData: []Element{updated}})
				default:
					t.Fatalf("extend the post-reopen edit matrix for %q", e.Kind)
				}
			}
			for _, edit := range edits {
				revision := s.version
				command(edit)
				if s.version != revision+1 {
					t.Fatal("reopened edit did not commit exactly once", edit.Action, edit.Property)
				}
			}
			editedAgain := cloneDeck(s.deck)
			if len(s.undo) != len(edits) {
				t.Fatal("reopened edit missing history")
			}
			for range edits {
				command(nativeEditorAction{Action: "undo"})
			}
			if !reflect.DeepEqual(s.deck, reopenedBefore) {
				t.Fatal("reopened Undo did not restore full deck")
			}
			for range edits {
				command(nativeEditorAction{Action: "redo"})
			}
			if !reflect.DeepEqual(s.deck, editedAgain) {
				t.Fatal("reopened Redo did not restore every primitive edit")
			}
			resaved, err := serializeDeck("Edited again.md", s.deck)
			if err != nil {
				t.Fatal("resave edited objects", err)
			}
			reloaded, err := parseDeckData("Edited again.md", resaved)
			if err != nil {
				t.Fatal("reopen edited objects", err)
			}
			if !reflect.DeepEqual(projection(s.deck), projection(reloaded)) {
				t.Fatal("second reopen lost edited primitive scene")
			}
			if root := os.Getenv("KEYNOPE_PRIMITIVE_EVIDENCE"); root != "" {
				if err := os.WriteFile(filepath.Join(root, mode+"-edited.md"), resaved, 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("%s: %d reopened objects, %d committed edits, full Undo/Redo and second-reopen scene parity", mode, len(owner().Elements), len(edits))
		})
	}
}
