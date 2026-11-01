//go:build !js && !keynope_sitegen

package app

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPQRCodeCreator(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	a := automationTest(t)
	st, ct := mcp.NewInMemoryTransports()
	server, err := a.server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "qr-test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	version := a.editor.version
	r, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "qr_code_create", Arguments: map[string]any{"qr_code": map[string]any{"content": "https://keynope.sh/", "correction": "H"}, "expected_version": version}})
	if err != nil || r.IsError || len(a.editor.deck.Slides[0].Elements) != 1 {
		t.Fatal(r, err)
	}
	if len(a.editor.deck.Assets) != 1 || a.editor.deck.Slides[0].Elements[0].Kind != "image" {
		t.Fatal("not a native image")
	}
	guide := authoringCall(t, a, "deck_authoring_guide", automationArguments{Topic: "artwork"})
	if !strings.Contains(guide["guide"].(string), "qr_code_create") {
		t.Fatal("missing built-in instructions")
	}
	if _, _, err := a.call(context.Background(), "qr_code_create", automationArguments{QRCode: &QRCodeOptions{Content: "no revision"}}); err == nil {
		t.Fatal("accepted unversioned write")
	}
	authoringCommand(t, a, map[string]any{"action": "toggle-master-mode"})
	masterCount := len(a.editor.deck.Masters.Base.Slide.Elements)
	raw := authoringCommand(t, a, map[string]any{"action": "insert-qr-code", "qrCode": map[string]any{"content": "Master code"}})
	if len(raw["created"].([]automationObjectStamp)) != 1 {
		t.Fatal("missing master receipt")
	}
	if len(a.editor.deck.Masters.Base.Slide.Elements) != masterCount+1 {
		t.Fatal("master insertion failed")
	}
}
