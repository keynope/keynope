package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func clipboardMatrixScope(s *nativeEditorSession, scope, prefix string) {
	s.deck.EnsureDefaultMasters()
	owner := cloneSlide(s.deck.Slides[0])
	owner.ID = prefix + "-owner"
	owner.Notes = "PRIVATE-MATRIX-NOTES"
	owner.Engagement = &EngagementDefinition{ID: prefix + "-activity", Kind: "storm", Prompt: "PRIVATE-MATRIX-ACTIVITY"}
	if build, ok := s.deck.Objects.Builds[s.deck.Slides[0].ID]; ok {
		s.deck.Objects.Builds[owner.ID] = cloneBuild(build)
	}
	switch scope {
	case "base":
		s.deck.Masters.Base.Slide = owner
		s.masterMode, s.currentMaster = true, 0
	case "layout":
		s.deck.Masters.Layouts[0].Slide = owner
		s.masterMode, s.currentMaster = true, 1
	default:
		delete(s.deck.Objects.Builds, s.deck.Slides[0].ID)
		s.deck.Slides[0] = owner
		if scope == "tab" {
			s.deck.Slides[0].TabID = prefix + "-tab"
			syncSlideTabs(&s.deck)
		}
	}
}

func clipboardMatrixOwner(s *nativeEditorSession) Slide {
	if s.masterMode {
		return *masterSlideAt(&s.deck, s.currentMaster)
	}
	return s.deck.Slides[s.current]
}

func TestPreparedClipboardAllSourceDestinationScopes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, from := range []string{"slide", "tab", "base", "layout"} {
		for _, to := range []string{"slide", "tab", "base", "layout"} {
			t.Run(fmt.Sprintf("%s-to-%s", from, to), func(t *testing.T) {
				source := newNativeEditorSession("Source.md", clipboardGeometryFixture(t))
				clipboardMatrixScope(source, from, "source")
				sourceBefore, sourceRevision := cloneDeck(source.deck), source.version
				bundle := copyGeometryViaHTTP(t, source)
				wire, err := json.Marshal(bundle)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(wire, []byte("PRIVATE-MATRIX")) || bundle.Objects == nil || len(bundle.Objects.Builds) != 0 {
					t.Fatal("copy leaked note/activity/timeline")
				}
				if !reflect.DeepEqual(source.deck, sourceBefore) || source.version != sourceRevision || len(source.undo) != 0 {
					t.Fatal("copy mutated source")
				}
				bundleBefore := append([]byte(nil), wire...)
				destination := newNativeEditorSession("Destination.md", clipboardGeometryFixture(t))
				clipboardMatrixScope(destination, to, "destination")
				before, revision := cloneDeck(destination.deck), destination.version
				originalOwner := clipboardMatrixOwner(destination)
				action := nativeEditorAction{Action: "paste-objects", SceneRevision: &revision, Clipboard: &bundle}
				request, err := json.Marshal(action)
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				destination.handlePasteObjectClipboard(response, httptest.NewRequest(http.MethodPost, "/api/editor/paste-clipboard", bytes.NewReader(request)))
				if response.Code != http.StatusOK {
					t.Fatal(response.Code, response.Body.String())
				}
				if len(destination.undo) != 1 || destination.version != revision+1 {
					t.Fatal("paste was not one transaction")
				}
				if err := validateDeckObjectReferences(destination.deck); err != nil {
					t.Fatal(err)
				}
				owner := clipboardMatrixOwner(destination)
				if owner.ID != originalOwner.ID || len(owner.Elements) != len(originalOwner.Elements)+len(bundle.Elements) {
					t.Fatal("paste changed owner identity/count")
				}
				if !reflect.DeepEqual(destination.deck.Objects.Builds, before.Objects.Builds) {
					t.Fatal("object paste copied or replaced slide timelines")
				}
				oldIDs, newIDs := map[string]bool{}, map[string]bool{}
				for _, e := range originalOwner.Elements {
					oldIDs[e.ID] = true
				}
				pasted := map[string]Element{}
				group := ""
				for _, e := range owner.Elements {
					if oldIDs[e.ID] {
						continue
					}
					if newIDs[e.ID] || e.ID == "" {
						t.Fatal("nonunique pasted identity")
					}
					newIDs[e.ID] = true
					name := mustQuery(e.Query).Get("object-name")
					pasted[name] = e
					if e.ID == name {
						t.Fatal("paste reused source ID", name)
					}
					if group == "" {
						group = elementGroup(e)
					}
					if elementGroup(e) != group || group == "group-source" || group == "" {
						t.Fatal("group identity was not remapped together")
					}
					if e.Kind == "table" && (e.ObjectID == "table-1" || !reflect.DeepEqual(destination.deck.Objects.Tables[e.ObjectID], bundle.Objects.Tables["table-1"])) {
						t.Fatal("table payload not freshly copied")
					}
					if e.Kind == "chart" && (e.ObjectID == "chart-1" || !reflect.DeepEqual(destination.deck.Objects.Charts[e.ObjectID], bundle.Objects.Charts["chart-1"])) {
						t.Fatal("chart payload not freshly copied")
					}
				}
				if len(pasted) != len(bundle.Elements) {
					t.Fatal("pasted names lost or duplicated")
				}
				line := mustQuery(pasted["wire"].Query)
				if line.Get("connector-from") != pasted["from"].ID || line.Get("connector-to") != pasted["to"].ID {
					t.Fatal("connector still addresses source/colliding destination")
				}
				for _, old := range originalOwner.Elements {
					found := false
					for _, now := range owner.Elements {
						if now.ID == old.ID {
							found = true
							if !reflect.DeepEqual(old, now) {
								t.Fatal("paste mutated original destination element")
							}
						}
					}
					if !found {
						t.Fatal("paste removed original element")
					}
				}
				accepted := cloneDeck(destination.deck)
				if err := destination.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(destination.deck, before) {
					t.Fatal("paste Undo not exact", err)
				}
				if err := destination.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(destination.deck, accepted) {
					t.Fatal("paste Redo not exact", err)
				}
				data, err := serializeDeck("Destination.md", destination.deck)
				if err != nil {
					t.Fatal(err)
				}
				reopened, err := parseDeckData("Destination.md", data)
				if err != nil {
					t.Fatal(err)
				}
				if err := validateDeckObjectReferences(reopened); err != nil {
					t.Fatal(err)
				}
				var reopenedOwner Slide
				if destination.masterMode {
					reopenedOwner = *masterSlideAt(&reopened, destination.currentMaster)
				} else {
					reopenedOwner = reopened.Slides[destination.current]
				}
				for _, e := range reopenedOwner.Elements {
					delete(newIDs, e.ID)
				}
				if len(newIDs) != 0 || reopenedOwner.ID != owner.ID {
					t.Fatal("save/reopen changed identities")
				}
				wire, err = json.Marshal(bundle)
				if err != nil || !bytes.Equal(wire, bundleBefore) || !reflect.DeepEqual(source.deck, sourceBefore) {
					t.Fatal("paste/history/save mutated clipboard source", err)
				}
			})
		}
	}
}
