package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"regexp"
)

// Presentation tools are transient companion state, never Deck metadata. Each
// accepted snapshot is immutable so the polling HTTP reader can encode it after
// releasing the lock without racing a pointer move.
type PresentationPoint [2]float64
type PresentationInk struct {
	ID     string              `json:"id"`
	Color  string              `json:"color"`
	Width  float64             `json:"width"`
	Points []PresentationPoint `json:"points"`
}
type PresentationOverlay struct {
	Session  string             `json:"session"`
	SlideID  string             `json:"slideId"`
	Slide    int                `json:"slide"`
	Page     int                `json:"page"`
	Revision int64              `json:"revision"`
	Mode     string             `json:"mode"`
	Color    string             `json:"color"`
	Width    float64            `json:"width"`
	Pointer  *PresentationPoint `json:"pointer,omitempty"`
	Strokes  []PresentationInk  `json:"strokes"`
}
type presentationOverlayRequest struct {
	ClientID string              `json:"clientId"`
	Sequence int64               `json:"sequence"`
	Overlay  PresentationOverlay `json:"overlay"`
}

var presentationToolID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var presentationToolColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func clonePresentationOverlay(source *PresentationOverlay) *PresentationOverlay {
	if source == nil {
		return nil
	}
	copy := *source
	if source.Pointer != nil {
		point := *source.Pointer
		copy.Pointer = &point
	}
	copy.Strokes = make([]PresentationInk, len(source.Strokes))
	for i, stroke := range source.Strokes {
		copy.Strokes[i] = stroke
		copy.Strokes[i].Points = append([]PresentationPoint(nil), stroke.Points...)
	}
	return &copy
}
func validPresentationPoint(point PresentationPoint) bool {
	for _, value := range point {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}
func validatePresentationOverlay(o *PresentationOverlay) error {
	if o == nil || len(o.Session) > 256 || len(o.SlideID) > 256 || o.Session == "" || o.SlideID == "" || o.Slide < 0 || o.Page < 0 {
		return fmt.Errorf("invalid presentation context")
	}
	switch o.Mode {
	case "off", "laser", "spotlight", "ink":
	default:
		return fmt.Errorf("invalid presentation tool")
	}
	if !presentationToolColor.MatchString(o.Color) || math.IsNaN(o.Width) || math.IsInf(o.Width, 0) || o.Width < 1 || o.Width > 24 || len(o.Strokes) > 32 {
		return fmt.Errorf("invalid presentation tool style or stroke count")
	}
	if o.Pointer != nil && (!validPresentationPoint(*o.Pointer) || o.Mode == "off" || o.Mode == "ink") {
		return fmt.Errorf("invalid presentation pointer")
	}
	seen := map[string]bool{}
	points := 0
	for _, stroke := range o.Strokes {
		if !presentationToolID.MatchString(stroke.ID) || seen[stroke.ID] || !presentationToolColor.MatchString(stroke.Color) || math.IsNaN(stroke.Width) || math.IsInf(stroke.Width, 0) || stroke.Width < 1 || stroke.Width > 24 || len(stroke.Points) < 1 || len(stroke.Points) > 512 {
			return fmt.Errorf("invalid presentation ink")
		}
		seen[stroke.ID] = true
		points += len(stroke.Points)
		if points > 2048 {
			return fmt.Errorf("temporary ink limit reached")
		}
		for _, point := range stroke.Points {
			if !validPresentationPoint(point) {
				return fmt.Errorf("invalid ink position")
			}
		}
	}
	return nil
}
func (p *presenterCompanion) syncPresentationOverlayLocked() {
	c := p.state.BuildCursor
	if c == nil {
		p.state.Overlay = nil
		p.overlaySequences = nil
		return
	}
	o := p.state.Overlay
	if o != nil && o.Session == c.Session && o.SlideID == c.SlideID && o.Slide == p.state.Slide && o.Page == p.state.Page {
		return
	}
	mode, color, width := "off", "#ff3355", float64(6)
	if o != nil && o.Session == c.Session {
		mode, color, width = o.Mode, o.Color, o.Width
	}
	p.overlayRevision++
	p.state.Overlay = &PresentationOverlay{Session: c.Session, SlideID: c.SlideID, Slide: p.state.Slide, Page: p.state.Page, Revision: p.overlayRevision, Mode: mode, Color: color, Width: width, Strokes: []PresentationInk{}}
	p.overlaySequences = map[string]int64{}
}
func (p *presenterCompanion) overlaySnapshot() *PresentationOverlay {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return clonePresentationOverlay(p.state.Overlay)
}
func (p *presenterCompanion) handlePresentationOverlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input presentationOverlayRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10)).Decode(&input); err != nil {
		http.Error(w, "invalid presentation tool snapshot", http.StatusBadRequest)
		return
	}
	if !presentationToolID.MatchString(input.ClientID) || input.Sequence < 1 || input.Sequence > 9007199254740991 || validatePresentationOverlay(&input.Overlay) != nil {
		http.Error(w, "invalid presentation tool snapshot", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	c, o := p.state.BuildCursor, p.state.Overlay
	if c == nil || o == nil || c.Paused || p.state.TimerMode != "" || input.Overlay.Session != c.Session || input.Overlay.SlideID != c.SlideID || input.Overlay.Slide != p.state.Slide || input.Overlay.Page != p.state.Page {
		p.mu.Unlock()
		http.Error(w, "presentation changed; refresh before using presenter tools", http.StatusConflict)
		return
	}
	if _, exists := p.overlaySequences[input.ClientID]; !exists && len(p.overlaySequences) >= 32 {
		p.mu.Unlock()
		http.Error(w, "too many presentation tool clients", http.StatusConflict)
		return
	}
	if input.Sequence > p.overlaySequences[input.ClientID] {
		candidate := clonePresentationOverlay(&input.Overlay)
		candidate.Revision = o.Revision
		p.overlaySequences[input.ClientID] = input.Sequence
		if !reflect.DeepEqual(candidate, o) {
			p.overlayRevision++
			candidate.Revision = p.overlayRevision
			p.state.Overlay = candidate
			p.state.Version++
		}
	}
	result := p.state.Overlay // Immutable after publishing.
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}
