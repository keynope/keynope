package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func inheritedEmphasisFixture(t testing.TB, rich bool) Deck {
	t.Helper()
	q := url.Values{"render": {"truetype"}, "modern-font": {"sans"}, "modern-size": {"64"}, "ttf-weight": {"bold"}, "width": {"160"}, "height": {"18"}, "top": {"6"}, "left": {"20"}}
	if rich {
		value, err := encodeModernRuns("Inherited text", "bold", "text", []sceneRun{{Text: "Inherited text", Bold: true, Italic: true, Underline: true, Color: "#aa3355"}})
		if err != nil {
			t.Fatal(err)
		}
		q.Set("modern-runs", value)
	}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base"}, Layouts: []MasterLayout{{ID: "emphasis", Name: "Emphasis", Slide: Slide{Elements: []Element{{ID: "master-text", SlotID: "text-slot", PlaceholderRole: placeholderBody, Kind: "text", Text: "Inherited text", Query: q.Encode()}}}}}}, Slides: []Slide{{ID: "child", LayoutID: "emphasis", PageNumber: pageNumberHide, Elements: []Element{{ID: "child-text", MasterSlotID: "text-slot", Kind: "text", Text: "Inherited text"}}}}}
}

func TestMasterInheritedTextEmphasisCanBeDisabled(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, transfer := range []bool{false, true} {
		for _, property := range []string{"text.bold", "text.italic", "text.underline"} {
			t.Run(fmt.Sprintf("transfer=%t/%s", transfer, property), func(t *testing.T) {
				s := newNativeEditorSession("Emphasis.md", inheritedEmphasisFixture(t, true))
				before := cloneDeck(s.deck)
				revision := s.version
				a := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage("false")}
				if transfer {
					a.Action = "paste-object-style"
					a.StyleData = &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{property: {Present: true, Value: "false"}}}
				}
				if err := s.apply(a); err != nil {
					t.Fatal(err)
				}
				if s.version != revision+1 || len(s.undo) != 1 {
					t.Fatal("turning inherited emphasis off did not commit once")
				}
				check := func(deck Deck) {
					t.Helper()
					resolved := deck.ResolveSlide(0, false)
					caps := buildSelectionCapabilities(deck, deck.Slides[0], resolved, []string{"child-text"}, "", 0)
					for _, name := range []string{"text.bold", "text.italic", "text.underline"} {
						value := caps.Properties[name]
						if value.State != "value" || value.Value != (name != property) {
							t.Fatalf("%s after disabling %s: %+v", name, property, value)
						}
					}
					for _, e := range resolved.Elements {
						if e.ID == "child-text" {
							for _, run := range sceneTextFor(e).Runs {
								if run.Color != "#aa3355" {
									t.Fatal("emphasis edit erased unrelated word colour", run)
								}
							}
						}
					}
				}
				check(s.deck)
				if !reflect.DeepEqual(before.Masters, s.deck.Masters) {
					t.Fatal("child formatting mutated master")
				}
				accepted := cloneDeck(s.deck)
				data, err := serializeDeck("Emphasis.md", s.deck)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("Emphasis.md", data)
				if err != nil {
					t.Fatal(err)
				}
				check(reopened)
				revision = s.version
				if err := s.apply(a); err != nil || s.version != revision || len(s.undo) != 1 {
					t.Fatal("repeated explicit Off was not a no-op", err)
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
					t.Fatal("Undo did not restore inherited formatting", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, accepted) {
					t.Fatal("Redo did not restore explicit Off", err)
				}
				revision = s.version
				if err := s.apply(nativeEditorAction{Action: "reset-object-style", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}}); err != nil {
					t.Fatal(err)
				}
				resolved := s.deck.ResolveSlide(0, false)
				caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], resolved, []string{"child-text"}, "", 0)
				for _, name := range []string{"text.bold", "text.italic", "text.underline"} {
					if caps.Properties[name].Value != true {
						t.Fatal("Reset did not restore master emphasis", name, caps.Properties[name])
					}
				}
			})
		}
	}
}

func TestMasterInheritedEmphasisAllOffRetainsWordColour(t *testing.T) {
	s := newNativeEditorSession("Emphasis.md", inheritedEmphasisFixture(t, true))
	for _, property := range []string{"text.bold", "text.italic", "text.underline"} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage("false")}); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range s.deck.ResolveSlide(0, false).Elements {
		if e.ID == "child-text" {
			for _, run := range sceneTextFor(e).Runs {
				if run.Bold || run.Italic || run.Underline || run.Color != "#aa3355" {
					t.Fatal("turning the final emphasis off lost its colour or restored another emphasis", run)
				}
			}
		}
	}
}

func TestMasterTextEmphasisBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_EMPHASIS_PROOF")
	if root == "" {
		t.Skip("browser fixture not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "inherited.md"), inheritedEmphasisFixture(t, true)); err != nil {
		t.Fatal(err)
	}
}

