package app

import (
	"context"
	"fmt"
)

// Private prepared input: clients can request detachment, never supply a
// replacement document. Conversion and saveability checks run off the lock.
type themeDetachDraft struct {
	owner    *nativeEditorSession
	identity string
	revision int64
	ctx      context.Context
	deck     Deck
	budget   *structuredBudgetIndex
}

func (s *nativeEditorSession) prepareThemeDetach(ctx context.Context, action nativeEditorAction) (*themeDetachDraft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	if err := s.checkThemeDraftLocked(action); err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	if s.deck.Theme == nil {
		s.mu.RUnlock()
		return nil, nil
	}
	draft := &themeDetachDraft{owner: s, identity: s.notesSessionID, revision: s.version, ctx: ctx}
	source, path, limits := cloneDeck(s.deck), s.deckPath, s.structuredLimits
	s.mu.RUnlock()
	candidate, err := concreteThemeDeck(source)
	if err != nil {
		return nil, editorFailure("invalid-value", "theme", "", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	draft.budget = newStructuredBudgetIndex(candidate.Objects)
	if limits.Objects == 0 {
		limits = defaultStructuredBudgetLimits
	}
	if err := draft.budget.usage.validate(limits); err != nil {
		return nil, err
	}
	// Detachment must never make a previously saveable deck unsaveable. This
	// exercises metadata envelopes and combined comment limits without writing.
	if _, err := serializeDeck(path, candidate); err != nil {
		return nil, editorFailure("payload-limit", "theme", "", fmt.Errorf("could not detach theme without losing saveability: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	draft.deck = candidate
	return draft, nil
}

func (s *nativeEditorSession) prepareAndApplyThemeDetach(action nativeEditorAction) error {
	ctx := action.commandContext
	if ctx == nil {
		ctx = context.Background()
	}
	draft, err := s.prepareThemeDetach(ctx, action)
	if err != nil || draft == nil {
		return err
	}
	action.preparedThemeDetach = draft
	return s.apply(action)
}

func (s *nativeEditorSession) checkThemeDetachLocked(draft *themeDetachDraft) error {
	if draft == nil || draft.owner != s || draft.ctx == nil || draft.budget == nil {
		return errInvalidEditorAction
	}
	if err := draft.ctx.Err(); err != nil {
		return err
	}
	// Use the document revision, not the geometry mutation epoch: note-only
	// edits must also invalidate a whole-deck candidate or they would be lost.
	if draft.identity != s.notesSessionID || draft.revision != s.version {
		return editorFailure("stale-target", "theme", "", fmt.Errorf("the document changed while detaching; retry from the theme editor"))
	}
	limits := s.structuredLimits
	if limits.Objects == 0 {
		limits = defaultStructuredBudgetLimits
	}
	return draft.budget.usage.validate(limits)
}
