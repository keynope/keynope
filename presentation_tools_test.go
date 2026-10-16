package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func postPresentationOverlay(t *testing.T, p *presenterCompanion, sequence int64, o *PresentationOverlay) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(presentationOverlayRequest{ClientID: "owned-test-client", Sequence: sequence, Overlay: *o})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.handlePresentationOverlay(w, httptest.NewRequest(http.MethodPost, "/presentation-overlay", bytes.NewReader(body)))
	return w
}
func TestPresentationToolsEphemeralImmutableAndOrdered(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	s := buildEditorFixture(false)
	s.companion = p
	before, _ := json.Marshal(s.deck)
	beforeVersion := s.version
	cursor := p.BuildSnapshot()
	o := p.overlaySnapshot()
	o.Mode = "ink"
	o.Strokes = []PresentationInk{{ID: "stroke-one", Color: "#ff3355", Width: 6, Points: []PresentationPoint{{.1, .2}, {.3, .4}}}}
	if w := postPresentationOverlay(t, p, 2, o); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := p.overlaySnapshot()
	revision := saved.Revision
	o.Strokes[0].Points[0][0] = .9
	if p.overlaySnapshot().Strokes[0].Points[0][0] != .1 {
		t.Fatal("request data escaped into live state")
	}
	if w := postPresentationOverlay(t, p, 1, o); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if !reflect.DeepEqual(saved, p.overlaySnapshot()) {
		t.Fatal("late snapshot rewound ink")
	}
	if w := postPresentationOverlay(t, p, 3, saved); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if p.overlaySnapshot().Revision != revision {
		t.Fatal("no-op advanced overlay revision")
	}
	saved.Strokes[0].Points[0][0] = .8
	if p.overlaySnapshot().Strokes[0].Points[0][0] != .1 {
		t.Fatal("snapshot aliases live state")
	}
	after, _ := json.Marshal(s.deck)
	if !bytes.Equal(before, after) || s.version != beforeVersion || !reflect.DeepEqual(cursor, p.BuildSnapshot()) {
		t.Fatal("pointer tools changed document or build position")
	}
}
func TestPresentationToolsContextChangesAndStop(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	o := p.overlaySnapshot()
	o.Mode = "laser"
	point := PresentationPoint{.5, .5}
	o.Pointer = &point
	if w := postPresentationOverlay(t, p, 1, o); w.Code != 200 {
		t.Fatal(w.Code)
	}
	navigateCursor(t, p, "next")
	if p.overlaySnapshot().Pointer == nil {
		t.Fatal("build reveal cleared same-page pointer")
	}
	p.Update(0, 1, true, nil)
	if current := p.overlaySnapshot(); current.Pointer != nil || len(current.Strokes) != 0 || current.Mode != "laser" || current.Page != 1 {
		t.Fatal("page change did not clear temporary artwork", current)
	}
	if w := postPresentationOverlay(t, p, 2, o); w.Code != 409 {
		t.Fatal("stale page annotation accepted", w.Code)
	}
	p.Update(0, 1, false, nil)
	if p.overlaySnapshot() != nil {
		t.Fatal("Stop retained tools")
	}
	if w := postPresentationOverlay(t, p, 3, o); w.Code != 409 {
		t.Fatal("stopped session accepted annotation")
	}
	p.Update(0, 0, true, nil)
	if p.overlaySnapshot().Session == o.Session || p.overlaySnapshot().Mode != "off" {
		t.Fatal("new presentation inherited old tools")
	}
}
func TestPresentationToolsBoundsAndRejectedSnapshots(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	baseline := p.overlaySnapshot()
	for name, alter := range map[string]func(*PresentationOverlay){
		"mode":    func(o *PresentationOverlay) { o.Mode = "html" },
		"colour":  func(o *PresentationOverlay) { o.Color = "url(example)" },
		"width":   func(o *PresentationOverlay) { o.Width = 1000 },
		"pointer": func(o *PresentationOverlay) { o.Mode = "laser"; v := PresentationPoint{2, .5}; o.Pointer = &v },
		"ink coordinates": func(o *PresentationOverlay) {
			o.Strokes = []PresentationInk{{ID: "one", Color: "#ffffff", Width: 6, Points: []PresentationPoint{{-.1, .5}}}}
		},
		"too many points": func(o *PresentationOverlay) {
			o.Strokes = []PresentationInk{{ID: "one", Color: "#ffffff", Width: 6, Points: make([]PresentationPoint, 513)}}
		},
		"duplicate stroke": func(o *PresentationOverlay) {
			s := PresentationInk{ID: "one", Color: "#ffffff", Width: 6, Points: []PresentationPoint{{.1, .1}}}
			o.Strokes = []PresentationInk{s, s}
		},
	} {
		t.Run(name, func(t *testing.T) {
			o := clonePresentationOverlay(baseline)
			alter(o)
			if w := postPresentationOverlay(t, p, 1, o); w.Code != 400 {
				t.Fatal(w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(baseline, p.overlaySnapshot()) {
				t.Fatal("rejected snapshot changed live state")
			}
		})
	}
	o := clonePresentationOverlay(baseline)
	o.Width = math.NaN()
	if validatePresentationOverlay(o) == nil {
		t.Fatal("nonfinite style accepted")
	}
	p.mu.Lock()
	p.state.BuildCursor.Paused = true
	p.mu.Unlock()
	if w := postPresentationOverlay(t, p, 1, baseline); w.Code != 409 {
		t.Fatal("paused presentation accepted tools")
	}
}
