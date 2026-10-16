package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func clipboardGeometryFixture(t *testing.T) Deck {
	t.Helper()
	d := structuredFixture()
	d.Appearance = &DeckAppearance{Version: 3}
	d.Theme = builtinDeckTheme("clean-light")
	label, _ := encodeShapeLabel(shapeLabelData{Text: "Label", Query: "render=truetype&modern-font=mono&modern-size=32"})
	d.Slides[0].Notes = "PRIVATE-NOTES-MUST-NOT-LEAK"
	d.Slides[0].Elements = append(d.Slides[0].Elements,
		Element{ID: "text", Kind: "text", Text: "A bounded paragraph", Query: "render=truetype&modern-font=sans&modern-size=48&left=110&top=2&width=80&height=12"},
		Element{ID: "natural", Kind: "text", Text: "Natural text", Query: "render=truetype&modern-font=mono&modern-size=48&align=center&top=18"},
		Element{ID: "from", Kind: "shape", Query: setQueryValue("shape=square&width=40&height=16&left=110&top=25&object-rotation=25&shape-stroke=%23ffffff&shape-stroke-width=2", "shape-label", label)},
		Element{ID: "to", Kind: "shape", Query: "shape=rounded&width=40&height=14&right=5&bottom=2&shape-stroke=%23ffffff&shape-stroke-width=2"},
		Element{ID: "wire", Kind: "connector", Query: "connector-from=from&connector-from-side=right&connector-to=to&connector-to-side=left&connector-mode=elbow&connector-width=2&connector-arrows=end"})
	for i := range d.Slides[0].Elements {
		e := &d.Slides[0].Elements[i]
		e.Query = setQueryValue(setQueryValue(e.Query, "group", "group-source"), "object-name", e.ID)
	}
	return d
}

func copyGeometryViaHTTP(t *testing.T, s *nativeEditorSession) ObjectClipboard {
	t.Helper()
	owner := s.deck.Slides[s.current]
	if s.masterMode {
		owner = *masterSlideAt(&s.deck, s.currentMaster)
	}
	ids := []string{}
	for _, e := range owner.Elements {
		ids = append(ids, e.ID)
	}
	body, _ := json.Marshal(map[string]any{"version": s.version, "ids": ids})
	response := httptest.NewRecorder()
	s.handleObjectClipboard(response, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(body)))
	if response.Code != 200 {
		t.Fatal("copy", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("PRIVATE-NOTES")) {
		t.Fatal("geometry leaked notes")
	}
	var bundle ObjectClipboard
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestClipboardGeometryCrossPageFramesGroupsAndHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, sourceSize := range []DeckPageSize{{1, 1920, 1080}, {1, 1440, 1080}, {1, 1080, 1920}} {
		for _, mode := range []string{"slide", "tab", "base", "layout"} {
			sourceDeck := clipboardGeometryFixture(t)
			sourceDeck.PageSize = sourceSize.Clone()
			source := newNativeEditorSession("Source.md", sourceDeck)
			switch mode {
			case "tab":
				source.deck.Slides[0].TabID = "source-tab"
				syncSlideTabs(&source.deck)
			case "base":
				source.masterMode = true
				source.currentMaster = 0
				source.deck.Masters.Base.Slide = cloneSlide(source.deck.Slides[0])
			case "layout":
				source.masterMode = true
				source.currentMaster = 1
				source.deck.Masters.Base.Slide = Slide{PageNumber: "hide"}
				source.deck.Masters.Layouts[0].Slide = cloneSlide(source.deck.Slides[0])
			}
			sourceBefore := cloneDeck(source.deck)
			var original slideScene
			var err error
			if source.masterMode {
				original = *source.deck.masterRenderPreview(source.currentMaster, 245, 56).ModernScene
			} else {
				original, err = buildDocumentSlideScene(source.deck, 0, 245, 56)
			}
			if err != nil {
				t.Fatal(err)
			}
			bundle := copyGeometryViaHTTP(t, source)
			originalByName := map[string]sceneObject{}
			for _, o := range original.Objects {
				originalByName[o.Name] = o
			}
			if bundle.Geometry == nil || len(bundle.Geometry.Bounds) != len(bundle.Elements)-1 {
				t.Fatal("missing owned geometry")
			}
			for _, destinationSize := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
				for _, master := range []bool{false, true} {
					target := newNativeEditorSession("Target.md", Deck{PageSize: destinationSize.Clone(), Theme: source.deck.Theme.Clone(), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}})
					if master {
						target.masterMode = true
						target.currentMaster = 0
						target.deck.Masters.Base.Slide = Slide{PageNumber: "hide"}
					}
					before := cloneDeck(target.deck)
					if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
						t.Fatal(sourceSize, destinationSize, master, err)
					}
					var pasted slideScene
					if master {
						pasted = *target.deck.masterRenderPreview(0, 245, 56).ModernScene
					} else {
						pasted, err = buildDocumentSlideScene(target.deck, 0, 245, 56)
						if err != nil {
							t.Fatal(err)
						}
					}
					if len(pasted.Objects) != len(original.Objects) {
						t.Fatal("missing pasted objects")
					}
					for _, o := range pasted.Objects {
						old := originalByName[o.Name]
						if o.ID == old.ID || o.Group == "" || o.Group == old.Group {
							t.Fatal("identity/group not remapped")
						}
						if o.Kind == "connector" {
							if len(o.Points) < 2 || len(old.Points) < 2 {
								t.Fatal("lost route")
							}
							for _, pair := range [][2]connectorPoint{{old.Points[0], o.Points[0]}, {old.Points[len(old.Points)-1], o.Points[len(o.Points)-1]}} {
								if math.Hypot(pair[0].X-pair[1].X, pair[0].Y-pair[1].Y) > 1e-5 {
									t.Fatal("attachment physical endpoint moved", sourceSize, destinationSize, pair)
								}
							}
							continue
						}
						for _, v := range [][2]float64{{o.Bounds.X, old.Bounds.X}, {o.Bounds.Y, old.Bounds.Y}, {o.Bounds.Width, old.Bounds.Width}, {o.Bounds.Height, old.Bounds.Height}, {o.Rotation, old.Rotation}} {
							if math.Abs(v[0]-v[1]) > 1e-5 {
								t.Fatal("physical frame changed", sourceSize, destinationSize, master, o.Name, old.Bounds, o.Bounds)
							}
						}
						if o.Text != nil && !reflect.DeepEqual(o.Text, old.Text) {
							t.Fatal("physical font/style changed")
						}
					}
					after := cloneDeck(target.deck)
					if err := target.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, target.deck) {
						t.Fatal("paste undo not exact", err)
					}
					if err := target.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, target.deck) {
						t.Fatal("paste redo not exact", err)
					}
					data, err := serializeDeck("Target.md", target.deck)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(data), "geometry\"") {
						t.Fatal("clipboard transport persisted")
					}
					if _, err := parseDeckData("Target.md", data); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !reflect.DeepEqual(source.deck, sourceBefore) {
				t.Fatal("copy/paste changed source")
			}
		}
	}
}

