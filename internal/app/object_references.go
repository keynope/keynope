package app

import (
	"fmt"
	"net/url"
	"reflect"
)

type objectReferenceScope struct {
	visible []Element
}

// A detached timeline can carry future semantics in nested transition/step
// fields, not just at its root. Preserve it as inert archive data when its
// authored slide disappears; there is no live scope in which to execute it.
func buildHasOpaqueMetadata(build SlideBuild) bool {
	if build.Extra != nil && len(*build.Extra) > 0 || build.Transition.Extra != nil && len(*build.Transition.Extra) > 0 {
		return true
	}
	for _, step := range build.Steps {
		if step.Extra != nil && len(*step.Extra) > 0 {
			return true
		}
	}
	return false
}

func connectorReferenceSignature(e Element, q url.Values) string {
	refs := url.Values{}
	for _, key := range []string{"connector-from", "connector-to", "connector-from-side", "connector-to-side"} {
		refs[key] = q[key]
	}
	return e.ID + "\n" + refs.Encode()
}

// Older editors left inert connectors behind when a copied slide no longer
// contained their endpoints. Preserve the authored data, not guesses at shapes
// on other slides. Ambiguous IDs, self-links and malformed parameters are not
// recoverable by this allowance.
func danglingConnectorReferences(q url.Values, elements map[string]Element, counts map[string]int) bool {
	from, to := q.Get("connector-from"), q.Get("connector-to")
	if from == "" || to == "" || from == to || counts[from] > 1 || counts[to] > 1 {
		return false
	}
	for _, id := range []string{from, to} {
		if counts[id] == 1 && elements[id].Kind != "shape" {
			return false
		}
	}
	for _, key := range []string{"connector-from-side", "connector-to-side"} {
		switch q.Get(key) {
		case "left", "right", "top", "bottom":
		default:
			return false
		}
	}
	return counts[from] == 0 || counts[to] == 0
}

func preserveLoadedDanglingConnectors(deck *Deck) {
	recovered := 0
	mark := func(slide *Slide, visible []Element) {
		ids, counts := referenceElementIndex(visible)
		for i := range slide.Elements {
			e := &slide.Elements[i]
			if e.Kind != "connector" {
				continue
			}
			q, err := url.ParseQuery(e.Query)
			if err == nil && danglingConnectorReferences(q, ids, counts) {
				e.unresolvedConnectorReference = connectorReferenceSignature(*e, q)
				recovered++
			}
		}
	}
	for i := range deck.Slides {
		for _, e := range deck.Slides[i].Elements {
			if e.Kind == "connector" {
				mark(&deck.Slides[i], deck.ResolveSlide(i, false).Elements)
				break
			}
		}
	}
	mark(&deck.Masters.Base.Slide, deck.Masters.Base.Slide.Elements)
	for i := range deck.Masters.Layouts {
		slide := &deck.Masters.Layouts[i].Slide
		visible := append(append([]Element(nil), deck.Masters.Base.Slide.Elements...), slide.Elements...)
		mark(slide, visible)
	}
	if recovered > 0 {
		deck.Diagnostics = append(deck.Diagnostics, documentDiagnostic{
			Code:    "dangling-connectors-preserved",
			Message: fmt.Sprintf("Preserved %d existing connector(s) whose shapes are missing from their slide. These lines stay invisible until their endpoints are restored or the lines are removed.", recovered),
		})
	}
}

