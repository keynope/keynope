package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func connectorSelectionFixture() Deck {
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "diagram", Elements: []Element{
		{ID: "from", Kind: "shape", Query: "shape=square&left=20&top=10&width=30&height=10"},
		{ID: "to", Kind: "shape", Query: "shape=rounded&left=160&top=30&width=30&height=10"},
		{ID: "line-a", Kind: "connector", Query: "connector-from=from&connector-from-side=right&connector-to=to&connector-to-side=left&connector-mode=elbow&connector-route=%5B%7B%22x%22%3A50%2C%22y%22%3A15%7D%2C%7B%22x%22%3A100%2C%22y%22%3A15%7D%2C%7B%22x%22%3A100%2C%22y%22%3A35%7D%2C%7B%22x%22%3A160%2C%22y%22%3A35%7D%5D"},
		{ID: "line-b", Kind: "connector", Query: "connector-from=from&connector-from-side=bottom&connector-to=to&connector-to-side=top&connector-width=3&connector-arrow-width=10&connector-arrows=end"},
	}}}}
}

func inheritedConnectorSelectionFixture() Deck {
	d := connectorSelectionFixture()
	template := cloneSlide(d.Slides[0])
	template.ID = "diagram-template"
	for i := range template.Elements {
		template.Elements[i].SlotID = "slot-" + template.Elements[i].ID
		template.Elements[i].Placeholder = true
		template.Elements[i].PlaceholderRole = placeholderBody
		d.Slides[0].Elements[i].MasterSlotID = template.Elements[i].SlotID
	}
	d.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{ID: "diagram-base", PageNumber: pageNumberHide}}, Layouts: []MasterLayout{{ID: "diagram-layout", Name: "Diagram", Slide: template}}}
	d.Slides[0].LayoutID = "diagram-layout"
	d.Slides[0].Elements[2].Query = removeImageQueryKeys(d.Slides[0].Elements[2].Query, "connector-mode", "connector-route")
	return d
}

