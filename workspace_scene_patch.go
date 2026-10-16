package main

import "reflect"

// Wire-only pages borrow immutable cache data until JSON encoding. They are
// never retained as authored state or written into a deck/export archive.
type workspaceWirePage struct {
	exportPage
	ScenePatch *workspaceScenePatch `json:"scenePatch,omitempty"`
}
type workspaceScenePatch struct {
	Version  int           `json:"version"`
	SlideID  string        `json:"slideId"`
	Metadata *slideScene   `json:"metadata,omitempty"`
	Order    []string      `json:"order"`
	Changed  []sceneObject `json:"changed"`
	Removed  []string      `json:"removed"`
}

func scenePatchFor(previous, next *slideScene, comparisons ...*workspaceSceneComparisons) *workspaceScenePatch {
	if previous == nil || next == nil || previous.Version != 1 || next.Version != 1 || previous.SlideID == "" || previous.SlideID != next.SlideID || len(previous.Objects) > 10000 || len(next.Objects) > 10000 {
		return nil
	}
	old := map[string]int{}
	for i, object := range previous.Objects {
		if object.ID == "" {
			return nil
		}
		if _, duplicate := old[object.ID]; duplicate {
			return nil
		}
		old[object.ID] = i
	}
	patch := &workspaceScenePatch{Version: 1, SlideID: next.SlideID, Order: []string{}, Changed: []sceneObject{}, Removed: []string{}}
	seen := map[string]bool{}
	for i, object := range next.Objects {
		if object.ID == "" || seen[object.ID] {
			return nil
		}
		seen[object.ID] = true
		patch.Order = append(patch.Order, object.ID)
		if before, exists := old[object.ID]; !exists || !workspaceComparison(comparisons).equal(&previous.Objects[before], &next.Objects[i]) {
			patch.Changed = append(patch.Changed, object)
		}
	}
	// Preserve deterministic source order; do not iterate maps into the wire.
	for _, object := range previous.Objects {
		if !seen[object.ID] {
			patch.Removed = append(patch.Removed, object.ID)
		}
	}
	if len(next.Objects) > 0 && len(patch.Changed) == len(next.Objects) {
		return nil
	}
	a, b := *previous, *next
	a.Objects = nil
	b.Objects = nil
	if !reflect.DeepEqual(a, b) {
		patch.Metadata = &b
	}
	return patch
}

func workspaceWirePages(pages []exportPage, previous map[int]workspacePageEntry, patches bool, comparisons ...*workspaceSceneComparisons) []workspaceWirePage {
	out := make([]workspaceWirePage, 0, len(pages))
	for _, page := range pages {
		wire := workspaceWirePage{exportPage: page}
		if patches {
			for _, old := range previous[page.Slide].pages {
				if old.Page == page.Page {
					wire.ScenePatch = scenePatchFor(old.Scene, page.Scene, workspaceComparison(comparisons))
					break
				}
			}
		}
		if wire.ScenePatch != nil {
			wire.Scene = nil
		}
		out = append(out, wire)
	}
	return out
}
