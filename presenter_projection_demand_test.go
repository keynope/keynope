package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeProjectionDemandPublicationAndOwnership(t *testing.T) {
	s, p, path := nativeProjectionFixture(t)
	defer p.Close()
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision,
		ObjectIDs: []string{s.deck.Slides[0].Elements[0].ID}, Property: "paint.color", PropertyValue: json.RawMessage(`"#ff00aa"`)}); err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	urgent := p.nativeProjection.urgent
	p.mu.RUnlock()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
	if err := ctx.Err(); err != nil {
		t.Fatal("accepted demand did not finish", err)
	}
	select {
	case <-urgent:
	default:
		t.Fatal("accepted demand did not bypass debounce")
	}
	p.mu.RLock()
	pending := p.nativeProjection.pending || p.nativeProjection.done != nil || p.nativeProjection.urgent != nil
	p.mu.RUnlock()
	if pending {
		t.Fatal("published projection retained waiters")
	}
	assertNativeProjectionMatchesCold(t, s, p, path)
	// Selection increments the editor version without changing document paint.
	if err := s.apply(nativeEditorAction{Action: "select-element", ElementData: &Element{ID: s.deck.Slides[0].Elements[0].ID}}); err != nil {
		t.Fatal(err)
	}
	p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
	if ctx.Err() != nil {
		t.Fatal("view-only version waited for a nonexistent scene")
	}
}

func TestNativeProjectionDemandIgnoresForeignFutureAndCancelled(t *testing.T) {
	s, p, _ := nativeProjectionFixture(t)
	defer p.Close()
	// No running projector: this negative assertion cannot race the normal
	// debounce timer on a slow machine.
	p.mu.Lock()
	p.nativeProjection.pending = true
	p.nativeProjection.urgent, p.nativeProjection.done = make(chan struct{}), make(chan struct{})
	urgent := p.nativeProjection.urgent
	p.mu.Unlock()
	p.awaitEditorProjection(t.Context(), "foreign", s.version)
	p.awaitEditorProjection(t.Context(), s.notesSessionID, s.version+1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p.awaitEditorProjection(ctx, s.notesSessionID, s.version)
	select {
	case <-urgent:
		t.Fatal("foreign/future/cancelled demand interrupted debounce")
	default:
	}
}

func TestNativeProjectionDemandCancellationAndSupersession(t *testing.T) {
	for _, finish := range []string{"cancel", "close", "rebind", "invalidate", "supersede"} {
		t.Run(finish, func(t *testing.T) {
			s, p, _ := nativeProjectionFixture(t)
			defer p.Close()
			// A deliberately stalled job isolates wait ownership from scheduling.
			p.mu.Lock()
			projection := p.nativeProjection
			projection.pending, projection.revision = true, s.version
			projection.urgent, projection.done = make(chan struct{}), make(chan struct{})
			urgent, done := projection.urgent, projection.done
			p.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			returned := make(chan struct{})
			go func() { p.awaitEditorProjection(ctx, s.notesSessionID, s.version); close(returned) }()
			select {
			case <-urgent:
			case <-time.After(time.Second):
				t.Fatal("demand did not wake the current projection")
			}
			switch finish {
			case "cancel":
				cancel()
			case "close":
				p.Close()
			case "rebind":
				other := newNativeEditorSession("other.md", s.deck)
				p.bindNativeProjection(other, cloneDeckForScene(other.deck), other.version, 245, 56)
			case "invalidate":
				p.mu.Lock()
				p.invalidateNativeProjectionLocked()
				p.mu.Unlock()
			case "supersede":
				p.mu.Lock()
				finishNativeProjectionWaiters(projection)
				projection.revision++
				projection.urgent, projection.done = make(chan struct{}), make(chan struct{})
				nextUrgent := projection.urgent
				p.mu.Unlock()
				select {
				case <-nextUrgent:
				case <-time.After(time.Second):
					t.Fatal("demand did not follow the superseding projection")
				}
				select {
				case <-returned:
					t.Fatal("old completion released demand before latest publication")
				default:
				}
				p.mu.Lock()
				projection.pending = false
				finishNativeProjectionWaiters(projection)
				p.mu.Unlock()
			}
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("demand did not release on", finish)
			}
			if finish != "cancel" {
				select {
				case <-done:
				default:
					t.Fatal("old lease remained alive after", finish)
				}
			}
		})
	}
}

