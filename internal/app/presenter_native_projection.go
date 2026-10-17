package app

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// One owned published-input snapshot, not an editable document or a second
// history. Encoded media bytes are borrowed immutably; private result archives
// are omitted. Newer accepted commands may coalesce before any scene is built.
type presenterNativeProjection struct {
	source       *nativeEditorSession
	deck         *Deck
	revision     int64
	cols, rows   int
	pending      bool
	projected    []int
	full         bool
	considered   []int                         // Exact comparison candidates, not all authored slides.
	dependencies *nativeProjectionDependencies // Immutable published-source index.
	urgent       chan struct{}                 // Editor demand bypasses background debounce.
	done         chan struct{}                 // Closed on publication, supersession or disposal.
	pageSizes    map[int]workspacePageEntry    // Bounded exact sizing hints for current immutable pages.
}

// All channel ownership lives under p.mu. Waiters never hold the document or
// presenter lock while projection runs, and stale jobs cannot close new leases.
func finishNativeProjectionWaiters(projection *presenterNativeProjection) {
	if projection == nil {
		return
	}
	if projection.urgent != nil {
		close(projection.urgent)
		projection.urgent = nil
	}
	if projection.done != nil {
		close(projection.done)
		projection.done = nil
	}
}

// Await an already accepted edit, never an arbitrary future revision. View-only
// commands create no pending projection and return immediately. Newer edits may
// supersede the requested one; follow the latest lease so no edit is lost.
func (p *presenterCompanion) awaitEditorProjection(ctx context.Context, owner string, revision int64) {
	for {
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		projection := p.nativeProjection
		if p.closed || projection == nil || projection.source == nil || owner == "" ||
			projection.source.notesSessionID != owner || !projection.pending || revision > projection.revision {
			p.mu.Unlock()
			return
		}
		if projection.urgent != nil {
			close(projection.urgent)
			projection.urgent = nil
		}
		done := projection.done
		p.mu.Unlock()
		if done == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
		}
	}
}

// Called while initially attaching a native session to its already projected
// companion. There is no reverse lock acquisition: refresh never locks source.
func (p *presenterCompanion) bindNativeProjection(source *nativeEditorSession, deck Deck, revision int64, cols, rows int) {
	if p == nil {
		return
	}
	dependencies := captureNativeProjectionDependencies(deck)
	p.bindPreparedNativeProjection(source, deck, revision, cols, rows, dependencies)
}

// The native bootstrap prepares its owned dependency snapshot before taking
// the editor lock. Attaching the already-prepared lease stays atomic with any
// edits accepted during initial HTML generation and renderer startup.
func (p *presenterCompanion) bindPreparedNativeProjection(source *nativeEditorSession, deck Deck, revision int64, cols, rows int, dependencies *nativeProjectionDependencies) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.seq++ // Attaching a different document cancels any older async publisher.
	p.clearPresenterSceneBaselinesLocked()
	finishNativeProjectionWaiters(p.nativeProjection)
	p.fullPending, p.slidePending = false, false
	p.nativeProjection = &presenterNativeProjection{source: source, deck: &deck, revision: revision, cols: cols, rows: rows, dependencies: dependencies}
}

// Compatibility callers supply resolved Slides, not authored dependencies.
// Retain session ownership/revision rejection but require a full next projection.
func (p *presenterCompanion) invalidateNativeProjectionLocked() {
	p.clearPresenterSceneBaselinesLocked()
	if p.nativeProjection != nil {
		finishNativeProjectionWaiters(p.nativeProjection)
		p.nativeProjection.deck = nil
		p.nativeProjection.pending = false
		p.nativeProjection.pageSizes = nil
		p.nativeProjection.dependencies = nil
	}
}

func nativeProjectionStructureEqual(before, after Deck) bool {
	if len(before.Slides) != len(after.Slides) {
		return false
	}
	for index, slide := range before.Slides {
		if slide.ID != after.Slides[index].ID {
			return false
		}
	}
	return true
}

