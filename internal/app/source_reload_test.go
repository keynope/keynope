package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sourceReloadFixture(t *testing.T) (*nativeEditorSession, string) {
	t.Helper()
	cols, rows, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = cols, rows, order
	})
	path := filepath.Join(t.TempDir(), "deck.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deck, err := parseEditorDeckData(path, []byte("# Original\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeEditorSession(path, deck)
	s.resetSourceWatchLocked()
	return s, path
}

func pollSource(t *testing.T, s *nativeEditorSession, allow bool) {
	t.Helper()
	s.sourceWatch.checked = time.Time{}
	if err := s.refreshSource(allow, false, "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestSourceReloadStableFileAndDraftDeferral(t *testing.T) {
	s, path := sourceReloadFixture(t)
	owner := s.notesSessionID
	if err := os.WriteFile(path, []byte("# External change\n\n---\n\n# Added slide\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pollSource(t, s, true)
	if s.deck.Slides[0].Elements[0].Text != "Original" {
		t.Fatal("read an unstable write")
	}
	pollSource(t, s, false)
	if s.sourceStatus == "" || s.deck.Slides[0].Elements[0].Text != "Original" {
		t.Fatal("draft was not preserved")
	}
	pollSource(t, s, true)
	if len(s.deck.Slides) != 2 || s.deck.Slides[0].Elements[0].Text != "External change" || s.sourceStatus != "" || s.dirtyLocked() {
		t.Fatal("changed deck not loaded cleanly")
	}
	if s.notesSessionID == owner || s.mutationRevision == 0 {
		t.Fatal("stale document owners were not invalidated")
	}
	if len(s.undo) != 0 || len(s.selection) != 0 {
		t.Fatal("old history/selection survived replacement")
	}
}

func TestSourceReloadPreservesUnsavedChangesAndRejectsStaleApproval(t *testing.T) {
	s, path := sourceReloadFixture(t)
	if err := s.apply(nativeEditorAction{Action: "add-slide"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# External replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pollSource(t, s, true)
	pollSource(t, s, true)
	if len(s.deck.Slides) != 2 || !s.dirtyLocked() || !strings.Contains(s.sourceStatus, "unsaved") {
		t.Fatal("local edits were overwritten")
	}
	if err := s.refreshSource(true, true, s.notesSessionID, s.version-1); err == nil {
		t.Fatal("stale discard accepted")
	}
	if err := s.refreshSource(true, true, s.notesSessionID, s.version); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Slides) != 1 || s.deck.Slides[0].Elements[0].Text != "External replacement" {
		t.Fatal("explicit reload failed")
	}
}

func TestSourceReloadInvalidFileAndOwnSave(t *testing.T) {
	s, path := sourceReloadFixture(t)
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	pollSource(t, s, true)
	s.sourceWatch.checked = time.Time{}
	if err := s.refreshSource(true, false, "", 0); err == nil {
		t.Fatal("empty document accepted")
	}
	if s.deck.Slides[0].Elements[0].Text != "Original" || !strings.Contains(s.sourceStatus, "Could not reload") {
		t.Fatal("invalid replacement damaged the document")
	}
	if err := saveDeck(path, s.deck); err != nil {
		t.Fatal(err)
	}
	if !s.confirmSaved(path, s.version) {
		t.Fatal("save was not accepted")
	}
	version, owner := s.version, s.notesSessionID
	pollSource(t, s, true)
	pollSource(t, s, true)
	if s.version != version || s.notesSessionID != owner || s.sourceStatus != "" {
		t.Fatal("own save was reloaded")
	}
}

func TestSourceReloadIsVisibleThroughStatePolling(t *testing.T) {
	s, path := sourceReloadFixture(t)
	if err := os.WriteFile(path, []byte("# Polling replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s.sourceWatch.checked = time.Time{}
		response := httptest.NewRecorder()
		s.handleState(response, httptest.NewRequest("GET", "/api/editor/state", nil))
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	if s.deck.Slides[0].Elements[0].Text != "Polling replacement" {
		t.Fatal("state polling did not reload")
	}
}

func TestSourceReloadPublishesNativeProjection(t *testing.T) {
	s, path := sourceReloadFixture(t)
	deck := cloneDeckForScene(s.deck)
	_, pages, err := projectPresenterDocument(path, deck.ResolvedSlides(), 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	p := &presenterCompanion{pages: pages}
	p.bindNativeProjection(s, deck, s.version, 245, 56)
	s.companion = p
	defer p.Close()
	if err := os.WriteFile(path, []byte("# Reloaded projection\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pollSource(t, s, true)
	pollSource(t, s, true)
	waitNativeProjection(t, p)
	p.mu.RLock()
	version := p.state.DeckVersion
	p.mu.RUnlock()
	if version == 0 {
		t.Fatalf("reload did not publish: text=%q revision=%d projection=%+v dims=%d/%d", s.deck.Slides[0].Elements[0].Text, s.version, p.nativeProjection, authoredTerminalWidth, authoredTerminalHeight)
	}
	assertNativeProjectionMatchesCold(t, s, p, path)
}
