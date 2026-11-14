//go:build !js && !keynope_sitegen

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func liveMCPFixture(t *testing.T) (*nativeEditorSession, *deckAutomation) {
	t.Helper()
	dir := t.TempDir()
	s := newNativeEditorSession(filepath.Join(dir, "deck.md"), Deck{Slides: []Slide{
		{ID: "first", Elements: []Element{{ID: "a", Kind: "text", Text: "First", Query: "render=truetype&modern-font=sans&modern-size=100"}, {ID: "b", Kind: "text", Text: "Second", Query: "render=truetype&modern-font=sans&modern-size=100"}}},
		{ID: "other", Elements: []Element{{ID: "c", Kind: "text", Text: "Elsewhere", Query: "render=truetype&modern-font=sans&modern-size=100"}}},
	}})
	a, err := newDeckAutomation(s, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.root.Close() })
	return s, a
}
func liveCommand(t *testing.T, a *deckAutomation, version int64, command any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(command)
	r, _, err := a.call(context.Background(), "editor_command", automationArguments{ExpectedVersion: &version, Command: raw})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestLiveMCPMaintainsHumanViewAndSharedUndo(t *testing.T) {
	s, a := liveMCPFixture(t)
	if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	initial := s.state()
	version := initial.Version
	liveCommand(t, a, version, map[string]any{"action": "select-slide", "slide": 1})
	if got := s.state(); got.Current != 0 || got.Selected != 0 {
		t.Fatalf("MCP moved the human view: %#v", got)
	}
	liveCommand(t, a, s.version, map[string]any{"action": "add-slide"})
	if got := s.state(); got.Current != 0 || got.Selected != 0 || len(got.Slides) != 3 {
		t.Fatal("new slide moved selection or was not shared")
	}
	if len(s.undo) != 1 {
		t.Fatal("MCP should share native undo")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Slides) != 2 {
		t.Fatal("human undo did not undo MCP")
	}
}
func TestLiveMCPRebasesAndCancelsOnlyAffectedSelection(t *testing.T) {
	s, a := liveMCPFixture(t)
	if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	baseline := s.version
	second := s.deck.Slides[0].Elements[1]
	second.Text = "Human change"
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: 1, ElementData: &second}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	first := s.deck.Slides[0].Elements[0]
	first.Text = "MCP change"
	liveCommand(t, a, baseline, map[string]any{"action": "update-element", "elementData": first})
	state := s.state()
	if state.Selected != -1 || state.Slides[0].Elements[0].Text != "MCP change" || state.Slides[0].Elements[1].Text != "Human change" {
		t.Fatal("lost unrelated human edit or retained conflicting selection")
	}
	delta := a.changesSince(baseline)
	events := delta["events"].([]editorChange)
	if len(events) != 3 || events[0].Origin != "human" || events[2].Origin != "mcp" {
		t.Fatalf("missing changes: %#v", delta)
	}
	if len(delta["objects"].([]map[string]any)) != 2 {
		t.Fatal("delta omitted current object values")
	}
	// A human draft based on before MCP cannot overwrite the MCP result.
	first.Text = "Stale human draft"
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", ObjectID: "a", Slide: 0, SceneRevision: &baseline, NotesSessionID: state.NotesSessionID, TextRuns: []sceneRun{{Text: first.Text}}}); err == nil {
		t.Fatal("stale conflicting human edit accepted")
	}
	// A draft of the independent object may still commit against that revision.
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", ObjectID: "b", Slide: 0, SceneRevision: &state.Version, NotesSessionID: state.NotesSessionID, TextRuns: []sceneRun{{Text: "Independent draft"}}}); err != nil {
		t.Fatal(err)
	}
}
func TestLiveMCPConcurrentCommands(t *testing.T) {
	s, a := liveMCPFixture(t)
	baseline := s.version
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			raw, _ := json.Marshal(map[string]any{"action": "set-scene-text", "objectId": "a", "textRuns": []map[string]any{{"text": fmt.Sprint("MCP ", i)}}})
			if err := a.command(context.Background(), raw, baseline); err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			state := s.state()
			if err := s.apply(nativeEditorAction{Action: "set-scene-text", ObjectID: "b", Slide: 0, SceneRevision: &state.Version, NotesSessionID: state.NotesSessionID, TextRuns: []sceneRun{{Text: fmt.Sprint("Human ", i)}}}); err != nil {
				failures <- err
				return
			}
		}
	}()
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if s.deck.Slides[0].Elements[0].Text != "MCP 19" || s.deck.Slides[0].Elements[1].Text != "Human 19" {
		t.Fatal("lost a concurrent edit")
	}
}

func TestLiveMCPStableTargetsAndHistoryGap(t *testing.T) {
	s, a := liveMCPFixture(t)
	baseline := s.version
	// A human inserts ahead of the target: old slide/element indices still refer
	// to the IDs in the MCP baseline, never whichever object now occupies them.
	if err := s.apply(nativeEditorAction{Action: "reorder-slide", Slide: 0, Value: 1}); err != nil {
		t.Fatal(err)
	}
	liveCommand(t, a, baseline, map[string]any{"action": "set-scene-text", "slide": 0, "objectId": "a", "textRuns": []map[string]any{{"text": "Stable target"}}})
	if s.deck.Slides[1].Elements[0].Text != "Stable target" || s.current != 1 {
		t.Fatal("reorder lost target or moved human view")
	}
	for i := 0; i < editorChangeHistoryLimit+2; i++ {
		if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
			t.Fatal(err)
		}
	}
	if a.changesSince(baseline)["reset_required"] != true {
		t.Fatal("evicted journal baseline needs explicit resync")
	}
}

