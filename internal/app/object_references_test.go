package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func referenceDiagram() Slide {
	return Slide{ID: "diagram-slide", PageNumber: pageNumberHide, Elements: []Element{
		{ID: "left-shape", Kind: "shape", Query: "shape=square&top=2&left=2&group=pair"},
		{ID: "right-shape", Kind: "shape", Query: "shape=square&top=2&left=30&group=pair"},
		{ID: "joining-line", Kind: "connector", Query: "connector-from=left-shape&connector-from-side=right&connector-to=right-shape&connector-to-side=left"},
	}}
}

func referenceDeck() Deck {
	return Deck{Slides: []Slide{referenceDiagram()}, Objects: &DeckObjects{Version: 1, Builds: map[string]SlideBuild{
		"diagram-slide": {Version: 1, Steps: []BuildStep{
			{TargetIDs: []string{"left-shape", "right-shape"}, Effect: "appear"},
			{TargetIDs: []string{"joining-line"}, Effect: "fade"},
		}},
	}}}
}

func TestDetachedOpaqueTimelinePreservesNestedExtensions(t *testing.T) {
	for _, location := range []string{"root", "transition", "step"} {
		t.Run(location, func(t *testing.T) {
			d := referenceDeck()
			build := d.Objects.Builds["diagram-slide"]
			extra := &jsonExtensions{"future": json.RawMessage(`{"dependency":"future-object","private":"PRIVATE BUILD"}`)}
			switch location {
			case "root":
				build.Extra = extra
			case "transition":
				build.Transition.Extra = extra
			case "step":
				build.Steps[0].Extra = extra
			}
			d.Objects.Builds["diagram-slide"] = build
			d.Slides = append(d.Slides, Slide{ID: "survivor", Elements: []Element{{Kind: "text", ID: "visible", Text: "Visible"}}})
			s := newNativeEditorSession("opaque-build.md", d)
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "delete-slide"}); err != nil {
				t.Fatal(err)
			}
			if got, ok := s.deck.Objects.Builds["diagram-slide"]; !ok || !reflect.DeepEqual(got, build) {
				t.Fatal("deleted nested opaque archive", got)
			}
			data, err := serializeDeck("opaque-build.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("opaque-build.md", data)
			if err != nil || !reflect.DeepEqual(loaded.Objects.Builds["diagram-slide"], build) {
				t.Fatal("detached metadata failed reopen", err)
			}
			public, err := participantSlideMarkdown(s.deck, 0)
			if err != nil || bytes.Contains(public, []byte("PRIVATE BUILD")) || bytes.Contains(public, []byte("keynope-objects")) {
				t.Fatal("orphan timeline leaked into participant page", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("delete undo lost timeline", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.deck.Objects.Builds["diagram-slide"], build) {
				t.Fatal("redo lost opaque timeline")
			}
		})
	}
}

func TestObjectReferencesRejectAmbiguousAndMissingTargets(t *testing.T) {
	for _, problem := range []string{"duplicate-element", "master-duplicate", "unknown-slide", "unknown-target", "other-slide-target", "connector-target", "connector-side", "connector-self"} {
		t.Run(problem, func(t *testing.T) {
			d := referenceDeck()
			switch problem {
			case "duplicate-element":
				d.Slides[0].Elements[1].ID = d.Slides[0].Elements[0].ID
			case "master-duplicate":
				d.Masters.Base.Slide = cloneSlide(d.Slides[0])
				d.Masters.Base.Slide.ID = "master-source"
				d.Masters.Base.Slide.Elements[1].ID = "left-shape"
			case "unknown-slide":
				d.Objects.Builds["missing-slide"] = d.Objects.Builds["diagram-slide"]
			case "unknown-target":
				d.Objects.Builds["diagram-slide"].Steps[0].TargetIDs[0] = "missing-shape"
			case "other-slide-target":
				d.Slides = append(d.Slides, Slide{ID: "another-slide", Elements: []Element{{ID: "other-target", Kind: "text", Text: "Other"}}})
				d.Objects.Builds["diagram-slide"].Steps[0].TargetIDs[0] = "other-target"
			case "connector-target":
				d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to", "missing-shape")
			case "connector-side":
				d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to-side", "centre-ish")
			case "connector-self":
				d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to", "left-shape")
			}
			if err := validateDeckObjectReferences(d); err == nil {
				t.Fatal("accepted invalid reference scope")
			}
		})
	}
}

func TestObjectReferenceScopesIncludeMastersAndTabs(t *testing.T) {
	d := referenceDeck()
	d.Masters = defaultMasterDeck()
	d.Slides[0].LayoutID, d.Slides[0].TabID = "blank", "reference-tab"
	d.Masters.Base.Slide.Elements = []Element{{ID: "master-shape", Kind: "shape", Query: "shape=square"}}
	d.Objects.Builds["diagram-slide"].Steps[0].TargetIDs = []string{"left-shape", "master-shape"}
	d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to", "master-shape")
	if err := validateDeckObjectReferences(d); err != nil {
		t.Fatal("inherited shape and tab-slide targets must resolve", err)
	}
	// Stable authored IDs are scoped, not required to be globally unique.
	other := cloneSlide(d.Slides[0])
	other.ID, other.TabID = "other-slide", ""
	d.Slides = append(d.Slides, other)
	if err := validateDeckObjectReferences(d); err != nil {
		t.Fatal("same object IDs in a different slide scope are valid", err)
	}
	// A build target that exists twice in the projection is ambiguous.
	d.Masters.Layouts[0].Slide.Elements = []Element{{ID: "left-shape", Kind: "shape", Query: "shape=square"}}
	if err := validateDeckObjectReferences(d); err == nil {
		t.Fatal("ambiguous authored/inherited build target accepted")
	}
}

func TestObjectReferenceDeletionUpdatesTimelineAndConnectors(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, actionName := range []string{"delete-element", "delete-selection", "delete-objects"} {
			t.Run(map[bool]string{false: "slide", true: "master"}[master]+"/"+actionName, func(t *testing.T) {
				s := newNativeEditorSession("references.md", referenceDeck(), false, false)
				s.masterMode = master
				if master {
					s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
					s.deck.Masters.Base.Slide.ID = "master-diagram"
					s.deck.Objects.Builds["master-diagram"] = cloneBuild(s.deck.Objects.Builds["diagram-slide"])
				}
				s.selected, s.selection = 0, map[int]bool{0: true}
				before := cloneDeck(s.deck)
				version := s.version
				if err := s.apply(nativeEditorAction{Action: actionName, Element: 0, ObjectIDs: []string{"left-shape"}, SceneRevision: &version}); err != nil {
					t.Fatal(err)
				}
				target := s.deck.Slides[0]
				if master {
					target = s.deck.Masters.Base.Slide
				}
				if len(target.Elements) != 1 || target.Elements[0].ID != "right-shape" || elementGroup(target.Elements[0]) != "" {
					t.Fatal("deleted endpoint left a line or singleton group", target.Elements)
				}
				build := s.deck.Objects.Builds[target.ID]
				if len(build.Steps) != 1 || !reflect.DeepEqual(build.Steps[0].TargetIDs, []string{"right-shape"}) {
					t.Fatal("timeline retained a removed target or empty step", build)
				}
				if err := validateDeckObjectReferences(s.deck); err != nil {
					t.Fatal(err)
				}
				if len(s.undo) != 1 {
					t.Fatal("reference cleanup must belong to the deletion transaction")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("undo did not restore all references", err)
				}
			})
		}
	}
}

