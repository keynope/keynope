package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCropPreservesNaturalImageFrame(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, style := range []string{"modern", "retro"} {
		for _, mode := range []string{"normal", "tab", "base", "layout", "inherited"} {
			for page, size := range []*DeckPageSize{nil, {Version: 1, Width: 1080, Height: 1920}, {Version: 1, Width: 2560, Height: 1080}} {
				t.Run(fmt.Sprintf("%s/%s/page%d", style, mode, page), func(t *testing.T) {
					d := sceneFixture(t)
					d.PageSize = size.Clone()
					d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
					for i, e := range d.Slides[0].Elements {
						if e.ID == "image" {
							d.Slides[0].Elements[i].Query = "image-style=" + style + "&align=center&top=10&width=100&height=30&brightness=1.4"
						}
					}
					if mode == "inherited" {
						var image Element
						for _, e := range d.Slides[0].Elements {
							if e.ID == "image" {
								image = e
							}
						}
						inherited := strokeInheritanceFixture()
						inherited.Assets = d.Assets
						inherited.PageSize = size.Clone()
						master := &inherited.Masters.Layouts[0].Slide.Elements[0]
						master.Kind, master.AssetID, master.Query = "image", image.AssetID, image.Query
						child := &inherited.Slides[0].Elements[0]
						child.ID, child.Kind, child.AssetID, child.Text, child.Query = "image", "image", image.AssetID, "", "top=10&brightness=1.4"
						d = inherited
					}
					s := newNativeEditorSession("Crop frame.md", d)
					if mode == "tab" {
						s.deck.Slides[0].TabID = "crop-tab"
						syncSlideTabs(&s.deck)
					}
					index := 0
					if mode == "base" || mode == "layout" {
						s.masterMode = true
						if mode == "layout" {
							index = 1
						}
						s.currentMaster = index
						*masterSlideAt(&s.deck, index) = cloneSlide(s.deck.Slides[0])
						masterSlideAt(&s.deck, index).ID = "crop-master"
					}
					bounds := func() sceneRect {
						resolved := s.deck.ResolveSlide(0, false)
						if s.masterMode {
							resolved = s.deck.applyShapeStrokeDefaults(masterViewPreview(s.deck.Masters, index))
						}
						scene, err := buildStyledSceneTarget(s.deck, resolved, index, 245, 56, true, "image")
						if err != nil {
							t.Fatal(err)
						}
						for _, o := range scene.Objects {
							if o.ID == "image" {
								return o.Bounds
							}
						}
						t.Fatal("missing image")
						return sceneRect{}
					}
					before := bounds()
					original := cloneDeck(s.deck)
					revision := s.version
					action := nativeEditorAction{Action: "set-scene-crop", Slide: index, SceneMaster: s.masterMode, SceneRevision: &revision, ObjectID: "image", Crop: &sceneCrop{Left: .3, Top: .1}}
					body, _ := json.Marshal(action)
					w := httptest.NewRecorder()
					s.handleMediaDraft(w, httptest.NewRequest("POST", "/api/editor/media-draft", bytes.NewReader(body)))
					if w.Code != 200 {
						t.Fatal("draft", w.Code, w.Body.String())
					}
					var draft slideScene
					if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(original, s.deck) {
						t.Fatal("draft mutated source")
					}
					if err := s.apply(action); err != nil {
						t.Fatal(err)
					}
					after := bounds()
					if len(draft.Objects) != 1 || draft.Objects[0].Bounds != after {
						t.Fatal("preview/commit frame mismatch", draft.Objects, after)
					}
					for _, pair := range [][2]float64{{before.X, after.X}, {before.Y, after.Y}, {before.Width, after.Width}, {before.Height, after.Height}} {
						if math.Abs(pair[0]-pair[1]) > 1e-6 {
							t.Fatalf("crop changed frame: %+v -> %+v", before, after)
						}
					}
					accepted := cloneDeck(s.deck)
					if !reflect.DeepEqual(original.Assets, accepted.Assets) {
						t.Fatal("crop changed source assets")
					}
					revision = s.version
					if err := s.apply(action); err != nil || s.version != revision {
						t.Fatal("repeat crop", err)
					}
					if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(original, s.deck) {
						t.Fatal("crop undo", err)
					}
					if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(accepted, s.deck) {
						t.Fatal("crop redo", err)
					}
					data, err := serializeDeck("Crop.md", s.deck)
					if err != nil {
						t.Fatal(err)
					}
					s.deck, err = parseDeckData("Crop.md", data)
					if err != nil {
						t.Fatal(err)
					}
					reopened := bounds()
					for _, pair := range [][2]float64{{after.X, reopened.X}, {after.Y, reopened.Y}, {after.Width, reopened.Width}, {after.Height, reopened.Height}} {
						if math.Abs(pair[0]-pair[1]) > 1e-6 {
							t.Fatal("reopen frame", after, reopened)
						}
					}
				})
			}
		}
	}
}

func TestCropFrameBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_CROP_FRAME_PROOF")
	if root == "" {
		t.Skip("optional crop browser fixture")
	}
	d := powerupProofCorpus(t)["media.md"]
	sources := append([]Element(nil), d.Slides[0].Elements...)
	d.Slides = nil
	for _, style := range []string{"modern", "retro"} {
		for _, e := range sources {
			e.Query = "image-style=" + style + "&align=center&top=10&width=100&height=30&brightness=1.2&object-rotation=13"
			d.Slides = append(d.Slides, Slide{ID: style + "-" + e.ID, Elements: []Element{e}})
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "crop-frames.md"), d); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "inherited.md"), cropFieldOwnershipFixture(t)); err != nil {
		t.Fatal(err)
	}
}
