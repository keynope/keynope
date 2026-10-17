package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func clipboardTestAsset(t *testing.T) DeckAsset {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return DeckAsset{MIME: "image/png", Width: 2, Height: 2, Data: buf.Bytes()}
}

func TestObjectClipboardCrossDeckOwnsOnlySelectedDependencies(t *testing.T) {
	source := structuredFixture()
	source.Slides[0].Notes = "PRIVATE SPEAKER NOTES"
	source.Slides[0].Engagement = &EngagementDefinition{ID: "private-activity", Kind: "storm", Prompt: "PRIVATE ACTIVITY"}
	source.Assets = map[string]DeckAsset{"photo": clipboardTestAsset(t), "unselected": clipboardTestAsset(t)}
	source.Slides[0].Elements = append(source.Slides[0].Elements, Element{ID: "photo-element", Kind: "image", AssetID: "photo", Path: "/Users/private/image.png"})
	bundle, err := makeObjectClipboard(source, []Element{source.Slides[0].Elements[0], source.Slides[0].Elements[2]})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"PRIVATE", "/Users/private", "chart-1", "slide-1", "unselected", "private-activity"} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatalf("clipboard leaked %s", private)
		}
	}
	if len(bundle.Objects.Tables) != 1 || len(bundle.Objects.Charts) != 0 || len(bundle.Assets) != 1 {
		t.Fatal("copied unrelated dependencies")
	}
	destination := Deck{Slides: []Slide{{Elements: []Element{{ID: "original", Kind: "text", Text: "Destination"}}}}}
	before, _ := json.Marshal(destination)
	elements, assets, objects, err := prepareObjectClipboardPaste(destination, bundle)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(destination)
	if !bytes.Equal(after, before) {
		t.Fatal("prepare mutated destination")
	}
	if elements[0].ID == "table-element" || elements[0].ObjectID == "table-1" {
		t.Fatal("paste shares source identity")
	}
	objects.Tables[elements[0].ObjectID].Cells[1][0].Text = "Edited copy"
	if source.Objects.Tables["table-1"].Cells[1][0].Text != "One" || bundle.Objects.Tables["table-1"].Cells[1][0].Text != "One" {
		t.Fatal("table payload aliases clipboard/source")
	}
	pastedAsset := assets[elements[1].AssetID]
	pastedAsset.Data[0] = 0
	if source.Assets["photo"].Data[0] != 0x89 || bundle.Assets["photo"].Data[0] != 0x89 {
		t.Fatal("asset bytes alias source/clipboard")
	}
}

func TestObjectClipboardHistoryAndMasterPaste(t *testing.T) {
	source := structuredFixture()
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			s := newNativeEditorSession("destination.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "original", Kind: "text", Text: "Destination"}}}}}, false, false)
			if master {
				if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
					t.Fatal(err)
				}
			}
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
				t.Fatal(err)
			}
			if len(s.undo) != 1 || !s.dirtyLocked() || len(s.selection) != 2 {
				t.Fatal("paste is not one selected, dirty transaction")
			}
			var target Slide
			if master {
				target = *masterSlideAt(&s.deck, s.currentMaster)
			} else {
				target = s.deck.Slides[0]
			}
			if len(target.Elements) < 2 || s.deck.Objects == nil {
				t.Fatal("paste lost elements/payloads")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck, before) || s.dirtyLocked() {
				t.Fatal("undo did not restore document and clean state")
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			if s.deck.Objects == nil || len(s.deck.Objects.Tables) != 1 || len(s.deck.Objects.Charts) != 1 {
				t.Fatal("redo lost payloads")
			}
			if _, err := serializeDeck("destination.md", s.deck); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestObjectClipboardAtomicValidation(t *testing.T) {
	s := newNativeEditorSession("destination.md", Deck{Slides: []Slide{{Elements: []Element{{ID: "original", Kind: "text", Text: "Destination"}}}}}, false, false)
	bundle, err := makeObjectClipboard(structuredFixture(), structuredFixture().Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []func(*ObjectClipboard){
		func(b *ObjectClipboard) { b.Version = 2 },
		func(b *ObjectClipboard) { b.Elements[0].ObjectID = "absent" },
		func(b *ObjectClipboard) { b.Elements[1].ID = b.Elements[0].ID },
		func(b *ObjectClipboard) { b.Elements[0].Kind = "unknown" },
		func(b *ObjectClipboard) { b.Objects.Builds = map[string]SlideBuild{"private": {Version: 1}} },
		func(b *ObjectClipboard) { b.Assets = map[string]DeckAsset{"unreferenced": clipboardTestAsset(t)} },
	} {
		data, _ := json.Marshal(bundle)
		var bad ObjectClipboard
		_ = json.Unmarshal(data, &bad)
		invalid(&bad)
		before := cloneDeck(s.deck)
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bad}); err == nil {
			t.Fatal("accepted invalid clipboard")
		}
		if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 || s.version != version {
			t.Fatal("invalid paste changed live state")
		}
	}
}

