package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSceneProjectionConsumerInputs(t *testing.T) {
	fixture := func() Deck {
		deck := structuredFixture()
		deck.Appearance = &DeckAppearance{Version: 3}
		deck.Theme = builtinDeckTheme("clean-light")
		deck.Assets = map[string]DeckAsset{
			"visible": {MIME: "image/png", Width: 1, Height: 1, Data: []byte{1, 2, 3}},
			"other":   {MIME: "image/png", Width: 1, Height: 1, Data: []byte{4, 5, 6}},
			"bg":      {MIME: "image/png", Width: 1, Height: 1, Data: []byte{7, 8, 9}},
		}
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "media", Kind: "image", AssetID: "visible"})
		deck.Slides[0].BackgroundMedia = &slideBackgroundMedia{AssetID: "bg"}
		deck.Slides = append(deck.Slides, Slide{ID: "other-slide", Elements: []Element{{ID: "other-text", Kind: "text", Text: "Unrelated"}}})
		deck.EnsureDefaultMasters()
		return deck
	}
	tests := []struct {
		name string
		same bool
		edit func(*Deck)
	}{
		{"notes", true, func(d *Deck) { d.Slides[0].Notes = "Speaker only" }},
		{"result", true, func(d *Deck) { d.Slides[0].EngagementResult = &EngagementResult{ActivityID: "private"} }},
		{"other-slide", true, func(d *Deck) { d.Slides[1].Elements[0].Text = "Changed elsewhere" }},
		{"unrelated-asset", true, func(d *Deck) { a := d.Assets["other"]; a.Data[0] = 99; d.Assets["other"] = a }},
		{"unrelated-payload", true, func(d *Deck) { d.Objects.Tables["new"] = cloneTable(d.Objects.Tables["table-1"]) }},
		{"unrelated-build", true, func(d *Deck) { d.Objects.Builds["other-slide"] = SlideBuild{Version: 1} }},
		{"unused-master", true, func(d *Deck) {
			d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{Kind: "text", ID: "unused", Text: "Unused"})
		}},
		{"visible-element", false, func(d *Deck) { d.Slides[0].Elements[0].Query += "&fg=%23ffaa00" }},
		{"visible-asset", false, func(d *Deck) { a := d.Assets["visible"]; a.Data[0] = 99; d.Assets["visible"] = a }},
		{"background-source", false, func(d *Deck) { a := d.Assets["bg"]; a.Data[0] = 99; d.Assets["bg"] = a }},
		{"missing-source", false, func(d *Deck) { delete(d.Assets, "visible") }},
		{"table-consumer", false, func(d *Deck) {
			a := d.Objects.Tables["table-1"]
			a.Cells[1][0].Text = "Changed"
			d.Objects.Tables["table-1"] = a
		}},
		{"chart-consumer", false, func(d *Deck) {
			a := d.Objects.Charts["chart-1"]
			a.Series[0].Values[0] = 42
			d.Objects.Charts["chart-1"] = a
		}},
		{"build-consumer", false, func(d *Deck) {
			a := d.Objects.Builds["slide-1"]
			a.Transition.DurationMS = 600
			d.Objects.Builds["slide-1"] = a
		}},
		{"slide-count", false, func(d *Deck) { d.Slides = append(d.Slides, Slide{}) }},
		{"theme", false, func(d *Deck) { d.Theme.Colors.Text = "#ff00aa" }},
		{"profile", false, func(d *Deck) { d.Appearance.Version = 2 }},
		{"qr-setting", false, func(d *Deck) { d.HideActivityQR = true }},
		{"master-schema", false, func(d *Deck) { d.Masters.Version++ }},
		{"object-schema", false, func(d *Deck) { d.Objects.Version++ }},
		{"unknown-deck", false, func(d *Deck) { d.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }},
		{"unknown-master", false, func(d *Deck) { d.Masters.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }},
		{"unknown-object", false, func(d *Deck) { d.Objects.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := fixture()
			unchanged, after := cloneDeck(before), cloneDeck(before)
			test.edit(&after)
			got := sceneProjectionInputsEqual(before, after, sceneResponseKey{index: 0})
			if got != test.same {
				t.Fatalf("reuse=%v want %v", got, test.same)
			}
			if !reflect.DeepEqual(before, unchanged) {
				t.Fatal("dependency checking changed authored data")
			}
		})
	}
	before, after := fixture(), fixture()
	if sceneProjectionInputsEqual(before, after, sceneResponseKey{index: 0, report: "deck"}) || sceneProjectionInputsEqual(before, after, sceneResponseKey{index: 10000}) {
		t.Fatal("invalid projection or full-deck report must not reuse a slide-only proof")
	}
	// Registry namespaces are typed: equal table/chart keys are not aliases.
	before = fixture()
	before.Slides[0].Elements[1].ObjectID = "table-1"
	before.Objects.Charts["table-1"] = cloneChart(before.Objects.Charts["chart-1"])
	after = cloneDeck(before)
	table := after.Objects.Tables["table-1"]
	table.Cells[1][0].Text = "Changed table, same chart key"
	after.Objects.Tables["table-1"] = table
	if sceneProjectionInputsEqual(before, after, sceneResponseKey{index: 0}) {
		t.Fatal("chart namespace hid the changed table consumer")
	}
}