// IDs identify authored objects within a slide/master scope. A visible master
// instance can be used as a target, but an ambiguous projection cannot.
func validateObjectReferenceScopes(deck Deck) error {
	if err := deck.Objects.Validate(); err != nil {
		return err
	}
	scopes := map[string]objectReferenceScope{}
	check := func(slide Slide, visible []Element) error {
		if slide.ID != "" {
			if _, exists := scopes[slide.ID]; !validObjectID(slide.ID) || exists {
				return fmt.Errorf("invalid or duplicate slide ID %q", slide.ID)
			}
			scopes[slide.ID] = objectReferenceScope{visible: visible}
		}
		ids := map[string]bool{}
		for _, e := range slide.Elements {
			if e.ID != "" {
				if !validObjectID(e.ID) || ids[e.ID] {
					return fmt.Errorf("invalid or duplicate object ID %q in slide %q", e.ID, slide.ID)
				}
				ids[e.ID] = true
			}
			if e.Kind == "table" || e.Kind == "chart" {
				if !validObjectID(e.ID) || !validObjectID(e.ObjectID) || deck.Objects == nil {
					return fmt.Errorf("%s %q has no structured payload or stable identity", e.Kind, e.ID)
				}
				if e.Kind == "table" {
					if _, ok := deck.Objects.Tables[e.ObjectID]; !ok {
						return fmt.Errorf("missing table payload %q", e.ObjectID)
					}
				} else if _, ok := deck.Objects.Charts[e.ObjectID]; !ok {
					return fmt.Errorf("missing chart payload %q", e.ObjectID)
				}
			}
		}
		visibleIDs, counts := referenceElementIndex(visible)
		for _, e := range slide.Elements {
			if e.Kind != "connector" {
				continue
			}
			q, err := url.ParseQuery(e.Query)
			if err != nil {
				return fmt.Errorf("connector %q has invalid parameters: %w", e.ID, err)
			}
			from, to := q.Get("connector-from"), q.Get("connector-to")
			if from == to || counts[from] != 1 || counts[to] != 1 || visibleIDs[from].Kind != "shape" || visibleIDs[to].Kind != "shape" {
				if e.unresolvedConnectorReference == "" || e.unresolvedConnectorReference != connectorReferenceSignature(e, q) || !danglingConnectorReferences(q, visibleIDs, counts) {
					return fmt.Errorf("connector %q has missing or ambiguous shape endpoints", e.ID)
				}
			}
			for _, key := range []string{"connector-from-side", "connector-to-side"} {
				switch q.Get(key) {
				case "left", "right", "top", "bottom":
				default:
					return fmt.Errorf("connector %q has invalid attachment side", e.ID)
				}
			}
		}
		return nil
	}
	for i, slide := range deck.Slides {
		visible := slide.Elements
		if slide.LayoutID != "" {
			visible = deck.ResolveSlide(i, false).Elements
		}
		if err := check(slide, visible); err != nil {
			return err
		}
	}
	if err := check(deck.Masters.Base.Slide, deck.Masters.Base.Slide.Elements); err != nil {
		return err
	}
	for _, master := range deck.Masters.Layouts {
		visible := append(append([]Element(nil), deck.Masters.Base.Slide.Elements...), master.Slide.Elements...)
		if err := check(master.Slide, visible); err != nil {
			return err
		}
	}
	if deck.Objects == nil {
		return nil
	}
	for id, build := range deck.Objects.Builds {
		scope, exists := scopes[id]
		if !exists {
			if buildHasOpaqueMetadata(build) || deck.Objects.Extra != nil {
				continue // Preserve opaque extension-owned metadata, never execute it.
			}
			return fmt.Errorf("build references missing slide %q", id)
		}
		_, targets := referenceElementIndex(scope.visible)
		for _, step := range build.Steps {
			for _, target := range step.TargetIDs {
				if targets[target] != 1 {
					return fmt.Errorf("build on slide %q references missing or ambiguous object %q", id, target)
				}
			}
		}
	}
	return nil
}

func referenceElementIndex(elements []Element) (map[string]Element, map[string]int) {
	byID, counts := map[string]Element{}, map[string]int{}
	for _, e := range elements {
		if e.ID != "" {
			byID[e.ID], counts[e.ID] = e, counts[e.ID]+1
		}
	}
	return byID, counts
}

// Remove only references to targets that this edit actually removed. Invalid
// newly introduced references remain errors, not a silent cleanup policy.
func reconcileSlideObjectReferences(deck *Deck, before Slide, after *Slide, oldVisible, newVisible []Element) error {
	reconcileBackgroundSourceOwner(before, after)
	_, oldIDs := referenceElementIndex(oldVisible)
	_, newIDs := referenceElementIndex(newVisible)
	removed := map[string]bool{}
	for id := range oldIDs {
		if newIDs[id] == 0 {
			removed[id] = true
		}
	}
	if len(removed) == 0 {
		return nil
	}
	elements := make([]Element, 0, len(after.Elements))
	for _, e := range after.Elements {
		if e.Kind == "connector" {
			q, _ := url.ParseQuery(e.Query)
			if removed[q.Get("connector-from")] || removed[q.Get("connector-to")] {
				if objectLocked(e) {
					return fmt.Errorf("unlock connected line %q before removing its endpoint", e.ID)
				}
				removed[e.ID] = true
				continue
			}
		}
		elements = append(elements, e)
	}
	after.Elements = elements
	pruneSingletonGroups(after)
	if deck.Objects == nil || before.ID == "" || before.ID != after.ID {
		return nil
	}
	build, exists := deck.Objects.Builds[before.ID]
	if !exists {
		return nil
	}
	updated := cloneBuild(build)
	steps := updated.Steps
	updated.Steps = nil
	for _, step := range steps {
		copyStep := step
		copyStep.TargetIDs = nil
		for _, id := range step.TargetIDs {
			if !removed[id] {
				copyStep.TargetIDs = append(copyStep.TargetIDs, id)
			}
		}
		if len(copyStep.TargetIDs) > 0 {
			updated.Steps = append(updated.Steps, copyStep)
		}
	}
	if !reflect.DeepEqual(build, updated) {
		deck.Objects.Builds[before.ID] = updated
	}
	return nil
}

