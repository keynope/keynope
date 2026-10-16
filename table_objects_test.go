package main

import (
	"math"
	"net/url"
	"reflect"
	"testing"
)

func TestTableCommandNormalMasterOwnershipAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := capabilityFixture()
		if master {
			s.masterMode = true
			s.currentMaster = 0
		}
		before := cloneDeck(s.deck)
		rev := s.version
		action := nativeEditorAction{Action: "insert-table", Slide: 0, SceneMaster: master, SceneRevision: &rev, Rows: 3, Cols: 4}
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		if len(s.undo) != 1 || s.version != rev+1 {
			t.Fatal("insert must be one history entry")
		}
		state := s.state()
		if state.SelectedTable == nil || len(state.SelectedTable.Data.Columns) != 4 {
			t.Fatal("selected owned data missing")
		}
		state.SelectedTable.Data.Cells[0][0].Text = "Heading"
		if s.deck.Objects.Tables[state.Slides[0].Elements[s.selected].ObjectID].Cells[0][0].Text != "" {
			t.Fatal("state aliased archive")
		}
		rev = s.version
		action = nativeEditorAction{Action: "set-table", Slide: 0, SceneMaster: master, SceneRevision: &rev, ObjectID: state.SelectedTable.ObjectID, TableData: &state.SelectedTable.Data}
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		if len(s.undo) != 2 {
			t.Fatal("edit must be one history entry")
		}
		state.SelectedTable.Data.Cells[0][0].Text = "Caller mutation"
		if !reflect.DeepEqual(after, s.deck) {
			t.Fatal("edit aliases caller")
		}
		next := s.state().SelectedTable
		rev = s.version
		action.SceneRevision = &rev
		action.TableData = &next.Data
		if err := s.apply(action); err != nil || s.version != rev || len(s.undo) != 2 {
			t.Fatal("same data not a no-op", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("redo failed", err)
		}
	}
}

func TestTableContentFitCommitsPayloadAndBoundsAtomically(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("fit.md", structuredFixture(), false, false)
		s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		s.deck.Masters.Base.Slide.ID = "fit-master"
		s.masterMode, s.currentMaster = master, 0
		s.selected, s.selection = 0, map[int]bool{0: true}
		before, revision := cloneDeck(s.deck), s.version
		table := cloneTable(s.deck.Objects.Tables["table-1"])
		table.Rows[0].Height = 60
		table.Rows[1].Height = 120
		geometry := sceneRect{X: 20.25, Y: 4.5, Width: 120, Height: 40}
		action := nativeEditorAction{Action: "set-table", Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectID: "table-element", TableData: &table, Geometry: &geometry}
		if err := s.apply(action); err != nil {
			t.Fatal(master, err)
		}
		current := s.deck.Slides[0]
		if master {
			current = s.deck.Masters.Base.Slide
		}
		if len(s.undo) != 1 || s.version != revision+1 || !reflect.DeepEqual(explicitSelectionGeometry(current.Elements[0]), &geometry) || !reflect.DeepEqual(current.Elements[1], before.Slides[0].Elements[1]) {
			t.Fatal("fit must change only the table in one transaction", master, current)
		}
		fitted := cloneDeck(s.deck)
		encoded, err := serializeDeck("fit.md", fitted)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseDeckData("fit.md", encoded)
		if err != nil {
			t.Fatal(err)
		}
		reopenedSlide := reopened.Slides[0]
		if master {
			reopenedSlide = reopened.Masters.Base.Slide
		}
		reopenedElement := reopenedSlide.Elements[0]
		if !reflect.DeepEqual(explicitSelectionGeometry(reopenedElement), &geometry) || !reflect.DeepEqual(reopened.Objects.Tables[reopenedElement.ObjectID], table) {
			t.Fatal("fit save/reopen lost tracks or bounds", master, reopenedElement)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("fit undo lost original tracks/bounds", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, fitted) {
			t.Fatal("fit redo lost fitted tracks/bounds", err)
		}
		revision = s.version
		action.SceneRevision = &revision
		geometry.Height = 100 // Invalid fit must not publish new tracks either.
		table.Rows[0].Height = 200
		if err := s.apply(action); err == nil || !reflect.DeepEqual(s.deck, fitted) || s.version != revision {
			t.Fatal("rejected fit modified document", master, err)
		}
		geometry.Height = 42
		table = cloneTable(s.deck.Objects.Tables[current.Elements[0].ObjectID])
		if err := s.apply(action); err != nil || s.version != revision+1 {
			t.Fatal("bounds-only fit was ignored", master, err)
		}
	}
}

