package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"reflect"
	"testing"
)

type connectorContextProbe struct {
	context.Context
	checks int
	change func()
}

func (p *connectorContextProbe) Err() error {
	p.checks++
	if p.checks == 3 {
		p.change()
	}
	return nil
}

func TestConnectorDraftRejectsChangedScopeBeforePublication(t *testing.T) {
	s := nativeEditorSession{deck: Deck{Slides: []Slide{connectorFixture()}}, version: 5}
	request := connectorDraftRequest{Revision: 5, Bounds: map[string]sceneRect{"a": {X: 30, Y: 6, Width: 35, Height: 14}}}
	data, _ := json.Marshal(request)
	probe := &connectorContextProbe{Context: context.Background(), change: func() { s.mu.Lock(); s.version++; s.mu.Unlock() }}
	w := httptest.NewRecorder()
	s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(data)).WithContext(probe))
	if probe.checks < 3 || w.Code != 409 {
		t.Fatalf("stale draft published: checks=%d status=%d", probe.checks, w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request.Revision = s.version
	data, _ = json.Marshal(request)
	w = httptest.NewRecorder()
	s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(data)).WithContext(ctx))
	if w.Body.Len() != 0 {
		t.Fatal("cancelled draft published a result")
	}
}

func TestGeometryResolutionScopeParityOwnershipAndOmissions(t *testing.T) {
	d := strokeRouteFixture(t)
	d.Slides = append([]Slide{{ID: "unused-a"}, {ID: "unused-b"}}, d.Slides...)
	d.Slides[2].LayoutID = "relevant"
	d.Masters.Layouts = []MasterLayout{{ID: "irrelevant"}, {ID: "also-irrelevant"}, {ID: "relevant", Slide: Slide{BG: "#112233", BGSet: true}}}
	d.Assets = map[string]DeckAsset{"unrelated": {Data: make([]byte, 1<<20)}}
	d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"unrelated": {}}}
	d.Slides[2].Notes = "private notes"
	d.Slides[2].EngagementResult = &EngagementResult{Version: 1, State: map[string]json.RawMessage{"private": json.RawMessage(`"answer"`)}}
	for _, mode := range []int{-1, 0, 3} {
		master, index := mode >= 0, 2
		if master {
			index = mode
			*masterSlideAt(&d, index) = cloneSlide(d.Slides[2])
		}
		scope, err := captureSlideResolutionScope(d, index, master)
		if err != nil {
			t.Fatal(err)
		}
		var want Slide
		if master {
			want = d.applyShapeStrokeDefaults(masterViewPreview(d.Masters, index))
		} else {
			want = d.ResolveSlide(index, false)
		}
		want = cloneSlideResolutionInput(want)
		got := scope.resolved()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("scope changed normal/master resolution (%d)", mode)
		}
		if scope.deck.Assets != nil || scope.deck.Objects != nil || scope.deck.Slides != nil || scope.source.Notes != "" || scope.source.EngagementResult != nil || len(scope.deck.Masters.Layouts) > 1 {
			t.Fatal("scope copied unrelated/private/heavy inputs")
		}
		before := cloneDeck(d)
		scope.source.Elements[0].Query = "changed"
		scope.deck.Theme.Colors.Accent[0] = "#abcdef"
		if !reflect.DeepEqual(d, before) {
			t.Fatal("scope aliases document inputs")
		}
	}
}

