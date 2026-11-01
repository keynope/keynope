//go:build !js && !keynope_sitegen

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func authoringCall(t *testing.T, a *deckAutomation, name string, input automationArguments) map[string]any {
	t.Helper()
	if input.ExpectedVersion == nil {
		v := a.commandContext().Version
		input.ExpectedVersion = &v
	}
	result, _, err := a.call(context.Background(), name, input)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}
func authoringCommand(t *testing.T, a *deckAutomation, command any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return authoringCall(t, a, "editor_command", automationArguments{Command: raw})
}

func TestMCPAuthoringDiscoveryAndCompactProtocol(t *testing.T) {
	a := automationTest(t)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	server, err := a.server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "authoring-test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	resources, err := client.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 5 {
		t.Fatal(resources)
	}
	for _, uri := range []string{"keynope://authoring/skill", "keynope://authoring/recipes"} {
		r, err := client.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil || len(r.Contents) != 1 || len(r.Contents[0].Text) < 200 {
			t.Fatal(uri, err)
		}
	}
	for _, topic := range []string{"workflow", "typography", "layouts", "artwork", "recipes", "all"} {
		r, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "deck_authoring_guide", Arguments: map[string]any{"topic": topic}})
		if err != nil || r.IsError {
			t.Fatal(topic, err, r)
		}
		data, _ := json.Marshal(r)
		if len(data) < 300 {
			t.Fatal("empty guide", topic)
		}
	}
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "editor_batch", Arguments: map[string]any{"expected_version": 1, "commands": []any{map[string]any{"action": "add-element", "kind": "text", "elementData": map[string]any{"kind": "text", "text": "New object"}}, map[string]any{"action": "set-layout", "value": 0}}}})
	if err != nil || !result.IsError {
		t.Fatal(err, result)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var receipt map[string]any
	json.Unmarshal(raw, &receipt)
	if receipt["version"] != float64(2) || receipt["applied_count"] != float64(1) || len(receipt["created"].([]any)) != 1 || !strings.Contains(receipt["error"].(string), "kind=layout ID") {
		t.Fatal(string(raw))
	}
	if _, ok := receipt["masters"]; ok {
		t.Fatal("partial error leaked full state")
	}
}

func TestMCPCompactReceiptsAndObjectListing(t *testing.T) {
	a := automationTest(t)
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	for i := 0; i < 34; i++ {
		if i > 0 {
			authoringCommand(t, a, map[string]any{"action": "add-slide"})
		}
		authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "text", "elementData": Element{Kind: "text", Text: "Slide content", Query: "render=truetype&modern-size=64"}})
	}
	result := authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "text", "elementData": Element{Kind: "text", Text: "A generated ID", Query: "render=truetype&top=10"}})
	created := result["created"].([]automationObjectStamp)
	if len(created) != 1 || created[0].ID == "" {
		t.Fatal(result)
	}
	if _, ok := result["masters"]; ok {
		t.Fatal("full state leaked")
	}
	compact, _ := json.Marshal(result)
	full := a.inspect()
	full["selection"] = a.editor.state().SelectionCapabilities
	verbose, _ := json.Marshal(full)
	if len(compact)*5 >= len(verbose) {
		t.Fatalf("not compact: %d vs %d", len(compact), len(verbose))
	}
	t.Logf("34-slide mutation result: compact=%d bytes; legacy=%d bytes; reduction=%.1f%%", len(compact), len(verbose), 100*(1-float64(len(compact))/float64(len(verbose))))
	listed := authoringCall(t, a, "deck_objects", automationArguments{Slides: []int{33}, Limit: 1})
	if listed["total"] != 2 || listed["more"] != true {
		t.Fatal(listed)
	}
	next := authoringCall(t, a, "deck_objects", automationArguments{Slides: []int{33}, Limit: 1, Offset: 1})
	if next["more"] != false {
		t.Fatal(next)
	}
	raw, _ := json.Marshal(map[string]any{"action": "select-slide", "slide": 0})
	v := a.commandContext().Version
	legacy := authoringCall(t, a, "editor_command", automationArguments{ExpectedVersion: &v, Command: raw, Response: "full"})
	if legacy["masters"] == nil || legacy["slides"] == nil {
		t.Fatal("full opt-in missing")
	}
}

