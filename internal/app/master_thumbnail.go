package app

// Thumbnail generations are renderer bookkeeping, not document revisions or
// authored metadata. Track only requested previews and discard on mode exit.
// The limit bounds work/memory even with a very large imported layout archive.
const maxTrackedMasterPreviews = 128

type masterPreviewStamp struct {
	revision   int64
	cols, rows int
	used       uint64
}

// All helpers run under the editor lock. Unknown/evicted previews conservatively
// use the current revision until their first render registers dependencies.
func (s *nativeEditorSession) masterPreviewRevisionsLocked() []int64 {
	if !s.masterMode {
		return nil
	}
	values := make([]int64, len(s.deck.Masters.Layouts)+1)
	for index := range values {
		values[index] = s.version
		if stamp, exists := s.masterPreviews[index]; exists {
			values[index] = stamp.revision
		}
	}
	return values
}

func (s *nativeEditorSession) trackMasterPreviewLocked(index, cols, rows int) {
	if s.masterPreviews == nil {
		s.masterPreviews = make(map[int]masterPreviewStamp)
	}
	stamp, exists := s.masterPreviews[index]
	if !exists || stamp.cols != cols || stamp.rows != rows {
		stamp.revision = s.version
	}
	if !exists && len(s.masterPreviews) >= maxTrackedMasterPreviews {
		oldest := -1
		var used uint64
		for candidate, value := range s.masterPreviews {
			if oldest < 0 || value.used < used {
				oldest, used = candidate, value.used
			}
		}
		delete(s.masterPreviews, oldest)
	}
	s.masterPreviewUse++
	stamp.cols, stamp.rows, stamp.used = cols, rows, s.masterPreviewUse
	s.masterPreviews[index] = stamp
}

func (s *nativeEditorSession) invalidateMasterPreviewsLocked(before Deck, revision int64) {
	if !s.masterMode {
		s.masterPreviews = nil
		return
	}
	if s.version == revision {
		return
	} // Failed/no-op commands do not invalidate.
	for index, stamp := range s.masterPreviews {
		if index > len(s.deck.Masters.Layouts) {
			delete(s.masterPreviews, index)
			continue
		}
		key := sceneResponseKey{master: true, index: index, cols: stamp.cols, rows: stamp.rows}
		if len(before.Masters.Layouts) != len(s.deck.Masters.Layouts) || !sceneProjectionInputsEqual(before, s.deck, key) {
			stamp.revision = s.version
			s.masterPreviews[index] = stamp
		}
	}
}