func TestObjectReferenceCloneRemapsDiagramAndBuild(t *testing.T) {
	s := newNativeEditorSession("references.md", referenceDeck())
	if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
		t.Fatal(err)
	}
	if err := validateDeckObjectReferences(s.deck); err != nil {
		t.Fatal(err)
	}
	source, copied := s.deck.Slides[0], s.deck.Slides[1]
	if copied.ID == source.ID || copied.Elements[0].ID == source.Elements[0].ID {
		t.Fatal("clone reused stable identity")
	}
	if len(s.deck.Objects.Builds[copied.ID].Steps) != 2 {
		t.Fatal("clone lost its timeline")
	}
}

func TestObjectReferenceSettingsPreserveStableSlideIdentity(t *testing.T) {
	for _, mode := range []string{"slide", "base", "layout", "base-api", "layout-api"} {
		t.Run(mode, func(t *testing.T) {
			s := newNativeEditorSession("references.md", referenceDeck())
			target := &s.deck.Slides[0]
			action := nativeEditorAction{Action: "update-slide", SlideData: &Slide{ID: "untrusted-new-identity", BG: "#aabbcc"}}
			switch mode {
			case "base", "base-api":
				target = &s.deck.Masters.Base.Slide
				target.ID = "master-base-source"
				if mode == "base" {
					s.masterMode = true
				} else {
					action.Action, action.Kind = "update-layout", "base"
				}
			case "layout", "layout-api":
				target = &s.deck.Masters.Layouts[0].Slide
				target.ID = "layout-source"
				if mode == "layout" {
					s.masterMode, s.currentMaster = true, 1
				} else {
					action.Action, action.Kind = "update-layout", s.deck.Masters.Layouts[0].ID
				}
			}
			id := target.ID
			s.deck.Objects.Builds[id] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "fade"}}
			before := cloneDeck(s.deck)
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			if !authoredSlideIdentities(s.deck)[id] {
				t.Fatal("settings edit replaced stable slide identity")
			}
			if _, ok := s.deck.Objects.Builds[id]; !ok {
				t.Fatal("settings edit discarded timeline")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("settings undo did not restore authored state", err)
			}
		})
	}
}