func TestTableCommandRejectsInvalidStaleLocked(t *testing.T) {
	s := newNativeEditorSession("table.md", structuredFixture(), false, false)
	s.selected = 0
	s.selection = map[int]bool{0: true}
	table := cloneTable(s.deck.Objects.Tables["table-1"])
	table.Cells[1][0].Text = "Edited"
	for _, kind := range []string{"stale", "missing", "locked", "invalid"} {
		before := cloneDeck(s.deck)
		rev := s.version
		a := nativeEditorAction{Action: "set-table", Slide: 0, SceneRevision: &rev, ObjectID: "table-element", TableData: &table}
		switch kind {
		case "stale":
			rev--
		case "missing":
			a.ObjectID = "gone"
		case "locked":
			s.deck.Slides[0].Elements[0].Query = "object-locked=1"
			before = cloneDeck(s.deck)
		case "invalid":
			bad := cloneTable(table)
			bad.Columns[0].Width = math.NaN()
			a.TableData = &bad
		}
		if err := s.apply(a); err == nil {
			t.Fatal("accepted", kind)
		}
		if !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 {
			t.Fatal("rejection mutated", kind)
		}
		s.deck.Slides[0].Elements[0].Query = ""
	}
}

func TestTableCommandSharedPayloadFork(t *testing.T) {
	d := structuredFixture()
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "other-table", Kind: "table", ObjectID: "table-1"})
	table := cloneTable(d.Objects.Tables["table-1"])
	table.Cells[0][0].Text = "Fork"
	changed, err := applyTableCommand(&d, nativeEditorAction{Action: "set-table", Slide: 0, ObjectID: "table-element", TableData: &table}, "")
	if err != nil || !changed {
		t.Fatal(err)
	}
	if d.Slides[0].Elements[0].ObjectID == "table-1" || d.Objects.Tables["table-1"].Cells[0][0].Text == "Fork" {
		t.Fatal("shared payload modified")
	}
}

func TestTableCommandForksRepeatedScopedElementIDs(t *testing.T) {
	d := structuredFixture()
	d.Slides = append(d.Slides, cloneSlide(d.Slides[0]))
	d.Slides[1].ID = "second-scope"
	before := cloneDeck(d)
	table := cloneTable(d.Objects.Tables["table-1"])
	table.Cells[0][0].Text = "Only first scope"
	changed, err := applyTableCommand(&d, nativeEditorAction{Action: "set-table", Slide: 0, ObjectID: "table-element", TableData: &table}, "")
	if err != nil || !changed {
		t.Fatal(err)
	}
	if d.Slides[0].Elements[0].ObjectID == "table-1" || !reflect.DeepEqual(d.Slides[1], before.Slides[1]) || !reflect.DeepEqual(d.Objects.Tables["table-1"], before.Objects.Tables["table-1"]) {
		t.Fatal("element IDs from distinct scopes collapsed payload ownership")
	}
}

