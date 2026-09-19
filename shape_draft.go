package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

// Render an isolated shape body without changing the deck, undo history or
// dirty state. Text editing uses this for a sampled body at its draft size.
func (s *nativeEditorSession) handleShapeDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var action nativeEditorAction
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&action) != nil || action.ShapeTextBounds == nil {
		http.Error(w, "invalid shape draft", 400)
		return
	}
	b := *action.ShapeTextBounds
	if math.IsNaN(b.Width) || math.IsInf(b.Width, 0) || math.IsNaN(b.Height) || math.IsInf(b.Height, 0) || b.Width <= 0 || b.Height <= 0 || b.Width > 20000 || b.Height > 20000 {
		http.Error(w, "invalid shape dimensions", 400)
		return
	}
	s.mu.RLock()
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode {
		s.mu.RUnlock()
		http.Error(w, "document changed; reopen the editor", 409)
		return
	}
	deck := cloneDeckForRender(s.deck)
	s.mu.RUnlock()
	var slide Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			http.Error(w, "invalid master", 400)
			return
		}
		slide = masterViewPreview(deck.Masters, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			http.Error(w, "invalid slide", 400)
			return
		}
		slide = deck.ResolveSlide(action.Slide, false)
	}
	cols, rows := authoredRenderSize(245, 56)
	for _, e := range slide.Elements {
		if r.Context().Err() != nil {
			return
		}
		if e.ID != action.ObjectID || e.Kind != "shape" {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		for _, key := range []string{"top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "valign", "shape-offset-x", "shape-offset-y", "shape-label", "orientation"} {
			q.Del(key)
		}
		q.Set("top", "0")
		q.Set("left", "0")
		q.Set("width", strconv.FormatFloat(math.Ceil(b.Width/1920*float64(cols)*2-1e-8)/2, 'f', 1, 64))
		q.Set("height", strconv.FormatFloat(math.Ceil(b.Height/1080*float64(rows)*2-1e-8)/2, 'f', 1, 64))
		e.Query = q.Encode()
		slide.Elements = []Element{e}
		scene, err := buildStyledSlideScene(deck, slide, action.Slide, cols, rows, true)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scene)
		return
	}
	http.Error(w, "shape no longer exists", 404)
}
