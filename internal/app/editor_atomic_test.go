package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func atomicEditorFixture(master bool) *nativeEditorSession {
	elements := []Element{
		{ID: "atomic-first", Kind: "text", Text: "First", Query: "top=2&group=pair"},
		{ID: "atomic-second", Kind: "text", Text: "Second", Query: "top=5&group=pair"},
	}
	s := newNativeEditorSession("Atomic.md", Deck{Slides: []Slide{{Elements: elements}}})
	s.masterMode = master
	if master {
		s.deck.Masters.Base.Slide.Elements = cloneSlide(s.deck.Slides[0]).Elements
	}
	s.savedDeck = cloneDeck(s.deck)
	s.untitled, s.dirtyOverride = false, false
	s.selected, s.selection, s.editingGroup = 0, map[int]bool{0: true}, "pair"
	return s
}

func assertEditorFailureUnchanged(t *testing.T, s *nativeEditorSession, action nativeEditorAction) {
	t.Helper()
	beforeDeck, beforeState := cloneDeck(s.deck), s.state()
	beforeState.Masters = beforeDeck.Masters
	// Master previews may synthesize a page number with a fresh projection ID;
	// authored state and all session controls must still remain identical.
	beforeState.Resolved = nil
	beforeUndo, beforeRedo := slices.Clone(s.undo), slices.Clone(s.redo)
	beforeCache := s.sceneResponse
	if err := s.apply(action); err == nil {
		t.Fatalf("%s unexpectedly accepted invalid input", action.Action)
	}
	afterState := s.state()
	afterState.Resolved = nil
	if !reflect.DeepEqual(s.deck, beforeDeck) || !reflect.DeepEqual(afterState, beforeState) ||
		!reflect.DeepEqual(s.undo, beforeUndo) || !reflect.DeepEqual(s.redo, beforeRedo) || s.sceneResponse != beforeCache {
		t.Fatalf("%s changed state on failure: deck=%t view=%t undo=%t redo=%t cache=%t", action.Action,
			!reflect.DeepEqual(s.deck, beforeDeck), !reflect.DeepEqual(afterState, beforeState), !reflect.DeepEqual(s.undo, beforeUndo),
			!reflect.DeepEqual(s.redo, beforeRedo), s.sceneResponse != beforeCache)
	}
}

func TestEditorAtomicBatchRejectsLateInvalidReplacement(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			s := atomicEditorFixture(master)
			updates := cloneSlide(s.deck.Slides[0]).Elements
			updates[0].Text = "Must not be committed"
			updates[1].Kind = " \t "
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "update-elements", ElementIndices: []int{0, 1}, ElementsData: updates})
		})
	}
}

func TestEditorAtomicFailurePreservesEnteredGroup(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := atomicEditorFixture(master)
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "select-slide", Slide: 10000})
	}
}

func TestEditorAtomicBatchValidatesCompleteRequest(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, problem := range []string{"length", "duplicate", "missing", "query", "locked", "revision", "slide", "mode"} {
			t.Run(map[bool]string{false: "slide", true: "master"}[master]+"/"+problem, func(t *testing.T) {
				s := atomicEditorFixture(master)
				updates := cloneSlide(s.deck.Slides[0]).Elements
				updates[0].Text, updates[1].Text = "Edited first", "Edited second"
				action := nativeEditorAction{Action: "update-elements", ElementIndices: []int{0, 1}, ElementsData: updates}
				switch problem {
				case "length":
					action.ElementsData = action.ElementsData[:1]
				case "duplicate":
					action.ElementsData[1].ID = action.ElementsData[0].ID
				case "missing":
					action.ElementsData[1].ID = "no-longer-present"
				case "query":
					action.ElementsData[1].Query = "fg=%zz"
				case "locked":
					target := &s.deck.Slides[0]
					if master {
						target = &s.deck.Masters.Base.Slide
					}
					target.Elements[1].Query += "&object-locked=1"
					s.savedDeck = cloneDeck(s.deck)
				case "revision":
					stale := s.version - 1
					action.SceneRevision = &stale
				case "slide", "mode":
					revision := s.version
					action.SceneRevision, action.SceneMaster = &revision, master
					if problem == "slide" {
						action.Slide = 1
					} else {
						action.SceneMaster = !master
					}
				}
				assertEditorFailureUnchanged(t, s, action)
			})
		}
	}
}