func TestTableMasterEditPreservesNormalAndOtherMasterConsumers(t *testing.T) {
	for _, variant := range []struct {
		scope     int
		allShared bool
	}{{0, false}, {1, false}, {2, false}, {0, true}, {1, true}, {2, true}} {
		scope := variant.scope
		name := string(rune('0' + scope))
		if variant.allShared {
			name += "-all-shared"
		}
		t.Run(name, func(t *testing.T) {
			s := newNativeEditorSession("shared-table.md", structuredFixture(), false, false)
			s.deck.Masters.Base.Slide = Slide{ID: "shared-base", Elements: []Element{{ID: "master-table", Kind: "table", ObjectID: "table-1", Query: "left=20&top=5&width=120&height=30"}}}
			s.deck.Masters.Layouts = []MasterLayout{
				{ID: "shared-layout-one", Name: "One", Slide: Slide{ID: "layout-scope-one", Elements: []Element{{ID: "layout-table", Kind: "table", ObjectID: "table-1"}}}},
				{ID: "shared-layout-two", Name: "Two", Slide: Slide{ID: "layout-scope-two", Elements: []Element{{ID: "layout-table", Kind: "table", ObjectID: "table-1"}}}},
			}
			if !variant.allShared {
				for i := 0; i <= len(s.deck.Masters.Layouts); i++ {
					if i != scope {
						masterSlideAt(&s.deck, i).Elements = nil
					}
				}
			}
			s.masterMode, s.currentMaster = true, scope
			s.selected, s.selection = 0, map[int]bool{0: true}
			before, revision := cloneDeck(s.deck), s.version
			id := masterSlideAt(&s.deck, scope).Elements[0].ID
			table := cloneTable(s.deck.Objects.Tables["table-1"])
			table.Cells[0][0].Text = "Only selected master"
			box := sceneRect{X: 20.5, Y: 4.5, Width: 130, Height: 35}
			if err := s.apply(nativeEditorAction{Action: "set-table", Slide: scope, SceneMaster: true, SceneRevision: &revision, ObjectID: id, TableData: &table, Geometry: &box}); err != nil {
				t.Fatal(err)
			}
			selected := masterSlideAt(&s.deck, scope).Elements[0]
			if selected.ObjectID == "table-1" || !reflect.DeepEqual(s.deck.Slides, before.Slides) || !reflect.DeepEqual(s.deck.Objects.Tables["table-1"], before.Objects.Tables["table-1"]) || !reflect.DeepEqual(s.deck.Objects.Tables[selected.ObjectID], table) {
				t.Fatal("master edit mutated a shared normal/master consumer")
			}
			for i := 0; i <= len(s.deck.Masters.Layouts); i++ {
				if i != scope && !reflect.DeepEqual(*masterSlideAt(&s.deck, i), *masterSlideAt(&before, i)) {
					t.Fatal("unselected master changed", i)
				}
			}
			after := cloneDeck(s.deck)
			encoded, err := serializeDeck("shared-table.md", after)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("shared-table.md", encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reopened.Objects.Tables[selected.ObjectID], table) || !reflect.DeepEqual(reopened.Objects.Tables["table-1"], before.Objects.Tables["table-1"]) {
				t.Fatal("save/reopen lost separated table consumers")
			}
			if len(s.undo) != 1 || s.version != revision+1 || !reflect.DeepEqual(explicitSelectionGeometry(selected), &box) {
				t.Fatal("master fork and frame edit must be one transaction")
			}
			table.Cells[0][0].Text = "Caller changed"
			if !reflect.DeepEqual(s.deck, after) {
				t.Fatal("master payload aliases caller")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("master fork undo lost original consumers", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
				t.Fatal("master fork redo lost owned payload", err)
			}
		})
	}
}

func TestTableUniquePayloadRetainsIdentityInEveryScope(t *testing.T) {
	for _, scope := range []int{-1, 0, 2} {
		s := newNativeEditorSession("unique.md", structuredFixture(), false, false)
		if scope >= 0 {
			element := s.deck.Slides[0].Elements[0]
			element.ID = "unique-master-table"
			s.deck.Slides[0].Elements = s.deck.Slides[0].Elements[1:]
			masterSlideAt(&s.deck, scope).Elements = []Element{element}
			s.masterMode, s.currentMaster = true, scope
		}
		s.selected, s.selection = 0, map[int]bool{0: true}
		target := &s.deck.Slides[0]
		if scope >= 0 {
			target = masterSlideAt(&s.deck, scope)
		}
		element := target.Elements[0]
		table := cloneTable(s.deck.Objects.Tables[element.ObjectID])
		table.Cells[0][0].Text = "Same payload identity"
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-table", Slide: max(0, scope), SceneMaster: scope >= 0, SceneRevision: &revision, ObjectID: element.ID, TableData: &table}); err != nil {
			t.Fatal(scope, err)
		}
		if target.Elements[0].ObjectID != element.ObjectID || len(s.deck.Objects.Tables) != 1 {
			t.Fatal("unshared edit unnecessarily forked payload", scope)
		}
	}
}