// A promotion owner is advisory placement, not the background's source bytes.
// Deleting/replacing that image must retain the background and let it convert
// like imported media. Clear only a formerly valid local relationship actually
// invalidated by this edit; never guess at unrelated or inherited objects.
func reconcileBackgroundSourceOwner(before Slide, after *Slide) {
	previous, current := before.BackgroundMedia, after.BackgroundMedia
	if previous == nil || current == nil || !before.BackgroundMediaSet || !after.BackgroundMediaSet ||
		previous.SourceObjectID == "" || previous.SourceObjectID != current.SourceObjectID || previous.AssetID != current.AssetID {
		return
	}
	id := current.SourceObjectID
	if !backgroundHasSourceOwner(before, id, current.AssetID) {
		return
	}
	if backgroundHasSourceOwner(*after, id, current.AssetID) {
		return
	}
	after.BackgroundMedia = cloneSlideBackgroundMedia(current)
	after.BackgroundMedia.SourceObjectID = ""
}

func backgroundHasSourceOwner(slide Slide, id, assetID string) bool {
	count, valid := 0, false
	for _, element := range slide.Elements {
		if element.ID == id {
			count++
			valid = element.Kind == "image" && element.AssetID == assetID
		}
	}
	return count == 1 && valid
}

func editorActionChangesObjectReferences(action string) bool {
	switch action {
	case "add-element", "update-element", "update-elements", "paste-elements", "paste-objects", "duplicate-element",
		"delete-element", "delete-selection", "delete-objects", "clone-slide", "delete-slide", "add-slide", "add-layout",
		"delete-layout", "update-layout", "set-layout", "update-slide", "toggle-page-number", "convert-text-kind", "convert-selected-text-kind":
		return true
	}
	return false
}

// Called within the existing history transaction. Payloads are deliberately
// not garbage-collected here: the save snapshot owns that separate operation.
func reconcileEditorObjectReferences(deck *Deck, before Deck, action nativeEditorAction, current int, master bool) error {
	if !editorActionChangesObjectReferences(action.Action) {
		return nil
	}
	// Reconcile sources before their projections. Removing a master endpoint
	// also removes its connector, which normal-slide timelines may target.
	if master || action.Action == "update-layout" || action.Action == "delete-layout" {
		if err := reconcileSlideObjectReferences(deck, before.Masters.Base.Slide, &deck.Masters.Base.Slide, before.Masters.Base.Slide.Elements, deck.Masters.Base.Slide.Elements); err != nil {
			return err
		}
		oldLayouts := map[string]MasterLayout{}
		for _, m := range before.Masters.Layouts {
			oldLayouts[m.ID] = m
		}
		for i := range deck.Masters.Layouts {
			m := &deck.Masters.Layouts[i]
			if old, exists := oldLayouts[m.ID]; exists {
				oldVisible := append(append([]Element(nil), before.Masters.Base.Slide.Elements...), old.Slide.Elements...)
				newVisible := append(append([]Element(nil), deck.Masters.Base.Slide.Elements...), m.Slide.Elements...)
				if err := reconcileSlideObjectReferences(deck, old.Slide, &m.Slide, oldVisible, newVisible); err != nil {
					return err
				}
			}
		}
	}
	oldSlides := map[string]int{}
	for i, s := range before.Slides {
		if s.ID != "" {
			oldSlides[s.ID] = i
		}
	}
	for i := range deck.Slides {
		oldIndex, exists := oldSlides[deck.Slides[i].ID]
		if deck.Slides[i].ID == "" {
			// Legacy slides have no timeline. On non-structural edits their
			// position is stable; never guess across a structural reorder/delete.
			exists = i < len(before.Slides) && len(deck.Slides) == len(before.Slides) &&
				(master || action.Action != "clone-slide" && action.Action != "delete-slide" && action.Action != "add-slide")
			oldIndex = i
		}
		if exists && (master || i == current || action.Action == "update-layout" || action.Action == "delete-layout") {
			old, updated := before.ResolveSlide(oldIndex, false), deck.ResolveSlide(i, false)
			if err := reconcileSlideObjectReferences(deck, before.Slides[oldIndex], &deck.Slides[i], old.Elements, updated.Elements); err != nil {
				return err
			}
		}
	}
	if deck.Objects != nil {
		live, previouslyLive := authoredSlideIdentities(*deck), authoredSlideIdentities(before)
		for id, b := range deck.Objects.Builds {
			if previouslyLive[id] && !live[id] && !buildHasOpaqueMetadata(b) && deck.Objects.Extra == nil {
				delete(deck.Objects.Builds, id)
			}
		}
	}
	return validateDeckObjectReferences(*deck)
}

func authoredSlideIdentities(deck Deck) map[string]bool {
	ids := map[string]bool{}
	for _, s := range deck.Slides {
		if s.ID != "" {
			ids[s.ID] = true
		}
	}
	if deck.Masters.Base.Slide.ID != "" {
		ids[deck.Masters.Base.Slide.ID] = true
	}
	for _, m := range deck.Masters.Layouts {
		if m.Slide.ID != "" {
			ids[m.Slide.ID] = true
		}
	}
	return ids
}
