package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
)

// The journal retains identities and hashes, not copies of decks or media.
// It is enabled only for a document exposed to live MCP.
const editorChangeHistoryLimit = 128

type editorObjectVersion struct {
	ID      string   `json:"id"`
	SlideID string   `json:"slide_id"`
	Slide   int      `json:"slide"`
	Master  bool     `json:"master,omitempty"`
	Hash    [32]byte `json:"-"`
}
type editorRevision struct {
	Version         int64
	Owner           string
	Slides, Masters []string
	Objects         map[string]editorObjectVersion
	Elements        map[string][]string
	Metadata        map[string][32]byte
	Global          [32]byte
}
type editorChange struct {
	From    int64                 `json:"from_version"`
	Version int64                 `json:"version"`
	Origin  string                `json:"origin"`
	Objects []editorObjectVersion `json:"objects"`
	Removed []editorObjectVersion `json:"removed"`
	Slides  []string              `json:"slides"`
	Global  bool                  `json:"global,omitempty"`
}
type editorCollaboration struct {
	Revisions []*editorRevision
	Changes   []editorChange
	Save      *editorSaveRequest
}
type editorSaveRequest struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	done  chan error
}
type editorCollaborationState struct {
	Changes []editorChange     `json:"changes"`
	Save    *editorSaveRequest `json:"save,omitempty"`
}

func collaborationSlideID(slide Slide) string {
	if slide.ID != "" {
		return slide.ID
	}
	return slide.notesOwnerID
}
func collaborationKey(master bool, slideID, objectID string) string {
	return fmt.Sprintf("%t/%s/%s", master, slideID, objectID)
}
func collaborationHash(value any) [32]byte { b, _ := json.Marshal(value); return sha256.Sum256(b) }
func (s *nativeEditorSession) collaborationSnapshotLocked() *editorRevision {
	if s.collaboration == nil {
		return nil
	}
	r := &editorRevision{Version: s.version, Owner: s.notesSessionID, Objects: map[string]editorObjectVersion{}, Elements: map[string][]string{}, Metadata: map[string][32]byte{}}
	visit := func(slide Slide, index int, master bool, id string) {
		key := collaborationKey(master, id, "")
		meta := slide
		meta.Elements = nil
		r.Metadata[key] = collaborationHash(meta)
		r.Elements[key] = []string{}
		for _, e := range slide.Elements {
			var data any
			if s.deck.Objects != nil {
				if e.Kind == "table" {
					data = s.deck.Objects.Tables[e.ObjectID]
				}
				if e.Kind == "chart" {
					data = s.deck.Objects.Charts[e.ObjectID]
				}
			}
			r.Objects[collaborationKey(master, id, e.ID)] = editorObjectVersion{e.ID, id, index, master, collaborationHash([]any{e, data})}
			r.Elements[key] = append(r.Elements[key], e.ID)
		}
	}
	for i, slide := range s.deck.Slides {
		id := collaborationSlideID(slide)
		r.Slides = append(r.Slides, id)
		visit(slide, i, false, id)
	}
	r.Masters = append(r.Masters, "base")
	visit(s.deck.Masters.Base.Slide, 0, true, "base")
	for i, layout := range s.deck.Masters.Layouts {
		r.Masters = append(r.Masters, layout.ID)
		visit(layout.Slide, i+1, true, layout.ID)
	}
	global := s.deck
	global.Slides = nil
	global.Masters = MasterDeck{}
	global.Objects = nil
	// Asset/font IDs are content-addressed and referenced by object hashes.
	// Do not encode media payloads on every selection or command, and adding an
	// unrelated resource must not invalidate a human's active text draft.
	global.Assets = nil
	global.Typefaces = nil
	global.Diagnostics = nil
	r.Global = collaborationHash(global)
	return r
}
func (s *nativeEditorSession) enableCollaboration() {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collaboration == nil {
		s.collaboration = &editorCollaboration{}
		s.collaboration.Revisions = append(s.collaboration.Revisions, s.collaborationSnapshotLocked())
	}
}
func (s *nativeEditorSession) collaborationRevisionLocked(version int64) *editorRevision {
	if s.collaboration != nil {
		for i := len(s.collaboration.Revisions) - 1; i >= 0; i-- {
			r := s.collaboration.Revisions[i]
			if r.Version == version && r.Owner == s.notesSessionID {
				return r
			}
		}
	}
	return nil
}
func (s *nativeEditorSession) recordCollaborationLocked(before *editorRevision, origin string) {
	if before == nil || s.collaboration == nil {
		return
	}
	after := s.collaborationSnapshotLocked()
	if before.Version == after.Version && before.Owner == after.Owner {
		return
	}
	if before.Owner != after.Owner {
		s.collaboration.Revisions = nil
		s.collaboration.Changes = nil
	}
	change := editorChange{From: before.Version, Version: after.Version, Origin: origin, Objects: []editorObjectVersion{}, Removed: []editorObjectVersion{}, Slides: []string{}, Global: before.Global != after.Global}
	slides := map[string]bool{}
	for key, obj := range after.Objects {
		old, ok := before.Objects[key]
		if !ok || old.Hash != obj.Hash || old.Slide != obj.Slide {
			change.Objects = append(change.Objects, obj)
			slides[obj.SlideID] = true
		}
	}
	for key, obj := range before.Objects {
		if _, ok := after.Objects[key]; !ok {
			change.Removed = append(change.Removed, obj)
			slides[obj.SlideID] = true
		}
	}
	for key, hash := range after.Metadata {
		if before.Metadata[key] != hash {
			if len(key) > 4 && key[:4] == "true" {
				change.Global = true
			}
			slides[key] = true
		}
	}
	for key := range before.Metadata {
		if _, ok := after.Metadata[key]; !ok {
			slides[key] = true
		}
	}
	if !reflect.DeepEqual(before.Slides, after.Slides) {
		change.Slides = append(change.Slides, after.Slides...)
	}
	if !reflect.DeepEqual(before.Masters, after.Masters) {
		change.Global = true
	}
	for id := range slides {
		change.Slides = append(change.Slides, id)
	}
	sort.Strings(change.Slides)
	sort.Slice(change.Objects, func(i, j int) bool { return change.Objects[i].ID < change.Objects[j].ID })
	sort.Slice(change.Removed, func(i, j int) bool { return change.Removed[i].ID < change.Removed[j].ID })
	s.collaboration.Revisions = append(s.collaboration.Revisions, after)
	s.collaboration.Changes = append(s.collaboration.Changes, change)
	if len(s.collaboration.Revisions) > editorChangeHistoryLimit {
		s.collaboration.Revisions = s.collaboration.Revisions[len(s.collaboration.Revisions)-editorChangeHistoryLimit:]
	}
	if len(s.collaboration.Changes) > editorChangeHistoryLimit {
		s.collaboration.Changes = s.collaboration.Changes[len(s.collaboration.Changes)-editorChangeHistoryLimit:]
	}
}
func (s *nativeEditorSession) collaborationStateLocked() *editorCollaborationState {
	if s.collaboration == nil {
		return nil
	}
	return &editorCollaborationState{Changes: append([]editorChange(nil), s.collaboration.Changes...), Save: s.collaboration.Save}
}

