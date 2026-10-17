package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"unicode/utf8"
)

const maxSpeakerNotesBytes = 1 << 20

var speakerNotesPrefixRE = regexp.MustCompile(`^<!--\s*notes(?:=|\s|-->|$)`)

func validateSpeakerNotes(notes string) error {
	if len(notes) > maxSpeakerNotesBytes {
		return fmt.Errorf("speaker notes exceed %d bytes per slide", maxSpeakerNotesBytes)
	}
	if !utf8.ValidString(notes) {
		return fmt.Errorf("speaker notes must contain valid UTF-8 text")
	}
	return nil
}

func decodeSpeakerNotesComment(line string) (string, error) {
	if len(line) > base64.StdEncoding.EncodedLen(maxSpeakerNotesBytes)+256 {
		return "", fmt.Errorf("speaker notes metadata is too large")
	}
	match := notesRE.FindStringSubmatch(line)
	if match == nil || match[0] != line || len(match[1]) > base64.StdEncoding.EncodedLen(maxSpeakerNotesBytes) {
		return "", fmt.Errorf("malformed speaker notes metadata")
	}
	data, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return "", fmt.Errorf("invalid speaker notes encoding: %w", err)
	}
	notes := string(data)
	if err := validateSpeakerNotes(notes); err != nil {
		return "", err
	}
	return notes, nil
}

func validateDeckSpeakerNotes(deck Deck) error {
	for _, slide := range deck.Slides {
		if err := validateSpeakerNotes(slide.Notes); err != nil {
			return err
		}
	}
	if err := validateSpeakerNotes(deck.Masters.Base.Slide.Notes); err != nil {
		return err
	}
	for _, layout := range deck.Masters.Layouts {
		if err := validateSpeakerNotes(layout.Slide.Notes); err != nil {
			return err
		}
	}
	return nil
}

func validateSpeakerNotesAction(action nativeEditorAction) error {
	if action.Action == "update-slide-notes" {
		if err := validateSpeakerNotes(action.Notes); err != nil {
			return editorFailure("invalid-value", "notes", "", err)
		}
	}
	return nil
}

func replaceSpeakerNotes(slide *Slide, notes string, master bool) bool {
	if slide.Notes == notes {
		return false
	}
	slide.Notes = notes
	// A legacy notes-only page must not vanish when its last authored value is
	// cleared. Assign an authored identity only for this actual edit, not open.
	if !master && notes == "" && len(slide.Elements) == 0 && slide.ID == "" {
		slide.ID = newStableID("slide")
	}
	return true
}

func speakerNotesOwnerID(slide Slide) string {
	if slide.notesOwnerID != "" {
		return slide.notesOwnerID
	}
	return slide.ID
}

func speakerNotesOwnerAt(deck Deck, master bool, index int) string {
	if master {
		if index == 0 {
			return deck.Masters.Base.ID
		}
		if index > 0 && index <= len(deck.Masters.Layouts) {
			return deck.Masters.Layouts[index-1].ID
		}
		return ""
	}
	if index >= 0 && index < len(deck.Slides) {
		return speakerNotesOwnerID(deck.Slides[index])
	}
	return ""
}

// Notes may finish saving after navigation or a master-mode switch. Resolve
// their authored owner independently of the current viewport, never by a stale
// array index. Legacy callers without an owner keep the old command contract.
func (s *nativeEditorSession) applySpeakerNotes(action nativeEditorAction) (resultErr error) {
	if err := validateSpeakerNotesAction(action); err != nil {
		return err
	}
	s.mu.Lock()
	preflight := true
	defer func() {
		if preflight {
			s.mu.Unlock()
		}
	}()
	scoped := action.NotesOwnerID != ""
	if scoped && (action.NotesSessionID == "" || action.NotesSessionID != s.notesSessionID || action.SceneRevision == nil || *action.SceneRevision != s.version) {
		return editorFailure("stale-target", "notes", action.NotesOwnerID, fmt.Errorf("the document changed; retry saving the speaker notes"))
	}
	var target *Slide
	found := 0
	master := action.SceneMaster
	if !scoped {
		master = s.masterMode || action.SceneMaster
		if master && action.Slide >= 0 && action.Slide <= len(s.deck.Masters.Layouts) {
			target, found = masterSlideAt(&s.deck, action.Slide), 1
		} else if !master && action.Slide >= 0 && action.Slide < len(s.deck.Slides) {
			target, found = &s.deck.Slides[action.Slide], 1
		}
	} else if master {
		if s.deck.Masters.Base.ID == action.NotesOwnerID {
			target, found = &s.deck.Masters.Base.Slide, found+1
		}
		for i := range s.deck.Masters.Layouts {
			if s.deck.Masters.Layouts[i].ID == action.NotesOwnerID {
				target, found = &s.deck.Masters.Layouts[i].Slide, found+1
			}
		}
	} else {
		for i := range s.deck.Slides {
			if speakerNotesOwnerID(s.deck.Slides[i]) == action.NotesOwnerID {
				target, found = &s.deck.Slides[i], found+1
			}
		}
	}
	if found == 0 {
		return editorFailure("missing-target", "notes", action.NotesOwnerID, fmt.Errorf("the slide for these speaker notes was removed"))
	}
	if found != 1 {
		return editorFailure("stale-target", "notes", action.NotesOwnerID, fmt.Errorf("the speaker notes owner is ambiguous"))
	}
	sum := sha256.Sum256([]byte(target.Notes))
	if scoped && action.NotesBeforeHash != hex.EncodeToString(sum[:]) {
		return editorFailure("stale-target", "notes", action.NotesOwnerID, fmt.Errorf("the speaker notes changed; your draft has been kept"))
	}
	if target.Notes == action.Notes {
		return nil
	}
	tx := beginEditorCommand(s, true)
	tx.geometryNeutral = true
	preflight = false
	defer tx.finish(&resultErr)
	replaceSpeakerNotes(target, action.Notes, master)
	s.undo = append(s.undo, tx.beforeDeck)
	if len(s.undo) > 100 {
		s.undo = s.undo[len(s.undo)-100:]
	}
	s.redo = nil
	s.version++
	return nil
}
