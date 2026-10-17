package app

import (
	"encoding/json"
	"math"
	"net/url"
	"reflect"
	"testing"
)

func TestConnectedSelectionResizeCapabilities(t *testing.T) {
	slide := Slide{Elements: []Element{
		{ID: "a", Kind: "shape", Query: "element-style=modern"},
		{ID: "b", Kind: "shape", Query: "element-style=modern"},
		{ID: "chart", Kind: "chart"},
		{ID: "line", Kind: "connector", Query: "connector-from=a&connector-to=b"},
		{ID: "invalid", Kind: "connector", Query: "connector-from=a&connector-to=chart"},
	}}
	for _, tc := range []struct {
		ids  []string
		want bool
	}{
		{[]string{"a", "chart"}, true},
		{[]string{"a", "b", "line", "chart"}, true},
		{[]string{"a", "line"}, false},
		{[]string{"a", "chart", "invalid"}, false},
	} {
		got := buildSelectionCommands(Deck{}, slide, tc.ids, "", "")["arrange.resize"]
		if got.Enabled != tc.want {
			t.Errorf("resize %v enabled=%v, want %v: %s", tc.ids, got.Enabled, tc.want, got.Reason)
		}
	}
}

func TestSelectionGeometryOverridesMasterAnchors(t *testing.T) {
	for _, kind := range []string{"shape", "text"} {
		for _, position := range []sceneRect{{X: 12, Y: 7, Width: 40, Height: 12}, {X: 12.875, Y: 7.625, Width: 40, Height: 12}} {
			d := strokeInheritanceFixture()
			master := &d.Masters.Layouts[0].Slide.Elements[0]
			master.Kind = kind
			master.Query = "shape=square&width=40&height=12&align=center&valign=middle&left_pct=0.6&right=3&right_pct=0.1&bottom=2&row_delta=4&shape-offset-x=0.5&shape-offset-y=0.5&object-offset-x=0.25&object-offset-y=0.25&modern-size=32&modern-font=sans&text-align=right&text-valign=bottom&fg=%2300aa55"
			d.Slides[0].Elements[0].Kind = kind
			d.Slides[0].Elements[0].Text = "Positioned text"
			s := newNativeEditorSession("Geometry.md", d)
			before := cloneDeck(s.deck)
			revision := s.version
			action := nativeEditorAction{Action: "set-selection-geometry", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"instance"}, Geometry: &position, GeometryResize: true}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			check := func(deck Deck) {
				t.Helper()
				resolved := deck.ResolveSlide(0, false)
				var query url.Values
				for _, e := range resolved.Elements {
					if e.ID == "instance" {
						query, _ = url.ParseQuery(e.Query)
					}
				}
				if query == nil {
					t.Fatal("missing resolved instance")
				}
				for _, key := range []string{"left_pct", "right", "right_pct", "bottom", "row_delta", "align", "valign"} {
					if query.Has(key) {
						t.Fatalf("%s inherited competing %s after explicit geometry", kind, key)
					}
				}
				if query.Get("text-align") != "right" || query.Get("text-valign") != "bottom" || query.Get("fg") != "#00aa55" {
					t.Fatal("position changed inherited styling", query)
				}
				scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, object := range scene.Objects {
					if object.ID != "instance" {
						continue
					}
					found = true
					got := object.Bounds
					want := sceneRect{X: position.X * 1920 / 245, Y: position.Y * 1080 / 56, Width: position.Width * 1920 / 245, Height: position.Height * 1080 / 56}
					for i, value := range []float64{got.X - want.X, got.Y - want.Y, got.Width - want.Width, got.Height - want.Height} {
						if math.Abs(value) > 0.001 {
							t.Fatalf("%s axis %d scene bounds %+v != %+v", kind, i, got, want)
						}
					}
				}
				if !found {
					t.Fatal("instance absent from rendered scene")
				}
			}
			check(s.deck)
			if !reflect.DeepEqual(before.Masters, s.deck.Masters) {
				t.Fatal("master changed")
			}
			after := cloneDeck(s.deck)
			markdown, err := serializeDeck("Geometry.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Geometry.md", markdown)
			if err != nil {
				t.Fatal(err)
			}
			check(reopened)
			current := s.version
			action.SceneRevision = &current
			if err := s.apply(action); err != nil || s.version != current {
				t.Fatal("repeated position was not a no-op", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("undo did not restore master anchor inheritance", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
				t.Fatal("redo did not restore explicit position", err)
			}
		}
	}
}

func TestSelectionGeometryPreservesLegacyJustification(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		d := strokeInheritanceFixture()
		d.Slides[0].Elements[0].Kind = "text"
		d.Slides[0].Elements[0].Text = "This paragraph keeps its justification when repositioned."
		if inherited {
			d.Masters.Layouts[0].Slide.Elements[0].Kind = "text"
			d.Masters.Layouts[0].Slide.Elements[0].Query = "align=justify&width=40&height=12"
		} else {
			d.Slides[0].LayoutID = ""
			d.Slides[0].Elements[0].MasterSlotID = ""
			d.Slides[0].Elements[0].Query = "align=justify&width=40&height=12"
		}
		if _, err := applySelectionGeometry(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"instance"}, Geometry: &sceneRect{X: 15, Y: 8, Width: 40, Height: 12}}, ""); err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(d.Slides[0].Elements[0].Query)
		if q.Get("text-align") != "justify" || q.Has("align") {
			t.Fatal("lost legacy text alignment", q)
		}
	}
}

func TestSelectionGeometryAtomicAcceptedAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "master"}[master], func(t *testing.T) {
			s := capabilityFixture()
			s.selection = map[int]bool{0: true}
			if master {
				s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{ID: "geometry-master", Name: "Geometry", Slide: cloneSlide(s.deck.Slides[0])})
				s.masterMode, s.currentMaster = true, len(s.deck.Masters.Layouts)
			}
			before := cloneDeck(s.deck)
			revision := s.version
			index := 0
			if master {
				index = s.currentMaster
			}
			box := sceneRect{X: 30.125, Y: 4.375, Width: 85.25, Height: 9.5}
			action := nativeEditorAction{Action: "set-selection-geometry", Slide: index, SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"text-a"}, Geometry: &box, GeometryResize: true}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			after := cloneDeck(s.deck)
			state := s.actionTransportState(action.Action)
			if !reflect.DeepEqual(state.SelectionCapabilities.Geometry, &box) || len(s.undo) != 1 || s.version != revision+1 {
				t.Fatalf("accepted geometry/history mismatch: %+v", state.SelectionCapabilities)
			}
			elements := s.deck.Slides[0].Elements
			if master {
				elements = masterSlideAt(&s.deck, index).Elements
			}
			q, _ := url.ParseQuery(elements[0].Query)
			if q.Get("modern-size") != "120" || q.Get("modern-font") != "c64" || q.Get("text-box") != "1" || q.Get("object-offset-x") != "0.125" || q.Get("object-offset-y") != "0.375" {
				t.Fatal("geometry altered typography or lost precision", q)
			}
			current := s.version
			action.SceneRevision = &current
			if err := s.apply(action); err != nil || len(s.undo) != 1 || s.version != current {
				t.Fatal("repeated geometry was not a no-op", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("geometry undo did not restore entire deck", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
				t.Fatal("geometry redo did not restore entire deck", err)
			}
		})
	}
}

func TestSelectionGeometryRejectsAndKeepsState(t *testing.T) {
	for _, scenario := range []string{"nil", "stale", "locked", "group", "connector", "missing-id", "too-small", "outside", "nan"} {
		t.Run(scenario, func(t *testing.T) {
			s := capabilityFixture()
			s.selection = map[int]bool{0: true}
			revision := s.version
			box := sceneRect{X: 30, Y: 5, Width: 50, Height: 8}
			action := nativeEditorAction{Action: "set-selection-geometry", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"text-a"}, Geometry: &box, GeometryResize: true}
			switch scenario {
			case "nil":
				action.Geometry = nil
			case "stale":
				revision--
			case "locked":
				s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
			case "group":
				for i := 0; i < 2; i++ {
					s.deck.Slides[0].Elements[i].Query += "&group=g"
				}
			case "connector":
				s.deck.Slides[0].Elements[0].Kind = "connector"
			case "missing-id":
				action.ObjectIDs = []string{"missing"}
			case "too-small":
				box.Width = .1
			case "outside":
				box.X = 10000
			case "nan":
				box.X = math.NaN()
			}
			before := s.state()
			if err := s.apply(action); err == nil || !reflect.DeepEqual(s.state(), before) || len(s.undo) != 0 {
				t.Fatal("invalid geometry mutated document/view/history", err)
			}
		})
	}
}