func TestConnectorSelectionAutoRouteSuppressesMasterAndRestoresInheritance(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("Inherited.md", inheritedConnectorSelectionFixture())
	s.selected = 2
	s.selection = map[int]bool{2: true}
	p := s.transportState().SelectionCapabilities.Properties["line.routing"]
	if p.State != "value" || p.Value != "manual" || p.Provenance != "inherited" {
		t.Fatal("inherited route not reported", p)
	}
	before := cloneDeck(s.deck)
	// The previous local reset deleted only the child's route, which cannot
	// suppress bends inherited from its master. Keep this negative control.
	legacy := cloneDeck(before)
	legacy.Slides[0].Elements[2].Query = setQueryValue(removeImageQueryKeys(legacy.Slides[0].Elements[2].Query, "connector-route"), "connector-mode", "elbow")
	legacyStillManual := false
	for _, e := range legacy.ResolveSlide(0, false).Elements {
		if e.ID == "line-a" {
			legacyStillManual = mustQuery(e.Query).Has("connector-route")
		}
	}
	if !legacyStillManual {
		t.Fatal("fixture does not reproduce legacy reset defect")
	}
	revision := s.version
	action := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"line-a"}, Property: "line.routing", PropertyValue: json.RawMessage(`"automatic"`)}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	after := cloneDeck(s.deck)
	if !reflect.DeepEqual(before.Masters, after.Masters) {
		t.Fatal("route reset altered source master")
	}
	p = s.actionTransportState(action.Action).SelectionCapabilities.Properties[action.Property]
	if p.Value != "automatic" || p.Provenance != "explicit" {
		t.Fatal("manual route resurfaced", p, s.deck.Slides[0].Elements[2])
	}
	for _, e := range s.deck.ResolveSlide(0, false).Elements {
		if e.ID == "line-a" && mustQuery(e.Query).Has("connector-route") {
			t.Fatal("inherited manual bends survived reset")
		}
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("route reset undo", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
		t.Fatal("route reset redo", err)
	}
	revision = s.version
	if err := s.apply(action); err != nil || s.version != revision {
		t.Fatal("auto-route repeat not a no-op", err)
	}
	data, err := serializeDeck("Inherited.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Inherited.md", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range reopened.ResolveSlide(0, false).Elements {
		if e.ID == "line-a" && mustQuery(e.Query).Has("connector-route") {
			t.Fatal("save/reopen resurrected bends")
		}
	}
	action.PropertyValue = json.RawMessage(`""`)
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	p = s.actionTransportState(action.Action).SelectionCapabilities.Properties[action.Property]
	if p.Value != "manual" || p.Provenance != "inherited" {
		t.Fatal("inherit did not restore master bends", p)
	}
}

func TestConnectorSelectionInheritedRouteRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	data, err := serializeDeck("Inherited.md", inheritedConnectorSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	d, err := parseDeckData("Inherited.md", data)
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeEditorSession("Inherited.md", d)
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "line-a" {
			s.selected = i
			s.selection = map[int]bool{i: true}
		}
	}
	p := s.transportState().SelectionCapabilities.Properties["line.routing"]
	if p.State != "value" || p.Value != "manual" || p.Provenance != "inherited" {
		t.Fatalf("round-trip routing: %+v", p)
	}
	revision := s.version
	action := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"line-a"}, Property: "line.routing", PropertyValue: json.RawMessage(`"automatic"`)}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	data, err = serializeDeck("Inherited.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	d, err = parseDeckData("Inherited.md", data)
	if err != nil {
		t.Fatal(err)
	}
	s = newNativeEditorSession("Inherited.md", d)
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "line-a" {
			s.selected = i
			s.selection = map[int]bool{i: true}
		}
	}
	p = s.transportState().SelectionCapabilities.Properties["line.routing"]
	if p.Value != "automatic" || p.Provenance != "explicit" {
		t.Fatal("reopened reset", p)
	}
	revision = s.version
	action.PropertyValue = json.RawMessage(`""`)
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	p = s.transportState().SelectionCapabilities.Properties["line.routing"]
	if p.Value != "manual" || p.Provenance != "inherited" {
		t.Fatal("reopened inheritance", p)
	}
}

