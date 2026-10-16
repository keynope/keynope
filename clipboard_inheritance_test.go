package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipboardLegacyAppearanceSessionParity(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, version := range []int{0, 1, 2, 3} {
		for _, style := range []string{"retro", "modern"} {
			for _, kind := range []string{"text", "heading", "bullet", "code"} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, style, kind), func(t *testing.T) {
					d := Deck{Slides: []Slide{{Elements: []Element{{ID: "legacy-text", Kind: kind, Level: 2, Text: "Portable appearance", Query: "top=4&left=6&width=80&height=20&text-box=1&render=truetype"}}}}}
					if version != 0 {
						d.Appearance = &DeckAppearance{Version: version, Extra: map[string]json.RawMessage{"profiles": json.RawMessage(`{"modern":{"textScale":0.75},"retro":{"textScale":1.25}}`)}}
						if version == 1 {
							d.Appearance.Mode = style
						}
						if version == 2 {
							d.Appearance.DefaultStyle = style
						}
					}
					source := newNativeEditorSession("Legacy.md", d)
					before := cloneDeck(source.deck)
					original, err := buildDocumentSlideScene(source.deck, 0, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					request, _ := json.Marshal(map[string]interface{}{"version": source.version, "ids": []string{"legacy-text"}})
					response := httptest.NewRecorder()
					source.handleObjectClipboard(response, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(request)))
					if response.Code != http.StatusOK {
						t.Fatal(response.Code, response.Body.String())
					}
					var bundle ObjectClipboard
					if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
						t.Fatal(err)
					}
					target := newNativeEditorSession("Target.md", Deck{Slides: []Slide{{}}})
					if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
						t.Fatal(err)
					}
					pasted, err := buildDocumentSlideScene(target.deck, 0, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					if len(original.Objects) != 1 || len(pasted.Objects) != 1 {
						t.Fatal("missing clipboard text")
					}
					a, b := original.Objects[0], pasted.Objects[0]
					if !reflect.DeepEqual(a.Text, b.Text) || !reflect.DeepEqual(a.Paint, b.Paint) {
						t.Fatalf("legacy appearance changed on paste:\nsource text=%+v paint=%+v\npasted text=%+v paint=%+v", a.Text, a.Paint, b.Text, b.Paint)
					}
					if !reflect.DeepEqual(before, source.deck) {
						t.Fatal("copy mutated source")
					}
					if err := target.apply(nativeEditorAction{Action: "undo"}); err != nil || len(target.deck.Slides[0].Elements) != 0 {
						t.Fatal("paste undo failed", err)
					}
					if err := target.apply(nativeEditorAction{Action: "redo"}); err != nil {
						t.Fatal("paste redo failed", err)
					}
					encoded, err := serializeDeck("Pasted.md", target.deck)
					if err != nil {
						t.Fatal(err)
					}
					reopened, err := parseDeckData("Pasted.md", encoded)
					if err != nil {
						t.Fatal(err)
					}
					final, err := buildDocumentSlideScene(reopened, 0, 245, 56)
					if err != nil || len(final.Objects) != 1 || !reflect.DeepEqual(final.Objects[0].Text, a.Text) || !reflect.DeepEqual(final.Objects[0].Paint, a.Paint) {
						t.Fatal("legacy appearance lost after paste/history/save/reopen", err)
					}
				})
			}
		}
	}
}

