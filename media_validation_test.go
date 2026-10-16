package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestMediaValidationCacheBudgetCancellationAndReplacement(t *testing.T) {
	bundle, gate := blockedClipboardBundle(t)
	close(gate.release)
	asset := bundle.Assets["photo"]
	if err := validateClipboardAsset(asset); err != nil || gate.calls.Load() != 1 {
		t.Fatal("first image was not decoded", err, gate.calls.Load())
	}
	if err := validateClipboardAsset(cloneDeckAsset(asset)); err != nil || gate.calls.Load() != 1 {
		t.Fatal("same pixels were decoded again", err, gate.calls.Load())
	}
	asset.Extra = extensionFixture("future", `{"changed":true}`)
	if err := validateClipboardAsset(asset); err != nil || gate.calls.Load() != 1 {
		t.Fatal("authoring metadata invalidated pixel validation", err)
	}
	for _, start := range []int64{64000000 - 4, 64000000 - 3} {
		pixels := start
		err := validateClipboardAssetBudget(asset, &pixels)
		if pixels != start+4 || (err == nil) != (start == 64000000-4) || gate.calls.Load() != 1 {
			t.Fatal("cached pixels bypassed bundle budget", pixels, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var pixels int64
	if err := validateClipboardAssetBudgetContext(ctx, asset, &pixels); !errors.Is(err, context.Canceled) || pixels != 0 {
		t.Fatal("cache hit bypassed cancellation", err)
	}
	asset.Width++
	if err := validateClipboardAsset(asset); err == nil {
		t.Fatal("changed dimensions reused valid cache entry")
	}
	valid := clipboardTestAsset(t)
	if err := validateClipboardAsset(valid); err != nil {
		t.Fatal(err)
	}
	valid.Data[len(valid.Data)-1] ^= 1
	if err := validateClipboardAsset(valid); err == nil {
		t.Fatal("in-place corruption reused earlier validation")
	}
	key, err := mediaValidationDigest(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, cached := sharedMediaValidation.lookup(key); cached {
		t.Fatal("invalid pixels entered success cache")
	}
}

func TestMediaValidationCacheBoundedAndDigestInputs(t *testing.T) {
	cache := &mediaValidationCache{}
	var first [32]byte
	for i := 0; i < 513; i++ {
		var key [32]byte
		key[0], key[1] = byte(i), byte(i>>8)
		cache.remember(key, int64(i))
	}
	if len(cache.entries) != 512 || len(cache.order) != 512 {
		t.Fatal("unbounded cache bookkeeping")
	}
	if _, present := cache.lookup(first); present {
		t.Fatal("old validation was not evicted")
	}
	asset := metadataMediaFixture(t)
	base, _ := mediaValidationDigest(context.Background(), asset)
	for name, edit := range map[string]func(*DeckAsset){
		"mime":              func(a *DeckAsset) { a.MIME = "image/jpeg" },
		"dimensions":        func(a *DeckAsset) { a.Height++ },
		"data":              func(a *DeckAsset) { a.Data[0] ^= 1 },
		"loops":             func(a *DeckAsset) { a.LoopCount++ },
		"frames":            func(a *DeckAsset) { a.Frames[0].Data[0] ^= 1 },
		"delay":             func(a *DeckAsset) { a.Frames[0].DelayMS++ },
		"source mime":       func(a *DeckAsset) { a.Source.MIME = "image/jpeg" },
		"source dimensions": func(a *DeckAsset) { a.Source.Width++ },
		"source bytes":      func(a *DeckAsset) { a.Source.Data[0] ^= 1 },
		"source frames":     func(a *DeckAsset) { a.Source.Frames[0].Data[0] ^= 1 },
		"source delay":      func(a *DeckAsset) { a.Source.Frames[0].DelayMS++ },
		"vector":            func(a *DeckAsset) { a.Vector = &DeckAssetVector{Version: 1} },
	} {
		changed := cloneDeckAsset(asset)
		edit(&changed)
		key, _ := mediaValidationDigest(context.Background(), changed)
		if key == base {
			t.Fatal("validation input omitted from digest", name)
		}
	}
}

func TestDeckMediaPixelAdmissionAtomicAndSymmetric(t *testing.T) {
	base := clipboardTestAsset(t)
	for name, edit := range map[string]func(*DeckAsset){
		"truncated pixels": func(a *DeckAsset) { a.Data = a.Data[:len(a.Data)-12] },
		"wrong dimensions": func(a *DeckAsset) { a.Width++ },
		"wrong MIME":       func(a *DeckAsset) { a.MIME = "image/jpeg" },
		"oversized":        func(a *DeckAsset) { a.Width, a.Height = 32768, 32768 },
		"invalid frame": func(a *DeckAsset) {
			a.Data = nil
			a.Frames = []DeckAssetFrame{{Data: []byte("bad pixels"), DelayMS: 50}}
		},
		"invalid delay": func(a *DeckAsset) { a.Data = nil; a.Frames = []DeckAssetFrame{{Data: base.Data}} },
		"invalid original": func(a *DeckAsset) {
			a.Source = &DeckAssetSource{MIME: "image/png", Width: 2, Height: 2, Data: []byte("bad pixels")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			asset := cloneDeckAsset(base)
			edit(&asset)
			assets := map[string]DeckAsset{"rejected-pixels": asset}
			if _, err := encodeDeckAssets(assets); err == nil {
				t.Fatal("writer accepted invalid pixels")
			}
			for _, master := range []bool{false, true} {
				s := newNativeEditorSession("owned.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Keep"}}}}})
				s.masterMode = master
				element := Element{Kind: "image", AssetID: "rejected-pixels"}
				revision := s.version
				action := nativeEditorAction{Action: "add-element", ElementData: &element, AssetData: &asset, SceneRevision: &revision, SceneMaster: master}
				assertEditorFailureUnchanged(t, s, action)
				var pixels int64
				want := validateClipboardAssetBudgetContext(context.Background(), asset, &pixels)
				if err := s.apply(action); err == nil || want == nil || err.Error() != want.Error() {
					t.Fatal("image action failed for an unrelated reason", err, want)
				}
			}
			raw, err := json.Marshal(assets)
			if err != nil {
				t.Fatal(err)
			}
			archive := assetArchiveEnvelope(t, raw)
			if _, remaining, err := decodeDeckAssets(archive); err == nil || remaining != archive {
				t.Fatal("reader accepted/consumed invalid pixels", err)
			}
			revision := embeddedImageRegistrationRevision.Load()
			oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
			defer func() {
				authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
			}()
			authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
			_, err = parseEditorDeckData("candidate.md", []byte("<!-- keynope width=100 height=80 -->\n"+archive+"\nText"))
			if err == nil || authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged || revision != embeddedImageRegistrationRevision.Load() {
				t.Fatal("rejected pixels published runtime state", err)
			}
		})
	}
	// Multiple independent assets are valid; deck validation resets the count
	// per asset. The clipboard aggregate count above deliberately does not reset.
	assets := map[string]DeckAsset{}
	for i := 0; i < 4; i++ {
		assets[fmt.Sprint("image-", i)] = cloneDeckAsset(base)
	}
	if err := validateDeckAssetPixels(context.Background(), assets); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := embeddedImageAssetContext(context.Background(), bytes.Repeat([]byte("not an image"), 2)); err == nil {
		t.Fatal("normal import bypassed pixel decoding")
	}
}

func TestGIFFramePreflightMatchesStoredFrameLimit(t *testing.T) {
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	for _, count := range []int{2048, 2049} {
		animation := &gif.GIF{Image: make([]*image.Paletted, count), Delay: make([]int, count)}
		for i := range animation.Image {
			animation.Image[i] = frame
		}
		var encoded bytes.Buffer
		if err := gif.EncodeAll(&encoded, animation); err != nil {
			t.Fatal(err)
		}
		err := preflightGIFFrames(encoded.Bytes())
		if (err == nil) != (count == 2048) {
			t.Fatal("import/store frame limits disagree", count, err)
		}
	}
}
