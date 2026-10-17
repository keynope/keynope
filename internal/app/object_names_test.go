package app

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestObjectNamesHistoryAndPersistence(t *testing.T) {
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank", Elements: []Element{{ID: "body", Kind: "text", Text: "Content remains", Query: "render=truetype&modern-font=sans&left=5&top=5&width=100&height=15"}}}}}
			if scope == "tab" {
				d.Slides[0].TabID = "guide"
			}
			s := newNativeEditorSession("Names.md", d)
			if scope == "base" || scope == "layout" {
				s.masterMode = true
				if scope == "layout" {
					s.currentMaster = 1
				}
				masterSlideAt(&s.deck, s.currentMaster).Elements = cloneSlide(s.deck.Slides[0]).Elements
			}
			target := func() *Slide {
				if s.masterMode {
					return masterSlideAt(&s.deck, s.currentMaster)
				}
				return &s.deck.Slides[0]
			}
			before := cloneDeck(s.deck)
			updated := cloneElement(target().Elements[0])
			q, _ := url.ParseQuery(updated.Query)
			name := "Overview <&> — 中文 🚀"
			q.Set("object-name", name)
			updated.Query = q.Encode()
			version := s.version
			index := 0
			if s.masterMode {
				index = s.currentMaster
			}
			action := nativeEditorAction{Action: "update-elements", Slide: index, SceneMaster: s.masterMode, SceneRevision: &version, ElementIndices: []int{0}, ElementsData: []Element{updated}}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			accepted := cloneDeck(s.deck)
			if objectName(target().Elements[0]) != name || target().Elements[0].Text != "Content remains" || len(s.undo) != 1 {
				t.Fatal("rename lost identity/content or is not one transaction")
			}
			data, err := serializeDeck("Names.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("Names.md", data)
			if err != nil {
				t.Fatal(err)
			}
			owner := reopened.Slides[0]
			if s.masterMode {
				owner = *masterSlideAt(&reopened, s.currentMaster)
			}
			if objectName(owner.Elements[0]) != name {
				t.Fatal("name lost in Markdown", string(data))
			}
			version = s.version
			if err := s.apply(action); err != nil || s.version != version || len(s.undo) != 1 {
				t.Fatal("unchanged name created history", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("rename Undo failed", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
				t.Fatal("rename Redo failed", err)
			}
		})
	}
}

func TestObjectNameBoundsAndAtomicFailure(t *testing.T) {
	for _, name := range []string{strings.Repeat("x", 129), "two\nlines", "control\x00", "line\u2028separator"} {
		s := newNativeEditorSession("Names.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "first", Kind: "text", Text: "First"}, {ID: "last", Kind: "text", Text: "Last"}}}}})
		version := s.version
		first, last := cloneElement(s.deck.Slides[0].Elements[0]), cloneElement(s.deck.Slides[0].Elements[1])
		first.Query = setQueryValue(first.Query, "object-name", "Valid first")
		last.Query = setQueryValue(last.Query, "object-name", name)
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "update-elements", SceneRevision: &version, ElementIndices: []int{0, 1}, ElementsData: []Element{first, last}})
	}
}

func TestNamedSceneAndClipboard(t *testing.T) {
	d := structuredFixture()
	d.Slides[0].Elements[0].Query = setQueryValue(d.Slides[0].Elements[0].Query, "object-name", "Workshop plan")
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == d.Slides[0].Elements[0].ID {
			found = object.Name == "Workshop plan"
		}
	}
	if !found {
		t.Fatal("scene did not expose accessible name")
	}
	bundle, err := makeObjectClipboard(d, d.Slides[0].Elements[:1])
	if err != nil || objectName(bundle.Elements[0]) != "Workshop plan" {
		t.Fatal("clipboard lost name", err)
	}
}

func TestObjectNameLockAndStaleRevision(t *testing.T) {
	for _, mode := range []string{"locked", "stale", "wrong-slide", "wrong-master"} {
		s := newNativeEditorSession("Names.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "body", Kind: "text", Text: "Protected"}}}}})
		if mode == "locked" {
			s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
		}
		version := s.version
		updated := cloneElement(s.deck.Slides[0].Elements[0])
		updated.Query = setQueryValue(updated.Query, "object-name", "Attempt")
		action := nativeEditorAction{Action: "update-elements", SceneRevision: &version, ElementIndices: []int{0}, ElementsData: []Element{updated}}
		switch mode {
		case "stale":
			version--
		case "wrong-slide":
			action.Slide = 1
		case "wrong-master":
			action.SceneMaster = true
		}
		assertEditorFailureUnchanged(t, s, action)
	}
}
