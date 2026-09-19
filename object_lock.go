package main

import (
	"fmt"
	"net/url"
)

func objectLocked(element Element) bool {
	q, _ := url.ParseQuery(element.Query)
	return q.Get("object-locked") == "1"
}

// Resolve the entire authored selection before a command may write. Duplicate
// selection IDs are harmless; duplicate document identities are not.
func editableObjectIndices(slide Slide, ids []string) ([]int, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("select an object")
	}
	targets, seen := map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("object has no stable identity")
		}
		targets[id] = true
	}
	var indices []int
	for i, element := range slide.Elements {
		if !targets[element.ID] {
			continue
		}
		if seen[element.ID] {
			return nil, fmt.Errorf("ambiguous object identity")
		}
		seen[element.ID] = true
		if element.Inherited || objectLocked(element) || protectedActivityElement(element) {
			return nil, fmt.Errorf("object %s is inherited, locked or protected", element.ID)
		}
		if _, err := url.ParseQuery(element.Query); err != nil {
			return nil, err
		}
		indices = append(indices, i)
	}
	if len(seen) != len(targets) {
		return nil, fmt.Errorf("selected object no longer exists")
	}
	return indices, nil
}

// Validate before mutation: mixed selections must never partially edit a group.
func checkObjectLocks(slide Slide, action nativeEditorAction, selection map[int]bool, editingGroup string) error {
	targets := map[int]bool{}
	switch action.Action {
	case "update-elements":
		if err := resolveEditorBatch(slide.Elements, &action); err != nil {
			return err
		}
		for _, index := range action.ElementIndices {
			targets[index] = true
		}
	case "update-element":
		index := action.Element
		if action.ElementData != nil {
			index = nativeEditorElementIndex(slide.Elements, index, action.ElementData.ID)
		}
		targets[index] = true
	case "set-scene-text", "set-scene-crop", "move-object-layer":
		for i, element := range slide.Elements {
			if element.ID == action.ObjectID {
				targets[i] = true
			}
		}
	case "delete-element", "move-element", "convert-text-kind":
		targets[action.Element] = true
	case "delete-selection", "convert-selected-text-kind", "group-elements", "ungroup-elements":
		for index, selected := range selection {
			if selected {
				targets[index] = true
			}
		}
	default:
		return nil
	}
	for index := range targets {
		if index < 0 || index >= len(slide.Elements) {
			continue
		}
		element := slide.Elements[index]
		if objectLocked(element) {
			return fmt.Errorf("object is locked; unlock it in Objects before editing")
		}
		group := elementGroup(element)
		if group != "" && (group != editingGroup || action.Action == "group-elements" || action.Action == "ungroup-elements") {
			for _, member := range slide.Elements {
				if elementGroup(member) == group && objectLocked(member) {
					return fmt.Errorf("group contains a locked object; unlock it in Objects before editing")
				}
			}
		}
	}
	return nil
}

func setObjectLocks(slide *Slide, selection map[int]bool, selected int, locked bool) bool {
	changed := false
	for index := range slide.Elements {
		if index != selected && !selection[index] {
			continue
		}
		if objectLocked(slide.Elements[index]) == locked {
			continue
		}
		q, _ := url.ParseQuery(slide.Elements[index].Query)
		if locked {
			q.Set("object-locked", "1")
		} else {
			q.Del("object-locked")
		}
		slide.Elements[index].Query = q.Encode()
		changed = true
	}
	return changed
}