func TestConnectorSelectionSharedValuesHistoryAndScopes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		s := newNativeEditorSession("Diagram.md", connectorSelectionFixture())
		if mode == "tab" {
			s.deck.Slides[0].TabID = "diagram-tab"
			syncSlideTabs(&s.deck)
		}
		if mode == "base" || mode == "layout" {
			s.masterMode = true
			if mode == "layout" {
				s.currentMaster = 1
			}
			*masterSlideAt(&s.deck, s.currentMaster) = cloneSlide(s.deck.Slides[0])
			masterSlideAt(&s.deck, s.currentMaster).ID = "master-diagram-" + mode
		}
		s.selected, s.selection = 2, map[int]bool{0: true, 2: true, 3: true}
		caps := s.transportState().SelectionCapabilities
		for _, property := range []string{"line.width", "line.arrowWidth", "line.arrows", "line.path"} {
			p := caps.Properties[property]
			if p.State != "mixed" || p.Value != nil || !p.Editable || p.TotalCount != 3 || !reflect.DeepEqual(p.EligibleIDs, []string{"line-a", "line-b"}) {
				t.Fatal(mode, property, p)
			}
		}
		for _, edit := range []struct {
			property, raw string
			accepted      interface{}
		}{
			{"line.width", "2.5", 2.5}, {"line.arrowWidth", "7.5", 7.5}, {"line.arrows", `"both"`, "both"}, {"line.path", `"straight"`, "straight"},
			{"line.width", `""`, 1.0}, {"line.arrowWidth", `""`, 6.0}, {"line.arrows", `""`, "none"}, {"line.path", `""`, "straight"},
		} {
			before, revision, depth := cloneDeck(s.deck), s.version, len(s.undo)
			action := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: s.masterMode, SceneRevision: &revision, ObjectIDs: caps.TargetIDs, Property: edit.property, PropertyValue: json.RawMessage(edit.raw)}
			if s.masterMode {
				action.Slide = s.currentMaster
			}
			if err := s.apply(action); err != nil {
				t.Fatal(mode, edit, err)
			}
			p := s.actionTransportState(action.Action).SelectionCapabilities.Properties[edit.property]
			if p.State != "value" || !reflect.DeepEqual(p.Value, edit.accepted) || p.Provenance != map[bool]string{true: "inherited", false: "explicit"}[edit.raw == `""`] {
				t.Fatal("accepted line value", mode, edit, p)
			}
			if s.version != revision+1 || len(s.undo) != depth+1 {
				t.Fatal("not one history entry")
			}
			after := cloneDeck(s.deck)
			owner := s.deck.Slides[0]
			if s.masterMode {
				owner = *masterSlideAt(&s.deck, s.currentMaster)
			}
			oldOwner := before.Slides[0]
			if s.masterMode {
				oldOwner = *masterSlideAt(&before, s.currentMaster)
			}
			if !reflect.DeepEqual(owner.Elements[:2], oldOwner.Elements[:2]) {
				t.Fatal("line edit altered shape subset")
			}
			if edit.property == "line.path" && mustQuery(owner.Elements[2].Query).Has("connector-route") {
				t.Fatal("path change kept manual route")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("line undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("line redo", err)
			}
			revision = s.version
			if err := s.apply(action); err != nil || s.version != revision {
				t.Fatal("repeated value not no-op", err)
			}
		}
		data, err := serializeDeck("Diagram.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseDeckData("Diagram.md", data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConnectorSelectionRejectsInvalidAndLockedSubsets(t *testing.T) {
	for _, edit := range []struct{ property, raw string }{{"line.width", "0.5"}, {"line.width", "9"}, {"line.width", `"3"`}, {"line.arrowWidth", "0"}, {"line.arrowWidth", "21"}, {"line.arrows", `"triangle"`}, {"line.path", `"curved"`}, {"line.path", "null"}, {"line.arrows", "true"}} {
		s := newNativeEditorSession("Diagram.md", connectorSelectionFixture())
		revision := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"from", "line-a", "line-b"}, Property: edit.property, PropertyValue: json.RawMessage(edit.raw)})
	}
	for _, scenario := range []string{"locked-line", "locked-excluded", "stale", "missing"} {
		s := newNativeEditorSession("Diagram.md", connectorSelectionFixture())
		revision := s.version
		ids := []string{"from", "line-a", "line-b"}
		if scenario == "locked-line" {
			s.deck.Slides[0].Elements[3].Query += "&object-locked=1"
		}
		if scenario == "locked-excluded" {
			s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
		}
		if scenario == "stale" {
			revision--
		}
		if scenario == "missing" {
			ids = append(ids, "absent")
		}
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: ids, Property: "line.width", PropertyValue: json.RawMessage("2")})
	}
}

func TestConnectorSelectionMasterClearsAndEnteredGroups(t *testing.T) {
	for _, reset := range []bool{false, true} {
		e := connectorSelectionFixture().Slides[0].Elements[2]
		e.MasterSlotID = "source-line"
		e.Query += "&master-clear=connector-width,fg"
		slide := Slide{Elements: []Element{e}}
		raw := json.RawMessage(`"straight"`)
		if reset {
			raw = json.RawMessage(`""`)
		}
		if _, err := applyConnectorSelection(&slide, []string{e.ID}, connectorSelectionControls["line.path"], raw); err != nil {
			t.Fatal(err)
		}
		q := mustQuery(slide.Elements[0].Query)
		if q.Has("connector-route") || strings.Contains(q.Get("master-clear"), "connector-route") == reset || !strings.Contains(q.Get("master-clear"), "fg") {
			t.Fatal("wrong manual-route inheritance", reset, q)
		}
	}
	s := newNativeEditorSession("Diagram.md", connectorSelectionFixture())
	for i := range s.deck.Slides[0].Elements {
		s.deck.Slides[0].Elements[i].Query = setQueryValue(s.deck.Slides[0].Elements[i].Query, "group", "diagram-group")
	}
	s.editingGroup = "diagram-group"
	s.selected = 2
	s.selection = map[int]bool{2: true}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"line-a"}, Property: "line.width", PropertyValue: json.RawMessage("4")}); err != nil {
		t.Fatal(err)
	}
	if mustQuery(s.deck.Slides[0].Elements[2].Query).Get("connector-width") != "4" || mustQuery(s.deck.Slides[0].Elements[3].Query).Get("connector-width") != "3" {
		t.Fatal("entered child changed another connector")
	}
}

