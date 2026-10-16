package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPresentationActivityStateDoesNotResetBuildsOrInk(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	navigateCursor(t, p, "next")
	cursor := p.BuildSnapshot()
	overlay := p.overlaySnapshot()
	overlay.Mode = "ink"
	overlay.Strokes = []PresentationInk{{ID: "owned-ink", Color: "#ffffff", Width: 6, Points: []PresentationPoint{{.2, .3}, {.4, .5}}}}
	if w := postPresentationOverlay(t, p, 1, overlay); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := p.overlaySnapshot()
	for _, phase := range []int{0, 1, 2, 3, 4, 1} {
		activity := &EngagementRuntimeState{Definition: EngagementDefinition{ID: "owned-pulse", Kind: "pulse", Prompt: "How ready are we?", Options: append([]string(nil), defaultPulseOptions...)}, Slide: 0, Phase: phase, Counts: []int{0, 1, 0, 0, 0, 0}}
		body, _ := json.Marshal(activity)
		w := httptest.NewRecorder()
		p.handleEngagement(w, httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewReader(body)))
		if w.Code != http.StatusNoContent {
			t.Fatal(phase, w.Code, w.Body.String())
		}
		activity.Counts[1] = 99
		p.mu.RLock()
		got := cloneEngagementRuntime(p.state.Engagement)
		p.mu.RUnlock()
		if got.Counts[1] != 1 {
			t.Fatal("activity transport aliases request")
		}
		if !reflect.DeepEqual(cursor, p.BuildSnapshot()) || !reflect.DeepEqual(before, p.overlaySnapshot()) {
			t.Fatal("activity changed presentation playback", phase)
		}
		w = httptest.NewRecorder()
		p.handleEngagement(w, httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewBufferString("null")))
		if w.Code != http.StatusNoContent || !reflect.DeepEqual(cursor, p.BuildSnapshot()) || !reflect.DeepEqual(before, p.overlaySnapshot()) {
			t.Fatal("closing activity restarted presentation", phase)
		}
	}
}
