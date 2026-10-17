package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func wordThemeFixture(t *testing.T) Deck {
	t.Helper()
	runs := []sceneRun{{Text: "Role", Color: "#112233", ColorToken: "accent-1"}, {Text: " Fixed", Color: "#ff55aa"}}
	query := "render=truetype&modern-font=sans&modern-size=60&modern-width=100&modern-line-height=1.25&fg=%23ffffff&left=20&top=8&width=180&height=14"
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light")}
	for i, kind := range []string{"text", "bullet", "code", "shape"} {
		e := withConvertedRuns(Element{ID: "word-" + kind, Kind: kind, Query: query}, runs)
		if kind == "shape" {
			label := withConvertedRuns(Element{Kind: "text", Query: query}, runs)
			data, err := json.Marshal(shapeLabelData{Text: label.Text, Query: label.Query})
			if err != nil {
				t.Fatal(err)
			}
			e = Element{ID: "word-shape", Kind: "shape", Query: "shape=square&left=20&top=8&width=180&height=14&fg=%23203440&shape-stroke=none"}
			e.Query = setQueryValue(e.Query, "shape-label", base64.StdEncoding.EncodeToString(data))
		}
		deck.Slides = append(deck.Slides, Slide{ID: []string{"body", "list", "code", "label"}[i], Elements: []Element{e}, FG: "38;2;255;255;255", BG: "48;2;12;22;32", HeaderFG: "38;2;255;255;255", FGSet: true, BGSet: true, HeaderFGSet: true})
	}
	return deck
}

func firstWordRun(text *sceneText) sceneRun {
	if len(text.Runs) > 0 {
		return text.Runs[0]
	}
	return text.Paragraphs[0].Runs[0]
}