func TestSceneProjectionMasterConsumers(t *testing.T) {
	deck := Deck{Slides: []Slide{{ID: "one", LayoutID: "title"}, {ID: "two", LayoutID: "blank"}}}
	deck.EnsureDefaultMasters()
	after := cloneDeck(deck)
	after.Masters.Base.Slide.FG, after.Masters.Base.Slide.FGSet = "#ffaa00", true
	for _, key := range []sceneResponseKey{{index: 0}, {index: 1}, {index: 0, master: true}, {index: 1, master: true}} {
		if sceneProjectionInputsEqual(deck, after, key) {
			t.Fatalf("base-master change did not invalidate its consumer %+v", key)
		}
	}
	after = cloneDeck(deck)
	after.Masters.Layouts[0].Slide.Notes = "Notes only"
	if !sceneProjectionInputsEqual(deck, after, sceneResponseKey{index: 1, master: true}) {
		t.Fatal("master notes invalidated appearance")
	}
}

func TestSceneResponseRetainedAcrossNotesAndOtherSlideHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			deck := themedSharedTextFixture(3)
			deck.Slides = append(deck.Slides, Slide{Elements: []Element{{ID: "other", Kind: "text", Text: "Other slide", Query: "render=truetype"}}})
			s := newNativeEditorSession("Untitled.md", deck, true, false)
			s.masterMode = master
			get := func(profile bool) []byte {
				t.Helper()
				path := "/api/editor/scene?slide=0"
				if profile {
					path += "&timing=1"
				}
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 200 {
					t.Fatalf("scene %d: %s", w.Code, w.Body.String())
				}
				return w.Body.Bytes()
			}
			get(false)
			original := s.sceneResponse
			for _, action := range []nativeEditorAction{
				{Action: "update-slide-notes", Slide: 0, Notes: "Private note"},
				{Action: "undo"}, {Action: "redo"},
			} {
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				data := get(false)
				if &s.sceneResponse.data[0] != &original.data[0] {
					t.Fatal("notes/history rebuilt the scene")
				}
				if !bytes.Equal(data, get(true)) {
					t.Fatal("retained response differs from current cold projection")
				}
				var scene slideScene
				if err := json.Unmarshal(data, &scene); err != nil || scene.Revision == nil || *scene.Revision != s.version {
					t.Fatal("stale retained response revision")
				}
			}
			if original.key.revision == s.version {
				t.Fatal("old concurrent reader envelope mutated")
			}
			if !master {
				if err := s.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 1, Notes: "Other note"}); err != nil {
					t.Fatal(err)
				}
				get(false)
				if &s.sceneResponse.data[0] != &original.data[0] {
					t.Fatal("unrelated slide note invalidated cached scene")
				}
				if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 1}); err != nil {
					t.Fatal(err)
				}
				for _, action := range []nativeEditorAction{
					{Action: "update-slide", SlideData: &Slide{BG: "#552255", BGSet: true}},
					{Action: "undo"}, {Action: "redo"},
				} {
					if err := s.apply(action); err != nil {
						t.Fatal(err)
					}
					data := get(false)
					if &s.sceneResponse.data[0] != &original.data[0] {
						t.Fatal("unrelated slide appearance/history rebuilt cached scene")
					}
					if !bytes.Equal(data, get(true)) {
						t.Fatal("other slide edit reused stale scene")
					}
				}
			}
		})
	}
}

