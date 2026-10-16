package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func objectInputSession(master bool) *nativeEditorSession {
	e := Element{ID: "owned-source", Kind: "text", Text: "Original", Extra: &jsonExtensions{"future": json.RawMessage(`{"value":1}`)}}
	s := newNativeEditorSession("Owned.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
	if master {
		s.masterMode = true
		s.currentMaster = 0
		s.deck.Masters.Base.Slide.Elements = cloneSlide(s.deck.Slides[0]).Elements
	}
	return s
}

func objectInputTarget(s *nativeEditorSession) *Slide {
	if s.masterMode {
		return masterSlideAt(&s.deck, s.currentMaster)
	}
	return &s.deck.Slides[s.current]
}

func TestEditorSingleObjectOwnsIncomingMetadata(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, name := range []string{"update-element", "add-element"} {
			t.Run(map[bool]string{false: "slide", true: "master"}[master]+"/"+name, func(t *testing.T) {
				s := objectInputSession(master)
				before := cloneDeck(s.deck)
				extra := &jsonExtensions{"future": json.RawMessage(`{"value":1}`)}
				e := Element{ID: "owned-source", Kind: "text", Text: "Changed", Extra: extra}
				if err := s.apply(nativeEditorAction{Action: name, Kind: "text", Element: 0, ElementData: &e}); err != nil {
					t.Fatal(err)
				}
				accepted := cloneDeck(s.deck)
				(*extra)["future"][9] = '2'
				(*extra)["another"] = json.RawMessage(`true`)
				if !reflect.DeepEqual(accepted, s.deck) {
					t.Fatal("accepted metadata aliases the caller")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("owned edit Undo changed metadata", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(accepted, s.deck) {
					t.Fatal("owned edit Redo adopted caller mutation", err)
				}
			})
		}
	}
}

func TestEditorLegacyPasteOwnsRequestBeforeRemapping(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := objectInputSession(master)
		elements := []Element{{ID: "one", Kind: "text", Text: "One", Query: "group=old", Extra: &jsonExtensions{"future": json.RawMessage(`{"value":1}`)}}, {ID: "two", Kind: "text", Text: "Two", Query: "group=old"}}
		before := cloneSlide(Slide{Elements: elements}).Elements
		if err := s.apply(nativeEditorAction{Action: "paste-elements", ElementsData: elements}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(elements, before) {
			t.Fatal("paste rewrote caller-owned identities/groups", master)
		}
		accepted := cloneDeck(s.deck)
		(*elements[0].Extra)["future"][9] = '2'
		if !reflect.DeepEqual(accepted, s.deck) {
			t.Fatal("paste shares request metadata", master)
		}
	}
}

func TestEditorDuplicateOwnsExtensionMetadata(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := objectInputSession(master)
		if err := s.apply(nativeEditorAction{Action: "duplicate-element", Element: 0}); err != nil {
			t.Fatal(err)
		}
		target := objectInputTarget(s)
		(*target.Elements[1].Extra)["future"][9] = '2'
		if string((*target.Elements[0].Extra)["future"]) != `{"value":1}` {
			t.Fatal("duplicate shares source metadata", master)
		}
	}
}

func TestEditorDuplicateLoadedDanglingConnector(t *testing.T) {
	for _, master := range []bool{false, true} {
		d, err := parseDeckData("legacy.md", []byte(legacyDanglingDiagram))
		if err != nil {
			t.Fatal(err)
		}
		s := newNativeEditorSession("legacy.md", d)
		if master {
			s.masterMode = true
			s.currentMaster = 0
			s.deck.Masters.Base.Slide.Elements = cloneSlide(s.deck.Slides[0]).Elements
		}
		i := nativeEditorElementIndex(objectInputTarget(s).Elements, -1, "legacy-line")
		before := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "duplicate-element", Element: i}); err != nil {
			t.Fatal("legacy connector duplicate rejected", master, err)
		}
		if err := validateDeckObjectReferences(s.deck); err != nil {
			t.Fatal(err)
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
			t.Fatal("duplicate Undo lost legacy allowance", err)
		}
	}
}

func TestEditorImageInsertionOwnsUploadedMedia(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := objectInputSession(master)
		asset := clipboardTestAsset(t)
		asset.Frames = []DeckAssetFrame{{Data: append([]byte(nil), asset.Data...), DelayMS: 70}}
		asset.Source = &DeckAssetSource{Width: 2, Height: 2, MIME: asset.MIME, Data: append([]byte(nil), asset.Data...), Frames: []DeckAssetFrame{{Data: append([]byte(nil), asset.Data...), DelayMS: 90}}}
		e := Element{Kind: "image", Path: "keynope-asset:uploaded", AssetID: "uploaded", Query: "left=10&top=4&width=20&height=10"}
		if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "image", ElementData: &e, AssetData: &asset}); err != nil {
			t.Fatal(err)
		}
		accepted := cloneDeck(s.deck)
		asset.Data[0] = 0
		asset.Frames[0].Data[0] = 0
		asset.Source.Data[0] = 0
		asset.Source.Frames[0].Data[0] = 0
		asset.Source.Width = 999
		if !reflect.DeepEqual(accepted, s.deck) {
			t.Fatal("inserted media shares caller buffers", master)
		}
	}
}