func TestTableSceneTracksSpansStylesPersistence(t *testing.T) {
	table, _ := newTableData(2, 3)
	table.Rows[1].Height = 2
	table.Columns[1].Width = 2
	table.Cells[0][0] = TableCell{Text: "Combined", ColSpan: 2}
	table.Cells[0][1] = TableCell{Hidden: true}
	size, padding, border := 42.0, 6.0, 2.0
	table.Cells[1][1] = TableCell{Runs: []ObjectTextRun{{Text: "Rich", Bold: true, Color: "#aa55ff"}}, Style: &TableCellStyle{Fill: "#ff5555", Color: "#ffffff", FontID: "c64", FontSize: &size, Padding: &padding, BorderWidth: &border, Align: "center", Vertical: "middle"}}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"payload": table}}, Slides: []Slide{{Elements: []Element{{ID: "table", Kind: "table", ObjectID: "payload", Query: "left=10&top=5&width=120&height=24"}}}}}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var object *sceneObject
	for i := range scene.Objects {
		if scene.Objects[i].ID == "table" {
			object = &scene.Objects[i]
		}
	}
	if object == nil || object.Table == nil {
		t.Fatal("table missing from scene", scene.Diagnostics)
	}
	projection := object.Table
	if len(projection.Cells) != 5 || math.Abs(projection.Columns[0]*2-projection.Columns[1]) > 1e-8 || math.Abs(projection.Rows[0]*2-projection.Rows[1]) > 1e-8 {
		t.Fatal("tracks/spans incorrect", projection)
	}
	cell := projection.Cells[3]
	if cell.Row != 1 || cell.Column != 1 || cell.Text.Size != 42 || cell.Text.Align != "center" || cell.Text.Vertical != "middle" || cell.Fill != "#ff5555" || cell.ContentBounds.X != 6 || cell.Text.FontID != "c64" || !cell.Text.Runs[0].Bold {
		t.Fatal("cell style missing", cell)
	}
	encoded, err := serializeDeck("tables.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("tables.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Objects.Tables["payload"], table) {
		t.Fatal("payload persistence differs")
	}
	q, _ := url.ParseQuery(loaded.Slides[0].Elements[0].Query)
	if q.Get("width") != "120" {
		t.Fatal("table geometry lost")
	}
}

func TestTableWorkspacePayloadInvalidation(t *testing.T) {
	d := structuredFixture()
	d.Appearance = &DeckAppearance{Version: 3}
	d.Slides = append(d.Slides, Slide{Elements: []Element{{ID: "unrelated", Kind: "text", Text: "Unrelated"}}})
	var cache workspacePageCache
	if _, err := cache.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	before := cache.misses
	table := d.Objects.Tables["table-1"]
	table.Cells[1][0].Text = "Changed"
	d.Objects.Tables["table-1"] = table
	pages, err := cache.render(d, false, 0, 245, 56)
	if err != nil || cache.misses-before != 1 || !reflect.DeepEqual(cache.changed, []int{0}) {
		t.Fatal("payload edit did not invalidate exactly its consumer", err, cache.changed)
	}
	want, err := editorWorkspacePages(d, false, 0, 245, 56)
	if err != nil || !reflect.DeepEqual(want, pages) {
		t.Fatal("cache diverged", err)
	}
}

func TestTableForkInvalidatesOnlyEditedConsumer(t *testing.T) {
	d := structuredFixture()
	d.Appearance = &DeckAppearance{Version: 3}
	d.Slides = append(d.Slides, cloneSlide(d.Slides[0]))
	d.Slides[1].ID = "second-consumer"
	before := cloneDeck(d)
	var cache workspacePageCache
	if _, err := cache.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	misses := cache.misses
	table := cloneTable(d.Objects.Tables["table-1"])
	table.Cells[0][0].Text = "Only this instance"
	if changed, err := applyTableCommand(&d, nativeEditorAction{Action: "set-table", Slide: 0, ObjectID: "table-element", TableData: &table}, ""); err != nil || !changed {
		t.Fatal(err)
	}
	pages, err := cache.render(d, false, 0, 245, 56)
	if err != nil || cache.misses-misses != 1 || !reflect.DeepEqual(cache.changed, []int{0}) {
		t.Fatal("fork invalidated the untouched consumer", err, cache.changed)
	}
	if sceneProjectionInputsEqual(before, d, sceneResponseKey{index: 0}) || !sceneProjectionInputsEqual(before, d, sceneResponseKey{index: 1}) {
		t.Fatal("scene dependencies did not separate forked payloads")
	}
	want, err := editorWorkspacePages(d, false, 0, 245, 56)
	if err != nil || !reflect.DeepEqual(pages, want) {
		t.Fatal("forked cache diverged from cold rendering", err)
	}
}

