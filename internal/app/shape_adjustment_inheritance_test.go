package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func inheritedAdjustmentFixture() Deck {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	d.Masters.Layouts = []MasterLayout{{ID: "round-layout", Name: "Rounded card", Slide: Slide{Elements: []Element{{
		ID: "master-shape", Kind: "shape", SlotID: "card-slot", PlaceholderRole: placeholderBody,
		Query: "shape=rounded&shape-radius=0.35&width=80&height=25&fg=%2355aaff",
	}}}}}
	d.Slides = []Slide{{ID: "page", LayoutID: "round-layout", PageNumber: pageNumberHide, Elements: []Element{{
		ID: "instance", Kind: "shape", MasterSlotID: "card-slot", Query: "left=40&top=10",
	}}}}
	return d
}

func TestInheritedShapeAdjustmentPreviewCommitAndPin(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, tab := range []bool{false, true} {
		d := inheritedAdjustmentFixture()
		if tab {
			d.Slides[0].TabID = "reference"
			d.Tabs = []DeckTab{{ID: "reference", Name: "Reference", SlideTab: true, Page: 1}}
		}
		s := newNativeEditorSession("inherited.md", d)
		before := cloneDeck(s.deck)
		for _, value := range []float64{.35, .27} {
			revision := s.version
			a := nativeEditorAction{Action: "set-shape-adjustment", Slide: 0, SceneRevision: &revision, ObjectID: "instance", ShapeAdjustment: &shapeAdjustmentEdit{Key: "radius", Value: value}}
			body, _ := json.Marshal(a)
			w := httptest.NewRecorder()
			s.handleShapeAdjustmentDraft(w, httptest.NewRequest("POST", "/api/editor/shape-adjustment-draft", bytes.NewReader(body)))
			if w.Code != 200 {
				t.Fatalf("inherited rounded shape preview (tab=%t): %d %s", tab, w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
				t.Fatal("preview mutated inherited source/history")
			}
			if err := s.apply(a); err != nil {
				t.Fatal("inherited adjustment rejected", err)
			}
			q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
			if q.Get("shape-radius") != jsonFloat(value) || q.Has("shape") || q.Has("width") || q.Has("height") {
				t.Fatal("adjustment did not remain a sparse explicit override", q)
			}
			if !reflect.DeepEqual(s.deck.Masters, before.Masters) || len(s.undo) != 1 {
				t.Fatal("adjustment rewrote master/history")
			}
			version := s.version
			a.SceneRevision = &version
			if err := s.apply(a); err != nil || s.version != version || len(s.undo) != 1 {
				t.Fatal("explicit repeated adjustment dirtied history", err)
			}
			var preview slideScene
			if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			committed, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, object := range committed.Objects {
				if object.ID == "instance" {
					for _, p := range preview.Objects {
						if p.ID == object.ID {
							found = true
							// Private routing contours intentionally do not cross the
							// JSON boundary. Compare the actual public geometry wire.
							got, ge := json.Marshal(p.ShapeGeometry)
							want, we := json.Marshal(object.ShapeGeometry)
							if ge != nil || we != nil || !bytes.Equal(got, want) {
								t.Fatal("preview/commit public geometry differ", ge, we)
							}
						}
					}
				}
			}
			if !found {
				t.Fatal("preview/commit lost the adjusted shape")
			}
			serialized, err := serializeDeck("inherited.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := parseDeckData("inherited.md", serialized)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reopened.Masters, s.deck.Masters) || mustQuery(reopened.Slides[0].Elements[0].Query).Get("shape-radius") != jsonFloat(value) {
				t.Fatal("sparse override lost on reopen")
			}
			if mustQuery(reopened.Slides[0].Elements[0].Query).Has("shape") {
				t.Fatal("saving pinned a previously inherited shape type")
			}
			for _, element := range reopened.ResolveSlide(0, false).Elements {
				if element.ID == "instance" && shapeName(element) != "rounded" {
					t.Fatal("saving changed inherited shape geometry")
				}
			}
			probe := cloneDeck(s.deck)
			master := &probe.Masters.Layouts[0].Slide.Elements[0]
			master.Query = setQueryValue(setQueryValue(master.Query, "shape-radius", "0.1"), "width", "90")
			for _, element := range probe.ResolveSlide(0, false).Elements {
				if element.ID == "instance" {
					q := mustQuery(element.Query)
					if q.Get("shape-radius") != jsonFloat(value) || q.Get("width") != "90" {
						t.Fatal("pinned adjustment lost independent master inheritance", q)
					}
				}
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("inherited adjustment Undo", err)
			}
		}
	}
}

func TestInheritedShapeAdjustmentBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_INHERITED_ADJUSTMENT_EVIDENCE")
	if root == "" {
		t.Skip("fixture not requested")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := serializeDeck("inherited.md", inheritedAdjustmentFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inherited.md"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestShapeSlotTypeRoundTripAcrossCatalog(t *testing.T) {
	for _, definition := range shapeDefinitions {
		t.Run(definition.ID, func(t *testing.T) {
			deck := inheritedAdjustmentFixture()
			master := &deck.Masters.Layouts[0].Slide.Elements[0]
			q := mustQuery(master.Query)
			q.Set("shape", definition.ID)
			q.Del("shape-radius")
			for _, adjustment := range definition.Adjustments {
				q.Set("shape-"+adjustment.Key, jsonFloat((adjustment.Min+adjustment.Max)/2))
			}
			master.Query = q.Encode()
			data, err := serializeDeck("shapes.md", deck)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("[shape]\n")) {
				t.Fatal("missing sparse shape marker")
			}
			reopened, err := parseDeckData("shapes.md", data)
			if err != nil {
				t.Fatal(err)
			}
			if mustQuery(reopened.Slides[0].Elements[0].Query).Has("shape") {
				t.Fatal("round trip made type explicit")
			}
			found := false
			for _, element := range reopened.ResolveSlide(0, false).Elements {
				if element.ID == "instance" {
					found = true
					if shapeName(element) != definition.ID {
						t.Fatal("round trip changed resolved type", shapeName(element))
					}
				}
			}
			if !found {
				t.Fatal("round trip lost instance")
			}
		})
	}
	legacy := Deck{Slides: []Slide{{Elements: []Element{{Kind: "shape"}}}}}
	data, err := serializeDeck("legacy.md", legacy)
	if err != nil || !bytes.Contains(data, []byte("[shape:circle]")) {
		t.Fatal("unbound legacy shape default changed", err)
	}
}