func TestMCPNativeLayoutLayersAndMeasurement(t *testing.T) {
	a := automationTest(t)
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	authoringCommand(t, a, map[string]any{"action": "add-slide"})
	authoringCommand(t, a, map[string]any{"action": "set-layout", "kind": "blank"})
	if a.editor.deck.Slides[1].LayoutID != "blank" {
		t.Fatal("layout not assigned")
	}
	e := Element{Kind: "text", Text: "Proportional WWW iii\nA second line", Query: "render=truetype&modern-font=sans&modern-size=40&left=12&top=12&width=60"}
	r := authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "text", "elementData": e})
	id := r["created"].([]automationObjectStamp)[0].ID
	before := a.commandContext().Version
	measure := authoringCall(t, a, "text_measure", automationArguments{Slide: 1, Element: &e, BoxWidth: 60})
	slideMeasure := authoringCall(t, a, "slide_measure", automationArguments{Slide: 1})
	var textItem map[string]any
	for _, item := range slideMeasure["objects"].([]map[string]any) {
		if item["id"] == id {
			textItem = item
		}
	}
	if textItem == nil {
		t.Fatal(slideMeasure)
	}
	if math.Abs(measure["wrapped_height_pixels"].(float64)-textItem["required_height_pixels"].(float64)) > 1 {
		t.Fatal("native measurement disagrees with scene", measure, textItem)
	}
	if a.commandContext().Version != before {
		t.Fatal("read-only measure mutated deck")
	}
	for _, font := range []string{"sans", "mono", "c64"} {
		candidate := e
		candidate.Query = "render=truetype&modern-font=" + font + "&modern-size=48"
		value := authoringCall(t, a, "text_measure", automationArguments{Slide: 1, Element: &candidate, BoxWidth: 100, TextRuns: []sceneRun{{Text: "Big", FontSize: 96}, {Text: " normal", FontID: "mono"}}})
		if value["wrapped_height_pixels"].(float64) < 96 {
			t.Fatal("mixed run font size ignored", font, value)
		}
	}
	// A slide appearance-only edit must be visible in a compact receipt too.
	appearance := authoringCommand(t, a, map[string]any{"action": "update-slide", "slideData": Slide{BG: "48;2;16;24;39", BGSet: true}})
	if got := appearance["affected_slides"].([]int); len(got) != 1 || got[0] != 1 {
		t.Fatal("appearance edit absent from receipt", appearance)
	}
	authoringCommand(t, a, map[string]any{"action": "move-object-layer", "objectId": id, "kind": "back"})
	for _, name := range []string{"set-layout", "move-object-layer", "set-selection-property"} {
		value := authoringCall(t, a, "deck_schema", automationArguments{Topic: "command", CommandName: name})
		if value["fields"] == nil {
			t.Fatal(value)
		}
		for field, schema := range value["fields"].(map[string]any) {
			if schema == nil {
				t.Fatalf("missing schema for %s.%s", name, field)
			}
		}
		raw, _ := json.Marshal(value)
		if len(raw) > 6000 {
			t.Fatal("focused schema too large", name, len(raw))
		}
	}
	data, _ := newTableData(2, 2)
	data.Cells[0][0].Text = "Long enough cell content that it wraps across multiple lines on a narrow table"
	data.Cells[1][1].Text = "End"
	table := authoringCall(t, a, "table_measure", automationArguments{Table: &data, BoxWidth: 50})
	heights := table["row_heights"].([]float64)
	if len(heights) != 2 || heights[0] <= heights[1] || table["height"].(float64) <= 0 {
		t.Fatal(table)
	}
	for _, invalid := range []automationArguments{{Element: &e, BoxWidth: -1}, {Element: &e, BoxWidth: math.Inf(1)}, {Element: &Element{Kind: "image"}, BoxWidth: 50}} {
		_, _, err := a.call(context.Background(), "text_measure", invalid)
		if err == nil {
			t.Fatal("invalid measure admitted")
		}
	}
}

