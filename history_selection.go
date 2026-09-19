package main

func (s *nativeEditorSession) historyElements() []Element {
	if s.masterMode {
		if s.currentMaster >= 0 && s.currentMaster <= len(s.deck.Masters.Layouts) {
			return masterSlideAt(&s.deck, s.currentMaster).Elements
		}
	} else if s.current >= 0 && s.current < len(s.deck.Slides) {
		return s.deck.Slides[s.current].Elements
	}
	return nil
}

// History restores document snapshots, not stale array positions. Keep only
// unambiguous authored identities still present on the restored current slide.
func (s *nativeEditorSession) retainHistorySelection(previous, restored []Element) {
	ids := map[string]bool{}
	primary := ""
	counts := map[string]int{}
	for _, e := range previous {
		counts[e.ID]++
	}
	for index, e := range previous {
		if e.ID == "" || counts[e.ID] != 1 {
			continue
		}
		if index == s.selected || s.selection[index] {
			ids[e.ID] = true
		}
		if index == s.selected {
			primary = e.ID
		}
	}
	counts = map[string]int{}
	for _, e := range restored {
		counts[e.ID]++
	}
	s.selected, s.selection = -1, map[int]bool{}
	groupPresent := false
	for index, e := range restored {
		if s.editingGroup != "" && elementGroup(e) == s.editingGroup {
			groupPresent = true
		}
		if !ids[e.ID] || counts[e.ID] != 1 || e.Inherited {
			continue
		}
		s.selection[index] = true
		if e.ID == primary {
			s.selected = index
		}
	}
	if !groupPresent {
		s.editingGroup = ""
	}
	if s.selected < 0 {
		for index := range restored {
			if s.selection[index] {
				s.selected = index
				break
			}
		}
	}
}
