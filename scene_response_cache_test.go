package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSceneResponseRetainedAcrossSelection(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(3), true, false)
	s.version = 9
	get := func(profile bool) []byte {
		path := "/api/editor/scene?slide=0"
		if profile {
			path += "&timing=1"
		}
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		return w.Body.Bytes()
	}
	get(false)
	original := s.sceneResponse
	for i := 0; i < 12; i++ {
		if err := s.apply(nativeEditorAction{Action: "select-element", Element: i % 3}); err != nil {
			t.Fatal(err)
		}
		data := get(false)
		if &s.sceneResponse.data[0] != &original.data[0] {
			t.Fatal("selection rebuilt the scene payload")
		}
		var decoded slideScene
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Revision == nil || *decoded.Revision != s.version {
			t.Fatal("retained scene has stale command revision")
		}
		if !bytes.Equal(data, get(true)) {
			t.Fatal("retained selection scene differs from fresh projection")
		}
	}
	if original.key.revision != 9 {
		t.Fatal("in-flight immutable entry was mutated")
	}
}

func TestSceneResponseCacheConcurrentReaders(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(3), true, false)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := s.apply(nativeEditorAction{Action: "select-element", Element: i % 3}); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 5; n++ {
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
				if w.Code != 200 {
					t.Errorf("scene status %d", w.Code)
				}
			}
		}()
	}
	wg.Wait()
}

func TestSceneResponseCacheRevisionAndOptions(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(3), true, false)
	get := func(options string) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0"+options, nil))
		if w.Code != 200 {
			t.Fatalf("scene: %d %s", w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	first := get("")
	entry := s.sceneResponse
	if entry == nil {
		t.Fatal("response was not cached")
	}
	if !bytes.Equal(first, get("")) || entry != s.sceneResponse {
		t.Fatal("repeat request missed immutable response")
	}
	if !bytes.Equal(first, get("&timing=1")) {
		t.Fatal("cached response differs from fresh projection")
	}
	get("&styles=resolved")
	if s.sceneResponse == entry || s.sceneResponse.key.styles != "resolved" {
		t.Fatal("projection options reused the wrong entry")
	}
	beforeRevision := s.version
	updated := Slide{BG: "#123456"}
	if err := s.apply(nativeEditorAction{Action: "update-slide", SlideData: &updated}); err != nil {
		t.Fatal(err)
	}
	changed := get("")
	if s.version == beforeRevision || bytes.Equal(changed, first) || s.sceneResponse.key.revision != s.version {
		t.Fatal("mutation served stale scene")
	}
	if !bytes.Equal(changed, get("&timing=1")) {
		t.Fatal("mutation cache differs from fresh scene")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	restored := get("")
	if bytes.Equal(restored, changed) || !bytes.Equal(restored, get("&timing=1")) {
		t.Fatal("Undo served stale scene")
	}
	// Master mode is a separate projection input even if the revision matches.
	s.mu.Lock()
	s.masterMode = true
	s.mu.Unlock()
	master := get("")
	if !s.sceneResponse.key.master || bytes.Equal(master, restored) || !bytes.Equal(master, get("&timing=1")) {
		t.Fatal("master projection reused ordinary slide")
	}
}

func TestSceneResponseCaptureIsBounded(t *testing.T) {
	var capture sceneResponseCapture
	chunk := bytes.Repeat([]byte{'x'}, 1<<20)
	for i := 0; i < 9; i++ {
		n, err := capture.Write(chunk)
		if n != len(chunk) || err != nil {
			t.Fatal("capture must not interrupt response streaming")
		}
	}
	if !capture.exceeded || capture.data != nil {
		t.Fatal("oversized response retained")
	}
	capture.Write([]byte("tail"))
	if capture.data != nil {
		t.Fatal("capture resumed after exceeding limit")
	}
}
