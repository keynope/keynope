package main

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestShapeTextGrowthIsAtomicAndUndoable(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	deck.Slides[0].Elements = []Element{{ID: "growing-shape", Kind: "shape", Query: "shape=square&width=20&height=12&left=30&top=5&element-style=modern&fg=%2355aaff"}}
	s := newNativeEditorSession("Growth.md", deck)
	before := cloneDeck(s.deck)
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildMixedSlideScene(s.deck, 0, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	var original sceneRect
	for _, object := range scene.Objects {
		if object.ID == "growing-shape" {
			original = object.Bounds
		}
	}
	if original.Width == 0 {
		t.Fatal("missing shape")
	}
	bounds := original
	bounds.Width += 75
	revision := s.version
	action := nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "growing-shape", TextRuns: []sceneRun{{Text: "A wider label"}}, ShapeTextBounds: &bounds}
	for _, bad := range []sceneRect{{original.X + 1, original.Y, bounds.Width, bounds.Height}, {original.X, original.Y, original.Width - 1, original.Height}, {original.X, original.Y, math.NaN(), original.Height}} {
		action.ShapeTextBounds = &bad
		if err := s.apply(action); err == nil {
			t.Fatal("invalid growth accepted")
		}
		if !reflect.DeepEqual(s.deck, before) {
			t.Fatal("rejected growth mutated deck")
		}
	}
	action.ShapeTextBounds = &bounds
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	var got Element
	for _, e := range s.deck.Slides[0].Elements {
		if e.ID == "growing-shape" {
			got = e
		}
	}
	label, ok := shapeLabel(got)
	if !ok || label.Text != "A wider label" {
		t.Fatal("label missing")
	}
	after, err := buildMixedSlideScene(s.deck, 0, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range after.Objects {
		if object.ID == got.ID {
			if object.Bounds.X != original.X || object.Bounds.Y != original.Y || object.Bounds.Height != original.Height || object.Bounds.Width < bounds.Width {
				t.Fatalf("wrong growth: %+v -> %+v", original, object.Bounds)
			}
		}
	}
	if len(s.undo) != 1 {
		t.Fatal("text and growth must be one undo")
	}
	path := filepath.Join(t.TempDir(), "Growth.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.ID == got.ID {
			q, _ := url.ParseQuery(e.Query)
			want, _ := url.ParseQuery(got.Query)
			for _, key := range []string{"width", "height", "left", "top", "shape-label"} {
				if q.Get(key) != want.Get(key) {
					if key != "shape-label" {
						a, ea := strconv.ParseFloat(q.Get(key), 64)
						b, eb := strconv.ParseFloat(want.Get(key), 64)
						if ea == nil && eb == nil && a == b {
							continue
						}
					}
					t.Fatalf("saved growth changed %s: %q != %q", key, q.Get(key), want.Get(key))
				}
			}
			found = true
		}
	}
	if !found {
		t.Fatal("saved shape missing")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("undo did not restore exact deck")
	}
}

func TestModernTextFitSizeIsIndependentAndUndoable(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Fit.md", deck)
	before := cloneDeck(s.deck)
	element := s.deck.Slides[0].Elements[0]
	size, revision := 17.25, s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: element.ID, ModernSize: &size, TextRuns: sceneTextFor(element).Runs}); err != nil {
		t.Fatal(err)
	}
	got := s.deck.Slides[0].Elements[0]
	if s.deck.modernSceneText(got).Size != size || trueTypeSize(got) != trueTypeSize(element) || got.Text != element.Text {
		t.Fatal("fit changed content or Retro typography")
	}
	q, _ := url.ParseQuery(got.Query)
	original, _ := url.ParseQuery(element.Query)
	q.Del("modern-size")
	if q.Encode() != original.Encode() {
		t.Fatalf("fit changed unrelated properties: %s", q.Encode())
	}
	if len(s.undo) != 1 {
		t.Fatal("fit must be one transaction")
	}
	path := filepath.Join(t.TempDir(), "Fit.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.ID == element.ID {
			found = loaded.modernSceneText(e).Size == size
		}
	}
	if !found {
		t.Fatal("Modern fit size did not survive save and reload")
	}
	reset, resetRevision := 0.0, s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &resetRevision, ObjectID: element.ID, ModernSize: &reset, TextRuns: sceneTextFor(element).Runs}); err != nil {
		t.Fatal(err)
	}
	resetQuery, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if resetQuery.Has("modern-size") {
		t.Fatal("inherit must remove the override, not persist a sentinel")
	}
	if s.deck.modernSceneText(s.deck.Slides[0].Elements[0]).Size != before.modernSceneText(element).Size {
		t.Fatal("inherit did not restore resolved default")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.deck.modernSceneText(s.deck.Slides[0].Elements[0]).Size != size {
		t.Fatal("Undo inheritance did not restore fitted size")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("Undo did not restore deck")
	}
	for _, invalid := range []float64{.5, -1, 1025, math.NaN(), math.Inf(1)} {
		revision = s.version
		if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: element.ID, ModernSize: &invalid, TextRuns: sceneTextFor(element).Runs}); err == nil {
			t.Fatalf("accepted size %v", invalid)
		}
	}
}