func TestSparseTextEmphasisLeavesOtherMasterPropertiesLive(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		for _, tab := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tab=%t", kind, tab), func(t *testing.T) {
				d := inheritedEmphasisFixture(t, true)
				d.Masters.Layouts[0].Slide.Elements[0].Kind = kind
				d.Slides[0].Elements[0].Kind = kind
				if tab {
					d.Slides[0].TabID = "emphasis-tab"
					syncSlideTabs(&d)
				}
				s := newNativeEditorSession("Sparse.md", d)
				revision := s.version
				if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: "text.italic", PropertyValue: json.RawMessage("false")}); err != nil {
					t.Fatal(err)
				}
				q := mustQuery(s.deck.Slides[0].Elements[0].Query)
				if q.Has("modern-runs") || q.Has("ttf-weight") || q.Get("text-italic") != "0" {
					t.Fatal("one-property edit froze unrelated master runs", q)
				}
				master := &s.deck.Masters.Layouts[0].Slide.Elements[0]
				mq := mustQuery(master.Query)
				mq.Del("ttf-weight")
				runs, err := encodeModernRunsMetadata(master.Text, "", []sceneRun{{Text: master.Text, Italic: true, Color: "#22aacc"}}, true)
				if err != nil {
					t.Fatal(err)
				}
				mq.Set("modern-runs", runs)
				master.Query = mq.Encode()
				resolved := s.deck.ResolveSlide(0, false)
				caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], resolved, []string{"child-text"}, "", 0)
				if caps.Properties["text.bold"].Value != false || caps.Properties["text.italic"].Value != false || caps.Properties["text.underline"].Value != false {
					t.Fatal("master edit did not propagate independent flags", caps.Properties)
				}
				if caps.Properties["text.bold"].Provenance != "inherited" || caps.Properties["text.italic"].Provenance != "explicit" {
					t.Fatal("per-property provenance lost", caps.Properties)
				}
				for _, e := range resolved.Elements {
					if e.ID != "child-text" {
						continue
					}
					text := sceneTextFor(e)
					all := append([]sceneRun(nil), text.Runs...)
					for _, p := range text.Paragraphs {
						all = append(all, p.Runs...)
					}
					for _, run := range all {
						if run.Color != "#22aacc" {
							t.Fatal("sparse emphasis froze master colour", run)
						}
					}
				}
				revision = s.version
				if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, StyleData: &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{"text.italic": {}}}}); err != nil {
					t.Fatal(err)
				}
				caps = buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), []string{"child-text"}, "", 0)
				if caps.Properties["text.italic"].Value != true || caps.Properties["text.italic"].Provenance != "inherited" {
					t.Fatal("absent style did not restore this property's inheritance", caps.Properties["text.italic"])
				}
			})
		}
	}
}

func TestSparseTextEmphasisAdmission(t *testing.T) {
	for _, key := range []string{"text-bold", "text-italic", "text-underline"} {
		for _, value := range []string{"0", "1"} {
			if err := validatePlacementQuery(key + "=" + value); err != nil {
				t.Fatal(err)
			}
		}
		for _, value := range []string{"", "true", "2", "NaN"} {
			if err := validatePlacementQuery(key + "=" + value); err == nil {
				t.Fatal("invalid emphasis flag accepted", key, value)
			}
		}
		if err := validatePlacementQuery(key + "=0&" + key + "=1"); err == nil {
			t.Fatal("duplicate emphasis owner accepted", key)
		}
	}
}

func TestSparseEmphasisDoesNotOverrideInlineMixedEditing(t *testing.T) {
	s := newNativeEditorSession("Mixed.md", inheritedEmphasisFixture(t, true))
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: "text.bold", PropertyValue: json.RawMessage("false")}); err != nil {
		t.Fatal(err)
	}
	revision = s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", Slide: 0, SceneRevision: &revision, ObjectID: "child-text", TextRuns: []sceneRun{{Text: "Inherited ", Bold: true}, {Text: "text"}}}); err != nil {
		t.Fatal(err)
	}
	check := func(deck Deck) {
		t.Helper()
		for _, e := range deck.ResolveSlide(0, false).Elements {
			if e.ID == "child-text" {
				runs := sceneTextFor(e).Runs
				if len(runs) != 2 || !runs[0].Bold || runs[1].Bold {
					t.Fatal("whole-text flag overrode new mixed inline emphasis", runs)
				}
			}
		}
	}
	check(s.deck)
	data, err := serializeDeck("Mixed.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Mixed.md", data)
	if err != nil {
		t.Fatal(err)
	}
	check(reopened)
}

func TestDetachedSparseEmphasisCanStillBeToggled(t *testing.T) {
	d := inheritedEmphasisFixture(t, true)
	d.Slides[0].Elements[0].Query = "text-bold=0&text-italic=0&text-underline=0"
	if !d.DetachSlide(0) {
		t.Fatal("could not detach fixture")
	}
	s := newNativeEditorSession("Detached.md", d)
	for _, property := range []string{"text.bold", "text.italic", "text.underline"} {
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &revision, ObjectIDs: []string{"child-text"}, Property: property, PropertyValue: json.RawMessage("true")}); err != nil {
			t.Fatal(err)
		}
		caps := buildSelectionCapabilities(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), []string{"child-text"}, "", 0)
		if caps.Properties[property].Value != true {
			t.Fatal("detached sparse flag overrode a subsequent typography edit", property, caps.Properties[property])
		}
	}
}
