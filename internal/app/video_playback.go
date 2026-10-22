package app

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Runtime-only, localhost state shared by the editor monitor and presentation
// windows. Never included in authored decks or participant broadcasts.
type presentationVideoPlayback struct {
	Revision  int64   `json:"revision"`
	UpdatedMS int64   `json:"updatedMs"`
	Playing   bool    `json:"playing"`
	Time      float64 `json:"time"`
	Rate      float64 `json:"rate"`
	Volume    float64 `json:"volume"`
	Muted     bool    `json:"muted"`
	resume    bool
}

type presentationVideoRequest struct {
	Session  string  `json:"session"`
	SlideID  string  `json:"slideId"`
	Slide    int     `json:"slide"`
	Page     int     `json:"page"`
	ObjectID string  `json:"objectId"`
	ClientID string  `json:"clientId"`
	Sequence int64   `json:"sequence"`
	Playing  bool    `json:"playing"`
	Time     float64 `json:"time"`
	Rate     float64 `json:"rate"`
	Volume   float64 `json:"volume"`
	Muted    bool    `json:"muted"`
}

func (p *presenterCompanion) syncVideoPlaybackLocked() {
	c := p.state.BuildCursor
	context := ""
	if c != nil {
		context = c.Session + "/" + c.SlideID + "/" + strconv.Itoa(p.state.Slide) + "/" + strconv.Itoa(p.state.Page)
	}
	if context == "" || context != p.videoContext {
		p.videoContext = context
		p.state.Videos = nil
		p.videoSequences = nil
		p.videoPaused = false
	}
	paused := c != nil && (c.Paused || p.state.TimerMode != "")
	if paused == p.videoPaused {
		return
	}
	p.videoPaused = paused
	now := time.Now().UnixMilli()
	next := make(map[string]presentationVideoPlayback, len(p.state.Videos))
	for id, state := range p.state.Videos {
		if paused {
			state.resume = state.Playing
			if state.Playing {
				state.Time += math.Max(0, float64(now-state.UpdatedMS)) / 1000 * state.Rate
			}
			state.Playing = false
		} else {
			state.Playing, state.resume = state.resume, false
		}
		p.videoRevision++
		state.Revision, state.UpdatedMS = p.videoRevision, now
		next[id] = state
	}
	p.state.Videos = next
}

func (p *presenterCompanion) handleVideoPlayback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var input presentationVideoRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil || !presentationToolID.MatchString(input.ClientID) || input.Sequence < 1 || input.Sequence > 9007199254740991 || !validObjectID(input.ObjectID) {
		http.Error(w, "invalid video playback command", http.StatusBadRequest)
		return
	}
	for _, value := range []float64{input.Time, input.Rate, input.Volume} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			http.Error(w, "invalid video playback value", http.StatusBadRequest)
			return
		}
	}
	if input.Time < 0 || input.Time > 86400 || input.Rate < .25 || input.Rate > 4 || input.Volume < 0 || input.Volume > 1 {
		http.Error(w, "video playback value out of range", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	c := p.state.BuildCursor
	if c == nil || !p.state.Presenting || c.Paused || p.state.TimerMode != "" || input.Session != c.Session || input.SlideID != c.SlideID || input.Slide != p.state.Slide || input.Page != p.state.Page {
		p.mu.Unlock()
		http.Error(w, "presentation changed; refresh video controls", http.StatusConflict)
		return
	}
	found := false
	for _, page := range p.pages[p.state.Slide] {
		if page.Page == p.state.Page && page.Scene != nil {
			for _, object := range page.Scene.Objects {
				found = found || object.ID == input.ObjectID && object.Kind == "video" && object.Video != nil
			}
		}
	}
	if !found || len(p.videoSequences) >= 32 && p.videoSequences[input.ClientID] == 0 {
		p.mu.Unlock()
		http.Error(w, "video is not available in this presentation", http.StatusConflict)
		return
	}
	if input.Sequence > p.videoSequences[input.ClientID] {
		next := make(map[string]presentationVideoPlayback, len(p.state.Videos)+1)
		for id, state := range p.state.Videos {
			next[id] = state
		}
		p.videoRevision++
		next[input.ObjectID] = presentationVideoPlayback{Revision: p.videoRevision, UpdatedMS: time.Now().UnixMilli(), Playing: input.Playing, Time: input.Time, Rate: input.Rate, Volume: input.Volume, Muted: input.Muted}
		if p.videoSequences == nil {
			p.videoSequences = map[string]int64{}
		}
		p.videoSequences[input.ClientID] = input.Sequence
		p.state.Videos = next
		p.state.Version++
	}
	result := p.state.Videos
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}
