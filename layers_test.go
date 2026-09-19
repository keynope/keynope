package main

import (
	"reflect"
	"testing"
)

func TestInsertedObjectsPreserveAuthoredStack(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, command := range []string{"add-element", "duplicate-element", "paste-elements"} {
			t.Run(command+map[bool]string{false: "/slide", true: "/master"}[master], func(t *testing.T) {
				base := Slide{Elements: []Element{
					{ID: "a", Kind: "text", Text: "A", Query: "top=1&z-index=1"},
					{ID: "b", Kind: "text", Text: "B", Query: "top=10&z-index=0"},
				}}
				s := newNativeEditorSession("Layers.md", Deck{Slides: []Slide{base}})
				if master {
					if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
						t.Fatal(err)
					}
					s.deck.Masters.Base.Slide = cloneSlide(base)
				}
				before := cloneDeck(s.deck)
				action := nativeEditorAction{Action: command, Kind: "text", Element: 1}
				if command == "paste-elements" {
					action.ElementsData = []Element{
						{ID: "x", Kind: "text", Text: "X", Query: "top=2&z-index=5"},
						{ID: "y", Kind: "text", Text: "Y", Query: "top=3&z-index=2"},
					}
				}
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				check := func(deck Deck) {
					t.Helper()
					slide := deck.Slides[0]
					if master {
						slide = deck.Masters.Base.Slide
					}
					got := ""
					for _, i := range slidePaintOrder(slide) {
						got += slide.Elements[i].Text + "|"
					}
					want := map[string]string{"add-element": "B|A|Text|", "duplicate-element": "B|B|A|", "paste-elements": "B|A|Y|X|"}[command]
					if got != want {
						t.Fatalf("stack %q, want %q", got, want)
					}
				}
				check(s.deck)
				data, err := serializeDeck("Layers.md", s.deck)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData("Layers.md", data)
				if err != nil {
					t.Fatal(err)
				}
				check(loaded)
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, s.deck) {
					t.Fatal("insertion undo changed source")
				}
			})
		}
	}
}

func TestInsertionLeavesLegacyStackUntouched(t *testing.T) {
	before := Slide{Elements: []Element{{ID: "a", Kind: "text", Text: "A"}}}
	after := cloneSlide(before)
	after.Elements = append(after.Elements, Element{ID: "b", Kind: "shape", Query: "shape=square"})
	want := cloneSlide(after)
	preserveInsertedStack(before, &after, nativeEditorAction{Action: "add-element"})
	if !reflect.DeepEqual(after, want) {
		t.Fatal("legacy insertion acquired explicit stack")
	}
}

func TestGroupStackAndArrangeCommands(t *testing.T) {
	base := Slide{Elements: []Element{
		{ID: "a", Kind: "text", Text: "A", Query: "top=1&group=team"},
		{ID: "b", Kind: "text", Text: "B", Query: "top=2&group=team"},
		{ID: "c", Kind: "text", Text: "C", Query: "top=3"},
		{ID: "d", Kind: "text", Text: "D", Query: "top=4"},
	}}
	ids := func(slide Slide) string {
		result := ""
		for _, i := range slidePaintOrder(slide) {
			result += slide.Elements[i].ID
		}
		return result
	}
	for _, test := range []struct{ id, target, side, want string }{{"a", "d", "after", "cdab"}, {"d", "b", "before", "dabc"}, {"c", "a", "after", "abcd"}, {"a", "b", "after", "abcd"}, {"a", "d", "before", "cabd"}} {
		slide := cloneSlide(base)
		if _, err := moveObjectStack(&slide, test.id, test.side, "", test.target); err != nil {
			t.Fatal(err)
		}
		if got := ids(slide); got != test.want {
			t.Fatalf("drop %s %s %s: %s", test.id, test.side, test.target, got)
		}
	}
	for _, test := range []struct{ direction, editing, want string }{{"forward", "", "cabd"}, {"front", "", "cdab"}, {"back", "", "abcd"}, {"forward", "team", "bacd"}} {
		slide := cloneSlide(base)
		if _, err := moveObjectStack(&slide, "a", test.direction, test.editing); err != nil {
			t.Fatal(err)
		}
		if got := ids(slide); got != test.want {
			t.Fatalf("%s/%s: %s, want %s", test.direction, test.editing, got, test.want)
		}
	}
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("Layers.md", Deck{Slides: []Slide{base}})
		if master {
			if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
				t.Fatal(err)
			}
			s.deck.Masters.Base.Slide = cloneSlide(base)
		}
		before := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "move-element", Element: 0, Kind: "front"}); err != nil {
			t.Fatal(err)
		}
		target := s.deck.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
		}
		if ids(target) != "cdab" {
			t.Fatal("Arrange did not use group stack", ids(target))
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.deck, before) {
			t.Fatal("Arrange undo changed content")
		}
	}
}

func TestLayerOrderSurvivesReadingOrderAndReopen(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "lower", Kind: "shape", Query: "shape=square&top=20&left=2&width=10&height=5"},
		{ID: "upper", Kind: "shape", Query: "shape=circle&top=2&left=2&width=10&height=5"},
		{ID: "text", Kind: "text", Text: "Readable", Query: "top=10&left=2"},
	}}}}
	s := newNativeEditorSession("Layers.md", deck)
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "move-object-layer", ObjectID: "lower", Value: 1}); err != nil {
		t.Fatal(err)
	}
	ids := func(slide Slide) []string {
		var ids []string
		for _, i := range slidePaintOrder(slide) {
			ids = append(ids, slide.Elements[i].ID)
		}
		return ids
	}
	want := []string{"upper", "lower", "text"}
	if !reflect.DeepEqual(ids(s.deck.Slides[0]), want) {
		t.Fatal(ids(s.deck.Slides[0]))
	}
	canonicalizeSlideElementOrder(&s.deck.Slides[0], 245, 56)
	if !reflect.DeepEqual(ids(s.deck.Slides[0]), want) {
		t.Fatal("reading order changed stacking")
	}
	data, err := serializeDeck("Layers.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Layers.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(loaded.Slides[0]), want) {
		t.Fatal("save/reopen changed stacking", ids(loaded.Slides[0]))
	}
	scene, err := buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, o := range scene.Objects {
		if o.ID == "lower" || o.ID == "upper" || o.ID == "text" {
			actual = append(actual, o.ID)
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("Modern renderer ignored stack", actual)
	}
	actual = nil
	for _, line := range layout(loaded.Slides[0], 245, 56) {
		if line.Element < 0 || line.Element >= len(loaded.Slides[0].Elements) {
			continue
		}
		id := loaded.Slides[0].Elements[line.Element].ID
		if len(actual) == 0 || actual[len(actual)-1] != id {
			actual = append(actual, id)
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("Retro renderer ignored stack", actual)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("layer undo was not exact")
	}
}
