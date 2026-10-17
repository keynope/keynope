package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestScopedShapeAdjustmentMatchesFullProjection(t *testing.T) {
	for _, mode := range []int{-1, 0, 3} {
		s := shapeAdjustmentSession(false)
		s.deck.Theme = builtinDeckTheme("clean-light")
		if mode >= 0 {
			s.deck.Masters.Base.Slide = Slide{FG: "#445566", FGSet: true}
			if mode == 0 {
				s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			} else {
				s.deck.Masters.Layouts = []MasterLayout{{ID: "unrelated-1"}, {ID: "unrelated-2"}, {ID: "selected", Slide: cloneSlide(s.deck.Slides[0])}}
			}
			s.masterMode = true
			s.currentMaster = mode
		}
		s.deck.Assets = map[string]DeckAsset{"unrelated": {Data: make([]byte, 1<<20)}}
		heavy := wordStyleHeavyFixture(t)
		s.deck.Objects = heavy.Objects
		s.deck.Slides = append(s.deck.Slides, heavy.Slides...)
		before := cloneDeck(s.deck)
		a := shapeAdjustmentAction(s, .55)
		if mode >= 0 {
			a.Slide = mode
		}
		data, _ := json.Marshal(a)
		w := httptest.NewRecorder()
		s.handleShapeAdjustmentDraft(w, httptest.NewRequest("POST", "/api/editor/shape-adjustment-draft", bytes.NewReader(data)))
		if w.Code != 200 {
			t.Fatal(mode, w.Code, w.Body.String())
		}
		var got slideScene
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		full := cloneDeck(s.deck)
		source := &full.Slides[0]
		if mode >= 0 {
			source = masterSlideAt(&full, mode)
		}
		if _, err := applyShapeAdjustment(source, a, ""); err != nil {
			t.Fatal(err)
		}
		visible := full.ResolveSlide(0, false)
		if mode >= 0 {
			visible = full.applyShapeStrokeDefaults(masterViewPreview(full.Masters, mode))
		}
		want, err := buildStyledSceneTargetsMeasured(full, visible, a.Slide, 245, 56, true, map[string]bool{"arrow": true, "link": true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		want.Revision, want.Master, want.SlideIndex = a.SceneRevision, a.SceneMaster, a.Slide
		annotateSceneEditing(&want, full, source.Elements)
		want.BackgroundMedia, want.Diagnostics, want.Build = nil, nil, nil
		encoded, _ := json.Marshal(want)
		var normalized slideScene
		if json.Unmarshal(encoded, &normalized) != nil {
			t.Fatal("bad cold scene")
		}
		if !reflect.DeepEqual(got, normalized) {
			t.Fatalf("scoped shape draft changed full projection (mode %d)", mode)
		}
		if !reflect.DeepEqual(before, s.deck) || len(s.undo) != 0 {
			t.Fatal("draft mutated source/history")
		}
	}
}

func TestShapeAdjustmentDraftRejectsAmbiguousMasterOwner(t *testing.T) {
	s := shapeAdjustmentSession(false)
	s.deck.Masters.Layouts = []MasterLayout{{ID: "duplicate", Slide: cloneSlide(s.deck.Slides[0])}, {ID: "duplicate", Slide: cloneSlide(s.deck.Slides[0])}}
	s.masterMode = true
	s.currentMaster = 2
	a := shapeAdjustmentAction(s, .55)
	a.Slide = 2
	data, _ := json.Marshal(a)
	w := httptest.NewRecorder()
	s.handleShapeAdjustmentDraft(w, httptest.NewRequest("POST", "/api/editor/shape-adjustment-draft", bytes.NewReader(data)))
	if w.Code != 400 {
		t.Fatal("ambiguous master preview accepted", w.Code, w.Body.String())
	}
}
