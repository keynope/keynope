package main

import "net/url"

// Marquee selection is applied atomically, without adding a document mutation
// or exposing intermediate selections while the pointer is still down.
func (s *nativeEditorSession) selectGroupElements(elements []Element, action nativeEditorAction) error {
	if len(action.ElementIndices) != len(action.ElementsData) {
		return errInvalidEditorAction
	}
	selection := map[int]bool{}
	for n, index := range action.ElementIndices {
		index = nativeEditorElementIndex(elements, index, action.ElementsData[n].ID)
		if index < 0 {
			return errInvalidEditorAction
		}
		selection[index] = true
	}
	for index := range selection {
		if elementGroup(elements[index]) != s.editingGroup {
			s.editingGroup = ""
			break
		}
	}
	s.selection = selection
	s.expandSelectedGroups(elements)
	s.selected = -1
	for index := range elements {
		if s.selection[index] {
			s.selected = index
			break
		}
	}
	return nil
}

// Membership is slide-local metadata, so reordering never changes identity.
func elementGroup(element Element) string {
	values, _ := url.ParseQuery(element.Query)
	return values.Get("group")
}

func (s *nativeEditorSession) selectGroupElement(elements []Element, index int, toggle bool) {
	id := ""
	if index >= 0 {
		id = elementGroup(elements[index])
	}
	if id != s.editingGroup {
		s.editingGroup = ""
	}
	indices := []int{}
	if index >= 0 {
		indices = append(indices, index)
		if id != "" && id != s.editingGroup {
			indices = nil
			for i, e := range elements {
				if elementGroup(e) == id {
					indices = append(indices, i)
				}
			}
		}
	}
	remove := toggle && s.selection[index]
	if !toggle || s.selection == nil {
		s.selection = map[int]bool{}
	}
	for _, i := range indices {
		if remove {
			delete(s.selection, i)
		} else {
			s.selection[i] = true
		}
	}
	s.expandSelectedGroups(elements)
	s.selected = -1
	if s.selection[index] {
		s.selected = index
	} else {
		for i := range elements {
			if s.selection[i] {
				s.selected = i
				break
			}
		}
	}
}

func (s *nativeEditorSession) applyGroupAction(slide *Slide, action nativeEditorAction) (bool, error) {
	if action.Action == "exit-group" {
		s.editingGroup = ""
		s.selectGroupElement(slide.Elements, s.selected, false)
		return false, nil
	}
	if action.Action == "enter-group" {
		index := action.Element
		if action.ElementData != nil {
			index = nativeEditorElementIndex(slide.Elements, index, action.ElementData.ID)
		}
		if index < 0 || index >= len(slide.Elements) || elementGroup(slide.Elements[index]) == "" {
			return false, errInvalidEditorAction
		}
		s.editingGroup = elementGroup(slide.Elements[index])
		s.selected, s.selection = -1, map[int]bool{}
		return false, nil
	}
	indices, groups := map[int]bool{}, map[string]bool{}
	for i := range s.selection {
		if i >= 0 && i < len(slide.Elements) {
			indices[i] = true
			if id := elementGroup(slide.Elements[i]); id != "" {
				groups[id] = true
			}
		}
	}
	// Regrouping flattens existing groups, including any unselected members.
	for i, e := range slide.Elements {
		if groups[elementGroup(e)] {
			indices[i] = true
		}
	}
	if action.Action == "group-elements" && len(indices) < 2 || action.Action == "ungroup-elements" && len(groups) == 0 {
		return false, errInvalidEditorAction
	}
	id := ""
	if action.Action == "group-elements" {
		id = newStableID("group")
	}
	for i := range indices {
		if id == "" {
			slide.Elements[i].Query = removeImageQueryKeys(slide.Elements[i].Query, "group")
		} else {
			slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "group", id)
		}
	}
	s.editingGroup, s.selection = "", indices
	return true, nil
}

func pruneSingletonGroups(slide *Slide) {
	counts := map[string]int{}
	for _, e := range slide.Elements {
		counts[elementGroup(e)]++
	}
	for i, e := range slide.Elements {
		if id := elementGroup(e); id != "" && counts[id] < 2 {
			slide.Elements[i].Query = removeImageQueryKeys(e.Query, "group")
		}
	}
}

func freshPastedGroups(elements []Element) {
	ids := map[string]string{}
	for i, e := range elements {
		if old := elementGroup(e); old != "" {
			if ids[old] == "" {
				ids[old] = newStableID("group")
			}
			elements[i].Query = setQueryValue(e.Query, "group", ids[old])
		}
	}
}

// Resolve every target before writing, as sorting may have moved the indices
// since the browser captured this batch. Reject the whole action if stale.
func resolveEditorBatch(elements []Element, action *nativeEditorAction) error {
	seen := map[int]bool{}
	indices := make([]int, len(action.ElementIndices))
	for n, index := range action.ElementIndices {
		updated := action.ElementsData[n]
		index = nativeEditorElementIndex(elements, index, updated.ID)
		if index < 0 || updated.Kind == "" || seen[index] {
			return errInvalidEditorAction
		}
		seen[index] = true
		indices[n] = index
	}
	action.ElementIndices = indices
	return nil
}

func (s *nativeEditorSession) expandSelectedGroups(elements []Element) {
	ids := map[string]bool{}
	for i := range s.selection {
		if i >= 0 && i < len(elements) {
			if id := elementGroup(elements[i]); id != "" && id != s.editingGroup {
				ids[id] = true
			}
		}
	}
	for i, e := range elements {
		if ids[elementGroup(e)] {
			s.selection[i] = true
		}
	}
}
