package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func mutationPreviewFixture(t testing.TB) Deck {
	t.Helper()
	d := structuredFixture()
	pixels := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 80, G: 170, B: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: encoded.Bytes()}
	d.Assets = map[string]DeckAsset{"preview-kept": asset, "unrelated": {Data: make([]byte, 1<<20)}}
	registerEmbeddedStillAsset("preview-kept", asset)
	owner := d.Slides[0]
	owner.Notes = "PRIVATE NOTES"
	owner.EngagementResult = &EngagementResult{Version: 1, State: map[string]json.RawMessage{"private": json.RawMessage(`"PRIVATE RESULT"`)}}
	owner.BackgroundMedia = &slideBackgroundMedia{AssetID: "preview-kept", Fit: "cover", Opacity: .5, OpacitySet: true}
	owner.BackgroundMediaSet = true
	owner.Elements = append(owner.Elements, Element{ID: "preview-image", Kind: "image", AssetID: "preview-kept", Path: embeddedAssetPath("preview-kept", asset), Query: "top=1&left=150&width=40&height=12&image-fit=contain&modern-mask=rounded"}, Element{ID: "preview-text", Kind: "text", Text: "Flowing text after the table", Query: "render=truetype&modern-size=65&left=10&width=180&height=6&slide=1"},
		Element{ID: "diagram-a", Kind: "shape", Query: "shape=circle&top=20&left=145&width=20&height=10"}, Element{ID: "diagram-b", Kind: "shape", Query: "shape=square&top=40&left=195&width=20&height=10"}, Element{ID: "diagram-line", Kind: "connector", Query: "connector-from=diagram-a&connector-to=diagram-b&connector-from-side=right&connector-to-side=left&connector-mode=elbow&connector-arrows=end"})
	d.Slides = []Slide{{ID: "other-0", Notes: "PRIVATE OTHER", Elements: []Element{{ID: "other-element", Kind: "text", Text: strings.Repeat("unrelated", 2048)}}}, {ID: "other-1"}, owner}
	d.Slides[2].LayoutID = "chosen"
	d.Theme = builtinDeckTheme("clean-light")
	d.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Slide: Slide{Elements: []Element{{ID: "base-shape", Kind: "shape", Query: "top=1&left=1&width=4&height=4&shape=square"}}}}, Layouts: []MasterLayout{{ID: "unused-a", Slide: cloneSlide(d.Slides[0])}, {ID: "unused-b"}, {ID: "chosen", Slide: Slide{ID: "layout-source", Elements: []Element{{ID: "layout-text", Kind: "text", Text: "Master caption", Query: "top=1&left=10&width=100&height=5&render=truetype&modern-size=40"}}}}}}
	table, _ := newTableData(12, 12)
	d.Objects.Tables["unrelated-table"] = table
	d.Objects.Charts["unrelated-chart"] = newChartData("pie")
	d.Objects.Builds["unrelated-build"] = SlideBuild{Version: 1}
	return d
}

func TestMutationPreviewScopeMatchesFullResponse(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []int{-1, 0, 3} {
		for _, name := range []string{"", "inline-edit", "frozen-image"} {
			t.Run(fmt.Sprintf("%d/%s", mode, name), func(t *testing.T) {
				d := mutationPreviewFixture(t)
				owner := cloneSlide(d.Slides[2])
				if mode == 0 {
					d.Masters.Base.Slide = owner
				} else if mode > 0 {
					d.Masters.Layouts[mode-1].Slide = owner
				}
				s := newNativeEditorSession("preview.md", d, false, false)
				s.current = 2
				if mode >= 0 {
					s.masterMode, s.currentMaster = true, mode
				}
				index := 2
				if mode >= 0 {
					index = mode
				}
				target := 0
				if name == "inline-edit" {
					target = 3
				} else if name == "frozen-image" {
					target = 2
				}
				draft := owner.Elements[target]
				draft.Query = setQueryValue(draft.Query, "width", "95.25")
				revision := s.version
				a := nativeEditorAction{Element: target, ElementData: &draft, SceneRevision: &revision, Slide: index, SceneMaster: mode >= 0, Cols: 245, Rows: 56, Name: name, Cursor: 3}
				before := cloneDeck(s.deck)
				body, _ := json.Marshal(a)
				w := httptest.NewRecorder()
				s.handlePreview(w, httptest.NewRequest("POST", "/api/editor/preview", bytes.NewReader(body)))
				if w.Code != 200 || w.Header().Get("X-Keynope-Preview-Scope") != "slide" {
					t.Fatal(w.Code, w.Header(), w.Body.String())
				}
				full := cloneDeckForRender(s.deck)
				original := &full.Slides[2]
				if mode >= 0 {
					original = masterSlideAt(&full, mode)
				}
				original.Elements[target] = standardTextElement(preserveRunStyles(original.Elements[target], draft))
				frozen := name == "frozen-image"
				if frozen {
					original.Elements[target].Query = setQueryValue(original.Elements[target].Query, "keynope_freeze", "1")
				}
				preview := full.slideRenderPreview(2, 245, 56)
				count := len(full.Slides)
				if mode >= 0 {
					preview = full.masterRenderPreview(mode, 245, 56)
					count = len(full.Masters.Layouts) + 1
				}
				annotateSceneEditing(preview.ModernScene, full, original.Elements)
				pages := exportSlidePages(preview, index, count, 245, 56)
				if frozen {
					pages = exportSlidePagesFrozen(preview, index, count, 245, 56)
				}
				want := httptest.NewRecorder()
				writeNativeEditorPreview(want, a, preview, pages, 245, 56)
				if !bytes.Equal(w.Body.Bytes(), want.Body.Bytes()) {
					t.Fatal("scoped response differs from complete reference snapshot", mode, name)
				}
				if !reflect.DeepEqual(before, s.deck) || s.version != revision || len(s.undo) != 0 {
					t.Fatal("preview changed document/history")
				}
			})
		}
	}
}

