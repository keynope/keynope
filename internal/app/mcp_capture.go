//go:build !js && !keynope_sitegen

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func captureAutomation(ctx context.Context, html string, page, width int, format string) ([]byte, error) {
	renderer := os.Getenv("KEYNOPE_RENDERER")
	if renderer == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		renderer = filepath.Clean(filepath.Join(filepath.Dir(executable), "../MacOS/Keynope"))
	}
	if info, err := os.Stat(renderer); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("native screenshot renderer unavailable; run the bundled Keynope.app engine, or set KEYNOPE_RENDERER to its Contents/MacOS/Keynope executable")
	}
	if len(html) > 128<<20 {
		return nil, errors.New("render document exceeds 128 MiB")
	}
	var request bytes.Buffer
	encoder := json.NewEncoder(&request)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{"html": html, "page": page, "width": width, "format": format}); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, renderer, "--mcp-render")
	var diagnostic bytes.Buffer
	output := &automationCaptureBuffer{}
	command.Stdin = &request
	command.Stdout = output
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("render slide: %w (%s)", err, diagnostic.String())
	}
	data := output.Bytes()
	if format == "png" && !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) || format == "pdf" && !bytes.HasPrefix(data, []byte("%PDF")) {
		return nil, errors.New("renderer returned an invalid image/document")
	}
	return data, nil
}

type automationCaptureBuffer struct{ bytes.Buffer }

func (b *automationCaptureBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 32<<20 {
		return 0, errors.New("rendered output exceeds 32 MiB")
	}
	return b.Buffer.Write(p)
}
