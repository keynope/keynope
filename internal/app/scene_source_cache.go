package app

import (
	"container/list"
	"crypto/sha256"
	"encoding/base64"
	"sync"
)

// Keep encoded source strings across projections without trusting asset IDs or
// slice identity: imports and draft callers may replace bytes in place. Only
// immutable strings are retained, with a bounded byte budget and LRU eviction.
type sceneSourceKey struct {
	mime   string
	digest [32]byte
}
type sceneSourceEntry struct {
	key sceneSourceKey
	uri string
}
type sceneSourceCache struct {
	mu           sync.Mutex
	limit, bytes int
	order        list.List
	entries      map[sceneSourceKey]*list.Element
}

var sharedSceneSources = sceneSourceCache{limit: 32 << 20}

func (c *sceneSourceCache) uri(mime string, data []byte) string {
	key := sceneSourceKey{mime, sha256.Sum256(data)}
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		uri := entry.Value.(sceneSourceEntry).uri
		c.mu.Unlock()
		return uri
	}
	c.mu.Unlock()
	// Encoding can be large (including original image and animation-frame
	// sources). Do not block unrelated cache hits or scene projections on it.
	uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	c.mu.Lock()
	defer c.mu.Unlock()
	// Another projection may have encoded the same source while we were
	// unlocked. Reuse its entry rather than double-counting the byte budget or
	// leaving an orphan in the LRU list.
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		return entry.Value.(sceneSourceEntry).uri
	}
	// Include MIME key storage, and cap entry count for very small resources.
	cost := len(uri) + len(mime)
	if cost > c.limit || c.limit <= 0 {
		return uri
	}
	for c.bytes+cost > c.limit || c.order.Len() >= 1024 {
		last := c.order.Back()
		old := last.Value.(sceneSourceEntry)
		delete(c.entries, old.key)
		c.bytes -= len(old.uri) + len(old.key.mime)
		c.order.Remove(last)
	}
	if c.entries == nil {
		c.entries = make(map[sceneSourceKey]*list.Element)
	}
	c.entries[key] = c.order.PushFront(sceneSourceEntry{key, uri})
	c.bytes += cost
	return uri
}