func TestSceneResponsePrivateResultsDoNotInvalidate(t *testing.T) {
	deck := themedSharedTextFixture(2)
	deck.Slides[0].Engagement = &EngagementDefinition{ID: "storm-1", Kind: "storm", Prompt: "Ideas?"}
	s := newNativeEditorSession("Untitled.md", deck, true, false)
	get := func(profile bool) []byte {
		t.Helper()
		path := "/api/editor/scene?slide=0"
		if profile {
			path += "&timing=1"
		}
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("scene %d: %s", w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	get(false)
	original := s.sceneResponse
	for _, result := range []*EngagementResult{activityResultFixture(), nil} {
		if err := s.apply(nativeEditorAction{Action: "set-engagement-result", Name: "storm-1", EngagementResult: result}); err != nil {
			t.Fatal(err)
		}
		data := get(false)
		if &s.sceneResponse.data[0] != &original.data[0] || !bytes.Equal(data, get(true)) {
			t.Fatal("private result arrival/reset invalidated or changed public scene")
		}
		if bytes.Contains(data, []byte("Private Name")) || bytes.Contains(data, []byte("Keep this answer")) {
			t.Fatal("private activity results leaked")
		}
	}
}

func TestSceneResponseSaveConfirmationDoesNotInvalidate(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(2), true, false)
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	original := s.sceneResponse
	if original == nil {
		t.Fatal("missing scene response")
	}
	if s.confirmSaved("deck.md", s.version-1) || s.sceneResponse != original {
		t.Fatal("stale save confirmation changed scene")
	}
	if !s.confirmSaved("deck.md", s.version) {
		t.Fatal("current save confirmation rejected")
	}
	if &s.sceneResponse.data[0] != &original.data[0] || s.sceneResponse.key.revision != s.version {
		t.Fatal("save confirmation invalidated scene or retained stale revision")
	}
	var retained bytes.Buffer
	s.sceneResponse.write(&retained)
	fresh := httptest.NewRecorder()
	s.handleScene(fresh, httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil))
	if !bytes.Equal(retained.Bytes(), fresh.Body.Bytes()) {
		t.Fatal("save confirmation changed scene appearance")
	}
}

func TestSceneDependencyRetainedResponsesConcurrentWithNotes(t *testing.T) {
	s := newNativeEditorSession("Untitled.md", themedSharedTextFixture(3), true, false)
	w := httptest.NewRecorder()
	s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
	original := s.sceneResponse
	if original == nil {
		t.Fatal("missing initial scene")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := s.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: strings.Repeat("Private ", i+1)}); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 10; n++ {
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0", nil))
				var scene slideScene
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &scene) != nil || scene.Revision == nil || bytes.Contains(w.Body.Bytes(), []byte("Private")) {
					t.Error("invalid retained concurrent scene")
				}
			}
		}()
	}
	wg.Wait()
	if &s.sceneResponse.data[0] != &original.data[0] || s.sceneResponse.key.revision != s.version {
		t.Fatal("notes invalidated immutable scene bytes or lost revision")
	}
}

func BenchmarkSceneProjectionDependencyComparison(b *testing.B) {
	deck := themedSharedTextFixture(500)
	deck.Appearance = &DeckAppearance{Version: 3}
	deck.Theme = builtinDeckTheme("clean-light")
	deck.Slides = append(deck.Slides, Slide{ID: "other", Elements: []Element{{Kind: "text", Text: "Other"}}})
	before := cloneDeck(deck)
	deck.Slides[1].Notes = "Changed outside the cached projection"
	key := sceneResponseKey{index: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !sceneProjectionInputsEqual(before, deck, key) {
			b.Fatal("unrelated notes affected scene")
		}
	}
}
