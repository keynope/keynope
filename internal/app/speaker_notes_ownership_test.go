package app

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func notesHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func TestNotesDoNotInvalidateGeometryOrNormalizeObjects(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		for _, scoped := range []bool{false, true} {
			s := capabilityFixture()
			s.masterMode = master
			if master {
				s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			}
			target := &s.deck.Slides[0]
			if master {
				target = &s.deck.Masters.Base.Slide
			}
			before := cloneDeck(s.deck)
			version, mutation := s.version, s.mutationRevision
			a := nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: "Private notes"}
			if scoped {
				a.SceneRevision, a.SceneMaster = &version, master
				a.NotesSessionID = s.notesSessionID
				a.NotesOwnerID = speakerNotesOwnerAt(s.deck, master, 0)
				a.NotesBeforeHash = notesHash(target.Notes)
			}
			if err := s.apply(a); err != nil {
				t.Fatal(master, scoped, err)
			}
			want := cloneDeck(before)
			if master {
				want.Masters.Base.Slide.Notes = a.Notes
			} else {
				want.Slides[0].Notes = a.Notes
			}
			if !reflect.DeepEqual(s.deck, want) || s.mutationRevision != mutation || s.version != version+1 || len(s.undo) != 1 {
				t.Fatal("notes changed geometry or skipped document history", master, scoped)
			}
			revision := s.version
			box := sceneRect{X: 30.875, Y: 35.625, Width: 12.125, Height: 6.375}
			if err := s.apply(nativeEditorAction{Action: "set-selection-geometry", Slide: 0, SceneMaster: master, SceneRevision: &revision, MutationRevision: &mutation, ObjectIDs: []string{"shape-a"}, Geometry: &box, GeometryResize: true}); err != nil {
				t.Fatal("note save invalidated original geometry token", master, scoped, err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, want) {
				t.Fatal("geometry Undo lost notes", master, scoped, err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("notes Undo changed geometry", master, scoped, err)
			}
		}
	}
}

func TestScopedNotesOwnerIndependentOfViewport(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, owner := range []struct {
		id     string
		master bool
	}{{"one", false}, {"base", true}, {"layout", true}} {
		t.Run(owner.id, func(t *testing.T) {
			d := Deck{Slides: []Slide{{ID: "one", Notes: "Original"}, {ID: "two", Notes: "Other"}}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Slide: Slide{Notes: "Original"}}, Layouts: []MasterLayout{{ID: "layout", Slide: Slide{Notes: "Original"}}}}}
			s := newNativeEditorSession("Notes.md", d)
			s.current = 1
			before, version, mutation := cloneDeck(s.deck), s.version, s.mutationRevision
			a := nativeEditorAction{Action: "update-slide-notes", Slide: 99, SceneRevision: &version, SceneMaster: owner.master, NotesSessionID: s.notesSessionID, NotesOwnerID: owner.id, NotesBeforeHash: notesHash("Original"), Notes: "New notes"}
			if err := s.apply(a); err != nil {
				t.Fatal(err)
			}
			if s.current != 1 || s.masterMode || s.selected != -1 || s.version != version+1 || s.mutationRevision != mutation || len(s.undo) != 1 {
				t.Fatal("note commit changed viewport or history boundary")
			}
			expected := cloneDeck(before)
			switch owner.id {
			case "one":
				expected.Slides[0].Notes = a.Notes
			case "base":
				expected.Masters.Base.Slide.Notes = a.Notes
			case "layout":
				expected.Masters.Layouts[0].Slide.Notes = a.Notes
			}
			if !reflect.DeepEqual(s.deck, expected) {
				t.Fatal("notes landed in a different owner")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("notes Undo drifted", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, expected) {
				t.Fatal("notes Redo drifted", err)
			}
		})
	}
}

func TestScopedNotesRejectStaleAndAmbiguousTargets(t *testing.T) {
	for _, kind := range []string{"revision", "hash", "missing", "ambiguous", "wrong-namespace"} {
		t.Run(kind, func(t *testing.T) {
			s := newNativeEditorSession("Notes.md", Deck{Slides: []Slide{{ID: "one", Notes: "Original"}, {ID: "two", Notes: "Other"}}})
			version := s.version
			a := nativeEditorAction{Action: "update-slide-notes", SceneRevision: &version, NotesSessionID: s.notesSessionID, NotesOwnerID: "one", NotesBeforeHash: notesHash("Original"), Notes: "Rejected"}
			switch kind {
			case "revision":
				version--
			case "hash":
				a.NotesBeforeHash = notesHash("Older")
			case "missing":
				a.NotesOwnerID = "gone"
			case "ambiguous":
				s.deck.Slides[1].ID = "one"
			case "wrong-namespace":
				a.SceneMaster = true
			}
			before, revision, mutation := cloneDeck(s.deck), s.version, s.mutationRevision
			if err := s.apply(a); err == nil || s.version != revision || s.mutationRevision != mutation || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("stale notes changed document/history", err)
			}
		})
	}
}

