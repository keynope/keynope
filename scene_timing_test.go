package main

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSceneTimingIsOptInAndContentNeutral(t *testing.T) {
	session := nativeEditorSession{deck: Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}, Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Private text must not enter timing headers", Query: "top=1&left=1&width=30&height=5"}}}}}}
	normal, profiled := httptest.NewRecorder(), httptest.NewRecorder()
	session.handleScene(normal, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	session.handleScene(profiled, httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil))
	if normal.Code != 200 || profiled.Code != 200 {
		t.Fatalf("status %d / %d", normal.Code, profiled.Code)
	}
	if normal.Body.String() != profiled.Body.String() {
		t.Fatal("profiling changed scene JSON")
	}
	if len(normal.Header().Values("Server-Timing")) != 0 {
		t.Fatal("normal request collected timings")
	}
	timings := profiled.Header().Values("Server-Timing")
	if len(timings) != 4 {
		t.Fatalf("timings: %v", timings)
	}
	for i, name := range []string{"snapshot", "projection", "annotation", "encoding"} {
		prefix := name + ";dur="
		if !strings.HasPrefix(timings[i], prefix) {
			t.Fatalf("unexpected timing %q", timings[i])
		}
		value, err := strconv.ParseFloat(strings.TrimPrefix(timings[i], prefix), 64)
		if err != nil || value < 0 {
			t.Fatalf("invalid duration %q", timings[i])
		}
	}
}