func TestMutationPreviewScopeDependenciesAndOwnership(t *testing.T) {
	d := mutationPreviewFixture(t)
	capture, scoped, err := captureMutationPreviewDeck(d, 2, false, nil)
	if err != nil || !scoped {
		t.Fatal(err, "unexpected fallback")
	}
	if len(capture.Slides) != 3 || len(capture.Slides[0].Elements) != 0 || capture.Slides[0].Notes != "" || capture.Slides[2].Notes != "" || capture.Slides[2].EngagementResult != nil || len(capture.Masters.Layouts[0].Slide.Elements) != 0 || len(capture.Assets) != 1 || len(capture.Objects.Tables) != 1 || len(capture.Objects.Charts) != 1 || len(capture.Objects.Builds) != 1 {
		t.Fatal("unrelated input retained")
	}
	if &capture.Assets["preview-kept"].Data[0] != &d.Assets["preview-kept"].Data[0] {
		t.Fatal("immutable pixels copied")
	}
	// A draft can switch its source/payload before it is committed. Capture
	// those references as well as the current slide's original references.
	d.Assets["draft-source"] = d.Assets["preview-kept"]
	draft := Element{ID: "preview-image", Kind: "image", AssetID: "draft-source"}
	changed, scoped, err := captureMutationPreviewDeck(d, 2, false, &draft)
	if err != nil || !scoped || len(changed.Assets) != 2 {
		t.Fatal("draft source omitted", err)
	}
	d.Objects.Charts["draft-chart"] = newChartData("bar")
	draft = Element{ID: "chart-element", Kind: "chart", ObjectID: "draft-chart"}
	changed, scoped, err = captureMutationPreviewDeck(d, 2, false, &draft)
	if err != nil || !scoped || len(changed.Objects.Charts) != 2 {
		t.Fatal("draft payload omitted", err)
	}
	delete(d.Assets, "draft-source")
	delete(d.Objects.Charts, "draft-chart")
	capture.Objects.Tables["table-1"].Cells[1][0].Text = "Changed"
	capture.Objects.Charts["chart-1"].Series[0].Values[0] = 99
	capture.Masters.Layouts[2].Slide.Elements[0].Text = "Changed"
	capture.Slides[2].BackgroundMedia.Query = "brightness=2"
	if d.Objects.Tables["table-1"].Cells[1][0].Text != "One" || d.Objects.Charts["chart-1"].Series[0].Values[0] != 1 || d.Masters.Layouts[2].Slide.Elements[0].Text != "Master caption" || d.Slides[2].BackgroundMedia.Query != "" {
		t.Fatal("capture aliases mutable source")
	}
	for _, kind := range []string{"element", "registry", "table-cell", "media"} {
		candidate := cloneDeck(d)
		extra := jsonExtensions{"future": json.RawMessage(`{"ref":"unrelated"}`)}
		switch kind {
		case "element":
			candidate.Slides[2].Elements[0].Extra = &extra
		case "registry":
			candidate.Objects.Extra = &extra
		case "table-cell":
			candidate.Objects.Tables["table-1"].Cells[0][0].Extra = &extra
		case "media":
			asset := candidate.Assets["preview-kept"]
			asset.Extra = &extra
			candidate.Assets["preview-kept"] = asset
		}
		fallback, scoped, err := captureMutationPreviewDeck(candidate, 2, false, nil)
		if err != nil || scoped || len(fallback.Slides[0].Elements) == 0 || len(fallback.Assets) != 2 {
			t.Fatal(kind, "unknown dependencies were pruned", err)
		}
	}
}

func BenchmarkMutationPreviewCapture(b *testing.B) {
	d := mutationPreviewFixture(b)
	for i := 0; i < 48; i++ {
		table, _ := newTableData(20, 20)
		d.Objects.Tables[fmt.Sprintf("heavy-%d", i)] = table
		d.Slides = append(d.Slides, Slide{ID: fmt.Sprintf("unused-%d", i), Elements: []Element{{ID: fmt.Sprintf("unused-owner-%d", i), Kind: "table", ObjectID: fmt.Sprintf("heavy-%d", i)}}})
	}
	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = cloneDeckForRender(d)
		}
	})
	b.Run("current-slide", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _, err := captureMutationPreviewDeck(d, 2, false, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
