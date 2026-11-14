//go:build !js && !keynope_sitegen

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// An MCP cursor is independent of the human's current page/selection. Resolve
// it by durable identities each time, including after human reorders/deletes.
type mcpEditorView struct {
	SlideID, MasterID      string
	Current, CurrentMaster int
	Master                 bool
	Selected               []string
	Group                  string
}

func (s *nativeEditorSession) captureMCPViewLocked() mcpEditorView {
	v := mcpEditorView{Current: s.current, CurrentMaster: s.currentMaster, Master: s.masterMode, Group: s.editingGroup}
	if s.current >= 0 && s.current < len(s.deck.Slides) {
		v.SlideID = collaborationSlideID(s.deck.Slides[s.current])
	}
	v.MasterID = "base"
	if s.currentMaster > 0 && s.currentMaster <= len(s.deck.Masters.Layouts) {
		v.MasterID = s.deck.Masters.Layouts[s.currentMaster-1].ID
	}
	var slide *Slide
	if s.masterMode {
		slide = masterSlideAt(&s.deck, s.currentMaster)
	} else if s.current >= 0 && s.current < len(s.deck.Slides) {
		slide = &s.deck.Slides[s.current]
	}
	if slide != nil {
		v.Selected = selectionTargetIDs(*slide, s.selection, s.selected, s.editingGroup)
	}
	return v
}
func (s *nativeEditorSession) restoreMCPViewLocked(v mcpEditorView, exclude map[string]bool) {
	s.current = min(max(v.Current, 0), max(0, len(s.deck.Slides)-1))
	for i, slide := range s.deck.Slides {
		if collaborationSlideID(slide) == v.SlideID {
			s.current = i
			break
		}
	}
	s.currentMaster = 0
	for i, layout := range s.deck.Masters.Layouts {
		if layout.ID == v.MasterID {
			s.currentMaster = i + 1
			break
		}
	}
	s.masterMode = v.Master
	s.editingGroup = v.Group
	s.selected = -1
	s.selection = map[int]bool{}
	var slide *Slide
	if s.masterMode {
		slide = masterSlideAt(&s.deck, s.currentMaster)
	} else if s.current < len(s.deck.Slides) {
		slide = &s.deck.Slides[s.current]
	}
	if slide != nil {
		for i, e := range slide.Elements {
			if slices.Contains(v.Selected, e.ID) && !exclude[e.ID] {
				s.selection[i] = true
				if s.selected < 0 {
					s.selected = i
				}
			}
		}
	}
	if s.selected < 0 {
		s.editingGroup = ""
	}
}
func (a *deckAutomation) applyLiveCommand(action nativeEditorAction, raw json.RawMessage, version int64) error {
	s := a.editor
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	s.mu.Lock()
	base := s.collaborationRevisionLocked(version)
	if base == nil {
		s.mu.Unlock()
		return errors.New("revision is no longer available; call deck_inspect before editing")
	}
	before := s.collaborationSnapshotLocked()
	human := s.captureMCPViewLocked()
	s.restoreMCPViewLocked(*a.liveView, nil)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if _, explicit := fields["slide"]; !explicit && action.Action != "toggle-master-mode" {
		targetID, ids := a.liveView.SlideID, before.Slides
		if a.liveView.Master {
			targetID, ids = a.liveView.MasterID, before.Masters
		}
		if targetID != "" && !slices.Contains(ids, targetID) {
			s.restoreMCPViewLocked(human, nil)
			s.mu.Unlock()
			return errors.New("the MCP target slide was deleted; select another slide before editing")
		}
	}
	master := s.masterMode
	list := base.Slides
	if master {
		list = base.Masters
	}
	target := s.current
	if master {
		target = s.currentMaster
	}
	if _, explicit := fields["slide"]; explicit {
		if action.Slide < 0 || action.Slide >= len(list) {
			s.restoreMCPViewLocked(human, nil)
			s.mu.Unlock()
			return errors.New("slide index not present in expected revision")
		}
		id := list[action.Slide]
		current := before.Slides
		if master {
			current = before.Masters
		}
		target = slices.Index(current, id)
		if target < 0 {
			s.restoreMCPViewLocked(human, nil)
			s.mu.Unlock()
			return errors.New("target slide was deleted")
		}
		if master {
			s.currentMaster = target
		} else {
			s.current = target
		}
	}
	action.Slide = target
	var slide *Slide
	if master {
		slide = masterSlideAt(&s.deck, target)
	} else if target >= 0 && target < len(s.deck.Slides) {
		slide = &s.deck.Slides[target]
	}
	if slide == nil {
		s.restoreMCPViewLocked(human, nil)
		s.mu.Unlock()
		return errors.New("target slide no longer exists")
	}
	id := collaborationSlideID(*slide)
	if master {
		id = "base"
		if target > 0 {
			id = s.deck.Masters.Layouts[target-1].ID
		}
	}
	oldIDs := base.Elements[collaborationKey(master, id, "")]
	resolve := func(index int, objectID string) (int, error) {
		if objectID == "" {
			if index < 0 || index >= len(oldIDs) {
				return -1, errors.New("element index not present in expected revision")
			}
			objectID = oldIDs[index]
		}
		for i, e := range slide.Elements {
			if e.ID == objectID {
				return i, nil
			}
		}
		return -1, fmt.Errorf("object %q was deleted", objectID)
	}
	var resolveErr error
	switch action.Action {
	case "select-element", "update-element", "delete-element", "duplicate-element", "move-element", "convert-text-kind":
		objectID := action.ObjectID
		if action.ElementData != nil && action.ElementData.ID != "" {
			objectID = action.ElementData.ID
		}
		if action.Action == "select-element" && action.Element < 0 {
			break
		}
		_, explicit := fields["element"]
		if !explicit && objectID == "" && s.selected >= 0 {
			action.Element = s.selected
			objectID = slide.Elements[s.selected].ID
		}
		action.Element, resolveErr = resolve(action.Element, objectID)
	}
	if action.Action == "select-elements" && len(action.ObjectIDs) > 0 {
		action.ElementIndices = nil
		action.ElementsData = nil
		for _, objectID := range action.ObjectIDs {
			var index int
			index, resolveErr = resolve(0, objectID)
			if resolveErr != nil {
				break
			}
			action.ElementIndices = append(action.ElementIndices, index)
			action.ElementsData = append(action.ElementsData, Element{ID: objectID})
		}
	} else if len(action.ElementIndices) > 0 {
		for n, index := range action.ElementIndices {
			objectID := ""
			if n < len(action.ElementsData) {
				objectID = action.ElementsData[n].ID
			}
			action.ElementIndices[n], resolveErr = resolve(index, objectID)
			if resolveErr != nil {
				break
			}
		}
	}
	if action.Action == "reorder-slide" {
		if action.Value < 0 || action.Value >= len(base.Slides) {
			resolveErr = errors.New("destination not in expected revision")
		} else {
			action.Value = slices.Index(before.Slides, base.Slides[action.Value])
			if action.Value < 0 {
				resolveErr = errors.New("destination slide was deleted")
			}
		}
	}
	if resolveErr != nil {
		s.restoreMCPViewLocked(human, nil)
		s.mu.Unlock()
		return resolveErr
	}
	revision := s.version
	action.SceneRevision = &revision
	action.automationRevision = &revision
	action.NotesSessionID = s.notesSessionID
	action.SceneMaster = master
	action.commandSerialized = true
	// Only the final human view is published to the presenter. Intermediate MCP
	// selections never trigger navigation or audience build changes.
	companion := s.companion
	page := 0
	if companion != nil {
		_, page = companion.Position()
	}
	s.companion = nil
	s.mu.Unlock()
	err := s.applyCommand(action)
	s.mu.Lock()
	*a.liveView = s.captureMCPViewLocked()
	after := s.collaborationSnapshotLocked()
	touched := map[string]bool{}
	if err == nil {
		for key, obj := range before.Objects {
			now, ok := after.Objects[key]
			if !ok || now.Hash != obj.Hash || before.Global != after.Global {
				touched[obj.ID] = true
			}
		}
	}
	s.restoreMCPViewLocked(human, touched)
	s.companion = companion
	s.recordCollaborationLocked(before, "mcp")
	current, path, revision := s.current, s.deckPath, s.version
	var deck Deck
	if companion != nil {
		deck = cloneDeckForScene(s.deck)
	}
	s.mu.Unlock()
	if companion != nil && err == nil {
		companion.RefreshEditorDeckAsync(s, path, deck, revision, authoredTerminalWidth, authoredTerminalHeight)
		companion.Update(current, page, companion.presentationEnabled(), nil)
	}
	return err
}

