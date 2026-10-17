package app

import (
	"encoding/json"
	"io"
	"net/http"
)

// Drafts use the production scene projection on an owned snapshot. The browser
// never has to guess font metrics, inheritance, padding or table paint rules.
func (s *nativeEditorSession) handleTableDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStructuredObjectBytes+1024))
	if decoder.Decode(&action) != nil || action.TableData == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid table draft", http.StatusBadRequest)
		return
	}
	if err := action.TableData.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current {
		s.mu.RUnlock()
		http.Error(w, "document changed; reopen the editor", http.StatusConflict)
		return
	}
	scope, scopeErr := captureSlideResolutionScope(s.deck, action.Slide, action.SceneMaster)
	editingGroup := s.editingGroup
	// The incoming table is already an owned, validated draft. Only existence
	// of its authored payload is needed from the registry, not a copy of every
	// table/chart/build in the document (or even the old selected table).
	payloadExists := false
	if scopeErr == nil && s.deck.Objects != nil {
		for _, element := range scope.source.Elements {
			if element.ID == action.ObjectID && element.Kind == "table" {
				_, payloadExists = s.deck.Objects.Tables[element.ObjectID]
				break
			}
		}
	}
	s.mu.RUnlock()
	if scopeErr != nil || scope.master && scope.index > 0 && !scope.layoutIsSource {
		http.Error(w, "invalid or ambiguous table scope", http.StatusBadRequest)
		return
	}
	deck, authored, resolved := scope.deck, scope.source, scope.resolved()
	var target *Element
	for i := range authored.Elements {
		element := &authored.Elements[i]
		if element.ID == action.ObjectID && element.Kind == "table" {
			target = element
			break
		}
	}
	if target == nil {
		http.Error(w, "table no longer exists", http.StatusNotFound)
		return
	}
	if !payloadExists {
		http.Error(w, "table data is missing", http.StatusNotFound)
		return
	}
	if _, err := typographySelection(authored, []string{target.ID}, editingGroup); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deck.Objects = &DeckObjects{Tables: map[string]TableData{target.ObjectID: *action.TableData}}
	// Table placement is explicit; sibling layout does not determine its frame.
	// Keep the resolved slide's typography/theme context, but don't lay out
	// unrelated text or decode background/media when previewing one cell style.
	for _, element := range resolved.Elements {
		if element.ID == target.ID {
			resolved.Elements = []Element{element}
			break
		}
	}
	resolved.BackgroundMedia = nil
	if r.Context().Err() != nil {
		return
	}
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildStyledSceneTarget(deck, resolved, action.Slide, cols, rows, true, target.ID)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	current = s.current
	if s.masterMode {
		current = s.currentMaster
	}
	stale := s.version != *action.SceneRevision || s.masterMode != action.SceneMaster || current != action.Slide || editingGroup != s.editingGroup
	s.mu.RUnlock()
	if stale {
		http.Error(w, "document changed; reopen the editor", http.StatusConflict)
		return
	}
	for _, object := range scene.Objects {
		if object.ID == target.ID && object.Table != nil {
			scene.Objects, scene.Diagnostics = []sceneObject{object}, nil
			scene.Master, scene.SlideIndex, scene.Revision = action.SceneMaster, action.Slide, action.SceneRevision
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(scene)
			return
		}
	}
	http.Error(w, "table could not be rendered", http.StatusBadRequest)
}