func TestTableSceneDefaultsContrastAndAlignment(t *testing.T) {
	table, _ := newTableData(2, 2)
	table.Cells[0][0].Text = "Header"
	table.Cells[1][0].Text = "Right"
	table.Cells[1][0].Style = &TableCellStyle{Align: "right"}
	table.Cells[1][1].Style = &TableCellStyle{Align: "left", Fill: "none", BorderColor: "none"}
	for _, color := range []string{"#ffffff", "#111111"} {
		projection, err := (Deck{}).projectTable(table, sceneRect{Width: 400, Height: 160}, color)
		if err != nil {
			t.Fatal(err)
		}
		if projection.Cells[0].Text.Size != 48 || projection.Cells[0].Text.Vertical != "middle" || !projection.Cells[0].Text.Runs[0].Bold {
			t.Fatal("table readability defaults missing", projection.Cells[0])
		}
		if projection.Cells[2].Text.Align != "end" || projection.Cells[3].Text.Align != "start" {
			t.Fatal("alignment does not use shared text layout vocabulary")
		}
		want := "#314765"
		if color == "#111111" {
			want = "#d4e0f2"
		}
		if projection.Cells[0].Fill != want || projection.Cells[3].Fill != "none" || projection.Cells[3].BorderColor != "none" {
			t.Fatal("contrast defaults or explicit Off were lost")
		}
	}
}

func TestTableSceneRichRunsKeepEmojiFontsAndHeaderStyle(t *testing.T) {
	table, _ := newTableData(2, 1)
	table.Cells[0][0].Runs = []ObjectTextRun{{Text: "Welcome "}, {Text: "😀", Color: "#55aaff"}}
	table.Cells[1][0].Runs = []ObjectTextRun{{Text: "Crew "}, {Text: "👩🏽‍🚀", Italic: true}}
	projection, err := (Deck{}).projectTable(table, sceneRect{Width: 400, Height: 160}, "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	for i, emoji := range []string{"😀", "👩🏽‍🚀"} {
		cell := projection.Cells[i]
		if len(cell.Text.EmojiFonts) != 1 || cell.Text.EmojiFonts[emoji] == "" {
			t.Fatalf("cell %d lost its owned emoji font", i)
		}
		if len(cell.Text.Runs) != 2 || cell.Text.Runs[1].Text != emoji {
			t.Fatalf("cell %d lost rich runs: %+v", i, cell.Text.Runs)
		}
	}
	if !projection.Cells[0].Text.Runs[0].Bold || !projection.Cells[0].Text.Runs[1].Bold || projection.Cells[0].Text.Runs[1].Color != "#55aaff" || !projection.Cells[1].Text.Runs[1].Italic {
		t.Fatal("header emphasis or authored rich styling was lost")
	}
}

func TestTableTracksAvoidWeightOverflow(t *testing.T) {
	for _, weights := range [][]float64{{1e308, 1e308}, {1e-308, 1e-308}, {1, 1e308, 1}} {
		tracks := normalizedTableTracks(weights, 1920)
		total := 0.0
		for _, value := range tracks {
			if math.IsInf(value, 0) || math.IsNaN(value) || value < 0 {
				t.Fatal("invalid track from valid weights", weights, tracks)
			}
			total += value
		}
		if math.Abs(total-1920) > 1e-8 {
			t.Fatal("normalized tracks do not fill table", tracks)
		}
	}
}

func TestTableSceneResolvesAuthoredPlacement(t *testing.T) {
	table, _ := newTableData(1, 1)
	for _, tc := range []struct {
		query string
		x, y  float64
	}{
		{"left=10&top=5", 10, 5},
		{"left=30&top=12&object-offset-x=0.25&object-offset-y=0.5", 30.25, 12.5},
		{"align=center&valign=middle", 62.5, 16},
		{"right=3&bottom=2", 122, 30},
		{"left_pct=0.25&top=2", 61, 2},
	} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"data": table}}, Slides: []Slide{{Elements: []Element{{ID: "table", Kind: "table", ObjectID: "data", Query: "width=120&height=24&" + tc.query}}}}}
		scene, err := buildSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		var got sceneRect
		for _, o := range scene.Objects {
			if o.ID == "table" {
				got = o.Bounds
			}
		}
		if math.Abs(got.X-tc.x*1920/245) > .00001 || math.Abs(got.Y-tc.y*1080/56) > .00001 {
			t.Fatalf("%s: table did not follow authored anchor: %#v", tc.query, got)
		}
	}
}