func (a *deckAutomation) changesSince(version int64) map[string]any {
	s := a.editor
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	s.mu.RLock()
	defer s.mu.RUnlock()
	changes := []editorChange{}
	reset := s.collaborationRevisionLocked(version) == nil
	if !reset {
		for _, change := range s.collaboration.Changes {
			if change.Version > version {
				changes = append(changes, change)
			}
		}
	}
	wanted := map[string]bool{}
	slideChanges := map[string]bool{}
	globalChanged := false
	for _, change := range changes {
		globalChanged = globalChanged || change.Global
		for _, id := range change.Slides {
			slideChanges[id] = true
		}
		for _, obj := range change.Objects {
			wanted[collaborationKey(obj.Master, obj.SlideID, obj.ID)] = true
		}
	}
	objects := []map[string]any{}
	appendObjects := func(slide Slide, index int, master bool, id string) {
		for _, e := range slide.Elements {
			if wanted[collaborationKey(master, id, e.ID)] {
				entry := map[string]any{"slide": index, "slide_id": id, "master": master, "element": redactAutomationData(e)}
				if s.deck.Objects != nil {
					if e.Kind == "table" {
						entry["table"] = redactAutomationData(s.deck.Objects.Tables[e.ObjectID])
					}
					if e.Kind == "chart" {
						entry["chart"] = redactAutomationData(s.deck.Objects.Charts[e.ObjectID])
					}
				}
				objects = append(objects, entry)
			}
		}
	}
	for i, slide := range s.deck.Slides {
		appendObjects(slide, i, false, collaborationSlideID(slide))
	}
	appendObjects(s.deck.Masters.Base.Slide, 0, true, "base")
	for i, layout := range s.deck.Masters.Layouts {
		appendObjects(layout.Slide, i+1, true, layout.ID)
	}
	slides := []map[string]any{}
	order := make([]string, 0, len(s.deck.Slides))
	for i, slide := range s.deck.Slides {
		id := collaborationSlideID(slide)
		order = append(order, id)
		if slideChanges[id] || slideChanges[collaborationKey(false, id, "")] {
			slides = append(slides, map[string]any{"index": i, "id": id, "slide": redactAutomationData(slide)})
		}
	}
	return map[string]any{"from_version": version, "version": s.version, "document_id": s.notesSessionID, "reset_required": reset, "events": changes, "objects": objects, "slides": slides, "slide_order": order, "settings_changed": globalChanged}
}
func (a *deckAutomation) requestLiveSave(ctx context.Context) (map[string]any, []byte, error) {
	s := a.editor
	s.mu.Lock()
	if s.collaboration.Save != nil {
		s.mu.Unlock()
		return nil, nil, errors.New("a save is already in progress")
	}
	request := &editorSaveRequest{ID: newStableID("mcp-save"), Owner: s.notesSessionID, done: make(chan error, 1)}
	s.collaboration.Save = request
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.collaboration.Save == request {
			s.collaboration.Save = nil
		}
		s.mu.Unlock()
	}()
	select {
	case err := <-request.done:
		if err != nil {
			return nil, nil, err
		}
		return a.mutationResult(automationArguments{}), nil, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (a *deckAutomation) applyImportedAction(ctx context.Context, version int64, action nativeEditorAction) error {
	if !a.attached {
		return a.editor.apply(action)
	}
	// The private cursor supplies scope; imported payloads need no JSON round-trip.
	return a.applyLiveCommand(action, json.RawMessage(`{}`), version)
}
