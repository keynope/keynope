package main

// These identifiers are shared with the inspector through the resolved scene.
// They describe type/treatment support, not permission: locked, inherited and
// protected targets are still preflighted before any command writes anything.
func supportsTextCapability(element Element, style, capability string) bool {
	switch element.Kind {
	case "text", "heading", "bullet", "code", "text-image":
	default:
		return false
	}
	switch capability {
	case "size", "emphasis", "alignment":
		return true
	case "width":
		return style == "modern" || isTrueType(element)
	case "font", "paragraph":
		return style == "modern"
	}
	return false
}

func textCapabilities(element Element, style string) []string {
	result := []string{}
	for _, capability := range []string{"size", "width", "emphasis", "alignment", "font", "paragraph"} {
		if supportsTextCapability(element, style, capability) {
			result = append(result, capability)
		}
	}
	return result
}

// Collapsed groups expand to children; an entered group keeps its explicit
// selection. Validate every member before filtering by command capability.
func typographySelection(slide Slide, objectIDs []string, editingGroup string) (map[string]bool, error) {
	ids := append([]string(nil), objectIDs...)
	selected, groups := map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	for _, element := range slide.Elements {
		if group := elementGroup(element); selected[element.ID] && group != "" && group != editingGroup {
			groups[group] = true
		}
	}
	for _, element := range slide.Elements {
		if groups[elementGroup(element)] && !selected[element.ID] {
			ids = append(ids, element.ID)
			selected[element.ID] = true
		}
	}
	if _, err := editableObjectIndices(slide, ids); err != nil {
		return nil, err
	}
	return selected, nil
}
