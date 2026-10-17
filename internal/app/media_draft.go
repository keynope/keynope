package app

import (
	"encoding/json"
	"net/http"
)

// Caller holds the document read lock. Metadata is owned; encoded source/frame
// bytes are borrowed immutable, exactly as in the existing render snapshots.
func captureMediaDraftAsset(deck Deck, id string) map[string]DeckAsset {
	asset, exists := deck.Assets[id]
	if !exists || id == "" {
		return nil
	}
	return cloneDeckForRender(Deck{Assets: map[string]DeckAsset{id: asset}}).Assets
}

// Preview crop/mask through the same renderer as the committed scene. Work on
// an isolated resolved slide; source pixels and the author's deck stay intact.
func (s *nativeEditorSession) handleMediaDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var action nativeEditorAction
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&action) != nil {
		http.Error(w, "invalid image draft", 400)
		return
	}
	s.mu.RLock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current {
		s.mu.RUnlock()
		http.Error(w, "document changed; reopen the editor", 409)
		return
	}
	var authored *Slide
	if s.masterMode {
		if current >= 0 && current <= len(s.deck.Masters.Layouts) {
			authored = masterSlideAt(&s.deck, current)
		}
	} else if current >= 0 && current < len(s.deck.Slides) {
		authored = &s.deck.Slides[current]
	}
	if authored == nil {
		s.mu.RUnlock()
		http.Error(w, "invalid image destination", 400)
		return
	}
	command := action
	command.Action = "set-scene-crop"
	targetErr := requireSelectionObjectCommand(s.deck, *authored, action.ObjectID, s.editingGroup, "image.crop")
	if targetErr == nil {
		targetErr = checkObjectLocks(*authored, command, s.selection, s.editingGroup)
	}
	if targetErr != nil {
		s.mu.RUnlock()
		http.Error(w, targetErr.Error(), 400)
		return
	}
	scope, scopeErr := captureSlideResolutionScope(s.deck, action.Slide, action.SceneMaster)
	s.mu.RUnlock()
	if scopeErr != nil {
		http.Error(w, "invalid image scope", 400)
		return
	}
	if scope.master && scope.index > 0 && !scope.layoutIsSource {
		http.Error(w, "ambiguous image scope", 400)
		return
	}
	slide := scope.resolved()
	assetID := ""
	for _, element := range slide.Elements {
		if element.ID == action.ObjectID {
			assetID = element.AssetID
			break
		}
	}
	// Resolve the target outside the document lock, then copy only its media
	// descriptors under a fresh scope check. Encoded bytes remain immutable,
	// using the existing render-snapshot contract; no unrelated assets survive.
	s.mu.RLock()
	valid := *action.SceneRevision == s.version && action.SceneMaster == s.masterMode && ((s.masterMode && action.Slide == s.currentMaster) || (!s.masterMode && action.Slide == s.current))
	var assets map[string]DeckAsset
	if valid {
		assets = captureMediaDraftAsset(s.deck, assetID)
	}
	s.mu.RUnlock()
	if !valid {
		http.Error(w, "document changed; draft discarded", 409)
		return
	}
	if len(assets) == 0 {
		http.Error(w, "image source is unavailable", 400)
		return
	}
	originalSlide := action.Slide
	if r.Context().Err() != nil {
		return
	}
	// Apply to the eligible authored owner, not to its inherited projection.
	// A layout's own image is marked inherited in masterViewPreview, but that
	// must not prevent editing it while the layout itself is selected.
	working := Deck{Slides: []Slide{scope.source}, Assets: assets}
	projection := scope.deck
	projection.Assets = assets
	mutation := action
	mutation.Slide = 0
	if _, err := applySceneCropResolved(&working, mutation, projection, slide, originalSlide); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	scope.source = working.Slides[0]
	slide, scopeErr = scope.resolvedEdited()
	if scopeErr != nil {
		http.Error(w, "ambiguous image scope", 400)
		return
	}
	deck := projection
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildStyledSceneTarget(deck, slide, originalSlide, cols, rows, true, action.ObjectID)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.RLock()
	valid = *action.SceneRevision == s.version && action.SceneMaster == s.masterMode && ((s.masterMode && originalSlide == s.currentMaster) || (!s.masterMode && originalSlide == s.current))
	s.mu.RUnlock()
	if !valid {
		http.Error(w, "document changed; draft discarded", 409)
		return
	}
	scene.Revision, scene.Master, scene.SlideIndex = action.SceneRevision, action.SceneMaster, originalSlide
	for _, object := range scene.Objects {
		if object.ID != action.ObjectID {
			continue
		}
		scene.Objects = []sceneObject{object}
		scene.Diagnostics = nil
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(scene)
		return
	}
	http.Error(w, "image no longer exists", 404)
}
