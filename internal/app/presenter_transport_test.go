//go:build darwin

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the actual Swift endpoint builder and Foundation cookie handling without
// opening a window or touching the user's application, files or cookie store.
func TestNativeSaveAfterReopeningSameDeck(t *testing.T) {
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("Swift compiler is unavailable")
	}
	useTestAuthoredSize(t, 245, 56)
	root := t.TempDir()
	path := filepath.Join(root, "Reopened.md")
	original := []byte("# Original title\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	first := newNativeEditorSession(path, cloneDeck(deck))
	reopened := newNativeEditorSession(path, cloneDeck(deck))
	changed := reopened.deck.Slides[0].Elements[0]
	changed.Text = "Edited after reopening"
	if err := reopened.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &changed}); err != nil {
		t.Fatal(err)
	}
	serve := func(session *nativeEditorSession, token string) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/editor/state", session.handleState)
		mux.HandleFunc("/api/editor/document", session.handleDocument)
		mux.HandleFunc("/api/editor/action", session.handleAction)
		mux.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(r.URL.Query())
		})
		server := httptest.NewServer((&presenterCompanion{token: token}).authorized(mux))
		t.Cleanup(server.Close)
		return server
	}
	oldToken, newToken := "previous-session", "current+session&token="
	old, next := serve(first, oldToken), serve(reopened, newToken)
	source, err := os.ReadFile(testRepositoryPath("presenter/KeynopePresenter.swift"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(source), "    private func editorEndpoint(_ path: String) -> URL? {")
	if start < 0 {
		t.Fatal("native endpoint builder missing")
	}
	end := strings.Index(string(source[start:]), "\n    @objc private func saveDeck()")
	if end < 0 {
		t.Fatal("native endpoint builder boundary missing")
	}
	fixture, err := os.ReadFile("testdata/editor_transport.swift")
	if err != nil {
		t.Fatal(err)
	}
	fixture = []byte(strings.Replace(string(fixture), "    // PRODUCTION_ENDPOINT_BUILDER", string(source[start:start+end]), 1))
	swiftPath, binary := filepath.Join(root, "Transport.swift"), filepath.Join(root, "transport-test")
	if err := os.WriteFile(swiftPath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	compile := exec.CommandContext(t.Context(), swiftc, "-parse-as-library", "-module-cache-path", filepath.Join(root, "modules"), swiftPath, "-o", binary)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile transport regression: %v\n%s", err, output)
	}
	oldURL := old.URL + "/?token=" + url.QueryEscape(oldToken)
	newURL := next.URL + "/?token=" + url.QueryEscape(newToken) + "&keynopeSurface=app"
	run := exec.CommandContext(t.Context(), binary, oldURL, newURL, path)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("native reopen/save regression: %v\n%s", err, output)
	}
	loaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slides[0].Elements[0].Text != changed.Text || reopened.state().Dirty {
		t.Fatal("native save did not persist and confirm the reopened deck")
	}
}