func TestNativeProjectionDemandStateHandler(t *testing.T) {
	s, p, _ := nativeProjectionFixture(t)
	defer p.Close()
	request := httptest.NewRequest(http.MethodGet, "/state?editorRevision=1&editorOwner="+url.QueryEscape(s.notesSessionID), nil)
	response := httptest.NewRecorder()
	p.handleState(response, request)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), s.notesSessionID) {
		t.Fatal("invalid or private presenter state", response.Code, response.Body.String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response = httptest.NewRecorder()
	p.handleState(response, request.WithContext(ctx))
	if response.Body.Len() != 0 {
		t.Fatal("cancelled request published state")
	}
}

// Owned loopback server for the native HTTP/shared-client latency measurement.
// No installed app, customer deck, cloud service or authentication is touched.
func TestNativeEditingLatencyBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_NATIVE_EDITING_EVIDENCE")
	if root == "" {
		t.Skip("native editing fixture not requested")
	}
	useTestAuthoredSize(t, 245, 56)
	deck := powerupDenseEditingDeck(t)
	if os.Getenv("KEYNOPE_NATIVE_LATENCY_COMPARISON") == "dependencies" {
		// Keep the original dense active page, plus 99 independently owned
		// forty-text pages. The native publisher must not resolve them for
		// local paint/metric edits on the active slide.
		for page := 1; page < 100; page++ {
			slide := cloneSlide(deck.Slides[0])
			slide.ID = fmt.Sprintf("background-slide-%d", page)
			slide.Elements = slide.Elements[2:42]
			deck.Slides = append(deck.Slides, slide)
		}
	}
	s := newNativeEditorSession("dense.md", deck)
	p := &presenterCompanion{}
	defer p.Close()
	path := filepath.Join(t.TempDir(), "dense.md")
	if err := p.Refresh(path, s.deck.ResolvedSlides(), 245, 56); err != nil {
		t.Fatal(err)
	}
	s.deckPath, s.companion = path, p
	p.bindNativeProjection(s, cloneDeckForScene(s.deck), s.version, 245, 56)
	p.Update(0, 0, false, nil)
	mux := http.NewServeMux()
	if os.Getenv("KEYNOPE_NATIVE_LATENCY_COMPARISON") == "dependencies" {
		// Isolated fixture-only A/B control. Never compiled into the shipped
		// application: disable only the source index, not demand publication,
		// object deltas, rendering, or the actual native comparison function.
		mux.HandleFunc("/test/native-dependencies", func(w http.ResponseWriter, r *http.Request) {
			if enabled := r.URL.Query().Get("enabled"); enabled != "" {
				if enabled != "0" && enabled != "1" {
					http.Error(w, "invalid index control", http.StatusBadRequest)
					return
				}
				p.mu.RLock()
				projection, seq := p.nativeProjection, p.seq
				baseline := projection.deck
				pending := projection.pending
				p.mu.RUnlock()
				if pending || baseline == nil {
					http.Error(w, "native publication pending", http.StatusConflict)
					return
				}
				var dependencies *nativeProjectionDependencies
				if enabled == "1" {
					dependencies = captureNativeProjectionDependencies(*baseline)
				}
				p.mu.Lock()
				if p.nativeProjection != projection || p.seq != seq || projection.pending || projection.deck != baseline {
					p.mu.Unlock()
					http.Error(w, "native baseline changed", http.StatusConflict)
					return
				}
				projection.dependencies = dependencies
				p.mu.Unlock()
			}
			p.mu.RLock()
			stats := map[string]int{"slides": len(p.nativeProjection.deck.Slides), "considered": len(p.nativeProjection.considered), "projected": len(p.nativeProjection.projected)}
			p.mu.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(stats)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := p.htmlSnapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/state", p.handleState)
	mux.HandleFunc("/slides", p.handlePageUpdate)
	mux.HandleFunc("/api/editor/state", s.handleState)
	mux.HandleFunc("/api/editor/action", s.handleAction)
	mux.HandleFunc("/api/editor/scene", s.handleScene)
	mux.HandleFunc("/api/editor/workspace", s.handleWorkspace)
	mux.HandleFunc("/api/editor/preview", s.handlePreview)
	mux.HandleFunc("/api/editor/connector-draft", s.handleConnectorDraft)
	mux.HandleFunc("/api/editor/scene-fonts", s.handleSceneFonts)
	server := httptest.NewServer(mux)
	defer server.Close()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]string{"url": server.URL + "/?keynopeSurface=app"})
	if err := os.WriteFile(filepath.Join(root, "connection.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.Context().Done():
			return
		case <-deadline.C:
			t.Fatal("native editing fixture timed out")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
				return
			}
		}
	}
}