func TestClipboardCapturesResolvedPlaceholderAppearance(t *testing.T) {
	for _, tab := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "tab"}[tab], func(t *testing.T) {
			d := masterProjectionOwnershipFixture()
			d.Masters.Layouts[1].Slide.Elements[0].Query = "render=truetype&modern-font=mono&modern-size=120&modern-width=82&fg=%23ff5500&left=11&top=9&width=80&height=20&shadow=soft&shadow-color=%23ffffff"
			d.Slides[0].Elements[0].Query = "master-clear=shadow,shadow-color&modern-size=155&modern-font=mono"
			if tab {
				d.Slides[0].TabID = "guide"
			}
			s := newNativeEditorSession("Copy.md", d)
			before := cloneDeck(s.deck)
			if dir := os.Getenv("KEYNOPE_CLIPBOARD_INHERITANCE_EVIDENCE"); dir != "" && !tab {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := saveDeck(filepath.Join(dir, "source.md"), before); err != nil {
					t.Fatal(err)
				}
			}
			data, _ := json.Marshal(map[string]interface{}{"version": s.version, "ids": []string{"child-title"}})
			w := httptest.NewRecorder()
			s.handleObjectClipboard(w, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(data)))
			if w.Code != http.StatusOK {
				t.Fatal(w.Code, w.Body.String())
			}
			var bundle ObjectClipboard
			if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
				t.Fatal(err)
			}
			q, _ := url.ParseQuery(bundle.Elements[0].Query)
			for key, expected := range map[string]string{"modern-font": "mono", "modern-size": "155", "modern-width": "82", "fg": "#ff5500", "left": "11", "top": "9", "width": "80", "height": "20"} {
				if q.Get(key) != expected {
					t.Fatalf("copy lost inherited %s: %s", key, q.Get(key))
				}
			}
			if q.Has("master-clear") || q.Has("shadow") || q.Has("shadow-color") || bundle.Elements[0].MasterSlotID != "" {
				t.Fatal("detached clipboard retained master-only state", bundle.Elements[0])
			}
			if !reflect.DeepEqual(bundle.Elements[0].Extra, before.Slides[0].Elements[0].Extra) || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("copy lost child metadata or modified source")
			}
			target := newNativeEditorSession("Paste.md", Deck{Slides: []Slide{{}}})
			if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
				t.Fatal(err)
			}
			pasted := target.deck.Slides[0].Elements[0]
			if pasted.ID == "child-title" || pasted.Query != bundle.Elements[0].Query || !reflect.DeepEqual(pasted.Extra, bundle.Elements[0].Extra) {
				t.Fatal("paste changed detached appearance or shared identity")
			}
		})
	}
}

func TestClipboardCapturesSlideTypographyDefaults(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{LayoutID: "blank"}}}
		d.Masters.Base.Slide.TTFSize = 194
		d.Masters.Base.Slide.TTFWidth = 70
		d.Masters.Base.Slide.FG, d.Masters.Base.Slide.FGSet = "38;2;170;85;255", true
		e := Element{ID: "body", Kind: "text", Text: "Keep these dimensions", Query: "render=truetype&modern-font=mono&left=5&top=5&text-box=1&width=150&height=20"}
		if master {
			d.Masters.Layouts[0].Slide.Elements = []Element{e}
		} else {
			d.Slides[0].Elements = []Element{e}
		}
		s := newNativeEditorSession("Defaults.md", d)
		s.masterMode, s.currentMaster = master, 1
		data, _ := json.Marshal(map[string]interface{}{"version": s.version, "ids": []string{"body"}})
		w := httptest.NewRecorder()
		s.handleObjectClipboard(w, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(data)))
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
		var bundle ObjectClipboard
		if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(bundle.Elements[0].Query)
		if q.Get("ttf-size") != "194" || q.Get("ttf-width") != "70" || q.Get("fg") != "#aa55ff" {
			t.Fatal("copy lost slide-level typography defaults", master, q)
		}
	}
}

func TestClipboardResolvedCaptureStillRequiresAuthoredUniqueIDs(t *testing.T) {
	for _, ids := range [][]string{{"decoration"}, {"child-title", "decoration"}, {"child-title", "child-title"}, {"missing"}} {
		s := newNativeEditorSession("Selection.md", masterProjectionOwnershipFixture())
		before := cloneDeck(s.deck)
		data, _ := json.Marshal(map[string]interface{}{"version": s.version, "ids": ids})
		w := httptest.NewRecorder()
		s.handleObjectClipboard(w, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(data)))
		if w.Code != http.StatusBadRequest || !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
			t.Fatal("inherited/invalid selection was copied or mutated state", ids, w.Code)
		}
	}
}
