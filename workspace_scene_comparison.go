package main

import "reflect"

// Only the serial WASM workspace request uses this memo. Its scene snapshots
// are read-only until encoding completes; nothing here survives that request or
// enters the retained page cache. Reuse the expensive structured-object equality
// result between exact size accounting and wire-patch construction.
type workspaceSceneComparisons struct {
	entries map[[2]*sceneObject]bool
	hits    int
}

func (c *workspaceSceneComparisons) equal(a, b *sceneObject) bool {
	if c == nil || (a.Table == nil && b.Table == nil && a.Chart == nil && b.Chart == nil) {
		return workspaceSceneObjectEqual(*a, *b)
	}
	key := [2]*sceneObject{a, b}
	if result, found := c.entries[key]; found {
		c.hits++
		return result
	}
	result := workspaceSceneObjectEqual(*a, *b)
	if len(c.entries) < 128 {
		if c.entries == nil {
			c.entries = make(map[[2]*sceneObject]bool)
		}
		c.entries[key] = result
	}
	return result
}

func workspaceSceneObjectEqual(a, b sceneObject) bool {
	if !workspaceSceneTableEqual(a.Table, b.Table) {
		return false
	}
	a.Table, b.Table = nil, nil
	return reflect.DeepEqual(a, b)
}

func workspaceComparison(c []*workspaceSceneComparisons) *workspaceSceneComparisons {
	if len(c) > 0 {
		return c[0]
	}
	return nil
}
