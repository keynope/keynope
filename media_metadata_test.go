package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func metadataMediaFixture(t *testing.T) DeckAsset {
	t.Helper()
	asset := clipboardTestAsset(t)
	extra := func() *jsonExtensions {
		return extensionFixture("futurePrivate", `{"target":"unseen","payload":{"x":1,"x":2}}`)
	}
	asset.Extra = extra()
	asset.Frames = []DeckAssetFrame{{Data: asset.Data, DelayMS: 50, Extra: extra()}}
	asset.Source = &DeckAssetSource{MIME: asset.MIME, Width: asset.Width, Height: asset.Height, Data: asset.Data, Frames: []DeckAssetFrame{{Data: asset.Data, DelayMS: 50, Extra: extra()}}, Extra: extra()}
	return asset
}

func TestMediaMetadataOwnershipRoundTripAndAudience(t *testing.T) {
	asset := metadataMediaFixture(t)
	if err := validateClipboardAsset(asset); err != nil {
		t.Fatal(err)
	}
	deck := Deck{Assets: map[string]DeckAsset{"photo": asset, "unseen": clipboardTestAsset(t)}, Slides: []Slide{{Elements: []Element{{Kind: "image", ID: "photo-element", AssetID: "photo"}}}}}
	encoded, err := serializeDeck("media.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the owned parser candidate unpublished: test codecs, not cache files.
	loaded, err := parseDeckDataWithAssetPublication("media.md", encoded, false)
	if err != nil || !reflect.DeepEqual(loaded.Assets["photo"], asset) || len(loaded.Assets) != 2 {
		t.Fatal("media metadata/dependencies lost on save/reopen", err)
	}
	for _, copyPixels := range []bool{false, true} {
		copy := cloneDeckAssetContent(asset, copyPixels)
		for _, extra := range []*jsonExtensions{copy.Extra, copy.Source.Extra, copy.Source.Frames[0].Extra, copy.Frames[0].Extra} {
			(*extra)["futurePrivate"][0] = '['
		}
		for _, extra := range []*jsonExtensions{asset.Extra, asset.Source.Extra, asset.Source.Frames[0].Extra, asset.Frames[0].Extra} {
			if (*extra)["futurePrivate"][0] != '{' {
				t.Fatal("metadata snapshot aliases source")
			}
		}
		if (reflect.ValueOf(copy.Data).Pointer() != reflect.ValueOf(asset.Data).Pointer()) != copyPixels {
			t.Fatal("pixel ownership policy changed", copyPixels)
		}
	}
	plain := cloneDeckAsset(asset)
	plain.Extra = extensionFixture("futurePrivate", `{"different":true}`)
	if clipboardAssetHash(plain) == clipboardAssetHash(asset) {
		t.Fatal("different authored metadata incorrectly deduplicated")
	}
	if embeddedImageSourceIdentity(plain) != embeddedImageSourceIdentity(asset) {
		t.Fatal("unknown authoring data invalidated unchanged render pixels")
	}
	public, err := participantSlideMarkdown(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	publicDeck, err := parseDeckDataWithAssetPublication("public.md", public, false)
	if err != nil || len(publicDeck.Assets) != 1 || assetHasOpaqueMetadata(publicDeck.Assets["photo"]) {
		t.Fatal("private media fields/dependencies leaked to participants", err)
	}
	if !assetHasOpaqueMetadata(deck.Assets["photo"]) {
		t.Fatal("audience filtering mutated authored metadata")
	}
	bundle, err := makeObjectClipboard(deck, deck.Slides[0].Elements)
	if err != nil || !reflect.DeepEqual(bundle.Assets["photo"], asset) {
		t.Fatal("selected clipboard asset dropped metadata", err)
	}
}

func TestMediaMetadataAmbiguityKeepsReceiver(t *testing.T) {
	for _, payload := range []string{
		`{"mime":"image/png","MIME":"image/jpeg"}`,
		`{"source":{"width":1,"Width":2}}`,
		`{"frames":[{"delayMs":1,"delayMs":2}]}`,
		`{"source":{"frames":[{"data":"","d\u0061ta":""}]}}`,
		`{"vector":{"version":1,"Version":2}}`,
	} {
		asset := metadataMediaFixture(t)
		before := cloneDeckAsset(asset)
		if err := json.Unmarshal([]byte(payload), &asset); err == nil || !strings.Contains(err.Error(), "ambiguous") || !reflect.DeepEqual(asset, before) {
			t.Fatal("ambiguous nested media accepted or changed receiver", payload, err)
		}
	}
}

func TestVectorMediaMetadataOwnership(t *testing.T) {
	_, asset, _, err := embeddedSVGAsset(context.Background(), []byte(ownedSVGLogo))
	if err != nil {
		t.Fatal(err)
	}
	asset.Vector.Extra = extensionFixture("futurePrivate", `{"curve":3}`)
	encoded, err := encodeDeckAssets(map[string]DeckAsset{"vector": asset})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := decodeDeckAssets(encoded)
	if err != nil || !reflect.DeepEqual(loaded["vector"], asset) {
		t.Fatal("vector companion fields lost", err)
	}
	copy := cloneDeckForRender(Deck{Assets: loaded}).Assets["vector"]
	(*copy.Vector.Extra)["futurePrivate"][0] = '['
	if (*loaded["vector"].Vector.Extra)["futurePrivate"][0] != '{' || !bytes.Equal(copy.Vector.Data, asset.Vector.Data) {
		t.Fatal("render snapshot did not own metadata/retain pixels")
	}
	if participantMediaAssets(loaded)["vector"].Vector.Extra != nil {
		t.Fatal("vector authoring metadata leaked")
	}
}

func TestClipboardMediaMIMEMatchesPixels(t *testing.T) {
	asset := clipboardTestAsset(t)
	asset.MIME = "image/jpeg"
	if err := validateClipboardAsset(asset); err == nil || !strings.Contains(err.Error(), "declared type") {
		t.Fatal("PNG pixels accepted as JPEG", err)
	}
	asset = clipboardTestAsset(t)
	asset.Source = &DeckAssetSource{MIME: "image/jpeg", Width: asset.Width, Height: asset.Height, Data: asset.Data}
	if err := validateClipboardAsset(asset); err == nil || !strings.Contains(err.Error(), "declared type") {
		t.Fatal("original source MIME mismatch accepted", err)
	}
}