// A stale human draft can survive unrelated MCP edits, but never overwrite an
// object MCP has updated. Retained identity/hash checks also prevent index reuse.
func (s *nativeEditorSession) rebaseHumanActionLocked(action *nativeEditorAction) error {
	if s.collaboration == nil || action.automationRevision != nil || action.SceneRevision == nil || *action.SceneRevision == s.version {
		return nil
	}
	before := s.collaborationRevisionLocked(*action.SceneRevision)
	if before == nil {
		return errors.New("The draft revision expired; reopen the editor before applying.")
	}
	after := s.collaborationSnapshotLocked()
	if before.Global != after.Global || !reflect.DeepEqual(before.Masters, after.Masters) {
		return nil
	}
	ids := append([]string(nil), action.ObjectIDs...)
	if action.ObjectID != "" {
		ids = append(ids, action.ObjectID)
	}
	if action.ElementData != nil && action.ElementData.ID != "" {
		ids = append(ids, action.ElementData.ID)
	}
	for _, e := range action.ElementsData {
		if e.ID != "" {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	master := action.SceneMaster
	list := before.Slides
	if master {
		list = before.Masters
	}
	if action.Slide < 0 || action.Slide >= len(list) {
		return nil
	}
	id := list[action.Slide]
	currentList := after.Slides
	if master {
		currentList = after.Masters
	}
	currentIndex := slices.Index(currentList, id)
	if currentIndex < 0 {
		return errors.New("The edited slide was deleted.")
	}
	key := collaborationKey(master, id, "")
	if before.Metadata[key] != after.Metadata[key] {
		return nil
	}
	for _, objectID := range ids {
		key := collaborationKey(master, id, objectID)
		old, ok := before.Objects[key]
		now, exists := after.Objects[key]
		if !ok || !exists || old.Hash != now.Hash {
			return editorFailure("stale-target", "", objectID, errors.New("MCP or another editor changed this object; the pending edit was cancelled"))
		}
	}
	action.Slide = currentIndex
	currentIDs := after.Elements[key]
	if action.ElementData != nil && action.ElementData.ID != "" {
		action.Element = slices.Index(currentIDs, action.ElementData.ID)
	}
	for i, e := range action.ElementsData {
		if i < len(action.ElementIndices) && e.ID != "" {
			action.ElementIndices[i] = slices.Index(currentIDs, e.ID)
		}
	}
	revision, mutation := s.version, s.mutationRevision
	action.SceneRevision = &revision
	if action.MutationRevision != nil {
		action.MutationRevision = &mutation
	}
	return nil
}
