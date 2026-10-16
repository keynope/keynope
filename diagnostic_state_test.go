package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func diagnosticStateFixture(count int) Deck {
	d := Deck{Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Text"}}}}}
	for i := 0; i < count; i++ {
		d.Diagnostics = append(d.Diagnostics, documentDiagnostic{Conversion: true, Slide: i % 3, Object: fmt.Sprintf("object-%05d", i), Code: "image-paint-rasterized", Message: strings.Repeat("Rendered image treatment. ", 16), Feature: "image", Severity: "warning", Fallback: "picture-artwork", Remediation: "Keep the original deck for editing."})
	}
	return d
}

func TestDiagnosticSessionStorageAndTransport(t *testing.T) {
	d := diagnosticStateFixture(5000)
	s := newNativeEditorSession("Import.md", d)
	d.Diagnostics[0].Message = "caller mutation"
	if len(s.diagnostics) != 5000 || s.diagnostics[0].Message == "caller mutation" || len(s.deck.Diagnostics) != 0 || len(s.savedDeck.Diagnostics) != 0 {
		t.Fatal("notices must be owned once, outside authored/saved decks")
	}
	full := s.state()
	full.Diagnostics[0].Message = "state mutation"
	if s.diagnostics[0].Message == "state mutation" {
		t.Fatal("state snapshot aliases runtime report")
	}
	for i := 0; i < 8; i++ {
		if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "text"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, history := range [][]Deck{s.undo, s.redo} {
		for _, deck := range history {
			if len(deck.Diagnostics) != 0 {
				t.Fatal("runtime notices copied into history")
			}
		}
	}
	if len(s.state().Diagnostics) != 5000 {
		t.Fatal("history lost runtime notices")
	}
	action := s.actionTransportState("undo")
	if !action.DiagnosticsUnchanged || len(action.Diagnostics) != 0 || action.NotesSessionID != s.notesSessionID {
		t.Fatal("action response retransmitted immutable report")
	}
	complete, _ := json.Marshal(s.transportState())
	compact, _ := json.Marshal(action)
	if len(complete) < len(compact)*50 {
		t.Fatal("expected substantial diagnostic transport reduction", len(complete), len(compact))
	}
	t.Logf("5000-note full state %d bytes; history-action response %d bytes", len(complete), len(compact))
	for _, tc := range []struct {
		owner    string
		version  int64
		status   int
		retained bool
	}{{"", 0, 200, false}, {s.notesSessionID, 0, 200, true}, {"another-document", s.version, 200, false}, {"", s.version, 200, false}, {s.notesSessionID, s.version, 204, false}} {
		request := httptest.NewRequest(http.MethodGet, "/api/editor/state?version="+strconv.FormatInt(tc.version, 10)+"&diagnosticsOwner="+url.QueryEscape(tc.owner), nil)
		recorder := httptest.NewRecorder()
		s.handleState(recorder, request)
		if recorder.Code != tc.status {
			t.Fatal("owner-aware state response", tc, recorder.Code)
		}
		if recorder.Code == 204 {
			continue
		}
		var result nativeEditorState
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.DiagnosticsUnchanged != tc.retained || (len(result.Diagnostics) == 0) != tc.retained {
			t.Fatal("report owner reuse incorrect", tc, len(result.Diagnostics), result.DiagnosticsUnchanged)
		}
	}
}

func BenchmarkDiagnosticActionState(b *testing.B) {
	for _, count := range []int{0, 5000} {
		b.Run(fmt.Sprintf("notes-%d", count), func(b *testing.B) {
			s := newNativeEditorSession("Import.md", diagnosticStateFixture(count))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := s.actionTransportState("select-element")
				if !state.DiagnosticsUnchanged || len(state.Diagnostics) != 0 {
					b.Fatal("report copied into hot state")
				}
			}
		})
	}
}
