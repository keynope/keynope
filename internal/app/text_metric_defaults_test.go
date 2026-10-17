package app

import (
	"fmt"
	"reflect"
	"testing"
)

func TestTextMetricDeltasStartAtRenderedDefaults(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, source := range []string{"fixed", "theme", "slide"} {
		for _, master := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/master=%v", source, master), func(t *testing.T) {
				d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Keep content", Query: "render=truetype&left=10&top=5&width=100&height=15"}}}}}
				d.Theme.Fonts.Body.Size, d.Theme.Fonts.Body.Width = 80, 75
				if source == "fixed" {
					d.Slides[0].Elements[0].Query += "&default-font=mono&default-size=90&default-width=65"
				}
				if source == "slide" {
					d.Slides[0].TTFSize, d.Slides[0].TTFWidth = 194, 60
				}
				if master {
					d.Masters.Base.Slide = cloneSlide(d.Slides[0])
					d.Masters.Base.Slide.ID = "master-slide"
				}
				s := newNativeEditorSession("metrics.md", d)
				s.masterMode = master
				s.selected, s.selection = 0, map[int]bool{0: true}
				before := cloneDeck(s.deck)
				metrics := func() (float64, float64) {
					t.Helper()
					state := s.transportState().SelectionCapabilities.Properties
					return state["text.size"].Value.(float64), state["text.width"].Value.(float64)
				}
				originalSize, originalWidth := metrics()
				for _, width := range []bool{false, true} {
					for _, delta := range []float64{1, -1} {
						version := s.version
						action := nativeEditorAction{Action: "set-text-size", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"text"}, TextSizeDelta: &delta}
						if width {
							action.Action, action.TextSizeDelta, action.TextWidthDelta = "set-text-width", nil, &delta
						}
						if err := s.apply(action); err != nil {
							t.Fatal(err)
						}
						size, fontWidth := metrics()
						wantSize, wantWidth := originalSize, originalWidth
						if width {
							wantWidth += delta
						} else {
							wantSize += delta
						}
						if size != wantSize || fontWidth != wantWidth {
							t.Fatal("relative edit ignored rendered defaults", size, fontWidth, wantSize, wantWidth)
						}
						after := cloneDeck(s.deck)
						if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
							t.Fatal("relative edit undo was not exact", err)
						}
						if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
							t.Fatal("relative edit redo was not exact", err)
						}
						if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestTextMetricDeltasKeepMixedMasterValues(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", LayoutID: "blank", Elements: []Element{{ID: "a", Kind: "text", MasterSlotID: "slot-a", Text: "First"}, {ID: "b", Kind: "text", MasterSlotID: "slot-b", Text: "Second"}}}}}
	d.Masters.Base.Slide.Elements = []Element{
		{ID: "slot-a", SlotID: "slot-a", PlaceholderRole: "body", Kind: "text", Query: "render=truetype&modern-font=mono&modern-size=90&modern-width=65&left=10&top=5&width=100&height=15"},
		{ID: "slot-b", SlotID: "slot-b", PlaceholderRole: "body", Kind: "text", Query: "render=truetype&modern-font=sans&modern-size=120&modern-width=95&left=120&top=5&width=100&height=15"},
	}
	s := newNativeEditorSession("mixed.md", d)
	s.selected, s.selection = 0, map[int]bool{0: true, 1: true}
	delta := 1.0
	for _, width := range []bool{false, true} {
		before, version := cloneDeck(s.deck), s.version
		action := nativeEditorAction{Action: "set-text-size", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"a", "b"}, TextSizeDelta: &delta}
		if width {
			action.Action, action.TextSizeDelta, action.TextWidthDelta = "set-text-width", nil, &delta
		}
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		resolved := s.deck.ResolveSlide(0, false)
		for i, element := range s.deck.Slides[0].Elements {
			caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], resolved, []string{element.ID}, "", s.version)
			property, want := "text.size", []float64{91, 121}[i]
			if width {
				property, want = "text.width", []float64{66, 96}[i]
			}
			if caps.Properties[property].Value != want {
				t.Fatal("mixed inherited values were flattened", property, caps.Properties[property], want)
			}
		}
		if len(s.undo) != 1 {
			t.Fatal("mixed edit was not one transaction")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("mixed edit undo changed source", err)
		}
	}
}

func TestTextMetricDefaultLimitsAreNoOps(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, test := range []struct{ size, width, delta float64 }{{1, 1, -1}, {1024, 200, 1}} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Limits", Query: fmt.Sprintf("render=truetype&default-size=%g&default-width=%g", test.size, test.width)}}}}}
		s := newNativeEditorSession("limits.md", d)
		before, version := cloneDeck(s.deck), s.version
		for _, width := range []bool{false, true} {
			action := nativeEditorAction{Action: "set-text-size", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text"}, TextSizeDelta: &test.delta}
			if width {
				action.Action, action.TextSizeDelta, action.TextWidthDelta = "set-text-width", nil, &test.delta
			}
			if err := s.apply(action); err != nil || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) {
				t.Fatalf("clamped inherited metric created a mutation: case=%+v width=%v error=%v revision=%d/%d undo=%d before=%s after=%s", test, width, err, version, s.version, len(s.undo), before.Slides[0].Elements[0].Query, s.deck.Slides[0].Elements[0].Query)
			}
		}
	}
}
