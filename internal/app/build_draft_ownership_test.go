package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildDraftBrowserFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_BUILD_DRAFT_EVIDENCE")
	if output == "" {
		t.Skip("set KEYNOPE_BUILD_DRAFT_EVIDENCE for owned fixtures")
	}
	s := buildEditorFixture(false)
	s.deck.Slides = append(s.deck.Slides, cloneSlide(s.deck.Slides[0]), Slide{ID: "other-slide"})
	s.deck.Slides[1].ID, s.deck.Slides[1].TabID = "tab-build-proof", "build-tab"
	syncSlideTabs(&s.deck)
	for _, index := range []int{0, 1} {
		slide := cloneSlide(s.deck.Slides[0])
		for i := range slide.Elements {
			if index == 0 {
				slide.Elements[i].ID = "base-" + slide.Elements[i].ID
			} else {
				slide.Elements[i].ID = "layout-" + slide.Elements[i].ID
			}
		}
		slide.ID = []string{"base-build-proof", "layout-build-proof"}[index]
		*masterSlideAt(&s.deck, index) = slide
	}
	data, err := serializeDeck("Build drafts.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "build-drafts.md"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDraftOwnerRejectsReplacementSession(t *testing.T) {
	for _, master := range []bool{false, true} {
		source, replacement := buildEditorFixture(master), buildEditorFixture(master)
		action := buildAction(source, ownedBuild())
		action.NotesSessionID = source.notesSessionID
		before := cloneDeck(replacement.deck)
		if source.version != replacement.version || source.notesSessionID == replacement.notesSessionID {
			t.Fatal("fixture must reproduce equal-revision replacement with a different owner")
		}
		if err := replacement.apply(action); err == nil {
			t.Fatal("foreign draft owner admitted")
		}
		if !reflect.DeepEqual(replacement.deck, before) || len(replacement.undo) != 0 || replacement.version != source.version {
			t.Fatal("rejected replacement draft changed the document")
		}
		action.NotesSessionID = replacement.notesSessionID
		if err := replacement.apply(action); err != nil || len(replacement.undo) != 1 {
			t.Fatal("current owned draft failed", err)
		}
	}
}
