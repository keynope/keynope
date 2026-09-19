package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAppearanceMetadataRoundTripPreservesExtensions(t *testing.T) {
	for _, mode := range []string{"retro", "modern"} {
		a := &DeckAppearance{Version: 1, Mode: mode, Extra: map[string]json.RawMessage{
			"profiles": json.RawMessage(`{"modern":{"theme":"paper"},"retro":{"theme":"c64"}}`),
			"future":   json.RawMessage(`{"keep":true}`),
		}}
		metadata, err := encodeDeckAppearance(a)
		if err != nil {
			t.Fatal(err)
		}
		got, remaining, err := decodeDeckAppearance(metadata + "\n# Hello\n")
		if err != nil || !reflect.DeepEqual(got, a) || remaining != "# Hello\n" {
			t.Fatalf("round trip: %v, %q, %v", got, remaining, err)
		}
		again, err := encodeDeckAppearance(got)
		if err != nil || again != metadata {
			t.Fatal("appearance encoding must be deterministic")
		}
	}
}

func TestAppearanceDefaultStyleMigration(t *testing.T) {
	legacy := &DeckAppearance{Version: 1, Mode: "modern", Extra: map[string]json.RawMessage{"future": json.RawMessage(`{"keep":true}`)}}
	d := Deck{Appearance: legacy}
	a, err := d.withDefaultStyle("retro")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Version != 1 || legacy.Mode != "modern" || a.Mode != "" || a.DefaultStyle != "retro" {
		t.Fatal("migration mutated source or retained competing mode")
	}
	encoded, err := encodeDeckAppearance(a)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := decodeDeckAppearance(encoded)
	if err != nil || !reflect.DeepEqual(got, a) {
		t.Fatalf("v2 roundtrip: %v", err)
	}
	d.Appearance = got
	if d.AppearanceMode() != "retro" {
		t.Fatal("default not resolved")
	}
	changed, err := d.withAppearanceMode("modern")
	if err != nil || changed.Version != 2 || changed.DefaultStyle != "modern" || changed.Mode != "" {
		t.Fatal("legacy command downgraded v2")
	}
	if _, err := d.withDefaultStyle("inherit"); err == nil {
		t.Fatal("deck default cannot inherit")
	}
}

func TestAppearanceLegacyAndValidation(t *testing.T) {
	if (Deck{}).AppearanceMode() != "retro" {
		t.Fatal("legacy documents must default to Retro")
	}
	if got, _ := encodeDeckAppearance(nil); got != "" {
		t.Fatal("legacy documents must not gain appearance metadata")
	}
	for _, raw := range []string{`null`, `{}`, `{"version":2,"mode":"retro"}`, `{"version":1,"mode":"unknown"}`, `{"version":1,"mode":42}`} {
		metadata := "<!-- keynope-appearance base64:" + base64.StdEncoding.EncodeToString([]byte(raw)) + " -->"
		if _, _, err := decodeDeckAppearance(metadata); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	valid, _ := encodeDeckAppearance(&DeckAppearance{Version: 1, Mode: "retro"})
	if _, _, err := decodeDeckAppearance(valid + "\n" + valid); err == nil {
		t.Fatal("accepted ambiguous appearance metadata")
	}
	if _, _, err := decodeDeckAppearance("<!-- keynope-appearance base64:" + strings.Repeat("A", 65537) + " -->"); err == nil {
		t.Fatal("accepted oversized appearance metadata")
	}
}

func TestUnifiedEditorRejectsRetiredAppearanceCommands(t *testing.T) {
	s := newNativeEditorSession("Unified.md", sceneFixture(t), false, false)
	before := cloneDeck(s.deck)
	revision := s.version
	for _, action := range []nativeEditorAction{
		{Action: "set-appearance-profile"},
		{Action: "set-style-default", Kind: "deck", Name: "retro", Slide: 0, SceneRevision: &revision},
		{Action: "set-element-style", Name: "retro", Slide: 0, ObjectIDs: []string{s.deck.Slides[0].Elements[0].ID}, SceneRevision: &revision},
	} {
		if err := s.apply(action); err != errInvalidEditorAction {
			t.Fatalf("%s returned %v, want invalid editor action", action.Action, err)
		}
		if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
			t.Fatalf("retired action %s mutated the unified document", action.Action)
		}
	}
}

