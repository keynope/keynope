package app

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Immediate theme commands act on the current document. A modeless/delayed
// theme draft additionally carries the document and theme it was opened from.
// Bind to theme content rather than the whole editor revision: unrelated slide
// edits and navigation must not throw away a colour/typography draft.
type themeDraftContext struct {
	Owner string     `json:"owner"`
	Base  *DeckTheme `json:"base"`
}

func (s *nativeEditorSession) checkThemeDraftLocked(action nativeEditorAction) error {
	if action.Action != "set-deck-theme" && action.Action != "detach-deck-theme" {
		return nil
	}
	if action.ThemeDraft == nil {
		if action.Action == "detach-deck-theme" {
			return editorFailure("invalid-value", "theme", "", fmt.Errorf("missing theme draft context"))
		}
		return nil
	}
	draft := action.ThemeDraft
	if draft.Base == nil || draft.Owner == "" || draft.Base.Validate() != nil {
		return editorFailure("invalid-value", "theme", "", fmt.Errorf("invalid theme draft context"))
	}
	if draft.Owner != s.notesSessionID {
		return editorFailure("stale-target", "theme", "", fmt.Errorf("the document changed; reopen the theme editor before applying"))
	}
	base, err := json.Marshal(draft.Base)
	current, currentErr := json.Marshal(s.deck.resolvedDeckTheme())
	if err != nil || currentErr != nil || !bytes.Equal(base, current) {
		return editorFailure("stale-target", "theme", "", fmt.Errorf("the theme changed; reopen the theme editor before applying"))
	}
	return nil
}
