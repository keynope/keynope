package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestScopedTableDraftMatchesFullProjection(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []int{-1, 0, 3} {
		s := newNativeEditorSession("draft.md", structuredFixture(), false, false)
		owner := cloneSlide(s.deck.Slides[0])
		s.deck.Theme = builtinDeckTheme("clean-light")
		s.deck.Assets = map[string]DeckAsset{"unrelated": {Data: make([]byte, 1<<20)}}
		s.deck.Slides = []Slide{{ID: "other-a"}, {ID: "other-b"}, owner}
		s.current = 2
		if mode >= 0 {
			s.masterMode, s.currentMaster = true, mode
			if mode == 0 {
				s.deck.Masters.Base.Slide = owner
			} else {
				s.deck.Masters.Layouts = []MasterLayout{{ID: "unused-a"}, {ID: "unused-b"}, {ID: "chosen", Slide: owner}}
			}
		}
		heavy := wordStyleHeavyFixture(t)
		for id, data := range heavy.Objects.Tables {
			s.deck.Objects.Tables[id] = data
		}
		before := cloneDeck(s.deck)
		revision, index := s.version, s.current
		if mode >= 0 {
			index = mode
		}
		data := cloneTable(s.deck.Objects.Tables["table-1"])
		data.Cells[1][0].Text = "Scoped table"
		a := nativeEditorAction{Slide: index, SceneMaster: mode >= 0, SceneRevision: &revision, ObjectID: "table-element", TableData: &data}
		body, _ := json.Marshal(a)
		w := httptest.NewRecorder()
		s.handleTableDraft(w, httptest.NewRequest("POST", "/api/editor/table-draft", bytes.NewReader(body)))
		if w.Code != 200 {
			t.Fatal(mode, w.Code, w.Body.String())
		}
		var got slideScene
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		full := cloneDeck(s.deck)
		full.Objects.Tables["table-1"] = data
		visible := full.ResolveSlide(index, false)
		if mode >= 0 {
			visible = masterViewPreview(full.Masters, index)
		}
		want, err := buildStyledSceneTarget(full, visible, index, 245, 56, true, a.ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		want.Master, want.SlideIndex, want.Revision = a.SceneMaster, index, &revision
		want.Diagnostics, want.BackgroundMedia, want.Build = nil, nil, nil
		encoded, _ := json.Marshal(want)
		var normalized slideScene
		_ = json.Unmarshal(encoded, &normalized)
		if !reflect.DeepEqual(got, normalized) {
			t.Fatalf("scoped table differs from full projection: mode %d", mode)
		}
		if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 || s.version != revision {
			t.Fatal("draft changed document/history")
		}
	}
}

type tableDraftContextProbe struct {
	context.Context
	session *nativeEditorSession
	change  func(*nativeEditorSession)
	calls   int
}

func (p *tableDraftContextProbe) Err() error {
	p.calls++
	if p.calls == 2 {
		p.session.mu.Lock()
		p.change(p.session)
		p.session.mu.Unlock()
	}
	return nil
}

func TestTableDraftRejectsContextChangeBeforePublication(t *testing.T) {
	for _, kind := range []string{"slide", "group", "revision", "mode"} {
		t.Run(kind, func(t *testing.T) {
			s := newNativeEditorSession("draft.md", structuredFixture(), false, false)
			revision := s.version
			data := cloneTable(s.deck.Objects.Tables["table-1"])
			a := nativeEditorAction{SceneRevision: &revision, ObjectID: "table-element", TableData: &data}
			body, _ := json.Marshal(a)
			probe := &tableDraftContextProbe{Context: context.Background(), session: s, change: func(s *nativeEditorSession) {
				switch kind {
				case "slide":
					s.current++
				case "group":
					s.editingGroup = "another"
				case "revision":
					s.version++
				case "mode":
					s.masterMode = true
				}
			}}
			w := httptest.NewRecorder()
			s.handleTableDraft(w, httptest.NewRequest("POST", "/api/editor/table-draft", bytes.NewReader(body)).WithContext(probe))
			if w.Code != 409 {
				t.Fatal("stale table published", w.Code, w.Body.String())
			}
		})
	}
}

func TestTableDraftRejectsAmbiguousLayoutAndCancelledRequest(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		s := newNativeEditorSession("draft.md", structuredFixture(), false, false)
		revision := s.version
		data := cloneTable(s.deck.Objects.Tables["table-1"])
		a := nativeEditorAction{SceneRevision: &revision, ObjectID: "table-element", TableData: &data}
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		} else {
			s.masterMode, s.currentMaster = true, 2
			s.deck.Masters.Layouts = []MasterLayout{{ID: "same", Slide: s.deck.Slides[0]}, {ID: "same", Slide: s.deck.Slides[0]}}
			a.SceneMaster, a.Slide = true, 2
		}
		body, _ := json.Marshal(a)
		w := httptest.NewRecorder()
		s.handleTableDraft(w, httptest.NewRequest("POST", "/api/editor/table-draft", bytes.NewReader(body)).WithContext(ctx))
		cancel()
		if cancelled && w.Body.Len() != 0 || !cancelled && w.Code != 400 || len(s.undo) != 0 {
			t.Fatal("cancelled/ambiguous table preview published", cancelled, w.Code, w.Body.String())
		}
	}
}

func BenchmarkTableDraftCapture(b *testing.B) {
	d := wordStyleHeavyFixture(b)
	d.Slides[0] = structuredFixture().Slides[0]
	d.Assets = map[string]DeckAsset{"unrelated": {Data: make([]byte, 4<<20)}}
	for _, scoped := range []bool{false, true} {
		b.Run(fmt.Sprint("scoped=", scoped), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if scoped {
					scope, err := captureSlideResolutionScope(d, 0, false)
					if err != nil || len(scope.source.Elements) == 0 {
						b.Fatal("missing table scope", err)
					}
				} else if len(cloneDeckForScene(d).Slides) == 0 {
					b.Fatal("missing deck snapshot")
				}
			}
		})
	}
}
