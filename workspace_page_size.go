package main

import (
	"encoding/json"
	"strconv"
)

// Compute the exact JSON payload cost without encoding every unchanged scene
// object again. Size records contain integers only, never additional scene or
// media references. Reordering simply misses reuse; identity is not inferred.
func workspaceEntryEncodedSize(pages []exportPage, input Slide, previous workspacePageEntry, comparisons ...*workspaceSceneComparisons) (int, [][]int, error) {
	wrapper, err := json.Marshal(struct {
		Pages []exportPage
		Input Slide
	}{nil, input})
	if err != nil {
		return 0, nil, err
	}
	size := len(wrapper)
	if pages == nil {
		return size, nil, nil
	}
	size += 2 - len("null") + max(0, len(pages)-1)
	costs := make([][]int, len(pages))
	for i, page := range pages {
		shell := page
		shell.Scene = nil
		encoded, err := json.Marshal(shell)
		if err != nil {
			return 0, nil, err
		}
		size += len(encoded)
		if page.Scene == nil {
			continue
		}
		header := *page.Scene
		header.Objects = nil
		encoded, err = json.Marshal(header)
		if err != nil {
			return 0, nil, err
		}
		size += len(`,"scene":`) + len(encoded)
		if page.Scene.Objects == nil {
			continue
		}
		size += 2 - len("null") + max(0, len(page.Scene.Objects)-1)
		costs[i] = make([]int, len(page.Scene.Objects))
		var old *slideScene
		var oldCosts []int
		if i < len(previous.pages) && i < len(previous.objectSizes) && previous.pages[i].Page == page.Page && previous.pages[i].Slide == page.Slide {
			old, oldCosts = previous.pages[i].Scene, previous.objectSizes[i]
		}
		for j, object := range page.Scene.Objects {
			if old != nil && j < len(old.Objects) && j < len(oldCosts) && oldCosts[j] > 0 && workspaceComparison(comparisons).equal(&old.Objects[j], &page.Scene.Objects[j]) {
				costs[i][j] = oldCosts[j]
			} else {
				encoded, err = json.Marshal(object)
				if err != nil {
					return 0, nil, err
				}
				costs[i][j] = len(encoded)
			}
			size += costs[i][j]
		}
	}
	return size, costs, nil
}

// The established budget charges encoded input/output, not total Go heap.
// Charge the new bounded size-index storage too (slice headers plus integers).
func workspaceSizeBookkeeping(costs [][]int) int {
	if costs == nil {
		return 0
	}
	words := 3 + 3*len(costs)
	for _, objects := range costs {
		words += len(objects)
	}
	return words * (strconv.IntSize / 8)
}
