package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"reflect"
	"strings"
	"testing"
)

func ownedWebPFixture(t *testing.T) []byte {
	t.Helper()
	// Authored 2x2 red/green/blue/white pixels, encoded as lossless WebP.
	data, err := base64.StdEncoding.DecodeString("UklGRiwAAABXRUJQVlA4TB8AAAAvAUAAAB8gEEjeHzqN+RcQFPwf3fxHZA/gBgwR/Q8BAA==")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func importedWebPFixture(t *testing.T) (string, DeckAsset) {
	t.Helper()
	id, asset, _, err := embeddedImageAsset(ownedWebPFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return id, asset
}

func TestWebPImportPreservesCorrectSourceType(t *testing.T) {
	id, asset := importedWebPFixture(t)
	if asset.MIME != "image/png" || asset.Source == nil || asset.Source.MIME != "image/webp" ||
		asset.Source.Width != 2 || asset.Source.Height != 2 || !bytes.Equal(asset.Source.Data, ownedWebPFixture(t)) {
		t.Fatal("WebP source was relabelled or changed")
	}
	encoded, err := encodeDeckAssets(map[string]DeckAsset{id: asset})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := decodeDeckAssets(encoded)
	if err != nil || !reflect.DeepEqual(loaded[id], asset) {
		t.Fatal("fresh WebP import failed strict archive round trip", err)
	}
}

func TestLegacyWebPSourceArchiveRecovery(t *testing.T) {
	id, asset := importedWebPFixture(t)
	asset.Extra = extensionFixture("futureAsset", `{"x":1}`)
	asset.Source.Extra = extensionFixture("futureSource", `{"x":2}`)
	expected := cloneDeckAsset(asset)
	asset.Source.MIME = "image/png"
	// Only archive reading migrates legacy metadata; clipboard admission and
	// new archive writing must still reject incorrectly labelled media.
	if err := validateClipboardAsset(asset); err == nil {
		t.Fatal("clipboard accepted mismatched MIME")
	}
	if _, err := encodeDeckAssets(map[string]DeckAsset{id: asset}); err == nil {
		t.Fatal("writer accepted mismatched MIME")
	}
	raw, err := json.Marshal(map[string]DeckAsset{id: asset})
	if err != nil {
		t.Fatal(err)
	}
	loaded, remaining, err := decodeDeckAssets(assetArchiveEnvelope(t, raw) + "\n# Slide")
	if err != nil || strings.TrimSpace(remaining) != "# Slide" || !reflect.DeepEqual(loaded[id], expected) {
		t.Fatalf("legacy source migration changed identity, pixels or metadata: err=%v remaining=%q equal=%v", err, remaining, reflect.DeepEqual(loaded[id], expected))
	}
	encoded, err := encodeDeckAssets(loaded)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _, err := decodeDeckAssets(encoded)
	if err != nil || !reflect.DeepEqual(loaded, reopened) {
		t.Fatal("repaired metadata did not persist on serialization", err)
	}
}

func TestLegacyWebPRecoveryDoesNotWeakenAdmission(t *testing.T) {
	_, original := importedWebPFixture(t)
	for name, mutate := range map[string]func(*DeckAsset){
		"wrong-source-dimensions": func(a *DeckAsset) { a.Source.Width++ },
		"wrong-source-type":       func(a *DeckAsset) { a.Source.MIME = "image/jpeg" },
		"wrong-derivative-type":   func(a *DeckAsset) { a.MIME = "image/jpeg" },
		"corrupt-derivative":      func(a *DeckAsset) { a.Data = a.Data[:16] },
		"truncated-source": func(a *DeckAsset) {
			a.Source.Data = a.Source.Data[:len(a.Source.Data)-8]
			if _, format, err := image.DecodeConfig(bytes.NewReader(a.Source.Data)); err != nil || format != "webp" {
				t.Fatal("fixture must retain its valid WebP header", err)
			}
		},
		"source-frames": func(a *DeckAsset) {
			a.Source.Frames = []DeckAssetFrame{{Data: a.Data, DelayMS: 100}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			asset := cloneDeckAsset(original)
			asset.Source.MIME = "image/png"
			mutate(&asset)
			raw, err := json.Marshal(map[string]DeckAsset{"legacy": asset})
			if err != nil {
				t.Fatal(err)
			}
			text := assetArchiveEnvelope(t, raw)
			if _, remaining, err := decodeDeckAssets(text); err == nil || remaining != text {
				t.Fatal("invalid archive accepted or consumed", err)
			}
		})
	}
}

// Optional read-only acceptance check against a locally supplied legacy deck.
// No customer content or machine-specific path is part of the fixture.
func TestLocalLegacyWebPDeck(t *testing.T) {
	path := os.Getenv("KEYNOPE_LEGACY_WEBP_DECK")
	if path == "" {
		t.Skip("set KEYNOPE_LEGACY_WEBP_DECK for read-only local acceptance")
	}
	width, height, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = width, height, order
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeckDataWithAssetPublication(path, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) == 0 || len(deck.Assets) == 0 {
		t.Fatal("empty local deck")
	}
	encoded, err := serializeDeck(path, deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckDataWithAssetPublication(path, encoded, false)
	if err != nil || len(reopened.Slides) != len(deck.Slides) || !reflect.DeepEqual(reopened.Assets, deck.Assets) {
		t.Fatal("local deck could not round trip in memory", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("local source file changed", err)
	}
	t.Logf("opened and round-tripped %d slides and %d assets; original file unchanged", len(deck.Slides), len(deck.Assets))
}
