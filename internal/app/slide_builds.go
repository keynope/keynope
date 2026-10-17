package app

import (
	"fmt"
	"net/url"
	"reflect"
)

// Only this authored timeline is persisted. Runtime/preview cursors are owned
// by presentation surfaces and never enter the document or its undo history.
func (s *nativeEditorSession) setSlideBuildLocked(action nativeEditorAction, target *Slide) (bool, error) {
	index := s.current
	if s.masterMode {
		index = s.currentMaster
	}
	if target == nil || action.BuildData == nil || action.SceneRevision == nil || *action.SceneRevision != s.version || action.Slide != index || action.SceneMaster != s.masterMode {
		return false, editorFailure("stale-target", "", "", fmt.Errorf("the slide changed; reopen Builds before applying"))
	}
	if err := action.BuildData.Validate(); err != nil {
		return false, err
	}
	next := cloneBuild(*action.BuildData)
	visible := target.Elements
	if !s.masterMode {
		visible = s.deck.ResolveSlide(index, false).Elements
	} else if index > 0 {
		visible = append(append([]Element(nil), s.deck.Masters.Base.Slide.Elements...), target.Elements...)
	}
	byID, counts := referenceElementIndex(visible)
	already := map[string]bool{}
	for stepIndex := range next.Steps {
		step := &next.Steps[stepIndex]
		wanted := map[string]bool{}
		for _, id := range step.TargetIDs {
			if counts[id] != 1 {
				return false, fmt.Errorf("build target %q is missing or ambiguous", id)
			}
			wanted[id] = true
			q, _ := url.ParseQuery(byID[id].Query)
			if group := q.Get("group"); group != "" {
				for _, e := range visible {
					v, _ := url.ParseQuery(e.Query)
					if v.Get("group") == group {
						wanted[e.ID] = true
					}
				}
			}
		}
		step.TargetIDs = nil
		// Expand groups in paint order. Never mutate or reorder element layers.
		for _, e := range visible {
			if !wanted[e.ID] {
				continue
			}
			q, _ := url.ParseQuery(e.Query)
			if !validObjectID(e.ID) || counts[e.ID] != 1 || objectLocked(e) || protectedActivityElement(e) || q.Get("object-hidden") == "1" {
				return false, fmt.Errorf("unlock/show the object and avoid protected activity artwork before adding a build")
			}
			if already[e.ID] {
				return false, fmt.Errorf("object %q already appears in an earlier build", e.ID)
			}
			already[e.ID] = true
			step.TargetIDs = append(step.TargetIDs, e.ID)
		}
		if step.Advance == "" {
			step.Advance = "click"
		}
	}
	if err := next.Validate(); err != nil {
		return false, err
	}
	var previous SlideBuild
	exists := false
	if s.deck.Objects != nil {
		previous, exists = s.deck.Objects.Builds[target.ID]
	}
	if exists && next.Extra == nil {
		next.Extra = cloneJSONExtensions(previous.Extra)
	}
	// An empty draft removes the timeline, not unrelated structured payloads.
	empty := len(next.Steps) == 0 && (next.Transition.Effect == "" || next.Transition.Effect == "cut") && next.Transition.Extra == nil && next.Extra == nil
	if empty && !exists {
		return false, nil
	}
	if !empty && exists && reflect.DeepEqual(previous, next) {
		return false, nil
	}
	objects := s.deck.Objects.Clone()
	if objects == nil {
		objects = &DeckObjects{Version: 1}
	}
	if objects.Builds == nil {
		objects.Builds = map[string]SlideBuild{}
	}
	if empty {
		delete(objects.Builds, target.ID)
	} else {
		if target.ID == "" {
			target.ID = newStableID("slide")
		}
		objects.Builds[target.ID] = next
	}
	s.deck.Objects = objects
	if _, err := encodeDeckObjects(objects); err != nil {
		return false, err
	}
	if err := validateDeckObjectReferences(s.deck); err != nil {
		return false, err
	}
	return true, nil
}

func selectedSlideBuild(deck Deck, slide Slide) *SlideBuild {
	if deck.Objects != nil {
		if build, ok := deck.Objects.Builds[slide.ID]; ok {
			copy := cloneBuild(build)
			return &copy
		}
	}
	return &SlideBuild{Version: 1, Transition: SlideTransition{Effect: "cut"}, Steps: []BuildStep{}}
}

func slidesForBuildState(s *nativeEditorSession, index int) Slide {
	if s.masterMode {
		if target := masterSlideAt(&s.deck, index); target != nil {
			return *target
		}
	} else if index >= 0 && index < len(s.deck.Slides) {
		return s.deck.Slides[index]
	}
	return Slide{}
}

// Audience projection carries only executable settings and visible target IDs;
// opaque authoring extensions are never published as animation data.
func sceneSlideBuild(deck Deck, slideID string, objects []sceneObject) *SlideBuild {
	if deck.Objects == nil {
		return nil
	}
	build, ok := deck.Objects.Builds[slideID]
	if !ok {
		return nil
	}
	return filterSceneBuild(build, objects)
}

func filterSceneBuild(build SlideBuild, objects []sceneObject) *SlideBuild {
	visible := map[string]bool{}
	for _, o := range objects {
		visible[o.ID] = true
	}
	out := SlideBuild{Version: 1, Transition: SlideTransition{Effect: build.Transition.Effect, Direction: build.Transition.Direction, DurationMS: build.Transition.DurationMS}}
	for _, step := range build.Steps {
		next := BuildStep{Effect: step.Effect, Direction: step.Direction, DurationMS: step.DurationMS, Advance: step.Advance, DelayMS: step.DelayMS}
		for _, id := range step.TargetIDs {
			if visible[id] {
				next.TargetIDs = append(next.TargetIDs, id)
			}
		}
		if len(next.TargetIDs) > 0 {
			out.Steps = append(out.Steps, next)
		}
	}
	return &out
}