func TestConnectorSelectionMixedAutomaticRoutingHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"normal", "tab", "base", "layout", "group"} {
		s := newNativeEditorSession("Mixed routing.md", connectorSelectionFixture())
		if mode == "tab" {
			s.deck.Slides[0].TabID = "diagram-tab"
			syncSlideTabs(&s.deck)
		}
		if mode == "group" {
			for i := range s.deck.Slides[0].Elements {
				s.deck.Slides[0].Elements[i].Query = setQueryValue(s.deck.Slides[0].Elements[i].Query, "group", "diagram-group")
			}
		}
		if mode == "base" || mode == "layout" {
			s.masterMode = true
			if mode == "layout" {
				s.currentMaster = 1
			}
			*masterSlideAt(&s.deck, s.currentMaster) = cloneSlide(s.deck.Slides[0])
			masterSlideAt(&s.deck, s.currentMaster).ID = "routing-master-" + mode
		}
		s.selected = 2
		s.selection = map[int]bool{0: true, 2: true, 3: true}
		caps := s.transportState().SelectionCapabilities
		if p := caps.Properties["line.routing"]; p.State != "mixed" || len(p.EligibleIDs) != 2 {
			t.Fatal(mode, p)
		}
		before, revision := cloneDeck(s.deck), s.version
		action := nativeEditorAction{Action: "set-selection-property", SceneMaster: s.masterMode, SceneRevision: &revision, ObjectIDs: caps.TargetIDs, Property: "line.routing", PropertyValue: json.RawMessage(`"automatic"`)}
		if s.masterMode {
			action.Slide = s.currentMaster
		}
		if err := s.apply(action); err != nil {
			t.Fatal(mode, err)
		}
		p := s.actionTransportState(action.Action).SelectionCapabilities.Properties[action.Property]
		if p.Value != "automatic" || p.Provenance != "explicit" || s.version != revision+1 {
			t.Fatal("wrong accepted automatic routing", mode, p)
		}
		owner, old := s.deck.Slides[0], before.Slides[0]
		if s.masterMode {
			owner = *masterSlideAt(&s.deck, s.currentMaster)
			old = *masterSlideAt(&before, s.currentMaster)
		}
		for i, e := range owner.Elements {
			if e.Kind != "connector" {
				if !reflect.DeepEqual(e, old.Elements[i]) {
					t.Fatal("route command changed shape", mode)
				}
			} else if q := mustQuery(e.Query); q.Has("connector-route") || q.Get("connector-mode") != "elbow" {
				t.Fatal("route command left manual/straight path", mode, q)
			}
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("routing undo", mode, err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
			t.Fatal("routing redo", mode, err)
		}
		revision = s.version
		if err := s.apply(action); err != nil || revision != s.version {
			t.Fatal("repeated automatic routing not no-op", mode, err)
		}
	}
	for _, raw := range []string{`"manual"`, `"bogus"`, `1`, `null`, `true`} {
		s := newNativeEditorSession("Invalid routing.md", connectorSelectionFixture())
		revision := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", SceneRevision: &revision, ObjectIDs: []string{"from", "line-a", "line-b"}, Property: "line.routing", PropertyValue: json.RawMessage(raw)})
	}
}

func TestConnectorSelectionBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_CONNECTOR_SELECTION_PROOF")
	if root == "" {
		t.Skip("optional browser fixture")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "diagram.md"), connectorSelectionFixture()); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "inherited.md"), inheritedConnectorSelectionFixture()); err != nil {
		t.Fatal(err)
	}
}