func TestEditorRejectedLegacyPasteKeepsRequestAndHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := objectInputSession(master)
		table := structuredFixture().Objects.Tables["table-1"]
		s.deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"table-1": table}}
		elements := []Element{{ID: "table", Kind: "table", ObjectID: "table-1"}, {ID: "from", Kind: "shape", Query: "shape=square"}, {ID: "bad", Kind: "connector", Query: "connector-from=from&connector-from-side=right&connector-to=missing&connector-to-side=left"}}
		before := cloneSlide(Slide{Elements: elements}).Elements
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "paste-elements", ElementsData: elements})
		if !reflect.DeepEqual(elements, before) {
			t.Fatal("failed paste rewrote request identities/payload references", master)
		}
	}
}

func TestEditorObjectOwnershipCannotBeClientDisabled(t *testing.T) {
	s := objectInputSession(false)
	var action nativeEditorAction
	if err := json.Unmarshal([]byte(`{"action":"update-element","element":0,"objectInputOwned":true,"elementData":{"id":"owned-source","kind":"text","text":"Changed","future":{"value":1}}}`), &action); err != nil {
		t.Fatal(err)
	}
	if action.objectInputOwned {
		t.Fatal("client bypassed the ownership boundary")
	}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	before := cloneDeck(s.deck)
	(*action.ElementData.Extra)["future"][9] = '2'
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("JSON input metadata aliases the live document")
	}
}

func TestEditorMasterCloneOwnsDiagramPayloadsAndBuilds(t *testing.T) {
	for _, context := range []string{"base", "layout"} {
		t.Run(context, func(t *testing.T) {
			s := newNativeEditorSession("Clone.md", referenceDeck())
			s.masterMode = true
			s.currentMaster = 0
			if context == "layout" {
				s.currentMaster = 3
			}
			target := masterSlideAt(&s.deck, s.currentMaster)
			*target = referenceDiagram()
			target.ID = "source-master-" + context
			target.Elements[0].Query = "shape=square&left=20&top=10&width=35&height=12&group=pair&fg=%230e7490"
			target.Elements[1].Query = "shape=rounded&left=170&top=10&width=35&height=12&group=pair&fg=%230e7490"
			target.Elements[2].Query += "&connector-mode=elbow&fg=%23ff0055"
			structured := structuredFixture()
			s.deck.Objects.Tables = structured.Objects.Tables
			s.deck.Objects.Charts = structured.Objects.Charts
			target.Elements = append(target.Elements, structured.Slides[0].Elements...)
			target.Elements[3].Query = "left=10&top=30&width=95&height=18"
			target.Elements[4].Query = "left=130&top=30&width=100&height=18"
			target.Elements = append(target.Elements, Element{ID: "placeholder", Kind: "heading", Level: 1, Text: "Connected master", Query: "render=truetype&modern-font=sans&modern-size=40&left=10&top=0&text-box=1&width=200&height=6", PlaceholderRole: placeholderTitle, SlotID: "source-slot"})
			build := cloneBuild(s.deck.Objects.Builds["diagram-slide"])
			build.Steps = append(build.Steps, BuildStep{TargetIDs: []string{"table-element", "chart-element", "placeholder"}, Effect: "appear"})
			s.deck.Objects.Builds[target.ID] = build
			before := cloneDeck(s.deck)
			source := cloneSlide(*target)
			if root := os.Getenv("KEYNOPE_OWNERSHIP_EVIDENCE"); root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := saveDeck(filepath.Join(root, context+".md"), before); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
				t.Fatal("cloning connected master failed", err)
			}
			if err := validateDeckObjectReferences(s.deck); err != nil {
				t.Fatal(err)
			}
			copied := masterSlideAt(&s.deck, s.currentMaster)
			if copied.ID == source.ID || len(s.deck.Objects.Builds[copied.ID].Steps) != len(build.Steps) {
				t.Fatal("master build identity lost")
			}
			ids := map[string]bool{}
			for _, e := range source.Elements {
				ids[e.ID] = true
			}
			for _, e := range copied.Elements {
				if ids[e.ID] {
					t.Fatal("copied master reused source identity")
				}
				if e.PlaceholderRole != "" && e.SlotID != e.ID {
					t.Fatal("copied placeholder slot not reidentified")
				}
				if e.Kind == "connector" {
					q, _ := url.ParseQuery(e.Query)
					if ids[q.Get("connector-from")] || ids[q.Get("connector-to")] {
						t.Fatal("copied connector attaches to original master")
					}
				}
				if elementGroup(e) == "pair" {
					t.Fatal("copy shares a visible inherited group")
				}
				if e.Kind == "table" && e.ObjectID == "table-1" || e.Kind == "chart" && e.ObjectID == "chart-1" {
					t.Fatal("master clone shares a mutable structured payload")
				}
			}
			for _, step := range s.deck.Objects.Builds[copied.ID].Steps {
				for _, id := range step.TargetIDs {
					if ids[id] {
						t.Fatal("copied master build still targets source")
					}
				}
			}
			if _, err := serializeDeck("Clone.md", s.deck); err != nil {
				t.Fatal(err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("master clone Undo lost ownership", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
