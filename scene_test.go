package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func sceneFixture(t *testing.T) Deck {
	t.Helper()
	label, _ := json.Marshal(shapeLabelData{Text: "A labelled shape", Query: "ttf-size=65&fg=%23ffffff"})
	q := url.Values{"shape-label": {base64.StdEncoding.EncodeToString(label)}, "shape": {"square"}, "width": {"80.5"}, "height": {"10.5"}, "top": {"23"}, "left": {"12"}, "fg": {"#2463a8"}, "gradient-start": {"#2463a8"}, "gradient-end": {"#7345b6"}, "gradient-dir": {"horizontal"}}
	deck := Deck{Slides: []Slide{{BG: "48;2;245;247;250", FG: "38;2;25;35;50", HeaderFG: "38;2;25;35;50", PageNumber: "hide", Notes: "SECRET_NOTES", Engagement: &EngagementDefinition{ID: "private", Kind: "impostor", Prompt: "SECRET_PROMPT"}, EngagementResult: &EngagementResult{Version: 1, State: map[string]json.RawMessage{"roleAssignments": json.RawMessage(`{"SECRET_IDENTITY":"impostor"}`)}}, Elements: []Element{
		{ID: "title", Kind: "heading", Level: 1, Text: "One scene. Two personalities.", Query: "render=truetype&top=3&left=12&width=210&height=15&ttf-size=160"},
		{ID: "body", Kind: "text", Text: "Real proportional text with [color=#2463a8]colour[/color], mixed widths and editable content.", Query: "render=truetype&top=13&left=12&width=200&height=8&ttf-size=90"},
		{ID: "box", Kind: "shape", Query: q.Encode()},
		{ID: "circle", Kind: "shape", Query: "shape=circle&width=45&height=16&top=23&left=150&fg=%23d76642"},
		{ID: "line", Kind: "connector", Query: "connector-from=box&connector-from-side=right&connector-to=circle&connector-to-side=left&connector-mode=elbow&connector-arrows=end&connector-width=1.5&connector-arrow-width=6"},
	}}}}
	img := image.NewNRGBA(image.Rect(0, 0, 768, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 768; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x / 3), uint8(y), 180, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	id, asset, path, err := embeddedImageAsset(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	deck.Assets = map[string]DeckAsset{id: asset}
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "image", Kind: "image", AssetID: id, Path: path, Query: "top=41&left=12&width=78&height=10"})
	return deck
}

func TestSceneProjectionGeometryIdentityPrivacy(t *testing.T) {
	deck := sceneFixture(t)
	before, _ := json.Marshal(deck)
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(deck)
	if string(before) != string(after) {
		t.Fatal("projection changed authored deck")
	}
	objects := map[string]sceneObject{}
	for _, o := range scene.Objects {
		objects[o.ID] = o
	}
	box := objects["box"]
	if objects["title"].Paint.Color != "#192332" || objects["body"].Paint.Color != "#192332" {
		t.Fatal("inherited text color lost")
	}
	if math.Abs(box.Bounds.Width-80.5*1920/245) > 1e-8 || math.Abs(box.Bounds.Height-10.5*1080/56) > 1e-8 {
		t.Fatalf("fractional grid geometry lost: %+v", box.Bounds)
	}
	if box.Label == nil || box.LabelPaint == nil || box.Shape != "square" {
		t.Fatal("shape semantics lost")
	}
	if len(objects["line"].Points) < 2 || math.Abs(objects["line"].Paint.StrokeWidth-1.5*1920/245) > 1e-8 {
		t.Fatalf("route or fractional width lost: %+v", objects["line"])
	}
	if len(scene.Objects) != 6 {
		t.Fatalf("got %d objects", len(scene.Objects))
	}
	media := objects["image"].Media
	if media == nil || media.Width != 768 || media.Height != 256 {
		t.Fatal("source-quality media was not projected")
	}
	data, err := json.Marshal(scene)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET_NOTES", "SECRET_PROMPT", "SECRET_IDENTITY", "roleAssignments"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("scene leaked %s", secret)
		}
	}
	if path := os.Getenv("KEYNOPE_SCENE_FIXTURE"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".css", []byte(modernFontsCSS()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModernTextWidthProjection(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		text := sceneTextFor(Element{Kind: kind, Text: "Some text", Query: "ttf-width=50"})
		if text.WidthScale != .5 {
			t.Fatalf("%s width not projected", kind)
		}
	}
	if sceneTextFor(Element{Kind: "text"}).WidthScale != 1 {
		t.Fatal("Modern default must use natural proportional width")
	}
}

func TestSceneTextRunsPreserveLiteralAndNestedColor(t *testing.T) {
	text := sceneTextFor(Element{Kind: "text", Text: "**** [color=#112233]outer [color=#445566]inner[/color] tail[/color] end", Query: "ttf-weight=bold"})
	want := []sceneRun{{Text: "**** ", Bold: true}, {Text: "outer ", Bold: true, Color: "#112233"}, {Text: "inner", Bold: true, Color: "#445566"}, {Text: " tail", Bold: true, Color: "#112233"}, {Text: " end", Bold: true}}
	if !reflect.DeepEqual(text.Runs, want) {
		t.Fatalf("runs: %+v", text.Runs)
	}
	code := "[color=#112233]literal[/color] ****"
	if got := sceneTextFor(Element{Kind: "code", Text: code}); len(got.Runs) != 1 || got.Runs[0].Text != code {
		t.Fatalf("code was interpreted: %+v", got)
	}
	malformed := "before [color=#112233]unclosed"
	if got := sceneTextFor(Element{Kind: "text", Text: malformed}); len(got.Runs) != 1 || got.Runs[0].Text != malformed {
		t.Fatal("malformed literal content lost")
	}
}

func TestSceneBulletParagraphs(t *testing.T) {
	text := sceneTextFor(Element{Kind: "bullet", Text: "First [color=#123456]item[/color]\n  continuation\nSecond item"})
	if len(text.Runs) != 0 || len(text.Paragraphs) != 2 {
		t.Fatalf("expected one authoritative paragraph representation: %+v", text)
	}
	want := []sceneRun{{Text: "First "}, {Text: "item", Color: "#123456"}, {Text: "\n"}, {Text: "continuation"}}
	if !reflect.DeepEqual(text.Paragraphs[0].Runs, want) {
		t.Fatalf("lost continuation or styles: %+v", text.Paragraphs[0])
	}
	if !text.Paragraphs[0].Bullet || !text.Paragraphs[1].Bullet {
		t.Fatal("missing semantic list markers")
	}
}

func TestSceneHandlerValidation(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", sceneFixture(t), true, false)
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/editor/scene?slide=0", 200}, {"POST", "/api/editor/scene?slide=0", 405}, {"GET", "/api/editor/scene?slide=-1", 400}, {"GET", "/api/editor/scene", 400}} {
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
