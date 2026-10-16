package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func slideLinkReferenceDeck() Deck {
	d := Deck{Masters: defaultMasterDeck()}
	for _, id := range []string{"first", "second", "target", "last"} {
		d.Slides = append(d.Slides, Slide{ID: id, LayoutID: "blank", Elements: []Element{
			{ID: "jump", Kind: "text", Text: "Jump", Query: "slide=3&fg=%23abcdef&future-control=keep", Extra: extensionFixture("future", `{"keep":true}`)},
			{ID: "url", Kind: "text", Text: "External", Query: "link=https%3A%2F%2Fkeynope.sh%2F%23section"},
		}})
	}
	d.Slides[3].TabID = "resources"
	d.Masters.Base.Slide.Elements = []Element{{ID: "master-jump", Kind: "text", Text: "Jump from master", Query: "slide=3"}}
	d.Masters.Layouts[0].Slide.Elements = []Element{{ID: "layout-jump", Kind: "text", Text: "Jump from layout", Query: "link=%233"}}
	d.Tabs = []DeckTab{{ID: "target-tab", Name: "Target", Page: 3}}
	return d
}

func TestSlideLinksFollowStructuralEditsAtomically(t *testing.T) {
	for _, operation := range []string{"add-slide", "clone-slide", "reorder-slide", "delete-before", "delete-target"} {
		t.Run(operation, func(t *testing.T) {
			s := newNativeEditorSession("Links.md", slideLinkReferenceDeck())
			before := cloneDeck(s.deck)
			if output := os.Getenv("KEYNOPE_SLIDE_LINK_EVIDENCE"); output != "" && operation == "add-slide" {
				if err := os.MkdirAll(output, 0o755); err != nil {
					t.Fatal(err)
				}
				data, err := serializeDeck("Links.md", before)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "links.md"), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			action := nativeEditorAction{Action: operation}
			switch operation {
			case "reorder-slide":
				action.Slide, action.Value = 2, 0
			case "delete-before":
				action.Action = "delete-slide"
			case "delete-target":
				action.Action = "delete-slide"
				s.current = 2
			}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			target := 0
			for index, slide := range s.deck.Slides {
				if slide.ID == "target" {
					target = index + 1
				}
			}
			check := func(slide Slide) {
				for _, element := range slide.Elements {
					if element.Text == "External" {
						q, _ := url.ParseQuery(element.Query)
						if q.Get("link") != "https://keynope.sh/#section" {
							t.Fatal("structural edit changed external link")
						}
					}
					if element.Text != "Jump" && element.Text != "Jump from master" && element.Text != "Jump from layout" {
						continue
					}
					link, exists := linkTargetFromQuery(element.Query, len(s.deck.Slides))
					if target == 0 && exists || target > 0 && (!exists || link.Slide != target-1) {
						t.Fatalf("%s changed target identity: query %s, target position %d", operation, element.Query, target)
					}
					if element.Text == "Jump" {
						q, _ := url.ParseQuery(element.Query)
						if q.Get("fg") != "#abcdef" || q.Get("future-control") != "keep" || element.Extra == nil {
							t.Fatal("structural edit lost unrelated object metadata")
						}
					}
				}
			}
			for _, slide := range s.deck.Slides {
				check(slide)
			}
			check(s.deck.Masters.Base.Slide)
			for _, layout := range s.deck.Masters.Layouts {
				check(layout.Slide)
			}
			after := cloneDeck(s.deck)
			encoded, err := serializeDeck("Links.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := parseDeckData("Links.md", encoded)
			if err != nil {
				t.Fatal(err)
			}
			for _, slide := range opened.Slides {
				check(slide)
			}
			check(opened.Masters.Base.Slide)
			for _, layout := range opened.Masters.Layouts {
				check(layout.Slide)
			}
			if len(s.undo) != 1 {
				t.Fatal("structural link update split history")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("undo did not restore links", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("redo did not restore links", err)
			}
		})
	}
}

func TestDeletedLocalSlideLinkDoesNotRestoreMasterLink(t *testing.T) {
	d := slideLinkReferenceDeck()
	d.Masters.Base.Slide.Elements = nil
	d.Masters.Layouts[0].Slide.Elements = []Element{{ID: "master-title", SlotID: "link-slot", PlaceholderRole: "title", Kind: "text", Text: "Master", Query: "slide=1"}}
	d.Slides[0].Elements = []Element{{ID: "local-title", MasterSlotID: "link-slot", Kind: "text", Text: "Local", Query: "slide=3"}}
	s := newNativeEditorSession("Inherited links.md", d)
	s.current = 2
	if err := s.apply(nativeEditorAction{Action: "delete-slide"}); err != nil {
		t.Fatal(err)
	}
	check := func(deck Deck) {
		for _, element := range deck.ResolveSlide(0, false).Elements {
			if element.ID == "local-title" {
				if link := elementLink(element.Query); link != "" {
					t.Fatal("deleted local link restored master destination", link)
				}
				return
			}
		}
		t.Fatal("bound instance disappeared")
	}
	check(s.deck)
	data, err := serializeDeck("Inherited links.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("Inherited links.md", data)
	if err != nil {
		t.Fatal(err)
	}
	check(opened)
}

func TestNavigationRemapDoesNotRewriteUnrelatedOrInvalidQueries(t *testing.T) {
	queries := []string{"link=https%3A%2F%2Fkeynope.sh%2F%23x&fg=%23abcdef", "slide=99&future=keep", "slide=bad", "slide=2&broken=%zz", "slide=1&fg=%23abcdef"}
	d := Deck{Slides: []Slide{{}, {}}}
	for _, query := range queries {
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{Kind: "text", Query: query})
	}
	remapDeckNavigationReferences(&d, func(index int) int {
		if index == 1 {
			return 2
		}
		return index
	})
	for index, query := range queries {
		if d.Slides[0].Elements[index].Query != query {
			t.Fatal("unrelated legacy query was rewritten", query)
		}
	}
}

func TestNewAndClonedBlankSlidesKeepStableIdentity(t *testing.T) {
	for _, action := range []string{"add-slide", "clone-slide"} {
		s := newNativeEditorSession("Blank.md", Deck{Slides: []Slide{{}}})
		before := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: action}); err != nil {
			t.Fatal(err)
		}
		added := s.deck.Slides[s.current]
		if !validObjectID(added.ID) || len(added.Elements) != 0 || s.deck.Slides[0].ID != "" {
			t.Fatal(action, "did not assign a fresh copy identity without changing legacy source")
		}
		// Both pages must be authored; mark the otherwise indistinguishable old
		// empty source here to isolate persistence of the new/copy page.
		persisted := cloneDeck(s.deck)
		persisted.Slides[0].ID = "legacy-blank"
		data, err := serializeDeck("Blank.md", persisted)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := parseDeckData("Blank.md", data)
		if err != nil || len(opened.Slides) != 2 || opened.Slides[1].ID != added.ID {
			t.Fatal(action, "blank page disappeared", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal(action, "undo changed original legacy identity", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || s.deck.Slides[1].ID != added.ID {
			t.Fatal(action, "redo generated a different page identity", err)
		}
	}
}

func TestOpaqueMetadataOnlySlideDoesNotDisappear(t *testing.T) {
	extra := extensionFixture("futureSlide", `{"dependency":"future-payload","private":"retained metadata"}`)
	result := &EngagementResult{Version: 1, ActivityID: "archived-activity", Kind: "storm", State: map[string]json.RawMessage{"phase": json.RawMessage(`3`)}}
	deck := Deck{Slides: []Slide{{Extra: extra}, {EngagementResult: result}}}
	data, err := serializeDeck("Opaque page.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("Opaque page.md", data)
	if err != nil || len(opened.Slides) != 2 || !reflect.DeepEqual(opened.Slides[0].Extra, extra) || !reflect.DeepEqual(opened.Slides[1].EngagementResult, result) {
		t.Fatal("nonvisual authored metadata was treated as an empty separator", err)
	}
}
