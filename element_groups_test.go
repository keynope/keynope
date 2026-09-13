package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestEditorGroupLifecycle(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			elements := []Element{{ID: "a", Kind: "text", Text: "First", Query: "top=2"}, {ID: "b", Kind: "shape", Text: "[shape:square]", Query: "top=10&shape=square"}, {ID: "c", Kind: "text", Text: "Third", Query: "top=20"}}
			s := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: elements}}})
			if master {
				s.masterMode = true
				s.currentMaster = 0
				s.deck.Masters.Base.Slide.Elements = elements
			}
			get := func() []Element {
				if master {
					return s.deck.Masters.Base.Slide.Elements
				}
				return s.deck.Slides[0].Elements
			}
			action := func(name string, index int, toggle bool) {
				t.Helper()
				a := nativeEditorAction{Action: name, Element: index}
				if toggle {
					a.Name = "toggle"
				}
				if err := s.apply(a); err != nil {
					t.Fatal(name, err)
				}
			}
			action("select-element", 0, false)
			action("select-element", 1, true)
			action("group-elements", 0, false)
			id := elementGroup(get()[0])
			if id == "" || elementGroup(get()[1]) != id {
				t.Fatal("group not assigned")
			}
			action("select-element", -1, false)
			action("select-element", 1, false)
			if len(s.selection) != 2 {
				t.Fatal("click must select whole group")
			}
			action("enter-group", 0, false)
			action("select-element", 1, false)
			if len(s.selection) != 1 || s.editingGroup != id {
				t.Fatal("member selection")
			}
			action("exit-group", 0, false)
			if len(s.selection) != 2 {
				t.Fatal("exit must select group")
			}
			action("select-element", 2, true)
			action("group-elements", 0, false)
			for _, e := range get() {
				if (e.ID == "a" || e.ID == "b" || e.ID == "c") && elementGroup(e) != elementGroup(get()[0]) {
					t.Fatal("regroup must merge")
				}
			}
			action("ungroup-elements", 0, false)
			for _, e := range get() {
				if elementGroup(e) != "" {
					t.Fatal("ungroup")
				}
			}
			action("undo", 0, false)
			if elementGroup(get()[0]) == "" {
				t.Fatal("undo group")
			}
			action("redo", 0, false)
			if elementGroup(get()[0]) != "" {
				t.Fatal("redo ungroup")
			}
		})
	}
}

func TestGroupMetadataRoundTrip(t *testing.T) {
	q := "top=4&group=group-abc123&fg=%23ff0000"
	parsed, ok := textPlacementComment("<!-- " + placementCommentText(q) + " -->")
	if !ok || elementGroup(Element{Query: parsed}) != "group-abc123" {
		t.Fatalf("group metadata lost: %s", parsed)
	}
	for _, bad := range []string{"group=<script>", "group=bad/id"} {
		parsed, _ := textPlacementComment("<!-- " + bad + " -->")
		if elementGroup(Element{Query: parsed}) != "" {
			t.Fatal("unsafe group accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "group.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Grouped", Query: q}, {Kind: "shape", Query: "top=12&group=group-abc123&shape=square&width=12&height=6"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	reloaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Slides[0].Elements) != 2 {
		t.Fatal("round trip changed members")
	}
	for _, e := range reloaded.Slides[0].Elements {
		if elementGroup(e) != "group-abc123" {
			t.Fatal("saved group lost", e)
		}
	}
}

func TestMarqueeSelection(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			elements := []Element{{ID: "a", Kind: "shape", Query: "group=g&top=1"}, {ID: "b", Kind: "shape", Query: "group=g&top=10"}, {ID: "c", Kind: "text", Text: "Outside", Query: "top=20"}}
			s := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{{Elements: elements}}})
			if master {
				s.masterMode = true
				s.deck.Masters.Base.Slide.Elements = elements
			}
			selectIDs := func(ids ...string) error {
				a := nativeEditorAction{Action: "select-elements"}
				for _, id := range ids {
					a.ElementIndices = append(a.ElementIndices, 0) // Stale indices resolve by ID.
					a.ElementsData = append(a.ElementsData, Element{ID: id})
				}
				return s.apply(a)
			}
			if err := selectIDs("b", "c"); err != nil || len(s.selection) != 3 {
				t.Fatalf("whole group plus outsider: %v, %v", s.selection, err)
			}
			if len(s.undo) != 0 {
				t.Fatal("selection must not enter document history")
			}
			if err := selectIDs("missing"); err == nil || len(s.selection) != 3 {
				t.Fatal("invalid batch must not partially select")
			}
			if err := s.apply(nativeEditorAction{Action: "enter-group", ElementData: &Element{ID: "a"}}); err != nil {
				t.Fatal(err)
			}
			if err := selectIDs("b"); err != nil || len(s.selection) != 1 || s.editingGroup != "g" {
				t.Fatal("marquee must allow individual members inside a group")
			}
			if err := selectIDs(); err != nil || len(s.selection) != 0 || s.selected != -1 {
				t.Fatal("empty selection")
			}
		})
	}
}

func TestGroupPasteAndStaleBatch(t *testing.T) {
	elements := []Element{{ID: "a", Kind: "text", Query: "group=old"}, {ID: "b", Kind: "shape", Query: "group=old"}}
	freshPastedGroups(elements)
	if id := elementGroup(elements[0]); id == "old" || id == "" || id != elementGroup(elements[1]) {
		t.Fatal("paste must create independent group")
	}
	a := nativeEditorAction{ElementIndices: []int{0, 1}, ElementsData: []Element{elements[1], elements[0]}}
	if err := resolveEditorBatch(elements, &a); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.ElementIndices, []int{1, 0}) {
		t.Fatal("batch did not follow IDs")
	}
	a.ElementsData[1].ID = "removed"
	if resolveEditorBatch(elements, &a) == nil {
		t.Fatal("stale batch accepted")
	}
	slide := Slide{Elements: elements[:1]}
	pruneSingletonGroups(&slide)
	if elementGroup(slide.Elements[0]) != "" {
		t.Fatal("singleton group retained")
	}
}