func TestObjectReferenceMasterDeletionReconcilesInheritedConnectors(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable-slide", true: "legacy-slide"}[legacy], func(t *testing.T) {
			d := referenceDeck()
			d.Masters = defaultMasterDeck()
			d.Masters.Base.Slide = referenceDiagram()
			d.Masters.Base.Slide.ID = "base-diagram"
			d.Slides[0].Elements, d.Slides[0].LayoutID = nil, "blank"
			if legacy {
				d.Slides[0].ID = ""
				d.Objects.Builds = nil
			}
			s := newNativeEditorSession("references.md", d)
			s.masterMode = true
			before := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "delete-element", Element: 0}); err != nil {
				t.Fatal(err)
			}
			if len(s.deck.Masters.Base.Slide.Elements) != 1 {
				t.Fatal("master retained orphaned connector")
			}
			if !legacy {
				build := s.deck.Objects.Builds["diagram-slide"]
				if len(build.Steps) != 1 || !reflect.DeepEqual(build.Steps[0].TargetIDs, []string{"right-shape"}) {
					t.Fatal("normal-slide timeline retained removed inherited connector", build)
				}
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("master deletion undo lost inherited references", err)
			}
		})
	}
}

func TestObjectReferenceDeletedLayoutCleansLegacySlideConnector(t *testing.T) {
	d := referenceDeck()
	d.Masters = defaultMasterDeck()
	d.Masters.Layouts[0].Slide = Slide{ID: "blank-source", Elements: []Element{{ID: "layout-shape", Kind: "shape", Query: "shape=square"}}}
	d.Slides[0].ID, d.Slides[0].LayoutID = "", "blank"
	d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to", "layout-shape")
	d.Objects.Builds = map[string]SlideBuild{"blank-source": {Version: 1}}
	s := newNativeEditorSession("references.md", d)
	s.masterMode, s.currentMaster = true, 1
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "delete-slide"}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Slides[0].Elements) != 2 || s.deck.Slides[0].LayoutID != "" || len(s.deck.Objects.Builds) != 0 {
		t.Fatal("deleted master left inherited references behind")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
		t.Fatal("layout deletion undo failed", err)
	}
}

