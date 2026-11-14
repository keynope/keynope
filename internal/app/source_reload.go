package app

import (
	"fmt"
	"os"
	"time"
)

// Only native documents have a source on disk. Polling follows the editor's
// state requests so a focused text draft or modal can defer replacement.
type editorSourceWatch struct {
	path      string
	accepted  os.FileInfo
	candidate os.FileInfo
	checked   time.Time
}

func (s *nativeEditorSession) resetSourceWatchLocked() {
	if s.untitled {
		return
	}
	info, err := os.Stat(s.deckPath)
	if err != nil {
		return
	}
	s.sourceWatch = &editorSourceWatch{path: s.deckPath, accepted: info}
	s.sourceStatus = ""
}

func sameSourceFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (s *nativeEditorSession) refreshSource(allow, force bool, owner string, revision int64) error {
	s.mu.Lock()
	w := s.sourceWatch
	if w == nil || s.untitled {
		s.mu.Unlock()
		return nil
	}
	if force && (owner != s.notesSessionID || revision != s.version) {
		s.mu.Unlock()
		return fmt.Errorf("the document changed; review it before reloading")
	}
	if !force && time.Since(w.checked) < 750*time.Millisecond {
		s.mu.Unlock()
		return nil
	}
	w.checked = time.Now()
	info, err := os.Stat(w.path)
	if err != nil {
		s.mu.Unlock()
		return err
	} // Atomic file replacements may briefly remove the path.
	if !force && sameSourceFile(info, w.accepted) {
		w.candidate = nil
		if s.sourceStatus != "" {
			s.sourceStatus = ""
			s.retainUnchangedSceneRevision()
			s.version++
		}
		s.mu.Unlock()
		return nil
	}
	// Require the new file to survive two polls, avoiding partially written decks.
	if !force && !sameSourceFile(info, w.candidate) {
		w.candidate = info
		s.mu.Unlock()
		return nil
	}
	status := "File changed on disk. Finish editing to reload."
	if s.dirtyLocked() {
		status = "File changed on disk. Your unsaved edits have been kept."
	}
	if force || allow && !s.dirtyLocked() {
		var data []byte
		data, err = os.ReadFile(w.path)
		if err == nil {
			after, statErr := os.Stat(w.path)
			if statErr != nil || !sameSourceFile(info, after) {
				w.candidate = nil
				s.mu.Unlock()
				return fmt.Errorf("the source file changed while reading; retry the reload")
			}
			var deck Deck
			deck, err = parseEditorDeckData(w.path, data)
			if err == nil {
				ensureDefaultAuthoredSize()
				next := newNativeEditorSession(w.path, deck, false, parsedDeckElementOrderChanged)
				s.deck, s.savedDeck, s.diagnostics = next.deck, next.savedDeck, next.diagnostics
				s.structuredBudget, s.dirtyOverride = next.structuredBudget, next.dirtyOverride
				s.current = min(s.current, len(s.deck.Slides)-1)
				s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
				s.selected, s.selection, s.editingGroup = -1, map[int]bool{}, ""
				s.undo, s.redo, s.fontLibrary, s.fontExport, s.styleClipboard = nil, nil, nil, nil, nil
				s.sceneResponse, s.masterPreviews = nil, nil
				s.notesSessionID = next.notesSessionID
				s.mutationRevision++
				s.version++
				s.sourceStatus = ""
				w.accepted, w.candidate = info, nil
				companion, path, version, current := s.companion, s.deckPath, s.version, s.current
				var rendered Deck
				if companion != nil {
					rendered = cloneDeckForScene(s.deck)
				}
				s.mu.Unlock()
				if companion != nil {
					companion.RefreshEditorDeckAsync(s, path, rendered, version, authoredTerminalWidth, authoredTerminalHeight)
					companion.Update(current, 0, companion.presentationEnabled(), nil)
				}
				return nil
			}
		}
		status = "Could not reload the changed file: " + err.Error()
	}
	if s.sourceStatus != status {
		s.sourceStatus = status
		s.retainUnchangedSceneRevision()
		s.version++
	}
	s.mu.Unlock()
	return err
}