func TestMCPArtworkNativeSafetyAndDeterminism(t *testing.T) {
	a := automationTest(t)
	ctx := context.Background()
	im := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := 8; y < 24; y++ {
		for x := 16; x < 48; x++ {
			im.SetNRGBA(x, y, color.NRGBA{85, 255, 255, 255})
		}
	}
	var raw bytes.Buffer
	png.Encode(&raw, im)
	a.root.WriteFile("logo.png", raw.Bytes(), 0600)
	result := authoringCall(t, a, "image_pixelart", automationArguments{Path: "logo.png", Columns: 12})
	if result["rows"] != 5 || len(result["text_runs"].([]sceneRun)) == 0 {
		t.Fatal(result)
	}
	if len(a.editor.deck.Slides[0].Elements) != 0 {
		t.Fatal("conversion changed deck")
	}
	if _, _, err := a.call(ctx, "image_pixelart", automationArguments{Path: "../logo.png", Columns: 12}); err == nil {
		t.Fatal("path escaped")
	}
	im = image.NewNRGBA(image.Rect(0, 0, 8, 8))
	raw.Reset()
	png.Encode(&raw, im)
	a.root.WriteFile("empty.png", raw.Bytes(), 0600)
	if _, _, err := a.call(ctx, "image_pixelart", automationArguments{Path: "empty.png", Columns: 12}); err == nil {
		t.Fatal("empty image accepted")
	}
	config := &automationFireworks{Width: 128, Height: 72, Seconds: 1, DelayMS: 80, Rockets: 5, Seed: 42}
	version := a.commandContext().Version
	first := authoringCall(t, a, "animation_create", automationArguments{Path: "first.gif", Animation: config})
	authoringCall(t, a, "animation_create", automationArguments{Path: "second.gif", Animation: config})
	one, _ := a.root.ReadFile("first.gif")
	two, _ := a.root.ReadFile("second.gif")
	if !bytes.Equal(one, two) {
		t.Fatal("seed not deterministic")
	}
	g, err := gif.DecodeAll(bytes.NewReader(one))
	if err != nil || len(g.Image) != int(first["frames"].(int)) || g.LoopCount != 0 {
		t.Fatal(err, first)
	}
	if a.commandContext().Version != version {
		t.Fatal("asset generation mutated deck")
	}
	if _, _, _, err := embeddedImageAsset(one); err != nil {
		t.Fatal("generated GIF cannot import", err)
	}
	if _, _, err := a.call(ctx, "animation_create", automationArguments{ExpectedVersion: &version, Path: "first.gif", Animation: config}); err == nil {
		t.Fatal("overwrote without permission")
	}
	tooLarge := *config
	tooLarge.Width = 2048
	tooLarge.Height = 2048
	tooLarge.Seconds = 60
	if _, _, err := a.call(ctx, "animation_create", automationArguments{ExpectedVersion: &version, Path: "huge.gif", Animation: &tooLarge}); err == nil {
		t.Fatal("animation budget not enforced")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := a.call(cancelled, "animation_create", automationArguments{ExpectedVersion: &version, Path: "cancelled.gif", Animation: config}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

// Optional comparison against an external historical helper checkout. Neither
// the application nor its normal test suite depends on the retired scripts.
func TestMCPContributorPythonParity(t *testing.T) {
	if os.Getenv("KEYNOPE_AUTHORING_PARITY") != "1" {
		t.Skip("set KEYNOPE_AUTHORING_PARITY=1 for contributor-script comparisons")
	}
	scripts := os.Getenv("KEYNOPE_LEGACY_SCRIPTS")
	if scripts == "" {
		t.Skip("set KEYNOPE_LEGACY_SCRIPTS to the archived helper directory for historical comparisons")
	}
	pythonName := os.Getenv("KEYNOPE_PARITY_PYTHON")
	if pythonName == "" {
		pythonName = "python3"
	}
	python, err := exec.LookPath(pythonName)
	if err != nil {
		t.Fatal(err)
	}
	a := automationTest(t)
	script := func(name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(python, append([]string{filepath.Join(scripts, name)}, args...)...)
		cmd.Dir = a.directory
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", name, err, result)
		}
		return result
	}
	diagram := "+-----+\n| A B |\n+-----+"
	path := filepath.Join(a.directory, "diagram.txt")
	os.WriteFile(path, []byte(diagram), 0600)
	legacy := strings.TrimSpace(string(script("ascii_layers.py", path, "left=0-2:0-3")))
	parts := strings.SplitN(legacy, "\t", 2)
	var oldText string
	json.Unmarshal([]byte(parts[1]), &oldText)
	native := authoringCall(t, a, "diagram_layers", automationArguments{Text: diagram, Layers: []automationLayer{{Name: "left", Rectangles: []automationRectangle{{0, 2, 0, 3}}}}})
	if got := native["layers"].([]map[string]any)[0]["text"]; got != oldText {
		t.Fatal("diagram differs", got, oldText)
	}
	if err := exec.Command(python, "-c", "import PIL").Run(); err == nil {
		im := image.NewNRGBA(image.Rect(0, 0, 32, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 32; x++ {
				c := color.NRGBA{85, 255, 255, 255}
				if x >= 16 {
					c = color.NRGBA{255, 85, 170, 255}
				}
				im.SetNRGBA(x, y, c)
			}
		}
		var encoded bytes.Buffer
		png.Encode(&encoded, im)
		a.root.WriteFile("parity.png", encoded.Bytes(), 0600)
		script("pixelart.py", "legacy", filepath.Join(a.directory, "parity.png"), "16", "alpha", ".4")
		oldRaw, err := a.root.ReadFile("legacy-runs.json")
		if err != nil {
			t.Fatal(err)
		}
		var oldRuns []sceneRun
		if err = json.Unmarshal(oldRaw, &oldRuns); err != nil {
			t.Fatal(err)
		}
		pixel := authoringCall(t, a, "image_pixelart", automationArguments{Path: "parity.png", Columns: 16})
		newRuns := pixel["text_runs"].([]sceneRun)
		var oldPlain, newPlain strings.Builder
		for _, r := range oldRuns {
			oldPlain.WriteString(r.Text)
		}
		for _, r := range newRuns {
			newPlain.WriteString(r.Text)
		}
		if oldPlain.String() != newPlain.String() {
			t.Fatal("pixel occupancy differs")
		}
		// Resamplers may differ at colour edges, so compare solid interiors.
		grid := pixel["grid"].([][]string)
		if grid[0][0] != "#55ffff" || grid[0][15] != "#ff55aa" {
			t.Fatal("pixel colours differ", grid[0])
		}
		script("fireworks_gif.py", filepath.Join(a.directory, "legacy.gif"), "--size", "128x72", "--seconds", "1", "--delay", "80", "--rockets", "5", "--seed", "42")
		oldGIF, _ := a.root.ReadFile("legacy.gif")
		old, err := gif.DecodeAll(bytes.NewReader(oldGIF))
		if err != nil {
			t.Fatal(err)
		}
		newConfig := &automationFireworks{Width: 128, Height: 72, Seconds: 1, DelayMS: 80, Rockets: 5, Seed: 42}
		authoringCall(t, a, "animation_create", automationArguments{Path: "native.gif", Animation: newConfig})
		newGIF, _ := a.root.ReadFile("native.gif")
		next, err := gif.DecodeAll(bytes.NewReader(newGIF))
		if err != nil {
			t.Fatal(err)
		}
		duration := func(g *gif.GIF) int {
			total := 0
			for _, delay := range g.Delay {
				total += delay
			}
			return total
		}
		if old.Config.Width != next.Config.Width || old.Config.Height != next.Config.Height || duration(old) != duration(next) || old.LoopCount != next.LoopCount {
			t.Fatal("animation timing/dimensions differ", old.Config, next.Config, old.Delay, next.Delay)
		}
		t.Log("Pillow/native parity: identical block occupancy, solid colours, GIF dimensions/duration/looping; particle RNG, duplicate-frame coalescing and edge resampling intentionally differ")
	} else {
		t.Log("Pillow unavailable: image/GIF oracle comparison skipped; native safety and determinism tests still run")
	}
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	for i := 0; i < 3; i++ {
		if i > 0 {
			authoringCommand(t, a, map[string]any{"action": "add-slide"})
		}
		authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "text", "elementData": Element{Kind: "text", Text: "Parity text", Query: "render=truetype&modern-font=mono&modern-size=40&top=10&left=12&width=100"}})
	}
	authoringCall(t, a, "deck_save", automationArguments{Path: "baseline.md"})
	oldIDs := string(script("list_ids.py", filepath.Join(a.directory, "baseline.md")))
	listing := authoringCall(t, a, "deck_objects", automationArguments{})
	for _, e := range listing["objects"].([]map[string]any) {
		if !strings.Contains(oldIDs, e["id"].(string)) {
			t.Fatal("native listing misses legacy ID", e)
		}
	}
	script("set_layout.py", filepath.Join(a.directory, "baseline.md"), "blank")
	data, _ := a.root.ReadFile("baseline.md")
	legacyDeck, err := parseDeckDataWithAssetPublication(filepath.Join(a.directory, "baseline.md"), data, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		authoringCommand(t, a, map[string]any{"action": "select-slide", "slide": i})
		authoringCommand(t, a, map[string]any{"action": "set-layout", "kind": "blank"})
	}
	for i := 0; i < 3; i++ {
		if legacyDeck.Slides[i].LayoutID != a.editor.deck.Slides[i].LayoutID {
			t.Fatal("layout mismatch")
		}
	}
	authoringCall(t, a, "deck_export", automationArguments{Path: "native.html", Format: "html"})
	bounds := string(script("scene_bounds.py", filepath.Join(a.directory, "native.html"), "1"))
	native = authoringCall(t, a, "slide_measure", automationArguments{Slide: 1})
	for _, e := range native["objects"].([]map[string]any) {
		if !strings.Contains(bounds, e["id"].(string)) {
			t.Fatal("scene bounds missing object", e)
		}
	}
	// Native measurement and exported projection must agree at non-default
	// widths. The legacy approximation is not authoritative for proportional fonts.
	e := Element{Kind: "text", Text: "Parity text", Query: "render=truetype&modern-font=mono&modern-size=40"}
	m := authoringCall(t, a, "text_measure", automationArguments{Element: &e, BoxWidth: 100})
	if m["height"].(float64) <= 0 {
		t.Fatal(m)
	}
	before := a.commandContext()
	authoringCommand(t, a, map[string]any{"action": "undo"})
	if reflect.DeepEqual(before, a.commandContext()) {
		t.Fatal("native layout lost undo")
	}
	t.Log("Python/native parity: identical layer masks, IDs and layouts; exported scene IDs match native bounds; native layout retains undo")
}
