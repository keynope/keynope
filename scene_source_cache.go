package main

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
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		return entry.Value.(sceneSourceEntry).uri
	}
	uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
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
