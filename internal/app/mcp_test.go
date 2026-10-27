//go:build !js && !keynope_sitegen

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func automationTest(t *testing.T) *deckAutomation {
	t.Helper()
	directory := t.TempDir()
	a, err := newDeckAutomation(newNativeEditorSession(filepath.Join(directory, "Untitled.md"), Deck{Slides: []Slide{{}}}, true), directory, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.root.Close() })
	return a
}
func TestMCPCommandCatalogCoversNativeEditor(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "native_editor.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	omitted := map[string]bool{"confirm-save": true, "upsert-font": true, "delete-font": true, "delete-library-font": true}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "apply" && fn.Name.Name != "applyMaster" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			statement, ok := node.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			selector, ok := statement.Tag.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Action" {
				return true
			}
			for _, child := range statement.Body.List {
				for _, expr := range child.(*ast.CaseClause).List {
					literal, ok := expr.(*ast.BasicLit)
					if !ok {
						continue
					}
					name, _ := strconv.Unquote(literal.Value)
					if !omitted[name] && !strings.Contains(" "+automationCommands+" ", " "+name+" ") {
						t.Errorf("native command missing from MCP catalog: %s", name)
					}
				}
			}
			return true
		})
	}
}
func TestMCPProtocolAndCommands(t *testing.T) {
	a := automationTest(t)
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server, err := a.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "Keynope test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tools, err := client.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) < 12 {
		t.Fatalf("tools: %v %v", tools, err)
	}
	resources, err := client.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 3 {
		t.Fatalf("resources: %v %v", resources, err)
	}
	call := func(name string, args any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := call("deck_inspect", map[string]any{})
	if result.IsError {
		t.Fatal(result.Content)
	}
	result = call("editor_command", map[string]any{"expected_version": 1, "command": map[string]any{"action": "add-element", "kind": "text", "elementData": map[string]any{"kind": "text", "text": "MCP-created text"}}})
	if result.IsError {
		t.Fatal(result.Content)
	}
	if a.editor.state().Slides[0].Elements[0].Text != "MCP-created text" {
		t.Fatal("native command was not applied")
	}
	result = call("editor_command", map[string]any{"expected_version": 1, "command": map[string]any{"action": "delete-selection"}})
	if !result.IsError {
		t.Fatal("accepted stale edit")
	}
	result = call("editor_command", map[string]any{"expected_version": 2, "command": map[string]any{"action": "set-selection-property", "property": "paint.blur", "propertyValue": 10, "objectIds": a.editor.state().SelectionCapabilities.TargetIDs}})
	if result.IsError {
		t.Fatal(result.Content)
	}
	result = call("deck_save", map[string]any{"path": "test.md", "expected_version": 3})
	if result.IsError {
		t.Fatal(result.Content)
	}
	raw, err := a.root.ReadFile("test.md")
	if err != nil || !strings.Contains(string(raw), "MCP-created text") || !strings.Contains(string(raw), "fx-blur=10") {
		t.Fatalf("save: %s %v", raw, err)
	}
	if a.editor.state().Dirty {
		t.Fatal("MCP save did not confirm actual persistence")
	}
	result = call("deck_save", map[string]any{"path": "test.md", "expected_version": 3})
	if !result.IsError {
		t.Fatal("overwrote without permission")
	}
	result = call("deck_schema", map[string]any{"topic": "action"})
	if result.IsError {
		t.Fatal(result.Content)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "ForType(") {
		t.Fatal(string(encoded))
	}
	result = call("deck_inspect", map[string]any{"unknown": true})
	if !result.IsError {
		t.Fatal("accepted unexpected arguments")
	}
}
func TestMCPWorkspaceBoundariesAndAtomicFiles(t *testing.T) {
	a := automationTest(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(a.directory, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside.md", filepath.Join(outside, "outside.md"), "escape/outside.md"} {
		if _, err := a.write(path, []byte("bad"), false); err == nil {
			t.Errorf("escaped workspace: %s", path)
		}
	}
	if _, err := a.write("deck.md", []byte("first"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.write("deck.md", []byte("second"), false); err == nil {
		t.Fatal("unapproved overwrite")
	}
	raw, _ := a.root.ReadFile("deck.md")
	if string(raw) != "first" {
		t.Fatal("failed write modified old file")
	}
	if _, err := a.write("deck.md", []byte("second"), true); err != nil {
		t.Fatal(err)
	}
}

func TestMCPVideoImport(t *testing.T) {
	a := automationTest(t)
	data, _, _, _ := videoFixture(t)
	if _, err := a.write("clip.mp4", data, false); err != nil {
		t.Fatal(err)
	}
	version := a.editor.state().Version
	if _, _, err := a.call(context.Background(), "video_import", automationArguments{Path: "clip.mp4", ExpectedVersion: &version}); err != nil {
		t.Fatal(err)
	}
	state := a.editor.state()
	if len(state.Slides[0].Elements) != 1 || state.Slides[0].Elements[0].Kind != "video" || len(a.editor.deck.Assets) != 1 {
		t.Fatal("MCP video import did not create an embedded, editable object")
	}
	if _, _, err := a.call(context.Background(), "video_import", automationArguments{Path: "clip.mp4", ExpectedVersion: &version}); err == nil {
		t.Fatal("MCP accepted stale video import")
	}
}

func TestMCPSchemasAndCatalogues(t *testing.T) {
	a := automationTest(t)
	for _, topic := range []string{"action", "element", "slide", "activity", "activities", "table", "chart", "theme", "build", "properties", "templates", "shapes", "fonts"} {
		data, err := json.Marshal(a.schema(topic))
		if err != nil || strings.Contains(string(data), `"error":`) {
			t.Errorf("%s schema: %s %v", topic, data, err)
		}
	}
	catalogue := a.schema("activities").(map[string]any)["activities"].([]map[string]string)
	if len(catalogue) < 25 {
		t.Fatalf("incomplete activity catalogue: %v", catalogue)
	}
	result, _, err := a.call(context.Background(), "emoji_catalog", automationArguments{Limit: 100})
	if err != nil || len(result["items"].([]map[string]string)) != 100 || result["more"] != true {
		t.Fatalf("emoji pagination: %v %v", result, err)
	}
}

func TestMCPBatchFailureAndLockedRevision(t *testing.T) {
	a := automationTest(t)
	version := a.editor.state().Version
	_, _, err := a.call(context.Background(), "editor_batch", automationArguments{ExpectedVersion: &version, Commands: []json.RawMessage{
		json.RawMessage(`{"action":"add-element","kind":"text","elementData":{"kind":"text","text":"Kept after batch failure"}}`),
		json.RawMessage(`{"action":"not-a-command"}`),
	}})
	if err == nil || !strings.Contains(err.Error(), "after 1 applied commands") {
		t.Fatalf("batch did not report partial application: %v", err)
	}
	state := a.editor.state()
	if len(state.Slides[0].Elements) != 1 || !state.Dirty {
		t.Fatal("batch failure lost the successful edit")
	}
	// Stamping under the native write lock also rejects commands if a human
	// changed the deck after MCP's preflight version check.
	err = a.editor.apply(nativeEditorAction{Action: "delete-slide", NotesSessionID: state.NotesSessionID, automationRevision: &version})
	if err == nil {
		t.Fatal("native mutation accepted a stale private revision")
	}
	raw := json.RawMessage(`{"action":"undo"}`)
	if err = a.command(context.Background(), raw, state.Version); err != nil {
		t.Fatal(err)
	}
	if len(a.editor.state().Slides[0].Elements) != 0 {
		t.Fatal("successful batch prefix was not undoable")
	}
}

func TestMCPMasterEditing(t *testing.T) {
	a := automationTest(t)
	command := func(raw string) {
		t.Helper()
		if err := a.command(context.Background(), json.RawMessage(raw), a.editor.state().Version); err != nil {
			t.Fatal(err)
		}
	}
	command(`{"action":"add-layout","name":"MCP test layout"}`)
	command(`{"action":"toggle-master-mode"}`)
	index := len(a.editor.deck.Masters.Layouts)
	command(fmt.Sprintf(`{"action":"select-slide","slide":%d}`, index))
	command(`{"action":"add-element","kind":"text","elementData":{"kind":"text","text":"Master through MCP"}}`)
	ids, _ := json.Marshal(a.editor.state().SelectionCapabilities.TargetIDs)
	command(fmt.Sprintf(`{"action":"set-selection-property","objectIds":%s,"property":"paint.blur","propertyValue":2}`, ids))
	result, _, err := a.call(context.Background(), "slide_inspect", automationArguments{Master: true, Slide: index})
	if err != nil || result["scene"] == nil {
		t.Fatalf("master inspection: %v %v", result, err)
	}
	command(`{"action":"toggle-master-mode"}`)
	if len(a.editor.deck.Slides[0].Elements) != 0 || a.editor.deck.Masters.Layouts[index-1].Slide.Elements[0].Text != "Master through MCP" {
		t.Fatal("master edits leaked into the authored slide")
	}
}

func TestMCPHTTPGate(t *testing.T) {
	a := automationTest(t)
	// A real open document must never become the automation document.
	a.editor.deck.Slides[0].Elements = []Element{{Kind: "text", Text: "PRIVATE OPEN DOCUMENT"}}
	p := &presenterCompanion{url: "http://127.0.0.1:12456/?token=test", token: "private-token"}
	mux := http.NewServeMux()
	configureAutomationMCP(mux, t.TempDir(), p, nil)
	t.Cleanup(p.automationCleanup)
	var response []byte
	method := http.MethodPost
	send := func(path, body, host, authorization, origin string) int {
		r := httptest.NewRequest(method, "http://127.0.0.1:12456"+path, strings.NewReader(body))
		r.Host = host
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		w := httptest.NewRecorder()
		// Management stays private; MCP itself needs no cookie or auth header.
		if path != "/mcp" {
			r.AddCookie(&http.Cookie{Name: "keynope_session", Value: p.token})
		}
		p.authorized(mux).ServeHTTP(w, r)
		response = append([]byte(nil), w.Body.Bytes()...)
		return w.Code
	}
	if got := send("/mcp", `{}`, "127.0.0.1:12456", "Bearer private-token", ""); got != 403 {
		t.Fatal(got)
	}
	method = http.MethodGet
	if got := send("/api/editor/mcp", "", "127.0.0.1:12456", "", ""); got != 200 || strings.Contains(string(response), "configuration") || !strings.Contains(string(response), `"enabled":false`) {
		t.Fatalf("reading settings must not enable MCP: %d %s", got, response)
	}
	method = http.MethodPost
	if got := send("/api/editor/mcp", `{"enabled":true}`, "127.0.0.1:12456", "", ""); got != 200 {
		t.Fatal(got)
	}
	var settings struct {
		Configuration struct {
			Servers map[string]struct {
				URL string `json:"url"`
			} `json:"mcpServers"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(response, &settings); err != nil {
		t.Fatal(err)
	}
	endpoint := settings.Configuration.Servers["keynope"].URL
	if endpoint != "http://127.0.0.1:12456/mcp" || strings.Contains(string(response), "headers") || strings.Contains(string(response), "token") {
		t.Fatal("configuration must not require or expose a token", string(response))
	}
	method = http.MethodGet
	for i := 0; i < 3; i++ {
		if got := send("/api/editor/mcp", "", "127.0.0.1:12456", "", ""); got != 200 || !strings.Contains(string(response), endpoint) {
			t.Fatalf("copy/read must not change endpoint: %d %s", got, response)
		}
	}
	method = http.MethodPost
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"gate test","version":"1"},"capabilities":{}}}`
	if got := send("/mcp", initialize, "127.0.0.1:12456", "", ""); got != 200 || !strings.Contains(string(response), "serverInfo") {
		t.Fatalf("valid MCP initialization: %d %s", got, response)
	}
	inspect := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"deck_inspect","arguments":{}}}`
	if got := send("/mcp", inspect, "127.0.0.1:12456", "", ""); got != 200 || !strings.Contains(string(response), `attached`) || strings.Contains(string(response), "PRIVATE OPEN DOCUMENT") {
		t.Fatalf("HTTP automation must own an isolated document: %d %s", got, response)
	}
	for _, row := range []struct {
		host, auth, origin string
		status             int
	}{{"127.0.0.1:12456", "", "", 200}, {"127.0.0.1:12456", "Bearer obsolete-token", "", 200}, {"127.0.0.1:12456", "", "http://127.0.0.1:12456", 200}, {"evil.example", "", "", 403}, {"127.0.0.1:12456", "", "https://evil.example", 403}, {"127.0.0.1:12456", "", "null", 403}} {
		if got := send("/mcp", initialize, row.host, row.auth, row.origin); got != row.status {
			t.Fatal(row, got)
		}
	}
	if got := send("/api/editor/mcp", `{"enabled":false}`, "127.0.0.1:12456", "", ""); got != 200 {
		t.Fatal(got)
	}
	if got := send("/mcp", inspect, "127.0.0.1:12456", "", ""); got != 403 {
		t.Fatal("disabled MCP still permits tools", got)
	}
	method = http.MethodGet
	if got := send("/api/editor/mcp", "", "127.0.0.1:12456", "", ""); got != 200 || strings.Contains(string(response), "configuration") {
		t.Fatal("copy must not enable disabled MCP", got, string(response))
	}
	method = http.MethodPost
	if got := send("/api/editor/mcp", `{"enabled":true}`, "127.0.0.1:12456", "", ""); got != 200 || !strings.Contains(string(response), endpoint) {
		t.Fatal("re-enabling must retain the same endpoint", got, string(response))
	}
	if got := send("/mcp", inspect, "127.0.0.1:12456", "", ""); got != 200 {
		t.Fatal("re-enabled MCP must accept local clients without a token", got)
	}
	// Knowing the public endpoint must not let a client enable MCP or read the
	// private app settings without the app's internal management credential.
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:12456/api/editor/mcp", strings.NewReader(`{"enabled":true}`))
	w := httptest.NewRecorder()
	p.authorized(mux).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("public MCP clients can change app settings", w.Code)
	}
	if a.editor.deck.Slides[0].Elements[0].Text != "PRIVATE OPEN DOCUMENT" {
		t.Fatal("automation modified the open document")
	}
}
