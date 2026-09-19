package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Reopening a presenter must use the same pages as its live transport. Patch
// only the bootstrap payload on demand: local edits do not regenerate the
// large HTML shell, rerun layout, or encode an entire deck on every keystroke.
func (p *presenterCompanion) htmlSnapshot() (string, error) {
	p.mu.RLock()
	html, pages := p.html, p.slidePagesLocked()
	p.mu.RUnlock()
	const marker = "<script id=\"keynope-data\" type=\"application/json\">"
	prefix, rest, ok := strings.Cut(html, marker)
	if !ok {
		return "", fmt.Errorf("missing presenter data")
	}
	payload, suffix, ok := strings.Cut(rest, "</script>")
	if !ok {
		return "", fmt.Errorf("unterminated presenter data")
	}
	// Skip old pages rather than reconstructing their nested scenes/assets.
	var metadata struct {
		Cols   int    `json:"cols"`
		Rows   int    `json:"rows"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		return "", err
	}
	updated, err := json.Marshal(exportDeck{Cols: metadata.Cols, Rows: metadata.Rows, Source: metadata.Source, Pages: pages})
	if err != nil {
		return "", err
	}
	if !strings.Contains(prefix, "<style data-keynope-modern-fonts>") {
		for _, page := range pages {
			if page.Scene != nil {
				prefix += "<style data-keynope-modern-fonts>" + modernFontsCSS() + "</style>\n"
				break
			}
		}
	}
	return prefix + marker + string(updated) + "</script>" + suffix, nil
}

// Project once for both the initial HTML and subsequent page transport. The
// pages are immutable render output, not a second editable document snapshot.
func projectPresenterDocument(deckPath string, slides []Slide, cols, rows int) (string, map[int][]exportPage, error) {
	deck := projectExportDeck(deckPath, slides, cols, rows, true)
	preserved := readPreservedExportHead(strings.TrimSuffix(deckPath, filepath.Ext(deckPath)) + ".html")
	html, err := exportProjectedHTML(deck, preserved, true)
	if err != nil {
		return "", nil, err
	}
	pages := make(map[int][]exportPage, len(slides))
	for _, page := range deck.Pages {
		pages[page.Slide] = append(pages[page.Slide], page)
	}
	return html, pages, nil
}

// deck is an owned render snapshot. Resolve only the edited slide after
// coalescing, unless a pending deck-wide update must be preserved.
func (p *presenterCompanion) RefreshDeckSlideAsync(path string, deck Deck, index, cols, rows int) {
	if p == nil || index < 0 || index >= len(deck.Slides) {
		return
	}
	p.mu.Lock()
	p.seq++
	p.refreshPath = path
	if p.slidePending && p.pendingSlide != index {
		p.fullPending = true
		p.refreshPath = path
	}
	seq, full := p.seq, p.fullPending
	p.slidePending, p.pendingSlide = !full, index
	p.mu.Unlock()
	go func() {
		time.Sleep(80 * time.Millisecond)
		p.mu.RLock()
		stale := seq != p.seq
		p.mu.RUnlock()
		if stale {
			return
		}
		if full {
			p.renderFullRefresh(seq, path, deck.ResolvedSlides(), cols, rows)
			return
		}
		pages := exportSlidePages(deck.slideRenderPreview(index, cols, rows), index, len(deck.Slides), cols, rows)
		p.mu.Lock()
		defer p.mu.Unlock()
		if seq != p.seq {
			return
		}
		if p.pages == nil {
			p.pages = map[int][]exportPage{}
		}
		p.pages[index] = pages
		p.slidePending = false
		p.state.DeckVersion++
		p.state.DeckSlide = index
		p.state.Version++
	}()
}

func (p *presenterCompanion) beginFullRefresh(path string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	p.fullPending = true
	p.slidePending = false
	p.refreshPath = path
	return p.seq
}

func (p *presenterCompanion) publishFullRefresh(seq int64, html string, pages map[int][]exportPage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if seq != p.seq {
		return false
	}
	p.html, p.pages = html, pages
	p.fullPending = false
	p.slidePending = false
	p.state.DeckVersion++
	p.state.DeckSlide = -1
	p.state.Version++
	return true
}

func (p *presenterCompanion) renderFullRefresh(seq int64, path string, slides []Slide, cols, rows int) {
	p.mu.RLock()
	stale := seq != p.seq
	p.mu.RUnlock()
	if stale {
		return
	}
	html, pages, err := projectPresenterDocument(path, slides, cols, rows)
	if err != nil {
		p.mu.RLock()
		current := seq == p.seq
		p.mu.RUnlock()
		if current {
			setUIError("Presenter refresh failed: " + err.Error())
		}
		return
	}
	p.publishFullRefresh(seq, html, pages)
}
