package main

import (
	"reflect"
	"sort"
)

const maxNativeDependencyPages = 1024
const maxNativeDependencyBytes = 4 << 20

// Immutable metadata for one published native snapshot. It borrows only string
// keys, never scenes, media bytes, structured payloads or authored objects.
// Missing/over-budget entries fall back to the full exact comparison.
type nativeProjectionDependencies struct {
	refs  map[int]workspaceReferences
	index workspaceConsumerIndex
	bytes int
}

func nativeDependencyReferenceCost(ref workspaceReferences) int {
	// Conservative bookkeeping, including both forward and reverse map entries
	// and repeated key storage. This is an index budget, not a total heap claim.
	cost := 512 + 2*(len(ref.layout)+len(ref.build))
	for id := range ref.assets {
		cost += 128 + 2*len(id)
	}
	for id := range ref.payloads {
		cost += 160 + 2*(len(id.id)+len(id.kind))
	}
	return cost
}

func retainNativeProjectionDependencies(previous *nativeProjectionDependencies, changed map[int]workspaceReferences) *nativeProjectionDependencies {
	retained := &nativeProjectionDependencies{refs: map[int]workspaceReferences{}, index: newWorkspaceConsumerIndex()}
	add := func(page int, ref workspaceReferences) {
		cost := nativeDependencyReferenceCost(ref)
		if page < 0 || len(retained.refs) >= maxNativeDependencyPages || cost > maxNativeDependencyBytes-retained.bytes {
			return
		}
		// Own forward map containers too. A publication cannot accidentally
		// expose its index to a caller changing the temporary preparation map.
		owned := ref
		owned.assets = make(map[string]bool, len(ref.assets))
		for key, value := range ref.assets {
			owned.assets[key] = value
		}
		owned.payloads = make(map[workspacePayloadReference]bool, len(ref.payloads))
		for key, value := range ref.payloads {
			owned.payloads[key] = value
		}
		retained.refs[page] = owned
		retained.index.add(page, owned)
		retained.bytes += cost
	}
	keys := make([]int, 0, len(changed))
	for page := range changed {
		keys = append(keys, page)
	}
	sort.Ints(keys)
	for _, page := range keys {
		add(page, changed[page])
	}
	// Changed owners take priority; structural replacements pass nil previous.
	if previous != nil {
		keys = keys[:0]
		for page := range previous.refs {
			if _, replaced := changed[page]; !replaced {
				keys = append(keys, page)
			}
		}
		sort.Ints(keys)
		for _, page := range keys {
			add(page, previous.refs[page])
		}
	}
	return retained
}

func captureNativeProjectionDependencies(deck Deck) *nativeProjectionDependencies {
	// Initial binding owns a render snapshot. Build the first bounded index
	// outside the presenter/document locks, never during an ordinary local edit.
	refs := map[int]workspaceReferences{}
	cost := 0
	for page, source := range deck.Slides {
		if len(refs) >= maxNativeDependencyPages || cost >= maxNativeDependencyBytes {
			break
		}
		ref := workspacePageReferences(source, deck.ResolveSlide(page, false))
		if size := nativeDependencyReferenceCost(ref); size <= maxNativeDependencyBytes-cost {
			refs[page] = ref
			cost += size
		}
	}
	return retainNativeProjectionDependencies(nil, refs)
}

// Select conservative candidates without resolving every slide. Existing exact
// sceneProjectionInputsEqual still decides whether a candidate really changed;
// inheritance masks and unused theme roles therefore retain their old behavior.
func nativeProjectionCandidateSlides(before, after Deck, dependencies *nativeProjectionDependencies) []int {
	var candidates []int
	all := func() []int {
		for page := range after.Slides {
			candidates = append(candidates, page)
		}
		return candidates
	}
	if dependencies == nil || len(before.Slides) != len(after.Slides) {
		return all()
	}
	dataChanged, masterChanged, unknownMaster := dependencies.index.changedSources(before, after)
	shared := unknownMaster || !reflect.DeepEqual(before.PageSize, after.PageSize) ||
		before.HideActivityQR != after.HideActivityQR ||
		before.usesElementStyles() != after.usesElementStyles() ||
		!reflect.DeepEqual(before.Appearance, after.Appearance) ||
		!sceneObjectHeadersEqual(before.Objects, after.Objects) ||
		!reflect.DeepEqual(before.Extra, after.Extra) ||
		!reflect.DeepEqual(before.Theme, after.Theme)
	if shared {
		return all()
	}
	for page, slide := range after.Slides {
		_, indexed := dependencies.refs[page]
		if !indexed || dataChanged[page] || masterChanged[page] ||
			!reflect.DeepEqual(workspaceSlideInput(before.Slides[page]), workspaceSlideInput(slide)) {
			candidates = append(candidates, page)
		}
	}
	return candidates
}
