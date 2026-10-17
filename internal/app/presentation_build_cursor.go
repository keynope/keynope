package app

import (
	"fmt"
	"reflect"
	"time"
)

type PresentationBuildCursor struct {
	Session     string `json:"session"`
	SlideID     string `json:"slideId"`
	Step        int    `json:"step"`
	Generation  int64  `json:"generation"`
	StartedAtMS int64  `json:"startedAtMs"`
	Motion      string `json:"motion"` // enter, advance, or instantaneous seek
	Paused      bool   `json:"paused"`
}

func (p *presenterCompanion) BuildSnapshot() *PresentationBuildCursor {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.BuildCursor == nil {
		return nil
	}
	value := *p.state.BuildCursor
	return &value
}
func (p *presenterCompanion) buildPageLocked() (string, SlideBuild) {
	id := fmt.Sprintf("view-slide-%d-%d", p.state.Slide, p.state.Page)
	build := SlideBuild{Version: 1, Transition: SlideTransition{Effect: "cut"}}
	for _, page := range p.pages[p.state.Slide] {
		if page.Page == p.state.Page && page.Scene != nil {
			if page.Scene.SlideID != "" {
				id = page.Scene.SlideID
			}
			if page.Scene.Build != nil && page.Scene.Build.Validate() == nil {
				build = cloneBuild(*page.Scene.Build)
			}
			break
		}
	}
	return id, build
}
func (p *presenterCompanion) cancelBuildTimerLocked() {
	if p.buildTimer != nil {
		p.buildTimer.Stop()
		p.buildTimer = nil
	}
	p.buildDeadline = time.Time{}
}
func (p *presenterCompanion) cursorLocked(step int, motion string, paused bool) {
	p.cancelBuildTimerLocked()
	p.buildHasRemaining = false
	p.buildGeneration++
	id, build := p.buildPageLocked()
	p.buildDefinition = &build
	step = max(0, min(step, len(build.Steps)))
	p.state.BuildCursor = &PresentationBuildCursor{Session: p.buildSession, SlideID: id, Step: step, Generation: p.buildGeneration, StartedAtMS: time.Now().UnixMilli(), Motion: motion, Paused: paused}
	p.syncPresentationOverlayLocked()
}
func (p *presenterCompanion) scheduleBuildLocked(wait time.Duration) {
	p.cancelBuildTimerLocked()
	cursor := p.state.BuildCursor
	if cursor == nil || cursor.Paused || p.buildDefinition == nil || cursor.Step >= len(p.buildDefinition.Steps) || p.buildDefinition.Steps[cursor.Step].Advance != "after" {
		return
	}
	if wait < 0 {
		wait = 0
	}
	expected := *cursor
	expectedSlide, expectedPage := p.state.Slide, p.state.Page
	p.buildDeadline = time.Now().Add(wait)
	p.buildTimer = time.AfterFunc(wait, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		current := p.state.BuildCursor
		if current == nil || current.Paused || p.fullPending || p.slidePending && p.pendingSlide == p.state.Slide || p.state.Slide != expectedSlide || p.state.Page != expectedPage || current.Session != expected.Session || current.Generation != expected.Generation || current.SlideID != expected.SlideID || current.Step != expected.Step {
			return
		}
		p.advanceCursorLocked()
		p.state.Version++
	})
}
func (p *presenterCompanion) scheduleNextBuildLocked(previousDuration int) {
	c := p.state.BuildCursor
	if c == nil || p.buildDefinition == nil || c.Step >= len(p.buildDefinition.Steps) {
		return
	}
	p.scheduleBuildLocked(time.Duration(previousDuration+p.buildDefinition.Steps[c.Step].DelayMS) * time.Millisecond)
}
func (p *presenterCompanion) advanceCursorLocked() {
	c := p.state.BuildCursor
	if c == nil || p.buildDefinition == nil || c.Step >= len(p.buildDefinition.Steps) {
		return
	}
	duration := p.buildDefinition.Steps[c.Step].DurationMS
	p.cursorLocked(c.Step+1, "advance", c.Paused)
	p.scheduleNextBuildLocked(duration)
}