func TestAppearanceProfileValidationAndIsolation(t *testing.T) {
	deck := Deck{}
	if value, err := deck.withAppearanceTextScale("modern", nil); err != nil || value != nil {
		t.Fatal("clearing an absent override must not upgrade a legacy document")
	}
	for _, value := range []float64{0, -.5, 2.01, math.Inf(1), math.NaN()} {
		if _, err := deck.withAppearanceTextScale("modern", &value); err == nil {
			t.Fatalf("accepted invalid scale %v", value)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"modern":null}`, `{"modern":42}`} {
		deck.Appearance = &DeckAppearance{Version: 1, Mode: "retro", Extra: map[string]json.RawMessage{"profiles": json.RawMessage(raw)}}
		if _, err := deck.withAppearanceTextScale("modern", nil); err == nil {
			t.Fatalf("accepted invalid profile structure %s", raw)
		}
	}
	deck = sceneFixture(t)
	before, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	scale := .75
	deck.Appearance, err = deck.withAppearanceTextScale("modern", &scale)
	if err != nil {
		t.Fatal(err)
	}
	after, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if deck.AppearanceMode() != "retro" || deck.appearanceTextScale("retro") != 1 {
		t.Fatal("editing Modern profile changed Retro defaults")
	}
	for i, object := range after.Objects {
		original := before.Objects[i]
		if object.Bounds != original.Bounds || object.ID != original.ID {
			t.Fatal("typography profile moved authored geometry")
		}
		if object.Text != nil && object.Text.Size != original.Text.Size*1.5 {
			t.Fatal("text did not use profile scale")
		}
		if object.Label != nil && object.Label.Size != original.Label.Size*1.5 {
			t.Fatal("shape label did not use profile scale")
		}
	}
}

func TestModernDocumentRestoresModernProjection(t *testing.T) {
	metadata, err := encodeDeckAppearance(&DeckAppearance{Version: 1, Mode: "modern"})
	if err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeckData("Modern.md", []byte(metadata+"\n# Modern\n"))
	if err != nil || deck.AppearanceMode() != "modern" || deck.ResolvedSlides()[0].ModernScene == nil {
		t.Fatalf("Modern document fell back to Retro: %v", err)
	}
}

func TestAppearanceSurvivesSaveCloneAndNativeHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appearance.md")
	d, err := parseDeckData(path, []byte("<!-- keynope width=245 height=56 -->\n# Hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	d.Appearance = &DeckAppearance{Version: 1, Mode: "retro", Extra: map[string]json.RawMessage{"profile": json.RawMessage(`{"theme":"original"}`)}}
	copy := cloneDeck(d)
	copy.Appearance.Extra["profile"][2] = 'X'
	if bytes.Equal(copy.Appearance.Extra["profile"], d.Appearance.Extra["profile"]) {
		t.Fatal("cloning aliases appearance data")
	}
	encoded, err := serializeDeck(path, d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, encoded)
	if err != nil || !reflect.DeepEqual(loaded.Appearance, d.Appearance) {
		t.Fatalf("save/load: %v", err)
	}
	session := newNativeEditorSession(path, loaded)
	migratedAppearance := session.deck.Appearance.Clone()
	if session.state().Dirty {
		t.Fatal("loading appearance made the deck dirty")
	}
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "text"}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if session.state().Dirty || !reflect.DeepEqual(session.deck.Appearance, migratedAppearance) {
		t.Fatal("undo lost appearance or saved-state equality")
	}
	if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(session.deck.Appearance, migratedAppearance) {
		t.Fatal("redo lost appearance")
	}
}

func TestImageRetainsSourceWithoutChangingRetroDerivative(t *testing.T) {
	original := image.NewNRGBA(image.Rect(0, 0, 768, 512))
	original.Set(700, 400, color.NRGBA{R: 173, G: 91, B: 37, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, original); err != nil {
		t.Fatal(err)
	}
	id, asset, _, err := embeddedImageAsset(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if asset.Width != 384 || asset.Height != 256 {
		t.Fatal("changed retro derivative dimensions")
	}
	if asset.Source == nil || asset.Source.Width != 768 || asset.Source.Height != 512 {
		t.Fatal("missing full-resolution source")
	}
	restored, err := png.Decode(bytes.NewReader(asset.Source.Data))
	if err != nil || restored.Bounds() != original.Bounds() {
		t.Fatalf("invalid source: %v", err)
	}
	if restored.At(700, 400) != original.At(700, 400) {
		t.Fatal("source pixels changed")
	}
	d := Deck{Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{Kind: "image", AssetID: id}}}}}
	copy := cloneDeck(d)
	copy.Assets[id].Source.Data[0] ^= 1
	if copy.Assets[id].Source.Data[0] == d.Assets[id].Source.Data[0] {
		t.Fatal("clone aliases image source")
	}
	path := filepath.Join(t.TempDir(), "source.md")
	encoded, err := serializeDeck(path, d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, encoded)
	if err != nil || !reflect.DeepEqual(loaded.Assets[id].Source, asset.Source) {
		t.Fatalf("source not portable: %v", err)
	}
}

func TestAssetIdentityIncludesSource(t *testing.T) {
	first, _, _, err := makeDeckAssetWithSource([]byte("same derivative"), "image/png", 1, 1, &DeckAssetSource{Data: []byte("first original")})
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := makeDeckAssetWithSource([]byte("same derivative"), "image/png", 1, 1, &DeckAssetSource{Data: []byte("second original")})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different source images must not deduplicate to the same derivative")
	}
}
