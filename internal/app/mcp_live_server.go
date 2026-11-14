//go:build !js && !keynope_sitegen

package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The editor engine owns the actual MCP server. The app-level process only
// forwards requests, keeping its public URL stable across File/Open and New.
func configureLiveEditorMCP(mux *http.ServeMux, s *nativeEditorSession, p *presenterCompanion) {
	var once sync.Once
	var automation *deckAutomation
	var initErr error
	mux.Handle("/api/editor/live-mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		once.Do(func() {
			var directory string
			directory, initErr = os.MkdirTemp("", "keynope-live-assets-")
			if initErr != nil {
				return
			}
			automation, initErr = newDeckAutomation(s, directory, true)
			if initErr == nil {
				s.enableCollaboration()
				s.mu.RLock()
				v := s.captureMCPViewLocked()
				s.mu.RUnlock()
				automation.liveView = &v
			}
			previous := p.automationCleanup
			p.automationCleanup = func() {
				if previous != nil {
					previous()
				}
				if automation != nil {
					automation.root.Close()
				}
				os.RemoveAll(directory)
			}
		})
		if initErr != nil {
			return nil
		}
		return automation.server
	}, &mcp.StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: 24 << 20}))
	mux.HandleFunc("/api/editor/mcp-save-result", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var result struct{ ID, Owner, Error string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&result) != nil {
			http.Error(w, "invalid result", 400)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.collaboration == nil || s.collaboration.Save == nil || s.collaboration.Save.ID != result.ID || s.collaboration.Save.Owner != result.Owner || s.notesSessionID != result.Owner {
			http.Error(w, "save request expired", 409)
			return
		}
		var err error
		if result.Error != "" {
			err = errors.New(result.Error)
		}
		select {
		case s.collaboration.Save.done <- err:
		default:
		}
		w.WriteHeader(204)
	})
}

// Private app-management API: target credentials never come from MCP clients
// and never appear in tool results or the public server's configuration.
func configureLiveMCPProxy(mux *http.ServeMux, p *presenterCompanion, public *automationHTTPListener) {
	var mu sync.RWMutex
	enabled := false
	var target *url.URL
	mux.HandleFunc("/api/editor/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			var request struct {
				Enabled *bool   `json:"enabled"`
				Port    *int    `json:"port"`
				Target  *string `json:"target"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request) != nil {
				http.Error(w, "invalid settings", 400)
				return
			}
			next := enabled
			if request.Enabled != nil {
				next = *request.Enabled
			}
			port := public.port
			if request.Port != nil {
				port = *request.Port
			}
			if port != 0 && !validMCPPort(port) {
				http.Error(w, "invalid port", 400)
				return
			}
			var nextTarget = target
			if request.Target != nil {
				nextTarget = nil
				if *request.Target != "" {
					u, err := url.Parse(*request.Target)
					if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/" || u.Query().Get("token") == "" || u.User != nil {
						http.Error(w, "invalid editor target", 400)
						return
					}
					nextTarget = u
				}
			}
			if next {
				if err := public.listen(port, func(host string) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Host != host || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host {
							http.Error(w, "forbidden origin or host", 403)
							return
						}
						mu.RLock()
						on, u := enabled, target
						if u != nil {
							copy := *u
							u = &copy
						}
						mu.RUnlock()
						if !on {
							http.Error(w, "MCP disabled", 403)
							return
						}
						if u == nil {
							http.Error(w, "Open a presentation in Keynope first", 503)
							return
						}
						token := u.Query().Get("token")
						u.Path = "/api/editor/live-mcp"
						u.RawQuery = ""
						proxy := httputil.NewSingleHostReverseProxy(u)
						director := proxy.Director
						proxy.Director = func(req *http.Request) {
							director(req)
							req.URL.Path = u.Path
							req.Host = u.Host
							req.Header.Del("Origin")
							req.Header.Del("Authorization")
							req.Header.Set("Cookie", "keynope_session="+token)
						}
						proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
							http.Error(w, "The active presentation changed; reconnect and inspect the current deck", 503)
						}
						proxy.ServeHTTP(w, r)
					})
				}); err != nil {
					http.Error(w, err.Error(), 409)
					return
				}
			} else {
				public.close()
				public.port = port
			}
			target = nextTarget
			enabled = next
		}
		response := map[string]any{"enabled": enabled, "port": public.port, "defaultPort": defaultMCPPort, "live": true, "attached": target != nil}
		if enabled {
			response["configuration"] = map[string]any{"mcpServers": map[string]any{"keynope": map[string]any{"type": "http", "url": public.url}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	p.automationCleanup = func() { mu.Lock(); enabled = false; target = nil; mu.Unlock(); public.close() }
}

func liveMCPReadPath(path string) bool {
	// Commands acquire their own operation lock. Render/data reads see only the
	// restored human view, never the MCP command's temporary targeting context.
	return strings.HasPrefix(path, "/api/editor/") && path != "/api/editor/live-mcp" && path != "/api/editor/action" && path != "/api/editor/upload" && path != "/api/editor/upload-video" && path != "/api/editor/paste-clipboard" && path != "/api/editor/state" && path != "/api/editor/mcp-save-result" && path != "/api/editor/font-export"
}
