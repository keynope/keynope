package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

type shapeAdjustmentEdit struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

func applyShapeAdjustment(slide *Slide, action nativeEditorAction, editingGroup string) (bool, error) {
	return applyShapeAdjustmentResolved(slide, *slide, action, editingGroup)
}

func applyShapeAdjustmentResolved(slide *Slide, resolved Slide, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.ShapeAdjustment == nil {
		return false, fmt.Errorf("choose a shape adjustment")
	}
	if err := requireSelectionObjectCommandProjected(Deck{}, *slide, &resolved, action.ObjectID, editingGroup, "shape.adjust"); err != nil {
		return false, err
	}
	indices, err := editableObjectIndices(*slide, []string{action.ObjectID})
	if err != nil {
		return false, err
	}
	e := &slide.Elements[indices[0]]
	if e.Kind != "shape" {
		return false, fmt.Errorf("only shapes have geometry adjustments")
	}
	if group := elementGroup(*e); group != "" && group != editingGroup {
		return false, fmt.Errorf("enter the group before adjusting an individual shape")
	}
	q, err := url.ParseQuery(e.Query)
	if err != nil {
		return false, err
	}
	effective := q
	for _, element := range resolved.Elements {
		if element.ID == e.ID {
			effective, err = url.ParseQuery(element.Query)
			if err != nil {
				return false, err
			}
			break
		}
	}
	spec := shapeSpecFromValues(effective)
	definition, _ := shapeDefinitionFor(spec.Kind)
	edit := action.ShapeAdjustment
	for _, a := range definition.Adjustments {
		if a.Key != edit.Key {
			continue
		}
		if math.IsNaN(edit.Value) || math.IsInf(edit.Value, 0) || edit.Value < a.Min || edit.Value > a.Max {
			return false, fmt.Errorf("%s must be between %g and %g", a.Label, a.Min, a.Max)
		}
		key := "shape-" + a.Key
		// Equal inherited values still establish an intentional sparse override.
		// Repeating an already explicit equivalent number remains a true no-op.
		previous, parseErr := strconv.ParseFloat(q.Get(key), 64)
		if parseErr == nil && math.Abs(previous-edit.Value) < 1e-10 {
			return false, nil
		}
		q.Set(key, strconv.FormatFloat(edit.Value, 'f', -1, 64))
		if e.MasterSlotID != "" {
			setMasterQueryClears(q, []string{key}, nil)
		}
		e.Query = q.Encode()
		return true, nil
	}
	return false, fmt.Errorf("this shape has no %q adjustment", edit.Key)
}

// Owned, revisioned previews never mutate the authoring model/history. Project
// only the adjusted shape and routes: unrelated media/text do not get encoded
// or repainted. Layout/routing still use the full visible slide as context.
func (s *nativeEditorSession) handleShapeAdjustmentDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&action) != nil {
		http.Error(w, "invalid shape adjustment draft", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current {
		s.mu.RUnlock()
		http.Error(w, "document changed; reselect the shape", http.StatusConflict)
		return
	}
	scope, scopeErr := captureSlideResolutionScope(s.deck, action.Slide, action.SceneMaster)
	editingGroup := s.editingGroup
	s.mu.RUnlock()
	if scopeErr != nil {
		http.Error(w, "invalid shape scope", http.StatusBadRequest)
		return
	}
	authored := &scope.source
	if _, err := applyShapeAdjustmentResolved(authored, scope.resolved(), action, editingGroup); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	visible, scopeErr := scope.resolvedEdited()
	if scopeErr != nil {
		http.Error(w, "ambiguous shape scope", http.StatusBadRequest)
		return
	}
	deck := scope.deck
	targets := map[string]bool{action.ObjectID: true}
	for _, e := range visible.Elements {
		if e.Kind == "connector" {
			targets[e.ID] = true
		}
	}
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildStyledSceneTargetsMeasured(deck, visible, action.Slide, cols, rows, true, targets, nil)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	foundTarget := false
	for _, object := range scene.Objects {
		foundTarget = foundTarget || object.ID == action.ObjectID
	}
	if !foundTarget {
		http.Error(w, "shape is not visible in this scope", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	valid := *action.SceneRevision == s.version && action.SceneMaster == s.masterMode &&
		((s.masterMode && action.Slide == s.currentMaster) || (!s.masterMode && action.Slide == s.current))
	s.mu.RUnlock()
	if !valid {
		http.Error(w, "document changed; draft discarded", http.StatusConflict)
		return
	}
	scene.Revision, scene.Master, scene.SlideIndex = action.SceneRevision, action.SceneMaster, action.Slide
	annotateSceneEditing(&scene, deck, authored.Elements)
	scene.BackgroundMedia, scene.Diagnostics = nil, nil
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(scene)
}