func TestClipboardGeometryInvalidAtomicAndLegacyCompatibility(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	source := newNativeEditorSession("Source.md", clipboardGeometryFixture(t))
	bundle := copyGeometryViaHTTP(t, source)
	valid, _ := json.Marshal(bundle)
	for _, mutate := range []func(*ObjectClipboard){func(b *ObjectClipboard) { b.Geometry.Version = 2 }, func(b *ObjectClipboard) { b.Geometry.Cols = 0 }, func(b *ObjectClipboard) { delete(b.Geometry.Bounds, "text") }, func(b *ObjectClipboard) { b.Geometry.Bounds["unselected"] = sceneRect{Width: 1, Height: 1} }, func(b *ObjectClipboard) {
		v := b.Geometry.Bounds["text"]
		v.X = math.NaN()
		b.Geometry.Bounds["text"] = v
	}} {
		var b ObjectClipboard
		if err := json.Unmarshal(valid, &b); err != nil {
			t.Fatal(err)
		}
		mutate(&b)
		target := newNativeEditorSession("Target.md", Deck{PageSize: &DeckPageSize{1, 1080, 1920}, Slides: []Slide{{}}})
		before := cloneDeck(target.deck)
		version := target.version
		if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &b}); err == nil || target.version != version || !reflect.DeepEqual(before, target.deck) || len(target.undo) != 0 {
			t.Fatal("bad geometry mutated destination", err)
		}
	}
	legacy := bundle
	legacy.Geometry = nil
	same, _, _, err := prepareObjectClipboardPaste(source.deck, bundle)
	if err != nil {
		t.Fatal(err)
	}
	old, _, _, err := prepareObjectClipboardPaste(source.deck, legacy)
	if err != nil {
		t.Fatal(err)
	}
	for i := range old {
		aq, bq := mustQuery(same[i].Query), mustQuery(old[i].Query)
		for _, key := range []string{"group", "connector-from", "connector-to"} {
			aq.Del(key)
			bq.Del(key)
		}
		if !reflect.DeepEqual(aq, bq) {
			t.Fatal("same-canvas queries changed")
		}
	}
	if err := captureClipboardGeometry(context.Background(), &legacy, source.deck, 0, false, 245, 56); err != nil {
		t.Fatal(err)
	}
	if legacy.Geometry == nil {
		t.Fatal("missing direct capture")
	}
}