func TestObjectReferenceCleanupRejectsNewOrphans(t *testing.T) {
	before := referenceDeck()
	after := cloneDeck(before)
	after.Objects.Builds["new-orphan"] = SlideBuild{Version: 1}
	if err := reconcileEditorObjectReferences(&after, before, nativeEditorAction{Action: "update-elements"}, 0, false); err == nil {
		t.Fatal("cleanup silently accepted a newly invalid timeline")
	}
	if _, ok := after.Objects.Builds["new-orphan"]; !ok {
		t.Fatal("cleanup discarded new invalid data rather than rejecting it")
	}
}

func TestObjectReferenceRejectedConnectorEditIsAtomic(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("references.md", referenceDeck())
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "base-diagram"
		}
		s.selected, s.selection = 2, map[int]bool{2: true}
		bad := s.deck.Slides[0].Elements[2]
		bad.Query = setQueryValue(bad.Query, "connector-to", "missing-shape")
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "update-element", Element: 2, ElementData: &bad})
	}
}

func TestObjectReferenceReorderAndDeletionKeepTimelineOwnership(t *testing.T) {
	s := newNativeEditorSession("references.md", referenceDeck())
	s.deck.Slides = append(s.deck.Slides, Slide{ID: "second-slide", Elements: []Element{{Kind: "text", ID: "second-text", Text: "Second"}}})
	s.deck.Objects.Builds["second-slide"] = SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"second-text"}, Effect: "appear"}}}
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "reorder-slide", Slide: 0, Value: 1}); err != nil {
		t.Fatal(err)
	}
	if s.deck.Slides[1].ID != "diagram-slide" || !reflect.DeepEqual(before.Objects, s.deck.Objects) {
		t.Fatal("reorder changed timeline ownership")
	}
	if err := s.apply(nativeEditorAction{Action: "delete-slide"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.deck.Objects.Builds["diagram-slide"]; ok {
		t.Fatal("deleted slide retained its timeline")
	}
	if _, ok := s.deck.Objects.Builds["second-slide"]; !ok {
		t.Fatal("deleted wrong timeline")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before.Objects, s.deck.Objects) {
		t.Fatal("undo did not restore deleted timeline", err)
	}
}

func TestObjectReferenceCleanupRetainsSelectionByID(t *testing.T) {
	d := referenceDeck()
	d.Masters = defaultMasterDeck()
	d.Masters.Layouts[0].Slide.Elements = []Element{{Kind: "shape", ID: "master-endpoint", Query: "shape=square"}}
	d.Slides[0].LayoutID = "blank"
	d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "connector-to", "master-endpoint")
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{Kind: "text", ID: "keep-selected", Text: "Keep me", Query: "top=20"})
	s := newNativeEditorSession("references.md", d)
	s.selected, s.selection = 3, map[int]bool{3: true}
	if err := s.apply(nativeEditorAction{Action: "update-layout", Kind: "blank", SlideData: &Slide{}}); err != nil {
		t.Fatal(err)
	}
	if s.selected < 0 || s.deck.Slides[0].Elements[s.selected].ID != "keep-selected" || len(s.selection) != 1 || !s.selection[s.selected] {
		t.Fatal("dependent connector deletion retargeted the selection", s.selected, s.selection)
	}
}

func TestObjectReferenceLockedDependentRejectsWholeDeletion(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession("references.md", referenceDeck())
		target := &s.deck.Slides[0]
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(*target)
			s.deck.Masters.Base.Slide.ID = "base-diagram"
			target = &s.deck.Masters.Base.Slide
		}
		target.Elements[2].Query = setQueryValue(target.Elements[2].Query, "object-locked", "1")
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "delete-element", Element: 0})
	}
}
