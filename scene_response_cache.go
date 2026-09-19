package main

import (
	"bytes"
	"io"
	"strconv"
)

// One bounded response per session avoids retaining a second copy of every
// slide's embedded media. The revision covers all editor mutations, including
// history and master edits; dimensions and projection options are also inputs.
const maxSceneResponseBytes = 8 << 20

type sceneResponseKey struct {
	revision          int64
	index, cols, rows int
	master            bool
	styles, report    string
}

type sceneResponseCache struct {
	key                        sceneResponseKey
	data                       []byte
	encodedRevision            int64
	revisionStart, revisionEnd int
}

func newSceneResponseCache(key sceneResponseKey, data []byte) *sceneResponseCache {
	needle := []byte(`"revision":` + strconv.FormatInt(key.revision, 10))
	start := bytes.Index(data, needle)
	if start < 0 {
		return nil
	}
	return &sceneResponseCache{key: key, data: data, encodedRevision: key.revision, revisionStart: start + len(`"revision":`), revisionEnd: start + len(needle)}
}

func (c *sceneResponseCache) write(w io.Writer) {
	if c.key.revision == c.encodedRevision {
		_, _ = w.Write(c.data)
		return
	}
	if _, err := w.Write(c.data[:c.revisionStart]); err != nil {
		return
	}
	if _, err := io.WriteString(w, strconv.FormatInt(c.key.revision, 10)); err != nil {
		return
	}
	_, _ = w.Write(c.data[c.revisionEnd:])
}

// Called under the session lock before incrementing the editor revision.
// View-only actions change command concurrency tokens, not scene appearance.
// Publish a new immutable entry so concurrent readers keep their old snapshot.
func (s *nativeEditorSession) retainSceneForViewAction(action string) {
	if !nativeEditorViewAction(action) || s.sceneResponse == nil || s.sceneResponse.key.revision != s.version {
		return
	}
	entry := *s.sceneResponse
	entry.key.revision = s.version + 1
	s.sceneResponse = &entry
}

type sceneResponseCapture struct {
	data     []byte
	exceeded bool
}

func (c *sceneResponseCapture) Write(p []byte) (int, error) {
	if !c.exceeded {
		if len(p) > maxSceneResponseBytes-len(c.data) {
			c.data = nil
			c.exceeded = true
		} else {
			c.data = append(c.data, p...)
		}
	}
	return len(p), nil
}
