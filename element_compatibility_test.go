package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"
)

func TestUnknownElementKindsPreserveAuthoredData(t *testing.T) {
	e := Element{Kind: "future-widget", ID: "future-object", ObjectID: "table-1", AssetID: "future-media", Path: "relative/future.blob", Text: "# Literal\n---\n*** future text ***", Query: "top=3&render=truetype&element-style=retro&element-id=alias&future=x%20y", Placeholder: true, SlotID: "future-slot", PlaceholderRole: "future-role", Extra: &jsonExtensions{"futureData": json.RawMessage(`{"curve":[1,2]}`)}}
	d := structuredFixture()
	d.Objects.Builds = nil
	d.Slides = []Slide{{ID: "future-slide", Elements: []Element{e}}}
	master := e
	master.ID = "future-master"
	layout := e
	layout.ID = "future-layout"
	d.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{Elements: []Element{master}}}, Layouts: []MasterLayout{{ID: "future", Name: "Future", Slide: Slide{Elements: []Element{layout}}}}}
	d.Assets = map[string]DeckAsset{"future-media": workspaceAsset(color.RGBA{R: 80, A: 255}), "unseen": workspaceAsset(color.RGBA{R: 80, A: 255})}
	before := cloneDeck(d)
	first, err := serializeDeck("compatible.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte("keynope-element version=1")) {
		t.Fatal("unknown kind fell through to text")
	}
	loaded, err := parseDeckData("compatible.md", first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Slides[0].Elements[0], e) || !reflect.DeepEqual(loaded.Masters.Base.Slide.Elements[0], master) || !reflect.DeepEqual(loaded.Masters.Layouts[0].Slide.Elements[0], layout) {
		t.Fatalf("unknown authored data changed: slide=%+v base=%+v layout=%+v want=%+v", loaded.Slides[0].Elements[0], loaded.Masters.Base.Slide.Elements[0], loaded.Masters.Layouts[0].Slide.Elements[0], e)
	}
	if len(loaded.Assets) != 2 || len(loaded.Objects.Tables) != 1 || len(loaded.Objects.Charts) != 1 {
		t.Fatal("unknown dependencies discarded")
	}
	session := newNativeEditorSession("compatible.md", loaded)
	if !reflect.DeepEqual(session.deck.Slides[0].Elements[0], e) {
		t.Fatal("editor migration rewrote unknown fields")
	}
	second, err := serializeDeck("compatible.md", loaded)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("compatible round trip not deterministic", err)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("save mutated authored input")
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range scene.Diagnostics {
		if warning.Code == "unsupported-object" {
			found = true
		}
	}
	if !found {
		t.Fatal("unsupported preview lacked diagnostic")
	}
}

func TestPageNumberSemanticKindSurvivesMarkdown(t *testing.T) {
	e := defaultPageNumberElement("authored-number")
	d := Deck{Slides: []Slide{{ID: "number-slide", Elements: []Element{e}}}}
	first, err := serializeDeck("number.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("number.md", first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Slides[0].Elements[0], e) {
		t.Fatal("page number flattened into text")
	}
}

func TestCompatibleElementMetadataBoundsAndFailures(t *testing.T) {
	valid, err := encodeCompatibleElement(Element{Kind: "future", ID: "unique", Text: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		strings.Replace(valid, "version=1", "version=2", 1),
		"<!-- keynope-element version=1 base64:AAAA -->",
		"<!-- keynope-element version=1 base64:" + base64.StdEncoding.EncodeToString([]byte(`{"text":"missing kind"}`)) + " -->",
		"<!-- keynope-element version=1 -->",
		valid + "\n" + valid,
	} {
		if _, err := parseDeckData("invalid.md", []byte(input)); err == nil {
			t.Fatal("invalid/duplicate identity accepted", input)
		}
	}
	if _, err := encodeCompatibleElement(Element{Kind: "future", Text: strings.Repeat("x", maxCompatibleElementBytes)}); err == nil {
		t.Fatal("oversized element accepted")
	}
	large, err := encodeCompatibleElement(Element{Kind: "future", Text: strings.Repeat("x", maxCompatibleElementBytes-100)})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCompatibleElementMetadata(strings.Repeat(large+"\n", 17)); err == nil {
		t.Fatal("aggregate compatibility budget not enforced")
	}
	code := "<!-- keynope-element version=2 base64:AAAA -->"
	deck, err := parseDeckData("code.md", []byte("```\n"+code+"\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides[0].Elements) != 1 || deck.Slides[0].Elements[0].Kind != "code" || deck.Slides[0].Elements[0].Text != code {
		t.Fatal("literal code mistaken for metadata")
	}
}

func TestParticipantMarkdownOmitsUnknownCompatibilityPayloads(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{{Kind: "future", ID: "future", Text: "PRIVATE FUTURE", AssetID: "private"}, {Kind: "text", Text: "Visible"}}}}, Assets: map[string]DeckAsset{"private": workspaceAsset(color.RGBA{R: 80, A: 255})}}
	data, err := participantSlideMarkdown(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "keynope-element version=1", "keynope-assets"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("unknown authoring data disclosed", secret)
		}
	}
}

func TestCompatibilitySaveAndLoadShareAggregateLimit(t *testing.T) {
	blob := json.RawMessage(`"` + strings.Repeat("x", maxCompatibleElementBytes-512) + `"`)
	deck := Deck{Slides: []Slide{{ID: "budget-slide"}}}
	for i := 0; i < 17; i++ {
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprintf("future-%d", i), Kind: "future", Extra: &jsonExtensions{"blob": blob}})
	}
	if data, err := serializeDeck("too-large.md", deck); err == nil || data != nil {
		t.Fatal("save emitted metadata that its loader cannot accept", err, len(data))
	}
	if len(deck.Slides[0].Elements) != 17 || !bytes.Equal((*deck.Slides[0].Elements[0].Extra)["blob"], blob) {
		t.Fatal("failed save changed source document")
	}
	deck.Slides[0].Elements = deck.Slides[0].Elements[:16]
	data, err := serializeDeck("within-limit.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("within-limit.md", data)
	if err != nil || len(loaded.Slides[0].Elements) != 16 {
		t.Fatal("accepted save cannot reopen", err)
	}
	if !reflect.DeepEqual(loaded.Slides[0].Elements, deck.Slides[0].Elements) {
		t.Fatal("near-limit round trip lost metadata")
	}
}