func TestEditorAtomicBatchStableTargetsAndSingleHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := atomicEditorFixture(master)
		before := cloneDeck(s.deck)
		updates := cloneSlide(s.deck.Slides[0]).Elements
		updates[0].Text, updates[1].Text = "First edited", "Second edited"
		// Deliberately wrong captured positions: IDs, not positions, own edits.
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "update-elements", SceneRevision: &revision, SceneMaster: master, Slide: 0, ElementIndices: []int{1, 0}, ElementsData: updates}); err != nil {
			t.Fatal(err)
		}
		target := s.deck.Slides[0]
		if master {
			target = s.deck.Masters.Base.Slide
		}
		for _, e := range target.Elements {
			if e.ID == "atomic-first" && e.Text != "First edited" || e.ID == "atomic-second" && e.Text != "Second edited" {
				t.Fatal("batch changed the wrong stable target")
			}
		}
		if len(s.undo) != 1 || !s.dirtyLocked() {
			t.Fatal("batch must create one dirty history transaction")
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) || s.dirtyLocked() {
			t.Fatalf("undo must exactly restore the saved deck: %v", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) || !s.dirtyLocked() {
			t.Fatalf("redo must exactly restore the batch: %v", err)
		}
	}
}

func TestEditorAtomicBatchNoOpKeepsRevisionAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := atomicEditorFixture(master)
		version := s.version
		updates := cloneSlide(s.deck.Slides[0]).Elements
		if err := s.apply(nativeEditorAction{Action: "update-elements", ElementIndices: []int{0, 1}, ElementsData: updates}); err != nil {
			t.Fatal(err)
		}
		if s.version != version || len(s.undo) != 0 || s.dirtyLocked() {
			t.Fatal("identical batch created a revision, history entry or dirty document")
		}
	}
}

func TestEditorAtomicRollbackPrecedesReaderAccess(t *testing.T) {
	s := atomicEditorFixture(false)
	before := cloneDeck(s.deck)
	s.mu.Lock()
	tx := beginEditorCommand(s, true)
	s.deck.Slides[0].Elements[0].Text = "Partial edit"
	s.selection[1], s.selected, s.version, s.editingGroup = true, 1, 100, ""
	s.undo = append(s.undo, before)
	read := make(chan Deck, 1)
	go func() {
		s.mu.RLock()
		defer s.mu.RUnlock()
		read <- cloneDeck(s.deck)
	}()
	err := errors.New("rejected command")
	tx.finish(&err)
	if !reflect.DeepEqual(<-read, before) || s.version != 1 || s.selected != 0 || s.editingGroup != "pair" || len(s.selection) != 1 || len(s.undo) != 0 {
		t.Fatal("reader observed partial state or rollback lost session state")
	}
}

func TestEditorAtomicMalformedMasterSetupRestoresDocument(t *testing.T) {
	s := atomicEditorFixture(true)
	s.deck.Masters = MasterDeck{}
	s.currentMaster = 1000
	s.savedDeck = cloneDeck(s.deck)
	assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "unsupported-command"})
}

func TestEditorBatchOwnsReplacementExtensions(t *testing.T) {
	s := atomicEditorFixture(false)
	updates := cloneSlide(s.deck.Slides[0]).Elements
	updates[0].Text = "Edited"
	extra := jsonExtensions{"future": []byte(`{"value":1}`)}
	updates[0].Extra = &extra
	if err := s.apply(nativeEditorAction{Action: "update-elements", ElementIndices: []int{0, 1}, ElementsData: updates}); err != nil {
		t.Fatal(err)
	}
	extra["future"][9] = '2'
	for _, element := range s.deck.Slides[0].Elements {
		if element.ID == "atomic-first" && string((*element.Extra)["future"]) != `{"value":1}` {
			t.Fatal("replacement extension aliases caller-owned bytes")
		}
	}
}