func TestWordThemeColoursPortableProjectionAndCache(t *testing.T) {
	deck := wordThemeFixture(t)
	before := cloneDeck(deck)
	for _, changed := range []bool{false, true} {
		if changed {
			deck.Theme.Colors.Accent[0] = "#003366"
		}
		for index := range deck.Slides {
			scene, err := buildSlideScene(deck, index, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			text := scene.Objects[0].Text
			if text == nil {
				text = scene.Objects[0].Label
			}
			run := firstWordRun(text)
			if run.Color != deck.Theme.Colors.Accent[0] || run.ColorToken != "accent-1" || text.ColorTokens["accent-1"] != run.Color {
				t.Fatal("theme word colour stale or baked", run, text.ColorTokens)
			}
			annotateSceneEditing(&scene, deck, deck.Slides[index].Elements)
			if scene.Objects[0].EditRuns[0].Color != "#112233" || scene.Objects[0].EditRuns[0].ColorToken != "accent-1" {
				t.Fatal("editing lost authored RGB fallback", scene.Objects[0].EditRuns)
			}
		}
	}
	if !reflect.DeepEqual(deck.Slides, before.Slides) {
		t.Fatal("projection mutated authored runs")
	}
	md, err := serializeDeck("Roles.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Roles.md", md)
	if err != nil {
		t.Fatal(err)
	}
	for index := range deck.Slides {
		if !reflect.DeepEqual(loaded.Slides[index].Elements, deck.Slides[index].Elements) {
			t.Fatal("word token changed on Markdown reopening", index, loaded.Slides[index].Elements, deck.Slides[index].Elements)
		}
		unused := cloneDeck(deck)
		unused.Theme.Colors.Accent[5] = "#abcdef"
		if !sceneThemeInputsEqual(deck, unused, deck.Slides[index], sceneResponseKey{cols: 245, rows: 56}) {
			t.Fatal("unused role invalidated scene", index)
		}
		used := cloneDeck(deck)
		used.Theme.Colors.Accent[0] = "#abcdef"
		if sceneThemeInputsEqual(deck, used, deck.Slides[index], sceneResponseKey{cols: 245, rows: 56}) {
			t.Fatal("used word role missing from theme consumers", index)
		}
	}
	if root := os.Getenv("KEYNOPE_WORD_THEME_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "roles.md"), md, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWordThemeSnapshotsAndDestinationFallback(t *testing.T) {
	source := wordThemeFixture(t)
	runs := []sceneRun{{Text: "Role", Color: "#112233", ColorToken: "accent-1"}}
	base, err := captureTextRangeStyle(runs, &textStyleRange{0, 4})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := wordSnapshotInDeck(source, base)
	if snapshot.Fields["paint.color"].Token != "accent-1" || snapshot.Fields["paint.color"].Value != source.Theme.Colors.Accent[0] || snapshot.ThemeSignature == "" {
		t.Fatal("capture lacks current-role RGB fallback", snapshot)
	}
	for _, portable := range []bool{false, true} {
		target := Deck{Appearance: &DeckAppearance{Version: 3}}
		if portable {
			target.Theme = builtinDeckTheme("clean-dark")
			target.Theme.Colors.Accent[0] = "#ffaa00"
		}
		adapted := wordSnapshotForDestination(target, snapshot)
		got, _, err := applyTextRangeStyle([]sceneRun{{Text: "Target"}}, &textStyleRange{0, 6}, adapted, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if portable && got[0].ColorToken != "accent-1" || !portable && got[0].ColorToken != "" || got[0].Color != source.Theme.Colors.Accent[0] {
			t.Fatal("cross-deck role/fallback mapping incorrect", portable, got)
		}
	}
	if base.Fields["paint.color"].Value != "#112233" || snapshot.Fields["paint.color"].Token != "accent-1" {
		t.Fatal("clipboard adaptation aliases source")
	}
	input := []sceneRun{{Text: "A", Color: "#112233", ColorToken: "accent-1"}, {Text: "B", Color: "#112233", ColorToken: "accent-2"}}
	mixed, err := captureTextRangeStyle(input, &textStyleRange{0, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := mixed.Fields["paint.color"]; exists {
		t.Fatal("different roles flattened because fallbacks matched")
	}
}

func TestWordThemeIgnoresStaleRunDependencies(t *testing.T) {
	d := wordThemeFixture(t)
	changed := cloneDeck(d)
	changed.Theme.Colors.Accent[0] = "#123456"
	e := d.Slides[0].Elements[0]
	if wordThemeInputsEqual(d, changed, e) {
		t.Fatal("valid role dependency ignored")
	}
	e.Text += " external edit"
	if !wordThemeInputsEqual(d, changed, e) {
		t.Fatal("stale source fingerprint invalidates unrelated palette")
	}
	e = d.Slides[0].Elements[0]
	e.Query = setQueryValue(e.Query, "ttf-weight", "bold")
	if !wordThemeInputsEqual(d, changed, e) {
		t.Fatal("stale weight fingerprint invalidates unrelated palette")
	}
}

func TestWordThemeDraftCommandsAndValidation(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck := wordThemeFixture(t)
		s := newNativeEditorSession("Roles.md", deck)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "role-master"
		}
		before, version := cloneDeck(s.deck), s.version
		call := func(action nativeEditorAction) (int, []byte) {
			raw, _ := json.Marshal(action)
			w := httptest.NewRecorder()
			s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(raw)))
			return w.Code, w.Body.Bytes()
		}
		a := nativeEditorAction{Action: "set-selection-property", SceneMaster: master, SceneRevision: &version, ObjectID: "word-text", StyleContext: "text-range", TextRuns: []sceneRun{{Text: "Role Fixed"}}, TextRange: &textStyleRange{0, 4}, Property: "paint.colorToken", PropertyValue: json.RawMessage(`"accent-2"`)}
		status, data := call(a)
		if status != 200 {
			t.Fatal(status, string(data))
		}
		var response struct {
			Runs   []sceneRun               `json:"textRuns"`
			Colors map[string]string        `json:"colorTokens"`
			Caps   selectionCapabilityState `json:"selectionCapabilities"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		if response.Runs[0].ColorToken != "accent-2" || response.Colors["accent-2"] != deck.Theme.Colors.Accent[1] || response.Caps.Properties["paint.colorToken"].Value != "accent-2" || response.Caps.Properties["paint.colorToken"].Provenance != "theme" {
			t.Fatal("accepted role/colour capability missing", response)
		}
		if s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("role draft committed early")
		}
		if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneMaster: master, SceneRevision: &version, ObjectID: "word-text", TextRuns: response.Runs}); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		if s.version != version+1 || len(s.undo) != 1 {
			t.Fatal("role commit not one transaction")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("role undo mismatch", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("role redo mismatch", err)
		}
	}
	s := newNativeEditorSession("No theme.md", Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "target", Kind: "text", Text: "Target"}}}}})
	version := s.version
	bad := &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{"paint.color": {Present: true, Value: "#123456", Token: "unknown"}}}
	a := nativeEditorAction{Action: "paste-object-style", SceneRevision: &version, ObjectID: "target", StyleContext: "text-range", TextRuns: []sceneRun{{Text: "Target"}}, TextRange: &textStyleRange{0, 6}, StyleData: bad}
	raw, _ := json.Marshal(a)
	w := httptest.NewRecorder()
	s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(raw)))
	if w.Code != 400 || s.version != version || len(s.undo) != 0 {
		t.Fatal("destination adaptation hid invalid source token", w.Code, w.Body.String())
	}
	for _, run := range []sceneRun{{Text: "Bad", ColorToken: "accent-1"}, {Text: "Bad", Color: "#fff", ColorToken: "accent-1"}, {Text: "Bad", Color: "#123456", ColorToken: "url(evil)"}} {
		if validSceneRun(run) {
			t.Fatal("malformed role/fallback accepted", run)
		}
	}
}

func TestWordThemeWholeObjectClipboardFallbacks(t *testing.T) {
	deck := wordThemeFixture(t)
	for _, index := range []int{0, 3} {
		bundle, err := makeObjectClipboard(deck, deck.Slides[index].Elements)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(bundle.ThemeSignature, deck.Theme.Name) || len(bundle.ThemeSignature) != 64 {
			t.Fatal("clipboard association not bounded hash")
		}
		for _, portable := range []bool{false, true} {
			target := Deck{Appearance: &DeckAppearance{Version: 3}}
			if portable {
				target.Theme = builtinDeckTheme("clean-dark")
				target.Theme.Colors.Accent[0] = "#cc55ff"
			}
			elements, _, _, err := prepareObjectClipboardPaste(target, bundle)
			if err != nil {
				t.Fatal(err)
			}
			e := elements[0]
			if e.Kind == "shape" {
				e, _, err = sceneEditableShapeLabel(e)
				if err != nil {
					t.Fatal(err)
				}
			}
			run := sceneTextFor(e).Runs[0]
			if run.Color != deck.Theme.Colors.Accent[0] || portable && run.ColorToken != "accent-1" || !portable && run.ColorToken != "" {
				t.Fatal("object clipboard lost current-role fallback", index, portable, run)
			}
		}
	}
}

func TestWordThemeOutputProjectionAndWarmOwnership(t *testing.T) {
	deck := wordThemeFixture(t)
	deck.Theme.Colors.Accent[0] = "#aabb55"
	before := cloneDeck(deck)
	manifest, _, err := projectPDFExport("Roles.md", deck, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Pages) != 4 {
		t.Fatal("unexpected PDF pages", len(manifest.Pages))
	}
	for _, page := range manifest.Pages {
		if page.Page.Scene == nil {
			t.Fatal("PDF scene missing")
		}
		o := page.Page.Scene.Objects[0]
		text := o.Text
		if text == nil {
			text = o.Label
		}
		if firstWordRun(text).Color != "#aabb55" {
			t.Fatal("PDF manifest has stale fallback RGB")
		}
	}
	data, report, err := exportPPTXDocument(deck)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, warning := range report.Warnings {
		if warning.Code == "word-theme-colours-frozen" {
			count++
		}
	}
	if count != 4 {
		t.Fatal("PPTX lost word roles without loss diagnostic", report)
	}
	read, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, slide := range read.Slides {
		found := false
		for _, object := range slide.Objects {
			if len(object.Paragraphs) > 0 {
				found = true
				if !strings.EqualFold(object.Paragraphs[0].Runs[0].Color, "#aabb55") {
					t.Fatal("PPTX has fallback instead of current role", object)
				}
			}
		}
		if !found {
			t.Fatal("PPTX word colour rendered as image or omitted")
		}
	}
	if !reflect.DeepEqual(deck, before) {
		t.Fatal("output baked theme colours")
	}
	// A palette update is a paint dependency, not a text-projection cache miss.
	source := deck.ResolveSlide(0, false)
	if _, err := buildStyledSceneTargetMeasured(deck, source, 0, 245, 56, false, "", nil); err != nil {
		t.Fatal(err)
	}
	deck.Theme.Colors.Accent[0] = "#003366"
	metrics := &sceneRenderMetrics{}
	scene, err := buildStyledSceneTargetMeasured(deck, deck.ResolveSlide(0, false), 0, 245, 56, false, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	if firstWordRun(scene.Objects[0].Text).Color != "#003366" || len(metrics.TextProjectionBuilds) != 0 || len(metrics.TextProjectionHits) != 1 {
		t.Fatal("palette edit reshaped or retained stale text", metrics, scene.Objects[0].Text)
	}
	scene.Objects[0].Text.ColorTokens["accent-1"] = "#ff0000"
	scene.Objects[0].Text.Runs[0].Color = "#ff0000"
	other, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if firstWordRun(other.Objects[0].Text).Color != "#003366" || other.Objects[0].Text.ColorTokens["accent-1"] != "#003366" {
		t.Fatal("warm palette map aliases request")
	}
}