func TestClipboardGeometryImagesAndHiddenObjects(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	t.Chdir(t.TempDir())
	id := newStableID("cross-page-image")
	asset := clipboardTestAsset(t)
	registerEmbeddedStillAsset(id, asset)
	t.Cleanup(func() {
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	source := newNativeEditorSession("Source.md", Deck{PageSize: &DeckPageSize{1, 1440, 1080}, Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{
		{ID: "image", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: "left=20&top=5&width=30&height=12&image-fit=stretch&brightness=1.2&modern-mask=ellipse&object-name=photo"},
		{ID: "hidden", Kind: "shape", Query: "shape=diamond&left=60&top=5&width=25&height=10&object-hidden=1&object-name=hidden"},
	}}}})
	before := cloneDeck(source.deck)
	bundle := copyGeometryViaHTTP(t, source)
	if len(bundle.Geometry.Bounds) != 2 {
		t.Fatal("hidden/image geometry omitted")
	}
	target := newNativeEditorSession("Target.md", Deck{PageSize: &DeckPageSize{1, 1080, 1920}, Slides: []Slide{{}}})
	if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
		t.Fatal(err)
	}
	for i, e := range target.deck.Slides[0].Elements {
		q := mustQuery(e.Query)
		if e.Kind == "image" {
			if q.Get("brightness") != "1.2" || q.Get("modern-mask") != "ellipse" {
				t.Fatal("image adjustment lost")
			}
		} else {
			if q.Get("object-hidden") != "1" {
				t.Fatal("hidden flag lost")
			}
			target.deck.Slides[0].Elements[i].Query = removeImageQueryKeys(e.Query, "object-hidden")
		}
	}
	scene, err := buildDocumentSlideScene(target.deck, 0, 245, 56)
	if err != nil || len(scene.Objects) != 2 {
		t.Fatal(err, scene.Objects)
	}
	for _, o := range scene.Objects {
		key := "hidden"
		if o.Kind == "image" {
			key = "image"
		}
		old := bundle.Geometry.Bounds[key]
		for _, p := range [][2]float64{{o.Bounds.X, old.X}, {o.Bounds.Y, old.Y}, {o.Bounds.Width, old.Width}, {o.Bounds.Height, old.Height}} {
			if math.Abs(p[0]-p[1]) > 1e-6 {
				t.Fatal("image/hidden frame changed", old, o.Bounds)
			}
		}
	}
	if !reflect.DeepEqual(before, source.deck) {
		t.Fatal("copy/unhide projection changed source")
	}
}

func TestClipboardGeometryAmbiguousJSONAndPreparationGridOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, geometry := range []string{
		`{"version":1,"width":1920,"Width":1440,"height":1080,"cols":245,"rows":56,"bounds":{"one":{"x":0,"y":0,"width":10,"height":10}}}`,
		`{"version":1,"width":1920,"height":1080,"cols":245,"rows":56,"bounds":{"one":{"x":0,"X":1,"y":0,"width":10,"height":10}}}`,
		`{"version":1,"width":1920,"height":1080,"cols":245,"rows":56,"bounds":{"one":{"y":0,"width":10,"height":10}}}`,
	} {
		raw := `{"version":1,"elements":[{"kind":"text","id":"one"}],"geometry":` + geometry + `}`
		var bundle ObjectClipboard
		if err := json.Unmarshal([]byte(raw), &bundle); err == nil {
			t.Fatal("ambiguous/incomplete geometry accepted")
		}
	}
	s := newNativeEditorSession("Target.md", Deck{Slides: []Slide{{}}})
	bundle, gate := blockedClipboardBundle(t)
	before, revision := cloneDeck(s.deck), s.version
	done := make(chan error, 1)
	go func() {
		done <- s.prepareAndApplyObjectClipboard(context.Background(), nativeEditorAction{Action: "paste-objects", Clipboard: &bundle})
	}()
	awaitClipboardDecode(t, gate)
	authoredTerminalWidth, authoredTerminalHeight = 160, 90
	close(gate.release)
	if err := awaitClipboardPaste(t, done); !errors.Is(err, errClipboardPasteStale) {
		t.Fatal("paste ignored changed destination grid", err)
	}
	if s.version != revision || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("stale grid paste changed target")
	}
}

