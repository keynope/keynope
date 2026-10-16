package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// The table editors own uncommitted drafts. This endpoint validates and styles
// those drafts with the same model operation used by committed commands; it
// never writes table data or history. Copy updates only the session clipboard.
func (s *nativeEditorSession) handleTableCellStyleDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStructuredObjectBytes+8192))
	if decoder.Decode(&action) != nil || action.TableData == nil || decoder.Decode(new(any)) != io.EOF || action.StyleContext != "table-cells" {
		http.Error(w, "invalid cell style draft", http.StatusBadRequest)
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
		http.Error(w, "document changed; reopen the table editor", http.StatusConflict)
		return
	}
	scope, owner, scopeErr := captureTableStyleScope(s.deck, action)
	group := s.editingGroup
	s.mu.RUnlock()
	if scopeErr != nil {
		http.Error(w, scopeErr.Error(), http.StatusBadRequest)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	deck := scope.deck
	color := deck.tableResolvedOwnerColor(scope.resolved(), owner)
	prepareCapabilities := func() (*tableCellStyleSelection, error) {
		return tableCellStyleCapabilitiesForTarget(deck, action, group, *action.SceneRevision, scope.source, owner, *action.TableData, color)
	}
	capabilities, err := prepareCapabilities()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var response interface{}
	command := map[string]string{"copy-object-style": "style.copy", "paste-object-style": "style.paste", "reset-object-style": "style.reset"}[action.Action]
	if command != "" && !capabilities.Commands[command].Enabled {
		http.Error(w, capabilities.Commands[command].Reason, http.StatusBadRequest)
		return
	}
	if action.Action == "set-selection-property" {
		property, ok := capabilities.Properties[action.Property]
		if !ok || !property.Editable {
			http.Error(w, "cell property is unavailable", http.StatusBadRequest)
			return
		}
	}
	if action.Action == "cell-style-capabilities" {
		response = struct {
			Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
		}{capabilities}
	} else if action.Action == "copy-object-style" {
		snapshot, err := captureTableCellStyle(deck, *action.TableData, action.CellRange)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		current = s.current
		if s.masterMode {
			current = s.currentMaster
		}
		if *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current || group != s.editingGroup || r.Context().Err() != nil {
			s.mu.Unlock()
			http.Error(w, "document changed; copy the cell style again", http.StatusConflict)
			return
		}
		s.styleClipboard = snapshot.Clone()
		s.mu.Unlock()
		response = struct {
			Clipboard    *ObjectStyleSnapshot     `json:"styleClipboard"`
			Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
		}{snapshot, capabilities}
	} else if action.Action == "paste-object-style" || action.Action == "reset-object-style" || action.Action == "set-selection-property" {
		var data TableData
		var err error
		if action.Action == "set-selection-property" {
			data, _, err = setTableCellPropertyPrepared(deck, *action.TableData, action, &color)
		} else {
			data, _, err = applyTableCellStyleData(deck, *action.TableData, action.CellRange, action.StyleData, action.Action == "reset-object-style")
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		action.TableData = &data
		capabilities, err = prepareCapabilities()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response = struct {
			Data         TableData                `json:"tableData"`
			Capabilities *tableCellStyleSelection `json:"selectionCapabilities"`
		}{data, capabilities}
	} else {
		http.Error(w, "unsupported cell style operation", http.StatusBadRequest)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	// Paste/reset and capability preparation happen outside the session lock.
	// Selection/group navigation may not advance the document revision: recheck
	// it as well, so no stale prepared result can enter a newer editor draft.
	s.mu.RLock()
	current = s.current
	if s.masterMode {
		current = s.currentMaster
	}
	valid := *action.SceneRevision == s.version && action.SceneMaster == s.masterMode && action.Slide == current && group == s.editingGroup
	s.mu.RUnlock()
	if !valid {
		http.Error(w, "document changed; reopen the table editor", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}