func TestModernDraftWidthIsAtomicAndUndoable(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	s := newNativeEditorSession("Width.md", deck)
	before := cloneDeck(s.deck)
	element := s.deck.Slides[0].Elements[0]
	for _, width := range []float64{0, -1, 201, math.NaN(), math.Inf(1)} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: element.ID, ModernWidth: &width, TextRuns: sceneTextFor(element).Runs}); err == nil {
			t.Fatal("invalid width accepted", width)
		}
		if !reflect.DeepEqual(before, s.deck) {
			t.Fatal("invalid width mutated deck")
		}
	}
	width, revision := 72.5, s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: element.ID, ModernWidth: &width, TextRuns: sceneTextFor(element).Runs}); err != nil {
		t.Fatal(err)
	}
	got := s.deck.Slides[0].Elements[0]
	q, _ := url.ParseQuery(got.Query)
	if q.Get("modern-width") != "72.5" || s.deck.modernSceneText(got).WidthScale != .725 {
		t.Fatal("width not persisted")
	}
	q.Del("modern-width")
	original, _ := url.ParseQuery(element.Query)
	if q.Encode() != original.Encode() || got.Text != element.Text {
		t.Fatal("width changed unrelated data")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("width undo did not restore deck")
	}
}

func TestModernLineSpacingRoundTripAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Spacing.md", deck)
	before := cloneDeck(s.deck)
	spacing, revision := 1.8, s.version
	action := nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "title", ModernLineHeight: &spacing, TextRuns: []sceneRun{{Text: deck.Slides[0].Elements[0].Text}}}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	if sceneTextFor(s.deck.Slides[0].Elements[0]).LineHeight != spacing || len(s.undo) != 1 {
		t.Fatal("spacing-only change was lost")
	}
	path := filepath.Join(t.TempDir(), "Spacing.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.Text == deck.Slides[0].Elements[0].Text {
			found = sceneTextFor(e).LineHeight == spacing
		}
	}
	if !found {
		t.Fatal("line spacing was not saved")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("line spacing undo changed other data")
	}
	for _, invalid := range []float64{0, .49, 4.1, math.NaN(), math.Inf(1)} {
		spacing, revision = invalid, s.version
		if err := s.apply(action); err == nil {
			t.Fatalf("accepted invalid line height %v", invalid)
		}
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("invalid spacing changed deck")
	}
}

func TestModernParagraphSpacingRoundTripAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	s := newNativeEditorSession("Paragraphs.md", deck)
	before := cloneDeck(s.deck)
	leading, trailing, revision := .25, .75, s.version
	action := nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "title", ModernParagraphBefore: &leading, ModernParagraphAfter: &trailing, TextRuns: sceneTextFor(deck.Slides[0].Elements[0]).Runs}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	style := sceneTextFor(s.deck.Slides[0].Elements[0])
	if style.ParagraphBefore != leading || style.ParagraphAfter != trailing || style.LineHeight != 1.25 {
		t.Fatalf("incorrect spacing: %+v", style)
	}
	path := filepath.Join(t.TempDir(), "Paragraphs.md")
	data, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, element := range loaded.Slides[0].Elements {
		if element.Text == deck.Slides[0].Elements[0].Text {
			style := sceneTextFor(element)
			found = style.ParagraphBefore == leading && style.ParagraphAfter == trailing
		}
	}
	if !found {
		t.Fatal("paragraph spacing lost on save")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("spacing undo changed other data")
	}
	for _, invalid := range []float64{-1, 4.01, math.NaN(), math.Inf(1)} {
		leading, revision = invalid, s.version
		if err := s.apply(action); err == nil {
			t.Fatalf("accepted %v", invalid)
		}
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("invalid spacing changed deck")
	}
}

func TestModernFontChoicePreservesRetroAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	deck.Slides[0].Engagement = nil
	deck.Slides[0].EngagementResult = nil
	deck.Slides[0].Elements[0].Query += "&font=custom-retro&glyph=braille"
	s := newNativeEditorSession("Fonts.md", deck)
	before := cloneDeck(s.deck)
	font := "mono"
	revision := s.version
	action := nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "title", ModernFont: &font, TextRuns: []sceneRun{{Text: deck.Slides[0].Elements[0].Text}}}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	e := s.deck.Slides[0].Elements[0]
	q, _ := url.ParseQuery(e.Query)
	if q.Has("font") || q.Has("glyph") || sceneTextFor(e).Family != "KeynopeModernMono, monospace" {
		t.Fatal("font choice retained retired metadata or lost Modern projection")
	}
	if len(s.undo) != 1 {
		t.Fatal("font-only change is not undoable")
	}
	path := filepath.Join(t.TempDir(), "Fonts.md")
	encoded, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, encoded)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.Text == deck.Slides[0].Elements[0].Text {
			found = sceneTextFor(e).FontID == "mono"
		}
	}
	if !found {
		t.Fatal("saved font choice did not reopen")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("font undo changed document")
	}
	font = "unsupported"
	revision = s.version
	if err := s.apply(action); err == nil {
		t.Fatal("unsupported font accepted")
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("rejected font mutated document")
	}
	font = "sans"
	code := Element{Kind: "code", Text: "hello", Query: "modern-font=sans&font=retro"}
	if sceneTextFor(code).Family != "KeynopeModern, sans-serif" {
		t.Fatal("code font override lost")
	}
	code.Query = withModernFont(code.Query, "")
	q, _ = url.ParseQuery(code.Query)
	if q.Has("modern-font") || q.Get("font") != "retro" || sceneTextFor(code).Family != "KeynopeModernMono, monospace" {
		t.Fatal("default font did not restore role default")
	}
}

