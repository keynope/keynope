package main

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"
)

// Cross the external-input ownership boundary before acquiring the document
// lock. Batch updates already own their replacements in prepareEditorElementBatch;
// clipboard bundles have their separate off-lock preparation pipeline.
func ownEditorObjectInput(action nativeEditorAction) nativeEditorAction {
	if action.objectInputOwned {
		return action
	}
	action.PageSize = action.PageSize.Clone()
	if action.Action == "set-deck-theme" || action.Action == "detach-deck-theme" {
		action.ThemeData = action.ThemeData.Clone()
		if action.ThemeDraft != nil {
			draft := *action.ThemeDraft
			draft.Base = draft.Base.Clone()
			action.ThemeDraft = &draft
		}
	}
	switch action.Action {
	case "add-element", "update-element":
		if action.ElementData != nil {
			element := cloneElement(*action.ElementData)
			action.ElementData = &element
		}
	case "paste-elements":
		action.ElementsData = cloneSlide(Slide{Elements: action.ElementsData}).Elements
	}
	if action.AssetData != nil {
		asset := cloneDeckAsset(*action.AssetData)
		action.AssetData = &asset
	}
	action.objectInputOwned = true
	return action
}

func duplicateEditorElement(source Element) Element {
	copy := cloneElement(source)
	copy.Query = removeImageQueryKeys(copy.Query, "group")
	elements := []Element{copy}
	// The parser-only allowance for an existing dangling line must follow its
	// new identity; JSON cannot manufacture this internal compatibility marker.
	freshPastedConnectorIDs(elements)
	return elements[0]
}

// Own the existing history snapshot and the small session state for one command.
// The caller holds mu; failures restore state before releasing it, so no reader
// can observe a half-applied document. View-only commands do not clone the deck.
type editorCommandTransaction struct {
	structuredBudget *structuredBudgetIndex
	session          *nativeEditorSession
	locked           bool
	restoreDeck      bool
	geometryNeutral  bool // Private note edits still have document history, but cannot invalidate canvas geometry.
	beforeDeck       Deck
	current          int
	currentMaster    int
	selected         int
	selection        map[int]bool
	editingGroup     string
	masterMode       bool
	version          int64
	undo, redo       []Deck
	timerDeadline    time.Time
	sceneResponse    *sceneResponseCache
}

func beginEditorCommand(s *nativeEditorSession, snapshotDeck bool) *editorCommandTransaction {
	tx := &editorCommandTransaction{
		structuredBudget: s.structuredBudget,
		session:          s, locked: true, restoreDeck: snapshotDeck,
		current: s.current, currentMaster: s.currentMaster, selected: s.selected,
		editingGroup: s.editingGroup, masterMode: s.masterMode, version: s.version,
		undo: s.undo, redo: s.redo, timerDeadline: s.timerDeadline, sceneResponse: s.sceneResponse,
	}
	if snapshotDeck {
		tx.beforeDeck = cloneEditorHistoryDeck(s.deck, s.undo)
	}
	if s.selection != nil {
		tx.selection = make(map[int]bool, len(s.selection))
		for index, selected := range s.selection {
			tx.selection[index] = selected
		}
	}
	return tx
}

func (tx *editorCommandTransaction) finish(result *error) {
	if !tx.locked {
		return
	}
	if *result != nil {
		s := tx.session
		if tx.restoreDeck {
			s.deck = restoreEditorHistoryDeck(tx.beforeDeck)
		}
		s.current, s.currentMaster, s.selected = tx.current, tx.currentMaster, tx.selected
		s.selection, s.editingGroup, s.masterMode = tx.selection, tx.editingGroup, tx.masterMode
		s.version, s.undo, s.redo = tx.version, tx.undo, tx.redo
		s.structuredBudget = tx.structuredBudget
		s.timerDeadline, s.sceneResponse = tx.timerDeadline, tx.sceneResponse
	}
	tx.release()
}

// Release successful commands before calling the presenter or starting work
// that may itself read the editor. Error exits instead use deferred finish.
func (tx *editorCommandTransaction) release() {
	if tx.locked {
		if tx.restoreDeck {
			if tx.session.version != tx.version && !tx.geometryNeutral {
				tx.session.mutationRevision++
			}
			tx.session.retainSceneForDeckChange(tx.beforeDeck, tx.version)
			tx.session.invalidateMasterPreviewsLocked(tx.beforeDeck, tx.version)
		}
		tx.locked = false
		tx.session.mu.Unlock()
	}
}

// The runtime document identity is shared with notes/draft transport. Keep the
// legacy notes command's own conflict response; all other stamped requests must
// reject a replacement document even when IDs and revision numbers coincide.
// SceneRevision protects dispatch concurrency. This optional token additionally
// binds a long gesture to the authored state where it started, even if the
// caller has since observed newer selection-only command revisions.
func (s *nativeEditorSession) checkCommandOwnerLocked(action nativeEditorAction) error {
	if action.Action != "update-slide-notes" && action.NotesSessionID != "" && action.NotesSessionID != s.notesSessionID {
		return editorFailure("stale-target", "", action.ObjectID, fmt.Errorf("the document changed; retry the action in the current document"))
	}
	return nil
}

func (s *nativeEditorSession) checkMutationRevisionLocked(action nativeEditorAction) error {
	if err := s.checkCommandOwnerLocked(action); err != nil {
		return err
	}
	if action.MutationRevision != nil && *action.MutationRevision != s.mutationRevision {
		return editorFailure("stale-target", "", action.ObjectID, fmt.Errorf("the document changed during the gesture; retry the edit"))
	}
	return nil
}

// Prepare all replacements in owned storage before touching the authored slide.
// Protected master content may be restyled/positioned but not renamed/retyped.
func prepareEditorElementBatch(target Slide, action nativeEditorAction, protected func(int) bool) ([]Element, bool, error) {
	if err := resolveEditorBatch(target.Elements, &action); err != nil {
		return nil, false, err
	}
	elements := cloneSlide(target).Elements
	for n, index := range action.ElementIndices {
		original, updated := target.Elements[index], action.ElementsData[n]
		updated.Kind = strings.TrimSpace(updated.Kind)
		if updated.ID == "" {
			updated.ID = original.ID
		}
		if protected != nil && protected(index) {
			updated.Kind, updated.Level, updated.Text = original.Kind, original.Level, original.Text
			updated.ID, updated.SlotID, updated.PlaceholderRole = original.ID, original.SlotID, original.PlaceholderRole
		}
		if updated.Kind == "" {
			return nil, false, errInvalidEditorAction
		}
		if _, err := url.ParseQuery(updated.Query); err != nil {
			return nil, false, fmt.Errorf("object %s has invalid style parameters: %w", updated.ID, err)
		}
		if err := validateShapeAdjustments(updated); err != nil {
			return nil, false, err
		}
		if err := validateObjectNameChange(original, updated); err != nil {
			return nil, false, err
		}
		updated.Extra = cloneJSONExtensions(updated.Extra)
		elements[index] = preserveRunStyles(original, updated)
	}
	return elements, !reflect.DeepEqual(target.Elements, elements), nil
}