// RefreshEditorDeckAsync consumes a revision-tagged owned scene snapshot. It
// compares against the actual published baseline, not the previous command:
// this preserves all earlier unrendered edits when a newer job supersedes it.
// Resolution/projection happens after debouncing and outside document/presenter
// locks. Synchronous/full compatibility refreshes still cancel the generation.
func (p *presenterCompanion) RefreshEditorDeckAsync(source *nativeEditorSession, path string, deck Deck, revision int64, cols, rows int) {
	if p == nil || source == nil || cols <= 0 || rows <= 0 {
		return
	}
	cols, rows = max(20, cols), max(10, rows)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if p.nativeProjection == nil {
		p.nativeProjection = &presenterNativeProjection{source: source}
	}
	projection := p.nativeProjection
	if projection.source != source || revision <= projection.revision {
		p.mu.Unlock()
		return
	}
	baseline := projection.deck
	dependencies := projection.dependencies
	finishNativeProjectionWaiters(projection)
	projection.urgent, projection.done = make(chan struct{}), make(chan struct{})
	urgent := projection.urgent
	full := p.fullPending && !projection.pending || baseline == nil || projection.cols != cols || projection.rows != rows
	projection.revision = revision
	projection.pending = true
	p.seq++
	seq := p.seq
	p.fullPending = true // Prevent navigation through a not-yet-published timeline.
	p.slidePending = false
	p.refreshPath = path
	p.freezeBuildForRefreshLocked()
	p.mu.Unlock()
	go func() {
		timer := time.NewTimer(80 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-urgent:
		}
		current := func() bool {
			p.mu.RLock()
			defer p.mu.RUnlock()
			return seq == p.seq && p.nativeProjection == projection
		}
		if !current() {
			return
		}
		if !full {
			full = !nativeProjectionStructureEqual(*baseline, deck)
		}
		candidates := []int{}
		if full {
			for index := range deck.Slides {
				candidates = append(candidates, index)
			}
		} else {
			candidates = nativeProjectionCandidateSlides(*baseline, deck, dependencies)
		}
		changed := []int{}
		for _, index := range candidates {
			if !current() {
				return
			}
			if full || !sceneProjectionInputsEqual(*baseline, deck, sceneResponseKey{index: index, cols: cols, rows: rows}) {
				changed = append(changed, index)
			}
		}
		pages := make(map[int][]exportPage, len(changed))
		previousPages := map[int]workspacePageEntry{}
		if !full {
			p.mu.RLock()
			if seq != p.seq || p.nativeProjection != projection {
				p.mu.RUnlock()
				return
			}
			for _, index := range changed {
				old := p.pages[index]
				sized := projection.pageSizes[index]
				if len(old) == 0 || len(sized.pages) != len(old) || &sized.pages[0] != &old[0] {
					sized = workspacePageEntry{pages: old}
				}
				previousPages[index] = sized
			}
			p.mu.RUnlock()
		}
		changedRefs := make(map[int]workspaceReferences, len(changed))
		for _, index := range changed {
			if !current() {
				return
			}
			resolved := deck.ResolveSlide(index, false)
			changedRefs[index] = workspacePageReferences(deck.Slides[index], resolved)
			pages[index] = exportSlidePages(deck.resolvedSlideRenderPreview(resolved, index, cols, rows), index, len(deck.Slides), cols, rows)
		}
		nextDependencies := dependencies
		if full {
			nextDependencies = retainNativeProjectionDependencies(nil, changedRefs)
		} else if len(changedRefs) > 0 {
			nextDependencies = retainNativeProjectionDependencies(dependencies, changedRefs)
		}
		beforePages, nextSizes := preparePresenterSceneBaselines(previousPages, pages)
		// The shell depends on dimensions/source on structural changes. Build it
		// from these already projected pages, never resolve the deck a second time.
		html := ""
		if full {
			ordered := []exportPage{}
			for index := range deck.Slides {
				ordered = append(ordered, pages[index]...)
			}
			var err error
			preserved := readPreservedExportHead(strings.TrimSuffix(path, filepath.Ext(path)) + ".html")
			html, err = exportProjectedHTML(exportDeck{Cols: cols, Rows: rows, Source: filepath.Base(path), Pages: ordered}, preserved, true)
			if err != nil {
				p.mu.Lock()
				if seq == p.seq && p.nativeProjection == projection {
					finishNativeProjectionWaiters(projection)
					projection.pending = false
					p.fullPending, p.slidePending = false, false
					projection.deck = nil // Retry from a complete snapshot next time.
					projection.dependencies = nil
					if p.maintainBuildCursorLocked() {
						p.state.Version++
					}
					setUIError("Presenter refresh failed: " + err.Error())
				}
				p.mu.Unlock()
				return
			}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if seq != p.seq || p.nativeProjection != projection {
			return
		}
		if full {
			p.html, p.pages = html, pages
			projection.pageSizes = nil
		} else {
			if p.pages == nil {
				p.pages = map[int][]exportPage{}
			}
			for index, updated := range pages {
				p.pages[index] = updated
			}
			projection.pageSizes = retainPresenterPageSizeEntries(projection.pageSizes, nextSizes, changed)
		}
		projection.deck = &deck
		projection.dependencies = nextDependencies
		projection.cols, projection.rows = cols, rows
		projection.pending = false
		projection.projected = append([]int(nil), changed...)
		projection.considered = append([]int(nil), candidates...)
		projection.full = full
		p.fullPending, p.slidePending = false, false
		if full || len(changed) > 0 {
			p.state.DeckVersion++
			p.recordPageChangeLocked(full, changed, beforePages)
			p.state.DeckSlide = -1
			if !full && len(changed) == 1 {
				p.state.DeckSlide = changed[0]
			}
			p.state.Version++
		}
		if p.maintainBuildCursorLocked() {
			p.state.Version++
		}
		finishNativeProjectionWaiters(projection)
	}()
}