func TestLiveMCPSaveRequestsDoNotWriteUntilNativeSave(t *testing.T) {
	s, a := liveMCPFixture(t)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _, _, err := a.requestLiveSave(ctx); done <- err }()
	var request *editorSaveRequest
	for request == nil {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
		s.mu.RLock()
		request = s.collaboration.Save
		s.mu.RUnlock()
		time.Sleep(time.Millisecond)
	}
	if _, err := os.Stat(s.deckPath); !os.IsNotExist(err) {
		t.Fatal("request wrote file before app handled Save")
	}
	state := s.state()
	if state.Collaboration.Save.ID != request.ID {
		t.Fatal("save request missing from editor state")
	}
	mux := http.NewServeMux()
	p := &presenterCompanion{}
	configureLiveEditorMCP(mux, s, p)
	body := fmt.Sprintf(`{"id":%q,"owner":%q}`, request.ID, request.Owner)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/api/editor/mcp-save-result", strings.NewReader(body)))
	if response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLiveMCPProxyFollowsDocumentAndProtectsTarget(t *testing.T) {
	makeEditor := func(text string) (*nativeEditorSession, *httptest.Server) {
		s, _ := liveMCPFixture(t)
		s.deck.Slides[0].Elements[0].Text = text
		p := &presenterCompanion{token: "editor-secret"}
		mux := http.NewServeMux()
		configureLiveEditorMCP(mux, s, p)
		server := httptest.NewServer(p.authorized(mux))
		t.Cleanup(server.Close)
		t.Cleanup(func() {
			if p.automationCleanup != nil {
				p.automationCleanup()
			}
		})
		return s, server
	}
	first, one := makeEditor("First document")
	second, two := makeEditor("Second document")
	p := &presenterCompanion{token: "private-management"}
	public := &automationHTTPListener{}
	mux := http.NewServeMux()
	configureLiveMCPProxy(mux, p, public)
	management := httptest.NewServer(p.authorized(mux))
	t.Cleanup(management.Close)
	t.Cleanup(p.automationCleanup)
	setting := func(target string) {
		body, _ := json.Marshal(map[string]any{"enabled": true, "target": target})
		request, _ := http.NewRequest("POST", management.URL+"/api/editor/mcp", bytes.NewReader(body))
		request.AddCookie(&http.Cookie{Name: "keynope_session", Value: p.token})
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			data, _ := io.ReadAll(response.Body)
			t.Fatal(response.StatusCode, string(data))
		}
	}
	rpc := func(name string, args any) map[string]any {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		request, _ := http.NewRequest("POST", public.url, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", "2025-06-18")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode, string(data))
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data: ")) {
				data = bytes.TrimPrefix(line, []byte("data: "))
				break
			}
		}
		var envelope map[string]any
		if err = json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err, string(data))
		}
		if envelope["error"] != nil {
			t.Fatal(envelope)
		}
		return envelope["result"].(map[string]any)
	}
	setting(one.URL + "/?token=editor-secret")
	initial := rpc("deck_inspect", map[string]any{})["structuredContent"].(map[string]any)
	version := initial["version"]
	owner := initial["document_id"]
	result := rpc("editor_command", map[string]any{"expected_version": version, "document_id": owner, "command": map[string]any{"action": "set-scene-text", "objectId": "a", "textRuns": []map[string]any{{"text": "Live MCP"}}}})
	if result["isError"] == true || first.deck.Slides[0].Elements[0].Text != "Live MCP" {
		t.Fatal(result)
	}
	endpoint := public.url
	setting(two.URL + "/?token=editor-secret")
	if endpoint != public.url {
		t.Fatal("changing document moved endpoint")
	}
	result = rpc("editor_command", map[string]any{"expected_version": version, "document_id": owner, "command": map[string]any{"action": "delete-slide"}})
	if result["isError"] != true || len(second.deck.Slides) != 2 {
		t.Fatal("old document request modified new document", result)
	}
	current := rpc("deck_inspect", map[string]any{})["structuredContent"].(map[string]any)
	if current["document_id"] == owner {
		t.Fatal("proxy retained old document")
	}
	// A public client cannot choose a new private target or expose its token.
	response, err := http.Get(strings.TrimSuffix(public.url, "/mcp") + "/api/editor/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode == 200 {
		t.Fatal("public listener exposed settings")
	}
}

func TestLiveMCPUnrelatedSlideInsertionPreservesHumanDraft(t *testing.T) {
	s, a := liveMCPFixture(t)
	state := s.state()
	liveCommand(t, a, state.Version, map[string]any{"action": "add-slide"})
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", ObjectID: "a", Slide: 0, SceneRevision: &state.Version, NotesSessionID: state.NotesSessionID, TextRuns: []sceneRun{{Text: "Human draft survives slide insertion"}}}); err != nil {
		t.Fatal(err)
	}
	if s.deck.Slides[0].Elements[0].Text != "Human draft survives slide insertion" {
		t.Fatal("human draft lost")
	}
}

func TestLiveMCPDeletedCursorNeverTargetsNeighbor(t *testing.T) {
	s, a := liveMCPFixture(t)
	version := s.version
	liveCommand(t, a, version, map[string]any{"action": "select-slide", "slide": 1})
	if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "delete-slide"}); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"action":"add-element","kind":"text"}`)
	if err := a.command(context.Background(), raw, s.version); err == nil {
		t.Fatal("deleted cursor silently targeted its neighbor")
	}
	if len(s.deck.Slides[0].Elements) != 2 {
		t.Fatal("modified wrong slide")
	}
}
