package app

import "sort"

// Runtime-only immutable render baselines, not another document/history. Charge
// complete encoded pages plus size-index storage conservatively, even when
// nested media/table resources are shared. Missing/evicted baselines always
// retain the existing complete-page catch-up path.
const maxPresenterSceneBaselineBytes = 8 << 20
const maxPresenterSceneBaselinePages = 128

func presenterPageSizeEntry(pages []exportPage, previous workspacePageEntry, comparisons *workspaceSceneComparisons) workspacePageEntry {
	if len(pages) == 0 || len(pages) > 128 {
		return workspacePageEntry{}
	}
	remaining := maxPresenterSceneBaselineBytes
	mediaFits := func(media *sceneMedia) bool {
		if media == nil {
			return true
		}
		remaining -= len(media.Source)
		for _, frame := range media.Frames {
			remaining -= len(frame.Source)
			if remaining < 0 {
				return false
			}
		}
		return remaining >= 0
	}
	for _, page := range pages {
		if page.Scene == nil || page.Scene.Version != 1 || page.Scene.SlideID == "" || len(page.Scene.Objects) > 10000 {
			return workspacePageEntry{}
		}
		// Reject obviously oversized immutable resources before JSON sizing can
		// allocate a second large base64 buffer merely to discover a cache miss.
		if background := page.Scene.BackgroundMedia; background != nil && !mediaFits(&background.sceneMedia) {
			return workspacePageEntry{}
		}
		for _, object := range page.Scene.Objects {
			if !mediaFits(object.Media) {
				return workspacePageEntry{}
			}
		}
	}
	size, costs, err := workspaceEntryEncodedSize(pages, Slide{}, previous, comparisons)
	if err != nil || size <= 0 {
		return workspacePageEntry{}
	}
	size += workspaceSizeBookkeeping(costs) + 64 // Map/entry bookkeeping allowance.
	if size > maxPresenterSceneBaselineBytes {
		return workspacePageEntry{}
	}
	return workspacePageEntry{pages: pages, bytes: size, objectSizes: costs}
}

// The caller captured these immutable pages/hints under p.mu and owns the maps.
// JSON sizing and structured comparison run outside all document/presenter locks.
func preparePresenterSceneBaselines(previous map[int]workspacePageEntry, next map[int][]exportPage) (map[int]workspacePageEntry, map[int]workspacePageEntry) {
	before, after := map[int]workspacePageEntry{}, map[int]workspacePageEntry{}
	indices := make([]int, 0, len(next))
	for index := range next {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	comparisons := &workspaceSceneComparisons{}
	remaining := maxPresenterSceneBaselineBytes
	for _, index := range indices {
		if len(before) >= maxPresenterSceneBaselinePages || remaining <= 0 {
			break
		}
		old := previous[index]
		if old.bytes <= 0 {
			old = presenterPageSizeEntry(old.pages, workspacePageEntry{}, comparisons)
		}
		if old.bytes <= 0 || old.bytes > remaining {
			continue
		}
		before[index] = old
		remaining -= old.bytes
		if current := presenterPageSizeEntry(next[index], old, comparisons); current.bytes > 0 {
			after[index] = current
		}
	}
	return before, after
}

func retainPresenterPageSizeEntries(previous, next map[int]workspacePageEntry, changed []int) map[int]workspacePageEntry {
	retained := map[int]workspacePageEntry{}
	remaining := maxPresenterSceneBaselineBytes
	add := func(index int, entry workspacePageEntry) {
		if entry.bytes > 0 && entry.bytes <= remaining && len(retained) < maxPresenterSceneBaselinePages {
			retained[index] = entry
			remaining -= entry.bytes
		}
	}
	changedSet := map[int]bool{}
	for _, index := range changed {
		changedSet[index] = true
		add(index, next[index])
	}
	indices := make([]int, 0, len(previous))
	for index := range previous {
		if !changedSet[index] {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	for _, index := range indices {
		add(index, previous[index])
	}
	return retained
}

func (p *presenterCompanion) clearPresenterSceneBaselinesLocked() {
	for i := range p.pageChanges {
		p.pageChanges[i].before = nil
		p.pageChanges[i].bytes = 0
	}
}

// A lineage record owns its containers; page values themselves are immutable
// projection output. Drop oldest baselines on budget pressure without dropping
// changed-slide lineage, so slow clients still receive coherent full pages.
func (p *presenterCompanion) retainPresenterSceneBaselineBudgetLocked() {
	bytes := 0
	for _, entry := range p.pageChanges {
		bytes += entry.bytes
	}
	for i := range p.pageChanges {
		if bytes <= maxPresenterSceneBaselineBytes {
			break
		}
		bytes -= p.pageChanges[i].bytes
		p.pageChanges[i].before, p.pageChanges[i].bytes = nil, 0
	}
}
