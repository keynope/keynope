package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

// Legacy output remains readable, but every editing session has one shared
// vector diagram model. Do not introduce a second sampled-route editor.
func TestLegacyConnectorEditorMigrationAndDraftContexts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, appearance := range []struct {
		name  string
		value *DeckAppearance
	}{
		{"implicit", nil},
		{"v1-retro", &DeckAppearance{Version: 1, Mode: "retro"}},
		{"v1-modern", &DeckAppearance{Version: 1, Mode: "modern"}},
		{"v2-retro", &DeckAppearance{Version: 2, DefaultStyle: "retro"}},
		{"v2-modern", &DeckAppearance{Version: 2, DefaultStyle: "modern"}},
		{"current", &DeckAppearance{Version: 3}},
	} {
		for _, mode := range []string{"slide", "tab", "base", "layout"} {
			t.Run(appearance.name+"/"+mode, func(t *testing.T) {
				source := connectorFixture()
				source.ID = "legacy-scope"
				for i := range source.Elements {
					source.Elements[i].Query = setQueryValue(source.Elements[i].Query, "element-style", "retro")
				}
				source.Elements[2].Query = setQueryValue(source.Elements[2].Query, "connector-mode", "elbow")
				source.Elements = append(source.Elements, Element{ID: "caption", Kind: "text", Text: "C64 stays C64", Query: "render=truetype&element-style=retro&left=4&top=45&width=100&height=8"})
				deck := Deck{Appearance: appearance.value.Clone(), Masters: defaultMasterDeck(), Slides: []Slide{{ID: "ordinary"}}}
				master, index := false, 0
				switch mode {
				case "slide", "tab":
					deck.Slides[0] = source
					if mode == "tab" {
						deck.Slides[0].TabID = "legacy-tab"
						syncSlideTabs(&deck)
					}
				case "base":
					master = true
					deck.Masters.Base.Slide = source
				case "layout":
					master, index = true, 1
					deck.Masters.Layouts[0].Slide = source
				}
				before := cloneDeck(deck)
				visible := deck.ResolveSlide(0, false)
				if master {
					visible = masterViewPreview(deck.Masters, index)
				}
				legacy, err := buildStyledSlideScene(deck, visible, index, 245, 56, true)
				if err != nil {
					t.Fatal(err)
				}
				foundLegacy := false
				for _, object := range legacy.Objects {
					if object.ID == "line" {
						foundLegacy = object.RetroLines != nil
					}
				}
				if appearance.name != "current" && !foundLegacy {
					t.Fatal("control did not exercise a genuinely sampled legacy connector")
				}
				s := newNativeEditorSession("Legacy.md", deck)
				s.masterMode = master
				if master {
					s.currentMaster = index
				}
				if !s.deck.usesConcreteAppearance() || !reflect.DeepEqual(s.savedDeck, s.deck) || s.dirtyOverride {
					t.Fatal("opening failed to establish a clean unified baseline")
				}
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", fmt.Sprintf("/api/editor/scene?slide=%d", index), nil))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				var scene slideScene
				if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, object := range scene.Objects {
					if object.Kind == "shape" || object.Kind == "connector" {
						if object.RetroLines != nil {
							t.Fatal("editor still uses sampled diagram artwork", object.ID)
						}
					}
					if object.ID == "line" {
						found = len(object.Points) >= 2
					}
					if object.ID == "caption" && (object.Text == nil || object.Text.FontID != "c64") {
						t.Fatal("unified diagrams changed the requested C64 typography")
					}
				}
				if !found {
					t.Fatal("editor lost the attached vector route")
				}
				liveBefore := cloneDeck(s.deck)
				request := connectorDraftRequest{Revision: s.version, Master: master, Slide: index, Bounds: map[string]sceneRect{"a": {X: 30.25, Y: 6.5, Width: 35.5, Height: 14}}}
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				w = httptest.NewRecorder()
				s.handleConnectorDraft(w, httptest.NewRequest("POST", "/api/editor/connector-draft", bytes.NewReader(body)))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				var draft struct {
					Routes []shapeConnector
					Lines  map[string][]exportLine
				}
				if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
					t.Fatal(err)
				}
				if len(draft.Routes) != 1 || draft.Routes[0].ID != "line" || len(draft.Lines) != 0 {
					t.Fatal("unified draft did not use vector-only route preview", draft)
				}
				if !reflect.DeepEqual(s.deck, liveBefore) || len(s.undo) != 0 {
					t.Fatal("draft changed authored data or history")
				}
				saved, err := serializeDeck("Legacy.md", s.deck)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("Legacy.md", saved)
				if err != nil {
					t.Fatal(err)
				}
				if !reopened.usesConcreteAppearance() {
					t.Fatal("save restored the legacy diagram model")
				}
				owner := reopened.Slides[0]
				if master {
					owner = *masterSlideAt(&reopened, index)
				}
				for _, element := range owner.Elements {
					q, _ := url.ParseQuery(element.Query)
					if q.Has("element-style") {
						t.Fatal("generic Retro/Modern choice resurrected after save")
					}
				}
				if !reflect.DeepEqual(deck, before) {
					t.Fatal("session creation or output mutated the caller's legacy deck")
				}
				if !reflect.DeepEqual(s.deck, liveBefore) {
					t.Fatal("saving or reopening mutated the active unified deck")
				}
			})
		}
	}
}
