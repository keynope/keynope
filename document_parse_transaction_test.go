package main

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectedDocumentParseKeepsRuntimeDimensionsAndRegisteredPixels(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
	w, h, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = w, h, order })
	id := "failed-document-asset"
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, A: 255})}
	registerEmbeddedStillAsset(id, asset)
	t.Cleanup(func() {
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	identity := embeddedImageCacheIdentity("keynope-asset:" + id + ".png")
	revision := embeddedImageRegistrationRevision.Load()
	asset.Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	envelope, err := encodeDeckAssets(map[string]DeckAsset{id: asset})
	if err != nil {
		t.Fatal(err)
	}
	authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
	bad := "<!-- keynope width=100 height=80 -->\n\n" + envelope + "\n\n<!-- element-id=bad-table object-id=missing -->\n[table]\n"
	if _, err := parseDeckData("rejected.md", []byte(bad)); err == nil {
		t.Fatal("missing table dependency accepted")
	}
	if authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged {
		t.Errorf("failed parse changed active geometry/order: %d %d %v", authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged)
	}
	if embeddedImageCacheIdentity("keynope-asset:"+id+".png") != identity || embeddedImageRegistrationRevision.Load() != revision {
		t.Error("rejected document replaced registered source pixels")
	}
	if _, err := os.Stat(embeddedAssetPath(id, asset)); !os.IsNotExist(err) {
		t.Errorf("rejected document materialized file: %v", err)
	}
	if _, err := parseDeck(filepath.Join(t.TempDir(), "missing.md")); err == nil || !parsedDeckElementOrderChanged {
		t.Error("failed file read changed active ordering state")
	}
	empty := "<!-- keynope width=100 height=80 -->\n\n" + envelope + "\n"
	if _, err := parseEditorDeckData("empty.md", []byte(empty)); err == nil {
		t.Fatal("editor accepted no-slide candidate")
	}
	if authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged || embeddedImageCacheIdentity("keynope-asset:"+id+".png") != identity || embeddedImageRegistrationRevision.Load() != revision {
		t.Fatal("caller-rejected candidate published runtime state")
	}
	if _, err := os.Stat(embeddedAssetPath(id, asset)); !os.IsNotExist(err) {
		t.Fatal("caller-rejected candidate materialized pixels", err)
	}
	valid := "<!-- keynope width=100 height=80 -->\n\n" + envelope + "\n\n# Accepted\n"
	if _, err := parseDeckData("accepted.md", []byte(valid)); err != nil {
		t.Fatal(err)
	}
	if authoredTerminalWidth != 100 || authoredTerminalHeight != 80 || embeddedImageCacheIdentity("keynope-asset:"+id+".png") == identity {
		t.Fatal("successful parse did not publish new geometry/pixels")
	}
	if _, err := os.Stat(embeddedAssetPath(id, asset)); err != nil {
		t.Fatal("successful asset not materialized", err)
	}
}