func TestSelectionGeometryShapePrecisionAndImageMove(t *testing.T) {
	s := capabilityFixture()
	s.selected, s.selection = 2, map[int]bool{2: true}
	box := sceneRect{X: 30.875, Y: 35.625, Width: 12.125, Height: 6.375}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-geometry", SceneRevision: &revision, Slide: 0, ObjectIDs: []string{"shape-a"}, Geometry: &box, GeometryResize: true}); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[2].Query)
	if q.Get("shape-offset-x") != "0.5" || q.Get("shape-offset-y") != "0.5" || q.Get("object-offset-x") != "0.375" || q.Get("object-offset-y") != "0.125" || !reflect.DeepEqual(s.transportState().SelectionCapabilities.Geometry, &box) {
		t.Fatal("shape precision lost", q)
	}
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "image", Kind: "image", Query: "scale=0.3&width=20&height=8&brightness=1.5&tint=%23ff5555&image-style=retro&modern-crop=0.1,0.1,0.8,0.8"}}}}}
	before := deck.Slides[0].Elements[0].Query
	if _, err := applySelectionGeometry(&deck, nativeEditorAction{Slide: 0, ObjectIDs: []string{"image"}, Geometry: &sceneRect{X: 10.125, Y: 4.25, Width: 20, Height: 8}}, ""); err != nil {
		t.Fatal(err)
	}
	old, _ := url.ParseQuery(before)
	moved, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
	for _, key := range []string{"scale", "width", "height", "brightness", "tint", "image-style", "modern-crop"} {
		if old.Get(key) != moved.Get(key) {
			t.Fatalf("move changed %s", key)
		}
	}
	if moved.Has("stretch") || explicitSelectionGeometry(deck.Slides[0].Elements[0]) != nil {
		t.Fatal("move changed aspect-fit semantics")
	}
}

func TestSelectionRotationAndCommandEligibility(t *testing.T) {
	s := capabilityFixture()
	caps := s.transportState().SelectionCapabilities
	for _, name := range []string{"arrange.move", "arrange.resize", "arrange.rotate", "arrange.align", "arrange.match", "arrange.distribute"} {
		if !caps.Commands[name].Enabled {
			t.Fatalf("valid selection disabled %s: %+v", name, caps.Commands[name])
		}
	}
	if caps.Commands["arrange.geometry"].Enabled || caps.Properties["geometry.rotation"].Editable {
		t.Fatal("multi-selection offered independent box/rotation")
	}
	s.selection = map[int]bool{0: true}
	s.deck.Slides[0].Elements[0].Query += "&orientation=cw"
	caps = s.transportState().SelectionCapabilities
	if caps.Properties["geometry.rotation"].Value != 90.0 || !caps.Commands["arrange.geometry"].Enabled || caps.Commands["arrange.distribute"].Enabled {
		t.Fatal("single command/legacy angle mismatch")
	}
	before := cloneDeck(s.deck)
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"text-a"}, Property: "geometry.rotation", PropertyValue: json.RawMessage("37.5")}); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("orientation") || q.Get("object-rotation") != "37.5" || s.transportState().SelectionCapabilities.Properties["geometry.rotation"].Value != 37.5 {
		t.Fatal("rotation did not replace legacy orientation")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("rotation undo failed", err)
	}
	s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
	for name, command := range s.transportState().SelectionCapabilities.Commands {
		if name == "style.copy" {
			continue
		} // Reading a locked source is harmless.
		if command.Enabled || command.Reason == "" {
			t.Fatalf("locked %s enabled or unexplained", name)
		}
	}
}
