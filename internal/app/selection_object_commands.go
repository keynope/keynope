package app

import (
	"fmt"
	"net/url"
)

// Typed editors share the same selection/lock contract as formatting. Most
// require one independent owner; image adjustments deliberately target a named
// subset, and builds expand logical groups rather than silently editing a child.
type selectionObjectCommand struct {
	kind   string
	single bool
}

var selectionObjectCommands = map[string]selectionObjectCommand{
	"image.adjust":     {kind: "image"},
	"image.crop":       {kind: "image", single: true},
	"image.background": {kind: "image", single: true},
	"table.edit":       {kind: "table", single: true},
	"table.fit":        {kind: "table", single: true},
	"chart.edit":       {kind: "chart", single: true},
	"shape.adjust":     {kind: "shape", single: true},
	"build.add":        {},
}

func selectionObjectCommandEligible(deck Deck, element, resolved Element, name string, definition selectionObjectCommand) bool {
	if definition.kind != "" && element.Kind != definition.kind {
		return false
	}
	switch name {
	case "image.background", "image.crop":
		_, exists := deck.Assets[element.AssetID]
		return element.AssetID != "" && exists
	case "table.edit", "table.fit":
		if deck.Objects == nil {
			return false
		}
		_, exists := deck.Objects.Tables[element.ObjectID]
		return exists
	case "chart.edit":
		if deck.Objects == nil {
			return false
		}
		_, exists := deck.Objects.Charts[element.ObjectID]
		return exists
	case "shape.adjust":
		q, err := url.ParseQuery(resolved.Query)
		if err != nil {
			return false
		}
		spec := shapeSpecFromValues(q)
		shape, exists := shapeDefinitionFor(spec.Kind)
		return exists && len(shape.Adjustments) > 0
	case "build.add":
		q, err := url.ParseQuery(resolved.Query)
		return err == nil && validObjectID(element.ID) && q.Get("object-hidden") != "1" &&
			!protectedActivityElement(element) && (selectionObjectCanPosition(element) || element.Kind == "connector")
	}
	return true
}

func buildSelectionObjectCommands(deck Deck, slide Slide, resolved map[string]Element, ids []string, editingGroup, blocked string) map[string]selectionCommandState {
	byID := make(map[string]Element, len(ids))
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	for _, element := range slide.Elements {
		if wanted[element.ID] {
			byID[element.ID] = element
		}
	}
	result := make(map[string]selectionCommandState, len(selectionObjectCommands))
	for name, definition := range selectionObjectCommands {
		state := selectionCommandState{EligibleIDs: []string{}, TotalCount: len(ids), Reason: blocked}
		for _, id := range ids {
			element, exists := byID[id]
			projected, ok := resolved[id]
			if !ok {
				projected = element
			}
			if !exists || !selectionObjectCommandEligible(deck, element, projected, name, definition) {
				continue
			}
			state.EligibleIDs = append(state.EligibleIDs, id)
			if definition.single && elementGroup(element) != "" && elementGroup(element) != editingGroup && state.Reason == "" {
				state.Reason = "Enter the group to edit this object independently."
			}
		}
		if definition.single && len(ids) != 1 && state.Reason == "" {
			state.Reason = "Select one independent " + definition.kind + " object."
		}
		if len(state.EligibleIDs) == 0 && state.Reason == "" {
			state.Reason = "This selection has no available target for this operation."
		}
		if name == "build.add" && len(state.EligibleIDs) != len(ids) && state.Reason == "" {
			state.Reason = "Show all selected objects before adding them to a build."
		}
		state.Enabled = len(state.EligibleIDs) > 0 && state.Reason == ""
		result[name] = state
	}
	return result
}

// The editor snapshot is advisory. Recompute the single-owner contract inside
// the actual typed transaction, including whole-group lock expansion.
func requireSelectionObjectCommand(deck Deck, slide Slide, id, editingGroup, name string) error {
	return requireSelectionObjectCommandProjected(deck, slide, nil, id, editingGroup, name)
}

// Resolved geometry is needed for inherited shape types; table/chart/media
// ownership can be checked without resolving or rendering any slide.
func requireSelectionObjectCommandProjected(deck Deck, slide Slide, resolved *Slide, id, editingGroup, name string) error {
	selected, err := typographySelection(slide, []string{id}, editingGroup)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(selected))
	for _, element := range slide.Elements {
		if selected[element.ID] {
			ids = append(ids, element.ID)
		}
	}
	var byID map[string]Element
	if resolved != nil {
		byID = make(map[string]Element, len(ids))
		for _, element := range resolved.Elements {
			if selected[element.ID] {
				byID[element.ID] = element
			}
		}
	}
	state, exists := buildSelectionObjectCommands(deck, slide, byID, ids, editingGroup, "")[name]
	if !exists || !state.Enabled {
		return fmt.Errorf("%s: %s", name, state.Reason)
	}
	return nil
}
