package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func layoutReferenceLifecycleDeck() Deck {
	d := Deck{Masters: defaultMasterDeck(), Objects: &DeckObjects{Version: 1, Builds: map[string]SlideBuild{}}}
	d.Masters.Layouts[2].Slide = Slide{ID: "layout-source", PageNumber: pageNumberHide, Elements: []Element{{ID: "layout-endpoint", Kind: "shape", Query: "shape=rounded&left=170&top=10&width=40&height=14&fg=%2355aaff"}}}
	d.Objects.Builds["layout-source"] = SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"layout-endpoint"}, Effect: "appear"}}}
	d.Slides = append(d.Slides, Slide{ID: "unrelated", LayoutID: "blank", Notes: "Unrelated notes", Elements: []Element{{ID: "unrelated-text", Kind: "text", Text: "Unaffected slide", Query: "render=truetype&modern-font=sans&modern-size=40&left=10&top=10"}}})
	for i := 1; i <= 2; i++ {
		id := strconv.Itoa(i)
		slide := Slide{ID: "consumer-" + id, LayoutID: d.Masters.Layouts[2].ID, Notes: "Keep participant instructions", Elements: []Element{{ID: "local-" + id, Kind: "shape", Query: "shape=square&left=20&top=10&width=40&height=14&group=pair&fg=%230e7490"}, {ID: "wire-" + id, Kind: "connector", Query: "connector-from=local-" + id + "&connector-from-side=right&connector-to=layout-endpoint&connector-to-side=left&connector-mode=elbow&group=pair&fg=%23ff0055"}, {ID: "caption-" + id, Kind: "text", Text: "Local content remains", Query: "render=truetype&modern-font=sans&modern-size=40&left=20&top=35"}}}
		if i == 2 {
			slide.TabID = "guide"
		}
		d.Slides = append(d.Slides, slide)
		d.Objects.Builds[slide.ID] = SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"local-" + id, "layout-endpoint"}, Effect: "appear"}, {TargetIDs: []string{"wire-" + id}, Effect: "fade"}}}
	}
	return d
}

func TestLayoutReferenceLifecycleReconcilesAllConsumersAtomically(t *testing.T) {
	for _, operation := range []string{"replace", "delete-api", "delete-master", "rebind"} {
		for _, locked := range []bool{false, true} {
			t.Run(operation+"/locked="+strconv.FormatBool(locked), func(t *testing.T) {
				s := newNativeEditorSession("Lifecycle.md", layoutReferenceLifecycleDeck())
				if locked {
					s.deck.Slides[2].Elements[1].Query += "&object-locked=1"
				}
				action := nativeEditorAction{Action: "update-layout", Kind: s.deck.Masters.Layouts[2].ID, SlideData: &Slide{PageNumber: pageNumberHide}}
				switch operation {
				case "delete-api":
					action = nativeEditorAction{Action: "delete-layout", Kind: s.deck.Masters.Layouts[2].ID}
				case "delete-master":
					s.masterMode = true
					s.currentMaster = 3
					action = nativeEditorAction{Action: "delete-slide"}
				case "rebind":
					s.current = 1
					action = nativeEditorAction{Action: "set-layout", Kind: "blank"}
				}
				if locked && operation != "rebind" {
					assertEditorFailureUnchanged(t, s, action)
					return
				}
				before := cloneDeck(s.deck)
				revision := s.version
				if err := s.apply(action); err != nil {
					t.Fatal(err)
				}
				if s.version != revision+1 || len(s.undo) != 1 {
					t.Fatal("layout change was not one transaction")
				}
				if err := validateDeckObjectReferences(s.deck); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before.Slides[0], s.deck.Slides[0]) {
					t.Fatal("unrelated slide changed")
				}
				for i := 1; i <= 2; i++ {
					if operation == "rebind" && i == 2 {
						if !reflect.DeepEqual(before.Slides[i], s.deck.Slides[i]) || !reflect.DeepEqual(before.Objects.Builds[before.Slides[i].ID], s.deck.Objects.Builds[s.deck.Slides[i].ID]) {
							t.Fatal("rebind changed a different consumer")
						}
						continue
					}
					for _, e := range s.deck.Slides[i].Elements {
						if e.Kind == "connector" || elementGroup(e) != "" {
							t.Fatal("removed inherited endpoint left a line or singleton group")
						}
					}
					build := s.deck.Objects.Builds[s.deck.Slides[i].ID]
					if len(build.Steps) != 1 || !reflect.DeepEqual(build.Steps[0].TargetIDs, []string{"local-" + strconv.Itoa(i)}) {
						t.Fatal("timeline targets not reconciled", build)
					}
					if s.deck.Slides[i].Notes != before.Slides[i].Notes || s.deck.Slides[i].TabID != before.Slides[i].TabID {
						t.Fatal("layout operation altered private content/tab binding")
					}
				}
				accepted := cloneDeck(s.deck)
				if _, err := serializeDeck("Lifecycle.md", s.deck); err != nil {
					t.Fatal(err)
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("layout Undo lost consumers", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(accepted, s.deck) {
					t.Fatal("layout Redo lost consumers", err)
				}
				if root := os.Getenv("KEYNOPE_LAYOUT_REFERENCE_EVIDENCE"); root != "" && operation == "replace" && !locked {
					if err := os.MkdirAll(root, 0700); err != nil {
						t.Fatal(err)
					}
					if err := saveDeck(filepath.Join(root, "references.md"), before); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
