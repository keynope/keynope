package main

import (
	"net/url"
	"sort"
	"strconv"
)

func hiddenSlideObjects(slide Slide) map[int]bool {
	hidden := map[int]bool{}
	ids := map[string]bool{}
	for i, e := range slide.Elements {
		q, _ := url.ParseQuery(e.Query)
		if q.Get("object-hidden") == "1" {
			hidden[i] = true
			if e.ID != "" {
				ids[e.ID] = true
			}
		}
	}
	for i, e := range slide.Elements {
		if e.Kind == "connector" {
			q, _ := url.ParseQuery(e.Query)
			if ids[q.Get("connector-from")] || ids[q.Get("connector-to")] {
				hidden[i] = true
			}
		}
	}
	return hidden
}

// Once a stack has been authored, inserting into the Markdown array must not
// make new objects inherit a coincident rank or perturb existing layer order.
func preserveInsertedStack(before Slide, after *Slide, action nativeEditorAction) {
	if action.Action != "add-element" && action.Action != "duplicate-element" && action.Action != "paste-elements" {
		return
	}
	explicit := false
	for _, e := range before.Elements {
		q, _ := url.ParseQuery(e.Query)
		if q.Has("z-index") {
			explicit = true
			break
		}
	}
	if !explicit {
		return
	}
	existing := map[string]int{}
	for i, e := range after.Elements {
		existing[e.ID] = i
	}
	known := map[string]bool{}
	order := []int{}
	for _, index := range slidePaintOrder(before) {
		id := before.Elements[index].ID
		known[id] = true
		if i, ok := existing[id]; ok {
			order = append(order, i)
		}
	}
	added := []int{}
	for i, e := range after.Elements {
		if !known[e.ID] {
			added = append(added, i)
		}
	}
	if len(added) == 0 {
		return
	}
	// Clipboard source order can be spatial reading order rather than paint order.
	inserted := Slide{}
	for _, index := range added {
		inserted.Elements = append(inserted.Elements, after.Elements[index])
	}
	sortedAdded := make([]int, 0, len(added))
	for _, index := range slidePaintOrder(inserted) {
		sortedAdded = append(sortedAdded, added[index])
	}
	added = sortedAdded
	position := len(order)
	if action.Action == "duplicate-element" && action.Element >= 0 && action.Element < len(before.Elements) {
		id := before.Elements[action.Element].ID
		for i, index := range order {
			if after.Elements[index].ID == id {
				position = i + 1
				break
			}
		}
	}
	combined := append([]int{}, order[:position]...)
	combined = append(combined, added...)
	combined = append(combined, order[position:]...)
	for rank, index := range combined {
		after.Elements[index].Query = setQueryValue(after.Elements[index].Query, "z-index", strconv.Itoa(rank))
	}
}

// Paint order is independent of Markdown reading order. Unmodified legacy
// objects retain their existing back/front bands; an explicit reorder assigns
// every authored object a rank so later spatial sorting cannot change stacking.
func slidePaintOrder(slide Slide) []int {
	indices := make([]int, len(slide.Elements))
	ranks := make([]int, len(indices))
	for i, e := range slide.Elements {
		indices[i] = i
		ranks[i] = i
		if elementLayer(e) == "front" {
			ranks[i] += len(indices)
		}
		if e.Kind == "connector" {
			ranks[i] += 2 * len(indices)
		}
		q, _ := url.ParseQuery(e.Query)
		if rank, err := strconv.Atoi(q.Get("z-index")); err == nil && rank >= 0 && rank <= 1000000 {
			ranks[i] = rank
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := indices[i], indices[j]
		if slide.Elements[a].Inherited != slide.Elements[b].Inherited {
			return slide.Elements[a].Inherited
		}
		return ranks[a] < ranks[b]
	})
	return indices
}

func moveObjectLayer(slide *Slide, id string, delta int, editing ...string) (bool, error) {
	if delta != -1 && delta != 1 {
		return false, errInvalidEditorAction
	}
	direction := "forward"
	if delta < 0 {
		direction = "backward"
	}
	group := ""
	if len(editing) > 0 {
		group = editing[0]
	}
	return moveObjectStack(slide, id, direction, group)
}

// Groups move as one stack unit, unless the user explicitly entered that group.
func moveObjectStack(slide *Slide, id, direction, editingGroup string, target ...string) (bool, error) {
	if id == "" || (direction != "forward" && direction != "backward" && direction != "front" && direction != "back" && direction != "before" && direction != "after") {
		return false, errInvalidEditorAction
	}
	order := slidePaintOrder(*slide)
	units := [][]int{}
	groups := map[string]int{}
	position := -1
	for _, index := range order {
		e := slide.Elements[index]
		if e.Inherited {
			continue
		}
		group := elementGroup(e)
		unit, found := groups[group]
		if group == "" || group == editingGroup || !found {
			unit = len(units)
			units = append(units, nil)
			if group != "" && group != editingGroup {
				groups[group] = unit
			}
		}
		units[unit] = append(units[unit], index)
		if e.ID == id {
			position = unit
		}
	}
	if position < 0 {
		return false, errInvalidEditorAction
	}
	for _, index := range units[position] {
		if protectedActivityElement(slide.Elements[index]) {
			return false, errInvalidEditorAction
		}
	}
	next := position
	switch direction {
	case "forward":
		next++
	case "backward":
		next--
	case "front":
		next = len(units) - 1
	case "back":
		next = 0
	case "before", "after":
		if len(target) != 1 || target[0] == "" {
			return false, errInvalidEditorAction
		}
		found := -1
		for unit, indices := range units {
			for _, index := range indices {
				if slide.Elements[index].ID == target[0] {
					found = unit
				}
			}
		}
		if found < 0 {
			return false, errInvalidEditorAction
		}
		if found == position {
			return false, nil
		}
		next = found
		if position < found {
			next--
		}
		if direction == "after" {
			next++
		}
	}
	if next < 0 || next >= len(units) || next == position {
		return false, nil
	}
	moved := units[position]
	units = append(units[:position], units[position+1:]...)
	units = append(units, nil)
	copy(units[next+1:], units[next:])
	units[next] = moved
	order = nil
	for _, unit := range units {
		order = append(order, unit...)
	}
	for rank, index := range order {
		slide.Elements[index].Query = setQueryValue(slide.Elements[index].Query, "z-index", strconv.Itoa(rank))
	}
	return true, nil
}