// Call after changing playback status, pages or position while holding p.mu.
// Pausing retains the cursor and monotonic remaining delay; stopping clears the
// session. Source edits invalidate callbacks without replaying finished builds.
func (p *presenterCompanion) maintainBuildCursorLocked() bool {
	defer p.syncPresentationOverlayLocked()
	wanted := p.state.Presenting
	paused := false
	if p.helper {
		wanted = p.target != "none"
		paused = p.paused
	}
	if !wanted {
		had := p.state.BuildCursor != nil
		p.cancelBuildTimerLocked()
		p.buildSession = ""
		p.buildDefinition = nil
		p.state.BuildCursor = nil
		p.buildHasRemaining = false
		return had
	}
	if p.fullPending || p.slidePending && p.pendingSlide == p.state.Slide {
		return false
	}
	id, definition := p.buildPageLocked()
	c := p.state.BuildCursor
	if c == nil || c.SlideID != id {
		if p.buildSession == "" {
			p.buildSession = newStableID("presentation")
		}
		p.cursorLocked(0, "enter", paused)
		p.scheduleNextBuildLocked(definition.Transition.DurationMS)
		return true
	}
	if !reflect.DeepEqual(p.buildDefinition, &definition) {
		p.cursorLocked(c.Step, "seek", paused)
		return true
	}
	if c.Paused != paused {
		remaining, had := p.buildRemaining, p.buildHasRemaining
		if paused && p.buildTimer != nil {
			remaining = time.Until(p.buildDeadline)
			had = true
		}
		if !paused {
			remaining, had = p.buildRemaining, p.buildHasRemaining
		}
		p.cursorLocked(c.Step, "seek", paused)
		p.buildRemaining, p.buildHasRemaining = remaining, had
		if !paused && had {
			p.scheduleBuildLocked(remaining)
		}
		return true
	}
	if p.buildHasRemaining && !paused {
		remaining := p.buildRemaining
		p.buildHasRemaining = false
		p.scheduleBuildLocked(remaining)
	}
	return false
}

func (p *presenterCompanion) freezeBuildForRefreshLocked() {
	if c := p.state.BuildCursor; c != nil {
		remaining, had := p.buildRemaining, p.buildHasRemaining
		if p.buildTimer != nil {
			remaining = time.Until(p.buildDeadline)
			had = true
		}
		p.cursorLocked(c.Step, "seek", c.Paused)
		p.buildRemaining, p.buildHasRemaining = remaining, had
		p.state.Version++
	}
}

func (p *presenterCompanion) navigateBuildLocked(direction string, generation *int64) (bool, error) {
	if p.fullPending || p.slidePending && p.pendingSlide == p.state.Slide {
		return false, fmt.Errorf("presentation pages are being refreshed; retry navigation")
	}
	c := p.state.BuildCursor
	if c == nil || generation == nil || *generation != c.Generation {
		return false, fmt.Errorf("presentation changed; refresh the build cursor before navigating")
	}
	switch direction {
	case "next", "previous", "first", "last":
	default:
		return false, errInvalidEditorAction
	}
	if direction == "next" && p.buildDefinition != nil && c.Step < len(p.buildDefinition.Steps) {
		p.advanceCursorLocked()
		p.state.Version++
		return true, nil
	}
	if direction == "previous" && c.Step > 0 {
		p.cursorLocked(c.Step-1, "seek", c.Paused)
		p.state.Version++
		return true, nil
	}
	pages := p.slidePagesLocked()
	position := -1
	for i, page := range pages {
		if page.Slide == p.state.Slide && page.Page == p.state.Page {
			position = i
			break
		}
	}
	destination := position
	delta := 1
	if direction == "previous" {
		delta = -1
	}
	if direction == "first" {
		destination = -1
		delta = 1
	} else if direction == "last" {
		destination = len(pages)
		delta = -1
	}
	for i := destination + delta; i >= 0 && i < len(pages); i += delta {
		if !pages[i].TabOnly {
			destination = i
			break
		}
	}
	if destination < 0 || destination >= len(pages) {
		return false, nil
	}
	target := pages[destination]
	final := direction == "previous" || direction == "last"
	if target.Slide == p.state.Slide && target.Page == p.state.Page && direction != "first" && direction != "last" {
		return false, nil
	}
	p.state.Slide, p.state.Page = target.Slide, target.Page
	_, build := p.buildPageLocked()
	step := 0
	if final {
		step = len(build.Steps)
	}
	motion := "enter"
	if final {
		motion = "seek"
	}
	p.cursorLocked(step, motion, c.Paused)
	if !final {
		p.scheduleNextBuildLocked(build.Transition.DurationMS)
	}
	p.state.Version++
	return true, nil
}

func (s *nativeEditorSession) navigatePresentationBuild(action nativeEditorAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterMode || s.companion == nil {
		return errInvalidEditorAction
	}
	p := s.companion
	p.mu.Lock()
	defer p.mu.Unlock()
	changed, err := p.navigateBuildLocked(action.Name, action.BuildGeneration)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if s.current != p.state.Slide {
		s.current = p.state.Slide
		s.selected = -1
		s.selection = map[int]bool{}
		s.editingGroup = ""
		s.retainUnchangedSceneRevision()
		s.version++
	}
	return nil
}
