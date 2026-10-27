//go:build !js && !keynope_sitegen

package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultMCPPort = 50987

func validMCPPort(port int) bool { return port >= 1024 && port <= 65535 }

// Management stays on its private, authenticated socket. Only the public MCP
// listener moves, so changing ports never discards an unsaved automation deck.
// The management handler's lock owns these fields and calls.
type automationHTTPListener struct {
	port   int
	url    string
	server *http.Server
}

func (l *automationHTTPListener) listen(port int, handler func(string) http.Handler) error {
	if l.server != nil && l.port == port {
		return nil
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("could not listen on MCP port %d; choose another port in Automation settings: %w", port, err)
	}
	address := listener.Addr().String()
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler(address))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	old := l.server
	l.server, l.port, l.url = server, listener.Addr().(*net.TCPAddr).Port, "http://"+address+"/mcp"
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "MCP listener stopped: %v\n", err)
		}
	}()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (l *automationHTTPListener) close() {
	if l.server != nil {
		_ = l.server.Close()
	}
	l.server, l.url = nil, ""
}