func TestSceneEditStableTargetRevisionAndUndo(t *testing.T) {
	s := newNativeEditorSession("Scene.md", sceneFixture(t))
	s.savedDeck = cloneDeck(s.deck)
	before := cloneDeck(s.deck)
	response := httptest.NewRecorder()
	s.handleScene(response, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	var scene slideScene
	if err := json.Unmarshal(response.Body.Bytes(), &scene); err != nil || scene.Revision == nil {
		t.Fatalf("missing editor revision: %v", err)
	}
	editable := false
	for _, object := range scene.Objects {
		if object.ID == "body" {
			editable = object.Editable
		}
	}
	if !editable {
		t.Fatal("authored text not editable")
	}
	action := nativeEditorAction{Action: "set-scene-text", SceneRevision: scene.Revision, Slide: 0, ObjectID: "body", TextRuns: []sceneRun{{Text: "Edited "}, {Text: "colour", Color: "#112233"}}}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	if s.deck.Slides[0].Elements[1].Text != "Edited [color=#112233]colour[/color]" || len(s.undo) != 1 || !s.state().Dirty {
		t.Fatal("edit was not one dirty document transaction")
	}
	expected := cloneDeck(before)
	expected.Slides[0].Elements[1].Text = s.deck.Slides[0].Elements[1].Text
	if !reflect.DeepEqual(s.deck, expected) {
		t.Fatal("text edit changed geometry, identities, styles or private workshop data")
	}
	if err := s.apply(action); err == nil {
		t.Fatal("stale scene accepted")
	}
	if !reflect.DeepEqual(s.deck, expected) || len(s.undo) != 1 {
		t.Fatal("rejected stale edit changed state")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) || s.state().Dirty {
		t.Fatal("scene edit undo was not exact")
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, expected) {
		t.Fatal("scene edit redo was not exact")
	}
}

func TestSceneEditRejectsUnsupportedTargets(t *testing.T) {
	deck := cloneDeck(sceneFixture(t))
	before := cloneDeck(deck)
	for _, id := range []string{"image", "line", "missing"} {
		if _, err := applySceneText(&deck, nativeEditorAction{ObjectID: id, TextRuns: []sceneRun{{Text: "No"}}}); err == nil {
			t.Fatalf("accepted unsupported target %s", id)
		}
	}
	if _, err := applySceneText(&deck, nativeEditorAction{ObjectID: "body", TextRuns: []sceneRun{{Text: "No", Color: "red<script>"}}}); err == nil {
		t.Fatal("accepted invalid colour")
	}
	if !reflect.DeepEqual(deck, before) {
		t.Fatal("invalid edits mutated document")
	}
}

func TestSceneShapeLabelEditPreservesOwner(t *testing.T) {
	s := newNativeEditorSession("Shape.md", sceneFixture(t))
	before := cloneDeck(s.deck)
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	var scene slideScene
	if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "box" {
			found = object.Editable && object.Label != nil
		}
	}
	if !found {
		t.Fatal("labelled shape not editable")
	}
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: scene.Revision, ObjectID: "box", TextRuns: []sceneRun{{Text: "New label"}, {Text: " colour", Color: "#55aaff"}}}); err != nil {
		t.Fatal(err)
	}
	owner := s.deck.Slides[0].Elements[2]
	old := before.Slides[0].Elements[2]
	label, _ := shapeLabel(owner)
	oldLabel, _ := shapeLabel(old)
	if label.Text != "New label[color=#55aaff] colour[/color]" || label.Query != oldLabel.Query {
		t.Fatal("label text/style mismatch")
	}
	q, _ := url.ParseQuery(owner.Query)
	oldQ, _ := url.ParseQuery(old.Query)
	q.Del("shape-label")
	oldQ.Del("shape-label")
	if q.Encode() != oldQ.Encode() || owner.ID != old.ID || owner.Kind != old.Kind {
		t.Fatal("label edit changed shape geometry or style")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("shape label undo not exact")
	}
}

func TestSceneCreatesFirstShapeLabelOnlyOnApply(t *testing.T) {
	s := newNativeEditorSession("NewLabel.md", sceneFixture(t))
	before := cloneDeck(s.deck)
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	var scene slideScene
	if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "circle" {
			found = object.Editable && object.Label != nil && len(object.Label.Runs) == 1 && object.Label.Runs[0].Text == ""
		}
	}
	if !found || !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
		t.Fatal("opening empty label draft mutated deck or omitted editor")
	}
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: scene.Revision, ObjectID: "circle", TextRuns: []sceneRun{{Text: ""}}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
		t.Fatal("empty label apply created metadata")
	}
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "circle", TextRuns: []sceneRun{{Text: "First label"}}}); err != nil {
		t.Fatal(err)
	}
	label, ok := shapeLabel(s.deck.Slides[0].Elements[3])
	if !ok || label.Text != "First label" {
		t.Fatal("first label not persisted")
	}
	if len(s.undo) != 1 {
		t.Fatal("first label must be one transaction")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("first label undo not exact")
	}
}

func TestSceneEditBulletKeepsListAndContinuation(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{{ID: "list", Kind: "bullet", Text: "First\nSecond", Query: "top=2&left=5&width=120&height=20"}}}}}
	s := newNativeEditorSession("List.md", deck)
	before := cloneDeck(s.deck)
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "list", TextRuns: []sceneRun{{Text: "First\n  continuation\n"}, {Text: "Second", Color: "#55aaff"}}}); err != nil {
		t.Fatal(err)
	}
	e := s.deck.Slides[0].Elements[0]
	if e.Kind != "bullet" || e.Query != before.Slides[0].Elements[0].Query || e.ID != "list" {
		t.Fatal("list edit changed type, geometry or identity")
	}
	text := sceneTextFor(e)
	if len(text.Paragraphs) != 2 {
		t.Fatal("missing rendered paragraphs")
	}
	var first string
	for _, run := range text.Paragraphs[0].Runs {
		first += run.Text
	}
	if first != "First\ncontinuation" || text.Paragraphs[1].Runs[0].Color != "#55aaff" {
		t.Fatal("list continuation or colour lost")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("list undo was not exact")
	}
}
