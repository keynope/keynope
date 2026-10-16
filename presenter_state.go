package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// Ordinary presenter polling remains nonblocking. The editing surface can ask
// for the matching accepted projection rather than waiting for debounce/poll.
// The owner is runtime-only and validated against the bound editor session;
// it is neither authored deck metadata nor part of the audience state.
func (p *presenterCompanion) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if revision, err := strconv.ParseInt(r.URL.Query().Get("editorRevision"), 10, 64); err == nil && revision > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		p.awaitEditorProjection(ctx, r.URL.Query().Get("editorOwner"), revision)
		cancel()
	}
	if r.Context().Err() != nil {
		return
	}
	p.mu.RLock()
	state := p.state
	p.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(state)
}