func TestConnectorDraftActiveScopeLocksAndInheritedTypography(t *testing.T) {
	d := strokeRouteFixture(t)
	label, _ := json.Marshal(shapeLabelData{Text: "Inherited label", Query: "render=truetype"})
	for i, e := range d.Slides[0].Elements {
		if e.ID == "source" {
			d.Slides[0].Elements[i].Query = setQueryValue(e.Query, "shape-label", base64.StdEncoding.EncodeToString(label))
		}
	}
	d.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 86, Width: 100, LineHeight: 1.4}
	s := nativeEditorSession{deck: d, version: 5}
	request := connectorDraftRequest{Revision: 5, Bounds: map[string]sceneRect{"source": {X: 20.25, Y: 19.5, Width: 10, Height: 8}}}
	call := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(data)))
		return w
	}
	w := call()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct{ Routes []shapeConnector }
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Routes) != 1 {
		t.Fatal("missing route")
	}
	expected := cloneDeck(d)
	for i, e := range expected.Slides[0].Elements {
		if e.ID == "source" {
			for key, value := range map[string]string{"left": "20", "top": "19", "width": "10", "height": "8", "object-offset-x": "0.25", "object-offset-y": "0.5"} {
				e.Query = setQueryValue(e.Query, key, value)
			}
			expected.Slides[0].Elements[i] = e
		}
	}
	scene, err := buildSlideScene(expected, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := expected.ResolveSlide(0, false)
	oldRoutes := slideShapeConnectors(unresolved, displayLines(unresolved, 245, 56, 0), 245, 56)
	if reflect.DeepEqual(result.Routes, oldRoutes) {
		t.Fatal("fixture did not exercise missing inherited typography")
	}
	for _, object := range scene.Objects {
		if object.ID == "line" {
			for i, p := range result.Routes[0].Points {
				if i >= len(object.Points) || math.Abs(p.X*1920/245-object.Points[i].X) > 1e-7 || math.Abs(p.Y*1080/56-object.Points[i].Y) > 1e-7 {
					t.Fatalf("draft differs at %d: routes=%+v scene=%+v", i, result.Routes[0].Points, object.Points)
				}
			}
		}
	}
	request.Slide = 1
	if call().Code != 409 {
		t.Fatal("non-current slide accepted")
	}
	request.Slide = 0
	for i, e := range s.deck.Slides[0].Elements {
		if e.ID == "source" {
			s.deck.Slides[0].Elements[i].Query = setQueryValue(e.Query, "object-locked", "1")
		}
	}
	if call().Code != 400 {
		t.Fatal("locked draft accepted")
	}
}

func TestConnectorDraftCompactedMasterScopeAndGroupLocks(t *testing.T) {
	d := strokeRouteFixture(t)
	d.Masters.Layouts = []MasterLayout{{ID: "unused-a"}, {ID: "unused-b"}, {ID: "selected", Slide: cloneSlide(d.Slides[0])}}
	s := nativeEditorSession{deck: d, version: 3, masterMode: true, currentMaster: 3}
	request := connectorDraftRequest{Revision: 3, Master: true, Slide: 3, Bounds: map[string]sceneRect{"source": {X: 20, Y: 20, Width: 45, Height: 12}}}
	call := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(data)))
		return w
	}
	if w := call(); w.Code != 200 {
		t.Fatal("compacted master index lost target", w.Code, w.Body.String())
	}
	master := masterSlideAt(&s.deck, 3)
	for i, e := range master.Elements {
		if e.ID == "source" || e.ID == "wall" {
			master.Elements[i].Query = setQueryValue(e.Query, "group", "pair")
			if e.ID == "wall" {
				master.Elements[i].Query = setQueryValue(master.Elements[i].Query, "object-locked", "1")
			}
		}
	}
	if call().Code != 400 {
		t.Fatal("locked sibling allowed whole-group draft")
	}
	s.editingGroup = "pair"
	if w := call(); w.Code != 200 {
		t.Fatal("entered group lost unlocked individual target", w.Code, w.Body.String())
	}
	for i, e := range master.Elements {
		if e.ID == "source" {
			master.Elements[i].PlaceholderRole = activityQRCodeRole
		}
	}
	if call().Code != 400 {
		t.Fatal("protected activity artwork accepted as a draft target")
	}
}

func BenchmarkConnectorDraftCapture(b *testing.B) {
	d := wordStyleHeavyFixture(b)
	d.Slides[0] = connectorFixture()
	for i := 0; i < 4; i++ {
		if d.Assets == nil {
			d.Assets = map[string]DeckAsset{}
		}
		d.Assets[fmt.Sprint(i)] = DeckAsset{Data: make([]byte, 1<<20)}
	}
	b.Run("whole-deck", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			snapshot := cloneDeckForScene(d)
			if len(snapshot.Slides) == 0 {
				b.Fatal("missing snapshot")
			}
		}
	})
	b.Run("scoped", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			scope, err := captureSlideResolutionScope(d, 0, false)
			if err != nil || len(scope.source.Elements) != 3 {
				b.Fatal("missing scope", err)
			}
		}
	})
}
