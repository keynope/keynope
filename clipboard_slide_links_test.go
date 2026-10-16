package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestClipboardSlideLinkDestinationsFollowIdentity(t *testing.T) {
	source := Deck{Slides: []Slide{{ID: "source"}, {ID: "destination"}}}
	elements := []Element{{ID: "jump", Kind: "text", Text: "Jump", Query: "slide=2&fg=%23abcdef"}, {ID: "web", Kind: "text", Text: "Website", Query: "link=https%3A%2F%2Fkeynope.sh%2F"}}
	bundle, err := makeObjectClipboard(source, elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"reordered", "missing", "ambiguous", "legacy-source"} {
		t.Run(scenario, func(t *testing.T) {
			destination := Deck{Slides: []Slide{{ID: "destination"}, {ID: "unrelated"}}}
			copy := bundle
			want := "#1"
			switch scenario {
			case "missing":
				destination.Slides[0].ID = "different"
				want = ""
			case "ambiguous":
				destination.Slides[1].ID = "destination"
				want = ""
			case "legacy-source":
				legacy := cloneDeck(source)
				legacy.Slides[1].ID = ""
				copy, err = makeObjectClipboard(legacy, elements)
				if err != nil {
					t.Fatal(err)
				}
				want = ""
			}
			before := cloneDeck(destination)
			pasted, _, _, err := prepareObjectClipboardPaste(destination, copy)
			if err != nil {
				t.Fatal(err)
			}
			if got := elementLink(pasted[0].Query); got != want {
				t.Fatalf("%s: link redirected by page number: %q, want %q", scenario, got, want)
			}
			if elementLink(pasted[1].Query) != "https://keynope.sh/" || !reflect.DeepEqual(before, destination) {
				t.Fatal("paste changed external link or destination")
			}
			q, _ := url.ParseQuery(pasted[0].Query)
			if q.Get("fg") != "#abcdef" || pasted[0].Text != "Jump" {
				t.Fatal("link resolution changed content or paint")
			}
		})
	}
}

func TestClipboardSlideLinkWirePrivacyAndValidation(t *testing.T) {
	source := Deck{Slides: []Slide{{ID: "source"}, {ID: "destination", Notes: "PRIVATE NOTES", Elements: []Element{{Kind: "text", Text: "PRIVATE PAGE"}}}}}
	elements := []Element{{ID: "jump", Kind: "text", Text: "Jump", Query: "link=%232"}, {ID: "web", Kind: "text", Text: "Web", Query: "link=https%3A%2F%2Fkeynope.sh"}}
	bundle, err := makeObjectClipboard(source, elements)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle.SlideLinks, map[string]string{"jump": "destination"}) {
		t.Fatal("missing selected object link identities", bundle.SlideLinks)
	}
	wire, err := json.Marshal(bundle)
	if err != nil || bytes.Contains(wire, []byte("PRIVATE")) {
		t.Fatal("clipboard leaked unselected destination content", err)
	}
	for _, scenario := range []string{"unknown-source", "invalid-destination", "external-source", "legacy-bundle"} {
		var copy ObjectClipboard
		if err := json.Unmarshal(wire, &copy); err != nil {
			t.Fatal(err)
		}
		switch scenario {
		case "unknown-source":
			copy.SlideLinks["unselected"] = "destination"
		case "invalid-destination":
			copy.SlideLinks["jump"] = "../bad"
		case "external-source":
			copy.SlideLinks["web"] = "destination"
		case "legacy-bundle":
			copy.SlideLinks = nil
		}
		s := newNativeEditorSession("Clipboard.md", Deck{Slides: []Slide{{ID: "destination"}}})
		if scenario == "legacy-bundle" {
			warnings, err := s.prepareAndApplyObjectClipboardResult(context.Background(), nativeEditorAction{Action: "paste-objects", Clipboard: &copy})
			if err != nil || len(warnings) != 1 || elementLink(s.deck.Slides[0].Elements[0].Query) != "" {
				t.Fatal("legacy numeric destination redirected instead of warning", err, warnings)
			}
		} else {
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "paste-objects", Clipboard: &copy})
		}
	}
	if after, _ := json.Marshal(bundle); !bytes.Equal(wire, after) {
		t.Fatal("validation/paste mutated clipboard source")
	}
}

func TestClipboardSlideLinkSessionScopesHistoryAndCapture(t *testing.T) {
	source := Deck{Slides: []Slide{{ID: "source"}, {ID: "destination"}}}
	bundle, err := makeObjectClipboard(source, []Element{{ID: "jump", Kind: "text", Text: "Jump", Query: "slide=2"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"slide", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			s := newNativeEditorSession("Destination.md", Deck{Slides: []Slide{{ID: "owner"}, {ID: "unrelated", Notes: "PRIVATE"}, {ID: "destination", Elements: []Element{{Kind: "text", Text: "PRIVATE"}}}}})
			if scope == "tab" {
				s.deck.Slides[0].TabID = "resources"
				syncSlideTabs(&s.deck)
			}
			if scope == "base" || scope == "layout" {
				s.masterMode = true
				if scope == "layout" {
					s.currentMaster = 1
				}
			}
			action := nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}
			_, snapshot, err := s.captureObjectClipboardPaste(context.Background(), action)
			if err != nil || len(snapshot.Slides) != 3 {
				t.Fatal("destination identities were omitted", err)
			}
			for index, slide := range snapshot.Slides {
				if !reflect.DeepEqual(slide, Slide{ID: s.deck.Slides[index].ID}) {
					t.Fatal("capture copied unrelated page content", slide)
				}
			}
			before := cloneDeck(s.deck)
			warnings, err := s.prepareAndApplyObjectClipboardResult(context.Background(), action)
			if err != nil || len(warnings) != 0 {
				t.Fatal("identified target was not retained", err, warnings)
			}
			target := s.deck.Slides[0]
			if s.masterMode {
				target = *masterSlideAt(&s.deck, s.currentMaster)
			}
			found := false
			for _, element := range target.Elements {
				if element.Text == "Jump" {
					found = elementLink(element.Query) == "#3"
				}
			}
			if !found || len(s.undo) != 1 {
				t.Fatal("paste lost destination or split history")
			}
			after := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
				t.Fatal("redo", err)
			}
		})
	}
}

func TestClipboardSlideLinkPasteResponseWarningsAreTransient(t *testing.T) {
	s := newNativeEditorSession("Destination.md", Deck{Slides: []Slide{{ID: "unrelated"}}})
	request := nativeEditorAction{Action: "paste-objects", Clipboard: &ObjectClipboard{Version: 1, Elements: []Element{{ID: "jump", Kind: "text", Text: "Jump", Query: "slide=1"}}}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handlePasteObjectClipboard(w, httptest.NewRequest(http.MethodPost, "/api/editor/paste-clipboard", bytes.NewReader(data)))
	var state nativeEditorState
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || len(state.ClipboardWarnings) != 1 {
		t.Fatal("paste did not surface removed link warning", w.Code, w.Body.String())
	}
	if len(s.transportState().ClipboardWarnings) != 0 {
		t.Fatal("one-response warning leaked into session state")
	}
}
