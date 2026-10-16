package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

type connectorDraftRequest struct {
	Revision int64                       `json:"revision"`
	Master   bool                        `json:"master"`
	Slide    int                         `json:"slide"`
	Bounds   map[string]sceneRect        `json:"bounds"` // Authored grid coordinates, not pixels.
	Routes   map[string][]connectorPoint `json:"routes,omitempty"`
}

// Project routing against temporary shape geometry. The authoring document and
// history are never mutated, and no media/text scene objects are serialized.
func (s *nativeEditorSession) handleConnectorDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var request connectorDraftRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&request) != nil || len(request.Bounds) == 0 || len(request.Bounds) > 1000 || len(request.Routes) > 1000 {
		http.Error(w, "invalid connector draft", 400)
		return
	}
	s.mu.RLock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	if request.Revision != s.version || request.Master != s.masterMode || request.Slide != current {
		s.mu.RUnlock()
		http.Error(w, "document changed", 409)
		return
	}
	scope, scopeErr := captureSlideResolutionScope(s.deck, request.Slide, request.Master)
	editingGroup := s.editingGroup
	s.mu.RUnlock()
	if scopeErr != nil {
		http.Error(w, "invalid slide scope", 400)
		return
	}
	ids := make([]string, 0, len(request.Bounds))
	for id := range request.Bounds {
		ids = append(ids, id)
	}
	foundIDs := make(map[string]bool, len(ids))
	for _, element := range scope.source.Elements {
		if _, wanted := request.Bounds[element.ID]; wanted {
			foundIDs[element.ID] = true
		}
	}
	for _, id := range ids {
		if !foundIDs[id] {
			http.Error(w, "shape no longer exists", 404)
			return
		}
	}
	indices, targetErr := editableObjectIndices(scope.source, ids)
	if targetErr != nil {
		http.Error(w, targetErr.Error(), 400)
		return
	}
	command := nativeEditorAction{Action: "update-elements", ElementIndices: indices}
	for _, i := range indices {
		command.ElementsData = append(command.ElementsData, scope.source.Elements[i])
	}
	if err := checkObjectLocks(scope.source, command, nil, editingGroup); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	deck := scope.deck
	slide := deck.withThemeTypography(withTrueTypeDefaults(scope.resolved()))
	cols, rows := authoredRenderSize(245, 56)
	seen := 0
	for i, e := range slide.Elements {
		b, ok := request.Bounds[e.ID]
		if !ok {
			continue
		}
		if e.Kind != "shape" {
			http.Error(w, "draft target is not a shape", 400)
			return
		}
		for _, v := range []float64{b.X, b.Y, b.Width, b.Height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				http.Error(w, "invalid bounds", 400)
				return
			}
		}
		if b.X < 0 || b.Y < 0 || b.Width <= 0 || b.Height <= 0 || b.X+b.Width > float64(cols)+1e-6 || b.Y+b.Height > float64(rows)+1e-6 {
			http.Error(w, "invalid bounds", 400)
			return
		}
		q, _ := url.ParseQuery(e.Query)
		for _, key := range []string{"bottom", "right", "left_pct", "right_pct", "row_delta", "align", "valign", "shape-offset-x", "shape-offset-y"} {
			q.Del(key)
		}
		q.Set("left", strconv.Itoa(int(math.Floor(b.X))))
		q.Set("top", strconv.Itoa(int(math.Floor(b.Y))))
		for key, v := range map[string]float64{"width": b.Width, "height": b.Height, "object-offset-x": b.X - math.Floor(b.X), "object-offset-y": b.Y - math.Floor(b.Y)} {
			q.Set(key, strconv.FormatFloat(v, 'f', -1, 64))
		}
		slide.Elements[i].Query = q.Encode()
		seen++
	}
	if seen != len(request.Bounds) {
		http.Error(w, "shape no longer exists", 404)
		return
	}
	seen = 0
	for i, e := range slide.Elements {
		points, ok := request.Routes[e.ID]
		if !ok {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		_, from := request.Bounds[q.Get("connector-from")]
		_, to := request.Bounds[q.Get("connector-to")]
		encoded, _ := json.Marshal(points)
		if e.Kind != "connector" || !from || !to || decodeConnectorRoute(string(encoded)) == nil {
			http.Error(w, "invalid draft route", 400)
			return
		}
		q.Set("connector-route", string(encoded))
		slide.Elements[i].Query = q.Encode()
		seen++
	}
	if seen != len(request.Routes) {
		http.Error(w, "connector no longer exists", 404)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	// Match the unified scene, not the legacy terminal pagination/rounding path.
	fitSlideShapeLabels(&slide, cols, rows)
	lines := layout(slide, cols, rows)
	if r.Context().Err() != nil {
		return
	}
	routes := slideShapeConnectors(slide, lines, cols, rows)
	// Keep the Retro preview on the same semi-block painter as committed
	// connectors. The client can retain Modern paint and replace only points.
	// layout already includes the unified-scene connector raster.
	// Reuse it rather than calculating ports, obstacles and paths again.
	raster := lines
	art := map[string][]exportLine{}
	retroSlide := slide
	retroSlide.ThemeColors = deck.themeColors("retro")
	for index, e := range slide.Elements {
		if e.Kind == "connector" && !deck.usesConcreteAppearance() && deck.elementStyle(slide, e) == "retro" {
			art[e.ID] = retroObjectLines(deck, retroSlide, raster, index, cols, rows)
		}
	}
	if r.Context().Err() != nil {
		return
	}
	s.mu.RLock()
	valid := request.Revision == s.version && request.Master == s.masterMode && ((s.masterMode && request.Slide == s.currentMaster) || (!s.masterMode && request.Slide == s.current))
	s.mu.RUnlock()
	if !valid {
		http.Error(w, "document changed; draft discarded", 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(struct {
		Revision int64                   `json:"revision"`
		Routes   []shapeConnector        `json:"routes"`
		Lines    map[string][]exportLine `json:"lines"`
	}{request.Revision, routes, art})
}
