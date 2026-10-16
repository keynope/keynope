package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Shared word-formatting preparation. The text editor owns draft/history;
// only Copy touches the ephemeral session clipboard. Apply text commits once.
func (s *nativeEditorSession) handleTextStyleDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
	if decoder.Decode(&action) != nil || decoder.Decode(new(any)) != io.EOF || action.StyleContext != "text-range" {
		http.Error(w, "invalid word style draft", http.StatusBadRequest)
		return
	}
	if action.PropertyDelta != nil && (action.Action != "set-selection-property" || action.Property != "text.size" || len(action.PropertyValue) != 0) {
		http.Error(w, "invalid or ambiguous word increment", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current {
		s.mu.RUnlock()
		http.Error(w, "document changed; reopen the text editor", http.StatusConflict)
		return
	}
	scope, err := captureWordStyleScope(s.deck, action)
	group := s.editingGroup
	s.mu.RUnlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deck := scope.deck
	owner, err := scope.owner(action, group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, _, err = textRangeBytes(action.TextRuns, action.TextRange); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	capabilityAction := action
	capabilityAction.TextRuns = deck.wordFontSnapshotRuns(action.TextRuns)
	capabilities, clipboard, copyErr := textRangeStyleCapabilities(capabilityAction)
	clipboard = wordSnapshotInDeck(deck, clipboard)
	capabilities = wordCapabilitiesInDeck(deck, action, capabilities, clipboard)
	response := struct {
		ColorTokens  map[string]string         `json:"colorTokens,omitempty"`
		FontRoles    map[string]string         `json:"fontRoles,omitempty"`
		Runs         []sceneRun                `json:"textRuns,omitempty"`
		Clipboard    *ObjectStyleSnapshot      `json:"styleClipboard,omitempty"`
		Capabilities *selectionCapabilityState `json:"selectionCapabilities"`
	}{Capabilities: capabilities}
	switch action.Action {
	case "text-style-capabilities":
	case "copy-object-style":
		if copyErr != nil {
			http.Error(w, copyErr.Error(), http.StatusBadRequest)
			return
		}
		response.Clipboard = clipboard
	case "paste-object-style", "reset-object-style", "set-selection-property":
		snapshot := action.StyleData
		if action.Action == "paste-object-style" {
			if err := snapshot.Validate(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			snapshot = wordSnapshotForDestination(deck, snapshot)
		}
		if action.Action == "set-selection-property" && action.PropertyDelta == nil {
			// Explicit range properties use the same bounded snapshot validator.
			value := ""
			if json.Unmarshal(action.PropertyValue, &value) != nil {
				var number float64
				var flag bool
				if json.Unmarshal(action.PropertyValue, &number) == nil {
					value = strconv.FormatFloat(number, 'f', -1, 64)
				} else if json.Unmarshal(action.PropertyValue, &flag) == nil {
					value = strconv.FormatBool(flag)
				} else {
					http.Error(w, "invalid word property", http.StatusBadRequest)
					return
				}
			}
			snapshot = &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{action.Property: {Present: value != "", Value: value}}}
			if action.Property == "text.font" && wordFontRole(value) != "" {
				snapshot.Fields[action.Property] = ObjectStyleField{Present: true, Value: value, Fallback: deck.wordFontID(wordFontRole(value), "c64")}
			}
			if action.Property == "paint.colorToken" {
				if value != "" && !validThemeColorToken(value) {
					http.Error(w, "invalid word colour role", http.StatusBadRequest)
					return
				}
				field := ObjectStyleField{}
				if value != "" {
					field = ObjectStyleField{Present: true, Value: deck.themeTokenColor(value), Token: value}
				}
				snapshot.Fields = map[string]ObjectStyleField{"paint.color": field}
			}
		}
		if action.PropertyDelta != nil {
			var size float64
			size, err = wordIncrementBaseSize(deck, owner, action)
			if err == nil {
				response.Runs, _, err = applyTextRangeSizeDelta(action.TextRuns, action.TextRange, size, *action.PropertyDelta)
			}
		} else if action.Action == "set-selection-property" && action.Property == "text.font" && deck.Theme != nil && snapshot != nil && wordFontRole(snapshot.Fields["text.font"].Value) != "" {
			response.Runs, _, err = applyTextRangeFontRole(action.TextRuns, action.TextRange, snapshot)
		} else {
			response.Runs, _, err = applyTextRangeStyle(action.TextRuns, action.TextRange, snapshot, mustQuery(owner.Query).Get("ttf-weight") == "bold", action.Action == "reset-object-style")
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		accepted := action
		response.ColorTokens = deck.wordColorPalette(response.Runs)
		response.FontRoles = deck.wordFontPalette(response.Runs)
		accepted.TextRuns = response.Runs
		accepted.TextRuns = deck.wordFontSnapshotRuns(accepted.TextRuns)
		response.Capabilities, _, _ = textRangeStyleCapabilities(accepted)
		acceptedSnapshot, _ := captureTextRangeStyle(accepted.TextRuns, accepted.TextRange)
		response.Capabilities = wordCapabilitiesInDeck(deck, accepted, response.Capabilities, wordSnapshotInDeck(deck, acceptedSnapshot))
	default:
		http.Error(w, "unsupported word style operation", http.StatusBadRequest)
		return
	}
	// Rendering/format preparation is outside the lock. Reject navigation,
	// group changes or cancellation before publishing a prepared result.
	s.mu.Lock()
	current = s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if r.Context().Err() != nil {
		s.mu.Unlock()
		return
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode || action.Slide != current || group != s.editingGroup {
		s.mu.Unlock()
		http.Error(w, "document changed; reopen the text editor", http.StatusConflict)
		return
	}
	if response.Clipboard != nil {
		s.styleClipboard = response.Clipboard.Clone()
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}

func textRangeStyleCapabilities(action nativeEditorAction) (*selectionCapabilityState, *ObjectStyleSnapshot, error) {
	clipboard, copyErr := captureTextRangeStyle(action.TextRuns, action.TextRange)
	capabilities := &selectionCapabilityState{Version: 1, Revision: *action.SceneRevision, Context: "text-range", TargetIDs: []string{action.ObjectID}, TextRange: action.TextRange, Commands: map[string]selectionCommandState{}, Properties: map[string]selectionPropertyState{}}
	if action.TableWord != nil {
		copy := *action.TableWord
		if copy.BaseSize != nil {
			value := *copy.BaseSize
			copy.BaseSize = &value
		}
		capabilities.TableWord = &copy
	}
	for _, key := range []string{"style.copy", "style.paste", "style.reset"} {
		command := selectionCommandState{Enabled: true, EligibleIDs: []string{action.ObjectID}, TotalCount: 1}
		if key == "style.copy" && copyErr != nil {
			command.Enabled, command.Reason = false, copyErr.Error()
		}
		capabilities.Commands[key] = command
	}
	for _, key := range textRangeStyleFields {
		property := selectionPropertyState{State: "mixed", Provenance: "mixed", EligibleIDs: []string{action.ObjectID}, TotalCount: 1, Editable: true}
		if clipboard != nil {
			if field, ok := clipboard.Fields[key]; ok {
				property.State, property.Provenance = "uniform", "inherited"
				if field.Present {
					property.Value, property.Provenance = field.Value, "explicit"
					if key == "text.size" {
						property.Value, _ = strconv.ParseFloat(field.Value, 64)
					} else if key == "text.bold" || key == "text.italic" || key == "text.underline" {
						property.Value = field.Value == "true"
					}
				}
			}
		}
		capabilities.Properties[key] = property
	}
	return capabilities, clipboard, copyErr
}

func wordIncrementBaseSize(deck Deck, owner Element, action nativeEditorAction) (float64, error) {
	if action.TableWord != nil {
		if action.TableWord.BaseSize == nil {
			return 0, fmt.Errorf("word increments need the current inherited cell size")
		}
		return *action.TableWord.BaseSize, nil
	}
	if action.ModernSize != nil {
		if !validWordBaseSize(*action.ModernSize) {
			return 0, fmt.Errorf("invalid inherited word font size")
		}
		return *action.ModernSize, nil
	}
	// Resolve empty text only: typography, not emoji/font content preparation.
	owner.Text = ""
	text := deck.modernSceneTextFromValues(owner, mustQuery(owner.Query))
	if text == nil || !validWordBaseSize(text.Size) {
		return 0, fmt.Errorf("inherited word font size is unavailable")
	}
	return text.Size, nil
}
