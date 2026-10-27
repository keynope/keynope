//go:build !js && !keynope_sitegen

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMCPPortConfiguration(t *testing.T) {
	if defaultMCPPort != 50987 {
		t.Fatal("unexpected default MCP port")
	}
	native, err := os.ReadFile(testRepositoryPath("presenter/Automation.swift"))
	if err != nil || !strings.Contains(string(native), fmt.Sprintf("static let defaultPort = %d", defaultMCPPort)) {
		t.Fatal("Mac settings and engine default ports disagree", err)
	}
	for _, port := range []int{-1, 0, 80, 1023, 65536} {
		if validMCPPort(port) {
			t.Fatal("invalid port admitted", port)
		}
		if err := runMCPHTTP([]string{"--workspace", t.TempDir(), "--port", fmt.Sprint(port)}); err == nil {
			t.Fatal("invalid helper port accepted", port)
		}
	}
	for _, port := range []int{1024, defaultMCPPort, 65535} {
		if !validMCPPort(port) {
			t.Fatal("valid port rejected", port)
		}
	}
}

func TestMCPPortChangesPreserveAutomationDeck(t *testing.T) {
	p := &presenterCompanion{token: "private-management"}
	// An OS-selected test port avoids interfering with a user's running app.
	public := &automationHTTPListener{}
	mux := http.NewServeMux()
	configureAutomationMCP(mux, t.TempDir(), p, public)
	t.Cleanup(p.automationCleanup)
	management := httptest.NewServer(p.authorized(mux))
	t.Cleanup(management.Close)
	p.url = management.URL + "/"
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	settings := func(body string) (int, map[string]any) {
		t.Helper()
		method := http.MethodGet
		if body != "" {
			method = http.MethodPost
		}
		request, _ := http.NewRequest(method, management.URL+"/api/editor/mcp", strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: "keynope_session", Value: p.token})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value map[string]any
		if response.StatusCode == 200 {
			if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode, value
	}
	endpoint := func(value map[string]any) string {
		t.Helper()
		return value["configuration"].(map[string]any)["mcpServers"].(map[string]any)["keynope"].(map[string]any)["url"].(string)
	}
	rpc := func(address, method string, params map[string]any, host, origin string) (int, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		request, _ := http.NewRequest(http.MethodPost, address, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", "2025-06-18")
		if host != "" {
			request.Host = host
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		var value map[string]any
		if response.StatusCode == 200 {
			for _, line := range bytes.Split(data, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data: ")) {
					data = bytes.TrimPrefix(line, []byte("data: "))
					break
				}
			}
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if value["error"] != nil {
				t.Fatal(value)
			}
		}
		return response.StatusCode, value
	}
	inspect := func(address string) map[string]any {
		t.Helper()
		code, value := rpc(address, "tools/call", map[string]any{"name": "deck_inspect", "arguments": map[string]any{}}, "", "")
		if code != 200 {
			t.Fatal(code)
		}
		return value["result"].(map[string]any)["structuredContent"].(map[string]any)
	}
	code, state := settings(`{"enabled":true}`)
	if code != 200 || state["defaultPort"] != float64(defaultMCPPort) {
		t.Fatal(code, state)
	}
	first := endpoint(state)
	if !strings.HasPrefix(first, "http://127.0.0.1:") {
		t.Fatal("non-loopback endpoint", first)
	}
	initial := inspect(first)
	code, added := rpc(first, "tools/call", map[string]any{"name": "editor_command", "arguments": map[string]any{"expected_version": initial["version"], "command": map[string]any{"action": "add-element", "kind": "text", "elementData": map[string]any{"kind": "text", "text": "Keep this unsaved automation deck"}}}}, "", "")
	if code != 200 || added["result"].(map[string]any)["isError"] == true {
		t.Fatal(code, added)
	}
	before := inspect(first)
	if before["dirty"] != true {
		t.Fatal("fixture is not an unsaved deck")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { occupied.Close() })
	port := occupied.Addr().(*net.TCPAddr).Port
	if code, _ := settings(fmt.Sprintf(`{"port":%d}`, port)); code != http.StatusConflict {
		t.Fatal("port conflict was not reported", code)
	}
	_, unchanged := settings("")
	if endpoint(unchanged) != first || !reflect.DeepEqual(inspect(first), before) {
		t.Fatal("failed port change lost endpoint or deck")
	}
	for _, invalid := range []string{"0", "1023", "65536", "50987.5", `"not a port"`} {
		if code, _ := settings(`{"port":` + invalid + `}`); code != 400 {
			t.Fatal("invalid port accepted", invalid, code)
		}
	}
	_ = occupied.Close()
	code, state = settings(fmt.Sprintf(`{"port":%d}`, port))
	if code != 200 || state["enabled"] != true {
		t.Fatal(code, state)
	}
	next := endpoint(state)
	if next != fmt.Sprintf("http://127.0.0.1:%d/mcp", port) || next == first || !reflect.DeepEqual(inspect(next), before) {
		t.Fatal("live port change lost the unsaved deck or URL")
	}
	for _, address := range []string{"http://example.com", "null"} {
		if code, _ := rpc(next, "tools/list", nil, "", address); code != 403 {
			t.Fatal("foreign origin accepted", code)
		}
	}
	if code, _ := rpc(next, "tools/list", nil, "evil.example", ""); code != 403 {
		t.Fatal("foreign host accepted", code)
	}
	response, err := client.Get(strings.TrimSuffix(next, "/mcp") + "/api/editor/mcp")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("public listener exposed management", response.StatusCode)
	}
	if code, _ := settings(`{"enabled":false}`); code != 200 {
		t.Fatal(code)
	}
	released, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal("disabling MCP did not release port", err)
	}
	_ = released.Close()
	code, state = settings(`{"enabled":true}`)
	if code != 200 || endpoint(state) != next || !reflect.DeepEqual(inspect(next), before) {
		t.Fatal("re-enable changed endpoint or unsaved deck")
	}
}