func TestObjectClipboardDeduplicatesAssetsWithoutOverwriting(t *testing.T) {
	asset := clipboardTestAsset(t)
	bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "image", Kind: "image", AssetID: "source"}}, Assets: map[string]DeckAsset{"source": asset}}
	deck := Deck{Assets: map[string]DeckAsset{"already-here": asset}}
	elements, assets, _, err := prepareObjectClipboardPaste(deck, bundle)
	if err != nil || len(assets) != 1 || elements[0].AssetID != "already-here" {
		t.Fatalf("did not reuse immutable content: %v", err)
	}
	hash := clipboardAssetHash(asset)
	other := asset
	other.LoopCount = 2
	deck.Assets = map[string]DeckAsset{hash: other}
	elements, assets, _, err = prepareObjectClipboardPaste(deck, bundle)
	if err != nil || len(assets) != 2 || elements[0].AssetID == hash || assets[hash].LoopCount != 2 {
		t.Fatalf("overwrote conflicting destination asset: %v", err)
	}
}

func TestObjectClipboardConnectionsAndGroups(t *testing.T) {
	elements := []Element{{ID: "a", Kind: "shape", Query: "group=old"}, {ID: "b", Kind: "shape", Query: "group=old"}, {ID: "line", Kind: "connector", Query: "connector-from=a&connector-from-side=right&connector-to=b&connector-to-side=left&group=old"}}
	bundle, err := makeObjectClipboard(Deck{}, elements)
	if err != nil {
		t.Fatal(err)
	}
	copied, _, _, err := prepareObjectClipboardPaste(Deck{}, bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(copied[2].Query)
	if q.Get("connector-from") != copied[0].ID || q.Get("connector-to") != copied[1].ID || elementGroup(copied[0]) == "old" || elementGroup(copied[0]) != elementGroup(copied[2]) {
		t.Fatal("group/connector refs not remapped")
	}
	bundle, err = makeObjectClipboard(Deck{}, elements[1:])
	if err != nil || len(bundle.Elements) != 1 || len(bundle.Warnings) != 1 {
		t.Fatalf("dangling connector not omitted with warning: %v", err)
	}
}

func TestObjectClipboardAnimationSources(t *testing.T) {
	asset := clipboardTestAsset(t)
	asset.MIME = "image/gif"
	asset.LoopCount = -1
	asset.Frames = []DeckAssetFrame{{Data: asset.Data, DelayMS: 40}, {Data: asset.Data, DelayMS: 120}}
	asset.Source = &DeckAssetSource{MIME: "image/png", Width: 2, Height: 2, Frames: append([]DeckAssetFrame(nil), asset.Frames...)}
	asset.Data = nil
	if err := validateClipboardAsset(asset); err != nil {
		t.Fatal(err)
	}
	asset.Source.Frames[1].DelayMS = 0
	if validateClipboardAsset(asset) == nil {
		t.Fatal("accepted invalid source delay")
	}
}

func TestObjectClipboardEndpointAndStableCut(t *testing.T) {
	s := newNativeEditorSession("source.md", structuredFixture(), false, false)
	data, _ := json.Marshal(map[string]any{"version": s.version, "ids": []string{"table-element"}})
	w := httptest.NewRecorder()
	s.handleObjectClipboard(w, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(data)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var bundle ObjectClipboard
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Elements) != 1 || s.dirtyLocked() || len(s.undo) != 0 {
		t.Fatal("copy endpoint mutated deck")
	}
	wrong := httptest.NewRecorder()
	s.handleObjectClipboard(wrong, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", strings.NewReader(`{"version":0,"ids":["table-element"]}`)))
	if wrong.Code != http.StatusConflict {
		t.Fatal("accepted stale clipboard selection")
	}
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "delete-objects", ObjectIDs: []string{"table-element"}, SceneRevision: &version}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Slides[0].Elements) != 1 || s.deck.Slides[0].Elements[0].ID != "chart-element" {
		t.Fatal("cut targeted another object")
	}
	if err := s.apply(nativeEditorAction{Action: "delete-objects", ObjectIDs: []string{"chart-element"}, SceneRevision: &version}); err == nil {
		t.Fatal("cut accepted stale revision")
	}
}

func TestObjectClipboardCompletePixelsAndSharedDecodeBudget(t *testing.T) {
	asset := clipboardTestAsset(t)
	truncated := asset
	truncated.Data = asset.Data[:33] // A valid IHDR is not a valid full PNG.
	if _, _, err := image.DecodeConfig(bytes.NewReader(truncated.Data)); err != nil {
		t.Fatal("fixture must retain a decodable image header", err)
	}
	if err := validateClipboardAsset(truncated); err == nil {
		t.Fatal("accepted truncated image pixels")
	}
	used := int64(64000000 - 4)
	if err := validateClipboardAssetBudget(asset, &used); err != nil {
		t.Fatal(err)
	}
	if err := validateClipboardAssetBudget(asset, &used); err == nil {
		t.Fatal("each asset received a separate decode budget")
	}
}
