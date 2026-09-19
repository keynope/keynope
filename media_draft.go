package main

import (
	"encoding/json"
	"net/http"
)

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
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode {
		s.mu.RUnlock()
		http.Error(w, "document changed; reopen the editor", 409)
		return
	}
	deck := cloneDeckForRender(s.deck)
	s.mu.RUnlock()
	var slide Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			http.Error(w, "invalid master", 400)
			return
		}
		slide = masterViewPreview(deck.Masters, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			http.Error(w, "invalid slide", 400)
			return
		}
		slide = deck.ResolveSlide(action.Slide, false)
	}
	// Keep layout context so flow-positioned and inherited images retain the
	// same bounds. Only the requested object's output leaves this endpoint.
	deck.Slides = []Slide{slide}
	action.Slide = 0
	if r.Context().Err() != nil {
		return
	}
	if _, err := applySceneCrop(&deck, action); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildStyledSceneTarget(deck, deck.Slides[0], 0, cols, rows, true, action.ObjectID)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	for _, object := range scene.Objects {
		if object.ID != action.ObjectID {
			continue
		}
		scene.Objects = []sceneObject{object}
		scene.Diagnostics = nil
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scene)
		return
	}
	http.Error(w, "image no longer exists", 404)
}
