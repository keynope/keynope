package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestEditorStateConditionalPolling(t *testing.T) {
	s := viewCommandFixture(10, false)
	request := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleState(w, httptest.NewRequest(http.MethodGet, "/api/editor/state"+query, nil))
		return w
	}
	version := strconv.FormatInt(s.version, 10)
	w := request("?version=" + version)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unchanged poll should have no payload and no cache")
	}
	for _, query := range []string{"", "?version=invalid", "?version=-1", "?version=999999"} {
		w = request(query)
		var state nativeEditorState
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || len(state.Slides) != 10 || state.ResolvedCurrent == nil || len(state.Resolved) != 0 {
			t.Fatalf("full recovery missing for %s", query)
		}
	}
	if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	w = request("?version=" + version)
	var state nativeEditorState
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Selected != 0 || state.Version != s.version {
		t.Fatal("selection change not delivered")
	}
	if request("?version="+strconv.FormatInt(s.version, 10)).Code != http.StatusNoContent {
		t.Fatal("new revision did not settle")
	}
}

func TestEditorTransportResolvesOnlyCurrentSlide(t *testing.T) {
	s := viewCommandFixture(30, false)
	s.current = 17
	state := s.transportState()
	if len(state.Resolved) != 0 {
		t.Fatalf("transport resolved %d non-current slides", len(state.Resolved))
	}
	if state.ResolvedCurrent == nil || !reflect.DeepEqual(*state.ResolvedCurrent, s.deck.ResolveSlide(17, false)) {
		t.Fatal("transport omitted or changed the current resolved slide")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte(`"resolved":`)) || !bytes.Contains(payload, []byte(`"resolvedCurrent":`)) {
		t.Fatalf("transport leaked the all-slide projection: %s", payload)
	}
	full := s.state()
	if len(full.Resolved) != 30 || full.ResolvedCurrent != nil {
		t.Fatal("internal full-state contract changed")
	}
}

func TestEditorActionTransportUsesCurrentSlideDelta(t *testing.T) {
	s := viewCommandFixture(30, false)
	s.current = 17
	delta := s.actionTransportState("update-element")
	if len(delta.Slides) != 0 || delta.CurrentSlide == nil {
		t.Fatal("local action did not use a current-slide delta")
	}
	if !reflect.DeepEqual(*delta.CurrentSlide, s.deck.Slides[17]) || delta.ResolvedCurrent == nil {
		t.Fatal("local action delta changed authored or resolved current slide")
	}
	deltaJSON, _ := json.Marshal(delta)
	fullJSON, _ := json.Marshal(s.transportState())
	if len(deltaJSON)*3 >= len(fullJSON) {
		t.Fatalf("current-slide delta is not materially smaller: delta=%d full=%d", len(deltaJSON), len(fullJSON))
	}
	requestBody, _ := json.Marshal(nativeEditorAction{Action: "select-element", Element: 0})
	response := httptest.NewRecorder()
	s.handleAction(response, httptest.NewRequest(http.MethodPost, "/api/editor/action", bytes.NewReader(requestBody)))
	var routed nativeEditorState
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &routed) != nil || routed.CurrentSlide == nil || len(routed.Slides) != 0 {
		t.Fatal("action endpoint did not publish the current-slide delta contract")
	}
	for _, action := range []string{"add-slide", "reorder-slide", "set-tabs", "delete-layout", "undo", "set-engagement-result"} {
		state := s.actionTransportState(action)
		if len(state.Slides) != len(s.deck.Slides) || state.CurrentSlide != nil {
			t.Fatalf("structural action %q did not retain a full recovery response", action)
		}
	}
	s.masterMode = true
	master := s.actionTransportState("update-element")
	if len(master.Slides) == 0 || master.CurrentSlide != nil {
		t.Fatal("master action incorrectly used a normal-slide delta")
	}
}

func TestEditorClientMergesActionStateBeforeUse(t *testing.T) {
	source := exportHTMLSuffix()
	for _, marker := range []string{
		"function mergeEditorStateResponse(next)",
		"const nextState = mergeEditorStateResponse(await response.json())",
		"window.keynopeNavigateWebWorkspace = (current,state) => {\n    state=mergeEditorStateResponse(state);",
		"window.keynopeLoadWebWorkspace = async (workspace,state) => {\n    if (!workspace || !Array.isArray(workspace.pages)) return;\n    state=mergeEditorStateResponse(state);",
	} {
		if !bytes.Contains([]byte(source), []byte(marker)) {
			t.Fatalf("editor state delta merge omitted %q", marker)
		}
	}
}

func BenchmarkEditorStatePolling(b *testing.B) {
	s := viewCommandFixture(1000, false)
	for _, conditional := range []bool{false, true} {
		b.Run(fmt.Sprint("conditional=", conditional), func(b *testing.B) {
			url := "/api/editor/state"
			if conditional {
				url += "?version=" + strconv.FormatInt(s.version, 10)
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.handleState(httptest.NewRecorder(), r)
			}
		})
	}
}

func TestEditorStateMetadataWithoutRendering(t *testing.T) {
	deck := sceneFixture(t)
	deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: "modern"}
	session := nativeEditorSession{deck: deck, untitled: true}
	state := session.state()
	expected, _ := json.Marshal(deck.ResolvedSlides())
	actual, _ := json.Marshal(state.Resolved)
	if !bytes.Equal(expected, actual) {
		t.Fatal("metadata changed when removing render projections")
	}
	for _, slide := range state.Resolved {
		if slide.ModernScene != nil {
			t.Fatal("state generated a render projection")
		}
	}
	payload, err := json.Marshal(state)
	if err != nil || bytes.Contains(payload, []byte(`"fonts":`)) {
		t.Fatal("editor state still transports retired glyph fonts")
	}
}

func BenchmarkEditorState(b *testing.B) {
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}}
	for s := 0; s < 30; s++ {
		slide := Slide{}
		for i := 0; i < 40; i++ {
			slide.Elements = append(slide.Elements, Element{ID: fmt.Sprintf("text-%d-%d", s, i), Kind: "text", Text: "A workshop text object", Query: "render=truetype&top=3&left=4&width=30&height=5"})
		}
		deck.Slides = append(deck.Slides, slide)
	}
	session := nativeEditorSession{deck: deck, untitled: true, current: 15}
	for _, test := range []struct {
		name string
		read func() nativeEditorState
	}{
		{name: "full", read: session.state},
		{name: "transport", read: session.transportState},
		{name: "action-delta", read: func() nativeEditorState { return session.actionTransportState("select-element") }},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				test.read()
			}
		})
	}
}
