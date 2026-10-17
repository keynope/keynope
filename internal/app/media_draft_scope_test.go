package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMediaDraftSelectedAssetCaptureOwnsMetadataOnly(t *testing.T) {
	payload := []byte{1, 2, 3}
	d := Deck{Assets: map[string]DeckAsset{"selected": {Data: payload, Frames: []DeckAssetFrame{{Data: payload, DelayMS: 100}}, Source: &DeckAssetSource{Data: payload, Width: 20, Frames: []DeckAssetFrame{{Data: payload, DelayMS: 200}}}}, "unrelated": {Data: make([]byte, 1<<20)}}}
	assets := captureMediaDraftAsset(d, "selected")
	if len(assets) != 1 {
		t.Fatal("unrelated media captured")
	}
	a := assets["selected"]
	a.Frames[0].DelayMS = 1
	a.Source.Width = 1
	a.Source.Frames[0].DelayMS = 1
	if d.Assets["selected"].Frames[0].DelayMS != 100 || d.Assets["selected"].Source.Width != 20 || d.Assets["selected"].Source.Frames[0].DelayMS != 200 {
		t.Fatal("draft metadata aliases document")
	}
	if &a.Data[0] != &payload[0] || &a.Source.Data[0] != &payload[0] {
		t.Fatal("immutable media bytes copied unnecessarily")
	}
	if captureMediaDraftAsset(d, "missing") != nil {
		t.Fatal("missing asset invented")
	}
}

func TestScopedMediaDraftMatchesFullProjection(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	corpus := powerupProofCorpus(t)
	for _, mode := range []int{-1, 0, 3} {
		for _, image := range corpus["media.md"].Slides[0].Elements {
			d := cloneDeck(corpus["media.md"])
			d.Slides = []Slide{{ID: "unused-a"}, {ID: "unused-b"}, {ID: "selected-slide", Notes: "private", Elements: []Element{{ID: "before", Kind: "text", Text: "Flow context", Query: "render=truetype"}, image}}}
			s := nativeEditorSession{deck: d, version: 7, current: 2}
			index := 2
			if mode >= 0 {
				index = mode
				s.masterMode = true
				s.currentMaster = mode
				if mode == 0 {
					s.deck.Masters.Base.Slide = cloneSlide(d.Slides[2])
					s.deck.Masters.Base.Slide.ID = ""
				} else {
					s.deck.Masters.Base.Slide = Slide{}
					s.deck.Masters.Layouts = []MasterLayout{{ID: "unused-1"}, {ID: "unused-2"}, {ID: "selected", Slide: cloneSlide(d.Slides[2])}}
					s.deck.Masters.Layouts[2].Slide.ID = ""
				}
			}
			before := cloneDeck(s.deck)
			if root := os.Getenv("KEYNOPE_DRAFT_SCOPE_FIXTURE"); root != "" && mode == 3 && image.ID == "alpha" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				name := filepath.Join(root, "media-scopes.md")
				data, err := serializeDeck(name, s.deck)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			revision := s.version
			mask := "ellipse"
			a := nativeEditorAction{Slide: index, SceneMaster: s.masterMode, SceneRevision: &revision, ObjectID: image.ID, Crop: &sceneCrop{Left: .1, Top: .05, Right: .1}, ModernMask: &mask}
			body, _ := json.Marshal(a)
			w := httptest.NewRecorder()
			s.handleMediaDraft(w, httptest.NewRequest("POST", "/api/editor/media-draft", bytes.NewReader(body)))
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(mode, image.ID, w.Code, w.Body.String())
			}
			var got slideScene
			if json.Unmarshal(w.Body.Bytes(), &got) != nil {
				t.Fatal("invalid draft")
			}
			full := cloneDeck(s.deck)
			if mode >= 0 {
				owner := masterSlideAt(&full, index)
				working := Deck{Slides: []Slide{*owner}, Assets: full.Assets}
				mutation := a
				mutation.Slide = 0
				if _, err := applySceneCrop(&working, mutation); err != nil {
					t.Fatal(err)
				}
				*owner = working.Slides[0]
			} else if _, err := applySceneCrop(&full, a); err != nil {
				t.Fatal(err)
			}
			visible := full.ResolveSlide(index, false)
			if mode >= 0 {
				visible = masterViewPreview(full.Masters, index)
			}
			want, err := buildStyledSceneTarget(full, visible, index, 245, 56, true, image.ID)
			if err != nil {
				t.Fatal(err)
			}
			want.Revision, want.Master, want.SlideIndex = &revision, a.SceneMaster, index
			want.BackgroundMedia, want.Diagnostics, want.Build = nil, nil, nil
			encoded, _ := json.Marshal(want)
			var normalized slideScene
			_ = json.Unmarshal(encoded, &normalized)
			if !reflect.DeepEqual(got, normalized) {
				t.Fatalf("scoped media projection differs (%d, %s)", mode, image.ID)
			}
			if !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 {
				t.Fatal("crop draft changed document/history")
			}
		}
	}
}

type mediaDraftContextProbe struct {
	context.Context
	checks int
	change func()
}

func (p *mediaDraftContextProbe) Err() error {
	p.checks++
	if p.checks == 2 {
		p.change()
	}
	return nil
}

func TestMediaDraftRejectsChangeBeforePublication(t *testing.T) {
	s := nativeEditorSession{deck: sceneFixture(t), version: 5}
	revision := s.version
	a := nativeEditorAction{Slide: 0, SceneRevision: &revision, ObjectID: "image", Crop: &sceneCrop{Left: .1}}
	body, _ := json.Marshal(a)
	probe := &mediaDraftContextProbe{Context: context.Background(), change: func() { s.mu.Lock(); s.version++; s.mu.Unlock() }}
	w := httptest.NewRecorder()
	s.handleMediaDraft(w, httptest.NewRequest("POST", "/api/editor/media-draft", bytes.NewReader(body)).WithContext(probe))
	if probe.checks < 2 || w.Code != 409 {
		t.Fatal("stale media draft published", probe.checks, w.Code, w.Body.String())
	}
}