func TestClipboardGeometryBrowserFixtures(t *testing.T) {
	output := os.Getenv("KEYNOPE_CLIPBOARD_GEOMETRY_PROOF")
	if output == "" {
		t.Skip("optional browser fixture")
	}
	useTestAuthoredSize(t, 245, 56)
	source := clipboardGeometryFixture(t)
	source.PageSize = &DeckPageSize{1, 1440, 1080}
	target := Deck{PageSize: &DeckPageSize{1, 1920, 1080}, Appearance: &DeckAppearance{Version: 3}, Theme: source.Theme.Clone(), Slides: []Slide{{ID: "destination"}}}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(output, "source.md"), source); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(output, "target.md"), target); err != nil {
		t.Fatal(err)
	}
}

func TestClipboardGeometryDifferentGridAndRepresentability(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	source := newNativeEditorSession("Source.md", clipboardGeometryFixture(t))
	original, err := buildDocumentSlideScene(source.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	bundle := copyGeometryViaHTTP(t, source)
	// The same physical page with a different authoring grid is still a
	// geometry conversion, not the same-canvas legacy fast path.
	authoredTerminalWidth, authoredTerminalHeight = 320, 90
	target := newNativeEditorSession("Target.md", Deck{Theme: source.deck.Theme.Clone(), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}})
	if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
		t.Fatal(err)
	}
	pasted, err := buildDocumentSlideScene(target.deck, 0, 320, 90)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range original.Objects {
		if old.Kind == "connector" {
			continue // grid-based connector paint has a disclosed warning.
		}
		found := false
		for _, next := range pasted.Objects {
			if next.Name != old.Name {
				continue
			}
			found = true
			for _, pair := range [][2]float64{{next.Bounds.X, old.Bounds.X}, {next.Bounds.Y, old.Bounds.Y}, {next.Bounds.Width, old.Bounds.Width}, {next.Bounds.Height, old.Bounds.Height}} {
				if math.Abs(pair[0]-pair[1]) > 1e-5 {
					t.Fatal("changed-grid physical frame moved", old.Name, old.Bounds, next.Bounds)
				}
			}
		}
		if !found {
			t.Fatal("changed-grid object missing", old.Name)
		}
	}
	// Never silently round a frame that the target metadata cannot represent.
	for _, width := range []float64{0.01, 1e8} {
		bad := bundle
		geometry := *bundle.Geometry
		geometry.Bounds = make(map[string]sceneRect, len(bundle.Geometry.Bounds))
		for id, frame := range bundle.Geometry.Bounds {
			geometry.Bounds[id] = frame
		}
		frame := geometry.Bounds["from"]
		frame.Width = width
		geometry.Bounds["from"] = frame
		bad.Geometry = &geometry
		before, revision, history := cloneDeck(target.deck), target.version, len(target.undo)
		if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bad}); err == nil {
			t.Fatal("unrepresentable width accepted", width)
		}
		if !reflect.DeepEqual(before, target.deck) || revision != target.version || history != len(target.undo) {
			t.Fatal("unrepresentable frame changed destination")
		}
	}
}

func TestClipboardGeometryRouteAdmission(t *testing.T) {
	for _, route := range []string{
		`[{"x":0,"X":1,"y":0},{"x":20,"y":0}]`,
		`[{"y":0},{"x":20,"y":0}]`,
		`[{"x":0,"y":0},{"x":20,"y":0},{"x":20,"y":10}]`,
		`[{"x":0,"y":0},{"x":20,"y":5},{"x":20,"y":10},{"x":30,"y":10}]`,
		`[` + strings.Repeat(`{"x":0,"y":0},`, 128) + `{"x":10,"y":0}]`,
	} {
		raw := `{"version":1,"elements":[{"kind":"connector","id":"wire"}],"geometry":{"version":1,"width":1920,"height":1080,"cols":245,"rows":56,"bounds":{},"routes":{"wire":` + route + `}}}`
		var bundle ObjectClipboard
		err := json.Unmarshal([]byte(raw), &bundle)
		if err == nil {
			err = bundle.Geometry.validate(bundle.Elements)
		}
		if err == nil {
			t.Fatal("ambiguous, incomplete, oversized or nonorthogonal route accepted", route)
		}
	}
}