func TestScopedNotesFollowOwnerAfterReorderAndModeSwitch(t *testing.T) {
	s := newNativeEditorSession("Notes.md", Deck{Slides: []Slide{{ID: "one", Notes: "Original"}, {ID: "two", Notes: "Other"}}})
	s.deck.Slides[0], s.deck.Slides[1] = s.deck.Slides[1], s.deck.Slides[0]
	s.masterMode = true
	version := s.version
	a := nativeEditorAction{Action: "update-slide-notes", Slide: 0, SceneRevision: &version, NotesSessionID: s.notesSessionID, NotesOwnerID: "one", NotesBeforeHash: notesHash("Original"), Notes: "Kept with one"}
	if err := s.apply(a); err != nil || s.deck.Slides[1].Notes != a.Notes || s.deck.Slides[0].Notes != "Other" || !s.masterMode {
		t.Fatal("notes followed index/view instead of owner", err)
	}
	version = s.version
	a.NotesBeforeHash = notesHash(a.Notes)
	undo, mutation := len(s.undo), s.mutationRevision
	if err := s.apply(a); err != nil || s.version != version || len(s.undo) != undo || s.mutationRevision != mutation {
		t.Fatal("exact note no-op created history", err)
	}
}

func TestEditorSeedsPrivateLegacyNotesIdentityWithoutDirtying(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Slides: []Slide{{Notes: "One"}, {Notes: "Two"}, {ID: "authored", Notes: "Three"}}}
	s := newNativeEditorSession("Notes.md", d)
	owners := []string{speakerNotesOwnerAt(s.deck, false, 0), speakerNotesOwnerAt(s.deck, false, 1), speakerNotesOwnerAt(s.deck, false, 2)}
	if s.dirtyLocked() || owners[0] == "" || owners[0] == owners[1] || owners[2] != "authored" || s.deck.Slides[0].ID != "" || d.Slides[0].notesOwnerID != "" {
		t.Fatal("identity migration changed baseline/source")
	}
	newSession := newNativeEditorSession("Copy.md", s.deck)
	if newSession.state().NotesOwnerID == owners[0] || s.state().NotesOwnerID != owners[0] {
		t.Fatal("in-memory session replacement reused or mutated note-owner tokens")
	}
	encoded, err := serializeDeck("Notes.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "notes-owner") {
		t.Fatal("runtime notes identity leaked to Markdown")
	}
	loaded, err := parseDeckData("Notes.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	reopened := newNativeEditorSession("Notes.md", loaded)
	for i := range s.deck.Slides {
		if s.deck.Slides[i].ID != reopened.deck.Slides[i].ID {
			t.Fatal("note owner identity changed after reopen")
		}
	}
	if reopened.state().NotesOwnerID == owners[0] {
		t.Fatal("runtime note identity survived a new session")
	}
}

func TestLegacyNoteOwnerSurvivesBuildIdentityButNotClone(t *testing.T) {
	s := newNativeEditorSession("Legacy.md", Deck{Slides: []Slide{{Notes: "Original"}}})
	owner := s.state().NotesOwnerID
	// Adding a build can give an existing legacy slide its first authored ID.
	// Pending notes must still recognize the original runtime owner.
	s.deck.Slides[0].ID = "now-authored"
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "update-slide-notes", SceneRevision: &version, NotesSessionID: s.notesSessionID, NotesOwnerID: owner, NotesBeforeHash: notesHash("Original"), Notes: "Kept"}); err != nil {
		t.Fatal("authored identity introduction detached pending notes", err)
	}
	if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
		t.Fatal(err)
	}
	if s.state().NotesOwnerID == owner || s.deck.Slides[0].notesOwnerID != owner || s.deck.Slides[1].notesOwnerID != "" {
		t.Fatal("cloned page reused the source note draft owner")
	}
}

func TestClearedLegacyNotesOnlySlideSurvivesSave(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, scoped := range []bool{false, true} {
		s := newNativeEditorSession("Notes.md", Deck{Slides: []Slide{{Notes: "Only authored content"}}})
		before := cloneDeck(s.deck)
		a := nativeEditorAction{Action: "update-slide-notes", Slide: 0}
		if scoped {
			version := s.version
			a.SceneRevision = &version
			a.NotesOwnerID = s.state().NotesOwnerID
			a.NotesSessionID = s.notesSessionID
			a.NotesBeforeHash = notesHash(s.deck.Slides[0].Notes)
		}
		if err := s.apply(a); err != nil {
			t.Fatal(err)
		}
		encoded, err := serializeDeck("Notes.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		d, err := parseEditorDeckData("Notes.md", encoded)
		if err != nil || len(d.Slides) != 1 || d.Slides[0].ID == "" || d.Slides[0].Notes != "" {
			t.Fatal("clearing notes removed the legacy page", err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("note clearing identity did not undo atomically", err)
		}
	}
}

func TestNotesSessionRejectsReopenedAuthoredOwners(t *testing.T) {
	for _, master := range []bool{false, true} {
		first := newNativeEditorSession("First.md", Deck{Slides: []Slide{{ID: "same", Notes: "Original"}}})
		owner, before := "same", "Original"
		if master {
			owner, before = first.deck.Masters.Base.ID, first.deck.Masters.Base.Slide.Notes
		}
		version := first.version
		a := nativeEditorAction{Action: "update-slide-notes", SceneRevision: &version, SceneMaster: master, NotesOwnerID: owner, NotesSessionID: first.notesSessionID, NotesBeforeHash: notesHash(before), Notes: "Old request"}
		second := newNativeEditorSession("Second.md", first.deck)
		source := cloneDeck(second.deck)
		if second.version != version || second.notesSessionID == first.notesSessionID {
			t.Fatal("invalid session reuse fixture")
		}
		if err := second.apply(a); err == nil || !reflect.DeepEqual(second.deck, source) || len(second.undo) != 0 || second.version != version {
			t.Fatal("request crossed sessions with identical owner/revision/value", err)
		}
	}
}
