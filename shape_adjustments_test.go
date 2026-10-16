package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestShapeAdjustmentHandleTracks(t *testing.T) {
	for _, definition := range shapeDefinitions {
		for _, aspect := range []float64{.2, 1, 5} {
			for _, extreme := range []string{"default", "min", "max"} {
				q := url.Values{"shape": {definition.ID}}
				for _, a := range definition.Adjustments {
					v := a.Default
					if extreme == "min" {
						v = a.Min
					}
					if extreme == "max" {
						v = a.Max
					}
					q.Set("shape-"+a.Key, jsonFloat(v))
				}
				geometry := shapeGeometry(shapeSpecFromValues(q), aspect)
				if len(geometry.Handles) != len(definition.Adjustments) {
					t.Fatal("missing shape handles", definition.ID)
				}
				for _, h := range geometry.Handles {
					v := h.Vector
					den := v.X*v.X + v.Y*v.Y
					inverse := ((h.Point.X-h.Origin.X)*v.X + (h.Point.Y-h.Origin.Y)*v.Y) / den
					if den <= 0 || math.IsNaN(inverse) || math.Abs(inverse-h.Value) > 1e-10 || h.Point.X < 0 || h.Point.X > 1 || h.Point.Y < 0 || h.Point.Y > 1 {
						t.Fatal("invalid unit-space adjustment track", definition.ID, h)
					}
				}
			}
		}
	}
}

func jsonFloat(value float64) string { b, _ := json.Marshal(value); return string(b) }

func shapeAdjustmentSession(master bool) *nativeEditorSession {
	s := newNativeEditorSession("Shapes.md", Deck{Slides: []Slide{{PageNumber: pageNumberHide, Elements: []Element{
		{ID: "arrow", Kind: "shape", Query: "shape=arrow-right&top=4&left=8&width=30&height=12&fg=%23ff0055&object-rotation=15"},
		{ID: "other", Kind: "shape", Query: "shape=square&top=4&left=70&width=12&height=10"},
		{ID: "link", Kind: "connector", Query: "connector-from=arrow&connector-from-side=right&connector-to=other&connector-to-side=left"},
		{ID: "unrelated-text", Kind: "text", Text: "Do not project me", Query: "render=truetype&top=20&left=20"},
	}}}})
	if master {
		s.masterMode = true
		*masterSlideAt(&s.deck, 0) = cloneSlide(s.deck.Slides[0])
	}
	return s
}

func shapeAdjustmentAction(s *nativeEditorSession, value float64) nativeEditorAction {
	revision := s.version
	return nativeEditorAction{Action: "set-shape-adjustment", SceneMaster: s.masterMode, SceneRevision: &revision, Slide: 0, ObjectID: "arrow", ShapeAdjustment: &shapeAdjustmentEdit{Key: "depth", Value: value}}
}

func TestShapeAdjustmentCommandsOwnedAndAtomic(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := shapeAdjustmentSession(master)
		before := cloneDeck(s.deck)
		if err := s.apply(shapeAdjustmentAction(s, .55)); err != nil {
			t.Fatal(err)
		}
		slide := s.deck.Slides[0]
		if master {
			slide = *masterSlideAt(&s.deck, 0)
		}
		q, _ := url.ParseQuery(slide.Elements[nativeEditorElementIndex(slide.Elements, -1, "arrow")].Query)
		if q.Get("shape-depth") != "0.55" || q.Get("fg") != "#ff0055" || q.Get("object-rotation") != "15" || len(s.undo) != 1 {
			t.Fatal("adjustment changed unrelated properties or history", q)
		}
		version := s.version
		if err := s.apply(shapeAdjustmentAction(s, .55)); err != nil || s.version != version || len(s.undo) != 1 {
			t.Fatal("no-op adjustment changed history", err)
		}
		data, err := serializeDeck("Shapes.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseDeckData("Shapes.md", data); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("undo lost original shape", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		for _, problem := range []string{"value", "key", "missing", "other-kind", "stale", "scope", "locked", "group"} {
			s := shapeAdjustmentSession(master)
			a := shapeAdjustmentAction(s, .6)
			target := &s.deck.Slides[0]
			if master {
				target = masterSlideAt(&s.deck, 0)
			}
			switch problem {
			case "value":
				a.ShapeAdjustment.Value = 1e30
			case "key":
				a.ShapeAdjustment.Key = "not-an-adjustment"
			case "missing":
				a.ObjectID = "missing"
			case "other-kind":
				a.ObjectID = "unrelated-text"
			case "stale":
				v := s.version - 1
				a.SceneRevision = &v
			case "scope":
				a.SceneMaster = !master
			case "locked":
				target.Elements[nativeEditorElementIndex(target.Elements, -1, "arrow")].Query += "&object-locked=1"
			case "group":
				target.Elements[nativeEditorElementIndex(target.Elements, -1, "arrow")].Query += "&group=collapsed"
			}
			assertEditorFailureUnchanged(t, s, a)
		}
	}
}

func TestShapeAdjustmentDraftIsRevisionedReadOnlyAndNarrow(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := shapeAdjustmentSession(master)
		before, state := cloneDeck(s.deck), s.version
		request := func(a nativeEditorAction, ctx context.Context) *httptest.ResponseRecorder {
			data, _ := json.Marshal(a)
			r := httptest.NewRequest(http.MethodPost, "/api/editor/shape-adjustment-draft", bytes.NewReader(data)).WithContext(ctx)
			w := httptest.NewRecorder()
			s.handleShapeAdjustmentDraft(w, r)
			return w
		}
		w := request(shapeAdjustmentAction(s, .55), context.Background())
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
		var scene slideScene
		if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, o := range scene.Objects {
			ids[o.ID] = true
			if o.ID == "arrow" && !o.Editable {
				t.Fatal("draft lost editor-only shape annotations")
			}
			if o.ID == "arrow" && o.ShapeGeometry.Spec.Adjustments["depth"] != .55 {
				t.Fatal("draft did not apply geometry")
			}
		}
		if !ids["arrow"] || !ids["link"] || ids["other"] || ids["unrelated-text"] || len(ids) != 2 || scene.Revision == nil || *scene.Revision != state {
			t.Fatal("preview encoded unrelated consumers", ids)
		}
		if !reflect.DeepEqual(before, s.deck) || s.version != state || len(s.undo) != 0 {
			t.Fatal("preview changed authored state")
		}
		stale := shapeAdjustmentAction(s, .6)
		v := state - 1
		stale.SceneRevision = &v
		if request(stale, context.Background()).Code != http.StatusConflict {
			t.Fatal("stale draft accepted")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if request(shapeAdjustmentAction(s, .6), ctx).Body.Len() != 0 {
			t.Fatal("cancelled draft returned a scene")
		}
	}
}
