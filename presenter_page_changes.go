package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
)

func (p *presenterCompanion) handlePageUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	since := int64(-1)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			since = value
		}
	}
	snapshot := p.pageUpdateSince(since, r.URL.Query().Get("scenePatches") == "1")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(snapshot)
}

// Bounded version lineage lets a slow/background view catch up to several
// changed slides in one coherent response. Missing history or any structural
// publication falls back to a complete snapshot. Runtime-only; never authored.
type presenterPageChange struct {
	version int64
	full    bool
	slides  []int
	before  map[int]workspacePageEntry
	bytes   int
}
type presenterPageUpdate struct {
	Version       int64               `json:"version"`
	Pages         []exportPage        `json:"pages"`
	Delta         bool                `json:"delta,omitempty"`
	BaseVersion   int64               `json:"baseVersion,omitempty"`
	ReplaceSlides []int               `json:"replaceSlides,omitempty"`
	PatchedPages  []workspaceWirePage `json:"patchedPages,omitempty"`
}

func (p *presenterCompanion) recordPageChangeLocked(full bool, slides []int, baselines ...map[int]workspacePageEntry) {
	if len(slides) > 4096 {
		full = true
		slides = nil
	}
	entry := presenterPageChange{version: p.state.DeckVersion, full: full, slides: append([]int(nil), slides...)}
	if full {
		p.clearPresenterSceneBaselinesLocked()
	} else if len(baselines) > 0 {
		entry.before = make(map[int]workspacePageEntry)
		for _, index := range slides {
			if value := baselines[0][index]; value.bytes > 0 && value.bytes <= maxPresenterSceneBaselineBytes && len(entry.before) < maxPresenterSceneBaselinePages {
				entry.before[index] = value
				entry.bytes += value.bytes
			}
		}
	}
	p.pageChanges = append(p.pageChanges, entry)
	if len(p.pageChanges) > 64 {
		p.pageChanges = append([]presenterPageChange(nil), p.pageChanges[len(p.pageChanges)-64:]...)
	}
	p.retainPresenterSceneBaselineBudgetLocked()
}

func (p *presenterCompanion) pageUpdateSince(since int64, scenePatches ...bool) presenterPageUpdate {
	p.mu.RLock()
	wantPatches := len(scenePatches) > 0 && scenePatches[0]
	snapshot, previous := p.pageUpdateSinceLocked(since, wantPatches)
	p.mu.RUnlock()
	if !wantPatches || !snapshot.Delta || len(previous) == 0 {
		return snapshot
	}
	// Compare/encode nothing under the presenter publication lock. Both sides
	// are immutable render snapshots captured at this response's exact version.
	wires := workspaceWirePages(snapshot.Pages, previous, true, &workspaceSceneComparisons{})
	snapshot.Pages = []exportPage{}
	for _, wire := range wires {
		if wire.ScenePatch == nil {
			snapshot.Pages = append(snapshot.Pages, wire.exportPage)
		} else {
			snapshot.PatchedPages = append(snapshot.PatchedPages, wire)
		}
	}
	return snapshot
}

func (p *presenterCompanion) pageUpdateSinceLocked(since int64, scenePatches bool) (presenterPageUpdate, map[int]workspacePageEntry) {
	version := p.state.DeckVersion
	complete := func() presenterPageUpdate { return presenterPageUpdate{Version: version, Pages: p.slidePagesLocked()} }
	if since < 0 || since > version {
		return complete(), nil
	}
	changed := map[int]bool{}
	previous := map[int]workspacePageEntry{}
	next := since + 1
	for _, entry := range p.pageChanges {
		if entry.version <= since {
			continue
		}
		if entry.version != next || entry.full {
			return complete(), nil
		}
		for _, index := range entry.slides {
			// The first change after the requested version owns that slide's
			// exact baseline. A missing first baseline cannot use a later one.
			if scenePatches && !changed[index] {
				if baseline, ok := entry.before[index]; ok {
					previous[index] = baseline
				}
			}
			changed[index] = true
		}
		next++
	}
	if next != version+1 {
		return complete(), nil
	}
	indices := make([]int, 0, len(changed))
	for index := range changed {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	pages := []exportPage{}
	for _, index := range indices {
		pages = append(pages, p.pages[index]...)
	}
	return presenterPageUpdate{Version: version, Pages: pages, Delta: true, BaseVersion: since, ReplaceSlides: indices}, previous
}
